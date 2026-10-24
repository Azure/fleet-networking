# Local Developer E2E Guide

The E2E suite provisions one hub AKS cluster and two member AKS clusters, installs the Fleet
networking components, and runs the Ginkgo tests under `test/e2e`.

## Prerequisites

- [Go](https://go.dev/dl/) at the version declared in `go.mod`
- [Azure CLI](https://learn.microsoft.com/cli/azure/install-azure-cli)
- [Docker](https://docs.docker.com/engine/install/)
- [Helm](https://helm.sh/docs/intro/install/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [`jq`](https://jqlang.github.io/jq/download/)
- [`yq`](https://github.com/mikefarah/yq#install)
- Bash and GNU Make
- An Azure subscription in which the signed-in identity has the Owner role

Sign in and select the subscription before starting:

```bash
az login
export AZURE_SUBSCRIPTION_ID=<YOUR-SUBSCRIPTION-ID>
az account set --subscription "${AZURE_SUBSCRIPTION_ID}"
```

The setup builds and pushes test images, creates an Azure Container Registry and AKS clusters, and
assigns Azure roles. Use a test subscription rather than a production subscription.

## Choose an E2E scenario

All scenarios use the same setup, test, and cleanup targets. The environment variables determine
the cluster network topology and whether Traffic Manager reconciliation is enabled.

### Baseline Fleet networking

Use this scenario for ServiceExport, ServiceImport, MultiClusterService, and networking behavior
without Azure Traffic Manager:

```bash
export AZURE_RESOURCE_GROUP=<YOUR-RESOURCE-GROUP-NAME>
export AZURE_SUBSCRIPTION_ID=<YOUR-SUBSCRIPTION-ID>
export AZURE_NETWORK_SETTING=shared-vnet
export ENABLE_TRAFFIC_MANAGER=false
```

`AZURE_NETWORK_SETTING` supports:

- `shared-vnet`
- `dynamic-ip-allocation`
- `peered-vnet`
- `unsupported`

The scripts under `test/scripts` define each topology. `unsupported` verifies behavior when member
clusters do not have supported network connectivity.

### Azure Traffic Manager

Traffic Manager E2E provisions and validates real Azure Traffic Manager resources. It is supported
only with the shared-VNet topology:

```bash
export AZURE_RESOURCE_GROUP=<YOUR-RESOURCE-GROUP-NAME>
export AZURE_SUBSCRIPTION_ID=<YOUR-SUBSCRIPTION-ID>
export AZURE_NETWORK_SETTING=shared-vnet
export ENABLE_TRAFFIC_MANAGER=true
```

The setup configures the additional AKS identities, Azure role assignments, and controller feature
flags required by `test/e2e/traffic_manager_test.go`. Tests in that file skip when
`ENABLE_TRAFFIC_MANAGER` is not `true`.

### Azure Front Door and Gateway API

The separate Gateway manager has an opt-in local image build target. It requires
make, Docker/buildx and a Linux container builder:

```bash
make image-hub-gateway-controller-manager REGISTRY=fleet-networking TAG=dev
docker run --rm --network=none fleet-networking/hub-gateway-controller-manager:dev --help
```

The image uses the existing Azure Linux distroless/nonroot policy and compiles
the complete manager package. AFD remains disabled by default. Ordinary
`make image`/`make push` bundles include Gateway, reusing the existing GHCR and
MCR publication paths. MCR uses the existing architecture-suffix/manifest pattern
and skips Gateway manifests for older releases that did not publish it.
No build/push installs or enables the controller.

The unit/integration workflow checks bundle membership, output modes and
architecture/tag overrides with GNU make dry runs. Native amd64 and arm64 jobs
then build and execute the separate Linux Gateway image, verify its architecture
and UID/GID, and run its entrypoint read-only without network access. A second
smoke starts the default-disabled manager with an unreachable loopback-only
synthetic kubeconfig and checks actual `/healthz` and `/readyz` responses through
an ephemeral host-loopback port. No Azure credentials or real cluster are used.
Each job also verifies actual registry export, manifest retrieval, pull and
nonroot execution using a disposable loopback-only registry. Plain HTTP is
limited to that local registry; production publication retains its existing
registry authentication and TLS configuration. MCR publication verifies both
Linux architectures in the combined Gateway manifest.

Current amd64 image build, restricted execution, disabled-manager probes and
local-registry delivery have passed under WSL, as have the repository-pinned full
lint and Helm contracts. Native arm64 CI execution and production-registry
publication remain unrun for this revision. Dry-run checks do not build or push
images, and none of these checks prove enabled informer startup or private HTTP
delivery.

The independent [Gateway chart](../charts/hub-gateway-controller-manager/README.md)
is disabled by default and requires an explicitly selected image. Its opt-in
render contract suite needs only Go and Helm v3.17.3, not Docker, a cluster or
Azure access:

```bash
go test -count=1 -tags=helm ./test/helm
```

The suite runs real Helm rendering and strict lint for disabled isolation,
enabled Secret/class/RBAC configuration, namespace consistency and identity
projection/reuse. The read-only `gateway-chart` CI job runs this command
separately. This does not install a controller or prove live RBAC, informers or
private HTTP. Ordinary Go tests do not require Helm.

Current origin-observation diagnostics reuse the manager's client-go recorder and
Prometheus registry. Package regressions cover post-status transition events,
unchanged/stale/overridden observations, failed/conflicting status patches,
fixed-label call count/duration, wrapped throttling and skipped calls:

```bash
go test -count=1 -skip '^TestAPIs$' ./pkg/controllers/hub/gateway ./cmd/hub-gateway-controller-manager
```

See the [event/metric reference](../charts/hub-gateway-controller-manager/README.md#origin-observation-diagnostics).
Owned-profile cleanup regressions additionally cover pending/absent/error/
ownership-conflict/throttled outcomes, skipped prerequisites, duration seconds,
unchanged pending polls, sanitized events, finalizer conflicts/failures and
missing/replaced Gateways. Completion requires persisted removal; metadata
retries do not repeat the provider call or its samples. Graph-operation
regressions cover disabled staging, empty-intent suspension and denied-reference
suspension: fixed labels, wrapped errors, sanitized attempt warnings, skipped
prerequisites, stale results and failed/conflicting status patches. A nil-error
call is not readiness or enforcement proof. Repeat these diagnostics without
starting manager setup multiple times:

```bash
go test -count=3 -run 'TestGatewayFinalizesOwnedProfileWithoutClass|TestProfileCleanup|TestOriginObservation|TestGraphOperation' ./pkg/controllers/hub/gateway
```

See the [cleanup event/metric reference](../charts/hub-gateway-controller-manager/README.md#owned-profile-cleanup-diagnostics).
See the [graph-operation event/metric reference](../charts/hub-gateway-controller-manager/README.md#graph-operation-diagnostics);
enabled startup now injects the complete HTTP lifecycle, not disabled-only staging.
Fake-recorder/registry tests and Helm RBAC rendering do not prove live API-server
event delivery, rate limiting or Azure/traffic behavior.

Complete HTTP controller unit tests cover ownership before provisioning, all
lifecycle states, stable enabled maintenance, exact Gateway/listener readiness
and address publication, status-conflict no-replay, stale cached/uncached
identity/authorization evidence, failure readiness clearing, withdrawal and
deletion priority. Approval/readiness Events require persisted current status.
These use fake Kubernetes clients/providers alongside separate SDK transport
regressions, not real Azure or installed-controller traffic evidence.

#### Gateway API-server integration

`pkg/controllers/hub/gateway` now has a Ginkgo `TestAPIs` suite using real
envtest API-server/etcd, the actual three Gateway controllers, caches/indexes and
native EventRecorder. It installs repository Fleet CRDs and the standard CRDs
from the Gateway API version selected by `go.mod` through a fixed read-only Go
module lookup. It does not copy schemas, call Azure or inject the SDK graph
stager.

Use Linux or WSL with Go available. Native Windows assets can start, but this
controller-runtime version cannot complete its signal-based teardown on Windows.
Use the existing Makefile-selected setup tool/asset versions for a focused run:

```bash
export KUBEBUILDER_ASSETS="$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.22 use 1.33.0 -p path)"
go test -race -count=1 -run '^(TestAPIs|TestIntegrationObservationHold)$' ./pkg/controllers/hub/gateway
go test -count=1 ./pkg/controllers/hub/gateway... ./cmd/hub-gateway-controller-manager
```

The existing `make test` CI path includes this suite. Missing assets/setup failures
are explicit failures, not silent skips. Without assets, use the unit-only
`-skip '^TestAPIs$'` command above. Run repeated integration checks in separate
`go test` processes rather than `-count>1`, preserving controller-runtime's
controller-name validation.

The lifecycle scenario verifies pending approval -> observed origin convergence
by changing only thread-safe fake provider state, without Kubernetes edits or
direct Reconcile calls. It retains the production 30-second-plus-jitter polling
cadence, invalidates/restores discovery through the actual export watch, and
checks current unprogrammed/address-free status. Approval/convergence/cleanup
Events are persisted through the real API server. A test-seeded ownership
finalizer remains while cleanup is pending and is released only after a later
absence observation, using server-assigned hub/Gateway/source-Service identities.
Its route lives in a different namespace with explicit AllowedRoutes and a
ReferenceGrant. Deleting the grant preserves current `Accepted=True` attachment
but sets `ResolvedRefs=False/RefNotPermitted`, replaces the Gateway convergence
message with denial and stops observer calls after denied state settles. Removing
the target does not override the authorization denial. Recreating the grant while
the target is absent exposes `BackendNotFound`; recreating the ServiceImport and its platform
status restores resolution/observation through dependency watches. Recreated
grant/backend UIDs differ, while Gateway/HTTPRoute specs and generations remain
unchanged. The observer does not enforce Azure withdrawal, and these settled
cache assertions are not a transactional authorization guarantee.
The lifecycle also recreates the fixture source Service and transports its new
server-assigned UID, then replaces the exported PLS/frontend identity. Each
export spec change first retains stale unconflicted status: the Gateway rejects
that generation and observer calls stop after rejection settles, while route
references remain valid. Updating conflict status to the current generation
restores observation of the exact replacement origin through the export watch.
Gateway/HTTPRoute specs and generations remain unchanged. This proves imported
model replacement, not member discovery/transport, independent live Service/PLS
verification or Azure origin replacement/withdrawal.
After convergence, provider-only `Disconnected` and `Drifted` observations
replace stale convergence diagnostics through ordinary polling, persist matching
warning Events and recover after the fake returns `Ready`. Recovery requires
an increased persisted convergence Event occurrence count, not merely the
historical Event's existence. Gateway/HTTPRoute intent and backend objects remain
unchanged; route references remain valid and Gateway status stays unprogrammed
and address-free at each observed transition. The fake builds correlated origin
references/targets with the existing backend planner. This does not prove SDK
classification, real approval withdrawal, configuration repair or traffic safety.
An in-flight scenario holds a captured `Rejected` observation, changes the valid
exported private frontend and refreshes current conflict status, then waits until
the controller's own cache sees the exact replacement spec/status before release.
Subsequent observations return `Ready`: the replacement origin and a fresh
convergence Event occurrence must appear, without a stale rejection warning.
Gateway/HTTPRoute intent remains unchanged and current status stays unprogrammed
and address-free. The provider releases its mutex before the one-shot hold waits,
honors cancellation and has idempotent cleanup; table-driven unit tests cover release,
cancellation, captured-state stability and rejection of a second armed hold.
This synchronized case does not prove transactional cache freshness, concurrent
writer exclusion, actual ARM cancellation or Azure mutation safety.
The spec takes roughly four to five minutes including startup and production
polling intervals.

Focused, race-enabled and full Gateway/manager runs passed locally under WSL, with normal
manager/control-plane teardown and no remaining test-owned processes. This is
local API/cache/Event/finalizer evidence with fake providers and an envtest admin
identity, not deployed Helm RBAC/identity, actual AFD approval/provisioning,
recorder rate limits, `/readyz` HTTP integration or private-HTTP enforcement proof.
Restart, weight/member changes and actual production-provider work or
drift/revocation scenarios remain open; grant-status, fake-provider state or
in-flight-read coverage is not Azure traffic revocation proof.

The AFD-oriented E2E scenario has a baseline API-contract entry and an opt-in controller entry.
Setup installs the Gateway API CRDs in the hub. The baseline entry in
`test/e2e/gateway_api_test.go` verifies that the API server accepts and round-trips:

- `GatewayClass`, `Gateway`, and `HTTPRoute`
- Same-namespace Fleet `ServiceImport` backends
- Cross-namespace Fleet `ServiceImport` references and their `ReferenceGrant` objects
- The Premium Gateway annotation and exact backend group, kind, port, and weight,
  without customer annotations on generated ServiceImports

Use the baseline environment:

```bash
export AZURE_RESOURCE_GROUP=<YOUR-RESOURCE-GROUP-NAME>
export AZURE_SUBSCRIPTION_ID=<YOUR-SUBSCRIPTION-ID>
export AZURE_NETWORK_SETTING=shared-vnet
export ENABLE_TRAFFIC_MANAGER=false
```

The setup does not deploy the separate Gateway manager. To enable the controller entry, first
start the current manager against the test hub in another terminal. Set `AFD_CLOUD_CONFIG` to a
valid Azure cloud-config JSON file containing the test subscription ID and
Azure authentication settings supported by the existing hub controller. Enabled
startup initializes authenticated AFD cleanup and origin-observation clients; credential construction
alone does not prove token acquisition or Azure permissions.
**The following command now enables AFD lifecycle writes for supported Gateway
intent**, not just observation. Use only an explicitly authorized scope:

```bash
kubectl config use-context "${HUB_CLUSTER:-hub}-admin"
go run ./cmd/hub-gateway-controller-manager \
  --enable-afd \
  --cloud-config="${AFD_CLOUD_CONFIG}" \
  --afd-resource-group="${AZURE_RESOURCE_GROUP}" \
  --metrics-bind-address=0 \
  --health-probe-bind-address=0
```

Then set this in the terminal that runs the tests:

```bash
export ENABLE_GATEWAY_API=true
```

The controller entry waits for current-generation GatewayClass/Gateway acceptance and HTTPRoute
reference conditions. It checks missing-backend recovery and `ReferenceGrant` deletion/recreation
without editing the cross-namespace route. Gateways and listeners must remain
`Programmed=False`, with no fabricated Azure address or Azure cleanup finalizer.
If the flag is absent, this entry is explicitly skipped while the baseline entry still runs.

These tests use synthetic hub exports, not real private origins. They verify Kubernetes controller
status and dependency watches, not Azure resource provisioning, connection approval, or traffic.
These synthetic hostname-restricted fixtures remain outside the programmable
execution subset, so they create no AFD resources or Gateway cleanup finalizers.
For supported real private intent, the enabled manager now provisions resources
and persists cleanup ownership before doing so.
Ownership-preparation unit tests separately cover metadata-only finalizer
persistence/re-observation with uncached UID, class, route, namespace policy,
grant and backend rechecks. They reject cache-only field selectors and exercise
metadata conflicts, lost ownership, changed intent and read/write failures.
The helper itself makes no Azure requests. Injected-stager controller tests now
exercise it through reconciliation, asserting persisted ownership before staging,
no stage replay on metadata/status conflicts, nil-injection skips, deletion
priority, explicit errors and current-intent-only pending status. Neither an
observed disabled graph nor an SDK hostname publishes an address or sets
`Programmed=True`. Production instead injects the complete HTTP lifecycle, which
can publish readiness only after observed complete enabled-graph convergence.
Injected withdrawal tests additionally cover complete empty effective intent for
already-owned Gateways: uncached emptiness/identity/finalizer checks, no metadata
ownership writes, status-conflict retries without replaying suspension, stale
cache refusal, reattachment, provider errors and retained cleanup metadata.
SDK suspension tests assert GET-only absence handling and disable-only PATCH,
fresh observations after submission, cancellation/error preservation and no
dependency on backend/route clients or a hostname. They do not prove live traffic
revocation or selective withdrawal.
All-denied ReferenceGrant tests cover deleting/absent grants, denied target
read avoidance even when the target is missing, stale cached denial, fresh grant
read errors, restored permission during suspension and no ownership acquisition
for never-owned Gateways. Mixed denied/permitted or denied/unresolved route sets
must return no candidate and never stage a partial graph, but a separate current
authorization-only proof suspends the whole already-owned endpoint. This
intentionally interrupts permitted peers too. Tests cover backend API failures
without blocking suspension or unprogrammed status, preserved read/provider/
status errors, other grant-read failures alongside a positive denial, stale
cached permission, permission restored before the operation, route/ownership
drift, ineligible routes, fresh proof failure and status conflicts without
replaying Azure. The uncached reader rejects cache-only field selectors. These
are fake-client/provider checks, not live traffic enforcement evidence.
Separate provider unit tests exercise real SDK profile/disabled-endpoint
GET/PUT/PATCH serialization through fake HTTPS transport, including ownership,
restart, cancellation, error preservation and observed convergence. That
root behavior is composed into the enabled complete lifecycle; these tests are
not live Azure evidence.
Backend provider tests additionally cover origin-group/private-origin SDK
GET/PUT/PATCH operations behind observed owned disabled roots, including
pending approval without starving later origins. Those operations are composed
into the enabled complete lifecycle; expected-backend convergence alone is not
route readiness or stale cleanup.
Route provider tests compose that staging with SDK disabled HTTP route
GET/PUT/PATCH assertions, requiring observed backend approval/convergence and
one shared budget/one write across dependencies. Routing drift is refused,
and successful submissions are not convergence. This disabled-stage coverage is
separate from full-lifecycle activation tests; neither proves live Azure behavior.
Composed graph tests additionally
enforce one shared deadline and at most one write across root/backend/route stages,
including disappearance of roots between stages and errors after initial root
convergence. Missing later-stage clients fail before root access.
After expected disabled-route convergence, composed staging reobserves owned
disabled roots and checks complete paginated endpoint/route/group/origin
membership. Tests cover empty first pages, unexpected later-page resources,
missing/duplicate/nil/malformed identities, casing and foreign-parent IDs,
fresh ownership/disabled-state refusal, collection 404/errors and cancellation.
For all four supported collections, missing/null arrays on either side of valid
records return zero staging result and stop on the ambiguous page. Explicit empty
first/final pages remain valid controls; continuation alone never proves absence.
Transport regressions keep expected GETs converged but change later collection
endpoint/route/group configuration or deployment, private-origin configuration,
or approval (including a second origin page). Conflicting reads return zero
staging result and stop without writes; pending approval retains the asynchronous
authorized PLS-owner action. Positive controls preserve unmanaged load-balancing
defaults, equivalent ARM casing and empty optional route collections.
Pending/submitted provisioning stages skip inventory; all requests retain the shared deadline
and one-write bound. `inventoryVerified=true` does not publish an address or set
`Programmed=True`. Additional tests require
profile custom-domain/rule-set/security-policy/secret collections to be explicitly
empty on every page. Nonempty/nil records, omitted/null arrays and later-page
403/404 failures return no staging result. Missing inventory clients fail before
any ARM request; these observations share the same operation budget. Enabled lifecycle
injection requires profile collection read/list access, not Key Vault or
external WAF/certificate mutation privileges. Feature support, automatic migration
and live traffic proof remain separate work.
Stale-origin transport tests cover member removal, source Service UID/PLS
replacement, deterministic one-at-a-time DELETE, failed/rejected private stale
bindings, SDK success/404 submissions and restart while deletion is in progress.
Every desired origin and other inventory must pass first; missing/denied desired
members, duplicate/foreign/public/malformed records and unsupported assets refuse
cleanup. Fresh candidate identity/configuration and owned disabled roots are
rechecked before mutation. Error/cancellation checks cover each extra request,
including after DELETE; no polling restarts the deadline or submits another write.
`staleOriginsPending` cannot become verified absence until a later complete
collection read, and controller status remains unprogrammed/address-free.
The enabled lifecycle needs scoped origin DELETE permission; the SDK has no
If-Match DELETE option, and these fakes do not prove replacement coexistence,
external-writer exclusion or live traffic enforcement. No other child or
external assets are pruned.
Manager unit tests separately cover initialization of all seven watched informer
kinds, each unsynced/stopped cache, transition to readiness after complete initial
synchronization, shutdown, initialization/registration failures and standby
readiness without leadership. HTTP probe checks use controller-runtime's
readiness handler with the manager's prefix stripping. Liveness remains healthy
while caches are unsynced; disabled mode requests no cache and loads no Azure
configuration. These tests use fake informer state, not live informer/API-server
evidence, ongoing cache-freshness proof or Azure/dataplane readiness checks.
For accepted, fully resolved nonempty private backend intent within the supported
execution subset (one unrestricted HTTP/80 listener, root PathPrefix `/`, one
logical backend port, no filters/WAF), the enabled controller persists cleanup
ownership, stages disabled Premium/private resources, waits for manual approval,
activates route then endpoint and verifies complete enabled graph inventory.
It rechecks every 30 seconds plus up to 20% jitter; every lifecycle provider call
has one shared 30-second deadline and at most one write. Startup/client construction
is lazy, but enabled reconciliation is not read-only. See the chart guide for
scoped Azure lifecycle/list permissions and uncached Kubernetes identity access.
Pending/submitted/incomplete/error states remain address-free and unprogrammed;
only fresh full convergence publishes the observed Azure hostname and current
Gateway/listener readiness. Confirmed empty, denied or unprogrammable intent
suspends the whole owned endpoint, while ambiguous Kubernetes reads authorize no
mutation. Approval automation with the AFD/PLS teams remains an action item.
For a deleting Gateway already bearing `networking.fleet.azure.com/afd-cleanup`, it can
read and delete the owned AFD profile in the configured subscription/resource group.
This requires Azure profile read/delete permissions and Kubernetes access to read the
`kube-system` Namespace UID and patch Gateway metadata. Cleanup verifies the full profile
ARM ID and hub/Gateway ownership tags and retains the finalizer until absence is observed.
The synthetic controller scenario has no ready private origin, so it does not
exercise these ARM operations. The separately opted-in private HTTP scenario
below prepares actual deployment/identity, approval, SDK, traffic and cleanup
assertions. Its live execution remains deferred.

#### Installed-controller private HTTP proof (prepared, not live-verified)

`gateway_private_http_test.go` adds one serial scenario labeled
`afd-private-http-proof`. It is prepared and unit-tested offline, **not evidence
of working Azure traffic**. The enabled manager now injects the complete provider;
live platform and installed-controller evidence remain deferred. Opt-in against
an old observation-only image must fail rather than report readiness.
The scenario does not install a controller, call the provisioning methods itself,
approve Private Link connections or edit generated ServiceImports.

Before any live run, obtain explicit authorization for the subscription/resource
groups, resource budget, lifetime, cluster changes and cleanup. Arrange an authorized
PLS owner who can approve the actual AFD requests during the run. Use the existing
shared-VNet E2E environment with `hub-admin`, `member-1-admin` and `member-2-admin`
contexts, working member discovery and ServiceExport/ServiceImport transport.
Rebuild and publish the existing sample-app image from this checkout: the proof
requires its optional run identity, request/Host echo and structured probe logs.
Image publication, environment setup and controller installation are separate
authorized operations, not performed by the proof.

The selected hub Gateway chart Deployment must run the candidate controller image,
an explicit service account, `--enable-afd=true` and
`--afd-resource-group=<AZURE_RESOURCE_GROUP>`. Its Azure identity must have the
documented AFD lifecycle permissions; the test runner needs read access to AFD
and member PLS/load-balancer frontend resources, Kubernetes namespace/workload/
Gateway write access, and controller-object/pod-log read access.
Do not run a competing local Gateway manager or other writers
against the owned profile. ARM observations are sequential, not transactional.
The scenario checks the Deployment rollout, ReplicaSet/Pod ownership and actual
running image IDs rather than assuming a local process proves installed behavior.

For a future authorized run with the updated controller installed, to establish
the still-unverified platform and installed-controller evidence:

```bash
export AZURE_SUBSCRIPTION_ID=<AUTHORIZED-SUBSCRIPTION-ID>
export AZURE_RESOURCE_GROUP=<AUTHORIZED-E2E-RESOURCE-GROUP>
export AZURE_NETWORK_SETTING=shared-vnet
export ENABLE_TRAFFIC_MANAGER=false
export ENABLE_GATEWAY_API=true
export ENABLE_AFD_PRIVATE_HTTP_PROOF=true
export AFD_PROOF_CONTROLLER_NAMESPACE=<INSTALLED-DEPLOYMENT-NAMESPACE>
export AFD_PROOF_CONTROLLER_DEPLOYMENT=<INSTALLED-DEPLOYMENT-NAME>
export AFD_PROOF_CONTROLLER_IMAGE=<EXACT-IMAGE-IN-DEPLOYMENT>
AFD_PROOF_REPORT="${TMPDIR:-/tmp}/afd-private-http-proof-$(date -u +%Y%m%dT%H%M%SZ).json"
go test -count=1 -tags=e2e -run '^TestE2E$' -timeout=120m ./test/e2e \
  -args -ginkgo.v -ginkgo.label-filter=afd-private-http-proof \
  -ginkgo.json-report="${AFD_PROOF_REPORT}"
```

Wait for **"Pending approval checkpoint captured"** and its correlated profile/
PLS/origin targets before the PLS owner approves only those requests. Approval is
asynchronous; the scenario never automates it. The approval/activation window is
15 minutes, ordinary stages allow 10 minutes, the spec allows 45 minutes, and
individual cleanup nodes allow up to 12 minutes. The focused command's longer
timeout includes suite startup and deferred cleanup.

A successful run must observe all of the following, not just `Programmed=True`:

- Fresh namespaces, real workloads/member exports and Services created with internal
  load-balancer/PLS annotations from the outset; actual discovered Service UIDs,
  Azure PLS bindings and private subnet-bound frontends without public IP resources.
- An initially absent owned profile, all real member origins pending approval,
  then current Gateway/listener/route status and the complete owned enabled
  Premium/private graph with the exact published hostname.
- Fresh HTTP 200 responses from both members, matching run ID, unique request nonce
  and incoming Host, with AFD edge evidence; actual member logs of root GET
  `Edge Health Probe` requests, recording the observed probe Host. Configuration
  separately requires the platform's HTTP GET `/`/30-second probe policy.
- Route removal followed by observed deployed endpoint disablement and affirmative
  negative edge responses sustained for a minute. Transport failures/timeouts,
  redirects, throttling or missing edge evidence cannot count as withdrawal.
- Controller-mediated Gateway deletion, owned AFD profile absence and persisted
  Gateway removal. Cleanup never strips finalizers or directly deletes AFD.

Retain the JSON report, controller runtime-image evidence, approval checkpoint,
member response/probe evidence and any cleanup failures. Namespace cleanup waits
for removal while preserving Service/PLS finalizers. Failed cleanup needs operator
follow-up within the approved scope; a failed test does not establish cloud-resource
absence. The existing environment cleanup remains a separate authorized operation.

For **offline checks only**, do not invoke `TestE2E` or run a broad tagged E2E test:
its `BeforeSuite` contacts Kubernetes and Azure. These commands exclude that entry:

```bash
go test -count=1 ./pkg/providers/azure/frontdoor
go test -count=1 -tags=e2e -run '^TestPrivateHTTP' ./test/e2e
go test -tags=e2e -run '^$' ./test/e2e
go vet -tags=e2e ./pkg/providers/azure/frontdoor ./test/e2e/...
python -B -m unittest discover -v -s ./examples/getting-started/app -p test_main.py
```

The Python command needs the app's already-declared Flask dependency in the chosen
Python environment. Offline SDK tests use fake transports; helper tests exercise
evidence rejection without creating Azure credentials or cluster clients. They do
not complete platform, installed-controller or live traffic proof.

#### Comparison with ATM live proof

The existing [ATM E2E](e2e/traffic_manager_test.go) is gated by
`ENABLE_TRAFFIC_MANAGER=true`. It creates real member workloads and hub
TrafficManagerProfile/Backend intent, waits for current-generation status/DNS/
resource IDs, and compares actual Azure SDK profile/endpoints. It also exercises
Azure drift/deletion recovery, backend/member/weight changes and Kubernetes plus
Azure resource absence during cleanup.

The [E2E workflow](../.github/workflows/e2e-tests.yml) enables ATM for the shared-VNet
matrix entry, uses the existing setup/test targets, and collects logs/cleans up
with `if: always()`. These are implemented test/CI paths, not evidence that a
particular live run passed.

The reviewed ATM scenario has no application HTTP request, observed health-probe
logs or statistical traffic-distribution assertion; its Azure comparison ignores
monitor-status fields. AFD reuses its intent/status/actual-ARM/cleanup pattern but
adds private-only frontend inspection, asynchronous PLS-owner approval, fresh
two-member application identity/Host responses, actual probe logs and deployed
withdrawal evidence. Do not reuse ATM's public-LB workload creation for AFD.

## Set up the E2E environment

After exporting the variables for one scenario, run:

```bash
make e2e-setup
```

The default cluster names are:

```bash
export HUB_CLUSTER=hub
export MEMBER_CLUSTER_1=member-1
export MEMBER_CLUSTER_2=member-2
```

Switch kubeconfig contexts with:

```bash
kubectl config use-context "${HUB_CLUSTER}-admin"
kubectl config use-context "${MEMBER_CLUSTER_1}-admin"
kubectl config use-context "${MEMBER_CLUSTER_2}-admin"
```

## Run the tests

Run the complete E2E suite:

```bash
make e2e-tests
```

The environment can be reused for repeated test runs. To run only the current Gateway API contract
scenario:

```bash
go test -timeout 50m -tags=e2e -v ./test/e2e \
  -args -ginkgo.v -ginkgo.focus='Gateway API contract'
```

## Collect logs

Collect controller and agent logs before cleanup when investigating a failure:

```bash
export LOG_DIR=agent-logs
make e2e-collect-logs
```

## Clean up

Delete the Azure resource group created for the E2E environment:

```bash
make e2e-cleanup
```

Cleanup requires `AZURE_RESOURCE_GROUP` to still identify the test resource group.
