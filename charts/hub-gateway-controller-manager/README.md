# Hub Gateway Controller Manager

This independent chart installs the Fleet hub Gateway manager. **AFD is disabled
by default.** Explicitly enabling it now connects the complete private HTTP
provider: create owned disabled Premium resources, wait for asynchronous manual
Private Link approval, activate the route before the endpoint, maintain the graph,
and suspend it on confirmed withdrawal/drift. Only current complete enabled-graph
convergence publishes the observed Azure hostname and Gateway/listener
`Programmed=True`. Cleanup deletes only the verified owned profile.

**Enabled mode is no longer observation-only: it performs AFD writes.** Controller
code is validated offline; live Azure/identity, approval, traffic, Host/probe,
withdrawal and cleanup verification remain deferred. No successful live proof
or production-readiness claim follows from this wiring.

The intended product is **Zero-Public-Exposure Ingress**: AFD Premium reaches
member internal load balancers only through Private Link. The client-facing AFD
edge remains public. Installing this chart does not establish a working ingress
datapath or prove private connectivity.

## Select an image explicitly

Both `image.repository` and `image.tag` are required. Select a release whose
publication you have verified; source wiring does not prove an image exists.
Gateway uses the same delivery path as the other controllers: `make image`
includes its local build and `make push` includes registry publication.
The existing GHCR workflow invokes that push bundle; the existing MCR workflow
supplies amd64/arm64 tags and combines their manifests using buildx imagetools.
Historical releases that did not publish Gateway skip its MCR manifest steps.
No additional registry or release workflow is introduced.

For an isolated local build, use
`make image-hub-gateway-controller-manager REGISTRY=<registry> TAG=<tag>` in a
supported Docker/buildx environment. The dedicated wrapper remains local even
though the low-level `docker-build-hub-gateway-controller-manager` target now
honors the same `OUTPUT_TYPE` as the other components. Actual builds, pushes and
multiarchitecture execution still require the corresponding CI/registry proof;
the chart assumes no published image or tag.

Set `GATEWAY_IMAGE_REPOSITORY` and `GATEWAY_IMAGE_TAG` to that accessible image.
Render first, without contacting a Kubernetes or Azure API:

```bash
helm template hub-gateway ./charts/hub-gateway-controller-manager \
  --namespace fleet-system \
  --set-string image.repository="${GATEWAY_IMAGE_REPOSITORY}" \
  --set-string image.tag="${GATEWAY_IMAGE_TAG}"
```

To install the disabled manager:

```bash
helm upgrade --install hub-gateway ./charts/hub-gateway-controller-manager \
  --namespace fleet-system --create-namespace \
  --set-string image.repository="${GATEWAY_IMAGE_REPOSITORY}" \
  --set-string image.tag="${GATEWAY_IMAGE_TAG}"
```

Disabled mode creates only a Deployment, ServiceAccount and namespaced
leader-election Role/RoleBinding. It mounts no Azure configuration, creates no
GatewayClass or cluster RBAC, and does not initialize Gateway/Fleet informers.
It needs neither Azure credentials nor Gateway API/Fleet CRDs. Configuring Azure
values alone does not enable the feature.

## Enabled installation prerequisites

Before enabling AFD, the platform operator must:

1. Install Gateway API v1.2.1 and the current Fleet networking CRDs on the hub,
   including the updated InternalServiceExport private-origin schema.
2. Provision the AFD resource group and an identity supported by the existing
   Azure cloud-config authentication path. Provide its subscription/authentication
   configuration in an existing Secret in the release namespace. The chart never
   generates a Secret or accepts embedded credentials.
3. Grant appropriate Azure access in the configured scope: profile/endpoint/
   origin-group/origin/route read and lifecycle writes, scoped stale-origin and
   owned-profile deletion, and list/read access to profile custom-domain/rule-set/
   security-policy/secret collections for unsupported-resource checks. The
   controller neither mutates those unsupported collections nor approves PLS
   connections, creates PLS resources or manages external WAF/Key Vault assets.
   Kubernetes RBAC does not grant Azure access; credential construction is not an
   authentication/authorization proof. Use an exclusively controller-managed
   profile; sequential ARM reads do not exclude competing external writers.
   The hub identity also needs `Microsoft.Network/privateLinkServices/read` on
   each linked member PLS. AFD resource-group permissions do not cover those
   linked resources. An authorized platform owner can grant Reader at each
   actual PLS resource scope once it exists, before origin provisioning; the
   chart does not create these Azure role assignments. This is distinct from
   member discovery permissions and does not authorize connection approval.
