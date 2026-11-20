# Local Service private origins

## Requirements and approved scope

User approved a revised Private Link-only implementation for the unchanged
HTTPRouteSimpleSameNamespace test, replacing the public-origin proposal.
Create a new branch; do not run live Azure provisioning or approval.

## Plan and success criteria

1. [x] Resolve local Service references alongside existing Fleet ServiceImports.
2. [x] Create owned selectorless companion ILB/PLS Services and mirror eligible endpoints.
3. [x] Reuse verified private-origin discovery and AFD programming; preserve private-only behavior.
4. [x] Add minimal upstream HTTP fixtures and bounded exact-connection approval in the test runner.
5. [x] Gate manager/chart support off by default; keep affected RBAC/configuration consistent.
6. [x] Validate focused offline regressions and compilation; document live gaps.

## Decisions

- Original application Services and EndpointSlices are never rewritten.
- Companion resources are keyed by source Service UID and selected port, shared only
  by authorized consumers of that source. Per-Gateway AFD graphs retain existing ownership.
- Initial Service backend scope is same-namespace, IPv4 HTTP root routing. Broader
  conformance remains deferred and must be reported explicitly.
- Existing ServiceImport backend paths and generated names must remain stable.
- Test approval is opt-in, caller-authorized, and matches the exact companion PLS
  request and Gateway-owned AFD profile. Controller does not auto-approve PLS.

## Implementation and validation

Branch: `feat/afd-local-service-private-origins`.

- Added the localservice controller/resolver. It only exposes eligible local
  ClusterIP/headless Services, owns companions through source Service UID/port,
  mirrors eligible IPv4 TCP endpoints, and checks mirrors, source authorization,
  companion identity/resource version, and ingress again around Azure discovery.
  Conflicting selected ports withdraw companions. Foreign objects are not adopted
  or deleted; original Service and CCM finalizers remain untouched.
- Extracted reusable ILB/PLS discovery into `pkg/common/privateorigin`; existing
  member discovery calls it with the same input and error behavior.
- Added backend Kind to model/provider keys so Service and ServiceImport names do
  not alias. Empty Kind retains existing private ServiceImport generated names.
- Added opt-in local resolver dispatch, Service/EndpointSlice watches, uncached
  current-intent rechecks, and matching cache readiness. Manager flags:
  `--enable-local-service-origins` and `--local-service-resource-group`, requiring
  AFD and leader election. Discovery shares existing Azure auth/retry/rate options.
- Updated the chart option/RBAC, regenerated aggregate RBAC, and excluded unrelated
  generated CRD documentation churn. No public CRD schema changes.
- Minimal conformance setup reads four unmodified HTTP prerequisites from the
  upstream embedded manifests, changes only the GatewayClass placeholder, and
  stages the unchanged route fixture before approval/propagation. This replaces
  shared upstream HTTPS setup, not the test's assertions or backend identity.
- The test caller verifies the owned profile and companion binding, scopes approval
  to the supplied subscription/node group, pins an exact request ID, and refuses
  duplicate/replaced/malformed requests. The controller never approves connections.
  Staging the same route before `cs.Run` avoids racing approval/edge propagation
  against the upstream 30-second HTTP assertion window; that window is unchanged.
- ADO adds default-off `runLocalServiceConformance`, enables companions during
  existing setup only for the selected AFD scenario, reuses the service connection,
  and publishes Go JSON in the existing always-run artifact/cleanup path.
- Updated Gateway chart/configuration docs and added the test guide. Core request
  routing remains HTTP/80, no custom hostnames, root PathPrefix, and one logical backend.

### Validation

All commands used Go 1.26.10 and no live Azure operations:

- Localservice focused race tests and private-discovery/member regression tests passed.
- Gateway package (including real API-server/envtest integration), localservice,
  privateorigin, member serviceexport, and Gateway manager package tests passed with
  Kubernetes 1.35.0 envtest binaries.
- Resolver/model/HTTPRoute/provider/script/fixture/approval regression tests passed.
- Race-enabled localservice/resolver/model/HTTPRoute/manager/conformance unit tests passed.
- `go vet` for all affected packages and the tagged conformance package passed.
- `go test -mod=readonly -tags=conformance ./test/conformance -run '^$'` passed:
  compilation only; the live HTTP test was not run.
- Helm render/lint contracts, including local opt-in arguments and privilege gating,
  passed with `HELM_NO_PLUGINS=1` to exclude an unrelated incompatible user plugin.
- ADO YAML default-off/AFD-only gating, Bash syntax, failure propagation, durable
  artifacts, and always-run diagnostics/cleanup checks passed.
- Relative documentation links and `git diff --check` passed.
- Initial readonly compile identified missing upstream test checksums; installed
  only the pinned upstream test package and ran `go mod tidy`. Existing versions
  were not upgraded.

### Changed areas

`pkg/controllers/hub/localservice`, `pkg/common/privateorigin`, member discovery,
hub resolver/Gateway/HTTPRoute/model, AFD backend identity/preflight,
Gateway manager startup/readiness, Gateway chart/RBAC and generated aggregate role,
E2E config/install scripts, `test/conformance`, focused tests, ADO pipeline, docs,
and upstream-test transitive dependencies.

## Risks

Selectorless LoadBalancer/PLS behavior and real traffic need a separately approved
AKS/Azure run. Offline checks do not prove permissions, approval, or edge propagation.
No claim that HTTPRouteSimpleSameNamespace passes live is made. The source-owned
companion may remain terminating while CCM cleans up; a new test must not reuse the
namespace until cleanup completes. TLS/hostname/other Core conformance gaps remain.

## Commit and live-run authorization

User requested commit, push, and ADO execution. Definition 489143 was verified to
use Azure/fleet-networking and `/.pipeline/e2e-tests.yaml`.
User explicitly approved subscription `8ecadfc9-d1a3-4ea4-b844-0d9f87e4d7c8`,
fresh `fleet-ado-<BuildID>-afd-<attempt>` resource groups, existing three-cluster
E2E setup and role assignments, AFD/ILB/PLS provisioning, exact test-connection
approval, diagnostics, and scoped resource-group cleanup. Conformance-only execution
temporarily comments out other scenarios and the ordinary E2E suite; the explicit
`runLocalServiceConformance=true` parameter is required even to start provisioning.
Do not reuse existing resources or local kubeconfig contexts. The approval is for
this requested pipeline run, not unrelated live operations.
