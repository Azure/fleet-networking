// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package controllers

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-02-01/network"
	"github.com/Azure/go-autorest/autorest/to"
	networkingv1alpha1 "github.com/Azure/multi-cluster-networking/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// FrontendIPConfigIDTemplate is the template of the frontend IP configuration
	FrontendIPConfigIDTemplate = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/loadBalancers/%s/frontendIPConfigurations/%s"
	// BackendPoolIDTemplate is the template of the backend pool
	BackendPoolIDTemplate = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/loadBalancers/%s/backendAddressPools/%s"
	// LoadBalancerProbeIDTemplate is the template of the load balancer probe
	LoadBalancerProbeIDTemplate = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/loadBalancers/%s/probes/%s"
)

// RegionalIPConfig defines the metadata for regional LB config.
type RegionalIPConfig struct {
	IP       string
	ConfigID string
}

func (r *GlobalServiceReconciler) getGLB() (*network.LoadBalancer, error) {
	lb, rerr := r.LoadBalancerClient.Get(
		context.Background(),
		r.AzureConfig.GlobalLoadBalancerResourceGroup,
		r.AzureConfig.GlobalLoadBalancerName,
		"")
	if rerr != nil {
		if rerr.HTTPStatusCode == http.StatusNotFound {
			return nil, nil
		}

		return nil, rerr.Error()
	}

	return &lb, nil
}

func (r *GlobalServiceReconciler) reconcileGLB(globalService *networkingv1alpha1.GlobalService, wantLB bool) error {
	serviceName := fmt.Sprintf("%s-%s", globalService.Namespace, globalService.Name)
	namespacedName := types.NamespacedName{Namespace: globalService.Namespace, Name: globalService.Name}
	log := r.Log.WithValues("globalservice", namespacedName.String())

	glb, err := r.getGLB()
	if err != nil {
		return err
	}

	if !wantLB && glb == nil {
		log.Info("GlobalLoadBalancer has already been deleted")
		return nil
	}

	if glb == nil {
		glb = &network.LoadBalancer{
			Name:     to.StringPtr(r.AzureConfig.GlobalLoadBalancerName),
			Location: to.StringPtr(r.AzureConfig.GlobalVIPLocation),
			Sku: &network.LoadBalancerSku{
				Name: network.LoadBalancerSkuNameStandard,
				Tier: network.LoadBalancerSkuTierGlobal,
			},
			LoadBalancerPropertiesFormat: &network.LoadBalancerPropertiesFormat{},
		}
	}

	dirtyGLB := false
	lbBackendPoolID := fmt.Sprintf(BackendPoolIDTemplate,
		r.AzureConfig.SubscriptionID,
		r.AzureConfig.GlobalLoadBalancerResourceGroup,
		r.AzureConfig.GlobalLoadBalancerName,
		serviceName)
	lbFrontendConfigID := fmt.Sprintf(FrontendIPConfigIDTemplate,
		r.AzureConfig.SubscriptionID,
		r.AzureConfig.GlobalLoadBalancerResourceGroup,
		r.AzureConfig.GlobalLoadBalancerName, serviceName)

	log.Info("Reconciling GLB backend address pools")
	changed, newBackendAddressPool, err := r.reconcileGLBBackendPools(glb, globalService, lbBackendPoolID, wantLB)
	if err != nil {
		log.Error(err, "uname to reconcile GLB backend pools")
		return err
	}
	if changed {
		dirtyGLB = true
	}

	log.Info("Reconciling GLB frontend IP configurations")
	changed, vipToDelete, err := r.reconcileGLBFrontendIPConfigs(glb, globalService, lbFrontendConfigID, wantLB)
	if err != nil {
		log.Error(err, "uname to reconcile GLB frontend IP configurations")
		return err
	}
	if changed {
		dirtyGLB = true
	}

	log.Info("Reconciling GLB load balancer rules")
	expectedProbes, expectedRules, err := r.getExpectedGLBRulesProbes(glb, globalService, lbFrontendConfigID, lbBackendPoolID, wantLB)
	if err != nil {
		log.Error(err, "uname to get expected GLB rules and probes")
		return err
	}

	changed, err = r.reconcileGLBRules(glb, globalService, expectedRules, wantLB)
	if err != nil {
		log.Error(err, "uname to reconcile GLB rules")
		return err
	}
	if changed {
		dirtyGLB = true
	}

	log.Info("Reconciling GLB probes")
	changed, err = r.reconcileGLBProbes(glb, globalService, expectedProbes, wantLB)
	if err != nil {
		log.Error(err, "uname to reconcile GLB probes")
		return err
	}
	if changed {
		dirtyGLB = true
	}

	if dirtyGLB {
		if glb.FrontendIPConfigurations == nil || len(*glb.FrontendIPConfigurations) == 0 {
			// Delete the GLB for the last deleting rule.
			log.Info("Deleting the GLB since there is no frontend IP configurations")
			if rerr := r.LoadBalancerClient.Delete(context.Background(), r.AzureConfig.GlobalLoadBalancerResourceGroup, r.AzureConfig.GlobalLoadBalancerName); rerr != nil {
				log.Error(rerr.Error(), "unable to delete global load balancer")
				return rerr.Error()
			}
		} else {
			// Update the GLB on rule/probe changes.
			if err := r.updateGLB(glb, globalService, newBackendAddressPool, serviceName); err != nil {
				return err
			}
		}
	}

	// Delete the global VIP that is not referenced anymore.
	if vipToDelete != "" {
		log.Info("Deleting the global VIP that is not referenced anymore")
		vipName := getLastSegment(vipToDelete, "/")
		if err := r.deleteGlobalVip(globalService, vipName); err != nil {
			log.Error(err, "unable to delete global VIP")
			return err
		}
	}

	return nil
}

