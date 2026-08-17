# Fix Trivy Database Repository

## Requirements

- Replace the stale Trivy vulnerability database mirror in `.github/workflows/trivy.yml`.
- Use `mcr.microsoft.com/oss/v2/aquasecurity/trivy-db` for every image scan.
- Preserve the existing `CRITICAL,HIGH` severity and `ignore-unfixed: true` scan policy.
- Create a pull request containing the durable workflow fix.

## Additional comments from user

- The mirror tag was created on 2026-06-29 and returned no findings with the workflow's filters.
- The official MCR database returned seven High findings with the same filters.
- `GO-2026-5932` remains intentionally excluded by the current severity and unfixed-vulnerability policy.

## Plan

### Phase 1: Verify the regression and expected configuration

- [x] **Task 1.1: Add a configuration assertion before implementation.**
  - Confirm that all Trivy scan steps currently use the stale mirror.
  - Success criteria: the assertion identifies exactly three stale repository references.

### Phase 2: Update the workflow

- [x] **Task 2.1: Replace every stale database repository reference.**
  - Change each `TRIVY_DB_REPOSITORY` value to `mcr.microsoft.com/oss/v2/aquasecurity/trivy-db`.
  - Success criteria: all three scan steps use the official MCR repository and no stale references remain.

### Phase 3: Validate and publish

- [x] **Task 3.1: Validate the workflow change.**
  - Re-run the configuration assertion and inspect the resulting diff.
  - Success criteria: the workflow contains exactly three official repository references and no unrelated changes.
- [x] **Task 3.2: Commit and create the pull request.**
  - Commit the workflow and breadcrumb updates, push the branch, and open a GitHub pull request.
  - Success criteria: the pull request clearly explains the stale database root cause and durable fix.

### Phase 4: Resolve upstream merge conflict

- [x] **Task 4.1: Merge the current upstream `main` branch.**
  - Preserve upstream's daily JSON scan, vulnerability summary, and issue-creation workflow changes.
  - Success criteria: the branch contains upstream commit `556170bd5cdbf36270f05bc9c67317fb03061480` and has no unresolved files.
- [x] **Task 4.2: Retain the official Trivy database repository.**
  - Resolve `.github/workflows/trivy.yml` so all three image scans use `mcr.microsoft.com/oss/v2/aquasecurity/trivy-db`.
  - Success criteria: exactly three official references and zero stale mirror references remain.
- [x] **Task 4.3: Validate and publish the conflict resolution.**
  - Inspect the merge diff, push the merge commit, and verify upstream PR #399 is mergeable.
  - Success criteria: GitHub reports no merge conflict and the PR contains both upstream workflow behavior and the intended database fix.

### Detailed checklist

- [x] Phase 1 / Task 1.1 completed.
- [x] Phase 2 / Task 2.1 completed.
- [x] Phase 3 / Task 3.1 completed.
- [x] Phase 3 / Task 3.2 completed.
- [x] Phase 4 / Task 4.1 completed.
- [x] Phase 4 / Task 4.2 completed.
- [x] Phase 4 / Task 4.3 completed.

### Overall success criteria

- Every Trivy scan uses the official MCR database repository.
- Existing scan filters remain unchanged.
- The configuration assertion passes.
- A focused pull request is open.

## Decisions

- Update all three scan steps because each independently configures the Trivy database repository.
- Do not change severity or unfixed-vulnerability filtering because those settings intentionally exclude `GO-2026-5932`.
- Use a direct configuration assertion because the change is isolated to workflow environment values.
- Resolve the conflict by preserving all newer upstream workflow behavior and applying only the database repository change on top.

## Implementation Details

- Phase 1 confirmed that lines 79, 95, and 110 of `.github/workflows/trivy.yml` independently use the stale mirror.
- Phase 2 updated the hub, member, and MCS controller image scans to use the official MCR Trivy database repository.
- Phase 3 validation counted three official repository references and zero stale mirror references; the diff contains only the intended workflow substitutions and this breadcrumb.
- Phase 3 published the fix in upstream GitHub pull request Azure/fleet-networking#399; the mistakenly opened fork pull request #1 was closed.
- Conflict inspection found only `.github/workflows/trivy.yml`; upstream added scheduled JSON scanning and automated issue creation after this branch diverged.
- Phase 4 merged upstream `main` at `556170bd5cdbf36270f05bc9c67317fb03061480` and resolved the workflow conflict without dropping upstream behavior.
- The resolved workflow contains three official repository references, zero stale mirror references, and zero conflict markers.
- GitHub reports upstream pull request #399 as mergeable; its `BLOCKED` state reflects remaining checks or review requirements rather than merge conflicts.

## Changes Made

- Added this breadcrumb to document requirements, evidence, plan, and decisions.
- Verified the pre-change workflow contains exactly three stale database repository references.
- Replaced all three stale mirror references with the official MCR repository.
- Verified the final repository reference counts and reviewed the focused diff.
- Committed the fix and opened upstream GitHub pull request Azure/fleet-networking#399.
- Preserved upstream's daily JSON scans, vulnerability checks, summaries, and issue creation while resolving the database repository values.
- Pushed the merge resolution and confirmed GitHub no longer reports a conflict.

## Before/After Comparison

- **Before:** Trivy downloads vulnerability data from the stale `mcr.microsoft.com/mirror/ghcr/aquasecurity/trivy-db` mirror.
- **After:** Trivy downloads vulnerability data from `mcr.microsoft.com/oss/v2/aquasecurity/trivy-db`.

## References

- `.github/workflows/trivy.yml`: Current image-scanning workflow and scan policy.
- GitHub pull request Azure/fleet-networking#399: Publishes the durable database repository fix.
- Repository domain knowledge: no files were present under `.github/.copilot/domain_knowledge`.
- Repository specifications: no files were present under `.github/.copilot/specifications`.
- User-provided scan evidence: establishes that the stale database, rather than image scanning, caused missed findings.
