# GEP-1748 Gateway API Implementation Plan

## Plan status

- **Status:** Proposed
- **Date:** 2026-08-17
- **Design:** [GEP-1748 Gateway API for Fleet Global Ingress](gep-1748-gateway-api.md)
- **Target repository:** `Azure/fleet-networking`
- **Branch:** `rchinchani/gep-1748-gateway-api`

## Delivery principles

1. Preserve `networking.fleet.azure.com/v1alpha1 ServiceImport` without API changes.
2. Use Gateway API resources as the public global-ingress API.
3. Use optional annotations for Azure-specific configuration.
4. Write unit tests before implementation whenever practical.
5. Build a provider-neutral normalized model before ARM reconciliation.
6. Deliver public AFD origins before WAF, PLS, and HTTPS.
7. Keep AFD in a separate hub controller manager.
8. Fail explicitly through Gateway API conditions; never silently downgrade requested behavior.
9. Keep each implementation pull request independently reviewable and testable.
10. Require a Fleet Manager managed hub; hubless Fleet Manager resources need a separate
    architecture.

## Phase 0: Confirm API and dependency baselines

### Task 0.0: Define environment prerequisites

- [x] Map GKE multi-cluster Gateway prerequisites to Fleet, AKS, and Azure.
- [x] Define Fleet membership and ServiceExport/ServiceImport health requirements.
- [x] Define hub/configuration-plane selection, enablement, readiness, and migration procedures.
- [x] Define required Azure resource providers, identity, RBAC, quotas, and network reachability.

**Success criteria**

- A preflight checklist can determine whether the environment is ready.
- The `azure-fleet-afd` GatewayClass remains unaccepted when controller prerequisites are invalid.
- GKE-specific VPC and API requirements are not incorrectly applied to Azure.

### Task 0.1: Select the Gateway API version

- [x] Choose Gateway API `v1.2.1`, which uses the repository's Kubernetes `v0.31.1`
  dependency baseline.
- [x] Record the supported Core and Extended feature set.
- [x] Confirm the selected release includes the status fields used by the controller.

**Success criteria**

- The selected module version builds with the repository.
- A compatibility table identifies supported Gateway API and Kubernetes versions.
- The design's status behavior matches the selected API version.

### Task 0.2: Freeze the annotation contract

- [x] Review annotation names, values, defaults, and ownership.
- [x] Keep resource-group configuration controller-wide; reserve and reject the annotation.
- [x] Define additive versioning and deprecation rules for annotations.

**Success criteria**

- Every annotation has a documented parser, default, validation rule, and failure condition.
- No credential or secret values are permitted.
- Removing Azure annotations leaves portable Gateway routing intent.

### Task 0.3: Define the supported AFD mapping

- [x] Map Gateway listeners, HTTPRoute matches, filters, and backends to AFD resources.
- [x] Enforce Fleet ServiceImport as the only backend kind for the multi-cluster GatewayClass.
- [x] Identify unsupported or semantically incompatible features.
- [x] Define resource and quota limits that must be preflighted before production enablement.

**Success criteria**

- A reviewed mapping table exists.
- Unsupported features have explicit Gateway API condition behavior.
- The initial conformance profile is testable.

## Phase 1: Add test infrastructure and dependencies

### Task 1.1: Add dependency compile tests

- [x] Add a minimal test that registers Gateway API and Fleet networking schemes.
- [x] Verify existing Fleet ServiceImport GVK registration remains unchanged.

**Success criteria**

- The test fails before dependencies and scheme registration are added.
- The test passes after registration.
- No generated Fleet CRD changes are produced.

### Task 1.2: Add Gateway API dependencies

- [x] Add `sigs.k8s.io/gateway-api v1.2.1`.
- [x] Run `go mod tidy`.
- [x] Register Gateway API types with the new manager scheme.

**Success criteria**

- Target packages compile.
- Existing dependency replacements remain valid.
- `ServiceImport` continues using `networking.fleet.azure.com/v1alpha1`.

### Task 1.3: Create controller-manager skeleton

- [x] Add `cmd/hub-gateway-controller-manager`.
- [x] Add health, readiness, metrics, leader election, repository-standard logging, and signal
  handling.
- [x] Add a feature-enable flag for AFD reconciliation.
- [x] Add placeholder Azure configuration loading without creating resources.

**Success criteria**

- The binary builds and starts against envtest.
- Health and readiness endpoints pass.
- No controllers run when the feature is disabled.