// updateGLB updates rules/probes/backendPools for the GLB.
func (r *GlobalServiceReconciler) updateGLB(glb *network.LoadBalancer, globalService *networkingv1alpha1.GlobalService, newBackendAddressPool *network.BackendAddressPool, serviceName string) error {
	namespacedName := types.NamespacedName{Namespace: globalService.Namespace, Name: globalService.Name}
	log := r.Log.WithValues("globalservice", namespacedName.String())

	log.Info("Updating GLB")
	if rerr := r.LoadBalancerClient.CreateOrUpdate(context.Background(),
		r.AzureConfig.GlobalLoadBalancerResourceGroup,
		r.AzureConfig.GlobalLoadBalancerName,
		*glb,
		""); rerr != nil {
		log.Error(rerr.Error(), "unable to update global load balancer")
		return rerr.Error()
	}

	if newBackendAddressPool != nil {
		log.Info("Updating GLB backend pool")
		if rerr := r.LoadBalancerClient.CreateOrUpdateBackendPools(
			context.Background(),
			r.AzureConfig.GlobalLoadBalancerResourceGroup,
			r.AzureConfig.GlobalLoadBalancerName,
			to.String(newBackendAddressPool.Name),
			*newBackendAddressPool,
			""); rerr != nil {
			log.Error(rerr.Error(), "unable to update global load balancer backend address pool")
			return rerr.Error()
		}
	}

	// Get and update status.VIP
	pip, rerr := r.PublicIPClient.Get(context.Background(),
		r.AzureConfig.GlobalLoadBalancerResourceGroup,
		serviceName,
		"")
	if rerr != nil {
		log.Error(rerr.Error(), "unable to fetch VIP")
		return rerr.Error()
	}
	vip := to.String(pip.IPAddress)
	if err := r.reconcileGlobalVIP(globalService, vip); err != nil {
		log.Error(err, "unable to reconcile global VIP")
		return err
	}

	return nil
}

// reconcileGlobalVIP updates service annotations for all member clusters and then updates the VIP for globalService.
func (r *GlobalServiceReconciler) reconcileGlobalVIP(globalService *networkingv1alpha1.GlobalService, vip string) error {
	r.Log.Info("reconciling global VIP")
	ctx := context.Background()

	// Update the annotation for the services in member clusters
	for _, endpoint := range globalService.Status.Endpoints {
		clusterManager := r.AKSClusterReconciler.GetClusterManager(endpoint.Cluster)
		if clusterManager != nil {
			var service corev1.Service
			client := clusterManager.GetClient()
			serviceName := types.NamespacedName{Namespace: globalService.Namespace, Name: strings.Split(endpoint.Service, "/")[1]}
			r.Log.Info("getting service", "service", serviceName.String(), "cluster", endpoint.Cluster)
			if err := client.Get(ctx, serviceName, &service); err != nil {
				if apierrors.IsNotFound(err) {
					r.Log.Info("service not found", "service", serviceName.String(), "cluster", endpoint.Cluster)
					continue
				}

				r.Log.Error(err, "unable to fetch Service", "service", serviceName.String())
				return err
			}

			if service.Annotations["service.beta.kubernetes.io/azure-additional-public-ips"] != vip {
				r.Log.Info("updating service annotation", "service", serviceName.String(), "cluster", endpoint.Cluster)

				if service.Annotations == nil {
					service.Annotations = make(map[string]string)
				}
				service.Annotations["service.beta.kubernetes.io/azure-additional-public-ips"] = vip
				if err := client.Update(ctx, &service); err != nil {
					if apierrors.IsNotFound(err) {
						r.Log.Info("service not found", "service", serviceName.String(), "cluster", endpoint.Cluster)
						continue
					}
					r.Log.Error(err, "unable to update Service", "service", serviceName.String())
					return err
				}
			}
		}
	}

	// Update the VIP for globalService.
	if globalService.Status.VIP != vip {
		globalService.Status.VIP = vip
		globalService.Status.State = "ACTIVE"
		r.Log.Info("updating globalservice's VIP")
		if err := r.Status().Update(context.Background(), globalService); err != nil {
			r.Log.Error(err, "unable to update GlobalService VIP")
			return err
		}
	}

	return nil
}

