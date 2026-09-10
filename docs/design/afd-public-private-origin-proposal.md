# Azure Front Door Public and Private Fleet Origins

## Status

Proposed; the default-off API and read-only attachment-controller foundation is
implemented on `rchinchani/afd-public-private-origin-proposal`.

This document defines a target architecture and candidate Kubernetes API for exposing a
multi-cluster Fleet application through Azure Front Door (AFD) and Azure Web Application
Firewall (WAF). It covers:

1. AFD + WAF with public `LoadBalancer` Services in member clusters.
2. AFD + WAF + Azure Private Link Service (PLS) with internal `LoadBalancer` Services in
   member clusters.

The API names are introduced as `v1alpha1` contracts and remain subject to API review
before Azure resource programming is enabled.

## Decision Summary

Fleet should keep `ServiceImport` as the multi-cluster service identity and create one AFD
origin per eligible `InternalServiceExport`. Gateway API expresses listeners, hostnames,
routes, and route-to-backend weights. Two typed, Azure-specific provider resources express
AFD profile/WAF configuration and each Gateway-to-`ServiceImport` origin attachment.

The controller must not depend on GEP-4894's current Pod selector. Even if Gateway API
PR 5158 merges, a namespace-local Pod selector on the hub cannot select Pods in member
clusters. The initial backend path remains:

```text
HTTPRoute
  -> Fleet ServiceImport
    -> InternalServiceExport per member cluster
      -> public load balancer FQDN, or
      -> internal load balancer through Private Link Service
```

### Gateway API GEP Alignment

This design preserves the GEP-1748 multi-cluster backend model:
`HTTPRoute.backendRefs` continues to reference Fleet `ServiceImport`, and Fleet remains
responsible for aggregating the member-cluster endpoints behind that identity. The
Azure-specific `AzureFrontDoorBackendAttachment` adds placement and connectivity policy
for a `(Gateway, ServiceImport, port)` tuple without changing the portable route.

GEP-4894 is treated as a compatible future direction, not the current Fleet binding
contract. Its evolving, namespace-local Pod `selectorRef` cannot select member-cluster
Pods from a hub cluster. The implementation therefore does not reinterpret
`selectorRef`, create proxy Pods, or make production behavior depend on an experimental
field. The explicit attachment can be revisited if GEP-4894 later standardizes a backend
reference model that preserves Fleet's cross-cluster semantics.

Public and private origins must never share an AFD origin group. Private origins require
AFD Premium. A route must fail closed when its required WAF policy is absent, when origin
connectivity is ambiguous, or when a private origin is not approved and established.
There is no automatic private-to-public fallback.

### Why AFD, Not Traffic Manager, for SFI Application DDoS

The SFI Application DDoS standard requires untrusted internet HTTP/S traffic to pass
through an approved, globally distributed Layer-7 proxy before it consumes
service-owned regional capacity. Traffic Manager cannot provide that enforcement
boundary: it returns an origin through DNS and the client then connects directly to the
selected regional endpoint. It cannot inspect HTTP requests, apply WAF or Bot Manager
rules, enforce per-client rate limits, or prevent clients from bypassing the protected
path to reach an origin.

AFD is in the request data path and supplies the required shared global proxy capacity.
For the NS 2.5.3 baseline, the production design therefore uses AFD Premium with an
associated WAF policy, enabled `Microsoft_BotManagerRuleSet`, at least one enabled
rate-limit custom rule, and origin bypass prevention. Public origins require both
`AzureFrontDoor.Backend` filtering and exact `X-Azure-FDID` validation; private origins
remove the public path by using Private Link. Traffic Manager may remain useful for
non-HTTP protocols, but it is not an equivalent SFI Application DDoS control for this
HTTP/S ingress architecture.

## Goals

- Present one global HTTPS endpoint for an application exported from multiple member
  clusters.
- Apply WAF at the AFD edge before traffic reaches a member cluster.
- Use AFD health and latency signals to steer traffic only to healthy origins.
- Preserve per-cluster traffic weight and priority independently from `HTTPRoute`
  backend weights.
- Protect public origins from direct WAF bypass.
- Keep private member-cluster origins off the public internet by using PLS.
- Expose precise Kubernetes status for Azure provisioning, Private Link approval, and
  origin programming.
- Reuse Fleet's existing `ServiceExport` -> `InternalServiceExport` -> `ServiceImport`
  data flow and its established Azure ownership/finalizer patterns.

