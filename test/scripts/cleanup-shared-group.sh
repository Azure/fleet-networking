#!/bin/bash
#
# 1. Load the run configuration and reject cleanup when AFD retention is enabled.
# 2. Find run-owned resources and verify their tags, names, types and scope.
# 3. Verify VNet peerings, controller profiles, the hub identity and the AFD test principal.
# 4. Collect and validate run-owned identities, node resource groups and role assignments.
# 5. Stop hub controllers when needed and delete verified Traffic Manager and Front Door profiles.
# 6. Remove node-group role assignments, delete AKS clusters and wait for node groups to disappear.
# 7. Remove remaining role assignments, peerings, public IPs, VNets, registries and identities.
# 8. Verify that no run-owned resources or controller profiles remain, preserving the shared group.
#
set -o errexit
set -o nounset
set -o pipefail

source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"
if [[ "$RETAIN_AFD_E2E_SETUP" == true ]]; then
  echo "error: shared cleanup requires non-retained setup" >&2
  exit 1
fi

group_id="/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$AZURE_RESOURCE_GROUP"
resources="$(az resource list --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" -o json)"
if ! jq -e --arg prefix "$E2E_RESOURCE_PREFIX" --arg run "$E2E_RUN_ID" --arg group "$group_id" \
  --arg hub "$HUB_AZURE_CLUSTER" --arg member1 "$MEMBER_1_AZURE_CLUSTER" --arg member2 "$MEMBER_2_AZURE_CLUSTER" \
  --arg member3 "$MEMBER_3_AZURE_CLUSTER" --arg member4 "$MEMBER_4_AZURE_CLUSTER" '
  all(.[];
    if (.name | ascii_downcase | startswith($prefix)) or (.tags["fleet-e2e-run"] // "") == $run then
      .tags.source == "fleet-networking" and .tags["fleet-e2e-run"] == $run and
      ((.id | ascii_downcase) == (($group + "/providers/" + .type + "/" + .name) | ascii_downcase)) and
      (if (.type | ascii_downcase) == "microsoft.containerservice/managedclusters" then
         (.name == $hub or .name == $member1 or .name == $member2 or .name == $member3 or .name == $member4) and
         (.tags["fleet-e2e-atm"] == "true" or .tags["fleet-e2e-atm"] == "false") and
         (.tags["fleet-e2e-afd"] == "true" or .tags["fleet-e2e-afd"] == "false") and
         (.tags["fleet-e2e-auto-approval"] == "true" or .tags["fleet-e2e-auto-approval"] == "false") and
         (if .tags["fleet-e2e-afd"] == "true" and .tags["fleet-e2e-auto-approval"] == "true" then
            (.tags["fleet-e2e-test-principal"] | type == "string" and length > 0)
          else true end)
       else
         (.type | ascii_downcase) as $type |
         (["microsoft.network/virtualnetworks", "microsoft.network/publicipaddresses",
           "microsoft.containerregistry/registries", "microsoft.managedidentity/userassignedidentities"] | index($type)) != null
       end) and (.name | ascii_downcase | startswith($prefix))
    else true end)
' <<<"$resources" >/dev/null; then
  echo "error: refusing cleanup because a run resource has unexpected ownership, type, name or scope" >&2
  exit 1
fi

owned="$(jq --arg run "$E2E_RUN_ID" '[.[] | select(.tags["fleet-e2e-run"] == $run)]' <<<"$resources")"
vnet_names="$(jq -r '.[] | select((.type | ascii_downcase) == "microsoft.network/virtualnetworks") | .name' <<<"$owned")"
read_peerings() {
  local name="$1" vnet_id="$group_id/providers/Microsoft.Network/virtualNetworks/$1" peerings
  peerings="$(az network vnet peering list --subscription "$AZURE_SUBSCRIPTION_ID" \
    --resource-group "$AZURE_RESOURCE_GROUP" --vnet-name "$name" -o json)"
  jq -e --arg vnet "$vnet_id" --argjson owned "$owned" '
    type == "array" and all(.[];
      (.id | ascii_downcase) == (($vnet + "/virtualNetworkPeerings/" + .name) | ascii_downcase) and
      (.remoteVirtualNetwork.id | ascii_downcase) as $remote |
      any($owned[]; (.type | ascii_downcase) == "microsoft.network/virtualnetworks" and
        (.id | ascii_downcase) == $remote and $remote != ($vnet | ascii_downcase)))
  ' <<<"$peerings" >/dev/null
  printf '%s\n' "$peerings"
}
while IFS= read -r name; do
  [[ -n "$name" ]] || continue
  read_peerings "$name" >/dev/null
done <<<"$vnet_names"

profiles="$(jq --arg prefix "$E2E_RESOURCE_PREFIX" '[
  .[] | select(
    ((.tags["networking.fleet.azure.com.trafficManagerProfile"] // "") | startswith($prefix + "-")) or
    ((.tags["networking.fleet.azure.com.gateway-namespace"] // "") | startswith($prefix + "-")))
]' <<<"$resources")"
has_hub="$(jq -r --arg hub "$HUB_AZURE_CLUSTER" \
  'any(.[]; (.type | ascii_downcase) == "microsoft.containerservice/managedclusters" and .name == $hub)' <<<"$owned")"
if [[ "$(jq 'length' <<<"$profiles")" -gt 0 && "$has_hub" != true ]]; then
    echo "error: controller profiles remain but the tagged hub cluster is absent; refusing cleanup" >&2
    exit 1
fi
hub_uid=""
hub_has_controllers="$(jq -r --arg hub "$HUB_AZURE_CLUSTER" '
  any(.[]; (.type | ascii_downcase) == "microsoft.containerservice/managedclusters" and .name == $hub and
    (.tags["fleet-e2e-atm"] == "true" or .tags["fleet-e2e-afd"] == "true"))
' <<<"$owned")"
recorded_caller="$(jq -r --arg hub "$HUB_AZURE_CLUSTER" '
  [.[] | select((.type | ascii_downcase) == "microsoft.containerservice/managedclusters" and .name == $hub and
    .tags["fleet-e2e-afd"] == "true" and .tags["fleet-e2e-auto-approval"] == "true") |
    .tags["fleet-e2e-test-principal"]] | first // ""
' <<<"$owned")"
if [[ -n "$recorded_caller" && "$recorded_caller" != "${E2E_TEST_PRINCIPAL_ID:-}" ]]; then
  echo "error: E2E_TEST_PRINCIPAL_ID must match the principal recorded on this run" >&2
  exit 1
fi
if [[ "$has_hub" == true && ( "$(jq 'length' <<<"$profiles")" -gt 0 ||
      "$hub_has_controllers" == true ) ]]; then
  hub="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name "$HUB_AZURE_CLUSTER" -o json)"
  jq -e --arg id "$group_id/providers/Microsoft.ContainerService/managedClusters/$HUB_AZURE_CLUSTER" --arg run "$E2E_RUN_ID" \
    '(.id | ascii_downcase) == ($id | ascii_downcase) and .tags.source == "fleet-networking" and .tags["fleet-e2e-run"] == $run' \
    <<<"$hub" >/dev/null
  # Azure CLI adds the "-admin" suffix for admin credentials.
  az aks get-credentials --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" \
    --name "$HUB_AZURE_CLUSTER" --admin --context "$HUB_CLUSTER" --overwrite-existing
  hub_uid="$(kubectl --context "$HUB_CLUSTER-admin" get namespace kube-system -o jsonpath='{.metadata.uid}')"
  if [[ ! "$hub_uid" =~ ^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$ ]]; then
    echo "error: could not verify hub UID for controller profile cleanup" >&2
    exit 1
  fi
fi
if [[ "$(jq 'length' <<<"$profiles")" -gt 0 ]]; then
  profile_lines="$(jq -c '.[]' <<<"$profiles")"
  while IFS= read -r profile; do
    type="$(jq -er '.type | ascii_downcase' <<<"$profile")"
    name="$(jq -er '.name' <<<"$profile")"
    jq -e --arg id "$group_id/providers/$(jq -r '.type' <<<"$profile")/$name" \
      '(.id | ascii_downcase) == ($id | ascii_downcase)' <<<"$profile" >/dev/null
    case "$type" in
      microsoft.network/trafficmanagerprofiles)
        jq -e --arg prefix "$E2E_RESOURCE_PREFIX" '
          (.name | test("^fleet-[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")) and
          (.tags["networking.fleet.azure.com.trafficManagerProfile"] |
            test("^" + $prefix + "-[a-z]{5}/[a-z0-9-]+$"))
        ' <<<"$profile" >/dev/null
        ;;
      microsoft.cdn/profiles)
        gateway_uid="$(jq -er '.tags["networking.fleet.azure.com.gateway-uid"]' <<<"$profile")"
        if [[ ! "$gateway_uid" =~ ^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$ ]]; then
          echo "error: invalid Gateway UID on Front Door profile" >&2
          exit 1
        fi
        expected_name="fleet-afd-$(printf '%s:%s' "${#gateway_uid}" "$gateway_uid" | sha256sum | cut -c1-32)"
        jq -e --arg prefix "$E2E_RESOURCE_PREFIX" --arg hub "$hub_uid" --arg name "$expected_name" '
          .name == $name and .tags["networking.fleet.azure.com.controller"] == "networking.fleet.azure.com/afd" and
          .tags["networking.fleet.azure.com.hub-uid"] == $hub and
          (.tags["networking.fleet.azure.com.gateway-namespace"] | test("^" + $prefix + "-[a-z]{5}$")) and
          (.tags["networking.fleet.azure.com.gateway-name"] | type == "string" and length > 0)
        ' <<<"$profile" >/dev/null
        ;;
      *) echo "error: refusing unrecognized controller profile type $type" >&2; exit 1 ;;
    esac
  done <<<"$profile_lines"
fi

principals=()
kubelet_principals=()
node_scopes=()
node_groups=()
identity_names="$(jq -r '.[] | select((.type | ascii_downcase) == "microsoft.managedidentity/userassignedidentities") | .name' <<<"$owned")"
while IFS= read -r name; do
  [[ -n "$name" ]] || continue
  identity="$(az identity show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name "$name" -o json)"
  jq -e --arg id "$group_id/providers/Microsoft.ManagedIdentity/userAssignedIdentities/$name" --arg run "$E2E_RUN_ID" \
    '(.id | ascii_downcase) == ($id | ascii_downcase) and .tags.source == "fleet-networking" and .tags["fleet-e2e-run"] == $run' \
    <<<"$identity" >/dev/null
  principal="$(jq -er '.principalId | select(type == "string" and length > 0)' <<<"$identity")"
  principals+=("$principal")
done <<<"$identity_names"
cluster_names="$(jq -r '.[] | select((.type | ascii_downcase) == "microsoft.containerservice/managedclusters") | .name' <<<"$owned")"
while IFS= read -r name; do
  [[ -n "$name" ]] || continue
  cluster="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name "$name" -o json)"
  jq -e --arg id "$group_id/providers/Microsoft.ContainerService/managedClusters/$name" --arg run "$E2E_RUN_ID" \
    '(.id | ascii_downcase) == ($id | ascii_downcase) and .tags.source == "fleet-networking" and .tags["fleet-e2e-run"] == $run' \
    <<<"$cluster" >/dev/null
  node_group="$(jq -r '.nodeResourceGroup // ""' <<<"$cluster")"
  if [[ -n "$node_group" ]]; then
    exists="$(az group exists --subscription "$AZURE_SUBSCRIPTION_ID" --name "$node_group" -o tsv)"
    if [[ "$exists" == true ]]; then
      node_group_json="$(az group show --subscription "$AZURE_SUBSCRIPTION_ID" --name "$node_group" -o json)"
      node_id="/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$node_group"
      jq -e --arg id "$node_id" --arg owner "$group_id/providers/Microsoft.ContainerService/managedClusters/$name" \
        '(.id | ascii_downcase) == ($id | ascii_downcase) and (.managedBy | ascii_downcase) == ($owner | ascii_downcase)' \
        <<<"$node_group_json" >/dev/null
      node_scopes+=("$node_id")
      node_groups+=("$node_group")
    elif [[ "$exists" != false ]]; then
      echo "error: unable to verify node resource group $node_group" >&2
      exit 1
    fi
  fi
  kubelet="$(jq -r '.identityProfile.kubeletidentity.objectId // ""' <<<"$cluster")"
  if [[ -n "$kubelet" ]]; then
    kubelet_principals+=("$kubelet")
  fi
  cluster_principals="$(jq -r '[.identity.principalId, .identityProfile.kubeletidentity.objectId,
    (.identity.userAssignedIdentities // {} | .[]?.principalId)] | .[] | select(type == "string" and length > 0)' <<<"$cluster")"
  while IFS= read -r principal; do
    [[ -n "$principal" ]] || continue
    principals+=("$principal")
  done <<<"$cluster_principals"
done <<<"$cluster_names"
principal_json="$(jq -cn '$ARGS.positional' --args "${principals[@]}")"
node_scope_json="$(jq -cn '$ARGS.positional' --args "${node_scopes[@]}")"
kubelet_json="$(jq -cn '$ARGS.positional' --args "${kubelet_principals[@]}")"

assignments="$(az role assignment list --subscription "$AZURE_SUBSCRIPTION_ID" --all --fill-principal-name false -o json)"
selected_assignments="$(jq -c --arg group "$group_id" --arg caller "$recorded_caller" \
  --argjson principals "$principal_json" --argjson kubelets "$kubelet_json" --argjson nodeScopes "$node_scope_json" '
  [.[] |
  (.principalId) as $principal | (.scope | ascii_downcase) as $scope |
  (.roleDefinitionId | ascii_downcase | split("/") | last) as $role |
  select(
    ((($principals | any(. == $principal)) and
      ($scope == ($group | ascii_downcase) or ($scope | startswith(($group | ascii_downcase) + "/"))) and
      (["acdd72a7-3385-48ef-bd42-f606fba81ae7", "4d97b98b-1d4f-4787-a291-c67834d212e7",
        "ec156ff8-a8d1-4d15-830c-5b80698ca432", "de2b316d-7a2c-4143-b4cd-c148f6a355a1",
        "f1a07417-d97a-45cb-824c-7a7467783830"] | index($role)) != null) or
     (($nodeScopes | any((ascii_downcase) == $scope)) and
      ((($kubelets | any(. == $principal)) and
        (["acdd72a7-3385-48ef-bd42-f606fba81ae7", "4d97b98b-1d4f-4787-a291-c67834d212e7",
          "de2b316d-7a2c-4143-b4cd-c148f6a355a1"] | index($role)) != null) or
       ($caller != "" and $principal == $caller and $role == "4d97b98b-1d4f-4787-a291-c67834d212e7"))))
  ) | {id, scope, name}]
' <<<"$assignments")"
if ! jq -e 'all(.[];
  (.name | test("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")) and
  ((.id | ascii_downcase) == ((.scope + "/providers/Microsoft.Authorization/roleAssignments/" + .name) | ascii_downcase)))
' <<<"$selected_assignments" >/dev/null; then
  echo "error: refusing role assignment with unexpected identity or scope" >&2
  exit 1
fi
node_assignment_ids="$(jq -r --argjson scopes "$node_scope_json" \
  '.[] | select((.scope | ascii_downcase) as $scope | $scopes | any((ascii_downcase) == $scope)) | .id' <<<"$selected_assignments")"
group_assignment_ids="$(jq -r --arg group "$group_id" \
  '.[] | select((.scope | ascii_downcase) == ($group | ascii_downcase) or
    (.scope | ascii_downcase | startswith(($group | ascii_downcase) + "/"))) | .id' <<<"$selected_assignments")"

if [[ -n "$hub_uid" ]]; then
  for deployment in hub-net-controller-manager hub-gateway-controller-manager; do
    existing="$(kubectl --context "$HUB_CLUSTER-admin" -n fleet-system get deployment "$deployment" --ignore-not-found -o name)"
    if [[ -n "$existing" ]]; then
      kubectl --context "$HUB_CLUSTER-admin" -n fleet-system scale "$existing" --replicas=0
      kubectl --context "$HUB_CLUSTER-admin" -n fleet-system rollout status "$existing" --timeout=120s
    fi
  done
  current_resources="$(az resource list --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" -o json)"
  current_profiles="$(jq -Sc --arg prefix "$E2E_RESOURCE_PREFIX" '[
    .[] | select(
      ((.tags["networking.fleet.azure.com.trafficManagerProfile"] // "") | startswith($prefix + "-")) or
      ((.tags["networking.fleet.azure.com.gateway-namespace"] // "") | startswith($prefix + "-")))
  ] | sort_by(.id)' <<<"$current_resources")"
  if [[ "$current_profiles" != "$(jq -Sc 'sort_by(.id)' <<<"$profiles")" ]]; then
    echo "error: controller profiles changed during cleanup; retry to verify their ownership" >&2
    exit 1
  fi
fi
if [[ "$(jq 'length' <<<"$profiles")" -gt 0 ]]; then
  while IFS= read -r profile; do
    id="$(jq -er '.id' <<<"$profile")"
    current="$(az resource show --ids "$id" -o json)"
    jq -e --argjson expected "$profile" \
      '(.id | ascii_downcase) == ($expected.id | ascii_downcase) and .tags == $expected.tags' <<<"$current" >/dev/null
    az resource delete --ids "$id"
  done <<<"$profile_lines"
fi

while IFS= read -r id; do
  [[ -n "$id" ]] || continue
  az role assignment delete --ids "$id"
done <<<"$node_assignment_ids"

while IFS= read -r name; do
  [[ -n "$name" ]] || continue
  cluster="$(az aks show --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name "$name" -o json)"
  jq -e --arg id "$group_id/providers/Microsoft.ContainerService/managedClusters/$name" --arg run "$E2E_RUN_ID" \
    '(.id | ascii_downcase) == ($id | ascii_downcase) and .tags.source == "fleet-networking" and .tags["fleet-e2e-run"] == $run' \
    <<<"$cluster" >/dev/null
  az aks delete --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name "$name" --yes --no-wait
done <<<"$cluster_names"
while IFS= read -r name; do
  [[ -n "$name" ]] || continue
  az aks wait --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" --name "$name" --deleted --interval 15 --timeout 900
done <<<"$cluster_names"
for node_group in "${node_groups[@]}"; do
  az group wait --subscription "$AZURE_SUBSCRIPTION_ID" --name "$node_group" --deleted --interval 15 --timeout 900
done
while IFS= read -r id; do
  [[ -n "$id" ]] || continue
  az role assignment delete --ids "$id"
done <<<"$group_assignment_ids"

while IFS= read -r name; do
  [[ -n "$name" ]] || continue
  peerings="$(read_peerings "$name")"
  peering_ids="$(jq -r '.[].id' <<<"$peerings")"
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    az network vnet peering delete --ids "$id"
  done <<<"$peering_ids"
done <<<"$vnet_names"

for type in Microsoft.Network/publicIPAddresses Microsoft.Network/virtualNetworks Microsoft.ContainerRegistry/registries Microsoft.ManagedIdentity/userAssignedIdentities; do
  resource_ids="$(jq -r --arg type "$type" '.[] | select((.type | ascii_downcase) == ($type | ascii_downcase)) | .id' <<<"$owned")"
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    current="$(az resource show --ids "$id" -o json)"
    jq -e --arg id "$id" --arg run "$E2E_RUN_ID" \
      '(.id | ascii_downcase) == ($id | ascii_downcase) and .tags.source == "fleet-networking" and .tags["fleet-e2e-run"] == $run' \
      <<<"$current" >/dev/null
    az resource delete --ids "$id"
  done <<<"$resource_ids"
done

remaining="$(az resource list --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$AZURE_RESOURCE_GROUP" -o json)"
if ! jq -e --arg run "$E2E_RUN_ID" --arg prefix "$E2E_RESOURCE_PREFIX" '
  all(.[];
    .tags["fleet-e2e-run"] != $run and
    ((.tags["networking.fleet.azure.com.trafficManagerProfile"] // "") | startswith($prefix + "-") | not) and
    ((.tags["networking.fleet.azure.com.gateway-namespace"] // "") | startswith($prefix + "-") | not))
' <<<"$remaining" >/dev/null; then
  echo "error: run-owned resources remain after cleanup" >&2
  exit 1
fi
