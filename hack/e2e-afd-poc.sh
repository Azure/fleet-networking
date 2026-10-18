#!/usr/bin/env bash
# hack/e2e-afd-poc.sh
#
# Tier-0 smoke test for the hub-afd-controller-manager (PATCH 06 reconciler).
# See docs/first-party/004-poc-runbook.md for the full test plan; this script
# automates only Tier 0 (30-minute FrontDoorProfile round-trip).
#
# What this creates in Azure (all billable):
#   * A resource group
#   * A 2-node AKS cluster with OIDC issuer + Workload Identity
#   * A user-assigned managed identity with Contributor on the RG
#   * A federated credential for the controller's ServiceAccount
#   * A single FrontDoorProfile CR that the controller reconciles into an
#     AFD Premium profile + WAF SecurityPolicy
#
# Idempotent: safe to re-run. Cleanup: `az group delete -n $RG --yes --no-wait`.
#
# Required env (or edit defaults below):
#   AZURE_SUBSCRIPTION_ID   subscription to deploy into
#   AFD_POC_RG              resource group name           (default: afd-poc-rg)
#   AFD_POC_LOC             region                        (default: eastus2)
#   AFD_POC_AKS             AKS cluster name              (default: afd-poc-aks)
#   AFD_POC_UAMI            managed identity name         (default: hub-afd-uami)
#   AFD_POC_IMAGE_REPO      controller image repository   (default: ghcr.io/azure/fleet-networking/hub-afd-controller-manager)
#   AFD_POC_IMAGE_TAG       controller image tag          (default: v0.1.0)
#
# Preconditions checked at runtime: az/kubectl/helm installed, `az account show`
# succeeds, current directory is the fleet-networking repo root.

set -euo pipefail

RG="${AFD_POC_RG:-afd-poc-rg}"
LOC="${AFD_POC_LOC:-eastus2}"
AKS="${AFD_POC_AKS:-afd-poc-aks}"
UAMI="${AFD_POC_UAMI:-hub-afd-uami}"
SA_NAMESPACE="fleet-system"
SA_NAME="hub-afd-controller-manager"
IMAGE_REPO="${AFD_POC_IMAGE_REPO:-ghcr.io/azure/fleet-networking/hub-afd-controller-manager}"
IMAGE_TAG="${AFD_POC_IMAGE_TAG:-v0.1.0}"

log() { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
require() { command -v "$1" >/dev/null 2>&1 || { echo "missing: $1" >&2; exit 1; }; }

require az
require kubectl
require helm

[[ -d charts/hub-afd-controller-manager ]] \
  || { echo "run from fleet-networking repo root" >&2; exit 1; }

SUB="${AZURE_SUBSCRIPTION_ID:-$(az account show --query id -o tsv)}"
TENANT="$(az account show --query tenantId -o tsv)"
az account set --subscription "$SUB"

log "Ensure resource group $RG in $LOC"
az group create -n "$RG" -l "$LOC" -o none

log "Ensure AKS $AKS (this can take ~5 min on first run)"
if ! az aks show -n "$AKS" -g "$RG" -o none 2>/dev/null; then
  az aks create -n "$AKS" -g "$RG" \
    --enable-oidc-issuer --enable-workload-identity \
    --node-count 2 --generate-ssh-keys --enable-managed-identity -o none
fi
az aks get-credentials -n "$AKS" -g "$RG" --overwrite-existing

OIDC_ISSUER="$(az aks show -n "$AKS" -g "$RG" --query oidcIssuerProfile.issuerUrl -o tsv)"

log "Ensure managed identity $UAMI"
if ! az identity show -n "$UAMI" -g "$RG" -o none 2>/dev/null; then
  az identity create -n "$UAMI" -g "$RG" -o none
fi
UAMI_CLIENT_ID="$(az identity show -n "$UAMI" -g "$RG" --query clientId -o tsv)"
UAMI_PRINCIPAL_ID="$(az identity show -n "$UAMI" -g "$RG" --query principalId -o tsv)"

log "Grant $UAMI Contributor on $RG (needed to create AFD profiles + WAF policies)"
SCOPE="/subscriptions/$SUB/resourceGroups/$RG"
az role assignment create --assignee-object-id "$UAMI_PRINCIPAL_ID" \
  --assignee-principal-type ServicePrincipal --role Contributor \
  --scope "$SCOPE" -o none 2>/dev/null || true

log "Federate $UAMI to $SA_NAMESPACE/$SA_NAME"
if ! az identity federated-credential show \
      --identity-name "$UAMI" -g "$RG" --name afd-federation -o none 2>/dev/null; then
  az identity federated-credential create \
    --identity-name "$UAMI" -g "$RG" --name afd-federation \
    --issuer "$OIDC_ISSUER" \
    --subject "system:serviceaccount:${SA_NAMESPACE}:${SA_NAME}" \
    --audiences api://AzureADTokenExchange -o none
fi

log "Apply AFD CRDs"
kubectl apply -f config/crd/bases/networking.fleet.azure.com_frontdoorprofiles.yaml
kubectl apply -f config/crd/bases/networking.fleet.azure.com_frontdoorbackends.yaml
kubectl apply -f config/crd/bases/networking.fleet.azure.com_frontdoorcustomdomains.yaml

log "Install hub-afd-controller-manager chart"
kubectl create namespace "$SA_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install hub-afd charts/hub-afd-controller-manager \
  --namespace "$SA_NAMESPACE" \
  --set azure.tenantId="$TENANT" \
  --set azure.clientId="$UAMI_CLIENT_ID" \
  --set azure.subscriptionID="$SUB" \
  --set image.repository="$IMAGE_REPO" \
  --set image.tag="$IMAGE_TAG"

log "Wait for controller pods to be Ready"
kubectl rollout status -n "$SA_NAMESPACE" deploy/hub-afd-controller-manager --timeout=180s

log "Apply a minimal FrontDoorProfile CR"
PROFILE_NAME="afd-poc"
cat <<EOF | kubectl apply -f -
apiVersion: networking.fleet.azure.com/v1alpha1
kind: FrontDoorProfile
metadata:
  name: ${PROFILE_NAME}
  namespace: ${SA_NAMESPACE}
spec:
  # SKU intentionally pinned — SharedPrivateLinkResource requires Premium.
  sku: Premium_AzureFrontDoor
  resourceGroup: ${RG}
  waf:
    enabled: true
    mode: Prevention
  complianceMode: Strict
EOF

log "Wait up to 5 min for Ready=True on FrontDoorProfile/$PROFILE_NAME"
for i in $(seq 1 30); do
  READY="$(kubectl get frontdoorprofile "$PROFILE_NAME" -n "$SA_NAMESPACE" \
    -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
  if [[ "$READY" == "True" ]]; then
    log "FrontDoorProfile is Ready"
    break
  fi
  sleep 10
done

kubectl get frontdoorprofile "$PROFILE_NAME" -n "$SA_NAMESPACE" -o yaml

log "Verify in Azure: AFD profile + attached WAF SecurityPolicy"
az afd profile show -g "$RG" --profile-name "$PROFILE_NAME" -o table || true
az afd security-policy list -g "$RG" --profile-name "$PROFILE_NAME" -o table || true

cat <<EOF

Tier-0 smoke test complete.

To iterate on the controller:
  kubectl -n $SA_NAMESPACE logs deploy/hub-afd-controller-manager -f

Teardown (removes ALL resources including the AFD profile):
  az group delete -n $RG --yes --no-wait

Next: docs/first-party/004-poc-runbook.md Tier 1 (single-cluster L7 flow).
EOF