## Non-goals

- Replacing `ServiceImport` with GEP-4894 `Backend`.
- Selecting member-cluster Pods from the hub.
- Creating fake hub Pods to satisfy a Gateway API selector.
- Configuring application-level authorization or WAF rules from `HTTPRoute`.
- Providing AFD-to-origin mutual TLS. AFD does not support mTLS for public or Private
  Link origins.
- Mixing public and private origins in one origin group.
- Silently falling back from private to public connectivity.
- Managing arbitrary Azure resources referenced by ID unless policy explicitly selects
  `Managed` ownership.
- Solving non-Azure member-cluster connectivity in the first release.

## Personas and Responsibility Boundaries

| Persona | Owns |
| --- | --- |
| Platform operator | `GatewayClass`, controller deployment, Azure identity, profile placement, diagnostics defaults |
| Security operator | WAF policy lifecycle and mode, managed/custom rules, origin bypass controls |
| Application operator | `Gateway`, `HTTPRoute`, `ServiceExport`, health endpoint, origin host/certificate |
| Fleet member agent | Observing member Service and Azure transport resources; publishing internal metadata |
| Fleet hub controller | Resolving routes and provider configuration; reconciling AFD resources; reporting status |
| Azure cloud provider | Creating public/internal load balancers and PLS from Service annotations |

Gateway API remains portable. Azure-specific intent is isolated in typed provider
resources instead of an expanding set of annotations on `Gateway` and `HTTPRoute`.

## Shared Architecture

### Kubernetes objects

The member cluster contains:

- application Pods;
- a `Service` of type `LoadBalancer`;
- a `ServiceExport`.

The hub contains:

- an internal `InternalServiceExport` for each exported member Service;
- a public, status-only `ServiceImport`;
- a `GatewayClass` controlled by Fleet AFD;
- a `Gateway`;
- one or more `HTTPRoute` objects;
- an `AzureFrontDoorGatewayPolicy` targeting the `Gateway`;
- an `AzureFrontDoorBackendAttachment` for each Gateway, `ServiceImport`, and service
  port tuple consumed through AFD.

### Azure objects

The controller maps the Kubernetes model to:

```text
Microsoft.Cdn/profiles
  Microsoft.Cdn/profiles/afdEndpoints
    Microsoft.Cdn/profiles/afdEndpoints/routes
  Microsoft.Cdn/profiles/originGroups
    Microsoft.Cdn/profiles/originGroups/origins
  Microsoft.Cdn/profiles/securityPolicies

Microsoft.Network/FrontDoorWebApplicationFirewallPolicies
```

For private origins, each member cluster also owns:

```text
Microsoft.Network/loadBalancers (internal frontend)
Microsoft.Network/privateLinkServices
  privateEndpointConnections requested by AFD
```

The member Service and its cloud provider own the load balancer and PLS. The hub AFD
controller consumes their resource IDs but does not adopt or delete them.

### Resource ownership

The first implementation should support two AFD profile modes:

- `Managed`: Fleet creates and owns the AFD profile and all child resources it names.
- `Existing`: Fleet references an operator-owned profile and owns only child resources
  carrying Fleet ownership tags.

WAF policy lifecycle is independent:

- `Existing` is required initially. The policy contains an Azure resource ID and the
  controller creates the AFD security-policy association.
- Creating and editing WAF rule sets is a later capability because it crosses a security
  administration boundary.

Every created Azure resource must carry stable ownership tags containing the hub cluster
identity and the owning Kubernetes UID. Deletion must remove only resources with matching
ownership.

## Candidate API

### AzureFrontDoorGatewayPolicy

This policy targets one `Gateway` and controls profile-wide and frontend settings.

```yaml
apiVersion: networking.fleet.azure.com/v1alpha1
kind: AzureFrontDoorGatewayPolicy
metadata:
  name: global-ingress
  namespace: app
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: global
  profile:
    mode: Managed
    sku: Premium_AzureFrontDoor
    resourceGroup: fleet-global
    name: fleet-global
  waf:
    required: true
    policyResourceID: /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/security/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies/fleet-waf
  diagnostics:
    enabled: true
    destinationResourceID: /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/observability/providers/Microsoft.OperationalInsights/workspaces/fleet
```

Proposed fields:

