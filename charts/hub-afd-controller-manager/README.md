# hub-afd-controller-manager

A Helm chart for the fleet-networking **Azure Front Door** controller-manager
on the hub cluster. This chart is a **sibling** of `hub-net-controller-manager`
and must be installed alongside it, not in place of it.

## Why a separate chart?

Proposal 001 §7 (SFI-NS253) requires the AFD controller to run under a
Workload-Identity federated subject that is **distinct** from the
ATM/MCS controller's subject, so ATM-only tenants do not inherit AFD
write permissions on the shared Azure subscription. A Kubernetes pod
projects exactly one WI token, so the identity split is only
enforceable at the Pod boundary — one binary per Deployment, one
ServiceAccount per binary, one AAD federated identity per
ServiceAccount. See:

- `docs/first-party/001-afd-global-load-balancing.md` §7
- `docs/first-party/003-pre-implementation-checklist.md` §2.4
- `cmd/hub-afd-controller-manager/main.go` (package comment)

## Prerequisites

1. **CRDs installed.** This chart does **not** install CRDs. The
   `net-crd-installer` job attached to the `hub-net-controller-manager`
   chart (or an equivalent) must have applied the following CRDs first:
   - `frontdoorprofiles.networking.fleet.azure.com`
   - `frontdoorcustomdomains.networking.fleet.azure.com`

   The controller startup fails fast if either is missing (see
   `cmd/hub-afd-controller-manager/main.go`).

2. **Workload Identity webhook.** The AKS cluster hosting the hub must
   have the `azure-workload-identity` mutating webhook enabled (on AKS,
   enable the `WorkloadIdentity` feature; on OSS clusters, install the
   webhook chart from `Azure/azure-workload-identity`).

3. **Federated AAD identity provisioned.** Before `helm install`, an
   operator must:
   - Create an AAD app registration (or User-Assigned MI) for AFD.
   - Federate it to the hub cluster's OIDC issuer with subject
     `system:serviceaccount:<fleetSystemNamespace>:<release>-hub-afd-controller-manager-sa`.
   - Grant the identity `Contributor` (or the equivalent least-privilege
     `Front Door Domain Contributor` + `Front Door Endpoint Contributor`)
     scoped to the resource groups referenced by `FrontDoorProfile.Spec.ResourceGroup`.

   > **SFI invariant.** This AAD identity **must not** be the same as
   > the identity backing the `hub-net-controller-manager` chart. The
   > chart cannot cross-check this — enforcement lives in operator
   > tooling (Terraform / `az` CLI).

## Required values

| Value | Description |
| --- | --- |
| `azure.tenantId` | AAD tenant hosting the federated identity. |
| `azure.clientId` | Client ID of the federated AAD app / MI. **Distinct from ATM.** |
| `azure.subscriptionID` | Subscription that owns the AFD profiles. |

`helm install` fails loudly if any of these is empty.

## Coexistence with hub-net-controller-manager

Both charts can (and typically will) be installed into the same
namespace on the same hub. They are designed for peaceful coexistence:

- **Distinct leader-election lease names** (`afd.hub.networking.fleet.azure.com`
  vs `2bf2b407.hub.networking.fleet.azure.com`) — no lease contention.
- **Distinct container ports** (metrics `:8082` vs `:8080`, probe `:8083`
  vs `:8081`).
- **Distinct ServiceAccounts** with distinct WI annotations — no
  identity crossover.
- **Non-overlapping RBAC** — the AFD ClusterRole grants zero verbs on
  ATM/MCS CRDs, and vice versa.

## Uninstall

```bash
helm uninstall <release> -n <fleet-system-namespace>
```

Uninstall leaves the CRDs in place (they are shared with the
hub-net-controller-manager install). Front Door profiles that are still
present when the controller pod is removed will keep their finalizers
until either the controller is re-installed or the finalizers are
manually cleared — see `pkg/controllers/hub/frontdoorprofile/controller.go`.
