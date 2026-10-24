#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

# shellcheck source=test/scripts/e2e-config.sh
source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"
if [[ "$ENABLE_AZURE_FRONT_DOOR" != true ]]; then
  exit 0
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

for member in member-1 member-2; do
  cluster="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name "$member")"
  node_group="$(jq -er '.nodeResourceGroup' <<<"$cluster")"
  principal="$(jq -er '.identityProfile.kubeletidentity.objectId' <<<"$cluster")"
  client="$(jq -er '.identityProfile.kubeletidentity.clientId' <<<"$cluster")"
  location="$(jq -er '.location' <<<"$cluster")"
  az role assignment create --subscription "$AZURE_SUBSCRIPTION_ID" --role Reader \
    --assignee-object-id "$principal" --assignee-principal-type ServicePrincipal \
    --scope "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$node_group" >/dev/null
  config="$(cloud_config "$client" "$node_group" "$location")"
  helm upgrade member-net-controller-manager ./charts/member-net-controller-manager \
    --kube-context "$member-admin" --namespace default --reuse-values \
    --set enablePrivateLinkOriginDiscovery=true --set-json "azureCloudConfig=$config" \
    --wait --timeout 10m
done

hub="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name hub)"
principal="$(jq -er '.identityProfile.kubeletidentity.objectId' <<<"$hub")"
client="$(jq -er '.identityProfile.kubeletidentity.clientId' <<<"$hub")"
location="$(jq -er '.location' <<<"$hub")"
az role assignment create --subscription "$AZURE_SUBSCRIPTION_ID" --role "CDN Profile Contributor" \
  --assignee-object-id "$principal" --assignee-principal-type ServicePrincipal \
  --scope "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$AZURE_RESOURCE_GROUP" >/dev/null
config="$(cloud_config "$client" "$AZURE_RESOURCE_GROUP" "$location")"
kubectl --context hub-admin create namespace "$AFD_PROOF_CONTROLLER_NAMESPACE" --dry-run=client -o yaml |
  kubectl --context hub-admin apply -f -
kubectl --context hub-admin --namespace "$AFD_PROOF_CONTROLLER_NAMESPACE" create secret generic afd-cloud-config \
  --from-literal="azure.json=$config" --dry-run=client -o yaml | kubectl --context hub-admin apply -f -
helm install hub-gateway-controller-manager ./charts/hub-gateway-controller-manager \
  --kube-context hub-admin --namespace "$AFD_PROOF_CONTROLLER_NAMESPACE" \
  --set-string "fullnameOverride=$AFD_PROOF_CONTROLLER_DEPLOYMENT" \
  --set-string "serviceAccount.name=$AFD_PROOF_CONTROLLER_DEPLOYMENT" \
  --set-string "image.repository=${AFD_PROOF_CONTROLLER_IMAGE%:*}" \
  --set-string "image.tag=${AFD_PROOF_CONTROLLER_IMAGE##*:}" \
  --set afd.enabled=true --set-string "afd.resourceGroup=$AZURE_RESOURCE_GROUP" \
  --set afd.cloudConfigSecret.name=afd-cloud-config --wait --timeout 10m
