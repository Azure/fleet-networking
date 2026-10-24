/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// BackendRequests maps validated backend inventory to stable AFD SDK requests.
// It is not an apply-ready graph: ownership, routing/domain and region/platform
// gates must pass before a provider may submit these requests.
type BackendRequests struct {
	ProfileName  string
	OriginGroups []OriginGroupRequest
}

// OriginGroupRequest names an origin group and its private member requests.
type OriginGroupRequest struct {
	Name       string
	Parameters armcdn.AFDOriginGroup
	Origins    []OriginRequest
}

// OriginRequest names an origin under its containing origin group.
type OriginRequest struct {
	Name       string
	Parameters armcdn.AFDOrigin
}

// BuildBackendRequests constructs private HTTP SDK bodies without Azure calls.
// All preflight failures return zero requests, never an applicable subset.
func BuildBackendRequests(model gatewaymodel.GlobalGateway) (BackendRequests, error) {
	plan, err := PlanBackends(model)
	if err != nil {
		return BackendRequests{}, err
	}
	if model.WAFPolicyID != "" {
		// Do not turn declared protection into an unprotected configuration.
		return BackendRequests{}, fmt.Errorf("WAF association is not implemented")
	}
	requests := BackendRequests{ProfileName: plan.ProfileName}
	for _, group := range plan.OriginGroups {
		request := OriginGroupRequest{
			Name: group.Name,
			Parameters: armcdn.AFDOriginGroup{Properties: &armcdn.AFDOriginGroupProperties{
				HealthProbeSettings: &armcdn.HealthProbeParameters{
					ProbePath: ptr.To(group.HealthProbe.Path), ProbeIntervalInSeconds: ptr.To(group.HealthProbe.IntervalSeconds),
					ProbeProtocol: ptr.To(armcdn.ProbeProtocolHTTP), ProbeRequestType: ptr.To(armcdn.HealthProbeRequestTypeGET),
				},
				SessionAffinityState: ptr.To(armcdn.EnabledStateDisabled),
			}},
		}
		for _, origin := range group.Origins {
			// Keep the SDK conversion locally checked, independently of model preflight.
			if origin.Weight < 1 || origin.Weight > 1000 {
				return BackendRequests{}, fmt.Errorf("origin %q weight must be in [1, 1000]", origin.Name)
			}
			request.Origins = append(request.Origins, OriginRequest{
				Name: origin.Name,
				Parameters: armcdn.AFDOrigin{Properties: &armcdn.AFDOriginProperties{
					HostName: ptr.To(origin.HostName), HTTPPort: ptr.To(origin.HTTPPort),
					Weight: ptr.To(int32(origin.Weight)), Priority: ptr.To(origin.Priority),
					EnabledState: ptr.To(armcdn.EnabledStateEnabled), EnforceCertificateNameCheck: ptr.To(origin.EnforceCertificateNameCheck),
					// Explicitly carry the approved empty override: preserve the incoming request Host.
					OriginHostHeader: ptr.To(origin.OriginHostHeader),
					SharedPrivateLinkResource: &armcdn.SharedPrivateLinkResourceProperties{
						// Custom PLS has no group ID. Approval status is observed, never requested.
						PrivateLink:         &armcdn.ResourceReference{ID: ptr.To(origin.PrivateLink.ResourceID)},
						PrivateLinkLocation: ptr.To(origin.PrivateLink.Location),
						RequestMessage:      ptr.To("Fleet AFD private origin for " + plan.ProfileName),
					},
				}},
			})
		}
		requests.OriginGroups = append(requests.OriginGroups, request)
	}
	return requests, nil
}