| Field | Meaning |
| --- | --- |
| `targetRef` | Same-namespace `Gateway` controlled by the Fleet AFD GatewayClass |
| `profile.mode` | `Managed` or `Existing` |
| `profile.sku` | `Standard_AzureFrontDoor` or `Premium_AzureFrontDoor` |
| `profile.resourceGroup/name` | Stable Azure placement and identity |
| `waf.required` | Fail route programming if no valid WAF association exists |
| `waf.policyResourceID` | Existing WAF policy to associate with route domains |
| `diagnostics` | Access, health-probe, and WAF diagnostic destination |

The policy must not contain credentials. The controller uses workload identity.

### AzureFrontDoorBackendAttachment

This attachment defines how one Gateway consumes one `ServiceImport` port through its
AFD profile. The attachment, rather than the `ServiceImport`, owns the resulting origin
group and member origins. This allows the same logical service to use different public or
Private Link, TLS, probe, and traffic settings in different Gateways without overloading
the portable `ServiceImport`.

```yaml
apiVersion: networking.fleet.azure.com/v1alpha1
kind: AzureFrontDoorBackendAttachment
metadata:
  name: global-store-https
  namespace: app
spec:
  gatewayRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: global
  backendRef:
    group: networking.fleet.azure.com
    kind: ServiceImport
    name: store
    port: 443
  connectivity:
    mode: PrivateLink
    privateLink:
      approval: Manual
      regionSelection: ClosestSupported
  origin:
    protocol: HTTPS
    hostHeader: store.internal.contoso.example
    certificateSubjectNameCheck: true
  healthProbe:
    protocol: HTTPS
    method: HEAD
    path: /healthz
    intervalSeconds: 30
    sampleSize: 4
    successfulSamplesRequired: 3
  traffic:
    defaultPriority: 1
    defaultWeight: 1000
  memberFailurePolicy: Partial
```

Proposed fields:

| Field | Meaning |
| --- | --- |
| `gatewayRef` | Same-namespace `Gateway` whose gateway policy resolves the AFD profile |
| `backendRef` | `ServiceImport` and required service port consumed by this Gateway |
| `connectivity.mode` | `Public` or `PrivateLink` |
| `privateLink.approval` | `Manual`; `Controller` is deferred until safe correlation is proven |
| `privateLink.regionSelection` | `MemberRegion` when supported, otherwise `ClosestSupported` |
| `origin.protocol` | AFD-to-origin connection protocol for `backendRef.port` |
| `origin.hostHeader` | HTTP Host header sent to every member origin |
| `certificateSubjectNameCheck` | Must be `true` for Private Link and defaults to `true` for public HTTPS |
| `healthProbe` | AFD origin-group health settings |
| `traffic.defaultPriority/defaultWeight` | Defaults before per-member overrides |
| `memberFailurePolicy` | `All` or `Partial` origin eligibility |

The attachment identity is the tuple of Gateway UID, `ServiceImport` UID, and service
port. Only one attachment may be accepted for a tuple. Admission rejects a duplicate
when it can observe the incumbent. Reconciliation remains deterministic under concurrent
creation: the oldest attachment by creation timestamp, with UID as the tie-breaker,
remains accepted and later duplicates report `Accepted=False` with reason `Conflicted`.
A new duplicate must not disrupt a programmed attachment.

An `HTTPRoute` backend is programmed only when its parent Gateway and
`ServiceImport`/port reference match an accepted attachment. Multiple routes may share
that attachment and its origin group. An accepted but unused attachment does not create
Azure resources. If one Gateway needs two different origin contracts for the same
`ServiceImport` port, the initial API requires separate Gateways rather than ambiguous
per-route overrides.

The initial API requires the attachment, Gateway, and `ServiceImport` to share a
namespace. A later cross-namespace `backendRef` may be enabled only with an applicable
Gateway API `ReferenceGrant` in the `ServiceImport` namespace.

### Provider API validation

Admission and reconciliation must enforce:

- `PrivateLink` requires `Premium_AzureFrontDoor`.
- One attachment cannot mix public and private member origins in its AFD origin group.
- `gatewayRef`, `backendRef`, and `backendRef.port` must resolve.
- `gatewayRef`, `backendRef`, and `backendRef.port` are immutable.
- A route backend must have an accepted attachment matching its parent Gateway,
  `ServiceImport`, and port before the route can be programmed.
