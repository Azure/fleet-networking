# Actionable Copilot instructions

## Requirements

- Replace stale global guidance with verified commands and architectural boundaries.
- Distinguish local validation from Azure-dependent E2E tests.
- Limit breadcrumbs and plan approval to non-trivial changes.
- Open a standalone documentation PR against main without unrelated changes.

## Additional comments from user

- Approved the main-based PR plan on 2026-11-06.
- Gateway/AFD must be marked in-progress, not documented as shipped on main.

## Plan

### Phase 1: Verify the base

- [x] Task 1.1: Check main's Makefile, architecture, test prerequisites, and available docs.
  Success: every documented command and local reference exists on the PR base.

### Phase 2: Update instructions

- [x] Task 2.1: Replace the global instructions with concise commands, boundaries, and collaboration rules.
  Success: no missing specification paths, obsolete golint guidance, or blanket test command.

### Phase 3: Validate and publish

- [x] Task 3.1: Check Markdown links, whitespace, command targets, and PR scope.
  Success: documentation checks pass and no unrelated files enter the commit.
- [x] Task 3.2: Commit, push, and open the PR against main.
  Success: the remote PR contains only the instructions and this breadcrumb.

## Decisions

- Use a separate worktree based on origin/main; the user's Gateway checkout is dirty.
- Do not copy branch-only Gateway paths or commands into main's instructions.
- Keep existing Makefile targets authoritative; explain envtest and mutating targets.
- No runtime tests are needed for documentation-only changes.

## Implementation Details

- Added a command matrix based on main's Makefile, with mutating-target warnings.
- Explained envtest-backed controller packages and separate Azure E2E approval.
- Recorded current hub/member/Traffic Manager boundaries and qualified Gateway/AFD as in-progress.
- Replaced missing specification paths with existing docs and conditional branch-specific guidance.
- Limited planning approval and breadcrumbs to non-trivial work.
- Initial local-link validation caught the legacy, nonexistent MCS controller path;
  corrected it to the actual multiclusterservice package before publication.
- Validation passed: 22 local links/anchors, 9 matched Make-command references,
  Markdown fence/newline structure, focused package example, requested content checks,
  and git diff --check. The external Go style guide returned HTTP 200.
- No Go builds or runtime tests were run: only Markdown documentation changed.

## Changes Made

- Created this breadcrumb before implementation.
- Rewrote [.github/copilot-instructions.md](../../copilot-instructions.md).
- Published [PR #442](https://github.com/Azure/fleet-networking/pull/442) against main
  from the isolated docs/actionable-copilot-instructions branch.

## Before/After Comparison

- Before: generic directory inventory and mandatory planning ceremony for all tasks.
- After: concise, branch-aware commands, architecture invariants, validation selection, and safety rules.

## References

- [Makefile](../../../Makefile): command targets and envtest setup.
- [README](../../../README.md): current hub/member architecture.
- [E2E guide](../../../test/README.md): Azure prerequisites and resource lifecycle.
- [Exporting services](../../../docs/concepts/ExportingService/README.md): service export/import flow.
- [DNS load balancing](../../../docs/concepts/DNSBasedGlobalLoadBalancing/README.md): Traffic Manager behavior.
- No domain-knowledge or specification directories exist on this PR base.

## Completion criteria

All phase tasks are checked, documentation checks pass, and the standalone PR is open.
