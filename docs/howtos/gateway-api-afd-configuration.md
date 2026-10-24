# Zero-Public-Exposure Ingress with Azure Front Door

> [!IMPORTANT]
> This guide describes the proposed GEP-1748 implementation on the
> `rchinchani/gep-1748-gateway-api` branch. The controller is not yet released.
> The manifests define the intended user contract and will become runnable as
> the implementation phases are completed.
> The explicitly enabled manager now reconciles the supported private HTTP
> graph, requests Private Link connections, waits for authorized manual approval,
> and activates the route before the endpoint. Current full enabled-graph
> convergence is required before publishing the observed Azure hostname and
> Gateway/listener `Programmed=True`; origin approval or accepted writes alone
> are insufficient. This controller path is validated offline, not live-proven.
>
> The approved first Azure execution subset is narrower than the full examples:
> one HTTP/80 listener with no hostname restriction, root `PathPrefix: /`,
> and one logical ServiceImport backend port across all rules. Pure request
> construction now produces this complete graph with both endpoint and route
> disabled. The enabled manager uses the complete provider lifecycle rather
> than repeatedly replaying disabled-only staging. AFD remains disabled by
> default. Live Azure convergence, identity/permissions, private HTTP delivery,
> Host/probe behavior, withdrawal and cleanup must still be proven in a separately
> authorized run before release or operational-readiness claims.
> Hostname/custom-domain routing, Exact/non-root paths, multiple
> logical backends, filters and WAF association remain outside that execution
> subset and are explicitly left unprogrammed. The hostname-based examples
> below still describe the future contract, not a working deployment today.

This guide describes **Zero-Public-Exposure Ingress**, the Fleet product solution
for multi-cluster application ingress through Azure Front Door (AFD) Premium.
The only supported origin topology is **AFD with Private Link to internal load
balancers**:

```text
Clients -> public AFD Premium edge -> Private Link -> member PLS
        -> internal Azure Load Balancer -> application
```

Zero public exposure applies to member application origins: they have no public
load balancer frontend or public origin endpoint. AFD remains the public
client-facing edge. Public member origins and AFD Standard are not supported;
Standard does not support Private Link origins.

<!-- Decision: Private Link is the only supported origin topology and requires
AFD Premium. Connectivity and SKU are product constraints, not customer choices. -->

The solution uses:

- Gateway API resources in the Fleet hub.
- A Fleet `ServiceImport` as the only supported `HTTPRoute` backend.
- A matching `Service` and `ServiceExport` in every participating member
  cluster.
- Optional attachment to an existing Azure Front Door WAF policy.

The examples configure HTTP listeners. HTTPS and certificate lifecycle are a
later implementation phase.

