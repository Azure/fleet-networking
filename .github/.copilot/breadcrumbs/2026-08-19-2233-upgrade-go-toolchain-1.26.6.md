# Go 1.26.6 Toolchain Upgrade

## Requirements
- Upgrade the same files changed by PR #403 from Go 1.25.13 to Go 1.26.6.

## Additional comments from user
- Upgrade same files from PR #403 to 1.26.6.

## Plan
1. Update the Go directive, Docker builder images, and CI Go version variables.
2. Verify all targeted references are updated and no stale references remain.
3. Run applicable validation, secret scanning, and CodeQL checks.

## Decisions
- Keep the scope identical to PR #403: the four Dockerfiles, six workflows, and `go.mod`.

## Implementation Details
- Updated the Go directive and Docker builder image tags to `1.26.6`.
- Updated the six workflow `GO_VERSION` values to `1.26.6`.

## Changes Made
- `go.mod`
- `docker/hub-net-controller-manager.Dockerfile`
- `docker/mcs-controller-manager.Dockerfile`
- `docker/member-net-controller-manager.Dockerfile`
- `docker/net-crd-installer.Dockerfile`
- `.github/workflows/go.yml`
- `.github/workflows/build-publish-mcr.yml`
- `.github/workflows/unit-integration-tests.yml`
- `.github/workflows/trivy.yml`
- `.github/workflows/e2e-tests.yml`
- `.github/workflows/publish-image.yml`

## Before/After Comparison
- Go 1.25.13 references in the PR #403 files are now Go 1.26.6.

## References
- PR #403: Go toolchain bump from 1.25.12 to 1.25.13.