func (r *GlobalServiceReconciler) deleteGlobalVip(globalService *networkingv1alpha1.GlobalService, vipName string) error {
	r.Log.Info("deleting global VIP")
	ctx := context.Background()

	// Remove the annotation for the services in member clusters
	for _, endpoint := range globalService.Status.Endpoints {
		clusterManager := r.AKSClusterReconciler.GetClusterManager(endpoint.Cluster)
		if clusterManager != nil {
			var service corev1.Service
			client := clusterManager.GetClient()
			serviceName := types.NamespacedName{Namespace: globalService.Namespace, Name: strings.Split(endpoint.Service, "/")[1]}
			r.Log.Info("getting service", "service", serviceName.String(), "cluster", endpoint.Cluster)
			if err := client.Get(ctx, serviceName, &service); err != nil {
				if apierrors.IsNotFound(err) {
					r.Log.Info("service not found", "service", serviceName.String(), "cluster", endpoint.Cluster)
					continue
				}

				r.Log.Error(err, "unable to fetch Service", "service", serviceName.String())
				return err
			}

			if service.Annotations["service.beta.kubernetes.io/azure-additional-public-ips"] != "" {
				r.Log.Info("cleaning up service annotation", "service", serviceName.String(), "cluster", endpoint.Cluster)
				delete(service.Annotations, "service.beta.kubernetes.io/azure-additional-public-ips")
				if err := client.Update(ctx, &service); err != nil {
					if apierrors.IsNotFound(err) {
						r.Log.Info("skipping annotation updates since service not found", "service", serviceName.String(), "cluster", endpoint.Cluster)
						continue
					}
					r.Log.Error(err, "unable to clean up Service", "service", serviceName.String())
					return err
				}
			}
		}
	}

	if rerr := r.PublicIPClient.Delete(
		context.Background(),
		r.AzureConfig.GlobalLoadBalancerResourceGroup,
		vipName); rerr != nil {
		return rerr.Error()
	}

	return nil
}

func (r *GlobalServiceReconciler) reconcileGLBProbes(glb *network.LoadBalancer, globalService *networkingv1alpha1.GlobalService, expectedProbes []network.Probe, wantLB bool) (bool, error) {
	dirtyProbes := false
	var updatedProbes []network.Probe
	if glb.Probes != nil {
		updatedProbes = *glb.Probes
	}

	// remove unwanted probes
	for i := len(updatedProbes) - 1; i >= 0; i-- {
		existingProbe := updatedProbes[i]
		if r.serviceOwnsRule(globalService, *existingProbe.Name) {
			keepProbe := false
			if findProbe(expectedProbes, existingProbe) {
				keepProbe = true
			}
			if !keepProbe {
				updatedProbes = append(updatedProbes[:i], updatedProbes[i+1:]...)
				dirtyProbes = true
			}
		}
	}

	// add missing, wanted probes
	for _, expectedProbe := range expectedProbes {
		foundProbe := false
		if findProbe(updatedProbes, expectedProbe) {
			foundProbe = true
		}
		if !foundProbe {
			updatedProbes = append(updatedProbes, expectedProbe)
			dirtyProbes = true
		}
	}
	if dirtyProbes {
		glb.Probes = &updatedProbes
	}
	return dirtyProbes, nil
}

func findProbe(probes []network.Probe, probe network.Probe) bool {
	for _, existingProbe := range probes {
		if strings.EqualFold(to.String(existingProbe.Name), to.String(probe.Name)) && to.Int32(existingProbe.Port) == to.Int32(probe.Port) {
			return true
		}
	}
	return false
}

