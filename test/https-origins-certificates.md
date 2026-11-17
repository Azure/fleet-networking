# First-party certificate fixtures for end-to-end AFD TLS

**Status: test preparation for a proposal, not an implemented TLS E2E command.**
Use this guide with the [engineering scenario matrix](../docs/design/gep-1748-https-origins-proposal.md#e2e-testing-requirements)
and [E2E environment guide](README.md). Obtain explicit authorization for the
subscription, resource group, Kubernetes contexts, certificate/DNS operations
and create/test/cleanup scope before provisioning anything. No live operations
are authorized by this document.

This runbook belongs in `docs(https)` so reviewers can assess real certificate
prerequisites before implementation. Executable fixtures, opt-ins and assertions
belong in the later `test(https)` commit. No TLS E2E flag exists today.

## What the first-party test owner must obtain

| Fixture | Owner and required result |
|---|---|
| Client-facing hostname/certificate | Platform operator supplies the observed generated AFD HTTPS URL. Azure provides/renews its edge certificate; do not request an origin certificate for an `azurefd.net` name. |
| Origin SNI hostname | Test/DNS owner reserves a precise DNS name under an approved, controlled domain. `store-origin.example.com` is documentation only, not a name you can enroll. |
| Member 1 serving certificate/key | Approved certificate owner supplies a publicly trusted server certificate with that DNS SAN and its matching application-usable private key. |
| Member 2 serving certificate/key | Separate issuance/key for the same DNS identity is preferred; sharing private keys across members is unnecessary. |
| Renewal and negative fixtures | Certificate/test owners plan permitted issuance, reload, rotation and isolated failure cases before the run. |

Keep the public repository free of real tenant/subscription/service identifiers,
internal enrollment instructions, internal DNS names, certificate request
records, private keys, PFX files, rendered Secrets and credentials. Use the
authorized internal run record for actual identities, enrollment references,
owners and custody evidence; never record key material there either.

## 1. Reserve an origin name, not a public application endpoint

1. Ask the first-party service's DNS/certificate owner for a dedicated,
   non-sensitive test name in an approved domain for the target cloud. Record
   the owning service, authorized requester, renewal contact and retirement
   date in the internal run record.
2. Obtain authorization to enroll that exact DNS SAN and to perform the
   issuer's domain-control validation. Domain ownership, issuer authorization
   and access to a member namespace are different permissions.
3. Use a publicly certifiable DNS name, not a Kubernetes `.svc` name, private
   IP address, invented suffix or the generated AFD endpoint name. Public
   certificate issuance can disclose the name through certificate transparency.
4. Arrange the approved domain-control validation method, such as DNS-based
   validation where the issuer supports it. A public validation record does
   not require exposing the application. Do not publish a public member
   LoadBalancer or add a public origin to complete issuance.
5. Declare this same origin identity in a Service-targeted BackendTLSPolicy in
   each member, with the proposed
   `networking.fleet.azure.com/export-to-fleet: "true"` annotation. This opts into
   Fleet export, not exclusive ownership by Fleet. Fleet transports validated
   non-secret intent and generates the hub policy from current eligible-member
   agreement and explicit verified exclusions.
   The application actor touches
   only members; no manual hub-policy apply or identity-change handoff is needed.
   Platform Gateway/HTTPRoute still explicitly authorizes ingress.

AFD's exact Private Link/PLS association supplies private connectivity. The
origin hostname supplies TLS SNI/name validation; neither a DNS record nor
domain enrollment alone proves that private path. Verify the supported
DNS/Private Link combination in the live test.

## 2. Obtain an approved publicly trusted certificate

### Can first-party testers use OneCert?

**Only through an authorized enrollment path that issues an AFD-trusted public
server certificate.** Do not assume a certificate is suitable because it was
obtained through OneCert or is trusted on a corporate workstation. An internal
enterprise-CA chain is not a valid positive fixture for this proposal.

The first-party certificate owner must use the current internal OneCert/domain
registration and enrollment runbooks to:

1. Verify/register the controlled domain and authorize the owning service and
   requesting identity for that namespace.
2. Select an approved public-CA server-TLS enrollment profile, not an internal
   CA or client-authentication-only profile. Confirm the resulting root is
   allowed by AFD's current trust requirements, not merely by local policy.
3. Request the exact DNS SAN, server-authentication use and an approved key
   algorithm/size. For identity-rotation tests, prearrange certificates covering
   both the old and new names in every participating member.
4. Choose approved key custody and workload delivery before enrollment. This
   example application needs access to a usable certificate/private-key pair;
   a downloadable public `.cer` file alone cannot configure its TLS server.
5. Complete issuer validation, retrieve the leaf/intermediate chain and arrange
   authorized private-key delivery to the application. Record issuance,
   expiry/renewal, delivery/reload and teardown owners.

OneCert-specific portal URLs, issuer/profile identifiers and internal access
procedures are deliberately not reproduced in this public repository. Resolve
them with the service's certificate owner using the authoritative internal
runbook. If that path cannot supply a public chain or approved key delivery,
record the fixture as blocked; do not select an internal issuer or bypass
certificate checks just to make the test run.

### Key Vault/CSR and key delivery considerations

Use the organization's supported enrollment integration where available. For
an approved external-signing workflow, [Key Vault CSR creation and merge](https://learn.microsoft.com/azure/key-vault/certificates/create-certificate-signing-request)
supports generating a key/CSR, having the authorized CA sign it and merging
the signed chain back with the original key. A CSR/public certificate contains
no private key; sending a CSR to the approved issuer does not mean sending
the private key. A non-integrated issuer does not provide automatic renewal
merely because the certificate is stored in Key Vault.

If policy permits export for this disposable fixture, retrieve the certificate
and private key through the approved secret-delivery path into an
access-restricted location **outside the checkout**, never into terminal/CI
output. [Key Vault certificate export](https://learn.microsoft.com/azure/key-vault/certificates/how-to-export-certificate)
requires an exportable key and authorized secret access; downloading the public
certificate is not equivalent. Non-exportable/HSM-backed keys cannot simply be
copied into a Kubernetes TLS Secret. If export is prohibited, use an approved
application/key integration that can serve TLS, or mark this illustrative
Secret-based fixture blocked. Do not weaken key policy or give Fleet controllers
Key Vault/private-key access.

Deliver the result as:

- `ORIGIN_LEAF_FILE`: public leaf certificate in PEM format.
- `ORIGIN_INTERMEDIATES_FILE`: required intermediate certificates in PEM format.
- `ORIGIN_CERT_CHAIN_FILE`: leaf followed by required intermediates.
- `ORIGIN_KEY_FILE`: matching application-usable PEM private key.
- `AFD_ALLOWED_ROOTS_FILE`: public trust anchors independently checked against
  AFD's current allowed roots, not an arbitrary corporate trust bundle.

Set paths in an approved private workspace and disable shell tracing/session
capture for secret handling. Do not place PEM/key contents, PFX passwords or
secret values in command arguments, source files, run reports or environment
variables. Restrict file access; arrange deletion according to the test's key
custody policy.

## 3. Validate before deploying

The following Bash/OpenSSL checks inspect supplied files; they neither issue a
certificate nor prove live AFD acceptance. Set the path variables above and
`ORIGIN_TLS_HOSTNAME` to the approved test name. Use supported OpenSSL tooling.

```bash
# Inspect public metadata, SAN coverage and validity; never print the key.
openssl x509 -in "${ORIGIN_LEAF_FILE}" -noout \
  -subject -issuer -dates -ext subjectAltName -fingerprint -sha256
openssl x509 -in "${ORIGIN_LEAF_FILE}" -noout \
  -checkhost "${ORIGIN_TLS_HOSTNAME}"
openssl x509 -in "${ORIGIN_LEAF_FILE}" -noout -checkend 604800
openssl verify -purpose sslserver -verify_hostname "${ORIGIN_TLS_HOSTNAME}" \
  -CAfile "${AFD_ALLOWED_ROOTS_FILE}" \
  -untrusted "${ORIGIN_INTERMEDIATES_FILE}" "${ORIGIN_LEAF_FILE}"
```

`-checkend 604800` requires at least seven days of remaining validity for this
example; choose a larger owner-approved margin for the planned run/rotation.
Inspect the served full chain as well as these individual files. A local
verification result is a preflight, not proof that AFD currently accepts it.

Compare digests of the public keys; these commands support RSA and EC without
printing private-key material:

```bash
set -o pipefail
openssl x509 -in "${ORIGIN_LEAF_FILE}" -pubkey -noout |
  openssl pkey -pubin -outform DER | openssl dgst -sha256
openssl pkey -in "${ORIGIN_KEY_FILE}" -pubout -outform DER |
  openssl dgst -sha256
```

Require both pipelines to succeed and the digests to match; differing output
is a hard stop, not a reason to disable certificate validation.

## 4. Deploy and prove both TLS connections

Provision each member's `store-origin-tls` through approved delivery, then
follow the [proposed customer setup](../docs/howtos/gateway-api-afd-configuration.md#proposed-setup-order).
The application actor needs only member credentials, including local policy
permissions. Fleet owns the generated hub BackendTLSPolicy; the platform operator
owns Gateway/HTTPRoute authorization and endpoint handoff. Require matching
current explicitly opted-in member policies and inspect source
provenance/projection ownership, complete source inventory and explicit exclusion
reasons/isolation evidence. Require exactly one applicable policy per eligible
member Service. Unmarked policies for other implementations must not be
implicitly exported but still count toward duplicate-target detection.
Test duplicate appearance/removal and opt-in removal separately from
certificate failures; Fleet must not choose a winner or delete extra policies.
Prove exact administrative origin isolation and healthy-member continuation
for local policy failures. No eligible members, conflicting otherwise-valid TLS
identities or unverifiable isolation require affected-route withdrawal. AFD
runtime certificate failures remain separate from controller-observed policy
errors; health-probe failure alone does not isolate an origin.
Do not copy the member key into the hub, AFD or public MCS fields.

Before going through AFD, use the [private preflight](../docs/howtos/gateway-api-afd-configuration.md#useful-private-preflight-not-afd-proof)
from an authorized private client to check actual certificate presentation,
selected Service port and SNI/Host behavior. Then use the operator-supplied
generated hostname with normal verification:

```bash
# AFD_HOSTNAME is supplied by the platform operator; no hub access is needed.
curl --verbose --fail-with-body --show-error --get \
  --data-urlencode "proof=$(date +%s)-${RANDOM}" \
  "https://${AFD_HOSTNAME}/"
```

Require edge certificate/name/chain verification plus correlated member
TLS/SNI/request logs, exact private bindings, HTTPS probes and both-member
responses. Client HTTPS alone does not prove origin TLS. Fresh HTTP requests
must not forward to the application. Do not use `-k`, replace AFD trust,
override client SNI/Host or manually repair an ARM graph to obtain a pass.
Record only sanitized public certificate metadata/fingerprints in evidence.

## 5. Prepare rotation, failures and retirement

| Fixture or operation | Preparation and guardrail |
|---|---|
| Same-name renewal | Obtain a second trusted issuance/key before the first expires; prove member reload and fresh AFD handshakes, not only pooled connections. |
| Origin-name rotation | Reserve/enroll the new name and deploy dual-name coverage everywhere, then update each member policy. Conflicting otherwise-valid identities withdraw the AFD route; after eligible agreement Fleet updates the hub projection automatically. Record exclusions and require fresh agreement before re-admission. Verify new SNI/traffic before retiring old coverage. |
| Wrong-name failure | Use a certificate for another authorized test name in an isolated fixture. Do not request someone else's identity. |
| Untrusted chain | Use a disposable local test CA only for the negative case; never add it as AFD trust or call it a positive fixture. |
| Expired certificate | Arrange an authorized retained expired test certificate/key or wait for an approved short-lived fixture to expire. Do not change clocks or attempt to backdate a public-CA issuance. |
| Incomplete chain | Use an isolated chain that demonstrably fails at AFD; cached/fetched intermediates can invalidate a naive negative test. |
| Cleanup | Collect sanitized evidence, have the operator withdraw hub routing, confirm no delivery and generated-policy reference cleanup, then retire member policies/Secrets/workloads, temporary key files and certificate automation. Revoke when required; do not delete shared DNS/delegations or issuers. |

If a negative certificate cannot be obtained safely or a case is not executed,
report it as blocked/skipped rather than manufacturing a pass. Certificate
issuance and validation records remain in the approved internal run record,
not the public evidence bundle.

## Public references

- [AFD end-to-end TLS and certificate requirements](https://learn.microsoft.com/azure/frontdoor/end-to-end-tls?pivots=front-door-standard-premium)
- [AFD Private Link ILB identity and SNI](https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer)
- [Key Vault certificate composition and exportability](https://learn.microsoft.com/azure/key-vault/certificates/about-certificates)
