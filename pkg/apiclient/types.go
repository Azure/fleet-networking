package apiclient

import (
	"context"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"istio.io/istio/pkg/config/schema/kubeclient"
	"istio.io/istio/pkg/kube/kubetypes"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// agwv1alpha1 "github.com/kgateway-dev/kgateway/v2/api/v1alpha1/agentgateway"
// "github.com/kgateway-dev/kgateway/v2/api/v1alpha1/kgateway"
// "github.com/kgateway-dev/kgateway/v2/pkg/kgateway/wellknown"

// RegisterTypes registers all the types used by our API Client
func RegisterTypes() {
	gvrSE := fleetnetv1beta1.GroupVersion.WithResource("serviceexports")
	kubeclient.Register[*fleetnetv1beta1.ServiceExport](
		gvrSE,
		fleetnetv1beta1.GroupVersion.WithKind("ServiceExport"),
		func(c kubeclient.ClientGetter, namespace string, o v1.ListOptions) (runtime.Object, error) {
			return c.(Client).Networking().NetworkingV1beta1().ServiceExports(namespace).List(context.Background(), o)
		},
		func(c kubeclient.ClientGetter, namespace string, o v1.ListOptions) (watch.Interface, error) {
			return c.(Client).Networking().NetworkingV1beta1().ServiceExports(namespace).Watch(context.Background(), o)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*fleetnetv1beta1.ServiceExport] {
			return c.(Client).Networking().NetworkingV1beta1().ServiceExports(namespace)
		},
	)

	gvrISE := fleetnetv1alpha1.GroupVersion.WithResource("internalserviceexports")
	kubeclient.Register[*fleetnetv1alpha1.InternalServiceExport](
		gvrISE,
		fleetnetv1alpha1.GroupVersion.WithKind("InternalServiceExport"),
		func(c kubeclient.ClientGetter, namespace string, o v1.ListOptions) (runtime.Object, error) {
			return c.(Client).Networking().NetworkingV1alpha1().InternalServiceExports(namespace).List(context.Background(), o)
		},
		func(c kubeclient.ClientGetter, namespace string, o v1.ListOptions) (watch.Interface, error) {
			return c.(Client).Networking().NetworkingV1alpha1().InternalServiceExports(namespace).Watch(context.Background(), o)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*fleetnetv1alpha1.InternalServiceExport] {
			return c.(Client).Networking().NetworkingV1alpha1().InternalServiceExports(namespace)
		},
	)

	gvrMCLB := fleetnetv1alpha1.GroupVersion.WithResource("multiclusterloadbalancers")
	kubeclient.Register[*fleetnetv1alpha1.MultiClusterLoadBalancer](
		gvrMCLB,
		fleetnetv1alpha1.GroupVersion.WithKind("MultiClusterLoadBalancer"),
		func(c kubeclient.ClientGetter, namespace string, o v1.ListOptions) (runtime.Object, error) {
			return c.(Client).Networking().NetworkingV1alpha1().MultiClusterLoadBalancers(namespace).List(context.Background(), o)
		},
		func(c kubeclient.ClientGetter, namespace string, o v1.ListOptions) (watch.Interface, error) {
			return c.(Client).Networking().NetworkingV1alpha1().MultiClusterLoadBalancers(namespace).Watch(context.Background(), o)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*fleetnetv1alpha1.MultiClusterLoadBalancer] {
			return c.(Client).Networking().NetworkingV1alpha1().MultiClusterLoadBalancers(namespace)
		},
	)
}