## Phase 2: Implement typed annotation handling

### Task 2.1: Write annotation parser tests

- [x] Cover absent values and defaults.
- [x] Cover every accepted SKU and connectivity value.
- [x] Cover malformed Azure resource IDs, hostnames, and probe paths.
- [x] Cover unknown reserved AFD annotation names.
- [x] Cover incompatible combinations.

**Success criteria**

- Tests are table-driven.
- Explicit invalid values never fall back to defaults.
- Error messages identify object, key, value, and expected format.

### Task 2.2: Implement annotation package

- [x] Add constants and typed configuration structures.
- [x] Add Gateway annotation parsing.
- [x] Add ServiceImport annotation parsing.
- [x] Add focused Azure Front Door WAF policy resource ID validation.

**Success criteria**

- All parser tests pass.
- Reconciler packages consume typed values instead of reading annotation maps directly.
- No annotation parsing logic is duplicated.

### Task 2.3: Add admission validation

- [ ] Add validating webhook handlers for Gateway and annotated ServiceImport resources.
- [ ] Keep reconciliation validation as the authoritative fallback.
- [ ] Add webhook chart and certificate configuration.

**Success criteria**

- Deterministic annotation errors are rejected at admission when the webhook is enabled.
- Reconciliation reports the same errors when the webhook is unavailable.
- Existing non-AFD Fleet annotations remain unaffected.

## Phase 3: Build ServiceImport origin resolution

### Task 3.1: Write public-origin resolver tests

- [ ] Resolve a ServiceImport port.
- [ ] Resolve contributing clusters.
- [ ] Resolve public IP/DNS information from internal exports.
- [ ] Apply existing ServiceExport cluster weights.
- [ ] Reject missing ports, empty clusters, invalid exports, and mixed endpoint states.

**Success criteria**

- Resolver behavior is deterministic regardless of Kubernetes list order.
- Tests cover member deletion and conflicting exports.
- Public `ServiceImport` objects are not mutated.

### Task 3.2: Implement origin resolver

- [ ] Add indexes for ServiceImport-to-route and internal-export-to-ServiceImport relationships.
- [ ] Resolve public origins from existing internal resources.
- [ ] Normalize cluster ordering and weights.
- [ ] Expose resolver errors suitable for `ResolvedRefs` conditions.

**Success criteria**

- ServiceImport updates enqueue affected routes.
- Internal export updates enqueue only affected routes.
- No polling is required for Kubernetes state changes.

### Task 3.3: Define the internal normalized model

- [ ] Write model validation and equality tests.
- [x] Add listeners, routes, matches, filters, logical backends, origins, WAF, and probes.
- [ ] Add stable sorting and deterministic naming inputs.

**Success criteria**

- Equal Kubernetes intent produces an equal normalized model.
- Model serialization used in tests is stable.
- Provider packages do not read Kubernetes resources directly.

## Phase 4: Implement GatewayClass and Gateway reconciliation

### Task 4.1: Write GatewayClass controller tests

- [ ] Test recognized and unrecognized controller names.
- [ ] Test invalid controller Azure configuration.
- [ ] Test missing Azure resource-provider registration and insufficient quota preflight results.
- [ ] Test status update conflict retries.

**Success criteria**

- `Accepted` follows Gateway API semantics.
- The controller does not modify classes owned by another implementation.

### Task 4.2: Implement GatewayClass controller

- [ ] Watch `GatewayClass`.
- [ ] Publish accepted status and supported features.
- [ ] Enqueue Gateways when class acceptance changes.

**Success criteria**

- Only `networking.fleet.azure.com/afd` is accepted.
- Status is idempotent and generation-aware where supported.

### Task 4.3: Write Gateway validation tests

- [ ] Test listener protocols, ports, hostnames, and route namespace policy.
- [ ] Test AFD SKU and WAF annotation validation.
- [ ] Test unsupported and conflicting listener configurations.

**Success criteria**

- Invalid configuration produces expected listener and Gateway conditions.
- Unsupported listeners never create Azure resources.

### Task 4.4: Implement Gateway controller foundation

- [ ] Resolve GatewayClass and typed annotations.
- [ ] Generate stable Azure resource names from Gateway UID.
- [ ] Add ownership tags and finalizer rules.
- [ ] Update Gateway and listener status.

**Success criteria**