4. Enable member `enablePrivateLinkOriginDiscovery` explicitly, independently of
   Traffic Manager, with its required Azure read permissions. The corresponding
   binary flag is `--enable-private-link-origin-discovery=true`; see the existing
   [member discovery guide](../member-net-controller-manager/README.md#private-origin-discovery-for-zero-public-exposure-ingress).
   This hub chart does not change member configuration.

For example, using an existing `platform-azure` Secret whose `azure.json` key
contains the cloud configuration:

```bash
helm upgrade --install hub-gateway ./charts/hub-gateway-controller-manager \
  --namespace fleet-system --create-namespace \
  --set-string image.repository="${GATEWAY_IMAGE_REPOSITORY}" \
  --set-string image.tag="${GATEWAY_IMAGE_TAG}" \
  --set afd.enabled=true \
  --set-string afd.resourceGroup="${AFD_RESOURCE_GROUP}" \
  --set-string afd.cloudConfigSecret.name=platform-azure
```

The selected Secret key is mounted read-only as
`/etc/kubernetes/provider/azure.json`. No Kubernetes Secret API permission is
granted. To reuse a platform GatewayClass, set `gatewayClass.create=false`.
The default created class is `azure-fleet-afd`, controlled by
`networking.fleet.azure.com/afd`; it is created only when AFD is enabled.

Only one release should manage that shared class/controller identity in a hub.
The chart does not install CRDs, Gateways, HTTPRoutes, Services or Ingresses.
It never writes controller-owned ServiceImports or InternalServiceExports.
Customers need no hub ServiceImport annotations or write access.

**AFD Private Link approval is asynchronous.** Authorized PLS owners must
approve connections and wait for convergence when provisioning is enabled.
Approval automation with the AFD/PLS teams remains an external action
item, not a capability of this chart.

## Identity and operational settings

| Value | Default | Purpose |
|---|---|---|
| `afd.enabled` | `false` | Explicitly enable Gateway/Fleet controllers and cluster RBAC |
| `afd.resourceGroup` | Empty | Required when enabled |
| `afd.cloudConfigSecret.name` | Empty | Existing release-namespace Secret; required when enabled |
| `afd.cloudConfigSecret.key` | `azure.json` | Selected configuration key; required when enabled |
| `gatewayClass.create` / `gatewayClass.name` | `true` / `azure-fleet-afd` | Enabled-only optional class |
| `serviceAccount.create` / `serviceAccount.name` | `true` / Generated | Set create=false and supply a name to reuse an existing account |
| `serviceAccount.annotations` | `{}` | Annotations on the chart-created account |
| `podLabels` / `podAnnotations` | `{}` | Platform projection/metadata; selector labels cannot be overridden |
| `leaderElection` | `true` | Namespaced leases and events; keep enabled for multiple replicas |
| `replicaCount` / `logVerbosity` | `1` / `2` | Deployment replica count and manager verbosity |
| `resources` | See values.yaml | CPU/memory requests and limits |
| `nodeSelector` / `tolerations` / `affinity` | Empty | Scheduling policy |

For workload identity, `serviceAccount.annotations` and `podLabels` expose the
usual projection plumbing. For example, a platform may use
`azure.workload.identity/client-id` on its account and
`azure.workload.identity/use: "true"` on pods. These settings alone do not configure
cloud-config authentication, federation, a webhook or Azure role assignments.
Annotate an existing account outside this chart when reusing it.

The release namespace controls all namespaced resources and leader election;
use the reserved `fleet-system` namespace. The chart creates no Namespace.
The image runs as UID/GID 65532 with a read-only root filesystem, no privilege
escalation, dropped capabilities and RuntimeDefault seccomp.

`/healthz` and `/readyz` are exposed on port 8083; metrics use 8082. No Service is
created. Enabled readiness requires initial synchronization of all seven watched
informers, not leadership, Azure uptime or Private Link approval. It does not
prove continuing API freshness or dataplane readiness.

## Origin-observation diagnostics

Enabled mode emits Gateway events in the Gateway's own namespace using the
existing client-go recorder. The chart grants core Event create/patch permissions
only when AFD is enabled. Events follow successful, current observation-status
transitions, not failed status-patch attempts or discarded observations.
Identical polls do not repeat transition events; the standard recorder also
aggregates and rate-limits delivery. Events are best-effort diagnostics, not a
durable audit trail or proof that an approval was performed.

| Reason | Type | Meaning |
|---|---|---|
| `AFDPrivateLinkApprovalRequired` | Normal | An authorized PLS owner must approve asynchronously; automation is not available |
| `AFDOriginObservationFailed` | Warning | Observation failed; inspect Gateway status and controller logs |
| `AFDPrivateOriginsNotReady` | Warning | Observed rejected/disconnected/timed-out approval, failed provisioning or configuration drift |
| `AFDPrivateOriginsConverged` | Normal | Expected origins are approved/deployed; **not** full-graph programming or ingress readiness |

Event text does not copy raw SDK errors, credentials or resource IDs. Ordinary
missing/provisioning/unknown observations remain in status without a warning
event from this slice.

The existing metrics endpoint exposes:

| Metric | Meaning |
|---|---|
| `fleet_networking_gateway_afd_origin_observations_total` | Counter of completed logical origin-observer calls |
| `fleet_networking_gateway_afd_origin_observation_duration_seconds` | Histogram of those calls' durations |

Both use only `outcome="success|error|throttled"`, with no resource identity or
error-text labels. `throttled` means a final returned Azure 429; SDK HTTP requests
and internal retries are not counted individually. A successful read may report
missing/unapproved origins or become stale before status publication. Skipped
providers and failed Kubernetes identity prerequisites produce no call samples,
and status retries do not repeat them.

These observation metrics do not measure current origin inventory, approval-wait
time, cleanup/staging operations, origin health or traffic.

## Owned-profile cleanup diagnostics

The existing finalization path emits these best-effort Gateway events using the
same recorder and enabled Event permissions:

| Reason | Type | Meaning |
|---|---|---|
| `AFDProfileCleanupFailed` | Warning | Provider cleanup failed; the finalizer remains for retry; inspect controller logs and Azure permissions/availability |
| `AFDProfileCleanupOwnershipConflict` | Warning | Profile ownership verification failed; a platform operator must investigate rather than remove the finalizer to bypass cleanup |
| `AFDProfileCleanupComplete` | Normal | The profile was observed absent and this reconcile successfully removed the matching deleting Gateway's cleanup finalizer |

Completion does not claim this controller submitted a deletion: the profile may
already have been absent. Pending deletion, failed metadata patches, a missing or
replaced Gateway, and finalizer removal by another writer emit no completion
event. Repeated pending polls emit no events; failure events use native recorder
aggregation/rate limiting. Fixed event messages do not copy raw SDK payloads or
resource IDs. Events are not a durable cleanup audit.

| Metric | Meaning |
|---|---|
| `fleet_networking_gateway_afd_profile_cleanup_calls_total` | Counter of completed logical profile-cleanup calls |
| `fleet_networking_gateway_afd_profile_cleanup_duration_seconds` | Histogram of those calls' durations |

Both have only `outcome="pending|absent|ownership_conflict|throttled|error"`.
`pending` means the provider has not yet observed absence, including deletion in
progress. `absent` means an absence observation, not a deletion count or persisted
Kubernetes finalization. Wrapped ownership errors and final returned Azure 429s
are classified explicitly; errors take precedence over an absence result.
Samples include internal SDK retry time, not individual HTTP requests or total
deletion wait time. Metadata conflict retries do not repeat the provider call
or its samples. Missing providers/readers, missing trusted hub identity and
Gateways without the cleanup finalizer produce no call samples.

Diagnostics do not enable provisioning or alter cleanup ownership, finalizer
retention or retry behavior.

## Graph-operation diagnostics

The enabled runtime's complete HTTP lifecycle and endpoint-suspension calls have
logical-call diagnostics. Disabled-only staging remains a separate regression
seam and is never injected alongside the complete provisioner.

| Metric | Meaning |
|---|---|
| `fleet_networking_gateway_afd_graph_operation_calls_total` | Counter of completed logical graph-operation calls |
| `fleet_networking_gateway_afd_graph_operation_duration_seconds` | Histogram of those calls' durations |

Both use only fixed `operation` and `outcome` labels:

- `operation="reconcile_http|suspend_empty|suspend_denied|suspend_invalid"` distinguishes
  complete HTTP reconciliation and confirmed empty, denied or unprogrammable
  intent suspension. `stage_disabled_graph` is retained for the disabled-only seam.
- `outcome="success|error|throttled|ownership_conflict"` classifies the final
  returned error, including wrapped Azure 429/ownership errors.

`success` means only a nil-error logical return, including pending, absent or
incomplete resource observations. It does not prove inventory convergence,
traffic readiness, or successful revocation/withdrawal enforcement. Durations
include internal SDK work/retries, not individual ARM request counts or total
approval/provisioning wait time. Failed prerequisites and skipped calls produce
no samples. A completed call is still counted if later intent changes or status
publication fails; status conflict retries do not repeat its call or samples.

| Reason | Type | Meaning |
|---|---|---|
| `AFDDisabledGraphStagingFailed` | Warning | A staging call failed; inspect controller logs; no new ingress readiness is established |
| `AFDHTTPProvisioningFailed` | Warning | A complete HTTP lifecycle call failed; no new ingress readiness is established |
| `AFDEndpointSuspensionFailed` | Warning | A suspension call failed; inspect controller logs; this attempt does not establish withdrawal enforcement |
| `AFDGraphOperationOwnershipConflict` | Warning | Ownership verification failed; a platform operator must inspect ownership and controller logs |

These best-effort events describe failed calls, not persisted status transitions.
They may appear even when the later status patch fails or intent becomes stale.
Messages omit raw SDK/resource payloads; repeated failures use native recorder
aggregation/rate limiting. Successful/pending calls emit no attempt-warning event. Separately,
`AFDPrivateLinkApprovalRequired` and `AFDHTTPProgrammed` Normal Events follow
persisted current status transitions, not failed status patches or stale results.
Programmed means observed ARM convergence, not measured application health or a
successful withdrawal audit.

Approval-wait telemetry and deployed Event/RBAC, Azure operation and enforcement
proof remain separate work. Ownership/authorization checks and finalizer retention
remain mandatory. Failures clear published readiness; only affirmative uncached
withdrawal evidence authorizes suspension, not transient Kubernetes read errors.

The [Gateway integration suite](../../test/README.md#gateway-api-server-integration)
has verified local API-server persistence of approval/convergence/cleanup Events
and informer-driven observation/finalization with fake providers. It runs with an
envtest admin identity, so it does not validate this chart's deployed RBAC or
identity configuration, recorder rate limits or Azure enforcement.

## Upgrade, disable and uninstall safely

Do not disable AFD or remove the controller while Gateways still require owned
resource cleanup. Delete the relevant Gateways first and allow the controller to
finish owned-profile cleanup and release Gateway/GatewayClass finalizers. Confirm
the Azure cleanup outcome before uninstalling or deleting its identity/config.
The chart has no cleanup hooks and cannot finish finalizers after removal.
Back up platform-owned configuration and review value changes on upgrade; never
put credentials in Helm values or command-line arguments.

## Local render checks

With Go and Helm v3.17.3 available:

```bash
go test -count=1 -tags=helm ./test/helm
```

This opt-in suite runs real Helm rendering and strict lint, including disabled
isolation, required configuration, identity reuse, Secret-key mapping, namespace
consistency and exact RBAC. Ordinary Go tests do not require Helm. Render checks
are not live API-server authorization, informer, image-execution or private HTTP
proof. See the [configuration guide](../../docs/howtos/gateway-api-afd-configuration.md)
for the intended product contract and current runtime limitations.
