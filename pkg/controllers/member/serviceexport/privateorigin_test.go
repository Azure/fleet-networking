/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	testFrontendID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/node-rg/providers/Microsoft.Network/loadBalancers/kubernetes-internal/frontendIPConfigurations/frontend"
	testPLSID      = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/node-rg/providers/Microsoft.Network/privateLinkServices/store-pls"
)

type fakeOriginLoadBalancers struct {
	items  []*armnetwork.LoadBalancer
	err    error
	groups []string
	ctx    context.Context
}

func (f *fakeOriginLoadBalancers) List(ctx context.Context, group string) ([]*armnetwork.LoadBalancer, error) {
	f.groups = append(f.groups, group)
	f.ctx = ctx
	return f.items, f.err
}

type fakeOriginPrivateLinkServices struct {
	items  []*armnetwork.PrivateLinkService
	err    error
	groups []string
	ctx    context.Context
}

func (f *fakeOriginPrivateLinkServices) List(ctx context.Context, group string) ([]*armnetwork.PrivateLinkService, error) {
	f.groups = append(f.groups, group)
	f.ctx = ctx
	return f.items, f.err
}

func privateOriginFixture() (*corev1.Service, *fakeOriginLoadBalancers, *fakeOriginPrivateLinkServices) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "store", Name: "api", UID: types.UID("source-uid"),
			Annotations: map[string]string{objectmeta.ServiceAnnotationAzureLoadBalancerInternal: internalLoadBalancerEnabled},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.10"}},
		}},
	}
	loadBalancers := &fakeOriginLoadBalancers{items: []*armnetwork.LoadBalancer{{
		Properties: &armnetwork.LoadBalancerPropertiesFormat{
			ProvisioningState: ptr.To(armnetwork.ProvisioningStateSucceeded),
			FrontendIPConfigurations: []*armnetwork.FrontendIPConfiguration{{
				ID: ptr.To(testFrontendID),
				Properties: &armnetwork.FrontendIPConfigurationPropertiesFormat{
					PrivateIPAddress:  ptr.To("10.0.0.10"),
					Subnet:            &armnetwork.Subnet{ID: ptr.To("/subnets/internal")},
					ProvisioningState: ptr.To(armnetwork.ProvisioningStateSucceeded),
				},
			}},
		},
	}}}
	privateLinks := &fakeOriginPrivateLinkServices{items: []*armnetwork.PrivateLinkService{{
		ID: ptr.To(testPLSID), Name: ptr.To("store-pls"), Location: ptr.To("eastus"),
		Properties: &armnetwork.PrivateLinkServiceProperties{
			ProvisioningState: ptr.To(armnetwork.ProvisioningStateSucceeded),
			LoadBalancerFrontendIPConfigurations: []*armnetwork.FrontendIPConfiguration{{
				ID: ptr.To(testFrontendID),
			}},
		},
	}}}
	return service, loadBalancers, privateLinks
}

