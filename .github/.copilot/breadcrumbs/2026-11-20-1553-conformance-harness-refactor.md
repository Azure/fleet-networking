# Shared conformance harness

## Scope and approval

User approved a test-only refactor. Preserve all existing production edits and
the single required HTTP test. No Azure operations or live tests.

## Plan

1. [x] Replace hard-coded base selection with explicit configurable upstream fixture keys.
2. [x] Discover current test Gateway/Service references from staged upstream manifests.
3. [x] Reuse shared multi-origin approval, request warmup, and Gateway cleanup.
4. [x] Keep one required test list, reuse upstream fixtures/assertions, and document exceptions.
5. [x] Validate discovery/selection/approval edge cases offline and compile the live harness.

## Implementation

- `requiredTests` remains a one-entry upstream baseline. Each case gets its declared
  feature set, fresh base fixtures, original upstream manifests, shared preparation,
  and original upstream assertions.
- `baseFixtureKeys` replaces the fixed four-resource selector; missing/duplicate/
  ambiguous selections are explicit errors.
- `OriginTargets` discovers every nonzero forwarding reference with Gateway scope,
  route scope, parent selection, selected rule, and service port. No Gateway or
  backend name is hard-coded in approval/warmup.
- Shared approval checks every target before warmup, retaining per-reference
  connection/target/Gateway identity checkpoints. Current route references and
  namespace attachment policy are rechecked before approval.
- Warmup uses actual rule paths, exact headers/query values, methods, and valid
  hostname intersection. Regex/HTTPS requirements remain explicit exceptions.
- Cleanup checks namespace UID and deletes/waits for fixture Gateways before
  backend withdrawal, then waits for owned namespace deletion between cases.
  Cleanup errors retain backend fixtures rather than claiming success.
- Denied/invalid references are not approved or warmed; their unchanged upstream
  negative assertions still run. No test-result fallback or skip list was added.

## Validation

Passed with `GOTOOLCHAIN=go1.26.10`:

- Race-enabled normal conformance helper tests.
- Tagged, race-enabled fixture/target/approval/cleanup/warmup tests; this also
  compiles the live harness without executing its Azure test.
- Vet of the tagged conformance package.
- Relative documentation link checks and `git diff --check`.

Added checks cover multiple Gateway/backend scopes and ports, zero-weight omission,
missing/duplicate prerequisites, blocked Gateway finalizers retaining backends,
replaced namespace refusal, and selected-rule/hostname warmup correctness.
No production files were changed during this refactor; previous production edits
remain intact and uncommitted.

## Boundaries

Domain/certificate fixture adaptations remain explicit. Infrastructure and product
features are not implemented by this refactor. The first live HTTP pass is still
unverified. Do not turn setup failures or unsupported input into success.
The shared approval phase covers the staged graph. Tests changing/creating new
backend references inside their own execution still need an approval-observer hook.
Per-case preparation/cleanup budgets may require larger command/pipeline timeouts
when the required list grows.
