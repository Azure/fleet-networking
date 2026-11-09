# Fix SA1019: migrate from GetEventRecorderFor to GetEventRecorder

## Requirements
- Fix the staticcheck SA1019 finding: `mgr.GetEventRecorderFor is deprecated: this uses the old events API and will be removed in a future release. Please use GetEventRecorder instead.`
- Preserve existing event behavior (types, reasons, reporting controller) and keep events deliverable to the API server.

## Additional comments from user
- Follow-up after reviewing PR #445: fix the CI `unit-integration-tests` failures introduced by the Fleet/controller-runtime dependency bump, and the stale Fleet CRD path in the e2e bootstrap script.

## Plan
1. Phase 1: Analysis
   - Task 1.1: Locate all `GetEventRecorderFor` call sites and `record.EventRecorder` consumers; success means every affected file is identified.
   - Task 1.2: Compare old (`core/v1`) and new (`events.k8s.io/v1`) recorder semantics (signature, validation, RBAC, dedup); success means migration risks are recorded.
2. Phase 2: Tests first
   - Task 2.1: Add table-driven unit tests for a note-truncating recorder wrapper; success means tests cover short, exact-limit, over-limit, and multi-byte notes.
3. Phase 3: Implementation
   - Task 3.1: Add `pkg/common/eventrecorder` with a wrapper that truncates notes to the 1024-byte API server limit.
   - Task 3.2: Switch reconciler `Recorder` fields from `record.EventRecorder` to `events.EventRecorder` and update every `Eventf` call to the new signature (`related` = `nil`, plus an UpperCamelCase `action`).
   - Task 3.3: Replace `GetEventRecorderFor` with `GetEventRecorder` in `cmd/*/main.go` and envtest suites; wrap the traffic manager recorders with the note-truncating recorder.
   - Task 3.4: Replace `record.NewFakeRecorder` with `events.NewFakeRecorder` in unit tests.
   - Task 3.5: Add `events.k8s.io` `events` RBAC (kubebuilder markers, regenerated `config/rbac/role.yaml`, and the three Helm charts).
4. Phase 4: Validation
   - Task 4.1: `make staticcheck` reports no SA1019 for `GetEventRecorderFor`.
   - Task 4.2: `go vet`, `make lint`, and goimports formatting pass.
   - Task 4.3: Unit and envtest integration tests for `./pkg/controllers/...` and `./cmd/...` pass; adjust expectations only for intrinsic new-API behavior changes.

### Checklist
- [x] Task 1.1: Locate call sites.
- [x] Task 1.2: Record migration risks.
- [x] Task 2.1: Unit tests for the note-truncating recorder.
- [x] Task 3.1: Note-truncating recorder.
- [x] Task 3.2: Reconciler recorder type and `Eventf` calls.
- [x] Task 3.3: `GetEventRecorder` wiring.
- [x] Task 3.4: Fake recorders in unit tests.
- [x] Task 3.5: RBAC for `events.k8s.io`.
- [x] Task 4.1: staticcheck clean for SA1019.
- [x] Task 4.2: vet/format (golangci-lint cannot run in this toolchain; see Implementation Details).
- [x] Task 4.3: Tests pass.
- [x] Follow-up: fix the CI failures caused by the dependency bump (see Implementation Details).

Success criteria: no `GetEventRecorderFor` usages remain, staticcheck no longer reports this SA1019, all controllers emit `events.k8s.io/v1` events with the RBAC needed to create them, and the targeted tests pass.

