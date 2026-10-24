# Optional Traffic Manager resilience for Fleet ingress

## Document status

- **Status:** Proposed enhancement; documentation only, not implemented.
- **Date:** 2026-11-01.
- **Customer default:** AFD-only; Traffic Manager resilience is optional.
- **Implementation scope:** Separate from native HTTPS and HTTPRoute path routing.

## Summary

Azure Traffic Manager (ATM) could provide an optional DNS selection layer between
Azure Front Door Premium and an independent standby ingress. This proposal does
not add controller behavior, annotations, APIs, cloud resources or live tests.
Customers do not need ATM to use Fleet's AFD ingress solution.

The base product remains Zero-Public-Exposure Ingress: application origins use
internal load balancers, while AFD is the public client-facing edge. The proposed
fallback must also reach those origins privately. It must not expose member
Services through public load balancer frontends.

Native HTTPS/path-routing implementation and its selected Azure-managed frontend
certificate strategy proceed independently of this proposal. Their current
implementation boundary remains documented in the
[AFD configuration guide](../howtos/gateway-api-afd-configuration.md).

## Customer choices and expectations

| Choice | Requirements | Protection |
| --- | --- | --- |
| AFD-only, default | Base Fleet/AFD/Private Link setup; no ATM profile or standby ingress. | AFD's built-in resilience and available private member origins, not an independent route around an AFD-wide outage. |
| Optional ATM resilience, proposed | Explicit opt-in, an independent ingress, verified private connectivity, DNS/TLS ownership, monitoring and a tested runbook. | DNS can select the alternate ingress for the defined failure scenarios; it is not instantaneous or unconditional failover. |

Extra standby cost, ATM configuration and additional permissions apply only to
customers who opt in. This proposal does not change ordinary AFD readiness,
require ServiceImport annotations, or assume application teams have hub access.
Incomplete opt-in configuration must never be advertised as failover protection.

## Proposed topology

ATM answers DNS queries; it does not proxy application HTTPS requests.

```text
DNS selection:
app.example.com -> Traffic Manager -> AFD endpoint or independent ingress endpoint

Primary HTTPS path:
client -> AFD Premium -> AFD-managed Private Link -> member PLS -> internal LB -> app

Alternate HTTPS path:
client -> independent HTTPS ingress -> separately configured private network -> internal LB -> app
```

Application Gateway WAF is one candidate for alternate ingress because it can
reach private backends through its configured network. Exact regional topology,
private routing to member networks, capacity and resource ownership need separate
design approval. It cannot reuse AFD's managed private endpoints.

Another AFD profile can isolate some profile/configuration failures but is not
an independent failure domain for an AFD-wide service outage. A third-party CDN
must not be assumed able to reach AFD-only Private Link origins.

## Proposed configuration boundary

A future Gateway annotation could reference a typed, platform-approved resilience
policy. No annotation name or manifest in this proposal is a supported API.
The policy would need to identify the application domain, ingress references,
DNS ownership, failover controls, monitoring and TLS/security requirements.
Authorization must cover referenced resources and any cross-namespace access.

Omitting resilience configuration means AFD-only and must provision no ATM or
standby resources. A reference that is present but invalid must return explicit
status rather than silently enabling partial resilience.

The existing Fleet APIs are not sufficient for this composition:

- `TrafficManagerBackend` references only a same-namespace ServiceImport.
- Its controller creates Azure public-IP endpoints and rejects internal load
  balancers. It does not accept AFD/alternate-ingress external hostnames.
- `TrafficManagerProfile` currently supports Weighted routing. New external
  endpoint and operational-control requirements need deliberate API design,
  preserving the existing ServiceImport integration.

A future implementation would reconcile the optional composition and its
ownership/status lifecycle. Whether it provisions the alternate ingress or
references a platform-provisioned ingress remains an open decision.

## HTTPS, DNS and security

Both ingress paths must serve the same application hostname with valid trusted
certificates. AFD frontend certificates do not provision application-origin or
alternate-ingress certificates.

Microsoft's current alternate-ingress HA guide requires bring-your-own
certificates for its pattern. Indirect CNAMEs through ATM can also require
managed-certificate revalidation. This is a certificate-design decision for
opt-in resilience, not a change to the selected AFD-only managed-certificate
approach.

Domain validation, certificate renewal, apex-domain alias support and DNS
caching must be tested. Short TTLs do not guarantee immediate client migration
or move established connections.

The fallback must preserve the supported application routing contract, including
Host/path/query behavior and catalog/checkout isolation. TLS validation, WAF
coverage and private-origin access must be independently verified; a public
member-LB fallback or certificate-check bypass is unacceptable.

## Failure detection and operations

Microsoft recommends controlled primary failover with global outside-in
monitoring for its mission-critical AFD HA pattern. ATM's geographically limited
probes do not establish the health of every AFD anycast location or the actual
customer-domain TLS path.

The future policy/runbook must define who authorizes failover, what evidence
triggers it, how to prevent flapping and how failback is validated. Automated
global failover is not an implied capability of an opt-in annotation.

Operational expectations include DNS cache delays, standby cost, certificate
renewal and regular drills. Configuration readiness is separate from proven
HTTPS delivery and from independently verified fallback readiness.

Future cleanup must detach/delete only verified controller-owned resources in
safe dependency order. Externally owned DNS, certificates, WAF policies or
alternate ingress must not be deleted by implication.

## Future validation requirements

These are acceptance criteria for a separately approved future implementation,
not tests added or executed by this documentation change.

- AFD-only/default-off creates no optional resources and keeps base behavior.
- Invalid policy/reference/authorization returns explicit failure.
- Both paths maintain private member origins and trusted HTTPS.
- Customer-domain catalog/checkout traffic reaches the right service and members
  on both ingress paths, including negative path cases.
- Failover and failback succeed under the intended failure model with observed
  DNS behavior, certificate renewal and monitoring evidence.
- Withdrawals, configuration drift and owned cleanup are verified without
  disrupting unrelated resources or the existing ATM integration.

## Open decisions

- Supported alternate ingress, regional topology and private connectivity.
- Reference-only versus controller-provisioned standby infrastructure.
- Exact policy/annotation contract, ownership and Azure permissions.
- Opt-in certificate provisioning and renewal responsibility.
- Monitoring coverage, controlled failover/failback and recovery objectives.
- Capacity, standby cost and parity with native Gateway API routing.

None of these decisions is a dependency on the initial native HTTPS feature.
No ATM implementation is authorized by this proposal.

## References

- [AFD configuration guide](../howtos/gateway-api-afd-configuration.md).
- [AFD high-availability implementation guide](https://learn.microsoft.com/azure/frontdoor/high-availability):
  independent ingress, controlled failover, probe geography, BYOC and private
  network considerations.
- [AFD domain validation](https://learn.microsoft.com/azure/frontdoor/domain#domain-validation):
  indirect-CNAME certificate revalidation.
- `api/v1beta1/trafficmanagerbackend_types.go`: ServiceImport backend contract.
- `api/v1beta1/trafficmanagerprofile_types.go`: Weighted profile configuration.
- `pkg/controllers/hub/trafficmanagerbackend/controller.go:587-618`: internal
  load balancer rejection and Azure public-IP endpoint construction.
