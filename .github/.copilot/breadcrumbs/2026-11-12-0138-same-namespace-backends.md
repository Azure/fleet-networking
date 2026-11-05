# Same-namespace HTTPRoute backends for the first release

## Requirements and approval

- Reviewer requested deferring cross-namespace backend support because the first
  release lacks live E2E validation of that path.
- User approved: "Approve implementation and amend".
- Restrict backend references to the HTTPRoute namespace, including when a
  ReferenceGrant exists. Preserve cross-namespace HTTPRoute-to-Gateway attachment
  under the existing listener policy.
- Preserve fail-closed withdrawal, ownership and finalizer behavior.
- Validate locally, amend the existing commit, and do not push or run live Azure.

## Plan and success criteria

1. [x] Add regression tests for namespace defaulting, explicit same-namespace
   references, and denied cross-namespace references with or without grants.
2. [x] Enforce the restriction before backend reads; update affected withdrawal
   handling and tests without weakening stale-intent checks.
3. [x] Update current limitations and directly affected documentation.
4. [x] Run affected package tests (including envtest), formatting, vet, lint,
   documentation checks and whitespace checks.
5. [x] Review the diff, amend the existing commit, and verify a clean worktree.

## Investigation and decisions

- `gatewayresolver/route.go` currently authorizes cross-namespace ServiceImports
  through ReferenceGrants.
- `gateway/reference_withdrawal.go` performs uncached denial detection and
  whole-endpoint suspension independently of backend availability.
- Keep that safety behavior for unsupported cross-namespace intent; grants must
  no longer make it eligible.
- Same namespace means HTTPRoute namespace, not Gateway namespace.
- Unit/envtest coverage does not prove live Azure datapath behavior.

## Implementation and validation

- `AuthorizeBackendReference` now checks kind and namespace without client reads.
  An omitted namespace defaults to the route namespace; an explicit matching
  namespace remains valid. Other namespaces return `RefNotPermitted` even with
  a grant. Removed grant matching and its no-longer-applicable tests.
- Withdrawal still verifies current ownership, route identity/spec and listener
  attachment, then suspends the whole endpoint for denied backend intent.
  Diagnostic messages now describe unsupported backend namespaces rather than
  grant revocation. No partial graph is programmed; never-owned denied intent
  does not acquire a finalizer.
- Updated ownership and readiness tests to use same-namespace backends while
  retaining cross-namespace Gateway attachment and stale-intent coverage.
- Envtest now uses a route-local ServiceImport and verifies denial even with a
  matching grant, grant deletion/recreation, missing-backend precedence, correction
  back to same-namespace intent and recovery through the ServiceImport watch.
- Existing E2E API round-trip fixtures remain valid API objects, but controller
  expectations now require namespace denial regardless of grants.
- Retained existing grant dependency watches/indexing; they do not authorize
  cross-namespace routing. Removing unused watch/RBAC infrastructure is outside
  this behavioral restriction.

Changed files:

- `pkg/controllers/hub/gatewayresolver/route.go`, `route_test.go`
- `pkg/controllers/hub/gateway/reference_withdrawal.go`,
  `reference_withdrawal_test.go`, `staging.go`, `ownership_test.go`,
  `http_provisioning_test.go`, `observation_integration_test.go`
- `pkg/controllers/hub/httproute/controller_test.go`
- `test/e2e/gateway_controlplane_test.go`
- Chart README, configuration guide, design, implementation plan, test README
  and this breadcrumb.

Validation (WSL Ubuntu, `GOTOOLCHAIN=go1.26.10`, cached Kubernetes 1.33.0 envtest):

- Before implementation:
  `go test -count=1 -run "^TestResolveBackendNamespaceScope$" ./pkg/controllers/hub/gatewayresolver`
  failed in both cross-namespace cases because the old implementation read grants.
- `go test -race -count=1 -skip "^TestAPIs$" ./pkg/controllers/hub/gatewayresolver ./pkg/controllers/hub/gateway`
  passed as the initial focused unit check; integration was subsequently run.
- Final `go test -race -count=1 -timeout=12m ./pkg/controllers/hub/gateway ./pkg/controllers/hub/gatewayresolver ./pkg/controllers/hub/httproute ./cmd/hub-gateway-controller-manager`
  passed, including the real API-server suite. Gateway package: 129.216 seconds.
- `go vet ./pkg/controllers/hub/gateway ./pkg/controllers/hub/gatewayresolver ./pkg/controllers/hub/httproute ./cmd/hub-gateway-controller-manager`
  passed.
- `hack/tools/bin/golangci-lint run ./pkg/controllers/hub/gateway/... ./pkg/controllers/hub/gatewayresolver/... ./pkg/controllers/hub/httproute/... ./cmd/hub-gateway-controller-manager/...`
  passed after extracting a repeated test namespace constant.
- `go test -tags=e2e -run "^$" ./test/e2e` passed compilation without running
  live tests (`[no tests to run]`).
- Ran configured `goimports -local go.goms.io/fleet-networking -w` on changed Go files.
- On each affected Markdown file, including this breadcrumb:
  `npx --offline --yes --package=node@22 --package=markdown-link-check -- markdown-link-check --quiet --config .github\workflows\markdown.links.config.json <file>`
  passed. Host npm emitted engine warnings; execution used the Node 22 package.
- `git diff --check` passed.
- `git diff --cached --check` passed before amending; existing commit message
  and Copilot trailers were preserved. The post-amend worktree was clean.
  No push was performed.

## Risks and unverified behavior

- No live Azure operations, Kubernetes E2E, image builds or pushes were performed.
- Whole-endpoint suspension intentionally interrupts permitted peer routes when
  an attached route has denied backend intent; selective withdrawal is not added.
- Sequential uncached checks are not a transactional multi-object snapshot.
- Future cross-namespace backend support requires explicit scope and live
  qualification rather than merely adding a ReferenceGrant.

## References

- PR #400 reviewer comment on `reference_withdrawal.go`.
- Related provider contract work:
  `2026-11-11-2344-required-http-provisioner.md`.
