# Refresh ACR authentication before the late E2E app push

## Requirements

- Refresh registry authentication immediately before the sample-app image push.
- Preserve failure reporting, performance-test skipping and existing scope/RBAC.
- Add focused offline regression coverage and fold changes into relevant commits.
- Do not modify the running cleanup or start another live run.

## Additional comments from user

- "ok, fix it then and amend appropriate commit"

## Plan

### Phase 1: Tests first

- [x] Task 1.1: Execute the actual bootstrap app-publication block with isolated
  Azure/Docker stubs. Simulate expired auth after the build and login failure;
  retain performance-test skip coverage. Success: the regression fails before
  the fix, and failed reauthentication prevents any push.

### Phase 2: Surgical fix

- [x] Task 2.1: Add `az acr login` immediately before the existing app push with
  a brief token-lifetime comment. Success: fresh authentication is obtained after
  long provisioning/build work, without retries, sleeps or expanded permissions.

### Phase 3: Validate and amend

- [x] Task 3.1: Run focused and complete offline script tests, Go formatting,
  vet/golint/pinned lint, Bash syntax and scoped ShellCheck comparison. Success:
  tests/checks pass and no new shell diagnostics appear, without Azure mutations.
- [x] Task 3.2: Fold bootstrap/harness changes into the build commit and relevant
  breadcrumbs into the docs commit. Success: four signed commits verified,
  original base/authors/messages/trailers preserved, clean worktree and backup;
  no push.

Success criteria: a late sample-app push receives refreshed registry auth,
failed login remains fatal, offline regressions pass and amendments preserve
the four-commit structure.

## Decisions

- Refresh after the Docker build, not just before it: build/provisioning time
  must not consume the new registry token's lifetime.
- Reuse existing `az acr login` behavior and the existing offline test helpers.
  Do not enable ACR admin credentials or add broader caller/controller roles.
- Include the related attempt-11 breadcrumb in the docs amendment; no unrelated
  worktree changes were present.

## Implementation Details

- User approved: "Approve; implement and amend".
- Bootstrap currently logs in at `test/scripts/bootstrap.sh:61`, but pushes the
  app much later at line 423 without refreshing.
- Docker credential-file write was 01:25:54Z; attempt-11 app push failed at
  04:27:20Z. The documented registry token lifetime is three hours.
- The first test extraction matched an earlier performance conditional; corrected
  it to select the actual app-publication section. The corrected red run failed
  specifically with stale registry authentication and missing login-before-push;
  the performance-test skip case already passed.
- Added one registry login between the existing Docker build and push, with a
  brief three-hour-token comment. No retry, permission or image changes.
- The green regression executes the actual app-publication block and proves
  that build-expired auth is refreshed, a denied login prevents pushing, and
  the performance-test path performs neither build/login nor push.
- Attempt-11 resources were independently confirmed absent at 04:42:56Z;
  its failed outcome is preserved in the related breadcrumb.

Validation:

| Command | Result |
| --- | --- |
| `goimports -w test/scripts/afd_commands_test.go` | Passed |
| `go test -count=1 -run '^TestAFDAppImageAuthentication$' ./test/scripts` | Passed after expected stale-auth red regression |
| `AFD_VALIDATE_LOCAL_TOOLS=true go test -count=1 ./test/scripts` | Passed; complete offline harness and installed CLI help checks |
| `go vet ./test/scripts` | Passed |
| `golint -set_exit_status test/scripts/afd_commands_test.go` | Passed |
| `golangci-lint run ./test/scripts` | Passed with existing pinned version |
| `bash -n test/scripts/bootstrap.sh` | Passed |
| `shellcheck -x -P test/scripts test/scripts/bootstrap.sh` | Failed on existing diagnostics; baseline and changed script each report 194, with no new findings or findings on added lines |
| `git -c core.safecrlf=false --no-pager diff --check` | Passed |

Do not broaden this fix to the pre-existing shell warnings. Registry token
expiry remains a high-confidence diagnosis from timing/documentation, not a
direct inspection of the old credential. No live retry was performed.

The signed amendment passed: bootstrap and its harness tests are in the build
commit; this and the related live breadcrumb are in the docs commit. Four
signatures were verified, with unchanged base/authors/messages/trailers, exact
final-tree preservation and a clean worktree. Backup:
`refs/copilot/backups/19936d92-before-acr-refresh-amend`. This completion record
is folded into the same docs commit with the same checks. No push.

## Changes Made

- `test/scripts/bootstrap.sh`: refresh ACR login after the app build, before push.
- `test/scripts/afd_commands_test.go`: three isolated publication regressions.
- This breadcrumb and the attempt-11 breadcrumb: plan, diagnosis, validation,
  cleanup completion and signed-amendment tracking.

## Before/After Comparison

- Before: the late app push relies on the initial, potentially expired login.
- After: refresh immediately before that push; abort if refresh fails.

## References

- `.github/.copilot/breadcrumbs/2026-10-31-0120-afd-live-auto-approval-proof.md`:
  attempt-11 failure and authentication timeline.
- `test/scripts/afd_commands_test.go:62-86`: offline bootstrap failure harness.
- `test/scripts/afd_test.go:226-274`: isolated environment/script/stub helpers.
- https://learn.microsoft.com/azure/container-registry/container-registry-authentication:
  three-hour registry-token lifetime and reauthentication guidance.
- Domain knowledge/specification folders are absent; no applicable versioned
  specification is available.