## Decisions
- `GetEventRecorder` returns `events.EventRecorder` (`events.k8s.io/v1`), not `record.EventRecorder`; its `Eventf` takes `regarding, related, type, reason, action, note`. `related` is `nil` everywhere because there is no secondary Kubernetes object, matching upstream `go.goms.io/fleet` usage.
- `action` is required by API server validation (non-empty, at most 128 characters). Use short UpperCamelCase verbs describing what the controller did (for example, `DeleteEndpoints`, `CreateOrUpdateProfile`), following upstream fleet's inline style.
- The API server rejects `events.k8s.io/v1` events whose note is longer than 1024 bytes; legacy `core/v1` events had no such limit. Traffic manager events embed Azure SDK errors (request URL plus response body), which can exceed 1 KB, so their recorders are wrapped to truncate notes. Other controllers only embed Kubernetes object names, which stay well below the limit.
- The new broadcaster creates and patches events through the `events.k8s.io` API group, so RBAC must grant `create` and `patch` on `events.k8s.io/events`. Keep the existing core `events` rules because controller-runtime leader election still uses the legacy recorder.
- Hub-side example Roles for member agents are unchanged; member agents record events with the member cluster manager only.
- The `events.k8s.io/v1` broadcaster deduplicates by type, action, reason, reporting controller/instance, and the full regarding reference (including `resourceVersion`), not by message. A repeat after the regarding object's `resourceVersion` changes (for example, after a status update) therefore creates a new Event object instead of bumping a count. This is the library's standard behavior (kube-scheduler has the same), so the traffic manager integration tests now compare the distinct events in first-emitted order instead of the raw Event objects. The alternative, stripping `resourceVersion` from the regarding reference, was rejected because it changes event payload semantics beyond this fix.
- `make manifests` also rewrote three CRD descriptions due to existing drift from upstream `k8s.io/api` docs; those unrelated changes were reverted.

## Implementation Details
- `pkg/common/eventrecorder`: `NewNoteTruncatingRecorder(events.EventRecorder)` formats the note, truncates it to at most 1024 bytes (ending in `...`) without splitting a multi-byte rune, and forwards it. Its table-driven test covers short, no-arg, exact-limit, over-limit, and multi-byte-boundary notes.
- Reconcilers (`hub/serviceimport`, `hub/membercluster`, `hub/trafficmanagerprofile`, `hub/trafficmanagerbackend`, `member/serviceexport`, `member/internalserviceexport`, `multiclusterservice`) now take `events.EventRecorder`. Every `Eventf` passes `nil` for `related` plus an action:
  - Traffic manager profile: `GetProfile`, `CreateOrUpdateProfile`, `DeleteProfile` (constants).
  - Traffic manager backend: `GetProfile`, `CreateOrUpdateEndpoint`, `DeleteEndpoint`, `DeleteEndpoints` (constants).
  - Hub serviceimport: `UpdateStatus`, `DeleteServiceImport`.
  - Member serviceexport: `ValidateService`, `ValidateServiceExport`, `UnexportService`, `ExportService`.
  - Member internalserviceexport: `CheckExportConflict`.
  - multiclusterservice: `UnimportService`, `ImportService`, `UpdateStatus`.
- `cmd/*/main.go` and the envtest suites call `GetEventRecorder`; the traffic manager recorders are wrapped with `eventrecorder.NewNoteTruncatingRecorder`.
- Unit tests use `events.NewFakeRecorder` instead of `record.NewFakeRecorder`.
- Validation so far:
  - `make staticcheck`: all 13 `GetEventRecorderFor` findings are gone. Three pre-existing, out-of-scope SA1019 findings for `ctrl.Result.Requeue` remain.
  - `go build ./...` and `go vet ./cmd/... ./pkg/...` pass, and goimports reports no changes.
  - `helm template` renders all three charts.
  - The pinned golangci-lint v1.64.7 cannot run here because it was built with Go 1.24 and the module targets Go 1.26.10 (existing tooling limit).
- Test results:
  - The first `go test ./cmd/... ./pkg/... -race` run failed the traffic manager envtest suites with event-list mismatches caused by the resourceVersion-based dedup above; later failures in those Ordered containers cascaded from leftover objects. The helpers were adapted as described.
  - `multiclusterservice` `TestHandleUpdate` also failed on the PR's dependency-bump commit. It was initially misreported here as pre-existing, but it passes on `main`; the bump to controller-runtime v0.23.3 caused it (see the follow-up below).
  - After the helper change, `go test ./pkg/controllers/hub/trafficmanagerprofile/ ./pkg/controllers/hub/trafficmanagerbackend/ -race` passes. All other `./cmd/...` and `./pkg/...` packages passed in the full run, and `go vet -tags=e2e ./test/...` passes.