- A valid Gateway reaches `Accepted=True`.
- A finalizer is added only when Azure resource ownership begins.
- Deleting a never-programmed Gateway does not block.

## Phase 5: Add Azure Front Door provider and public Gateway

### Task 5.1: Define provider interfaces and fakes

- [ ] Add narrow interfaces for AFD profile, endpoint, origin group, origin, route, domain, rule
  set, and security policy operations.
- [ ] Add deterministic fake clients.
- [ ] Add Azure error classification tests.

**Success criteria**

- Controller tests do not require live Azure.
- Retryable, authorization, conflict, not-found, and terminal errors are distinguishable.

### Task 5.2: Add Azure SDK clients

- [ ] Add required `armcdn` SDK dependencies.
- [ ] Initialize clients from the controller's Azure configuration.
- [ ] Apply repository-standard retry, rate limit, and user-agent behavior.

**Success criteria**

- Client initialization tests pass.
- No credentials are logged.
- Azure calls use bounded contexts and actionable wrapped errors.

### Task 5.3: Write Gateway programming tests

- [ ] Test create, update, no-op, drift correction, and deletion.
- [ ] Test ownership collision and foreign-resource rejection.
- [ ] Test partial ARM failure and retry.

**Success criteria**

- Repeated reconciliation is idempotent.
- Foreign resources are not adopted or deleted.
- `Programmed=True` is set only after observing desired state.

### Task 5.4: Implement AFD profile and endpoint reconciliation

- [ ] Create one profile and endpoint per Gateway.
- [ ] Apply stable names and ownership tags.
- [ ] Publish the AFD hostname in Gateway addresses.
- [ ] Implement cleanup through the Gateway finalizer.

**Success criteria**

- A valid HTTP Gateway becomes programmed.
- Deletion removes only owned resources.
- Out-of-band drift in owned fields is corrected.

## Phase 6: Implement HTTPRoute and public origins

### Task 6.1: Write route attachment tests

- [ ] Test parent references and listener selection.
- [ ] Test hostname intersection.
- [ ] Test allowed route namespaces.
- [ ] Test `ReferenceGrant` for cross-namespace ServiceImport.
- [ ] Test unsupported filters and matches.

**Success criteria**

- Route parent status matches Gateway API semantics.
- Unauthorized references set `ResolvedRefs=False`.

### Task 6.2: Write ServiceImport backend tests

- [ ] Test the exact Fleet group and kind.
- [ ] Test that core Kubernetes Service backends are rejected for the multi-cluster GatewayClass.
- [ ] Test port resolution.
- [ ] Test weighted logical backends and per-cluster origin weights.
- [ ] Test missing and deleted ServiceImports.
- [ ] Test references to unsupported groups and kinds.

**Success criteria**

- Fleet ServiceImport behavior follows the GEP-1748 routing model.
- The controller does not claim support for upstream MCS ServiceImport.

### Task 6.3: Implement HTTPRoute controller

- [ ] Build normalized routes from accepted Gateway parents.
- [ ] Resolve Fleet ServiceImport backends.
- [ ] Reconcile AFD origin groups, origins, routes, and supported rule sets.
- [ ] Update `Accepted` and `ResolvedRefs`.

**Success criteria**

- Host and path traffic routes to every valid member origin.
- Route updates do not recreate the AFD profile.
- Removing a route cleans only its owned route resources.

### Task 6.4: Add public-origin integration tests

- [ ] Run controllers with envtest and fake Azure clients.
- [ ] Create ServiceImport/internal export/Gateway/HTTPRoute fixtures.
- [ ] Verify desired Azure operations and final statuses.

**Success criteria**

- End-to-end controller reconciliation succeeds without live Azure.
- Changes to member endpoints update the origin set.

## Phase 7: Add optional WAF attachment

### Task 7.1: Write WAF validation and ownership tests

- [ ] Test absent policy, valid policy ID, malformed ID, inaccessible policy, and incompatible
  SKU.
- [ ] Test annotation removal and Gateway deletion.

**Success criteria**

- WAF policy objects are never created, modified, or deleted.
- Invalid explicit configuration never falls back to no WAF.

### Task 7.2: Implement WAF association

- [ ] Read and validate the existing WAF policy.
- [ ] Create or update the AFD security-policy association.
- [ ] Remove only the association when the annotation is removed.
- [ ] Reflect Azure errors in Gateway status.

**Success criteria**

- WAF is optional.
- The correct custom domains are associated.
- Gateway deletion leaves the external policy intact.

