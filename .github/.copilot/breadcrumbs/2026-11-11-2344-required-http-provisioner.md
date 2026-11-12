# Required Gateway HTTP provisioner

## Requirements and approval

- Review asks whether the HTTPProvisioner nil check in resolution-error handling
  exists only for tests and recommends fake provisioners instead.
- Production already requires the provider when AFD is enabled; disabled mode
  registers no Gateway controller.
- User approved: "ok approve the plan and amend after local validation".
- Preserve deletion, ownership, withdrawal, conflict retry, error propagation,
  and stale-readiness clearing. No live Azure operations are authorized.

## Plan and success criteria

1. [x] Require an HTTP provisioner at Gateway controller registration and test
   rejection before accessing the manager.
2. [x] Remove nil-dependent resolution-error handling in Reconcile; test clearing
   stale readiness and retaining the original error with a fake provisioner.
3. [x] Migrate the registered envtest controller to a fake HTTP provisioner and
   keep assertions consistent with the production provisioning path.
4. [x] Run local controller/manager tests, formatting, vet, and lint where supported;
   explicitly record any integration or tooling limitations.
5. [x] Review the final diff and amend HEAD with a clean worktree.

## Decisions and boundaries

- Keep the change focused on the required registration contract and the reviewed
  resolution-error branch. Existing disabled-graph and observation helper unit
  seams are not a supported production mode and need not be removed wholesale.
- The previous runtime nil branch could return before clearing published readiness
  in incomplete test configurations. Registered controllers will now fail startup
  without the provider, and resolution failures always clear readiness.
- The envtest suite currently registers an observation-only controller. Migrate
  it rather than bypassing the required provider contract.

## References

- `pkg/controllers/hub/gateway/controller.go`
- `pkg/controllers/hub/gateway/http_provisioning_test.go`
- `pkg/controllers/hub/gateway/suite_test.go`
- `pkg/controllers/hub/gateway/observation_integration_test.go`
- `cmd/hub-gateway-controller-manager/azure.go`
- PR #400 review discussion.

## Implementation and validation

- `controller.go`: registration now requires cleanup, the HTTP provisioner, and
  the uncached reader. Resolution errors restore pre-resolution status, clear
  readiness/addresses unconditionally, and retain the original error.
- `controller_test.go`: isolate all three missing-dependency cases, with fake
  provisioners in the other cases. The new missing-provisioner regression failed
  before implementation because setup proceeded to manager validation.
- `http_provisioning_test.go`: existing full-provider failure regressions now also
  assert original provider/resolution errors remain discoverable with `errors.Is`.
- `suite_test.go`: register the envtest Gateway controller with its fake HTTP
  provisioner, without bypassing the startup contract.
- `observation_integration_test.go`: implement the full fake provider interface
  with mutex-protected state. Test pending approval, programmed hostname publication,
  withdrawal on stale/invalid/denied intent, watch-driven recovery, stale observation
  suppression with current provisioned origin identity, and finalizer cleanup.
  Do not manually inject cleanup ownership that provisioning now acquires itself.
- Initial full-suite runs exposed obsolete observation-only message/Event assertions
  and assumptions that the separate observation and provisioning calls take an atomic
  snapshot. Correct the assertions for the full-provider path: count provisioning
  calls for provisioning transitions, and validate the captured provisioned origin
  after an identity replacement rather than requiring an incidental ready Event.
  A transient unprogrammed status during replacement may or may not be persisted.

### Final local checks

Run under WSL Ubuntu with `GOTOOLCHAIN=go1.26.10`, using existing repository tools
and cached Kubernetes 1.33.0 envtest assets. This avoids the Windows global linter's
old build toolchain; the configured repository v1.64.7 linter works here.

- `hack/tools/bin/setup-envtest use -i -p path 1.33.0`: passed; returned cached
  local test binaries, used as `KUBEBUILDER_ASSETS`.
- `go test -race -count=1 -timeout=12m ./pkg/controllers/hub/gateway ./cmd/hub-gateway-controller-manager`:
  passed both packages (including the real local API-server/etcd integration suite).
- `go vet ./pkg/controllers/hub/gateway ./cmd/hub-gateway-controller-manager`: passed.
- `hack/tools/bin/golangci-lint run ./pkg/controllers/hub/gateway/... ./cmd/hub-gateway-controller-manager/...`:
  passed with the repository configuration.
- `hack/tools/bin/goimports -local go.goms.io/fleet-networking -l` on all five
  changed Go files: passed, no formatting differences.
- `git diff --check`: passed. Reviewed patch scope; amend preserves the existing
  message and Copilot trailers. Commit/worktree verification is reported in the
  final handoff.

### Remaining boundaries

- Disabled-graph/observation-only helper unit tests remain deliberate isolated
  seams; registered controllers can no longer omit the full provisioner.
- Integration uses synthetic provider outcomes, not live Azure. No Azure resource,
  permission, propagation, or application-traffic claim is established by these checks.
- Amend only; no push is requested.
