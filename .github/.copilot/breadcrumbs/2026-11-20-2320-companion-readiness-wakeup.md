# Companion readiness wakeup fix

## Requirements and evidence

User requested getting the companion design working after ADO run 192465779 failed.
Retained diagnostics show the Gateway stopped reconciling at 21:07:51 while CCM
later supplied companion ILB IP 10.224.0.62. The harness subsequently awaited an
AFD profile that was never created. Preparation timed out after 75 minutes.
No PLS approval or upstream HTTP assertion failure was observed.

## Plan and success criteria

1. [x] Map verified companion Service/EndpointSlice events to original backend routes.
2. [x] Retry valid pending local origins, including when Gateway status is unchanged.
3. [x] Reject mismatched ownership/source UID mappings; preserve normal dependency paths.
4. [x] Add focused mapping/recovery/invalid-reference regressions and update related docs.
5. [x] Run affected race-enabled controller suites and vet.

## Implementation

- Shared companion label/manager constants in objectmeta; values unchanged.
- Dependency mapping checks controller-owner kind/name/UID and current original Service UID.
  Mirror events additionally verify their companion Service controller-owner identity.
  Mapping only schedules work; current intent/private-origin verification still governs writes.
- Final Gateway evaluation tracks ReferenceValid BackendUnavailable from local resolution.
  Its existing progress requeue now remains active for this condition, including stable status.
  Invalid references, disabled local origins, and unsupported routing do not acquire this retry.
- No application Service mutation, Azure permission expansion, approval change, or provider changes.

## Validation

- Targeted companion mapping and pending/recovery/invalid-reference tests passed.
- Vet and whitespace checks passed.
- `GOTOOLCHAIN=go1.26.10 KUBEBUILDER_ASSETS=.../1.35.0-linux-amd64
  go test -mod=readonly -race -count=1 ./pkg/controllers/hub/gateway
  ./pkg/controllers/hub/gatewayresolver ./pkg/controllers/hub/httproute
  ./pkg/controllers/hub/localservice`: passed, including Gateway envtest.
- No live resources or pipeline runs started. A new live run requires separate authorization.

## References

- [ADO run](https://msazure.visualstudio.com/CloudNativeCompute/_build/results?buildId=192465779&view=results)
- [Original implementation](./2026-11-20-1512-local-service-private-origins.md)