> [!IMPORTANT]
> **AFD Private Link approval is asynchronous and is a provisioning dependency.**
> Creating a Gateway or PLS does not mean traffic is ready. An authorized owner
> must approve each required connection, then wait for AFD provisioning and
> Gateway status to converge. Approval automation with the AFD and PLS teams is
> an open action item, not an available capability. See
> [Approve the AFD private endpoint connections](#4-approve-the-afd-private-endpoint-connections).

## Common prerequisites

> [!IMPORTANT]
> This integration requires a Fleet Manager with a managed hub. Hubless Fleet Manager resources
> do not have the Kubernetes configuration plane or hub-side `ServiceImport` aggregation used by
> the Gateway controller. Upgrade a hubless Fleet Manager by enabling its hub and reconciling
> existing members before following this guide. The upgrade cannot be reversed.

Before configuring the solution:

1. Confirm that the Fleet Manager has a managed hub. For a hubless Fleet Manager, follow the
   [hub upgrade guidance](https://learn.microsoft.com/azure/kubernetes-fleet/upgrade-hub-cluster-type)
   and reconcile its existing members.
2. Register every workload cluster as a healthy member of the same Fleet.
3. Install the Fleet hub and member networking controllers.
   Install the updated InternalServiceExport CRD on the hub first, then enable
   member `enablePrivateLinkOriginDiscovery` with Azure read permissions. This
   setting is independent of Traffic Manager. See the
   [member chart discovery guide](../../charts/member-net-controller-manager/README.md#private-origin-discovery-for-zero-public-exposure-ingress).
4. Install Gateway API `v1.2.1` CRDs in the Fleet hub.
5. Install the proposed `hub-gateway-controller-manager` in the Fleet hub using
   the [independent chart guide](../../charts/hub-gateway-controller-manager/README.md).
   The chart requires an explicit image, defaults to AFD disabled, and references
   an existing platform-owned Azure cloud-config Secret when enabled. It packages
   the opt-in private HTTP provisioning/observation/owned-cleanup runtime.
   Enabling it now authorizes controller AFD writes for supported Gateway intent;
   it is not an observation-only mode and does not itself prove live delivery.
6. Configure the controller with:
   - An Azure subscription.
   - A controller-wide AFD resource group.
   - An approved workload or managed identity.
   - Permission to manage AFD resources in the configured resource group.
   - AFD Premium and Private Link-only origins as mandatory product settings.
7. Confirm the platform-installed GatewayClass is accepted:

   ```bash
   kubectl get gatewayclass azure-fleet-afd
   ```

   Expected status:

   ```text
   NAME              CONTROLLER                          ACCEPTED
   azure-fleet-afd   networking.fleet.azure.com/afd     True
   ```

The examples use these placeholders:

| Placeholder | Description |
|---|---|
| `${MEMBER_CONTEXT}` | `kubectl` context for one Fleet member cluster. |
| `${HUB_CONTEXT}` | `kubectl` context for the Fleet hub. |
| `${NAMESPACE}` | Application namespace, such as `store`. |
| `${SERVICE_NAME}` | Service and ServiceExport name, such as `store-api`. |
| `${APP_HOSTNAME}` | Public application hostname, such as `store.example.com`. |
| `${WAF_POLICY_ID}` | Optional full resource ID of an existing AFD WAF policy. |

Repeat the member-cluster steps for every cluster that should contribute an
origin to the multi-cluster service.

## Responsibility and resource placement

| Configuration or action | Applied or performed by | Where |
|---|---|---|
| Install Gateway API CRDs | Fleet platform operator | Fleet hub |
| Install and configure `hub-gateway-controller-manager` | Fleet platform operator | Fleet hub |
| Create `GatewayClass/azure-fleet-afd` | Fleet platform operator, normally through the controller Helm chart | Fleet hub; cluster-scoped |
| Configure controller subscription, AFD resource group, identity, and feature flag | Fleet platform operator | Gateway controller deployment in the Fleet hub |
| Grant AFD and origin-discovery Azure permissions | Azure subscription or resource-group owner | Azure RBAC |
| Deploy application workload | Application team or Fleet placement controller | Every selected member cluster |
| Create the application `Service` and `ServiceExport` | Application team or Fleet placement controller | Every selected member cluster |
| Create the internal Load Balancer and PLS | AKS cloud provider, in response to the member `Service` annotations | Azure resources associated with each member cluster |
| Create and aggregate `ServiceImport` | Fleet networking controllers | Fleet hub |
| Own Private Link-only origin policy and health-probe configuration | Fleet platform operator | Gateway controller configuration; no customer ServiceImport edits |
| Create `Gateway` and `HTTPRoute` | Fleet platform operator or authorized ingress owner | Fleet hub |
| Create and maintain WAF policy | Security owner | Azure |
| Add the optional WAF policy annotation to `Gateway` | Fleet platform operator or authorized ingress owner after authorization from the security owner | Fleet hub |
| Create and reconcile AFD resources | Hub Gateway controller | Azure, in the configured AFD resource group |
| Approve AFD-to-PLS private endpoint connections | Network or security owner | Azure PLS for each member cluster |
| Create application DNS records | DNS owner | Authoritative public DNS zone |

Customers do not create or annotate `ServiceImport` directly. Fleet creates it
from matching member-cluster exports. Application teams need access only to
their member-cluster resources; the platform operator or an authorized ingress
owner performs the hub-side steps below.

Private Link is mandatory without a connectivity annotation. Health-probe
settings are controller/platform-owned rather than required customer
configuration on a generated ServiceImport. This guide does not require a
customer-selected probe path.

The approved initial HTTP-preview policy is HTTP `GET /` every 30 seconds on the
selected ServiceImport backend port. Forwarded application requests preserve the
incoming Host header. Applications must return HTTP 200 to the probes. This
policy is represented in the resolver's desired backend, not yet programmed into
AFD; actual probe Host behavior remains a provider-verification gate. Probes are
sent from distributed AFD edge locations, so the aggregate rate can exceed one
request per 30 seconds.

<!-- Decision: Do not require afd-origin-connectivity or afd-health-probe-path
annotations on ServiceImport. Customers may not have access to the hub, and
Fleet owns these generated resources. Private Link is mandatory. If configurable
probe settings or other backend settings are needed later, consider a
platform-owned ConfigMap; its schema, scope, defaults, and reconciliation are
future design work, not a supported configuration API today. -->

## Configure AFD with Private Link to internal load balancers

In this topology, each member application is exposed through an internal Azure
Load Balancer and an Azure Private Link Service (PLS). AFD Premium creates a
private endpoint connection to each PLS. The member Services are not exposed as
public origins.

This is the only supported topology for Zero-Public-Exposure Ingress and is
intended to support SFI-NS253 because the member workloads do not require public
IP addresses. The current PR provides discovery, private backend resolution and
control-plane reconcilers; it does not yet implement or certify the complete
Private Link data path.

### 1. Create an internal LoadBalancer Service and PLS in each member cluster

**Actor:** Application team or Fleet placement controller

**Target:** Every participating member cluster

```yaml
apiVersion: v1
kind: Service
metadata:
  name: store-api
  namespace: store
  annotations:
    service.beta.kubernetes.io/azure-load-balancer-internal: "true"
    service.beta.kubernetes.io/azure-pls-create: "true"
    service.beta.kubernetes.io/azure-pls-name: store-api-pls
spec:
  type: LoadBalancer
  selector:
    app: store-api
  ports:
  - name: http
    protocol: TCP
    port: 8080
    targetPort: 8080
---
apiVersion: networking.fleet.azure.com/v1alpha1
kind: ServiceExport
metadata:
  name: store-api
  namespace: store
```

Apply the manifest to every private-backend member cluster:

```bash
kubectl --context "${MEMBER_CONTEXT}" apply -f private-backend.yaml
```

Verify that the Service address is private and the export is valid:

```bash
kubectl --context "${MEMBER_CONTEXT}" \
  --namespace "${NAMESPACE}" get service "${SERVICE_NAME}" -o wide

kubectl --context "${MEMBER_CONTEXT}" \
  --namespace "${NAMESPACE}" get serviceexport "${SERVICE_NAME}" -o yaml
```

Do not continue until each `ServiceExport` reports `Valid=True` and
`Conflict=False`.

Also verify that Azure created the PLS in the AKS node resource group:

```bash
az network private-link-service show \
  --resource-group "${AKS_NODE_RESOURCE_GROUP}" \
  --name store-api-pls \
  --query "{id:id,provisioningState:provisioningState}" \
  --output yaml
```

The AKS cloud provider creates the internal load balancer and PLS. With private
origin discovery enabled, the member networking controller discovers the PLS
resource ID, Azure location, internal IP, and provisioning readiness and
transports them in `InternalServiceExport.spec.privateLinkOrigin`. These fields
are not added to public `ServiceImport.status`. Discovery readiness does not
represent approval of an AFD private endpoint connection.

If any member origin lacks the required PLS state, the route must remain
unresolved rather than fall back to a public origin. Missing approval is an
asynchronous provisioning state, not permission to change connectivity.

### 2. Create the Premium AFD Gateway and route

**Actor:** Fleet platform operator or authorized ingress owner

**Target:** Fleet hub

Only AFD Premium is supported. The explicit SKU below records that mandatory
setting; Standard is not an alternative:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: store-private-global
  namespace: store
  annotations:
    networking.fleet.azure.com/afd-sku: Premium_AzureFrontDoor
spec:
  gatewayClassName: azure-fleet-afd
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    hostname: store.example.com
    allowedRoutes:
      namespaces:
        from: Same
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: store-private-api
  namespace: store
spec:
  parentRefs:
  - name: store-private-global
    sectionName: http
  hostnames:
  - store.example.com
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /
    backendRefs:
    - group: networking.fleet.azure.com
      kind: ServiceImport
      name: store-api
      port: 8080
      weight: 100
```

Apply the resources to the hub:

```bash
kubectl --context "${HUB_CONTEXT}" apply -f private-gateway.yaml
```

The Fleet group in `backendRefs` is intentional. This implementation follows
the GEP-1748 behavior while preserving the existing
`networking.fleet.azure.com/v1alpha1 ServiceImport` API.

The intended controller must reject an incompatible SKU or origin topology
explicitly rather than downgrade to public connectivity.

### 3. Optionally attach WAF to the private Gateway

**Actors:** Security owner creates and maintains the policy; Fleet platform
operator or authorized ingress owner attaches the approved policy

**Targets:** WAF policy in Azure; annotation on the `Gateway` in the Fleet hub

Attach an existing AFD Premium-compatible WAF policy:

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" annotate gateway store-private-global \
  networking.fleet.azure.com/afd-waf-policy-id="${WAF_POLICY_ID}" \
  --overwrite
```

WAF is optional and attached at the Gateway, not at an individual route.
The controller creates only the AFD security-policy association; it does not
create, modify, or delete the external WAF policy. Removing the annotation
removes only the AFD association.

### 4. Approve the AFD private endpoint connections

**Actor:** Network or security owner

**Target:** Each member cluster's PLS in Azure

> [!IMPORTANT]
> **AFD Private Link approval is asynchronous.** AFD must first create a
> connection request, which can remain pending until the PLS owner approves it.
> An empty pending-connection list does not prove approval or readiness; the
> request may not yet be visible. Approval is required for every connection
> used by the intended topology, followed by AFD provisioning convergence.

**Open action item:** Work with the AFD and PLS teams to automate connection
approval while preserving PLS-owner authorization. Until that integration is
implemented and validated, plan for the manual approval step below. Do not
assume automatic approval is available.

<!-- Decision: Approval automation is an outstanding dependency with the AFD
and PLS teams. Keep the authorized manual workflow until automation and its
ownership/trust boundary are defined and validated. -->

Repeat this process for the PLS in every participating member cluster.

Get the PLS resource ID:

```bash
export PLS_ID=$(az network private-link-service show \
  --resource-group "${AKS_NODE_RESOURCE_GROUP}" \
  --name store-api-pls \
  --query id \
  --output tsv)
```

List pending private endpoint connections:

```bash
az network private-endpoint-connection list \
  --id "${PLS_ID}" \
  --query "[?privateLinkServiceConnectionState.status=='Pending'].{name:name,id:id}" \
  --output table
```

Approve each connection after verifying that its request belongs to the
controller-owned AFD origin:

```bash
az network private-endpoint-connection approve \
  --id "${PRIVATE_ENDPOINT_CONNECTION_ID}" \
  --description "Approve Fleet Gateway AFD origin"
```

Do not configure broad PLS auto-approval unless the platform security owner has
approved that trust boundary.

After submitting approval, observe the connection state and AFD/Gateway status;
the approval command succeeding does not mean the data path is ready.

### 5. Verify the private configuration

**Actor:** Fleet platform operator, network owner, or authorized ingress owner

**Targets:** Gateway API status in the Fleet hub, AFD resources, and member PLS
connections in Azure

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" get gateway store-private-global -o yaml

kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" get httproute store-private-api -o yaml
```

During approval, the intended controller must expose pending Private Link
approval in status and must not report the Gateway as programmed prematurely.
The route can remain `ResolvedRefs=False` or the Gateway can remain
`Programmed=False` with a reason identifying pending approval. After every
required connection is approved and provisioning converges, verify:

- Gateway `Accepted=True`.
- Gateway `Programmed=True`.
- Listener `Programmed=True`.
- HTTPRoute parent `Accepted=True`.
- HTTPRoute parent `ResolvedRefs=True`.
- Every AFD origin uses Private Link.
- No public AFD origin was created for the ServiceImport.
- Gateway `status.addresses` contains the AFD endpoint hostname.

The DNS owner creates the public DNS record for `${APP_HOSTNAME}` only after
the Gateway reports its AFD hostname. Follow the controller's status and
instructions for DNS ownership and custom-domain validation.

## Configure per-cluster traffic weights

**Actor:** Application or traffic-management owner

**Target:** `ServiceExport` in each member cluster

The existing ServiceExport annotation controls traffic
between member-cluster origins:

```bash
kubectl --context "${MEMBER_CONTEXT}" \
  --namespace "${NAMESPACE}" annotate serviceexport "${SERVICE_NAME}" \
  networking.fleet.azure.com/weight=100 \
  --overwrite
```

Use:

- `HTTPRoute.backendRefs[*].weight` to split traffic between logical
  ServiceImport backends.
- `ServiceExport` weight to split traffic between member clusters behind one
  ServiceImport.

## Remove the configuration

**Actors:** Fleet platform operator or authorized ingress owner removes hub
routing; application team or Fleet placement controller removes member resources

**Order:** Fleet hub first, then member clusters

Delete hub routing resources before deleting member Services:

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" delete httproute --all

kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" delete gateway --all
```

Wait for Gateway finalizers to finish deleting controller-owned AFD resources.
Then delete ServiceExports and Services from each member cluster. Externally
managed WAF policies are never deleted by the Gateway controller.

## Related design documents

- [GEP-1748 Gateway API for Fleet Global Ingress](../design/gep-1748-gateway-api.md)
- [GEP-1748 Gateway API Implementation Plan](../design/gep-1748-implementation-plan.md)
- [Exporting Services](../concepts/ExportingService/README.md)
