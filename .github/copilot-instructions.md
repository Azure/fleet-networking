# Fleet networking Copilot instructions

## Project and architecture

Fleet networking is a Go/controller-runtime project with a hub-and-member architecture.
Use the checked-out source and [Makefile](../Makefile) as the authority for implemented behavior
and commands; do not assume a feature branch's components exist on main.

- [Hub controllers](../pkg/controllers/hub) coordinate exported services and endpoint metadata
  and reconcile Azure Traffic Manager profiles and backends.
- [Member controllers](../pkg/controllers/member) export local services/endpoints and import
  fleet networking state. [MCS controllers](../pkg/controllers/multiclusterservice) reconcile member-side
  multi-cluster services.
- [API types](../api) define the networking CRDs; generated manifests live in
  [config/crd/bases](../config/crd/bases). Keep API types, generated code, manifests, RBAC,
  and affected charts consistent when changing an API.
- Preserve the ServiceExport/ServiceImport contract: matching service names and namespaces
  identify a logical service across clusters. Preserve hub/member client boundaries and
  reserved member namespaces; do not treat cluster-local objects as globally interchangeable.
- Reconciliation must tolerate retries and deletion. Preserve resource ownership checks,
  finalizer cleanup, status conditions, and existing error/requeue behavior. Keep Azure
  operations within the relevant controller's existing SDK-client boundary, reuse
  [Azure error helpers](../pkg/common/azureerrors), and test failure paths without live Azure.

See the [architecture overview](../README.md#architecture),
[service export contract](../docs/concepts/ExportingService/README.md),
[DNS load balancing](../docs/concepts/DNSBasedGlobalLoadBalancing/README.md), and
[Traffic Manager permissions](../docs/howtos/traffic-manager-permissions-setup.md).

## Development commands

Use the Go version declared in [go.mod](../go.mod). Make targets install their configured
tools when needed; do not substitute unrelated tools or versions.

| Purpose | Command | Notes |
|---------|---------|-------|
| Focused package tests | `go test -v ./pkg/common/azureerrors` | Replace with the affected package; see envtest prerequisites below. |
| Package analysis | `go vet ./pkg/common/azureerrors` | Replace with the affected package. |
| Package/controller suites | `make local-unit-test` | Tests under cmd and pkg, including envtest-backed suites; uses race detection. |
| Additional integration suites | `make integration-test` | Tests API and CRD-installer suites; requires Ginkgo CLI and envtest. |
| Full local test pipeline | `make test` | Generates manifests/code, formats, vets, then runs both test targets; no Azure E2E. |
| Build managers | `make build` | Also generates code, formats, and vets. |
| Format | `make fmt` | Runs go fmt and goimports repository-wide. |
| Lint | `make lint` | Uses the Makefile-configured golangci-lint; not golint. |
| Full quality checks | `make reviewable` | Formats, vets, lints, runs staticcheck and go mod tidy; can modify files. |
| Regenerate API artifacts | `make manifests generate` | Run after API or generation-marker changes; inspect generated changes. |
| Azure E2E lifecycle | `make e2e-setup`, `make e2e-tests`, `make e2e-collect-logs`, `make e2e-cleanup` | Requires an explicitly approved Azure test environment. |

### Choose the smallest sufficient validation

- For Go changes, add/update regression tests and run affected packages first, then relevant
  broader checks. Use `-run` for Go tests or Ginkgo focus flags for Ginkgo specs.
- Controller directories can contain both unit and envtest integration tests. A plain package
  test may start a local API server and etcd. Use the Makefile's envtest setup or configure
  `KUBEBUILDER_ASSETS` for a targeted run; do not silently skip integration suites.
- Unit/integration tests use table-driven Go tests and Ginkgo/Gomega. Reuse existing suites,
  setup, contexts, and mocks. Keep tests beside the code where the package already does so.
- Do not use blanket `go test ./...` as a substitute for selecting the right suites and
  prerequisites. Run `make test` when full local unit/integration coverage is needed.
- Documentation-only changes need link/Markdown checks, not Go builds or Azure E2E.
- Report the exact checks run, failures, and unverified behavior. Offline tests do not prove
  Azure permissions, provisioning, or end-to-end traffic.

### Azure E2E safety

Follow the [E2E guide](../test/README.md) and inspect the relevant
[scripts](../test/scripts) before running them. Use the current go.mod requirement rather
than an older Go version shown in a guide.

E2E setup creates Azure resources and role assignments and can incur costs. Before setup,
live tests, or cleanup, obtain explicit approval of the subscription, resource group, and
scope of operations. Use a test subscription, verify Kubernetes contexts, and never run
cleanup against an unverified environment. Collect relevant logs before approved cleanup.

## Change conventions

- Follow the [Go style guide](https://go.dev/wiki/Style) and existing package patterns.
  Favor the standard library and existing helpers over new dependencies.
- Keep changes surgical; preserve unrelated local edits and avoid repository-wide formatting
  churn when only a few files change. Format changed Go files with goimports.
- Run go mod tidy when dependencies change. Review generated and dependency changes before
  including them in a commit.
- Update directly affected docs and examples; do not describe proposals as implemented.
- Use [docs](../docs) and current source for domain context. Consult branch-specific specs
  only if they exist; do not assume a hidden specification directory or template is present.

## Planning and breadcrumbs

- Small, well-scoped fixes, documentation edits, and read-only questions do not require a
  breadcrumb or a separate plan-approval round. Proceed within the user's requested scope.
- For non-trivial work (cross-controller behavior, API changes, Azure resource lifecycle,
  or substantial refactors), propose a concise plan and get explicit approval before
  implementation. If the user already approved that plan, do not ask again unless scope changes.
- For such work, create a UTC-named breadcrumb under
  [.github/.copilot/breadcrumbs](.copilot/breadcrumbs) using
  `yyyy-mm-dd-HHMM-{title}.md`; choose a descriptive title unless the user supplies one.
  Record requirements, user input, numbered plan/checklist with success criteria, decisions,
  implementation details, changed files, before/after behavior, validation, and references.
  Start checklist tasks unchecked, add tests first where practical, and mark validation tasks
  complete only after checks pass.
- Update the breadcrumb before implementation and after significant changes, link related
  prior work, and summarize unresolved risks before concluding. Keep durable architecture
  guidance in docs rather than requiring every task to read historical breadcrumbs.
