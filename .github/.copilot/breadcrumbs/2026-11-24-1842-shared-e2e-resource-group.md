# Shared E2E resource group

## Requirements and decisions

- User originally approved opt-in shared resource group mode, then requested shared-group-only execution with no mode flag.
- Push a new branch based on main. Do not run live Azure E2E or cleanup without separate approval.
- Shared group `fleet-networking-e2e` was created in the test subscription, westus2. Proposed grants and WIF service connection are not yet deployed.
- Prior work: [retry PR #459](https://github.com/Azure/fleet-networking/pull/459).

## Plan

1. [x] Trace E2E setup, tests, Azure dependencies, resource ownership and cleanup.
2. [x] Add offline tests for concurrent run isolation, configuration and fail-closed cleanup.
3. [x] Implement opt-in shared-group setup, naming, permissions and safe per-run cleanup; preserve existing behavior.
4. [x] Update documentation and validate targeted tests and shell checks.
5. [x] Push branch and report unverified live behavior and remaining risks.

## Before

Each Azure DevOps scenario creates and deletes a unique resource group. Fixed resource and cluster names assume one run per group.

## Investigation

- `test/scripts/bootstrap.sh` creates the whole group, fixed cluster names, a registry named from the group, Azure identities and role assignments. The network topology scripts create fixed VNet names.
- `test/e2e/e2e_test.go`, `test/e2e/framework/workload_manager.go` and `test/scripts/install-afd.sh` assume fixed cluster or registry names. AFD validates the member node resource groups before assigning roles.
- Traffic Manager profiles are named from Kubernetes object UIDs and tagged with the namespaced profile name; Front Door profiles use Kubernetes hub and Gateway ownership tags. The Traffic Manager E2E also creates an independently named public IP. These resources cannot safely be selected for cleanup using only a generic run-name prefix.
- The proposed WIF identity and grants have not been deployed. Existing E2E scripts assign roles using `--assignee`, and the AFD proof needs separate Network Contributor on member node groups for its test caller.
- A correct shared-group implementation needs durable per-run ownership evidence for controller-generated resources, exact ownership checks and cleanup ordering, including partial setup and cancellation. The current group-deletion cleanup cannot be reused.

## Status

### Admin-context failure follow-up

The approved Azure DevOps run failed during setup because Azure CLI appended `-admin` to context names that already included that suffix. The same issue blocked ATM and AFD cleanup. The user requested a fix, push and another run.

1. [x] Reproduce the context failure in a focused offline test.
2. [x] Correct setup and cleanup and validate offline.
3. [ ] Inspect and report resources left by the failed run; obtain explicit approval before any manual cleanup.
4. [ ] Push the fix and trigger the requested new E2E run.

Azure CLI appends `-admin` to an explicitly supplied context for admin credentials. Setup and cleanup now pass the unsuffixed logical cluster name; the Kubernetes context remains `<name>-admin`. The focused mock reproduces the failure before the fix and passes afterward for ordinary setup, performance setup and cleanup. The full script suite, vet, Bash syntax and whitespace checks passed.

The original run completed with failed ATM and AFD cleanup. Read-only inspection found six surviving AKS clusters and their managed node groups, plus run-owned registries, VNets and identities. Manual cleanup requires separate approval; do not delete the shared resource group.

The user approved removing setup kubeconfig, group and collision checks, the cleanup marker, and matching cleanup checks. Azure DevOps log collection and cleanup will run with `always()`. Per-resource ownership checks remain.

1. [x] Remove the selected setup and cleanup checks and marker.
2. [x] Make Azure DevOps log collection and cleanup unconditional.
3. [x] Update documentation and validate offline.
4. [x] Push the changes.

The shared-group-only revision:

1. [x] Remove the legacy resource-group creation/deletion path and require per-run shared-group configuration.
2. [x] Update Azure DevOps and GitHub workflow callers and preserve safe cleanup eligibility.
3. [x] Keep performance topology working with run-specific resources and ownership-verified cleanup.
4. [x] Update tests and documentation and validate offline.
5. [x] Push the shared-group-only revision.

The Azure DevOps pipeline supplies a unique run ID per scenario and attempt; scripts derive physical Azure names and retain the established logical Kubernetes contexts. At the user's request, setup no longer validates the kubeconfig path, resource-group tags or run collisions. Azure DevOps always attempts log collection and cleanup.

Cleanup verifies run tags, resource IDs, AKS node-group ownership, role scopes and peering targets. It verifies Traffic Manager namespace tags and Front Door hub/Gateway identity, stops the run's hub controllers before deleting their profiles, deletes only identified roles and resources, and never deletes the shared group. It fails explicitly when ownership, controller quiescence or deletion cannot be verified.

Shared AFD requires the test principal object ID, grants it Network Contributor only on verified member node groups, and records the principal for cleanup. The user requested storing the object ID as the `E2E_TEST_PRINCIPAL_ID` Azure DevOps pipeline variable instead of parsing a token; the variable was configured for the current CI identity. It must be updated whenever the selected service connection changes; its identity is not independently verified by the pipeline.

The user initially chose to retain both resource-group tag checks, then explicitly requested removing them. The guide uses a caller-selected pre-created group name.

The user subsequently requested removal of `shared_group_test.go` and `azure_principal_id_test.go`. Dedicated mocked cleanup and principal-token regression coverage is no longer retained.

The user requested removal of the GitHub Actions E2E workflow. Azure DevOps remains the E2E pipeline; its setup and cleanup behavior is unchanged.

Changed files: `.pipeline/e2e-tests.yaml`, `test/README.md`, the `test/scripts` setup, topology, configuration, AFD and cleanup scripts and tests, plus E2E framework workload naming and Traffic Manager public-IP tagging.

## Validation and risks

- `GOTOOLCHAIN=go1.26.10 go test -count=1 ./test/scripts` passed after the requested test removal. Earlier runs also covered the now-removed mocked cleanup, token parsing and performance topology cases.
- `GOTOOLCHAIN=go1.26.10 go vet ./test/scripts ./test/e2e/framework` passed.
- `GOTOOLCHAIN=go1.26.10 go test -tags=e2e -run '^$' ./test/e2e` and `go vet -tags=e2e ./test/e2e` passed without executing live E2E.
- Performance packages compiled and passed `go vet` without running their suites. Installed Azure CLI option checks, Bash syntax, both workflow YAML parsing and `git diff --check` passed.
- The generic guide's Markdown fences are balanced and its local links resolve.
- After preflight removal, mocked setup reached creation with unset and existing kubeconfig paths; mocked cleanup reached credential retrieval without kubeconfig or group-tag checks. Azure DevOps YAML checks verified unconditional log collection and cleanup, and the remaining script suite and vet passed.
- The new preflight-removal change has not been validated against live Azure. Setup can overwrite existing kubeconfig contexts and reuse run resources; uniqueness is the caller's responsibility. Cleanup can fail closed and leave resources; there is no stale-run sweeper. Contributor on the shared RG is not a security boundary between runs.