## Phase 8: Add Private Link Service discovery

### Task 8.1: Define backward-compatible internal transport

- [ ] Select `InternalServiceExport` fields or a dedicated internal resource for PLS state.
- [ ] Define resource ID, location, provisioning state, and readiness fields.
- [ ] Define mixed-version defaulting and compatibility.

**Success criteria**

- Public ServiceImport API and generated CRD remain unchanged.
- Old member agents and new hub agents can coexist safely.
- Missing PLS fields mean unknown, not public.

### Task 8.2: Write member PLS discovery tests

- [ ] Test internal load balancer with PLS.
- [ ] Test public Service, missing PLS, provisioning PLS, replacement, and deletion.
- [ ] Test Azure authorization and transient errors.

**Success criteria**

- Discovery reports stable state without leaking credentials.
- PLS replacement triggers a hub update.

### Task 8.3: Implement member PLS discovery

- [ ] Read Service and Azure load balancer/PLS state.
- [ ] Publish PLS resource ID, location, and readiness through internal transport.
- [ ] Add required member-controller Azure permissions and documentation.

**Success criteria**

- Hub state converges after PLS creation, approval, replacement, and deletion.
- Public-origin behavior remains unchanged for non-PLS Services.

## Phase 9: Add AFD Private Link origins

### Task 9.1: Write connectivity-mode tests

- [ ] Test `auto`, `public`, and `private-link`.
- [ ] Test all-public, all-private, mixed, incomplete, and empty origin sets.
- [ ] Test incompatible SKU and asynchronous approval.

**Success criteria**

- Mixed topology is rejected.
- Explicit private mode never downgrades to public.
- `auto` selects only a fully valid topology.

### Task 9.2: Implement Private Link origin reconciliation

- [ ] Add PLS resource ID and location to normalized origins.
- [ ] Configure AFD Private Link origins.
- [ ] Observe private endpoint connection and approval state.
- [ ] Update route and Gateway status through asynchronous transitions.

**Success criteria**

- A fully private ServiceImport becomes programmed.
- Pending approval is visible and retryable.
- PLS deletion removes or disables the affected origin without impacting unrelated backends.

### Task 9.3: Add live Azure PLS end-to-end tests

- [ ] Provision two member-cluster internal Services with PLS.
- [ ] Program an AFD Premium Gateway and HTTPRoute.
- [ ] Approve connections using the documented ownership workflow.
- [ ] Verify traffic, failover, weight changes, and cleanup.

**Success criteria**

- Traffic reaches both private member origins.
- No public origin path is created.
- Cleanup leaves no controller-owned AFD resources.

## Phase 10: Add HTTPS and certificate lifecycle

### Task 10.1: Finalize certificate design

- [ ] Select the first supported certificate mode.
- [ ] Define listener validation and status.
- [ ] Define domain ownership validation and renewal behavior.
- [ ] Document future Key Vault/LUMA integration boundaries.

**Success criteria**

- Certificate identifiers do not expose secret material.
- Ownership and deletion behavior are explicit.
- The design supports safe rotation.

### Task 10.2: Implement AFD-managed certificate support

- [ ] Add HTTPS listener translation.
- [ ] Create and associate custom domains.
- [ ] Observe domain validation and certificate deployment.
- [ ] Publish listener status throughout provisioning.

**Success criteria**

- HTTPS becomes programmed only after the certificate is deployed.
- HTTP-only Gateways remain unaffected.
- Domain or certificate failure is actionable.

## Phase 11: Package, observe, and document

### Task 11.1: Add Helm chart and image targets

- [ ] Add controller deployment, service account, workload identity, RBAC, webhook, service,
  metrics, and configuration.
- [ ] Add Makefile, Dockerfile, and CI targets.
- [ ] Keep installation explicitly disabled by default until promoted.

**Success criteria**

- Chart rendering and installation tests pass.
- Controller has only required Kubernetes and Azure permissions.
- Existing controller charts remain unchanged unless shared helpers require updates.

### Task 11.2: Add metrics and diagnostics

- [ ] Add reconciliation latency, status, Azure operation, throttling, and resource-count metrics.
- [ ] Add structured logs keyed by Kubernetes and Azure resource identity.
- [ ] Add rate-limited events.

**Success criteria**

- Operators can identify validation, Kubernetes, ARM, quota, authorization, and approval failures.
- Logs do not expose secrets.

