//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

const (
	privateHTTPDiagnosticTimeout = time.Minute
	privateHTTPDiagnosticMaxBody = 1 << 20
)

type privateHTTPDiagnosticOrigin struct {
	Member, OriginGroup, Origin, OriginID, PLSID string
}

type privateHTTPDiagnosticHTTP struct {
	UTC, Method, Resource, APIVersion, RequestID, CorrelationID string
	Status                                                      int
	Raw                                                         map[string]interface{} `json:"raw"`
	Error                                                       string
}

type privateHTTPDiagnosticSDK struct {
	Operation, Resource string
	Fields              map[string]interface{} `json:"fields"`
}

type privateHTTPDiagnostics struct {
	UTC           string
	Expected      []privateHTTPDiagnosticOrigin
	HTTP          []privateHTTPDiagnosticHTTP
	SDK           []privateHTTPDiagnosticSDK `json:"sdk"`
	Issues        []string
	Complete      bool
	GatewayStatus *gatewayv1.GatewayStatus
	Controller    []map[string]interface{}
}

type privateHTTPDiagnosticCapture struct {
	snapshot    *privateHTTPDiagnostics
	paths       map[string]map[string]bool
	description string
	errors      []error
}

func (capture *privateHTTPDiagnosticCapture) issue(operation string, err error) {
	if err == nil {
		return
	}
	message := fmt.Sprintf("%T", err)
	var responseError *azcore.ResponseError
	if errors.As(err, &responseError) {
		// Do not serialize the SDK error's entire response body or headers.
		message = fmt.Sprintf("HTTP %d code=%s", responseError.StatusCode, responseError.ErrorCode)
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		message = "request context canceled or expired"
	}
	capture.snapshot.Issues = append(capture.snapshot.Issues, operation+": "+message)
	capture.errors = append(capture.errors, fmt.Errorf("%s: %w", operation, err))
}

func (capture *privateHTTPDiagnosticCapture) Do(request *policy.Request) (*http.Response, error) {
	raw := request.Raw()
	if err := raw.Context().Err(); err != nil {
		return nil, err
	}
	expected, allowed := capture.paths[strings.ToLower(raw.URL.Path)]
	if raw.Method != http.MethodGet || raw.URL.Scheme != "https" ||
		!strings.EqualFold(raw.URL.Host, "management.azure.com") || !allowed {
		return nil, fmt.Errorf("refusing diagnostic request outside the expected read-only resource paths")
	}
	response, err := request.Next()
	if response == nil {
		return response, err
	}
	record := privateHTTPDiagnosticHTTP{
		UTC: time.Now().UTC().Format(time.RFC3339Nano), Method: raw.Method, Resource: raw.URL.Path,
		APIVersion: raw.URL.Query().Get("api-version"), Status: response.StatusCode,
		RequestID: response.Header.Get("x-ms-request-id"), CorrelationID: response.Header.Get("x-ms-correlation-request-id"),
	}
	if response.Body == nil {
		record.Error = "response has no body"
	} else {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, privateHTTPDiagnosticMaxBody+1))
		// Replay every consumed byte (and read error) to the SDK; capture must not
		// change decoding, even for truncated diagnostic output.
		response.Body = &privateHTTPDiagnosticBody{prefix: bytes.NewReader(body), source: response.Body, readErr: readErr}
		switch {
		case readErr != nil:
			record.Error = "response body capture failed"
			capture.issue("capture "+raw.URL.Path, readErr)
		case len(body) > privateHTTPDiagnosticMaxBody:
			record.Error = "response exceeds diagnostic capture limit"
		default:
			record.Raw, readErr = projectPrivateHTTPDiagnosticBody(body, expected, capture.description)
			if readErr != nil {
				record.Error = readErr.Error()
			}
		}
	}
	if record.Error != "" {
		capture.issue("capture "+raw.URL.Path, errors.New(record.Error))
	}
	capture.snapshot.HTTP = append(capture.snapshot.HTTP, record)
	return response, err
}

type privateHTTPDiagnosticBody struct {
	prefix  *bytes.Reader
	source  io.ReadCloser
	readErr error
}

func (body *privateHTTPDiagnosticBody) Read(buffer []byte) (int, error) {
	if body.prefix.Len() > 0 {
		return body.prefix.Read(buffer)
	}
	if body.readErr != nil {
		err := body.readErr
		body.readErr = nil
		return 0, err
	}
	return body.source.Read(buffer)
}

func (body *privateHTTPDiagnosticBody) Close() error { return body.source.Close() }

