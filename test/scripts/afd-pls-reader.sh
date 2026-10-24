#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# shellcheck source=test/scripts/e2e-config.sh
source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"
if [[ "$ENABLE_AZURE_FRONT_DOOR" != true || $# != 3 ]]; then
  echo "error: exact PLS Reader preparation requires the standard AFD opt-in, namespace and two verified PLS IDs" >&2
  exit 1
fi
namespace="$1"
if [[ ${#namespace} -gt 63 || ! "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
  echo "error: expected a Kubernetes proof namespace" >&2
  exit 1
fi
targets=("$2" "$3")
hub="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name hub)"
principal="$(jq -er '.identityProfile.kubeletidentity.objectId' <<<"$hub")"
base="/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups"
reader=acdd72a7-3385-48ef-bd42-f606fba81ae7

# Validate both targets before any role write, including the actual AKS-managed node groups.
for index in 0 1; do
  member="member-$((index + 1))"
  cluster="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name "$member")"
  cluster_id="$(jq -er '.id' <<<"$cluster")"
  node_group="$(jq -er '.nodeResourceGroup' <<<"$cluster")"
  expected_cluster="$base/$AZURE_RESOURCE_GROUP/providers/Microsoft.ContainerService/managedClusters/$member"
  group="$(az group show --subscription "$AZURE_SUBSCRIPTION_ID" --name "$node_group")"
  managed_by="$(jq -er '.managedBy' <<<"$group")"
  expected="$base/$node_group/providers/Microsoft.Network/privateLinkServices/$namespace"
  if [[ "${cluster_id,,}" != "${expected_cluster,,}" || "${managed_by,,}" != "${cluster_id,,}" ||
        "${targets[$index],,}" != "${expected,,}" ]]; then
    echo "error: refusing Reader grant outside the verified member test PLS" >&2
    exit 1
  fi
  pls="$(az network private-link-service show --subscription "$AZURE_SUBSCRIPTION_ID" --ids "${targets[$index]}")"
  if ! jq -e --arg id "$expected" '(.id | ascii_downcase) == ($id | ascii_downcase) and .provisioningState == "Succeeded"' <<<"$pls" >/dev/null; then
    echo "error: test PLS identity/provisioning observation did not match $expected" >&2
    exit 1
  fi
done

for target in "${targets[@]}"; do
  az role assignment create --subscription "$AZURE_SUBSCRIPTION_ID" --role "$reader" \
    --assignee-object-id "$principal" --assignee-principal-type ServicePrincipal --scope "$target" >/dev/null
  bindings="$(az role assignment list --subscription "$AZURE_SUBSCRIPTION_ID" --scope "$target")"
  if ! jq -e --arg principal "$principal" --arg scope "$target" --arg role "$reader" '
    any(.[]; (.principalId | ascii_downcase) == ($principal | ascii_downcase) and
      (.scope | ascii_downcase) == ($scope | ascii_downcase) and
      (.roleDefinitionId | ascii_downcase | endswith("/" + $role)))' <<<"$bindings" >/dev/null; then
    echo "error: exact hub Reader assignment was not independently observed on $target" >&2
    exit 1
  fi
done
