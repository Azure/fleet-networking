# Standard live AFD E2E with automatic activation and cleanup

## Requirements

- Run committed candidate `9cdff241d899141ce7f705d982ff7812d999d620`, not the
  previous manually activated diagnostic image.
- Use the same Sweden Central shared-VNet fixture: hub and two member AKS
  clusters, two Standard_D2s_v5 nodes each, and a fresh run-owned resource group.
- Enable test-caller-only Private Link auto-approval and set retention false.
- Require installed automatic endpoint/route activation, current owned private
  configuration, exact backend-side approvals, fresh both-member traffic/Host
  evidence, actual probe logs, withdrawal and cleanup.
- Verify current effective Owner access, active lifetime and quota immediately
  before writes. Reserve 75 minutes for owned environment cleanup.
- Preserve unrelated resources and previous local evidence. No manual AFD
  enablement, permission bypass, competing controller or push is authorized.

## Additional comments from user

- "start a live test, i renewed owener perms"
- The preceding discussion selected `ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL=true`
  and `RETAIN_AFD_E2E_SETUP=false` for a complete live proof, including traffic.

## Plan

### Phase 1: Verify prerequisites

- Task 1.1: Independently verify active Owner schedule/effective permissions,
  execution deadline, quota and fresh fixture absence.
  Success: current authorization supports five execution hours plus cleanup.
- Task 1.2: Prepare an isolated exact committed checkout and validate existing
  tools/runner scripts without creating Azure resources.
  Success: clean code provenance and local checks pass.

### Phase 2: Execute the standard workflow

- Task 2.1: Build/publish and install the committed candidate using existing
  `make e2e-setup`. Success: test-observed deployed image matches the candidate.
- Task 2.2: Execute the full E2E suite, without retention or label exclusions.
  Success: real approval/traffic/probe/withdrawal assertions pass.

### Phase 3: Teardown and verify

- Task 3.1: Collect evidence, verify owned parent/managed-group identities, invoke
  ordinary environment cleanup, and independently confirm all four groups absent.
  Success: both E2E and cleanup outcomes are explicitly recorded.

### Checklist and success criteria

- [ ] Task 1.1: Current access/lifetime/quota/absence admission.
- [ ] Task 1.2: Exact source and local runner preparation.
- [ ] Task 2.1: Candidate installed on fresh fixture.
- [ ] Task 2.2: Full live E2E including traffic and withdrawal.
- [ ] Task 3.1: Owned cloud teardown and verified absence.

Complete only after real E2E evidence and independent teardown verification;
accepted configuration alone is never a passing live proof.

## Decisions

- Reuse AKS Standard and shared-VNet topology because this is the existing
  networking/identity E2E fixture, not a new production cluster architecture.
- Use new isolated runner artifacts; do not alter the earlier retention guards.
- Keep all Azure commands subscription-bound and writes within the verified
  lifetime. Cleanup is restricted to the fresh run-owned parent and its managed
  groups, with source/run tags and managedBy checks before teardown.

## Implementation Details

- Session artifacts: `files/afd-standard-live-14/`.
- Intended fresh parent: `fleet-afd-e2e-20261101-0815`.
- Runner invokes standard setup/full-suite tests and owned cleanup under a
  shared execution cutoff; the cleanup reserve is separate from active execution.
- Access initially verified with expiry `2026-11-01T16:12:30.833Z` and execution
  cutoff `2026-11-01T14:57:30.833Z`. Compute quota admitted twelve additional
  vCPUs: DSv5 usage 4/100; total usage 158/1230.
- The quota skill's all-resource helper exceeded the argument limit. Discovered
  single-resource queries plus a scoped Windows-CLI compatibility wrapper
  resolved its CRLF/local-extension argument failures without weakening checks.
- The current Azure server/execution clock subsequently passed that lease.
  Fresh effective-permission verification reports no unrestricted Owner actions.
  Local preparation's Azure CLI option checks correctly stopped at the expiry
  guard. The live launch has not started and no Azure resources were created.

## Changes Made

- Created session-only access, compatibility, preparation, execution and bounded
  cleanup scripts. No production/controller/E2E implementation was changed.
- Created this execution breadcrumb. Source candidate remains the signed commit.

## Before/After Comparison

- Before: prior live diagnostic required manual activation and was not a passing
  automated E2E; its Azure fixture was already deleted.
- Current: updated candidate is cloned locally, but fresh live execution is
  blocked by expired Owner access. Nothing is claimed as live-proven.

## References

- `2026-11-01-0611-afd-activation-without-projection-gates.md`: implemented,
  validated and signed configuration-based activation/lifecycle changes.
- `2026-11-01-0124-afd-retained-live-diagnostic.md`: earlier manual traffic
  evidence and subsequently authorized verified cleanup.
- `test/README.md`: standard setup/test/cleanup and exact auto-approval contract.
- `test/scripts/e2e-config.sh`, `run-e2e.sh`, `cleanup.sh`: default-off retention
  and standard full-suite execution/teardown.
- Current domain-knowledge/specification directories are absent; no applicable
  specification version.