func diagnosticPrivateHTTPFields(object map[string]json.RawMessage, names ...string) map[string]interface{} {
	result := make(map[string]interface{})
	for _, name := range names {
		if value, found := object[name]; found {
			result[name] = value
		}
	}
	return result
}

func diagnosticPrivateHTTPKeys(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func diagnosticPrivateHTTPDescription(object map[string]json.RawMessage, key, expected string, fields map[string]interface{}) {
	raw, found := object[key]
	if !found {
		return
	}
	var value string
	matches := json.Unmarshal(raw, &value) == nil && value == expected
	fields[key+"Present"] = true
	fields[key+"MatchesOwnedProfile"] = matches
	if matches {
		fields[key] = raw
	}
}

func projectPrivateHTTPDiagnosticObject(raw json.RawMessage, description string) (map[string]interface{}, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("diagnostic resource is not a JSON object")
	}
	result := diagnosticPrivateHTTPFields(object, "id", "name", "location")
	properties, found := object["properties"]
	if !found {
		return result, nil
	}
	if bytes.Equal(bytes.TrimSpace(properties), []byte("null")) {
		result["properties"] = json.RawMessage("null")
		return result, nil
	}
	fields, err := projectPrivateHTTPDiagnosticProperties(properties, description)
	if err != nil {
		return nil, err
	}
	result["properties"] = fields
	return result, nil
}

func projectPrivateHTTPDiagnosticProperties(raw json.RawMessage, description string) (map[string]interface{}, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("diagnostic properties are not a JSON object")
	}
	result := diagnosticPrivateHTTPFields(object, "provisioningState", "deploymentStatus", "enabledState",
		"hostName", "originHostHeader", "httpPort", "httpsPort", "originGroupName")
	result["fieldNames"] = diagnosticPrivateHTTPKeys(object)
	for _, key := range []string{"sharedPrivateLinkResource", "privateLinkServiceConnectionState", "privateEndpoint"} {
		value, found := object[key]
		if !found {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			result[key] = json.RawMessage("null")
			continue
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(value, &nested); err != nil || nested == nil {
			return nil, fmt.Errorf("diagnostic %s is not a JSON object", key)
		}
		selected := diagnosticPrivateHTTPFields(nested, "id", "status", "groupId", "privateLinkLocation")
		selected["fieldNames"] = diagnosticPrivateHTTPKeys(nested)
		diagnosticPrivateHTTPDescription(nested, "requestMessage", description, selected)
		diagnosticPrivateHTTPDescription(nested, "description", description, selected)
		if link, found := nested["privateLink"]; found {
			if bytes.Equal(bytes.TrimSpace(link), []byte("null")) {
				selected["privateLink"] = json.RawMessage("null")
				result[key] = selected
				continue
			}
			var reference map[string]json.RawMessage
			if err := json.Unmarshal(link, &reference); err != nil {
				return nil, fmt.Errorf("diagnostic privateLink is not an object")
			}
			selected["privateLink"] = diagnosticPrivateHTTPFields(reference, "id")
		}
		result[key] = selected
	}
	if rawConnections, found := object["privateEndpointConnections"]; found {
		if bytes.Equal(bytes.TrimSpace(rawConnections), []byte("null")) {
			result["privateEndpointConnections"] = json.RawMessage("null")
			return result, nil
		}
		var connections []json.RawMessage
		if err := json.Unmarshal(rawConnections, &connections); err != nil {
			return nil, fmt.Errorf("diagnostic PLS connections are not an array")
		}
		projected := make([]map[string]interface{}, 0, len(connections))
		for _, connection := range connections {
			fields, err := projectPrivateHTTPDiagnosticObject(connection, description)
			if err != nil {
				return nil, err
			}
			projected = append(projected, fields)
		}
		result["privateEndpointConnections"] = projected
	}
	return result, nil
}

var (
	privateHTTPDiagnosticErrorCodes = regexp.MustCompile(`ERROR CODE:\s*([A-Za-z][A-Za-z0-9]+)`)
	privateHTTPDiagnosticStatuses   = regexp.MustCompile(`RESPONSE\s+([0-9]{3})`)
	privateHTTPDiagnosticActions    = regexp.MustCompile(`(?i)Microsoft\.(?:Network|Cdn|Authorization)/[A-Za-z/]+`)
)

