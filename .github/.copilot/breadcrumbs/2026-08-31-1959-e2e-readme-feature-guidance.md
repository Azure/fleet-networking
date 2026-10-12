# E2E README Feature Guidance

## Requirements

- Update `test/README.md` so local E2E setup is clear and feature-specific.
- Separate the baseline networking, Azure Traffic Manager (ATM), and Azure Front Door (AFD)
  requirements and commands.
- Accurately describe which AFD behavior is testable today.
- Preserve the existing setup, test, and cleanup workflow.

## Additional comments from user

- The user asked whether the README can be updated separately for ATM and AFD.

## Plan

### Phase 1: Verify the current E2E wiring

- [x] **Task 1.1: Review the E2E README and Makefile entrypoints.**
  - Success criteria: the guide uses the repository's actual `e2e-setup`, `e2e-tests`,
    `e2e-collect-logs`, and `e2e-cleanup` targets.
- [x] **Task 1.2: Review feature gates and CI coverage.**
  - Success criteria: the guide accurately reflects `AZURE_NETWORK_SETTING`,
    `ENABLE_TRAFFIC_MANAGER`, and the current AFD contract-test behavior.

### Phase 2: Restructure the local developer guide

- [x] **Task 2.1: Document shared prerequisites and lifecycle.**
  - Success criteria: common Azure, tool, cluster, test, and cleanup instructions are stated once.
- [x] **Task 2.2: Add baseline networking and ATM scenarios.**
  - Success criteria: each scenario provides the exact environment variables, supported network
    settings, setup command, and test-selection behavior.
- [x] **Task 2.3: Add the AFD scenario and limitation.**
  - Success criteria: the guide explains that Gateway API CRDs and the live Kubernetes API contract
    are covered, while AFD resource reconciliation is not yet implemented or deployed by E2E.

### Phase 3: Review the documentation

- [x] **Task 3.1: Check commands, links, terminology, and diff.**
  - Success criteria: commands match the scripts and Makefile, ATM and AFD are not conflated, and
    the resulting diff contains only the README and breadcrumb.

### Detailed checklist

- [x] Phase 1 / Task 1.1 completed.
- [x] Phase 1 / Task 1.2 completed.
- [x] Phase 2 / Task 2.1 completed.
- [x] Phase 2 / Task 2.2 completed.
- [x] Phase 2 / Task 2.3 completed.
- [x] Phase 3 / Task 3.1 completed.

### Overall success criteria

- A developer can choose a baseline networking, ATM, or AFD-oriented E2E workflow without guessing
  which flags apply.
- The guide does not claim that the current AFD E2E creates or validates Azure Front Door resources.
- Existing Makefile and script behavior remains unchanged.

## Decisions

- Keep one shared lifecycle and use scenario sections only for differing environment variables and
  behavior.
- Describe the current AFD suite as a Gateway API contract scenario because the Gateway manager has
  no reconcilers yet.
- Do not introduce new scripts or feature flags in this documentation-only change.

## Implementation Details

- `test/README.md` now has one common lifecycle with separate baseline networking, Traffic Manager,
  and AFD/Gateway API scenario sections.
- The AFD section identifies the exact live API resources and backend-reference behavior exercised
  today.
- The guide states that real AFD reconciliation requires controller deployment, Azure
  configuration, validators, status assertions, and cleanup that do not exist yet.

## Changes Made

- Added this breadcrumb as the documentation and decision trail.
- Expanded prerequisites to cover the tools invoked by the E2E setup.
- Added feature-specific environment examples and behavior.
- Added focused Gateway API contract-test execution and log-collection guidance.
- Reviewed the final documentation diff and confirmed it has no whitespace errors.

## Before/After Comparison

- **Before:** The E2E guide presents Traffic Manager variables as if they apply to every local run
  and does not explain Gateway API or AFD coverage.
- **After:** The guide separates shared setup from baseline, ATM, and current AFD contract coverage
  and explicitly identifies the missing real-AFD reconciliation wiring.

## References

- `test/README.md`: Existing local E2E guide.
- `Makefile`: E2E setup, execution, log collection, and cleanup targets.
- `test/scripts/bootstrap.sh`: Network-setting and Traffic Manager feature setup.
- `test/e2e/e2e_test.go`: Shared E2E suite and Gateway API scheme registration.
- `test/e2e/traffic_manager_test.go`: Traffic Manager feature gate and Azure validation.
- `test/e2e/gateway_api_test.go`: Current Gateway API live-cluster contract coverage.
- `.github/workflows/e2e-tests.yml`: CI scenario matrix.
- Repository domain knowledge: no files are present under `.github/.copilot/domain_knowledge`.
- Repository specifications: no files are present under `.github/.copilot/specifications`.