- HTTPS Private Link requires certificate subject-name validation.
- `backendRef.port` must exist in `ServiceImport.status.ports`.
- HTTP and HTTPS are the only AFD origin protocols.
- Health probes use HTTP or HTTPS and `GET` or `HEAD`.
- `sampleSize >= successfulSamplesRequired > 0`.
- An origin weight and priority fit Azure limits.
- A WAF policy tier matches the AFD profile tier.
- `waf.required: true` rejects an empty or inaccessible policy resource ID.
- No controller-managed resource can collide with an existing resource lacking matching
  ownership tags.

## Member-to-Hub Transport Contract

`ServiceImport.status` remains portable and must not expose Azure resource IDs or private
endpoint connection state. `InternalServiceExportSpec` is the internal transport seam.

The member controller should publish an observed endpoint structure similar to:

```yaml
spec:
  serviceReference:
    namespace: app
    name: store
  serviceType: LoadBalancer
  azure:
    location: eastus2
    loadBalancer:
      internal: true
      frontendAddress: 10.20.0.10
    privateLinkService:
      resourceID: /subscriptions/.../privateLinkServices/pls-store
      alias: pls-store....azure.privatelinkservice
      provisioningState: Succeeded
```

For public Services it should publish:

- public IP resource ID;
- public FQDN derived from the public IP DNS settings;
- public IP provisioning readiness;
- whether `AzureFrontDoor.Backend` is allowed by the Service's Azure service-tag
  annotation;
- member Azure region.

For private Services it should publish:

- internal load balancer address;
- PLS resource ID and alias;
- PLS Azure region;
- PLS provisioning state;
- observed visibility configuration;
- observed private endpoint connections relevant to the managed AFD profile.

The internal fields are observed state, not a second source of desired configuration.
The member Service annotations remain authoritative for load balancer and PLS creation.

## Public Origin Topology

### Member Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: store
  namespace: app
  annotations:
    service.beta.kubernetes.io/azure-dns-label-name: store-eastus2
    service.beta.kubernetes.io/azure-allowed-service-tags: AzureFrontDoor.Backend
spec:
  type: LoadBalancer
  selector:
    app: store
  ports:
  - name: https
    protocol: TCP
    port: 443
    targetPort: 8443
---
apiVersion: networking.fleet.azure.com/v1beta1
kind: ServiceExport
metadata:
  name: store
  namespace: app
```

`azure-allowed-service-tags` and `loadBalancerSourceRanges` must not be used together.
AKS rejects that combination. The public origin must also validate `X-Azure-FDID` at the
application or regional ingress layer. The service tag blocks non-AFD source ranges; the
header binds requests to this specific AFD profile because AFD backend addresses are
shared across customers.

The header value is an output of the managed AFD profile. The controller reports it in
Gateway policy status; an application delivery mechanism outside this proposal injects
it into the regional ingress policy. `Programmed=True` must not be reported for a public
backend until the operator confirms bypass protection, unless an explicit unsafe
development override is enabled.

### Public request path

```text
Client
  -> AFD edge listener
  -> WAF security policy
  -> AFD route
  -> public origin group
  -> member public Standard Load Balancer
  -> Service endpoints
  -> application Pods
```

AFD uses the member FQDN as the origin hostname. For HTTPS, the member endpoint must
present a certificate whose subject matches that hostname. The controller does not
disable subject-name validation.

### Public origin eligibility

A member becomes eligible only when:

- the `InternalServiceExport` is valid and selected by the `ServiceImport`;
- the Service is a public `LoadBalancer`;
- the exported port exists;
- public IP and FQDN discovery succeeded;
- bypass protection is observed or acknowledged;
- the origin certificate and host contract are configured;
- the member is not deleting or explicitly disabled.

An AFD health probe decides runtime health after programming. Kubernetes
`Programmed=True` means the Azure desired configuration exists; it does not mean the
origin is healthy.

## Private Origin Topology

### Member Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: store
  namespace: app
  annotations:
    service.beta.kubernetes.io/azure-load-balancer-internal: "true"
    service.beta.kubernetes.io/azure-pls-create: "true"
    service.beta.kubernetes.io/azure-pls-name: pls-store
spec:
  type: LoadBalancer
  selector:
    app: store
  ports:
  - name: https
    protocol: TCP
    port: 443
    targetPort: 8443
---
apiVersion: networking.fleet.azure.com/v1beta1
kind: ServiceExport
metadata:
  name: store
  namespace: app
```

Omitting PLS visibility keeps the service at its most restrictive, RBAC-only default.
Broad visibility (`"*"`) is not an acceptable controller default. Auto-approval must not
be configured unless its subscription list is a subset of visibility and the security
owner explicitly opts in.