func TestDiscoverPrivateLinkOrigin(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*corev1.Service, *fakeOriginLoadBalancers, *fakeOriginPrivateLinkServices)
		reason string
		ready  bool
		err    error
	}{
		{name: "ready internal origin", reason: "Ready", ready: true},
		{
			name: "public Service is not an origin",
			mutate: func(s *corev1.Service, _ *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				delete(s.Annotations, objectmeta.ServiceAnnotationAzureLoadBalancerInternal)
			},
			reason: "NotInternalLoadBalancer",
		},
		{
			name: "ClusterIP is not an origin",
			mutate: func(s *corev1.Service, _ *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				s.Spec.Type = corev1.ServiceTypeClusterIP
			},
			reason: "NotInternalLoadBalancer",
		},
		{
			name: "wait for Service ingress",
			mutate: func(s *corev1.Service, _ *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				s.Status.LoadBalancer.Ingress = nil
			},
			reason: "WaitingForLoadBalancer",
		},
		{
			name: "reject multiple ingress addresses",
			mutate: func(s *corev1.Service, _ *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				s.Status.LoadBalancer.Ingress = append(s.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{IP: "10.0.0.11"})
			},
			reason: "UnsupportedIngress",
		},
		{
			name: "do not treat hostname as IP",
			mutate: func(s *corev1.Service, _ *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				s.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{Hostname: "example.com"}}
			},
			reason: "UnsupportedIngress",
		},
		{
			name: "unrelated frontend is not selected",
			mutate: func(_ *corev1.Service, l *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				l.items[0].Properties.FrontendIPConfigurations[0].Properties.PrivateIPAddress = ptr.To("10.0.0.11")
			},
			reason: "LoadBalancerNotFound",
		},
		{
			name: "public frontend cannot be used",
			mutate: func(_ *corev1.Service, l *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				l.items[0].Properties.FrontendIPConfigurations[0].Properties.PublicIPAddress = &armnetwork.PublicIPAddress{}
			},
			reason: "LoadBalancerNotFound",
		},
		{
			name: "ambiguous frontend fails closed",
			mutate: func(_ *corev1.Service, l *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				l.items = append(l.items, l.items[0])
			},
			reason: "AmbiguousLoadBalancer",
		},
		{
			name: "wait for frontend provisioning",
			mutate: func(_ *corev1.Service, l *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				l.items[0].Properties.FrontendIPConfigurations[0].Properties.ProvisioningState = ptr.To(armnetwork.ProvisioningStateUpdating)
			},
			reason: "LoadBalancerNotReady",
		},
		{
			name: "missing PLS",
			mutate: func(_ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items = nil
			},
			reason: "PrivateLinkServiceNotFound",
		},
		{
			name: "same name with unrelated frontend is not enough",
			mutate: func(_ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items[0].Properties.LoadBalancerFrontendIPConfigurations[0].ID = ptr.To(testFrontendID + "-other")
			},
			reason: "PrivateLinkServiceNotFound",
		},
		{
			name: "ambiguous PLS fails closed",
			mutate: func(_ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items = append(p.items, p.items[0])
			},
			reason: "AmbiguousPrivateLinkService",
		},
		{
			name: "wait for PLS provisioning",
			mutate: func(_ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items[0].Properties.ProvisioningState = ptr.To(armnetwork.ProvisioningStateUpdating)
			},
			reason: "PrivateLinkServiceNotReady",
		},
		{
			name: "malformed PLS identity is not ready",
			mutate: func(_ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items[0].ID = ptr.To("/invalid")
			},
			reason: "InvalidPrivateLinkService",
		},
		{
			name: "missing location is not ready",
			mutate: func(_ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items[0].Location = nil
			},
			reason: "InvalidPrivateLinkService",
		},
		{
			name: "wrong Azure resource kind is not ready",
			mutate: func(_ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items[0].ID = ptr.To(strings.Replace(testPLSID, "privateLinkServices", "publicIPAddresses", 1))
			},
			reason: "InvalidPrivateLinkService",
		},
		{
			name: "nil Azure entries are ignored",
			mutate: func(_ *corev1.Service, l *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				l.items = append(l.items, nil, &armnetwork.LoadBalancer{})
				p.items = append(p.items, nil, &armnetwork.PrivateLinkService{})
			},
			reason: "Ready", ready: true,
		},
		{
			name: "Azure resource IDs are case insensitive",
			mutate: func(_ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items[0].Properties.LoadBalancerFrontendIPConfigurations[0].ID = ptr.To(strings.ToUpper(testFrontendID))
			},
			reason: "Ready", ready: true,
		},
	}
	discoveryError := errors.New("Azure unavailable")
	tests = append(tests, struct {
		name   string
		mutate func(*corev1.Service, *fakeOriginLoadBalancers, *fakeOriginPrivateLinkServices)
		reason string
		ready  bool
		err    error
	}{
		name: "Azure error remains retryable",
		mutate: func(_ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
			p.err = discoveryError
		},
		reason: azureDiscoveryFailedReason, err: discoveryError,
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, lbs, plss := privateOriginFixture()
			if tt.mutate != nil {
				tt.mutate(service, lbs, plss)
			}
			before := service.DeepCopy()
			r := &Reconciler{ResourceGroupName: "node-rg", AzureLoadBalancerClient: lbs, AzurePrivateLinkServiceClient: plss}
			got, err := r.discoverPrivateLinkOrigin(context.Background(), service)
			if !errors.Is(err, tt.err) {
				t.Fatalf("error = %v, want %v", err, tt.err)
			}
			if got == nil || got.Ready != tt.ready || got.Reason != tt.reason {
				t.Fatalf("origin = %#v, want ready=%v reason=%s", got, tt.ready, tt.reason)
			}
			if tt.ready && (got.ResourceID != testPLSID || got.Location != "eastus" || got.InternalIP != "10.0.0.10") {
				t.Errorf("unexpected ready origin: %#v", got)
			}
			if !reflect.DeepEqual(service, before) {
				t.Error("discovery mutated the Service")
			}
		})
	}
}

