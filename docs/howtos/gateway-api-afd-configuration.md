# Configure Gateway API with Azure Front Door

> [!IMPORTANT]
> This guide describes the proposed GEP-1748 implementation on the
> `rchinchani/gep-1748-gateway-api` branch. The controller is not yet released.
> The manifests define the intended user contract and will become runnable as
> the implementation phases are completed.

This guide configures a Fleet multi-cluster HTTP application behind Azure Front
Door (AFD). It covers two independent backend topologies:

1. [AFD with public backends](#configure-afd-with-public-backends)
2. [AFD with Private Link to internal load balancers](#configure-afd-with-private-link-to-internal-load-balancers)

Both topologies use:

- Gateway API resources in the Fleet hub.
- A Fleet `ServiceImport` as the only supported `HTTPRoute` backend.
- A matching `Service` and `ServiceExport` in every participating member
  cluster.
- Optional attachment to an existing Azure Front Door WAF policy.

The examples configure HTTP listeners. HTTPS and certificate lifecycle are a
later implementation phase.

## Common prerequisites

> [!IMPORTANT]
> This integration requires a Fleet Manager with a managed hub. Hubless Fleet Manager resources
> do not have the Kubernetes configuration plane or hub-side `ServiceImport` aggregation used by
> the Gateway controller. Upgrade a hubless Fleet Manager by enabling its hub and reconciling
> existing members before following this guide. The upgrade cannot be reversed.

Before configuring either topology:

1. Confirm that the Fleet Manager has a managed hub. For a hubless Fleet Manager, follow the
   [hub upgrade guidance](https://learn.microsoft.com/azure/kubernetes-fleet/upgrade-hub-cluster-type)
   and reconcile its existing members.
2. Register every workload cluster as a healthy member of the same Fleet.
3. Install the Fleet hub and member networking controllers.
4. Install Gateway API `v1.2.1` CRDs in the Fleet hub.
5. Install the proposed `hub-gateway-controller-manager` in the Fleet hub.
6. Configure the controller with:
   - An Azure subscription.
   - A controller-wide AFD resource group.
   - An approved workload or managed identity.
   - Permission to manage AFD resources in the configured resource group.
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
| Create the public Load Balancer | AKS cloud provider, in response to the member `Service` | Azure resources associated with each member cluster |
| Create the internal Load Balancer and PLS | AKS cloud provider, in response to the member `Service` annotations | Azure resources associated with each member cluster |
| Create and aggregate `ServiceImport` | Fleet networking controllers | Fleet hub |
| Add AFD backend annotations to `ServiceImport` | Application or networking owner | Existing generated `ServiceImport` in the Fleet hub |
| Create `Gateway` and `HTTPRoute` | Application ingress owner | Fleet hub |
| Create and maintain WAF policy | Security owner | Azure |
| Add the optional WAF policy annotation to `Gateway` | Application ingress owner after authorization from the security owner | Fleet hub |
| Create and reconcile AFD resources | Hub Gateway controller | Azure, in the configured AFD resource group |
| Approve AFD-to-PLS private endpoint connections | Network or security owner | Azure PLS for each member cluster |
| Create application DNS records | DNS owner | Authoritative public DNS zone |

Users do not create `ServiceImport` directly. Fleet creates it from matching
member-cluster exports. Users annotate the generated hub object after it
appears. A production GitOps workflow should use a controller or patch that
waits for the generated object instead of attempting to own its full manifest.

## Configure AFD with public backends

In this topology, every member cluster exposes the application through a public
Azure Load Balancer. AFD connects to those public origins.

### 1. Create a public Service in each member cluster

**Actor:** Application team or Fleet placement controller

**Target:** Every participating member cluster

Use a unique Azure DNS label in each member cluster. The label gives AFD a
stable origin hostname and must be unique within the Azure region.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: store-api
  namespace: store
  annotations:
    service.beta.kubernetes.io/azure-dns-label-name: store-api-member-east
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

Apply the manifest to each member cluster:

```bash
kubectl --context "${MEMBER_CONTEXT}" apply -f public-backend.yaml
```

Verify that the Service has a public address and that the export is valid:

```bash
kubectl --context "${MEMBER_CONTEXT}" \
  --namespace "${NAMESPACE}" get service "${SERVICE_NAME}"

kubectl --context "${MEMBER_CONTEXT}" \
  --namespace "${NAMESPACE}" get serviceexport "${SERVICE_NAME}" -o yaml
```

Do not continue until each `ServiceExport` reports `Valid=True` and
`Conflict=False`.

### 2. Select public origin connectivity on the ServiceImport

**Actor:** Application or networking owner

**Target:** Generated `ServiceImport` in the Fleet hub

Fleet creates the `ServiceImport` in the hub after at least one valid export is
observed. Keep its API unchanged and add only the AFD connectivity annotation:

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" annotate serviceimport "${SERVICE_NAME}" \
  networking.fleet.azure.com/afd-origin-connectivity=public \
  networking.fleet.azure.com/afd-health-probe-path=/healthz \
  --overwrite
```

The `public` value is explicit: if any member origin lacks a usable public
endpoint, the route receives `ResolvedRefs=False`. The controller must not
silently omit the invalid member or switch the backend to Private Link.

### 3. Create the public AFD Gateway and route

**Actor:** Application ingress owner

**Target:** Fleet hub

AFD Standard is sufficient for public origins. Premium is also valid.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: store-global
  namespace: store
  annotations:
    networking.fleet.azure.com/afd-sku: Standard_AzureFrontDoor
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
  name: store-api
  namespace: store
spec:
  parentRefs:
  - name: store-global
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
kubectl --context "${HUB_CONTEXT}" apply -f public-gateway.yaml
```

The Fleet group in `backendRefs` is intentional. This implementation follows
the GEP-1748 behavior while preserving the existing
`networking.fleet.azure.com/v1alpha1 ServiceImport` API.

### 4. Optionally attach WAF to the public Gateway

**Actors:** Security owner creates and maintains the policy; application
ingress owner attaches the approved policy

**Targets:** WAF policy in Azure; annotation on the `Gateway` in the Fleet hub

WAF is optional and is attached at the Gateway, not at an individual route.
The referenced policy must already exist and must be compatible with the
selected AFD SKU.

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" annotate gateway store-global \
  networking.fleet.azure.com/afd-waf-policy-id="${WAF_POLICY_ID}" \
  --overwrite
```

The controller creates only the AFD security-policy association. It does not
create, modify, or delete the external WAF policy.

To remove WAF while retaining the Gateway:

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" annotate gateway store-global \
  networking.fleet.azure.com/afd-waf-policy-id-
```

### 5. Verify the public configuration

**Actor:** Application ingress owner or Fleet platform operator

**Targets:** Gateway API status in the Fleet hub and AFD resources in Azure

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" get gateway store-global -o yaml

kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" get httproute store-api -o yaml
```

Verify:

- Gateway `Accepted=True`.
- Gateway `Programmed=True`.
- Listener `Programmed=True`.
- HTTPRoute parent `Accepted=True`.
- HTTPRoute parent `ResolvedRefs=True`.
- Gateway `status.addresses` contains the AFD endpoint hostname.

Create the required public DNS record for `${APP_HOSTNAME}` only after the
Gateway reports its AFD hostname. DNS ownership and custom-domain validation
must follow the status and instructions emitted by the controller. The DNS
owner performs this step in the authoritative public DNS zone.

## Configure AFD with Private Link to internal load balancers

In this topology, each member application is exposed through an internal Azure
Load Balancer and an Azure Private Link Service (PLS). AFD Premium creates a
private endpoint connection to each PLS. The member Services are not exposed as
public origins.

This is the topology intended to support SFI-NS253 because the member workloads
do not require public IP addresses. The public backend configuration in the
preceding section is not the SFI-NS253 topology. The current PR documents and
validates the contract but does not yet implement or certify the complete
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

Also verify that Azure created the PLS in the AKS node resource group:

```bash
az network private-link-service show \
  --resource-group "${AKS_NODE_RESOURCE_GROUP}" \
  --name store-api-pls \
  --query "{id:id,provisioningState:provisioningState}" \
  --output yaml
```

The AKS cloud provider creates the internal load balancer and PLS. The member
networking controller then discovers the PLS resource ID, Azure location, and
readiness and transports them to the hub through internal Fleet networking
state. These fields are not added to public `ServiceImport.status`.

### 2. Require Private Link connectivity on the ServiceImport

**Actor:** Application or networking owner

**Target:** Generated `ServiceImport` in the Fleet hub

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" annotate serviceimport "${SERVICE_NAME}" \
  networking.fleet.azure.com/afd-origin-connectivity=private-link \
  networking.fleet.azure.com/afd-health-probe-path=/healthz \
  --overwrite
```

The explicit `private-link` value prevents unsafe downgrade. If any member
origin lacks a ready PLS, the route remains unresolved rather than becoming
public.

All origins represented by one ServiceImport must use the same connectivity
mode. Public and Private Link origins cannot be mixed in one AFD origin group.

### 3. Create the Premium AFD Gateway and route

**Actor:** Application ingress owner

**Target:** Fleet hub

Private Link origins require the Premium AFD SKU:

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

Using `private-link` with `Standard_AzureFrontDoor` is invalid and must produce
an explicit condition rather than a fallback.

### 4. Optionally attach WAF to the private Gateway

**Actors:** Security owner creates and maintains the policy; application
ingress owner attaches the approved policy

**Targets:** WAF policy in Azure; annotation on the `Gateway` in the Fleet hub

Attach an existing AFD Premium-compatible WAF policy:

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" annotate gateway store-private-global \
  networking.fleet.azure.com/afd-waf-policy-id="${WAF_POLICY_ID}" \
  --overwrite
```

As with the public topology, the WAF policy lifecycle remains external to the
Gateway. Removing the annotation removes only the AFD association.

### 5. Approve the AFD private endpoint connections

**Actor:** Network or security owner

**Target:** Each member cluster's PLS in Azure

AFD Private Link approval is asynchronous. Repeat this process for the PLS in
every participating member cluster.

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

### 6. Verify the private configuration

**Actor:** Application ingress owner, network owner, or Fleet platform operator

**Targets:** Gateway API status in the Fleet hub, AFD resources, and member PLS
connections in Azure

```bash
kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" get gateway store-private-global -o yaml

kubectl --context "${HUB_CONTEXT}" \
  --namespace "${NAMESPACE}" get httproute store-private-api -o yaml
```

During approval, the route can remain `ResolvedRefs=False` or the Gateway can
remain `Programmed=False` with a reason identifying pending Private Link
approval. After every required connection is approved, verify:

- Gateway `Accepted=True`.
- Gateway `Programmed=True`.
- HTTPRoute parent `Accepted=True`.
- HTTPRoute parent `ResolvedRefs=True`.
- Every AFD origin uses Private Link.
- No public AFD origin was created for the ServiceImport.

## Configure per-cluster traffic weights

**Actor:** Application or traffic-management owner

**Target:** `ServiceExport` in each member cluster

For either topology, the existing ServiceExport annotation controls traffic
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

**Actors:** Application ingress owner removes hub routing; application team or
Fleet placement controller removes member resources

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