Member prerequisites:

- AKS Standard Load Balancer;
- `nodeIPConfiguration` backend-pool type;
- IPv4 and TCP for AFD HTTP/HTTPS traffic;
- a PLS NAT subnet with sufficient addresses;
- if `externalTrafficPolicy: Local`, the PLS subnet differs from the Pod subnet;
- PROXY protocol remains disabled unless the backend and health-probe behavior support it.

### Private request path

```text
Client
  -> AFD Premium edge listener
  -> WAF security policy
  -> AFD route
  -> private origin group
  -> AFD-managed regional private endpoint
  -> member Private Link Service
  -> member internal Standard Load Balancer
  -> Service endpoints
  -> application Pods
```

The AFD origin references the PLS resource ID and chooses the same supported Private Link
region as the member, or the nearest supported region. The origin hostname is used for
SNI and must match the server certificate. The origin host header is independently
configurable.

### Private Link state machine

```text
PLSNotFound
  -> PLSProvisioning
  -> PLSReady
  -> AFDRequestCreated
  -> ApprovalPending
  -> Approved
  -> ConnectionEstablished
  -> OriginProgrammed
  -> ProbeHealthy
```

Terminal or degraded branches include:

- `PLSFailed`;
- `ApprovalRejected`;
- `ConnectionDisconnected`;
- `OriginProgrammingFailed`;
- `ProbeUnhealthy`.

In the initial release, approval is manual. The controller emits an event containing the
PLS resource ID, expected AFD profile, and request message, and requeues without treating
the pending state as an error. A later `Controller` mode may approve requests only after
the Azure API exposes enough stable identity to correlate exactly one pending request to
the owning AFD origin. It must never approve an arbitrary new PLS connection.

AFD traffic and health probes both use the private path. The controller must not create a
temporary public origin while approval is pending.

### Private endpoint reuse and ports

Within one AFD profile, origins with the same PLS resource ID, group ID, and Private Link
region share one AFD-managed private endpoint and therefore one approval. A change to any
of those values creates another request.

AFD documents a routing limitation when identical Private Link resource/group/region
tuples are used with different origin ports. Validation must reject that configuration
within one profile.

For regional resilience, distinct member origins should use distinct Private Link
regions. This avoids concentrating all private traffic through one AFD regional cluster.

## Hub Example

The same hub routing YAML works for either topology; only the backend attachment
connectivity changes.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: global
  namespace: app
spec:
  gatewayClassName: fleet-azure-front-door
  listeners:
  - name: https
    protocol: HTTPS
    port: 443
    hostname: store.contoso.com
    tls:
      mode: Terminate
      certificateRefs:
      - group: networking.fleet.azure.com
        kind: AzureFrontDoorCertificate
        name: store
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: store
  namespace: app
spec:
  parentRefs:
  - name: global
  hostnames:
  - store.contoso.com
  rules:
  - backendRefs:
    - group: networking.fleet.azure.com
      kind: ServiceImport
      name: store
      port: 443
      weight: 100
---
apiVersion: networking.fleet.azure.com/v1alpha1
kind: AzureFrontDoorBackendAttachment
metadata:
  name: global-store-https
  namespace: app
spec:
  gatewayRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: global
  backendRef:
    group: networking.fleet.azure.com
    kind: ServiceImport
    name: store
    port: 443
  connectivity:
    mode: PrivateLink
    privateLink:
      approval: Manual
      regionSelection: ClosestSupported
  origin:
    protocol: HTTPS
    hostHeader: store.internal.contoso.example
    certificateSubjectNameCheck: true
  healthProbe:
    protocol: HTTPS
    method: HEAD
    path: /healthz
    intervalSeconds: 30
    sampleSize: 4
    successfulSamplesRequired: 3
  traffic:
    defaultPriority: 1
    defaultWeight: 1000
  memberFailurePolicy: Partial