func TestDiscoverPrivateLinkOriginResourceGroups(t *testing.T) {
	service, lbs, plss := privateOriginFixture()
	service.Annotations[objectmeta.ServiceAnnotationLoadBalancerResourceGroup] = " custom-lb-rg "
	service.Annotations[objectmeta.ServiceAnnotationPLSResourceGroup] = " custom-pls-rg "
	service.Annotations[objectmeta.ServiceAnnotationPLSName] = "store-pls"
	plss.items[0].ID = ptr.To(strings.Replace(testPLSID, "node-rg", "custom-pls-rg", 1))
	r := &Reconciler{ResourceGroupName: "node-rg", AzureLoadBalancerClient: lbs, AzurePrivateLinkServiceClient: plss}
	origin, err := r.discoverPrivateLinkOrigin(context.Background(), service)
	if err != nil || !origin.Ready {
		t.Fatalf("origin = %#v, error = %v", origin, err)
	}
	if !reflect.DeepEqual(lbs.groups, []string{"custom-lb-rg"}) || !reflect.DeepEqual(plss.groups, []string{"custom-pls-rg"}) {
		t.Fatalf("wrong discovery groups: %v, %v", lbs.groups, plss.groups)
	}
	service.Annotations[objectmeta.ServiceAnnotationPLSName] = "unrelated-name"
	origin, err = r.discoverPrivateLinkOrigin(context.Background(), service)
	if err != nil || origin.Ready || origin.Reason != "PrivateLinkServiceNotFound" {
		t.Fatalf("named PLS mismatch did not fail closed: %#v, %v", origin, err)
	}
}

func TestDiscoverPrivateLinkOriginConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Reconciler)
	}{
		{name: "missing LB client", change: func(r *Reconciler) { r.AzureLoadBalancerClient = nil }},
		{name: "missing PLS client", change: func(r *Reconciler) { r.AzurePrivateLinkServiceClient = nil }},
		{name: "missing default resource group", change: func(r *Reconciler) { r.ResourceGroupName = " " }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, lbs, plss := privateOriginFixture()
			r := &Reconciler{ResourceGroupName: "node-rg", AzureLoadBalancerClient: lbs, AzurePrivateLinkServiceClient: plss}
			tt.change(r)
			origin, err := r.discoverPrivateLinkOrigin(context.Background(), service)
			if err == nil || origin.Ready || origin.Reason != azureDiscoveryFailedReason {
				t.Fatalf("incomplete discovery configuration did not fail explicitly: %#v, %v", origin, err)
			}
			if len(lbs.groups) != 0 || len(plss.groups) != 0 {
				t.Fatal("incomplete configuration made Azure calls")
			}
		})
	}
}

