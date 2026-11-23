#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# shellcheck source=test/scripts/e2e-config.sh
source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"
if [[ "$ENABLE_AZURE_FRONT_DOOR" != true ]]; then
  exit 0
fi
if [[ "$ENABLE_ORIGIN_TLS" == true ]]; then
  kubectl --context hub-admin apply --server-side \
    -f ./config/crd/bases/networking.fleet.azure.com_internalserviceexports.yaml
  kubectl --context hub-admin wait --for=condition=Established --timeout=60s \
    crd/internalserviceexports.networking.fleet.azure.com
  policy_crd="$(go list -m -f '{{.Dir}}' sigs.k8s.io/gateway-api)/config/crd/experimental/gateway.networking.k8s.io_backendtlspolicies.yaml"
  if [[ ! -f "$policy_crd" ]]; then
    echo "error: pinned experimental BackendTLSPolicy CRD is missing" >&2
    exit 1
  fi
  for context in hub-admin member-1-admin member-2-admin; do
    kubectl --context "$context" apply --server-side -f "$policy_crd"
    kubectl --context "$context" wait --for=condition=Established --timeout=60s \
      crd/backendtlspolicies.gateway.networking.k8s.io
  done
  kubectl --context hub-admin create clusterrole fleet-origin-tls-schema-reader \
    --verb=get --resource=customresourcedefinitions.apiextensions.k8s.io \
    --resource-name=internalserviceexports.networking.fleet.azure.com --dry-run=client -o yaml |
    kubectl --context hub-admin apply -f -
fi
tenant="$(az account show --subscription "$AZURE_SUBSCRIPTION_ID" --query tenantId -o tsv)"

cloud_config() {
  jq -n --arg subscription "$AZURE_SUBSCRIPTION_ID" --arg tenant "$tenant" \
    --arg client "$1" --arg group "$2" --arg location "$3" '{
      cloud: "AzurePublicCloud", subscriptionId: $subscription, tenantId: $tenant,
      useManagedIdentityExtension: true, userAssignedIdentityID: $client,
      resourceGroup: $group, location: $location
    }'
}

hub="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name hub)"
hub_principal="$(jq -er '.identityProfile.kubeletidentity.objectId' <<<"$hub")"
base="/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups"
declare -A clusters node_groups
# Validate both AKS-managed node groups before any role assignments.
for member in member-1 member-2; do
  cluster="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name "$member")"
  cluster_id="$(jq -er '.id' <<<"$cluster")"
  node_group="$(jq -er '.nodeResourceGroup' <<<"$cluster")"
  group="$(az group show --subscription "$AZURE_SUBSCRIPTION_ID" --name "$node_group")"
  group_id="$(jq -er '.id' <<<"$group")"
  managed_by="$(jq -er '.managedBy' <<<"$group")"
  expected_cluster="$base/$AZURE_RESOURCE_GROUP/providers/Microsoft.ContainerService/managedClusters/$member"
  expected_group="$base/$node_group"
  if [[ "${cluster_id,,}" != "${expected_cluster,,}" || "${managed_by,,}" != "${cluster_id,,}" ||
        "${group_id,,}" != "${expected_group,,}" ]]; then
    echo "error: refusing Reader grants outside verified member node resource groups" >&2
    exit 1
  fi
  clusters[$member]="$cluster"
  node_groups[$member]="$node_group"
done

reader=acdd72a7-3385-48ef-bd42-f606fba81ae7
for member in member-1 member-2; do
  scope="$base/${node_groups[$member]}"
  az role assignment create --subscription "$AZURE_SUBSCRIPTION_ID" --role Reader \
    --assignee-object-id "$hub_principal" --assignee-principal-type ServicePrincipal --scope "$scope" >/dev/null
  bindings="$(az role assignment list --subscription "$AZURE_SUBSCRIPTION_ID" --scope "$scope")"
  if ! jq -e --arg principal "$hub_principal" --arg scope "$scope" --arg role "$reader" '
    any(.[]; (.principalId | ascii_downcase) == ($principal | ascii_downcase) and
      (.scope | ascii_downcase) == ($scope | ascii_downcase) and
      (.roleDefinitionId | ascii_downcase | endswith("/" + $role)))' <<<"$bindings" >/dev/null; then
    echo "error: hub Reader assignment was not independently observed on $scope" >&2
    exit 1
  fi
