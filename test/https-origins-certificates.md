# First-party certificate fixtures for end-to-end AFD TLS

**Status: experimental fixture implemented; live Azure validation not run.**
Use this guide with the [engineering scenario matrix](../docs/design/gep-1748-https-origins-proposal.md#e2e-testing-requirements)
and [E2E environment guide](README.md). Obtain explicit authorization for the
subscription, resource group, Kubernetes contexts, certificate/DNS operations
and create/test/cleanup scope before provisioning anything. No live operations
are authorized by this document.

The installed-controller fixture has its own explicit
`ENABLE_AFD_PRIVATE_HTTPS_PROOF=true` opt-in and `afd-private-https-proof` label.
It is not enabled by ordinary AFD/HTTP testing. It exercises system-verified edge
HTTPS, member TLS/SNI and real probe logs, exact Private Link approvals, no HTTP
fallback/redirect, metadata-only single-member opt-out, observed origin isolation,
healthy-member delivery, all-ineligible withdrawal and owned cleanup. This is not
a claim that the entire engineering scenario matrix has been proven live.

## Executable fixture interface

After obtaining scope approval, use the existing E2E environment with candidate
images. Supply `ENABLE_AZURE_FRONT_DOOR=true`,
`ENABLE_AFD_PRIVATE_HTTPS_PROOF=true`, `AZURE_SUBSCRIPTION_ID`,
`AZURE_RESOURCE_GROUP` and the normal verified hub/member contexts.
`ENABLE_ORIGIN_TLS` defaults to the explicit HTTPS proof selection. The existing
install script upgrades the internal hub CRD first, installs the pinned
experimental policy CRD and grants members scoped hub-schema reads before
enabling TLS in the existing managers. It grants no AKV/Secret access to them.

Supply these environment variables; **all values are identifiers, not secrets**:

| Variable | Required value |
|---|---|
| `ORIGIN_TLS_HOSTNAME` | Approved precise DNS SAN shared by the members |
| `ORIGIN_TLS_NAMESPACE` | Fresh, preauthorized `https-proof-*` namespace, absent from hub and both members |
| `ORIGIN_AKV_NAME` | Existing Azure Key Vault name, not its URL |
| `ORIGIN_AKV_CERT_NAME` | Certificate's backing-secret name containing the exportable private key and full chain |
| `ORIGIN_AKV_TENANT_ID` | Entra tenant UUID containing the vault/identity |
| `ORIGIN_AKV_CLIENT_ID` | Workload Identity application or user-assigned managed identity client UUID, not a credential |
| `ORIGIN_AKV_CERT_VERSION` | Optional 32-hex-digit **backing-secret** version; empty means latest |

`ORIGIN_CERT_CHAIN_FILE` and `ORIGIN_KEY_FILE` are no longer supported and are
rejected. No PEM, PFX password, client secret or token belongs in environment
variables, command arguments, logs or the checkout. The runner neither reads
private-key files nor downloads AKV secret contents.

Before the run, the approved platform/certificate owner must prepare the CSI
driver, Azure provider and OIDC issuer in **each member**, AKV network access,
and least-privilege secret-read permissions for the chosen identity (for
example, appropriately scoped Key Vault Secrets User with Azure RBAC, or
secret `get` under access policies). No certificate/key write access is needed.
Preconfigure one federated credential per member issuer, with audience
`api://AzureADTokenExchange` and exact subject:

```text
system:serviceaccount:<ORIGIN_TLS_NAMESPACE>:origin-serving
```

The namespace may be authorized before it exists. The fixture refuses existing
namespaces, creates the ServiceAccount/SPC itself in each member, and never
creates identities, role assignments, federated credentials, vaults or
certificates. Changing the namespace requires matching federation first.
It checks the CSI driver token-request audience and SPC CRD before creation;
a missing provider, failed mount, AKV denial or certificate failure is a failed
proof, never an identity/protocol/trust fallback. See the
[CSI Workload Identity prerequisites](https://azure.github.io/secrets-store-csi-driver-provider-azure/docs/configurations/identity-access-modes/workload-identity-mode/).
CSI requests ServiceAccount tokens natively; no application token injection or
Workload Identity pod label/ServiceAccount annotation is needed for CSI alone.

Run `bash test/scripts/run-e2e.sh` only in the explicitly approved environment.
It selects the HTTPS label rather than relabelling or replacing the HTTP proof.
The workload has only an HTTPS listener and internal LB/PLS Service. Certificate
bytes are mounted read-only as `/tls/serving.pem` in member application pods,
using `objectType: secret`, `objectFormat: pem`, alias `serving.pem` and file
mode `0400`. This fixture's Python process runs as root; a non-root application
needs deliberately configured file ownership/access. There is no `secretObjects`
sync and no serving TLS Secret in any cluster. The hub receives no AKV identity
or CSI resource. The fixture never renders private material or changes/deletes
the AKV certificate. Retention mode is rejected; cleanup removes only the
namespaces created by this run and the owned Gateway/profile.

Compile without live operations using
`go test -tags=e2e ./test/e2e -run '^$'`. This does **not** run the proof.
Offline configuration/mount tests are
`go test ./test/fixtures/origintls ./test/scripts`. Certificate validation tests
are `python3 -m unittest discover -s test/e2e/fixtures -p 'test_*.py' -v`
(Python 3 and OpenSSL required); generated local CA/certificates are disposable
unit-test data, never positive live fixtures.

## What the first-party test owner must obtain

| Fixture | Owner and required result |
|---|---|
| Client-facing hostname/certificate | Platform operator supplies the observed generated AFD HTTPS URL. Azure provides/renews its edge certificate; do not request an origin certificate for an `azurefd.net` name. |
| Origin SNI hostname | Test/DNS owner reserves a precise DNS name under an approved, controlled domain. `store-origin.example.com` is documentation only, not a name you can enroll. |
| Member 1 serving certificate/key | Approved certificate owner supplies a publicly trusted server certificate with that DNS SAN and its matching application-usable private key. |
| Member 2 serving certificate/key | This disposable fixture uses one approved AKV reference/identity with federation for each member issuer. Production applications should prefer separate member keys/identities for the same DNS identity. |
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

If policy permits export for this disposable fixture, authorize the member
Workload Identity to read the certificate's **backing secret**, not just the
public certificate. [Key Vault certificate export](https://learn.microsoft.com/azure/key-vault/certificates/how-to-export-certificate)
requires an exportable key and authorized secret access. The Azure CSI provider
retrieves and mounts the application-usable PEM private key and complete chain
inside each member; the E2E runner does not retrieve or convert it.
`objectType: cert` and `objectType: key` provide public components, not a usable
serving key pair. See [certificate components](https://azure.github.io/secrets-store-csi-driver-provider-azure/docs/configurations/getting-certs-and-keys/).
Non-exportable/HSM-backed keys cannot be made exportable by CSI. If export is
prohibited, mark this fixture blocked rather than weakening key policy or
giving Fleet controllers Key Vault/private-key access.

## 3. Validate before deploying

Before the run, the certificate owner verifies issuance, DNS SAN, server usage,
expiry and full chain against AFD's current allowed roots through the approved
certificate-management process. Do not export the key into the runner to do so.

After CSI mounting, but **before binding its serving listener**, the Python
application loads the bundle and performs an in-memory server/client TLS
handshake using its system trust store, hostname verification with SAN rather
than CN fallback, and server-authentication checks. This verifies the actual
loaded key/chain without an external listener or copying it out of the pod.
It requires at least seven days remaining validity. Wrong name, bad trust,
missing/mismatched key, expired/near-expiry certificate or unsuitable server
usage prevents startup. There is no production custom-trust/verification bypass.
Pod-local trust is still a preflight, not proof of AFD trust; the real AFD
traffic/probe assertions remain required.

This server loads the certificate only at startup. CSI autorotation can update
files but does not reload this process. Do not renew/change the selected version
during the basic proof; use an approved pinned version if repeatability is
required. Renewal/hot reload and the full negative matrix remain separate,
explicitly authorized exercises.

## 4. Deploy and prove both TLS connections

Prepare each member's application-only AKV CSI delivery, then
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
| Same-name renewal | Obtain a second trusted AKV issuance/backing-secret version before expiry; update a pinned SPC or confirm CSI autorotation's mounted version, then reload/replace the application. Prove fresh AFD handshakes, not only pooled connections. |
| Origin-name rotation | Reserve/enroll the new name and deploy dual-name coverage everywhere, then update each member policy. Conflicting otherwise-valid identities withdraw the AFD route; after eligible agreement Fleet updates the hub projection automatically. Record exclusions and require fresh agreement before re-admission. Verify new SNI/traffic before retiring old coverage. |
| Wrong-name failure | Use a certificate for another authorized test name in an isolated fixture. Do not request someone else's identity. |
| Untrusted chain | Use a disposable local test CA only for the negative case; never add it as AFD trust or call it a positive fixture. |
| Expired certificate | Arrange an authorized retained expired test certificate/key or wait for an approved short-lived fixture to expire. Do not change clocks or attempt to backdate a public-CA issuance. |
| Incomplete chain | Use an isolated chain that demonstrably fails at AFD; cached/fetched intermediates can invalidate a naive negative test. |
| Cleanup | Collect sanitized evidence, have the operator withdraw hub routing, confirm no delivery and generated-policy reference cleanup, then retire member policies/workloads/SPCs/ServiceAccounts. The fixture deletes no AKV objects or external federation; owners retire dedicated grants/certificates separately within approved scope. Do not delete shared DNS, identities or issuers. |

If a negative certificate cannot be obtained safely or a case is not executed,
report it as blocked/skipped rather than manufacturing a pass. Certificate
issuance and validation records remain in the approved internal run record,
not the public evidence bundle.

## Public references

- [AFD end-to-end TLS and certificate requirements](https://learn.microsoft.com/azure/frontdoor/end-to-end-tls?pivots=front-door-standard-premium)
- [AFD Private Link ILB identity and SNI](https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer)
- [Key Vault certificate composition and exportability](https://learn.microsoft.com/azure/key-vault/certificates/about-certificates)