func TestDiscoverPrivateLinkOriginDeadline(t *testing.T) {
	service, lbs, plss := privateOriginFixture()
	r := &Reconciler{ResourceGroupName: "node-rg", AzureLoadBalancerClient: lbs, AzurePrivateLinkServiceClient: plss}
	if origin, err := r.discoverPrivateLinkOrigin(context.Background(), service); err != nil || !origin.Ready {
		t.Fatalf("discovery failed: %#v, %v", origin, err)
	}
	lbDeadline, lbOK := lbs.ctx.Deadline()
	plsDeadline, plsOK := plss.ctx.Deadline()
	if !lbOK || !plsOK || !lbDeadline.Equal(plsDeadline) {
		t.Fatal("Azure discovery calls must share one deadline")
	}
	if remaining := time.Until(lbDeadline); remaining <= 0 || remaining > 30*time.Second {
		t.Fatalf("discovery deadline has %v remaining, want (0, 30s]", remaining)
	}
}

func TestExportServicePrivateOrigin(t *testing.T) {
	for _, trafficManager := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(map[bool]string{false: "no-TM", true: "TM"}[trafficManager]+"/"+map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
				service, lbs, plss := privateOriginFixture()
				export := &fleetnetv1beta1.ServiceExport{ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: service.Name}}
				hub := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
				r := &Reconciler{
					MemberClient: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(export).Build(),
					HubClient:    hub, HubNamespace: "fleet-member-test", MemberClusterID: "member",
					EnablePrivateLinkOriginDiscovery: enabled,
					EnableTrafficManagerFeature:      trafficManager,
					ResourceGroupName:                "node-rg", AzureLoadBalancerClient: lbs, AzurePrivateLinkServiceClient: plss,
					Recorder: record.NewFakeRecorder(10),
				}
				result, err := r.exportService(context.Background(), export, service, time.Now(), 42)
				if err != nil {
					t.Fatal(err)
				}
				got := &fleetnetv1alpha1.InternalServiceExport{}
				key := client.ObjectKey{Namespace: r.HubNamespace, Name: formatInternalServiceExportName(export)}
				if err := hub.Get(context.Background(), key, got); err != nil {
					t.Fatal(err)
				}
				if enabled {
					if got.Spec.PrivateLinkOrigin == nil || !got.Spec.PrivateLinkOrigin.Ready || ptr.Deref(got.Spec.Weight, 0) != 42 {
						t.Fatalf("private metadata/weight not published: %#v", got.Spec)
					}
					if result.RequeueAfter < privateOriginRediscoveryInterval || result.RequeueAfter > privateOriginRediscoveryInterval*12/10 {
						t.Errorf("rediscovery delay = %v, want [%v, %v]", result.RequeueAfter, privateOriginRediscoveryInterval, privateOriginRediscoveryInterval*12/10)
					}
				} else if got.Spec.PrivateLinkOrigin != nil || len(lbs.groups) != 0 || len(plss.groups) != 0 || result != (ctrl.Result{}) {
					t.Fatalf("disabled discovery performed work: %#v, %#v", got.Spec, result)
				}
			})
		}
	}
}