done

for member in member-1 member-2; do
  cluster="${clusters[$member]}"
  node_group="${node_groups[$member]}"
  principal="$(jq -er '.identityProfile.kubeletidentity.objectId' <<<"$cluster")"
  client="$(jq -er '.identityProfile.kubeletidentity.clientId' <<<"$cluster")"
  location="$(jq -er '.location' <<<"$cluster")"
  az role assignment create --subscription "$AZURE_SUBSCRIPTION_ID" --role Reader \
    --assignee-object-id "$principal" --assignee-principal-type ServicePrincipal \
    --scope "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$node_group" >/dev/null
  config="$(cloud_config "$client" "$node_group" "$location")"
  if [[ "$ENABLE_ORIGIN_TLS" == true ]]; then
    kubectl --context hub-admin create clusterrolebinding "fleet-origin-tls-schema-reader-$member" \
      --clusterrole=fleet-origin-tls-schema-reader --user="$principal" --dry-run=client -o yaml |
      kubectl --context hub-admin apply -f -
  fi
  helm upgrade member-net-controller-manager ./charts/member-net-controller-manager \
    --kube-context "$member-admin" --namespace default --reuse-values \
    --set enablePrivateLinkOriginDiscovery=true --set "enableOriginTLS=$ENABLE_ORIGIN_TLS" --set-json "azureCloudConfig=$config" \
    --wait --timeout 10m
done

client="$(jq -er '.identityProfile.kubeletidentity.clientId' <<<"$hub")"
location="$(jq -er '.location' <<<"$hub")"
az role assignment create --subscription "$AZURE_SUBSCRIPTION_ID" --role "CDN Profile Contributor" \
  --assignee-object-id "$hub_principal" --assignee-principal-type ServicePrincipal \
  --scope "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$AZURE_RESOURCE_GROUP" >/dev/null
config="$(cloud_config "$client" "$AZURE_RESOURCE_GROUP" "$location")"
kubectl --context hub-admin create namespace "$AFD_PROOF_CONTROLLER_NAMESPACE" --dry-run=client -o yaml |
  kubectl --context hub-admin apply -f -
kubectl --context hub-admin --namespace "$AFD_PROOF_CONTROLLER_NAMESPACE" create secret generic afd-cloud-config \
  --from-literal="azure.json=$config" --dry-run=client -o yaml | kubectl --context hub-admin apply -f -
helm install hub-gateway-controller-manager ./charts/hub-gateway-controller-manager \
  --kube-context hub-admin --namespace "$AFD_PROOF_CONTROLLER_NAMESPACE" \
  --set-string "fleetSystemNamespace=$AFD_PROOF_CONTROLLER_NAMESPACE" \
  --set-string "fullnameOverride=$AFD_PROOF_CONTROLLER_DEPLOYMENT" \
  --set-string "serviceAccount.name=$AFD_PROOF_CONTROLLER_DEPLOYMENT" \
  --set-string "image.repository=${AFD_PROOF_CONTROLLER_IMAGE%:*}" \
  --set-string "image.tag=${AFD_PROOF_CONTROLLER_IMAGE##*:}" \
  --set afd.enabled=true --set "enableOriginTLS=$ENABLE_ORIGIN_TLS" --set-string "afd.resourceGroup=$AZURE_RESOURCE_GROUP" \
  --set afd.cloudConfigSecret.name=afd-cloud-config --wait --timeout 10m
