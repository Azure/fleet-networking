# Local Service private origins

This controller is registered whenever AFD is enabled and leaves the source ClusterIP/headless Service unchanged.
For an eligible same-namespace core `Service` HTTPRoute backend, it owns one
selectorless IPv4 internal LoadBalancer Service and requests a CCM-managed PLS.
The source Service UID and selected service port determine the companion name.
`CompanionName(*corev1.Service, corev1.ServicePort)` exposes that deterministic
Service/PLS name for approval workflows.
Multiple selected ports are rejected and existing companions are withdrawn.

Only valid root `PathPrefix /` routes attached to currently accepted Fleet AFD
GatewayClasses and Gateways authorize provisioning. Namespace selectors and
listener attachment policies are evaluated through `gatewayresolver`.
Ready, nonterminating source EndpointSlice addresses are mirrored using the
selected Service port **name**, not its `targetPort`. Manual slices are supported;
stale Service-owned slices, IPv6, and Pod target references are not mirrored.
Withdrawal removes owned slices and deletes companions using UID/resource-version
preconditions. CCM finalizers are never removed by this controller.

## Integration

AFD startup registers `Reconciler.SetupWithManager` with verified discovery:

```go
discovery := &privateorigin.Discovery{
    LoadBalancerClient: lbClient,
    PrivateLinkServiceClient: plsClient,
    ResourceGroupName: nodeResourceGroup,
}
controller := &localservice.Reconciler{
    Client: manager.GetClient(),
    APIReader: manager.GetAPIReader(),
    Discovery: discovery,
}
```

The clients implement `List(context.Context, string)` returning
`[]*armnetwork.LoadBalancer` / `[]*armnetwork.PrivateLinkService` and an error.
These are the same interfaces used by member ServiceExport discovery.
Existing `sigs.k8s.io/cloud-provider-azure/pkg/azclient/loadbalancerclient` and
`privatelinkserviceclient` adapters satisfy these interfaces. Both expose
`New(subscriptionID string, credential azcore.TokenCredential, options *arm.ClientOptions)`
returning `(Interface, error)`; no additional SDK pager adapter or
`localservice.NewDiscovery` constructor is needed.
Use the hub cluster's node resource group, not the AFD profile resource group.

`ResolveLocalService(ctx, routeNamespace, HTTPBackendRef)` is read-only and
returns a `gatewaymodel.Backend` with kind `Service`, the original Service
identity, and a single verified `local` origin. It requires no ServiceExport.
Missing Services and invalid ports return invalid-reference resolution errors;
provisioning, endpoint synchronization, and Azure discovery delays return
`BackendUnavailable` with `ReferenceValid=true`. No Azure discovery is inferred
from user-written resource IDs or requested IP annotations.
Resolution checks source/mirror endpoint equality both before and after Azure
discovery, then rechecks source slice identities/resource versions to detect
draining during the list operations. Companion changes, source deletion, and
Gateway withdrawal during discovery cannot produce a usable Backend.

Gateway/HTTPRoute dependency mapping follows companion Service and mirror
EndpointSlice ownership back to the current source Service UID. This wakes the
original backend's consumers when CCM publishes an ILB IP or companion endpoints
change. The Gateway also retries valid pending local origins on its existing
approximately 30-second observation interval, because Azure PLS readiness may
change without another Kubernetes event. Invalid references do not enable this retry.