func TestExportServicePrivateOriginTransitions(t *testing.T) {
	tests := []struct {
		name       string
		change     func(*Reconciler, *corev1.Service, *fakeOriginLoadBalancers, *fakeOriginPrivateLinkServices)
		wantReason string
		wantID     string
		wantErr    bool
	}{
		{
			name: "PLS deletion clears old ready identity",
			change: func(_ *Reconciler, _ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items = nil
			},
			wantReason: "PrivateLinkServiceNotFound",
		},
		{
			name: "PLS replacement publishes new identity",
			change: func(_ *Reconciler, _ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.items[0].ID = ptr.To(testPLSID + "-new")
			},
			wantReason: "Ready", wantID: testPLSID + "-new",
		},
		{
			name: "public Service clears private identity",
			change: func(_ *Reconciler, s *corev1.Service, _ *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				delete(s.Annotations, objectmeta.ServiceAnnotationAzureLoadBalancerInternal)
			},
			wantReason: "NotInternalLoadBalancer",
		},
		{
			name: "feature disable clears private identity",
			change: func(r *Reconciler, _ *corev1.Service, _ *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				r.EnablePrivateLinkOriginDiscovery = false
			},
		},
		{
			name: "authorization error publishes failure without losing export",
			change: func(_ *Reconciler, _ *corev1.Service, _ *fakeOriginLoadBalancers, p *fakeOriginPrivateLinkServices) {
				p.err = &azcore.ResponseError{StatusCode: 403}
			},
			wantReason: azureDiscoveryFailedReason, wantErr: true,
		},
		{
			name: "throttle publishes failure without losing export",
			change: func(_ *Reconciler, _ *corev1.Service, l *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				l.err = &azcore.ResponseError{StatusCode: 429}
			},
			wantReason: azureDiscoveryFailedReason, wantErr: true,
		},
		{
			name: "Service recreation removes old export first",
			change: func(_ *Reconciler, s *corev1.Service, _ *fakeOriginLoadBalancers, _ *fakeOriginPrivateLinkServices) {
				s.UID = types.UID("replacement-service")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, lbs, plss := privateOriginFixture()
			export := &fleetnetv1beta1.ServiceExport{ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: service.Name}}
			hub := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			r := &Reconciler{
				MemberClient: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(export).Build(),
				HubClient:    hub, HubNamespace: "fleet-member-test", MemberClusterID: "member",
				EnablePrivateLinkOriginDiscovery: true,
				ResourceGroupName:                "node-rg", AzureLoadBalancerClient: lbs, AzurePrivateLinkServiceClient: plss,
				Recorder: record.NewFakeRecorder(10),
			}
			if _, err := r.exportService(context.Background(), export, service, time.Now(), 42); err != nil {
				t.Fatal(err)
			}
			key := client.ObjectKey{Namespace: r.HubNamespace, Name: formatInternalServiceExportName(export)}
			// The fake client does not assign the API server's creation timestamp.
			existing := &fleetnetv1alpha1.InternalServiceExport{}
			if err := hub.Get(context.Background(), key, existing); err != nil {
				t.Fatal(err)
			}
			existing.CreationTimestamp = metav1.Now()
			if err := hub.Update(context.Background(), existing); err != nil {
				t.Fatal(err)
			}
			if err := r.MemberClient.Get(context.Background(), client.ObjectKeyFromObject(export), export); err != nil {
				t.Fatal(err)
			}
			tt.change(r, service, lbs, plss)
			result, err := r.exportService(context.Background(), export, service, time.Now(), 42)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want error=%v", err, tt.wantErr)
			}
			got := &fleetnetv1alpha1.InternalServiceExport{}
			getErr := hub.Get(context.Background(), key, got)
			if service.UID == types.UID("replacement-service") {
				if !apierrors.IsNotFound(getErr) || !result.Requeue {
					t.Fatalf("old Service identity was reused: %#v, %v", got, result)
				}
				return
			}
			if getErr != nil {
				t.Fatal(getErr)
			}
			if len(got.Spec.Ports) != 1 || got.Spec.ServiceReference.UID != service.UID {
				t.Fatalf("baseline export lost: %#v", got.Spec)
			}
			if tt.wantReason == "" {
				if got.Spec.PrivateLinkOrigin != nil {
					t.Fatalf("stale private metadata: %#v", got.Spec.PrivateLinkOrigin)
				}
			} else if origin := got.Spec.PrivateLinkOrigin; origin == nil || origin.Reason != tt.wantReason || origin.ResourceID != tt.wantID || origin.Ready != (tt.wantReason == "Ready") {
				t.Fatalf("wrong private discovery state: %#v", origin)
			}
		})
	}
}