### Task 11.3: Add examples and operational documentation

- [ ] Add public AFD example.
- [ ] Add optional WAF example.
- [ ] Add private PLS example.
- [ ] Add required Azure roles, limits, troubleshooting, and cleanup documentation.
- [ ] Document the implementation-specific GEP-1748 compatibility statement.
- [ ] Add a GKE-to-Fleet prerequisite comparison and an environment preflight procedure.

**Success criteria**

- Each supported topology has a runnable example.
- Limitations and non-conformance are prominent.
- Troubleshooting maps status reasons to operator actions.

## Phase 12: Conformance, upgrade, and release

### Task 12.1: Run Gateway API conformance tests

- [ ] Run conformance tests applicable to the selected multi-cluster GatewayClass feature set.
- [ ] Add implementation-specific tests for Fleet ServiceImport backend references.
- [ ] Verify core Kubernetes Service backends are rejected as documented.
- [ ] Publish supported and unsupported features.

**Success criteria**

- All claimed Gateway API features pass.
- Fleet ServiceImport support is tested without claiming upstream MCS Extended conformance.

### Task 12.2: Test upgrades and deletion

- [ ] Test controller upgrade with existing programmed Gateways.
- [ ] Test configuration-plane migration using observation-only ownership verification.
- [ ] Test fail-static behavior when ownership cannot be proven.
- [ ] Test annotation default changes are prohibited or safely versioned.
- [ ] Test member/hub mixed versions for PLS.
- [ ] Test deletion during partial Azure outages.

**Success criteria**

- Upgrades do not recreate stable AFD resources.
- Finalizers do not become permanently stuck on terminal authorization failures without operator
  guidance.
- Mixed-version behavior matches the compatibility contract.

### Task 12.3: Complete production-readiness review

- [ ] Review quotas, scale, rate limits, retry budgets, and disaster recovery.
- [ ] Review identity, RBAC, WAF ownership, and private connectivity.
- [ ] Review regional and Azure control-plane failure modes.
- [ ] Define support and rollback procedures.

**Success criteria**

- Release criteria, rollback, monitoring, and ownership are approved.
- The feature flag can be enabled for the target environment.

## Proposed pull request sequence

1. **Design:** design document, implementation plan, and examples of the API contract.
2. **Foundation:** Gateway API dependency, annotation parser, normalized model, and manager skeleton.
3. **Gateway status:** GatewayClass and Gateway validation/status without Azure mutation.
4. **Public AFD:** provider clients, profile/endpoint, HTTPRoute, ServiceImport public origins.
5. **WAF:** existing policy attachment.
6. **PLS discovery:** internal transport and member discovery.
7. **Private origins:** AFD Premium Private Link reconciliation.
8. **HTTPS:** domain and AFD-managed certificate lifecycle.
9. **Packaging:** chart, permissions, observability, examples, and conformance evidence.

Each PR must include focused unit or integration tests and must leave the repository buildable.

## Detailed checklist

- [ ] Phase 0: API, annotation, and AFD mapping decisions complete.
- [ ] Phase 1: Dependencies and manager skeleton complete.
- [ ] Phase 2: Typed annotation handling complete.
- [ ] Phase 3: ServiceImport origin resolution and normalized model complete.
- [ ] Phase 4: GatewayClass and Gateway reconciliation complete.
- [ ] Phase 5: Public AFD Gateway programming complete.
- [ ] Phase 6: HTTPRoute and public ServiceImport origins complete.
- [ ] Phase 7: Optional WAF attachment complete.
- [ ] Phase 8: Member PLS discovery complete.
- [ ] Phase 9: AFD Private Link origins complete.
- [ ] Phase 10: HTTPS and certificates complete.
- [ ] Phase 11: Packaging, observability, and documentation complete.
- [ ] Phase 12: Conformance and production readiness complete.

## Overall success criteria

- Users configure global HTTP(S) ingress with Gateway API resources.
- `HTTPRoute` references the unchanged Fleet `ServiceImport` API.
- Public, WAF-protected, and Private Link origin modes work as documented.
- Invalid annotations and unsupported Gateway features fail explicitly.
- Standard Gateway API status accurately represents Kubernetes and Azure state.
- Existing Service export/import and Traffic Manager functionality remains compatible.
- All claimed Gateway API conformance tests pass.
- Upgrade, deletion, throttling, identity, and failure scenarios are documented and tested.
