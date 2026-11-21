# Standard Service support when AFD is enabled

## Requirement and decision

User explicitly requested removing the separate localServiceOrigins enable flag.
AFD enablement now authorizes both local Service companions and existing private
ServiceImport origins. The AFD flag remains disabled by default.
Retain explicit node resource-group configuration and leader election.

## Implementation

- Removed manager enable-local-service-origins, chart
  afd.localServiceOrigins.enabled, and E2E ENABLE_LOCAL_SERVICE_ORIGINS.
- AFD startup always initializes/registers companion discovery/controller and
  requires its node group, leader election, and initialized discovery.
- AFD chart always supplies companion Service/EndpointSlice privileges and the
  retained afd.localServiceOrigins.resourceGroup setting.
- E2E AFD setup verifies the hub node group and grants scoped Reader access
  regardless of conformance selection; no additional approval privileges.
- Readiness always includes Service/EndpointSlice informers for enabled AFD.
- Updated directly affected test assertions, installation flags, and documentation.
  Historical breadcrumbs describe the former opt-in rollout and remain unchanged.

## Validation

- [x] Manager/startup/readiness and offline installer tests.
- [x] Helm render/lint contracts (HELM_NO_PLUGINS=1).
- [x] Final race/vet and configuration checks: AFD defaults off, five E2E scenarios
  retained, conformance stays optional, chart schema/ADO YAML parse, script syntax,
  relative documentation links and git diff --check pass.
- No pipeline or live Azure run requested or started; existing running pipeline
  uses its pinned commit and is unaffected by these uncommitted changes.
