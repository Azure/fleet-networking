# Controller-side AFD Private Link approval proposal

## Requirements

- Write a short, separate proposal describing what controller-side approval
  would require; do not implement or enable it.
- Include identity/RBAC, platform authorization, trustworthy request correlation,
  asynchronous reconciliation and verification.
- Fold the proposal into the docs commit, preserving four signed commits.

## Additional comments from user

- "write a short doc about auto approval what needs to happen in a separate proposal doc and add it to the docs commit"

## Plan

### Phase 1: Proposal

- [x] Task 1.1: Review current approval and identity guidance. Success: distinguish
  the existing E2E test-caller helper from the proposed controller capability.
- [x] Task 1.2: Write a short design proposal. Success: cover permissions, trust,
  opt-in, implementation, readiness and validation without implying availability.

### Phase 2: Publication

- [x] Task 2.1: Check documentation and whitespace. Success: local references
  resolve and no runtime files change.
- [x] Task 2.2: Amend the docs commit and re-sign descendants. Success: four
  verified signed commits, unchanged base/messages/authors/runtime tree, clean
  worktree and a backup ref; no push.

Success criteria: a separate, clearly unimplemented proposal is committed in the
docs commit, with no controller permission, behavior or live-resource changes.

## Decisions

- Use `docs/design` beside the existing GEP documents rather than introduce a
  new proposal directory.
- Treat cross-provider request attribution as a prerequisite: a matching request
  description alone is not sufficient production authorization.
- Separate permission to approve from AFD readiness and retain default-off,
  platform-owned configuration.
- The user authorized documentation and amendment only, not implementation,
  role assignment or a live run.

## Implementation Details

- Added proposal: `docs/design/gep-1748-private-link-auto-approval-proposal.md`.
- Reuse the controller's approved Azure identity; do not require a personal
  credential or grant the controller permission to assign itself Azure roles.
- State the permission categories without claiming a finalized minimal custom
  role. Verifying its exact provider actions is an implementation prerequisite.
- Documentation ASCII/whitespace and local-file/heading reference checks passed.
  No runtime tests are needed for this documentation-only change.
- Publication uses an isolated Git index, verifies signed replacements and the
  expected final tree before moving the branch, and retains the old tip under
  `refs/copilot/backups/19936d92-before-auto-approval-proposal`.
- The signed amendment passed: four signatures verified with `git verify-commit`,
  original base/messages/authors/trailers preserved, and only the proposal and
  breadcrumb differ from the prior tree. The worktree was clean after
  publication. This completion record is folded into the same docs commit
  with the identical signature/tree/clean-worktree checks; nothing is pushed.

## Changes Made

- Added the separate proposal and this documentation/amendment breadcrumb.
- No runtime code, chart defaults, credentials, Azure roles or resources changed.

## Before/After Comparison

- Before: only temporary, opt-in E2E test-caller approval is implemented.
- After: a separate proposal records requirements for future controller approval;
  actual production behavior remains unchanged.

## References

- `docs/howtos/gateway-api-afd-configuration.md:110-124`: controller identity,
  AFD permissions and exact-PLS Reader scope.
- `docs/design/gep-1748-gateway-api.md:1200-1208`: identity and security policy.
- `test/e2e/gateway_private_http_network_test.go:41-91`: E2E-only approval helper.
- `.github/.copilot/breadcrumbs/2026-10-31-0008-afd-approval-checkpoint-fix.md`:
  nullable AFD evidence, independent PLS observations and unchanged readiness.
- `.github/.copilot/domain_knowledge` and `.github/.copilot/specifications` are
  absent; no applicable versioned specification is available.