```

`HTTPRoute.backendRefs[].weight` divides traffic between logical backends in one route.
It does not replace the per-member origin weight inside the selected `ServiceImport`.

## Traffic Policy

Per-member priority and weight are resolved in this order:

1. an explicit per-member override in the backend attachment, if added after API review;
2. the existing `ServiceExport` weight annotation;
3. backend-attachment defaults.

Weights are normalized into AFD's accepted range while preserving relative ratios.
Priority takes precedence over weight: only healthy origins at the highest available
priority receive traffic, and weight divides traffic within that priority.

Member draining is two phase:

1. disable the AFD origin and wait at least the configured drain interval;
2. delete the origin after active traffic has quiesced.

Deleting a `ServiceExport`, leaving the Fleet, or losing endpoint readiness enters the
same drain path. A transient read failure does not immediately delete an origin.

`memberFailurePolicy` controls desired-state eligibility:

- `All`: any invalid member prevents origin-group updates and keeps the last known good
  configuration.
- `Partial`: valid members are programmed and invalid members appear in status.

`Partial` is recommended for availability, but the route is not accepted if zero origins
are eligible.

## WAF and Frontend Security

AFD WAF protection is attached through an AFD security policy that associates the WAF
policy with the domains used by routes. A WAF policy may be in:

- `Detection` mode for staged rollout;
- `Prevention` mode for enforcement.

Managed rule sets require AFD Premium. Standard supports custom rules but not managed rule
sets. Because the private topology already requires Premium, the recommended production
default for both topologies is Premium with managed rules in prevention mode after an
observed detection period.

The controller verifies that every programmed custom domain requiring WAF is present in
the security-policy association. It does not report the Gateway `Programmed=True` while
the association is missing.

Public origin security requires both:

- NSG filtering through `AzureFrontDoor.Backend`;
- exact `X-Azure-FDID` validation.

Private origin security relies on a non-public internal load balancer plus PLS. NSGs may
further restrict the VNet, but the member Service is not internet routable.

## TLS Semantics

There are two independent TLS hops:

1. client to AFD listener, configured by the `Gateway` listener certificate reference;
2. AFD to member origin, configured by `AzureFrontDoorBackendAttachment.origin`.

The controller must never infer the origin protocol from the listener protocol. HTTPS at
the edge may use HTTP or HTTPS to the origin, although HTTPS is recommended.

For origin HTTPS:

- use a DNS hostname, not an ephemeral IP, when possible;
- the origin hostname drives SNI;
- keep certificate subject-name validation enabled;
- report a clear condition if Fleet cannot determine a stable hostname;
- do not claim support for client certificates or AFD backend mTLS.

## Status and Conditions

### Gateway policy status

```yaml
status:
  observedGeneration: 3
  profile:
    resourceID: /subscriptions/.../profiles/fleet-global
    frontDoorID: 11111111-1111-1111-1111-111111111111
  conditions:
  - type: Accepted
    status: "True"
    reason: Valid
  - type: ProfileReady
    status: "True"
    reason: Succeeded
  - type: WAFReady
    status: "True"
    reason: Associated
  - type: DiagnosticsReady
    status: "True"
    reason: Configured
```

### Backend attachment status

```yaml
status:
  observedGeneration: 2
  conditions:
  - type: Accepted
    status: "True"
    reason: Valid
  - type: ResolvedRefs
    status: "True"
    reason: Resolved
  - type: OriginsProgrammed
    status: "False"
    reason: PrivateLinkApprovalPending
  members:
  - clusterName: member-east
    originName: store-member-east
    connectivity: PrivateLink
    privateLink:
      serviceResourceID: /subscriptions/.../privateLinkServices/pls-store
      connectionState: Pending
    conditions:
    - type: TransportReady
      status: "True"
      reason: PLSProvisioned
    - type: ConnectionReady
      status: "False"
      reason: ApprovalPending
