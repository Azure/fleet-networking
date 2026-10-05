# GlobalLoadBalancer Controller Architecture

This document describes the internal design of the controller that backs the
`MultiClusterLoadBalancer` (`mclb`) CRD. For a user-facing overview and
walkthrough, see [README.md](./README.md).

## Package

`pkg/controllers/hub/globalserviceexport`

Unlike fleet-networking's other hub controllers, this one is **not** a
controller-runtime `Reconciler`. It is built on
[Istio's `krt` library](../KrtApiClient/README.md) and is registered with the
controller-runtime manager as a plain `manager.Runnable` via `mgr.Add(r)`, so
it still benefits from leader election and graceful shutdown.

## Inputs

Two krt collections are built from the [`apiclient.Client`](../KrtApiClient/README.md):

| Collection | Source type | Purpose |
|---|---|---|
| `mclbs` | `v1alpha1.MultiClusterLoadBalancer` | The user-created mclb resources to reconcile. |
| `ises` | `v1alpha1.InternalServiceExport` | Per-member-cluster records of an exported service's public IP, written by the (existing) service-export pipeline. |

An index, `iseIndex`, is built over `ises`, keyed by
`(ServiceReference.Namespace, ServiceReference.Name)`, so that for a given
`mclb` (which shares its name/namespace with the originating `ServiceExport`)
all matching `InternalServiceExport`s across member clusters can be fetched
in one `krt.Fetch` call.

## Reconciliation Flow (`outputs` collection)

For every `MultiClusterLoadBalancer`, `krt.NewCollection(mclbs, ...)` computes
a derived `output` record:

1. **Resolve target gateway / resource group.**
   - `targetGateway` is read from the `targetGateway` annotation; if empty,
     the final global IP is annotated onto the member `Service` instead of a
     Gateway.
   - The Azure resource group is read from the
     `service.beta.kubernetes.io/azure-load-balancer-resource-group`
     annotation, falling back to the controller's configured default
     resource group.

2. **Deletion.** If `mclb.DeletionTimestamp` is set:
   - The corresponding Azure **Deployment Stack** is deleted
     (`armdeploymentstacks` `BeginDeleteAtResourceGroup` +
     `PollUntilDone`; a `404` is treated as already-deleted).
   - The `mclb` finalizer is removed via a JSON patch against the generated
     clientset.
   - No further processing occurs for this object.

3. **Resolve backends.** The matching `InternalServiceExport`s are fetched
   via `iseIndex`. For each one, the referenced Azure Public IP resource
   (`ise.Spec.PublicIPResourceID`) is looked up with
   `armresources.Client.GetByID`, and its load-balancer frontend IP
   configuration ID is collected as a backend address.
   - If a Public IP can't be found, the mclb's `Valid` condition is set to
     `False` (reason `PublicIPNotFound`) and processing stops for this
     object (`kctx.DiscardResult()`).

4. **Mark valid / ensure finalizer.** On success, `Valid=True` is applied,
   and the `mclb` finalizer is applied (asynchronously, via Server-Side
   Apply) if not already present.

5. **Deploy the Global Load Balancer.** `writeDeployment` submits an ARM
   **Deployment Stack** (see `template.go`) parameterized with the mclb's
   name and the collected backend frontend-IP-config IDs. The template
   provisions:
   - A `Standard`/`Global`-tier **Public IP Address**.
   - A `Standard`/`Global`-tier **Load Balancer** with a single rule
     (`tcp-80-k8s2`, port 80, floating IP enabled) whose backend pool
     (`kubernetes-mc`) contains one entry per member cluster backend.

   The call blocks until the deployment completes and returns the
   allocated public IP address.
   - On success, `Deployed=True` is applied asynchronously, including the
     backend count and the global IP (written to
     `status.loadBalancer.ingress[0].ip`).
   - On failure, `Deployed=False` is applied asynchronously.

6. **Emit `output`.** An `output{publicGlobalIPAddress, targetGateway,
   serviceName, namespace}` record is returned, keyed by
   `namespace/serviceName`.

## Propagation (`outputs` → annotation patch)

A second collection, `krt.NewCollection(outputs, ...)`, consumes each
`output` and patches the global IP onto either:

* the member `Service` (annotation
  `service.beta.kubernetes.io/azure-additional-public-ips`), if no
  `targetGateway` was set, or
* the named Gateway API `Gateway` object, if `targetGateway` was set,

via a Kubernetes merge-patch, so downstream infrastructure (e.g. an AKS/
Istio load-balancer controller) can pick up the new anycast address.

## Status Conditions

Two condition types are tracked on `mclb.status.conditions`, both with
`observedGeneration` and `lastTransitionTime` bookkeeping handled by
`processConditions`:

| Type | Meaning |
|---|---|
| `Valid` | Whether all referenced `InternalServiceExport`s currently resolve to a Public IP. |
| `Deployed` | Whether the Azure Deployment Stack for the Global Load Balancer deployed successfully. |

All status and finalizer writes use generated apply-configurations
(`pkg/applyconfigurations/api/v1alpha1`) applied via Server-Side Apply with
field manager `globalserviceexport-controller`, rather than read-modify-write.

## API Type

`api/v1alpha1/multiclusterloadbalancer_types.go` defines `MultiClusterLoadBalancer`:

* Namespaced, short name `mclb`, category `fleet-networking`.
* Printer columns: `External-IP`, `Is-Valid`, `Is-Deployed`, `Age`.
* No spec fields (currently commented out) — the object is intentionally a
  "trigger" correlated by name/namespace with an identically named
  `ServiceExport`; all meaningful state lives in `status`.
* `status.loadBalancer` reuses `corev1.LoadBalancerStatus` so the resulting
  IP shows up in the same shape clients already expect from a `Service`
  of type `LoadBalancer`.

## Known Issues / TODOs (flagged in code)

* `writeDeployment` runs **synchronously inside the krt transform
  function**, blocking recomputation of the `outputs` collection for the
  duration of the ARM deployment. This is called out in the source as
  needing to be factored out (e.g. into an async worker/queue).
* A comment notes the deployment "seems to run repeatedly" — i.e. possible
  unnecessary re-deployments are suspected and not yet root-caused.
* Errors from the deployment are only logged; no Kubernetes `Event` is
  currently written to the `mclb` object with failure details.
* Only public Azure IP addresses are supported as backends (no private-IP
  support yet, per the user-facing README).

## Related

* [User-facing overview & walkthrough](./README.md)
* [krt-based API client infrastructure](../KrtApiClient/README.md)