func (r *GlobalServiceReconciler) reconcileGLBRules(glb *network.LoadBalancer, globalService *networkingv1alpha1.GlobalService, expectedRules []network.LoadBalancingRule, wantLB bool) (bool, error) {
	var updatedRules []network.LoadBalancingRule
	dirtyRules := false
	if glb.LoadBalancingRules != nil {
		updatedRules = *glb.LoadBalancingRules
	}

	// update rules: remove unwanted
	for i := len(updatedRules) - 1; i >= 0; i-- {
		existingRule := updatedRules[i]
		if r.serviceOwnsRule(globalService, *existingRule.Name) {
			keepRule := false
			if findRule(expectedRules, existingRule, wantLB) {
				keepRule = true
			}
			if !keepRule {
				updatedRules = append(updatedRules[:i], updatedRules[i+1:]...)
				dirtyRules = true
			}
		}
	}

	// update rules: add needed
	for _, expectedRule := range expectedRules {
		foundRule := false
		if findRule(updatedRules, expectedRule, wantLB) {
			foundRule = true
		}
		if !foundRule {
			updatedRules = append(updatedRules, expectedRule)
			dirtyRules = true
		}
	}
	if dirtyRules {
		glb.LoadBalancingRules = &updatedRules
	}

	return dirtyRules, nil
}

func (r *GlobalServiceReconciler) serviceOwnsRule(globalService *networkingv1alpha1.GlobalService, rule string) bool {
	serviceName := fmt.Sprintf("%s-%s", globalService.Namespace, globalService.Name)
	return strings.HasPrefix(strings.ToUpper(rule), strings.ToUpper(serviceName))
}

func findRule(rules []network.LoadBalancingRule, rule network.LoadBalancingRule, wantLB bool) bool {
	for _, existingRule := range rules {
		if strings.EqualFold(to.String(existingRule.Name), to.String(rule.Name)) &&
			equalLoadBalancingRulePropertiesFormat(existingRule.LoadBalancingRulePropertiesFormat, rule.LoadBalancingRulePropertiesFormat) {
			return true
		}
	}
	return false
}

func equalLoadBalancingRulePropertiesFormat(s *network.LoadBalancingRulePropertiesFormat, t *network.LoadBalancingRulePropertiesFormat) bool {
	if s == nil || t == nil {
		return false
	}

	properties := reflect.DeepEqual(s.Protocol, t.Protocol) &&
		reflect.DeepEqual(s.FrontendIPConfiguration, t.FrontendIPConfiguration) &&
		reflect.DeepEqual(s.BackendAddressPool, t.BackendAddressPool) &&
		reflect.DeepEqual(s.LoadDistribution, t.LoadDistribution) &&
		reflect.DeepEqual(s.FrontendPort, t.FrontendPort) &&
		reflect.DeepEqual(s.BackendPort, t.BackendPort) &&
		reflect.DeepEqual(s.EnableFloatingIP, t.EnableFloatingIP) &&
		reflect.DeepEqual(to.Bool(s.EnableTCPReset), to.Bool(t.EnableTCPReset)) &&
		reflect.DeepEqual(to.Bool(s.DisableOutboundSnat), to.Bool(t.DisableOutboundSnat))

	return properties
}

