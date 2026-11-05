# HTTP-only focus and head comparison

## Requirements

- Pause and preserve all HTTPS work.
- Work from the equivalent local HTTP-only head.
- Explain the difference between fork head `52ca7ddf9de33c62e9a4c907fc78d901620e2e58`
  and local HTTP head `a0c29a828c3699ae957c7c22b2243dc3a5e1f8e4`.
- Keep subsequent hardening and merge preparation HTTP-only.

## Additional comments from user

- "stash all the https related changes for now"
- "let's focus only on http and harden and get that merged"
- "a0c29a828c3699ae957c7c22b2243dc3a5e1f8e4 may have been rebased to fix merge conflicts"
- "why not make this rchinchani/gep-1748-gateway-api itself so that http work can be completed first"

## Plan

The user explicitly authorized preservation and HTTP-only selection. No product
implementation changes are included in this comparison.

### Phase 1: Preserve HTTPS

- [x] Task 1.1: Preserve the committed stack with a named branch.
  Success: the complete HTTPS head remains reachable.
- [x] Task 1.2: Stash tracked and untracked local changes.
  Success: all 15 changed/new files are present in the stash.
- [x] Task 1.3: Create and select a separate HTTP-only branch.
  Success: HEAD equals the requested HTTP commit and the worktree is clean
  before creating this record.

### Phase 2: Compare HTTP heads

- [x] Task 2.1: Verify the remote fork head and inspect tree/history differences.
  Success: exact remote SHA and all changed files are accounted for.
- [x] Task 2.2: Separate feature differences from upstream maintenance.
  Success: compare actual content, not commit-message similarity.

### Completion checklist and success criteria

- [x] Phase 1 / Tasks 1.1-1.3: HTTPS preserved; HTTP-only checkout selected.
- [x] Phase 2 / Tasks 2.1-2.2: exact differences verified.

HTTP implementation hardening, tests, live proof and merge readiness remain
separate work. This comparison does not establish that HTTP is ready to merge.

## Decisions

- Preserve committed HTTPS on a branch rather than attempting to put commits
  into a worktree stash or resetting the original branch.
- Do not pop the HTTPS stash into the HTTP-only checkout.
- Prefer local `a0c29a8` as the hardening baseline because it retains upstream
  maintenance without changing HTTP feature code.
- No force-push, commit amendment, PR update or remote mutation was performed.
- At the user's subsequent request, moved the original local feature branch
  to the HTTP head and selected it. HTTPS remains on the preserved branch and
  in the stash. Rebase/conflict resolution can explain changed history; tree
  comparison establishes final content, not the exact historical operations.

## Implementation Details

- Current branch: `rchinchani/gep-1748-gateway-api`, at `a0c29a8`.
- The initially selected `copilot/afd-http-hardening` also remains at `a0c29a8`.
- Preserved HTTPS branch: `copilot/afd-https-paused-20261109`, at `f7883b6`.
- Original branch `rchinchani/gep-1748-gateway-api` was moved from `f7883b6`
  to `a0c29a8` only after verifying the named HTTPS preservation branch.
- Stash: `61f9cf02549862b6c1f04c778407b4bec89b66c2` (`stash@{0}` at creation).
- Stash message: `Paused HTTPS E2E defaults and AKV plan before HTTP-only hardening 2026-11-09`.
- Fork `rchinchani/gep-1748-gateway-api` still points to `52ca7dd`.

## Changes Made

- Preserved committed and uncommitted HTTPS work and switched to HTTP-only.
- Added only this breadcrumb to the HTTP worktree.
- Completed tree and ancestry comparison; no source code changed.

## Before/After Comparison

Both HTTP heads contain identical feature source, APIs, charts, tests, scripts,
documentation and dependency checksums. The local head reorganizes the older
foundation/feature history into four docs/feature/build/test commits atop
`5945d92`, rather than the older base/history reachable from `52ca7dd`.

The complete tree delta is 16 files, 113 insertions and 73 deletions:

- Go directive, five Docker builders and CI Go versions: 1.26.9 to 1.26.10.
- GitHub Actions pinned to full SHAs; associated action version updates.
- CodeQL grouping/synchronization and Dependabot cooldown.
- Weekly Trivy issue reuse and superseded-report closure.

These correspond to six upstream maintenance commits between common ancestor
`71e5f86` and `5945d92`: `f2a951e`, `632ed6d`, `60a9cad`, `f29d0e9`,
`326187d`, and `5945d92`. The feature-added gateway Dockerfile also carries
the matching Go builder update.

Validation used exact `git diff`, `git log --left-right`, `git merge-base`,
`git ls-remote`, and stash inspection. A feature-directory diff across `api`,
`charts`, `cmd`, `config`, `docs`, `examples`, `hack`, `pkg`, `test`, `tools`
and `go.sum` returned no differences. No runtime tests were needed for this
Git-only comparison; no hardening validation is claimed.

## References

- Exact trees/history: `52ca7dd` and `a0c29a8`.
- Preserved HTTPS history: `a0c29a8..f7883b6`.
- Prior HTTPS defaults and AKV plan breadcrumbs are preserved in the named
  stash, not carried into the HTTP-only checkout.
- This task changes Git state only; no domain or specification behavior changes.