```

Condition rules:

- conditions include `observedGeneration`;
- expected asynchronous states use `False` with specific reasons, not generic errors;
- Azure errors retain a sanitized error code and correlation ID;
- status is bounded and does not copy unbounded Azure response text;
- probe health is reported separately from configuration programming;
- events are emitted on transitions, not every retry.

`Gateway` and `HTTPRoute` standard conditions are updated only by their owning
reconcilers. Gateway-policy and backend-attachment status supply Azure-specific detail.

## Reconciliation Model

The hub controller builds an immutable normalized model from Gateway API, Fleet, and
provider configuration objects. Azure reconcilers consume only that model.

Reconciliation order:

1. validate GatewayClass, Gateway, gateway policy, backend attachments, listeners, and
   route references, including an exact attachment match for every AFD route backend;
2. resolve each accepted attachment's `ServiceImport` to current
   `InternalServiceExport` objects;
3. classify every member as public, private, invalid, or pending;
4. reject mixed connectivity for one backend;
5. ensure the profile, endpoint, domains, and certificates;
6. ensure the WAF security-policy association;
7. ensure homogeneous origin groups;
8. ensure origins, including Private Link request properties;
9. ensure routes only after required origins and WAF associations exist;
10. update provider and Gateway API status.

The finalizer is added immediately before the first owned Azure resource is created.
Deletion lists Azure resources by ownership tags, disables routes/origins, drains traffic,
deletes owned child resources, then removes the finalizer. A referenced PLS, load balancer,
public IP, or existing WAF policy is never deleted.

Drift policy:

- owned mutable fields are reconciled to desired state;
- operator changes to non-owned fields are preserved;
- ownership-tag loss or resource replacement is reported as a conflict, not adopted;
- the last known good route remains when a transient dependency read fails;
- invalid desired changes do not destructively replace a working origin group.

## Identity and RBAC

Kubernetes RBAC requires read/watch access to Gateway API objects, gateway policies,
backend attachments, `ServiceImport`, and `InternalServiceExport`, plus
status/finalizer updates only for owned types. The initial same-namespace attachment rule
keeps authorization explicit. Future cross-namespace backend references require a
`ReferenceGrant` from the `ServiceImport` namespace before the attachment is accepted.

The hub workload identity requires least-privilege Azure actions for:

- AFD profiles and owned child resources;
- reading referenced WAF policies and writing AFD security-policy associations;
- diagnostic settings when enabled;
- reading public IPs and PLS resources;
- reading private endpoint connection state.

Approval permissions on member PLS resources are not required for the initial manual
approval mode. If controller approval is added, it receives a separate opt-in identity or
role assignment so profile management does not imply permission to approve private
network access.

## Observability

Metrics should include:

- reconcile count, latency, result, and Azure error class;
- eligible, pending, invalid, programmed, and deleting origins;
- Private Link requests by state and state age;
- WAF association readiness;
- Azure API throttling and retry-after duration;
- configuration-to-programmed latency.

Azure diagnostics should enable AFD access logs, health-probe logs, and WAF logs. Logs and
events correlate:

- Kubernetes object UID;
- AFD profile, endpoint, origin group, and origin names;
- Fleet member cluster name;
- Azure operation/correlation ID.

Alerts should cover zero healthy origins, prolonged approval pending, WAF association
loss, repeated authorization failures, quota exhaustion, and high configuration latency.

## Failure Matrix

| Failure | Controller behavior | Traffic behavior |
| --- | --- | --- |
| Public IP/FQDN pending | Keep member pending | Other healthy origins continue |
| Missing public bypass control | Reject member | No direct origin programming |
| PLS provisioning | Requeue with pending condition | No public fallback |
| Private endpoint approval pending | Emit transition event; poll slowly | Other established origins continue |
| Private endpoint rejected | Mark member invalid until intervention | Origin remains disabled |
| Origin certificate mismatch | Report TLS contract failure | AFD marks origin unhealthy |
| WAF policy missing/inaccessible | Do not enable route | Fail closed |
| One invalid member with `Partial` | Program valid members | Reduced capacity |
| One invalid member with `All` | Preserve last known good config | Existing traffic continues |
| Azure throttling | Honor `Retry-After`, exponential backoff | Existing config continues |
| Ownership conflict | Refuse adoption or deletion | Existing Azure resource unchanged |
| ServiceImport deleted | Disable, drain, delete owned origins/group | Route ceases after drain |

## Scale and Quotas

Before programming, the controller estimates profile resource consumption for origins,
origin groups, routes, domains, private endpoints, and WAF associations. It reports a
`QuotaExceeded` condition before partial creation where possible.

Private Link has an additional Front Door regional-cluster protection limit. Workloads
expecting high request rates should use multiple origins in different Private Link
regions and validate current Azure limits during capacity planning.

Controller implementation must:

- use shared informers and indexes instead of namespace scans;
- deduplicate Azure reads within a reconciliation;
- limit concurrent Azure mutations per profile;
- serialize changes that would temporarily mix public and private origins;
- use deterministic Azure names with hashes to stay within service limits.

## Rollout and Migration

1. Introduce APIs, internal transport fields, normalized model, and feature gates without
   Azure writes.
2. Enable read-only validation/status in test environments.
3. Implement public origin groups and routes against new profiles.
4. Add mandatory WAF association and public bypass conformance checks.
5. Implement private PLS discovery and manual approval workflow.
6. Run public and private canaries in isolated AFD profiles.
7. Add opt-in adoption of compatible prototype annotations through a one-time conversion
   tool; do not dual-write annotations and typed resources indefinitely.
8. Promote the APIs only after multi-region failure, deletion, and quota tests pass.

Migration uses dual-read, single-write reconciliation. An AFD origin group may be owned
by either legacy annotations or the typed backend attachment, never both. Duplicate
ownership is a hard conflict.

## Alternatives Considered

### Use annotations for all provider configuration

Rejected as the durable API. Annotations are expedient but lack schema, structured status,
field-level validation, discoverability, and conflict semantics. They may remain only as a
temporary compatibility input.

### Point GEP-4894 selectorRef at ServiceImport

Rejected. This would be a Fleet-specific reinterpretation, not upstream conformance. The
current selector is Pod-oriented and namespace-local.

### Materialize member Pods or EndpointSlices on the hub

Rejected. It expands hub cardinality, creates stale identity and readiness risks, and
misrepresents endpoints that AFD cannot reach directly.

### One origin for the logical ServiceImport

Rejected for direct Service mode. AFD needs independently addressable member origins for
health, latency, weight, priority, drain, and Private Link approval.

### Attach one backend policy directly to ServiceImport

Rejected as the initial API because origin groups, probes, connectivity, and traffic
settings are scoped to a particular AFD profile. A direct policy would force every
Gateway consuming the service to share one contract, make per-Gateway readiness status
ambiguous, and create many-to-many Azure cleanup ownership. The explicit attachment
keeps `ServiceImport` portable and permits deliberate reuse across profiles.

### Mix public and private members

Rejected within a backend/origin group because AFD prohibits the topology. An application
that requires both must use separate Gateways/routes/origin groups and an explicit traffic
migration procedure.

### Automatically approve every AFD Private Link request

Rejected. Approval is a network access grant. Automation must prove exact request
ownership before it can be safely enabled.

## Open Questions

- Should Gateway policy support only managed profiles in the first release, reducing
  ownership ambiguity?
- What stable Azure metadata identifies an AFD private endpoint request strongly enough
  for safe automatic approval?
- Should the member agent only observe PLS configuration, or validate security defaults
  such as restrictive visibility?
- Which resource reports AFD probe health when Azure exposes it asynchronously?
- How should custom-domain certificate ownership integrate with the existing
  `AzureFrontDoorCertificate` design?
- Should public bypass validation be mandatory admission, observed status, or an external
  conformance check when `X-Azure-FDID` enforcement is application-owned?
- What is the exact API for per-member priority overrides without overloading the existing
  ServiceExport weight annotation?

## References

- [GEP-4894: Backend API for Gateway API](https://gateway-api.sigs.k8s.io/geps/gep-4894/)
- [Gateway API PR 5158](https://github.com/kubernetes-sigs/gateway-api/pull/5158)
- [Secure traffic to Azure Front Door origins](https://learn.microsoft.com/azure/frontdoor/origin-security)
- [Origins and origin groups in Azure Front Door](https://learn.microsoft.com/azure/frontdoor/origin)
- [Secure your origin with Private Link](https://learn.microsoft.com/azure/frontdoor/private-link)
- [Connect AFD to an internal load balancer with Private Link](https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer)
- [Use an internal load balancer with AKS](https://learn.microsoft.com/azure/aks/internal-lb)
- [Configure a public Standard Load Balancer in AKS](https://learn.microsoft.com/azure/aks/configure-load-balancer-standard)
- [WAF on Azure Front Door](https://learn.microsoft.com/azure/web-application-firewall/afds/afds-overview)
- [Azure Front Door Manager and security policies](https://learn.microsoft.com/azure/frontdoor/manager)
- [SFI NS 2.5.3 KPI](https://eng.ms/docs/initiatives/project-standard/standards-categories/sc-networking/ddos/sfi-ns/sfi-ns253-kpi)
- [Application DDoS Standard](https://eng.ms/docs/initiatives/project-standard/standards-categories/sc-networking/ddos/ads/index)
- `docs/design/gep-4894-backend-evaluation.md`
- `api/v1alpha1/serviceimport_types.go`
- `api/v1alpha1/internalserviceexport_types.go`
- `pkg/controllers/member/serviceexport/controller.go`
- `pkg/controllers/hub/serviceimport/controller.go`
- `pkg/controllers/hub/trafficmanagerbackend/controller.go`
- `pkg/controllers/hub/trafficmanagerprofile/controller.go`