func (r *GlobalServiceReconciler) getExpectedGLBRulesProbes(glb *network.LoadBalancer, globalService *networkingv1alpha1.GlobalService, lbFrontendConfigID string, lbBackendPoolID string, wantLB bool) ([]network.Probe, []network.LoadBalancingRule, error) {
	serviceName := getLastSegment(lbFrontendConfigID, "/")
	var ports []networkingv1alpha1.GlobalServicePort
	if wantLB {
		ports = globalService.Spec.Ports
	} else {
		ports = []networkingv1alpha1.GlobalServicePort{}
	}

	var expectedProbes []network.Probe
	var expectedRules []network.LoadBalancingRule
	for i := range ports {
		port := ports[i]

		lbRuleName := fmt.Sprintf("%s-%s-%d", serviceName, port.Protocol, port.Port)
		expectedProbes = append(expectedProbes, network.Probe{
			Name: &lbRuleName,
			ProbePropertiesFormat: &network.ProbePropertiesFormat{
				Protocol:          network.ProbeProtocol(port.Protocol),
				Port:              to.Int32Ptr(int32(port.Port)),
				IntervalInSeconds: to.Int32Ptr(5),
				NumberOfProbes:    to.Int32Ptr(2),
			},
		})

		expectedRules = append(expectedRules, network.LoadBalancingRule{
			Name: &lbRuleName,
			LoadBalancingRulePropertiesFormat: &network.LoadBalancingRulePropertiesFormat{
				Protocol: network.TransportProtocol(port.Protocol),
				FrontendIPConfiguration: &network.SubResource{
					ID: to.StringPtr(lbFrontendConfigID),
				},
				BackendAddressPool: &network.SubResource{
					ID: to.StringPtr(lbBackendPoolID),
				},
				LoadDistribution:    network.LoadDistributionDefault,
				FrontendPort:        to.Int32Ptr(int32(port.Port)),
				BackendPort:         to.Int32Ptr(int32(port.Port)),
				EnableTCPReset:      to.BoolPtr(true),
				DisableOutboundSnat: to.BoolPtr(false),
				EnableFloatingIP:    to.BoolPtr(true),
				Probe: &network.SubResource{
					ID: to.StringPtr(fmt.Sprintf(LoadBalancerProbeIDTemplate,
						r.AzureConfig.SubscriptionID,
						r.AzureConfig.GlobalLoadBalancerResourceGroup,
						r.AzureConfig.GlobalLoadBalancerName,
						lbRuleName)),
				},
			},
		})
	}

	return expectedProbes, expectedRules, nil
}

func (r *GlobalServiceReconciler) reconcileGLBFrontendIPConfigs(glb *network.LoadBalancer, globalService *networkingv1alpha1.GlobalService, lbFrontendConfigID string, wantLB bool) (bool, string, error) {
	serviceName := getLastSegment(lbFrontendConfigID, "/")
	var foundConfig int = -1
	var newConfigs []network.FrontendIPConfiguration
	if glb.FrontendIPConfigurations != nil {
		newConfigs = *glb.FrontendIPConfigurations
	}

	for i := range newConfigs {
		config := newConfigs[i]
		if strings.EqualFold(to.String(config.ID), lbFrontendConfigID) {
			foundConfig = i
			break
		}
	}

	if !wantLB {
		if foundConfig != -1 {
			configToDelete := newConfigs[foundConfig]
			newConfigs = append(newConfigs[:foundConfig], newConfigs[foundConfig+1:]...)
			glb.FrontendIPConfigurations = &newConfigs
			return true, to.String(configToDelete.PublicIPAddress.ID), nil
		}

		return false, "", nil
	}

	if foundConfig != -1 {
		return false, "", nil
	}

	pip, err := r.ensureGlobalPIP(serviceName)
	if err != nil {
		return false, "", err
	}

	newConfigs = append(newConfigs, network.FrontendIPConfiguration{
		Name: to.StringPtr(serviceName),
		ID:   to.StringPtr(lbFrontendConfigID),
		FrontendIPConfigurationPropertiesFormat: &network.FrontendIPConfigurationPropertiesFormat{
			PublicIPAddress: &network.PublicIPAddress{
				ID: pip.ID,
			},
		},
	})
	glb.FrontendIPConfigurations = &newConfigs
	return true, "", nil
}

func (r *GlobalServiceReconciler) ensureGlobalPIP(pipName string) (*network.PublicIPAddress, error) {
	pip, rerr := r.PublicIPClient.Get(
		context.Background(),
		r.AzureConfig.GlobalLoadBalancerResourceGroup,
		pipName, "")
	if rerr == nil {
		return &pip, nil
	}

	if rerr.HTTPStatusCode == http.StatusNotFound {
		err := r.PublicIPClient.CreateOrUpdate(
			context.Background(),
			r.AzureConfig.GlobalLoadBalancerResourceGroup,
			pipName,
			network.PublicIPAddress{
				Name:     to.StringPtr(pipName),
				Location: to.StringPtr(r.AzureConfig.GlobalVIPLocation),
				Sku: &network.PublicIPAddressSku{
					Name: network.PublicIPAddressSkuNameStandard,
					Tier: network.PublicIPAddressSkuTierGlobal,
				},
				PublicIPAddressPropertiesFormat: &network.PublicIPAddressPropertiesFormat{
					PublicIPAllocationMethod: network.IPAllocationMethodStatic,
				},
			})
		if err != nil {
			return nil, err.Error()
		}

		pip, rerr = r.PublicIPClient.Get(
			context.Background(),
			r.AzureConfig.GlobalLoadBalancerResourceGroup,
			pipName,
			"")
		if rerr != nil {
			return nil, rerr.Error()
		}

		return &pip, nil
	}

	return nil, rerr.Error()
}