func projectPrivateHTTPControllerDiagnostics(logs, namespace, name string, origins []privateHTTPDiagnosticOrigin) []map[string]interface{} {
	lines := strings.Split(logs, "\n")
	result := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		start := strings.IndexByte(line, '{')
		if start < 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(line[start:]), &fields) != nil {
			continue
		}
		var observedNamespace, observedName, detail string
		if json.Unmarshal(fields["namespace"], &observedNamespace) != nil ||
			json.Unmarshal(fields["name"], &observedName) != nil ||
			observedNamespace != namespace || observedName != name ||
			json.Unmarshal(fields["error"], &detail) != nil {
			continue
		}
		record := diagnosticPrivateHTTPFields(fields, "controller", "controllerGroup", "controllerKind", "namespace", "name", "reconcileID")
		record["errorPresent"] = true
		record["errorCodes"] = privateHTTPDiagnosticErrorCodes.FindAllStringSubmatch(detail, -1)
		record["httpStatuses"] = privateHTTPDiagnosticStatuses.FindAllStringSubmatch(detail, -1)
		record["actions"] = privateHTTPDiagnosticActions.FindAllString(detail, -1)
		var referenced []string
		for _, origin := range origins {
			for _, id := range []string{origin.OriginID, origin.PLSID} {
				if strings.Contains(strings.ToLower(detail), strings.ToLower(id)) {
					referenced = append(referenced, id)
				}
			}
		}
		record["expectedResources"] = referenced
		result = append(result, record)
	}
	return result
}

func projectPrivateHTTPDiagnosticBody(body []byte, expected map[string]bool, description string) (map[string]interface{}, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, fmt.Errorf("diagnostic response is not a JSON object")
	}
	if rawError, found := envelope["error"]; found {
		var detail map[string]json.RawMessage
		if err := json.Unmarshal(rawError, &detail); err != nil {
			return nil, fmt.Errorf("diagnostic error is not an object")
		}
		return map[string]interface{}{"error": diagnosticPrivateHTTPFields(detail, "code")}, nil
	}
	rawValues, collection := envelope["value"]
	if !collection {
		return projectPrivateHTTPDiagnosticObject(body, description)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(rawValues, &values); err != nil {
		return nil, fmt.Errorf("diagnostic origin collection is not an array")
	}
	items := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		var identity struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(value, &identity); err != nil {
			return nil, fmt.Errorf("diagnostic collection contains invalid origin JSON")
		}
		if !expected[strings.ToLower(identity.ID)] {
			continue
		}
		fields, err := projectPrivateHTTPDiagnosticObject(value, description)
		if err != nil {
			return nil, err
		}
		items = append(items, fields)
	}
	return map[string]interface{}{"value": items, "valueCount": len(values),
		"valueNull": bytes.Equal(bytes.TrimSpace(rawValues), []byte("null"))}, nil
}

func (capture *privateHTTPDiagnosticCapture) sdk(operation, resource string, value interface{}) {
	data, err := json.Marshal(value)
	if err != nil {
		capture.issue("decode "+operation, err)
		return
	}
	fields, err := projectPrivateHTTPDiagnosticBody(data, capture.paths[strings.ToLower(resource)], capture.description)
	if err != nil {
		capture.issue("decode "+operation, err)
		return
	}
	capture.snapshot.SDK = append(capture.snapshot.SDK, privateHTTPDiagnosticSDK{Operation: operation, Resource: resource, Fields: fields})
}

