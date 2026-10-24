/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"fmt"
	"maps"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

const (
	endpointActivationTag     = "networking.fleet.azure.com.activation"
	endpointActivationPending = "pending"
	endpointActivationStarted = "started"
)

func bootstrapEndpoint(endpoint armcdn.AFDEndpoint) (bool, error) {
	var phase string
	found := false
	for key, value := range endpoint.Tags {
		if !strings.EqualFold(key, endpointActivationTag) {
			continue
		}
		if found || value == nil {
			return false, fmt.Errorf("owned AFD endpoint has ambiguous activation metadata")
		}
		found, phase = true, *value
	}
	switch phase {
	case "":
		if found {
			return false, fmt.Errorf("owned AFD endpoint has empty activation metadata")
		}
		return false, nil
	case endpointActivationPending:
		return true, nil
	case endpointActivationStarted:
		return false, nil
	default:
		return false, fmt.Errorf("owned AFD endpoint has unknown activation metadata %q", phase)
	}
}

func endpointReadyForStaging(endpoint armcdn.AFDEndpoint) (bool, error) {
	bootstrap, err := bootstrapEndpoint(endpoint)
	if err != nil {
		return false, err
	}
	ready, err := disabledEndpointConverged(endpoint.Properties)
	if err != nil || ready {
		return ready, err
	}
	properties := endpoint.Properties
	// A fresh empty endpoint cannot deploy before its children exist. Never
	// apply this exception after the atomic first-enable marker has been written.
	if !bootstrap || ptr.Deref(properties.EnabledState, "") != armcdn.EnabledStateDisabled ||
		ptr.Deref(properties.ProvisioningState, "") != armcdn.AfdProvisioningStateSucceeded ||
		ptr.Deref(properties.DeploymentStatus, "") != armcdn.DeploymentStatusNotStarted {
		return false, nil
	}
	_, err = observedEndpointHostName(properties)
	return err == nil, err
}

func endpointReadyForHTTP(endpoint armcdn.AFDEndpoint) (bool, error) {
	if ptr.Deref(endpoint.Properties.EnabledState, "") == armcdn.EnabledStateDisabled {
		return endpointReadyForStaging(endpoint)
	}
	bootstrap, err := bootstrapEndpoint(endpoint)
	if err != nil {
		return false, err
	}
	if bootstrap {
		return false, fmt.Errorf("owned AFD endpoint was enabled without recording activation")
	}
	return endpointConverged(endpoint.Properties, ptr.Deref(endpoint.Properties.EnabledState, ""))
}

func endpointActivationParameters(endpoint armcdn.AFDEndpoint) (armcdn.AFDEndpointUpdateParameters, error) {
	bootstrap, err := bootstrapEndpoint(endpoint)
	if err != nil {
		return armcdn.AFDEndpointUpdateParameters{}, err
	}
	parameters := armcdn.AFDEndpointUpdateParameters{Properties: &armcdn.AFDEndpointPropertiesUpdateParameters{
		EnabledState: ptr.To(armcdn.EnabledStateEnabled),
	}}
	if bootstrap {
		parameters.Tags = maps.Clone(endpoint.Tags)
		for key := range parameters.Tags {
			if strings.EqualFold(key, endpointActivationTag) {
				delete(parameters.Tags, key)
			}
		}
		parameters.Tags[endpointActivationTag] = ptr.To(endpointActivationStarted)
	}
	return parameters, nil
}

func deploymentReadyForBootstrap(deployment *armcdn.DeploymentStatus) bool {
	state := ptr.Deref(deployment, "")
	return state == armcdn.DeploymentStatusNotStarted || state == armcdn.DeploymentStatusSucceeded
}

func originApprovedAndProvisioned(properties *armcdn.AFDOriginProperties) bool {
	return properties != nil && properties.SharedPrivateLinkResource != nil &&
		ptr.Deref(properties.SharedPrivateLinkResource.Status, "") == armcdn.SharedPrivateLinkResourceStatusApproved &&
		ptr.Deref(properties.ProvisioningState, "") == armcdn.AfdProvisioningStateSucceeded &&
		deploymentReadyForBootstrap(properties.DeploymentStatus)
}
