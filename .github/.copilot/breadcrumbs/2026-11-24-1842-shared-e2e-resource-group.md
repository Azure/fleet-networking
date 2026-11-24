# Shared E2E resource group

## Requirements and decisions

- User approved an opt-in shared resource group mode with unique run identities, safe cleanup, offline tests and documentation; existing disposable-group behavior remains the default.
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

Shared-group mode is opt-in. The Azure DevOps pipeline supplies a unique run ID per scenario and attempt; scripts derive physical Azure names and retain the established logical Kubernetes contexts. Setup requires a new run-specific kubeconfig, verifies the group and rejects name or ownership collisions before enabling cleanup.

Cleanup verifies the group, run tags, resource IDs, AKS node-group ownership, role scopes and peering targets. It verifies Traffic Manager namespace tags and Front Door hub/Gateway identity, stops the run's hub controllers before deleting their profiles, deletes only identified roles and resources, and never deletes the shared group. It fails explicitly when ownership, controller quiescence or deletion cannot be verified.

Shared AFD requires the test principal object ID, grants it Network Contributor only on verified member node groups, and records the principal for cleanup. The pipeline service connection remains unchanged by default and can be selected explicitly after its grants are deployed.

Changed files: `.pipeline/e2e-tests.yaml`, `test/README.md`, the `test/scripts` setup, topology, configuration, AFD and cleanup scripts and tests, plus E2E framework workload naming and Traffic Manager public-IP tagging.

## Validation and risks

- `GOTOOLCHAIN=go1.26.10 go test -count=1 ./test/scripts ./test/e2e/framework` passed, including mocked concurrent-run, collision, profile, peering, role, cleanup and legacy cases.
- `GOTOOLCHAIN=go1.26.10 go vet ./test/scripts ./test/e2e/framework` passed.
- `GOTOOLCHAIN=go1.26.10 go test -tags=e2e -run '^$' ./test/e2e` and `go vet -tags=e2e ./test/e2e` passed without executing live E2E.
- Installed Azure CLI AFD option checks, Bash syntax, pipeline YAML parsing and `git diff --check` passed.
- No live Azure provisioning, permission, test or cleanup operation was run. The proposed grants and service connection have not been enabled. Cleanup can fail closed and leave resources; there is no stale-run sweeper. Contributor on the shared RG is not a security boundary between runs.