func (r *GlobalServiceReconciler) reconcileGLBBackendPools(glb *network.LoadBalancer, globalService *networkingv1alpha1.GlobalService, lbBackendPoolID string, wantLB bool) (bool, *network.BackendAddressPool, error) {
	var newBackendPools []network.BackendAddressPool
	var foundBackendPool int = -1
	if glb.BackendAddressPools != nil {
		newBackendPools = *glb.BackendAddressPools
	}

	for i := range newBackendPools {
		backendPool := newBackendPools[i]
		if strings.EqualFold(to.String(backendPool.ID), lbBackendPoolID) {
			foundBackendPool = i
			break
		}
	}

	if !wantLB {
		if foundBackendPool != -1 {
			newBackendPools = append(newBackendPools[:foundBackendPool], newBackendPools[foundBackendPool+1:]...)
			glb.BackendAddressPools = &newBackendPools
			return true, nil, nil
		}
		return false, nil, nil
	}

	// Query regional regional SLB configurations.
	regionalSLBConfigurations, err := r.getRegionalSLBConfigurations(globalService)
	if err != nil {
		return false, nil, err
	}

	// Compose GLB backendAddressPool and loadBalancerBackendAddresses.
	serviceName := getLastSegment(lbBackendPoolID, "/")
	newBackendAddressPool := &network.BackendAddressPool{
		Name:                               to.StringPtr(serviceName),
		BackendAddressPoolPropertiesFormat: &network.BackendAddressPoolPropertiesFormat{},
	}
	var newLoadBalancerBackendAddresses []network.LoadBalancerBackendAddress
	for i := range regionalSLBConfigurations {
		rc := regionalSLBConfigurations[i]
		newLoadBalancerBackendAddresses = append(newLoadBalancerBackendAddresses, network.LoadBalancerBackendAddress{
			Name: to.StringPtr(fmt.Sprintf("backend%d", i)),
			LoadBalancerBackendAddressPropertiesFormat: &network.LoadBalancerBackendAddressPropertiesFormat{
				LoadBalancerFrontendIPConfiguration: &network.SubResource{
					ID: to.StringPtr(rc.ConfigID),
				},
				IPAddress: to.StringPtr(rc.IP),
			},
		})
	}

	if foundBackendPool != -1 {
		oldBackendAddressPool := &newBackendPools[foundBackendPool]
		if oldBackendAddressPool.LoadBalancerBackendAddresses != nil {
			oldLoadBalancerBackendAddresses := *oldBackendAddressPool.LoadBalancerBackendAddresses
			if len(oldLoadBalancerBackendAddresses) == len(newLoadBalancerBackendAddresses) {
				return false, nil, nil
			}
		}
	}

	newBackendAddressPool.LoadBalancerBackendAddresses = &newLoadBalancerBackendAddresses
	if foundBackendPool == -1 {
		newBackendPools = append(newBackendPools, *newBackendAddressPool)
	} else {
		newBackendPools[foundBackendPool] = *newBackendAddressPool
	}
	glb.BackendAddressPools = &newBackendPools

	return true, newBackendAddressPool, nil
}

func (r *GlobalServiceReconciler) getRegionalSLBConfigurations(globalService *networkingv1alpha1.GlobalService) ([]RegionalIPConfig, error) {
	if len(globalService.Status.Endpoints) == 0 {
		return nil, nil
	}

	regionalSLBConfigurations := make([]RegionalIPConfig, len(globalService.Status.Endpoints))
	for i, ep := range globalService.Status.Endpoints {
		// pipList, rerr := r.PublicIPClient.List(context.Background(), ep.ResourceGroup)
		pipList, rerr := r.PublicIPClient.ListAll(context.Background())
		if rerr != nil {
			return nil, rerr.Error()
		}

		found := false
		for _, pip := range pipList {
			if to.String(pip.IPAddress) == ep.IP && pip.IPConfiguration != nil {
				regionalSLBConfigurations[i] = RegionalIPConfig{
					IP:       ep.IP,
					ConfigID: to.String(pip.IPConfiguration.ID),
				}

				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unable to found public IP %s in subscription %s", ep.IP, r.AzureConfig.SubscriptionID)
		}
	}

	return regionalSLBConfigurations, nil
}

func getLastSegment(ID, separator string) string {
	parts := strings.Split(ID, separator)
	name := parts[len(parts)-1]
	if len(name) == 0 {
		return ""
	}

	return name
}
