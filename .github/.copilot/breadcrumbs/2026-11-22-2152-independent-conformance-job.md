# Independent AFD conformance job

## Requirements and approval

The user requested separate provisioning for AFD E2E and conformance and approved
the plan to use independent jobs with fresh environments, diagnostics, and cleanup.
All five existing E2E scenarios remain enabled; conformance remains opt-in.
No live run, commit, or push is requested by this implementation approval.

## Plan and success criteria

- [x] Extract the existing provisioning/test/cleanup job into a shared template.
- [x] Add an opt-in independent conformance job with a distinct resource group.
- [x] Update conformance documentation.
- [x] Validate template inputs, test separation, resource-group isolation, and cleanup.

## Decisions

Reuse existing AKS provisioning rather than introduce a second setup mechanism.
Conformance provisions its own shared-VNet AFD environment and does not run the
ordinary E2E suite. Neither job depends on the other's test result.

## Before and after

Before: conformance followed successful ordinary E2E in the same AFD job.
After: five ordinary E2E jobs and an optional sixth conformance job, each owning
its own environment and cleanup. The additional environment increases live cost.

## References

- [Pipeline](../../../.pipeline/e2e-tests.yaml)
- [Conformance guide](../../../test/conformance/README.md)

## Implementation and validation

Changed the pipeline entry point, added the shared job template, and updated the
conformance guide. The dedicated job uses scenario `conformance`, a distinct
resource-group/builder/artifact name, and a 450-minute job budget. Its test step
has the existing 150-minute budget and Go timeout of 140 minutes.

Local YAML validation passed: all five E2E scenario definitions remain unchanged,
the sixth job is opt-in, test commands are mutually exclusive, and there is no
cross-job dependency. Always-run diagnostics and tagged, run-scoped cleanup are
preserved. Bash syntax checks passed for all nine embedded scripts; relative
documentation links and `git diff --check` passed.

ADO server-side template expansion and live provisioning have not been run.
The new environment still provisions the existing full shared-VNet setup;
reducing its cluster footprint is outside this change.
