# krt-based API Client Infrastructure

## Overview

This change introduces a Kubernetes client built on top of
[Istio's `krt` (Kubernetes Reconciliation Toolkit)](https://pkg.go.dev/istio.io/istio/pkg/kube/krt)
library, alongside fleet-networking's existing controller-runtime based
controllers. It is the foundational infrastructure used by the new
[`MultiClusterLoadBalancer` controller](../GlobalLoadBalancer/architecture.md),
and is intended to be reused by future controllers that benefit from krt's
declarative, index-and-join collection model instead of hand-written
`Reconcile` loops.

## Why krt?

controller-runtime's `Reconciler` pattern requires manually wiring caches,
indexers, and watches, and reasoning about a queue of individual object keys.
krt instead lets a controller declare derived **collections** as pure
functions over one or more watched/source collections (`krt.NewCollection`),
with automatic re-computation when any input changes, and `krt.Fetch` /
`krt.NewIndex` for joining across types (e.g. "all `InternalServiceExport`s
for a given service name"). This is a better fit for the
`MultiClusterLoadBalancer` controller, which must join
`MultiClusterLoadBalancer` objects with `InternalServiceExport` objects
across member clusters to compute Azure Global Load Balancer backends.

## New Packages

### `pkg/apiclient`

Defines the `apiclient.Client` interface and its constructor, `apiclient.New(restConfig)`:

```go
type Client interface {
    kube.Client                          // istio's generic Kubernetes client (Core, Istio, GatewayAPI, RunAndWait, ...)
    Core() kube.Client
    Networking() internalclientset.Interface // fleet-networking's generated typed clientset
}
```

* `New` builds an istio `kube.Client` (`kube.NewClientConfigForRestConfig` +
  `kube.NewClient`) for generic/built-in resources and Gateway API resources,
  and a fleet-networking `internalclientset.Interface` (see below) for our
  CRDs.
* It calls `RegisterTypes()` (see below) so krt/istio's generic client
  machinery knows how to List/Watch/Write fleet-networking's custom types,
  and `kube.EnableCrdWatcher(kubeClient)` to let the client watch for CRDs
  becoming available.

`pkg/apiclient/types.go` registers each fleet-networking CRD with istio's
`kubeclient.Register[T]` generic registry, providing List/Watch/write
functions backed by the generated clientset:

* `ServiceExport` (`networking.fleet.azure.com/v1beta1`)
* `InternalServiceExport` (`networking.fleet.azure.com/v1alpha1`)
* `MultiClusterLoadBalancer` (`networking.fleet.azure.com/v1alpha1`)

Once registered, `kclient.New[T](client)` and `krt.WrapClient(...)` can be
used to build a krt collection for any of these types exactly as if they were
built-in Kubernetes types, which is what the GLB controller does for
`MultiClusterLoadBalancer` and `InternalServiceExport`.

### `pkg/common/krtutil`

A small helper, `krtutil.NewKrtOptions(stop, debugger).ToOptions(name)`,
that centralizes the boilerplate `krt.CollectionOption`s (`krt.WithName`,
`krt.WithDebugging`, `krt.WithStop`) every collection needs, so controllers
don't repeat them per collection.

### `pkg/applyconfigurations`

Generated "apply configuration" types (in the style of
`k8s.io/client-go/applyconfigurations`) for fleet-networking's `v1alpha1` and
`v1beta1` API types (`ServiceExport`, `InternalServiceExport`,
`InternalServiceImport`, `MultiClusterLoadBalancer`, status sub-objects,
etc.). These enable Server-Side Apply (SSA) instead of get-modify-update
loops, letting controllers atomically apply just the fields they own (status
conditions, finalizers) via a dedicated field manager, without clobbering
fields owned by other actors.

### `pkg/generated/clientset/internalclientset`

A generated typed Kubernetes clientset (and `fake` variant for tests) for
fleet-networking's CRDs, including `Apply`/`ApplyStatus` methods that consume
the apply-configuration types above. This is the client returned by
`apiclient.Client.Networking()`, and is what both `pkg/apiclient/types.go`
and the GLB controller use to read/write `MultiClusterLoadBalancer` and
`InternalServiceExport` objects.

## Dependency Changes

* `go.mod`: `istio.io/istio` bumped to a newer pseudo-version to pick up the
  `pkg/kube`, `pkg/kube/krt`, and `pkg/kube/kclient` packages used here, with
  the usual wave of transitive dependency bumps that follow
  (`istio.io/api`, `istio.io/client-go`, `k8s.io/*` 0.33 → 0.34,
  `sigs.k8s.io/controller-runtime` 0.21 → 0.22, `sigs.k8s.io/gateway-api`
  1.3.0 → 1.4.0, etc.).
* `github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armdeploymentstacks`
  (new direct dependency) and `.../armdeployments` — used by the GLB
  controller to provision Azure resources declaratively (see the
  [controller architecture doc](../GlobalLoadBalancer/architecture.md)).

## How This Is Wired Up

In `cmd/hub-net-controller-manager/main.go`, the manager now constructs an
`apiclient.Client` via `apiclient.New(mgr.GetConfig())` alongside the normal
controller-runtime `ctrl.Manager`, and passes it into
`globalserviceexport.NewReconciler(...)`. The resulting `*Reconciler` is
registered with `mgr.Add(r)` so its `Start(ctx)` (which calls
`client.RunAndWait(ctx.Done())` to run the krt-backed informers) participates
in the manager's leader-election and shutdown lifecycle like any other
`Runnable`, even though it does not implement the controller-runtime
`Reconciler` interface.

## Known Limitations / TODOs in this WIP

* Only `ServiceExport`, `InternalServiceExport`, and
  `MultiClusterLoadBalancer` are currently registered in `RegisterTypes()`;
  additional types will need registration before they can be used in krt
  collections.
* This infrastructure currently coexists with, but does not replace, the
  existing controller-runtime based controllers in `pkg/controllers/hub` —
  several of those (`endpointsliceexport`, `internalserviceexport`,
  `internalserviceimport`, `serviceimport`, `membercluster`) are currently
  commented out in `main.go` while this is under active development.
