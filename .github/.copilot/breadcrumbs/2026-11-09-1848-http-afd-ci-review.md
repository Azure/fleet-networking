# HTTP AFD shared-VNet CI review

## Requirements

- Validate the review request to include AFD in shared-VNet CI E2E.
- Fix valid feedback on top of current HTTP HEAD and commit separately.
- Preserve existing ATM/topology coverage and exclude paused HTTPS work.

## Additional comments from user

- "First validate them and then commit fixes on top on current HEAD."
- "We will organize and amend the commits later."
- Review: "Could we include the new AFD scenario in the shared-VNET CI E2E job?"

## Plan

Status: approved. User selected "Approve implementation and commit".
Approval covers local implementation, validation and a new commit, not a push
or live Azure execution.

### Phase 1: Add focused tests

- [x] Task 1.1: Add a workflow contract test using the existing YAML decoder.
  Verify the existing four matrix cases remain, an independent shared-VNet
  AFD case exists, feature selection reaches setup/tests, exact test-only
  auto-approval is enabled only for AFD, and log artifacts remain unique.
  Success: the test fails against the current workflow and passes after fixing.

### Phase 2: Wire CI

- [x] Task 2.1: Add a fifth, shared-VNet AFD matrix case with ATM disabled.
  Preserve the existing shared-VNet ATM and other topology cases.
  Pass AFD selection and test-only approval at job scope, and explicitly keep
  retained setup off so ordinary cleanup remains selected.
  Success: setup installs AFD and the standard runner includes HTTP proof plus
  baseline tests without requiring manual PLS approval.
- [x] Task 2.2: Include the AFD selector in log/artifact names.
  Success: both shared-VNet cases have distinct artifacts.
- [x] Task 2.3: Run actionlint; fix only workflow errors directly encountered
  while validating the edited workflow, preserving intended trigger semantics.
  Success: the changed workflow passes actionlint rather than hiding errors.

### Phase 3: Validate and commit

- [x] Task 3.1: Run script tests, goimports, golint, go vet, configured
  golangci-lint, actionlint and whitespace checks as appropriate.
  Success: focused validation passes without Azure operations.
- [x] Task 3.2: Record results and create a new fix commit with required trailers.
  Success: only this review fix/test/breadcrumb is committed; no amendment,
  push, HTTPS changes or live execution.

### Completion checklist and success criteria

- [x] Phase 1 / Task 1.1: failing-then-passing workflow regression test.
- [x] Phase 2 / Tasks 2.1-2.3: isolated HTTP AFD shared-VNet CI coverage.
- [x] Phase 3 / Tasks 3.1-3.2: checks passed and fix committed atop HTTP HEAD.

## Decisions

- Feedback is valid but targets the wrong file: E2E matrix is in
  `.github/workflows/e2e-tests.yml`, not `unit-integration-tests.yml`.
- Use a separate shared-VNet row rather than replacing ATM coverage or coupling
  ATM/AFD resource graphs in one fixture. This adds one Azure-backed CI job and
  therefore additional CI cost when the existing E2E workflow is triggered.
- Reuse `ENABLE_AZURE_FRONT_DOOR` and the existing exact owned-request
  `ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL` test helper. No production approval
  automation or broad PLS auto-approval is introduced.
- Do not add PR triggers or run live CI locally. GitHub-hosted job time limits
  still bound cloud convergence; this patch does not guarantee every Azure run
  completes or change the test's existing cloud-convergence assertions.

## Implementation Details

The workflow uses an explicit include-only matrix to avoid merging the second
shared-VNet case into the original ATM entry. AFD selection and the existing
test-only approval flag are job-level environment variables, so setup and tests
receive the same selection. Retention is explicitly false.

Existing config derives Gateway/proof enablement and the candidate image from
AFD selection. The existing standard runner retains baseline coverage.
The existing YAML decoder needed no dependency changes.

## Changes Made

- Inspected both workflows, configuration, bootstrap/installer, standard runner,
  exact HTTP approval admission and existing script tests.
- Added the fifth independent shared-VNet AFD row and unique artifact names in
  `.github/workflows/e2e-tests.yml`.
- Added `TestAFDSharedVNetWorkflow` in `test/scripts/afd_test.go`, covering
  topology/feature preservation, selection/approval/cleanup propagation, and
  shared-VNet artifact separation.
- Corrected the existing invalid `create.tags` trigger to the intended version
  tag push trigger and quoted GITHUB_ENV/federated-token shell expansions.
- Regression test before the workflow change:
  `go test ./test/scripts -run '^TestAFDSharedVNetWorkflow$' -count=1` failed
  as expected because no AFD job selection existed.
- Validation after implementation, through WSL:
  - `hack/tools/bin/goimports -w test/scripts/afd_test.go`: passed.
  - `go test ./test/scripts -count=1`: passed (6.345s).
  - `go vet ./test/scripts`: passed.
  - `hack/tools/bin/golint -set_exit_status ./test/scripts`: passed.
  - `hack/tools/bin/golangci-lint-v1.64.7 run ./test/scripts`: passed.
  - `/root/.local/bin/actionlint .github/workflows/e2e-tests.yml`: passed,
    including its embedded shell checks.
  - `git diff --check`: passed.
- No Bash scripts changed, so separate Bash syntax/ShellCheck invocations were
  unnecessary. No live CI/Azure test ran; actual runner permission, cost and
  propagation outcomes remain unverified.
- This record is included with the new review-fix commit atop `a0c29a8`;
  the unrelated HTTP-head comparison breadcrumb is not staged.

## Before/After Comparison

Before: four topology cases, shared-VNet ATM enabled, AFD never selected.
After: those cases retained, plus an independent shared-VNet HTTP AFD case
with scoped test approvals and distinct logs/artifacts.

## References

- `.github/workflows/e2e-tests.yml`: actual shared-VNet CI matrix and lifecycle.
- `.github/workflows/unit-integration-tests.yml`: unit/integration/chart/image
  checks, not the live E2E topology matrix.
- `test/scripts/e2e-config.sh`, `install-afd.sh`, `run-e2e.sh`: AFD selection,
  controller installation and baseline-inclusive HTTP proof.
- `test/e2e/gateway_private_http_network_test.go`: exact correlated test-only
  PLS approval and verification.
- `docs/howtos/gateway-api-afd-configuration.md`: private-only product contract
  and asynchronous PLS approval; no specification version declared.
- `2026-11-09-1839-http-only-hardening.md`: paused HTTPS and HTTP-only baseline.
