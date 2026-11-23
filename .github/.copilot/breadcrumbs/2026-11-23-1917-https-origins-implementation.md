# HTTPS origins implementation

## Requirements and authorization

- User: "assume that an AKV, a cert and corresponding private key is available
  (via env vars), go ahead and implement the \"feat\", \"build\", and \"test\" commits".
- Implement the reviewed [HTTPS contract](../../../docs/design/gep-1748-https-origins-proposal.md)
  following the [review history](2026-11-16-2239-https-origins-proposal.md).
- Preserve public MCS APIs, member-only customer configuration, automatic owned
  hub policy projection, platform ingress authorization and a control-plane-only hub.
- One applicable member policy, explicit Fleet export opt-in, system trust,
  shared SNI, HTTPS on both hops, no downgrade/custom domains or Secret reads
  by controllers. Member-local policy failure isolates the exact origin;
  shared ambiguity/no eligible origins/unverifiable isolation suspends the route.
- AKV and certificate inputs are test-fixture inputs only. No live subscription,
  resource group or cluster scope has been approved; do not provision, retrieve
  secrets or run live tests. Do not print environment values or private material.

## Plan and success criteria

1. [x] Implement typed internal TLS transport, member validation/watch/heartbeat,
   hub agreement/projection, model and provider HTTPS lifecycle/isolation.
2. [x] Wire default-off capability, startup prerequisites, conditional RBAC,
   generated internal artifacts and charts; preserve TLS-off HTTP startup.
3. [x] Add regression tests and separately gated env-var certificate fixtures.
   Run focused tests, generation/type/vet/lint and relevant controller suites.
4. [ ] Create separate feat(https), build(https), test(https) commits with required
   trailers; verify worktree and commit contents. No push unless requested.

## Implementation decisions

- Refresh member intent every minute; expire observations after five minutes.
  Future timestamps beyond one minute are invalid. This bounds stale authority
  during partitions without claiming immediate revocation.
- Opt-in changes are metadata updates: do not rely on policy generation alone.
- Bind a metadata-only internal heartbeat to the SHA-256 digest of the typed
  snapshot (including source UID/policy observation). Preserve the snapshot
  timestamp while intent is unchanged. Otherwise a minute-by-minute spec write
  would invalidate the existing MCS conflict-generation acknowledgement and
  spuriously withdraw healthy routes. No arbitrary member annotations propagate.
- Linux validation uses the existing WSL Go 1.26.10, matching go.mod; the host
  Windows Go differs. Existing tools/envtest assets are preferred.
- Installation/manager wiring is delegated in an independent scope. Runtime,
  internal API, fixtures and this record remain coordinated here.
- Owned hub policies are target-UID-bound and shared across real authorized
  Gateway ancestors. Last-reference cleanup uses UID/resourceVersion
  preconditions and never adopts foreign policies.
- AFD keeps Private Link identity separate from shared origin SNI, selects the
  Service's HTTPS port, enables certificate-name checks, omits the Host override,
  uses HTTPS GET probes and independently requires HTTPS on the frontend.
  Stale origins are removed before replacements; hostname publication requires
  complete provider inventory. TLS uncertainty attempts owned suspension and
  preserves both the original and enforcement errors.
- Default-off manager/chart capabilities require uncached prerequisite checks.
  The opt-in E2E installer upgrades the internal hub CRD first and grants each
  member credential only the additional named hub-CRD read before startup.
- Member Makefile/run/image commands compile the complete manager package;
  startup helpers must not be omitted by building only main.go.
- The fixture accepts certificate/key file paths outside the checkout, not raw
  PEM environment values. Certificate ownership/AKV retrieval is external; no
  controller or test runner gains AKV credentials.

## Changed files and behavior

- `api/v1alpha1/internalserviceexport_types.go`, its generated deepcopy and
  internal CRD: non-secret TLS snapshot; no public MCS API changes.
- `pkg/common/origintls`, member `serviceexport`, hub `gatewayresolver`,
  `gatewaymodel`, `gateway`, `httproute` and AFD provider: policy validation,
  current intent, owned projection, HTTPS configuration and verified isolation.
- Both manager packages/charts, `config/rbac/role.yaml`, `Makefile`, member
  Dockerfile and E2E install/config scripts: explicit capability and prerequisites.
- Existing adjacent suites plus `origintls*_test.go`/`https_test.go`, API-server
  TLS integration, chart/script tests, `test/fixtures/origintls` and the separate
  `gateway_private_https_test.go`/Python application fixture: offline regressions
  and opt-in live proof, without replacing HTTP tests.
- Design, customer how-to, chart READMEs and test guides distinguish this
  experimental branch from released HTTP-only images and live certification.
- Before: only the HTTP graph could be provisioned. After explicit enablement:
  both TLS hops and validated current member intent are mandatory. Invalid
  attributable members are excluded; shared conflict/uncertainty fails closed.
  The hub remains outside the data path.

## Validation and unresolved risks

- Passed with WSL Go 1.26.10, matching go.mod:

  ```bash
  export KUBEBUILDER_ASSETS="$(hack/tools/bin/setup-envtest-release-0.22 use 1.33.0 -p path)"
  go test -race ./pkg/common/origintls ./pkg/controllers/hub/gatewaymodel ./pkg/controllers/hub/gatewayresolver ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./pkg/controllers/hub/httproute ./pkg/controllers/member/serviceexport ./cmd/member-net-controller-manager ./cmd/hub-gateway-controller-manager ./test/fixtures/origintls ./test/scripts ./test/apis/v1alpha1 ./cmd/net-crd-installer/utils
  go test -tags helm ./test/helm
  go test -tags=e2e ./test/e2e -run "^$"
  go build ./cmd/member-net-controller-manager ./cmd/hub-gateway-controller-manager
  make lint
  ```

- Passed focused `go vet` on the affected runtime, manager, fixture and script
  packages; the final repository lint also runs govet/staticcheck.
- Passed `make manifests generate`; retained internal TLS/RBAC deltas only,
  removing unrelated pre-existing generated-description/import drift.
- Changed Go files formatted with the repository's configured goimports.
- Passed local Markdown link/anchor and fence checks, YAML parsing, Bash syntax
  (documented angle-bracket inputs substituted), changed-script syntax and Python
  AST parsing. Existing Mermaid diagrams were not changed.
- Container build unverified: `docker buildx build --platform linux/amd64 --load -f
  docker/member-net-controller-manager.Dockerfile --tag
  fleet-networking/member-net-controller-manager:copilot-0fbf921e-https-validation .`
  failed because Buildx is absent. Standard `docker build -f
  docker/member-net-controller-manager.Dockerfile --tag
  fleet-networking/member-net-controller-manager:copilot-0fbf921e-https-validation .`
  reached the existing cache-mount instruction and failed because the legacy
  builder lacks BuildKit. No replacement tool/version or Dockerfile workaround
  was introduced; package builds passed.
- No live Azure operations, secret retrieval or end-to-end TLS run performed.
- Offline checks cannot establish certificate trust at AFD, application/probe
  SNI/Host behavior, provider propagation or end-to-end availability.
- The complete TLS-01...TLS-15 live negative/rotation/restart/upgrade matrix is
  not claimed. The live fixture covers positive traffic/probes, member opt-out,
  all-ineligible withdrawal and cleanup only.
- Shared-policy reference reconciliation currently inventories hub routes on
  Gateway reconciliation; large-fleet scalability is unmeasured. Sequential ARM
  checks cannot exclude concurrent external writers; retain exclusive profile
  ownership. No instantaneous-stop or zero-downtime migration guarantee.