- Follow-up CI fixes for PR #445 (caused by the dependency bump, not the events migration):
  - The `hub/membercluster` and `member/internalmembercluster/v1beta1` envtest suites and `test/scripts/bootstrap.sh` loaded Fleet CRDs from a hardcoded `go.goms.io/fleet@v0.14.0` module-cache path. CI no longer downloads v0.14.0, so `BeforeSuite` failed there; it passed locally only because v0.14.0 was still cached. All three now point at `fleet@v0.18.16`, which has the same CRD file layout.
  - controller-runtime v0.23's fake client clears `TypeMeta` on typed objects, as the real client does. `TestHandleUpdate` now ignores `TypeMeta` in its object comparisons.
  - This also exposed a latent controller bug. `isServiceImportOwnedByOthers` compared owner references against `mcs.APIVersion`/`mcs.Kind`, which are empty after `updateMultiClusterLabel` writes the MCS. The conflict check then never matched: the reconcile returned an error instead of setting the status, and only recovered on a later pass. It is now a `Reconciler` method that resolves the GVK with `apiutil.GVKForObject(mcs, r.Scheme)`, the same source `controllerutil.SetControllerReference` uses to write the owner reference.
  - `go test ./pkg/controllers/hub/membercluster/ ./pkg/controllers/member/internalmembercluster/v1beta1/ ./pkg/controllers/multiclusterservice/ -race` passes; `bash -n test/scripts/bootstrap.sh`, `go vet`, goimports, and staticcheck are clean.

## Changes Made
- Added `pkg/common/eventrecorder/eventrecorder.go` and `eventrecorder_test.go`.
- Updated the 7 reconciler files listed above (recorder type, `Eventf` signature, `events.k8s.io` kubebuilder RBAC markers on reconcilers that emit events).
- Updated `cmd/hub-net-controller-manager/main.go`, `cmd/member-net-controller-manager/main.go`, and `cmd/mcs-controller-manager/main.go`.
- Updated 6 envtest `suite_test.go` files and 3 unit `controller_test.go` files.
- Updated `validateEmittedEvents` in the two traffic manager `controller_integration_test.go` files.
- Regenerated `config/rbac/role.yaml` (controller-gen merged `""` and `events.k8s.io` into one `events` rule).
- Added an `events.k8s.io` `events` rule (`create`, `patch`) to the hub, member, and MCS Helm chart ClusterRoles.
- Follow-up: bumped the Fleet CRD paths to `fleet@v0.18.16` in `pkg/controllers/hub/membercluster/suite_test.go`, `pkg/controllers/member/internalmembercluster/v1beta1/suite_test.go`, and `test/scripts/bootstrap.sh`. `pkg/controllers/multiclusterservice/controller.go` now resolves the MCS GVK from the scheme in the ownership check, and `controller_test.go` ignores `TypeMeta`.

## Before/After Comparison
- Before: controllers use the deprecated `GetEventRecorderFor` and emit legacy `core/v1` events; staticcheck reports 13 SA1019 findings for it.
- After: controllers use `GetEventRecorder` and emit `events.k8s.io/v1` events with explicit actions; staticcheck reports none for it. Long Azure error notes are truncated instead of being rejected, and RBAC grants the new API group.

## References
- `sigs.k8s.io/controller-runtime` v0.23.3 `pkg/recorder/recorder.go`: deprecation notice and `GetEventRecorder` API.
- `k8s.io/client-go` v0.35.8 `tools/events`: new recorder, broadcaster (Create/Patch sink, series dedup key), and `FakeRecorder`.
- `k8s.io/kubernetes` release-1.35 `pkg/apis/core/validation/events.go`: action/reason/reportingInstance limits of 128 and note limit of 1024 for new-style events.
- `go.goms.io/fleet` v0.18.16: upstream already uses `GetEventRecorder` with `nil` related objects and inline UpperCamelCase actions.
- `.github/.copilot/domain_knowledge` and `.github/.copilot/specifications`: absent from this checkout.
