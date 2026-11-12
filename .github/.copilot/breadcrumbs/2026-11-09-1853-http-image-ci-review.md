# Remove brittle image dry-run CI assertions

## Requirements

- Validate the concern about "Verify existing image delivery contracts".
- Commit the focused fix atop current HTTP HEAD without amending or pushing.
- Provide a suggested review response after fixing the comment.

## Additional comments from user

- Review: "this test looks brittle. I am thinking if it is better not to add this."
- "also give a review response after fixing each review comment"

## Plan

Status: approved. User selected "Approve implementation and commit".

### Phase 1: Simplify and validate

- [x] Task 1.1: Remove only the workflow step that greps Make dry-run output.
  Success: no command-string assertions or temporary plan files remain.
- [x] Task 1.2: Preserve actual amd64/arm64 image build, runtime, nonroot,
  startup/health and loopback registry export/pull checks.
  Success: diff contains no changes to those behavioral checks or Make targets.
- [x] Task 1.3: Run actionlint and whitespace checks, inspect the final diff,
  then commit with required trailers and supply a review response.
  Success: workflow validates and a new focused commit follows `4a626ae`.

### Completion checklist and success criteria

- [x] Phase 1 / Tasks 1.1-1.3: brittle step removed, useful coverage retained,
  workflow checked, commit created.

## Decisions

- The comment is valid: dry-run string matching tests command spelling and
  Make recipe shape rather than whether the delivered image works.
- Retain actual build/export/pull/runtime coverage. Removing the dry-run checks
  intentionally drops static assertions that every component is listed in the
  aggregate image/push targets; do not claim identical coverage.
- No replacement test asserting the deleted step's absence is needed.
- Workflow-only removal requires workflow validation, not a local Docker build
  of unchanged steps. No Azure operations or HTTPS work are included.

## Implementation Details

Remove the single named step from `.github/workflows/unit-integration-tests.yml`.
No dependencies, production behavior, Make targets or runner matrix change.

## Changes Made

Removed only the 34-line dry-run workflow step; retained image checks are
unchanged. `/root/.local/bin/actionlint .github/workflows/unit-integration-tests.yml`
passed through WSL, including embedded shell validation. `git diff --check`
passed and final diff inspection confirmed only the intended step was removed.
No actual Docker builds or Azure runs were performed.
This record accompanies the separate review-fix commit atop `4a626ae`.

## Before/After Comparison

Before: generated Make command plans are grepped in addition to real image tests.
After: the image job relies on real build, registry and runtime checks.

## References

- `.github/workflows/unit-integration-tests.yml`: reviewed step and retained
  amd64/arm64 behavioral image checks.
- `Makefile`: aggregate image/push and dedicated Gateway image targets.
- `2026-11-09-1848-http-afd-ci-review.md`: preceding HTTP review fix.
- No domain/specification contract changes are involved.