func collectPrivateHTTPDiagnostics(ctx context.Context, scope frontdoor.ResourceScope, owner frontdoor.GatewayOwner,
	plan frontdoor.BackendPlan, credential azcore.TokenCredential, options *arm.ClientOptions) (privateHTTPDiagnostics, error) {
	snapshot := privateHTTPDiagnostics{UTC: time.Now().UTC().Format(time.RFC3339Nano)}
	capture := &privateHTTPDiagnosticCapture{snapshot: &snapshot, paths: make(map[string]map[string]bool)}
	roots, err := frontdoor.BuildRootRequests(scope, owner)
	if err != nil {
		capture.issue("diagnostic owner", err)
		return snapshot, errors.Join(capture.errors...)
	}
	if plan.ProfileName != roots.ProfileName || len(plan.OriginGroups) == 0 {
		capture.issue("diagnostic intent", errors.New("backend plan does not match the owned profile"))
		return snapshot, errors.Join(capture.errors...)
	}
	capture.description = "Fleet AFD private origin for " + roots.ProfileName
	capture.paths[strings.ToLower(roots.ProfileID)] = map[string]bool{strings.ToLower(roots.ProfileID): true}
	for _, group := range plan.OriginGroups {
		list := roots.ProfileID + "/originGroups/" + group.Name + "/origins"
		capture.paths[strings.ToLower(list)] = make(map[string]bool)
		for _, origin := range group.Origins {
			id := list + "/" + origin.Name
			capture.paths[strings.ToLower(list)][strings.ToLower(id)] = true
			capture.paths[strings.ToLower(id)] = map[string]bool{strings.ToLower(id): true}
			snapshot.Expected = append(snapshot.Expected, privateHTTPDiagnosticOrigin{
				Member: origin.Cluster, OriginGroup: group.Name, Origin: origin.Name, OriginID: id, PLSID: origin.PrivateLink.ResourceID,
			})
		}
	}
	for _, link := range plan.PrivateLinks {
		id, err := arm.ParseResourceID(link.Target.ResourceID)
		if err != nil || !strings.EqualFold(id.SubscriptionID, scope.SubscriptionID) ||
			!strings.EqualFold(id.ResourceType.String(), "Microsoft.Network/privateLinkServices") {
			capture.issue("diagnostic PLS", errors.New("refusing unexpected PLS resource scope"))
			return snapshot, errors.Join(capture.errors...)
		}
		capture.paths[strings.ToLower(link.Target.ResourceID)] = map[string]bool{strings.ToLower(link.Target.ResourceID): true}
	}
	clientOptions := arm.ClientOptions{}
	if options != nil {
		clientOptions = *options
	}
	clientOptions.Retry.MaxRetries = -1
	clientOptions.PerRetryPolicies = append(append([]policy.Policy{}, clientOptions.PerRetryPolicies...), capture)
	ctx, cancel := context.WithTimeout(ctx, privateHTTPDiagnosticTimeout)
	defer cancel()
	profiles, err := armcdn.NewProfilesClient(scope.SubscriptionID, credential, &clientOptions)
	if err != nil {
		capture.issue("diagnostic profile client", err)
		return snapshot, errors.Join(capture.errors...)
	}
	profile, err := profiles.Get(ctx, scope.ResourceGroup, roots.ProfileName, nil)
	capture.issue("diagnostic profile GET", err)
	if err == nil {
		capture.sdk("profile GET", roots.ProfileID, profile.Profile)
		err = frontdoor.VerifyProfileOwnership(scope, owner, profile.Profile)
		capture.issue("diagnostic profile ownership", err)
	}
	if err == nil {
		collectPrivateHTTPOriginDiagnostics(ctx, capture, scope, roots, plan, credential, &clientOptions)
	}
	services, err := armnetwork.NewPrivateLinkServicesClient(scope.SubscriptionID, credential, &clientOptions)
	if err != nil {
		capture.issue("diagnostic PLS client", err)
		return snapshot, errors.Join(capture.errors...)
	}
	for _, link := range plan.PrivateLinks {
		id, err := arm.ParseResourceID(link.Target.ResourceID)
		if err != nil {
			capture.issue("diagnostic PLS ID", err)
			continue
		}
		pls, err := services.Get(ctx, id.ResourceGroupName, id.Name, nil)
		capture.issue("diagnostic PLS GET "+link.Target.ResourceID, err)
		if err == nil {
			capture.sdk("PLS GET", link.Target.ResourceID, pls.PrivateLinkService)
		}
	}
	snapshot.Complete = len(capture.errors) == 0
	return snapshot, errors.Join(capture.errors...)
}

func collectPrivateHTTPOriginDiagnostics(ctx context.Context, capture *privateHTTPDiagnosticCapture,
	scope frontdoor.ResourceScope, roots frontdoor.RootRequests, plan frontdoor.BackendPlan,
	credential azcore.TokenCredential, options *arm.ClientOptions) {
	origins, err := armcdn.NewAFDOriginsClient(scope.SubscriptionID, credential, options)
	if err != nil {
		capture.issue("diagnostic origin client", err)
		return
	}
	for _, group := range plan.OriginGroups {
		resource := roots.ProfileID + "/originGroups/" + group.Name + "/origins"
		pager := origins.NewListByOriginGroupPager(scope.ResourceGroup, roots.ProfileName, group.Name, nil)
		for pager.More() {
			page, err := pager.NextPage(ctx)
			capture.issue("diagnostic origin LIST "+group.Name, err)
			if err != nil {
				break
			}
			capture.sdk("origin LIST", resource, page.AFDOriginListResult)
		}
		for _, origin := range group.Origins {
			response, err := origins.Get(ctx, scope.ResourceGroup, roots.ProfileName, group.Name, origin.Name, nil)
			capture.issue("diagnostic origin GET "+origin.Name, err)
			if err == nil {
				capture.sdk("origin GET", resource+"/"+origin.Name, response.AFDOrigin)
			}
		}
	}
}
