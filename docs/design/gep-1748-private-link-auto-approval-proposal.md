# Proposal: controller-side AFD Private Link approval

**Status: proposed, not implemented.** Production approval remains manual.
The existing default-off E2E helper uses the test caller's identity, not the
controller's. This proposal does not authorize role assignments or a live run.

## Motivation

For Zero-Public-Exposure Ingress, automate approval of AFD Premium connections
to member PLS/internal load balancers without requiring a person at each
checkpoint. AFD's shared-link status can be null while the PLS already shows a
Pending or Approved connection; request discovery must not depend on AFD Pending.

## Prerequisites

- **Identity:** use the hub Gateway controller's approved managed/workload
  identity. No personal credentials or new client secret are required.
- **Permissions:** an authorized PLS owner must grant that identity PLS read
  and private endpoint connection read/update/approval permissions on each
  explicitly permitted PLS. Existing Reader plus CDN Profile Contributor is
  insufficient. Prefer a custom role with the required Network resource-provider
  actions verified during implementation. Network Contributor scoped to each
  PLS is a broader alternative, not a subscription-wide requirement. The
  controller must not grant itself roles.
- **Platform opt-in:** introduce a default-off controller setting, exposed
  through the chart, with a platform-owned allowed-target policy. Workload
  annotations must not authorize approval. Member PLS owners must explicitly
  delegate this trust to the hub identity.
- **Trustworthy attribution:** agree with the AFD/PLS teams how to verify that a
  connection belongs to the expected owned AFD profile/origin and exact permitted
  PLS. A request description or AFD-managed endpoint subscription alone is not
  sufficient. Unverifiable or ambiguous attribution must require manual approval.

## Implementation and validation

1. Add bounded PLS observation and approval to reconciliation, separate from
   AFD graph readiness. Validate current Gateway ownership, backend authorization
   and private frontend identity before any write.
2. Pin the verified connection ID and revalidate it before approval. Approve only
   Pending requests; skip Approved requests and never overwrite Rejected,
   Disconnected or replaced connections. Examine every expected member.
3. Independently read approval after a write. Requeue with bounded backoff for
   propagation; surface permission failures and attribution blockers through
   status/events and sanitized logs. Disabling the feature stops new approvals;
   it does not automatically revoke existing connections.
4. Preserve current fail-closed readiness: PLS Approved alone does not mean AFD
   is ready. Require the complete owned enabled graph and real traffic/probe
   evidence. Investigate persistent null AFD status/bootstrap ordering separately.
5. Add regressions for opt-out/no writes, denied access, spoofed/ambiguous
   requests, replacement IDs, staggered approvals, cancellation and retries.
   Prove the scoped custom role and installed-controller flow in a separately
   authorized live test, including negative cases and owned cleanup, before
   removing or replacing the temporary E2E helper.

Before implementation, resolve the cross-provider attribution contract, exact
custom-role actions and allowed-target configuration. Approval automation
must not ship without those decisions.

## References

- [Current design](gep-1748-gateway-api.md#asynchronous-approval)
- [Controller identity and permissions](../howtos/gateway-api-afd-configuration.md)
- [AFD Private Link lifecycle](https://learn.microsoft.com/azure/frontdoor/private-link)
- [Network Contributor permissions](https://learn.microsoft.com/azure/role-based-access-control/built-in-roles/networking#network-contributor)
