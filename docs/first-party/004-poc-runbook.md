# 004 — AFD First-Party POC Runbook

> **Status:** Draft — companion to [001](./001-afd-global-load-balancing.md),
> [002](./002-afd-implementation-plan.md), [003](./003-pre-implementation-checklist.md).
>
> **Audience:** Engineers who want to smoke-test the AFD first-party controller
> **without** access to a locked-down Azure Kubernetes Fleet Manager hub. All
> tiers below run against plain AKS cluster(s) that you own.

The controller is a normal Kubernetes controller-runtime binary. It only needs
credentials that can read the hub-cluster API server and can write to Azure
Front Door. It does not need to run *on* a Fleet Manager hub — any AKS cluster
where you can install Helm charts will do.

---

## Tier 0 — 30-minute smoke test (recommended first)

**Goal:** Prove the `FrontDoorProfile` reconciler wires a CR through to a real
AFD Premium profile + WAF SecurityPolicy in Azure. Exercises PATCH 03 (CRDs),
PATCH 04 (SDK bundle), PATCH 05 (chart+binary), PATCH 06 (profile reconciler).

**Cost:** ~$2/day for the AKS cluster + AFD Premium base fee.

**Prereqs on your workstation:**
- `az` (>= 2.60), `kubectl`, `helm` (>= 3.14), `docker`, `git`.
- An Azure subscription where you can create resource groups and AFD profiles.
- `az login` completed.

**Automated:** `hack/e2e-afd-poc.sh` performs the steps below idempotently.
Read the script comments before running — it creates billable Azure resources.

Manual outline:
1. `az group create -n <RG> -l <LOC>`
2. `az aks create -n <AKS> -g <RG> --enable-oidc-issuer --enable-workload-identity --node-count 2 --generate-ssh-keys`
3. `az identity create -n hub-afd-uami -g <RG>` — grant it `Contributor` on the RG.
4. `az identity federated-credential create` — federate to
   `system:serviceaccount:fleet-system:hub-afd-controller-manager`.
5. `kubectl apply -f config/crd/bases/networking.fleet.azure.com_frontdoor*.yaml`
6. Build & push the controller image (or reuse a prebuilt tag): `make docker-build-hub-afd-controller-manager`.
7. `helm install hub-afd charts/hub-afd-controller-manager --set azure.tenantId=… --set azure.clientId=<uami-clientId> --set azure.subscriptionID=… --set image.repository=… --set image.tag=…`
8. `kubectl apply -f -` a minimal `FrontDoorProfile` CR (WAF + ComplianceMode
   defaults). See sample in the script.
9. Wait for `.status.conditions[?(@.type=="Ready")].status == "True"`.
10. Verify in Azure: `az afd profile show`, `az afd security-policy list`.

**Success signal:** AFD Premium profile exists in Azure, WAF policy is
attached via a SecurityPolicy, and the CR's `.status.profileId` contains the
ARM resource ID.

**Teardown:** `az group delete -n <RG> --yes --no-wait` (removes AKS, UAMI,
AFD profile in one call).

---

## Tier 1 — 2-hour single-cluster full L7 flow

**Goal:** Exercise the end-to-end path — `ServiceExport` on a member cluster
resolves the workload's Private Link Service, `InternalServiceExport` carries
the PLS ARM ID to the hub, `FrontDoorBackend` provisions the AFD OriginGroup +
Origin with a `SharedPrivateLinkResource`. Exercises PATCH 08.

**Prereqs (in addition to Tier 0):**
- The AKS cluster's node subnet has `privateLinkServiceNetworkPolicies=Disabled`.
- AKS system-assigned identity has `Network Contributor` on that subnet.

Outline:
1. Deploy an nginx `Service type=LoadBalancer` with the
   [cloud-provider-azure PLS annotations](https://cloud-provider-azure.sigs.k8s.io/topics/pls-integration/):
   `service.beta.kubernetes.io/azure-load-balancer-internal: "true"`,
   `service.beta.kubernetes.io/azure-pls-create: "true"`,
   `service.beta.kubernetes.io/azure-pls-name: "nginx-pls"`.
2. Wait for the PLS to be created (`az network private-link-service list -g MC_<rg>_<aks>_<loc>`).
3. Apply a `ServiceExport` on the same namespace/service with annotation
   `networking.fleet.azure.com/export-mode: "L7"` (see PATCH 04 for annotation
   contract).
4. Use the same AKS cluster as both hub and member (skip real Fleet join by
   creating the `InternalServiceExport` manually in the reserved member
   namespace, wiring the PLS ARM ID onto `.spec.privateLinkServiceID`).
5. Apply a `FrontDoorBackend` CR referencing the service.
6. Watch the hub controller reconcile OriginGroup + Origin;
   `az afd origin list` should show the origin with `sharedPrivateLinkResource`
   populated. In Azure Portal, approve the pending PLS connection request.

**Success signal:** `curl https://<afd-endpoint>.z01.azurefd.net/` returns
nginx welcome page **over the AFD → PLS private path** (no public IP on the
service).

---

## Tier 2 — day-long two-cluster real Fleet flow

**Goal:** Exercise the real multi-cluster propagation — two AKS clusters, one
Fleet Manager hub, real `ServiceExport` → `InternalServiceExport` propagation
via the fleet-networking member agent.

Only needed to validate the member/hub split. Use Tier 1 for reconciler
development iterations; Tier 2 for release-gate validation.

Follow the standard fleet-networking e2e setup under
[`test/scripts/`](../../test/scripts/) but substitute the AFD chart for (or
alongside) the traffic-manager chart, and enable the AFD annotation on the
`ServiceExport`.

---

## Reference

- `hack/e2e-afd-poc.sh` — Tier 0 (and stub of Tier 1) automation.
- Chart values keys: `azure.tenantId`, `azure.clientId`, `azure.subscriptionID`,
  `image.repository`, `image.tag`. See
  [`charts/hub-afd-controller-manager/values.yaml`](../../charts/hub-afd-controller-manager/values.yaml).
- SFI-NS253 §7 rule: the AAD identity for `hub-afd-controller-manager` **must
  be distinct** from the identity used by `hub-net-controller-manager`.
