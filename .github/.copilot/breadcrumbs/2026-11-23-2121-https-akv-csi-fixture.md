# AKV CSI HTTPS fixture

## Requirements and authorization

- User: "ok, implement and adapt to akv csi driver method then".
- User: "also add the AKV env var then for the e2e?"
- Replace runner-local certificate/key handling with direct AKV CSI mounts in
  member application pods. Keep controller/private-key and hub boundaries intact.
- Preserve the PR's four docs/feat/build/test commits when publishing.
- No live Azure environment, identity grants, secret retrieval or cleanup is
  authorized. Existing AKV, certificate, CSI installation and federated identity
  are prerequisites supplied by the approved test owner.

## Plan and success criteria

1. [x] Add validated AKV reference/identity environment inputs and a deterministic
   fixture namespace compatible with preconfigured federated subjects.
2. [x] Create only member ServiceAccounts/SecretProviderClasses and mount the
   exportable certificate's backing secret as a PEM bundle, without Secret sync.
   Validate certificate/key/trust/name/lifetime inside the pod before serving.
3. [x] Test malformed/missing inputs, CSI pod wiring and local TLS validation;
   update the customer how-to and E2E runbook; run focused checks.
4. [x] Prepare publication in the existing test commit and confirm the remote
   lease/four-commit baseline. After the explicit-lease push, verify the remote
   PR head, unchanged first three commits and clean worktree.

## Decisions

- Environment variables contain identifiers only, never PEM, keys or tokens.
- Require a fresh `https-proof-*` namespace selected before the run, so an
  operator can preauthorize the exact ServiceAccount subject in both member
  OIDC issuers. Never create federated credentials or Azure role assignments
  from the fixture, and never reuse/delete a pre-existing namespace.
- Use CSI-native Workload Identity token requests, not node-identity fallback or
  application SDK credentials. Certificate/key contents never reach the runner
  or a Kubernetes Secret. The hub receives no SecretProviderClass or AKV identity.
- CSI rotation and application reload are distinct; this proof does not claim
  hot reload, renewal, or a full live rotation matrix.

## Implementation and before/after

- `test/fixtures/origintls/csi.go` replaces the runner-local PEM loader with
  validated `ORIGIN_AKV_NAME`, `ORIGIN_AKV_CERT_NAME`, `ORIGIN_AKV_TENANT_ID`,
  `ORIGIN_AKV_CLIENT_ID`, optional `ORIGIN_AKV_CERT_VERSION`, and required
  `ORIGIN_TLS_HOSTNAME`/`ORIGIN_TLS_NAMESPACE`. Legacy file inputs fail explicitly.
- `test/e2e/gateway_private_https_test.go` preflights fresh namespaces, the CSI
  token-request audience and SPC CRD. It creates member-only identity/reference
  objects and mounts the PEM backing secret directly. Namespace cleanup is bound
  to the created UID. No TLS Secret, raw key, Azure grant or hub CSI object is
  created. Existing traffic, probes, isolation and withdrawal proof is retained.
- `test/e2e/fixtures/https_server.py` validates the loaded key/chain with a bounded
  in-memory TLS handshake, system trust, SAN/server use and seven-day lifetime
  before binding the listener. It still loads certificates only at startup.
- `csi_test.go`, Python certificate tests and `test/scripts/afd_test.go` cover
  missing/malformed/legacy inputs, direct CSI identity/mount wiring, script
  refusal before executing commands, and nine certificate failure/success cases.
  `e2e-config.sh` requires identifiers only for explicit HTTPS proof selection.
- Updated the customer how-to's NGINX workload/setup/renewal/cleanup, proposal
  topology/implementation note, E2E README and certificate runbook. No controller,
  chart, public MCS API, dependency or unrelated HTTP behavior changes.

## Validation and limitations

Passed in WSL with the repository's configured Go/tool versions:

```bash
go test -race ./test/fixtures/origintls ./test/scripts
python3 -m unittest discover -s test/e2e/fixtures -p 'test_*.py' -v
go test -tags=e2e ./test/e2e -run "^$"
go vet ./test/fixtures/origintls ./test/scripts
go vet -tags=e2e ./test/e2e
make lint
git diff --check
```

- Python: nine tests, using disposable local CA/intermediate/leaf certificates.
- Configured goimports applied to the four changed Go files.
- Markdown structural checks: five current documents, 47 local links/anchors,
  49 Bash blocks (human angle-bracket inputs substituted for syntax checking),
  seven YAML blocks; all passed. Python test caches removed.
- Initial script-test formatting and three lint findings were corrected before
  the final passing run; no skips or suppression were added.
- No live AKV, IAM, CSI install/mount, certificate retrieval or Azure E2E run.
  Offline checks do not prove deployed provider availability, identity/network
  authorization, AFD trust, real traffic, renewal or rotation.
- Full runtime/envtest validation and the existing Docker Buildx/BuildKit
  limitation remain recorded in the prior implementation breadcrumb. Runtime
  code is unchanged by this follow-up.

## References

- [Prior implementation](2026-11-23-1917-https-origins-implementation.md).
- [AKS CSI Workload Identity](https://learn.microsoft.com/azure/aks/csi-secrets-store-identity-access#access-with-a-microsoft-entra-workload-id).
- [Certificate components](https://learn.microsoft.com/azure/aks/csi-secrets-store-identity-access#obtain-certificates-and-keys).
- [Rotation](https://learn.microsoft.com/azure/aks/csi-secrets-store-configuration-options).
