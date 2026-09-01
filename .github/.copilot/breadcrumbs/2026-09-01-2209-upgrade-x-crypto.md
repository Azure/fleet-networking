# Upgrade golang.org/x/crypto to v0.55.0

## Requirements

- Upgrade `golang.org/x/crypto` from v0.53.0 to v0.55.0.
- Refresh the Go module checksums.
- Validate the updated dependency.

## Additional comments from user

- None.

## Plan

### Phase 1: Dependency update

1. [ ] Update the `golang.org/x/crypto` requirement to v0.55.0.
   - Success criteria: `go.mod` resolves `golang.org/x/crypto` at v0.55.0.
2. [ ] Refresh module metadata.
   - Success criteria: `go.sum` contains the checksums for v0.55.0 and no stale selected version.

### Phase 2: Validation

3. [ ] Run the relevant Go formatting, vetting, and test targets.
   - Success criteria: validation passes without regressions caused by the dependency update.
4. [ ] Scan modified files for secrets and perform a security review.
   - Success criteria: no actionable secret-scanning or security findings remain.

## Decisions

- Use Go module tooling to resolve the requested version and generate canonical checksums.
- Limit changes to module metadata unless validation identifies a compatibility issue.

## Implementation Details

- Pending plan approval and implementation.

## Changes Made

- Created this breadcrumb before making the dependency change.

## Before/After Comparison

- Before: `golang.org/x/crypto` is selected at v0.53.0.
- After: it will be selected at v0.55.0 with refreshed checksums.

## References

- `go.mod`: current Go module requirements.
- `go.sum`: Go module dependency checksums.
- `Makefile`: repository validation targets.
