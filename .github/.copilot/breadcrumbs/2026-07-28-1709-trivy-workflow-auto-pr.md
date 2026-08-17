# Trivy Workflow Auto-PR via Copilot Agent

## Requirements

- Update the trivy workflow to run on a daily schedule at 6:00 AM UTC.
- When CVEs are found, create a GitHub Issue assigned to Copilot coding agent.
- Copilot coding agent will open a PR with the dependency fix.
- Keep existing triggers (push to main, tag creation, manual dispatch).

## Additional comments from user

- Dependabot is not running frequently enough to catch CVEs in a timely manner.
- Prefer Copilot coding agent to handle the remediation via an issue assignment.

## Plan

### Phase 1: Update Workflow Triggers
- Task 1.1: Add `schedule` cron trigger for 6:00 AM UTC daily.

### Phase 2: Add CVE Remediation Job
- Task 2.1: Change trivy scan output to JSON format (in addition to table) to parse results.
- Task 2.2: Add a step that parses trivy JSON output and creates a GitHub Issue assigned to Copilot.
- Task 2.3: The issue body should include CVE IDs, affected packages, and severity so Copilot can act on it.
- Task 2.4: Add `issues: write` permission for the workflow.

### Checklist

- [ ] Task 1.1: Add schedule cron trigger
- [ ] Task 2.1: Change trivy output to JSON for parsing
- [ ] Task 2.2: Add step to create GitHub Issue assigned to Copilot
- [ ] Task 2.3: Issue body includes actionable CVE details
- [ ] Task 2.4: Add issues:write permission

### Success Criteria

- Workflow runs daily at 6:00 AM UTC.
- When HIGH/CRITICAL CVEs are found, a GitHub Issue is created and assigned to Copilot.
- Existing push/tag/manual triggers continue to work (those still fail on CVEs without creating issues).

## Decisions

- Use JSON output from trivy to parse vulnerability details programmatically.
- Only create issues on scheduled runs (not on push/tag) to avoid noise during development.
- Assign issue to `copilot` user which triggers the Copilot coding agent.

## Implementation Details

- Added `schedule` trigger with cron `0 6 * * *` (daily 6 AM UTC).
- Added `issues: write` permission.
- Changed trivy output from `table` with `exit-code: 1` to `json` with file output.
- Added `check-vulns` step that parses JSON to detect any vulnerabilities.
- On non-scheduled runs: fails the build with a table summary (same behavior as before).
- On scheduled runs: builds a markdown issue body with a CVE table and creates a GitHub Issue assigned to `copilot`.
- Deduplication: checks if an issue with today's title already exists before creating.

## Changes Made

- `.github/workflows/trivy.yml` — Added schedule trigger, JSON output, vuln check logic, and Copilot issue creation.

## Before/After Comparison

**Before:** Workflow ran on push/tag/manual, output table format, failed build on CVEs.
**After:** Additionally runs daily at 6 AM UTC. On scheduled runs, creates a GitHub Issue assigned to Copilot with CVE details so it can open a fix PR automatically.

## References

- Existing workflow: `.github/workflows/trivy.yml`
- Copilot coding agent: GitHub Copilot can be assigned to issues to automatically create fix PRs
- [Trivy action](https://github.com/aquasecurity/trivy-action)
