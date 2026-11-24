#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

# shellcheck source=test/scripts/e2e-config.sh
source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"

# Check required variables.
[[ -z "${AZURE_SUBSCRIPTION_ID}" ]] && echo "AZURE_SUBSCRIPTION_ID is not set" && exit 1

# Check required tools.
if ! command -v yq &> /dev/null; then
    echo "error: yq is not installed. Please install yq to continue."
    echo "Install instructions: https://github.com/mikefarah/yq#install"
    exit 1
fi

export ENABLE_TRAFFIC_MANAGER=${ENABLE_TRAFFIC_MANAGER:-"false"}

if [ "$ENABLE_TRAFFIC_MANAGER" == "true" ] && [ "$AZURE_NETWORK_SETTING" != "shared-vnet" ]; then
  echo "error: setting up traffic manager testing env is not supported when AZURE_NETWORK_SETTING is not shared-vnet"
  exit 1
fi

if [[ "$ENABLE_AZURE_FRONT_DOOR" == true ]]; then
  for tool in az jq helm kubectl go docker; do
    if ! command -v "$tool" >/dev/null; then
      echo "error: AFD setup requires $tool" >&2
      exit 1
    fi
  done
  AFD_VALIDATE_LOCAL_TOOLS=true go test -count=1 ./test/scripts
fi

az account set -s ${AZURE_SUBSCRIPTION_ID}

export RESOURCE_GROUP="$AZURE_RESOURCE_GROUP"
export LOCATION="${LOCATION:-eastus2}"
if [[ -z "${KUBECONFIG:-}" || "$KUBECONFIG" == *:* || -e "$KUBECONFIG" || -L "$KUBECONFIG" ]]; then
  echo "error: shared setup requires a new, run-specific KUBECONFIG path" >&2
  exit 1
fi
group="$(az group show --subscription "$AZURE_SUBSCRIPTION_ID" --name "$RESOURCE_GROUP" -o json)"
jq -e --arg id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP" \
  '(.id | ascii_downcase) == ($id | ascii_downcase) and .tags.managedBy == "aks-rp/ci-resources" and .tags.workload == "fleet-networking-e2e"' \
  <<<"$group" > /dev/null
resources="$(az resource list --subscription "$AZURE_SUBSCRIPTION_ID" --resource-group "$RESOURCE_GROUP" -o json)"
if ! jq -e --arg prefix "$E2E_RESOURCE_PREFIX" --arg run "$E2E_RUN_ID" '
  all(.[];
    ((.name | ascii_downcase | startswith($prefix)) | not) and
    .tags["fleet-e2e-run"] != $run and
    ((.tags["networking.fleet.azure.com.trafficManagerProfile"] // "") | startswith($prefix + "-") | not) and
    ((.tags["networking.fleet.azure.com.gateway-namespace"] // "") | startswith($prefix + "-") | not))
' <<<"$resources" >/dev/null; then
  echo "error: refusing to reuse resources for run $E2E_RUN_ID" >&2
  exit 1
fi
echo "##vso[task.setvariable variable=e2eResourcesStarted]true"

if [[ "$RETAIN_AFD_E2E_SETUP" == true ]]; then
    trap 'echo "AFD setup interrupted; retaining resources for explicit later cleanup" >&2; exit 130' INT
    trap 'echo "AFD setup interrupted; retaining resources for explicit later cleanup" >&2; exit 143' TERM
else
    trap 'echo "Shared setup interrupted; run explicit per-run cleanup" >&2; exit 130' INT
    trap 'echo "Shared setup interrupted; run explicit per-run cleanup" >&2; exit 143' TERM
fi

# Pubilsh fleet networking agent images.
# TODO(mainred): Once we have a specific Azure sub for fleet networking e2e test, we can reuse that registry.
# Registry name must conform to the following pattern: '^[a-zA-Z0-9]*$'.
az acr create -g "$RESOURCE_GROUP" -n "$REGISTRY_NAME" --sku standard "${E2E_RESOURCE_TAGS[@]}"
# Enable anonymous to not wait for the long-running AKS creation.
# When attach-acr and `--enable-managed-identity` are both specified, AKS requires us to wait until the whole operation
# succeeds, and as AuthN and AuthZ for member cluster agents to hub cluster in our test requires managed identity,
# we enable anonymous pull access to the registry instead of enabling attach-acr.
az acr update --name $REGISTRY_NAME --anonymous-pull-enabled -g $RESOURCE_GROUP
az acr login -n $REGISTRY_NAME
export REGISTRY=$REGISTRY_NAME.azurecr.io
export TAG="${TAG:-$(git rev-parse --short=7 HEAD)}"
make push

# Create hub and member clusters and wait until all clusters are ready.
export NODE_COUNT=2

if [ "$ENABLE_TRAFFIC_MANAGER" == "false" ]; then
  # Create aks hub cluster, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
  echo "Creating aks cluster: ${HUB_CLUSTER}"
  az aks create \
       --location $LOCATION \
       --resource-group $RESOURCE_GROUP \
       --name $HUB_AZURE_CLUSTER \
       --node-count $NODE_COUNT \
       --node-vm-size Standard_A2_v2 \
       --generate-ssh-keys \
       --enable-aad \
       --enable-azure-rbac \
       --network-plugin azure \
       --no-wait "${E2E_RESOURCE_TAGS[@]}"
else
  # Create aks identity
  echo "Creating hub control-plane identity: ${HUB_CLUSTER_AKS_ID_NAME}"
  HUB_CLUSTER_AKS_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${HUB_CLUSTER_AKS_ID_NAME} "${E2E_RESOURCE_TAGS[@]}")
  echo "Creating hub kubelet identity: ${HUB_CLUSTER_AKS_KUBELET_ID_NAME}"
  HUB_CLUSTER_AKS_KUBELET_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${HUB_CLUSTER_AKS_KUBELET_ID_NAME} "${E2E_RESOURCE_TAGS[@]}")

  HUB_CLUSTER_AKS_IDENTITY_ID=$(echo ${HUB_CLUSTER_AKS_IDENTITY} | jq -r '. | .id')
  HUB_CLUSTER_AKS_KUBELET_IDENTITY_ID=$(echo ${HUB_CLUSTER_AKS_KUBELET_IDENTITY} | jq -r '. | .id')
  HUB_CLUSTER_KUBELET_PRINCIPAL_ID=$(echo ${HUB_CLUSTER_AKS_KUBELET_IDENTITY} | jq -r '. | .principalId')
  HUB_CLUSTER_KUBELET_CLIENT_ID=$(echo ${HUB_CLUSTER_AKS_KUBELET_IDENTITY} | jq -r '. | .clientId')

  echo "Creating aks cluster: ${HUB_CLUSTER}"
  # Create aks hub cluster, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
  az aks create \
         --location $LOCATION \
         --resource-group $RESOURCE_GROUP \
         --name $HUB_AZURE_CLUSTER \
         --node-count $NODE_COUNT \
         --node-vm-size Standard_A2_v2 \
         --generate-ssh-keys \
         --enable-aad \
         --enable-azure-rbac \
         --network-plugin azure \
         --enable-managed-identity --assign-identity ${HUB_CLUSTER_AKS_IDENTITY_ID} --assign-kubelet-identity ${HUB_CLUSTER_AKS_KUBELET_IDENTITY_ID} \
         --no-wait "${E2E_RESOURCE_TAGS[@]}"

  # Create aks identity for member-1
  echo "Creating member-1 control-plane identity: ${MEMBER_CLUSTER_1_AKS_ID_NAME}"
  MEMBER_CLUSTER_1_AKS_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${MEMBER_CLUSTER_1_AKS_ID_NAME} "${E2E_RESOURCE_TAGS[@]}")
  echo "Creating member-1 kubelet identity: ${MEMBER_CLUSTER_1_AKS_KUBELET_ID_NAME}"
  MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${MEMBER_CLUSTER_1_AKS_KUBELET_ID_NAME} "${E2E_RESOURCE_TAGS[@]}")

  export MEMBER_CLUSTER_1_AKS_IDENTITY_ID=$(echo ${MEMBER_CLUSTER_1_AKS_IDENTITY} | jq -r '. | .id')
  export MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY_ID=$(echo ${MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY} | jq -r '. | .id')
  export MEMBER_CLUSTER_1_KUBELET_PRINCIPAL_ID=$(echo ${MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY} | jq -r '. | .principalId')
  export MEMBER_CLUSTER_1_KUBELET_CLIENT_ID=$(echo ${MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY} | jq -r '. | .clientId')

  # Create aks identity for member-2
  echo "Creating member-2 control-plane identity: ${MEMBER_CLUSTER_2_AKS_ID_NAME}"
  MEMBER_CLUSTER_2_AKS_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${MEMBER_CLUSTER_2_AKS_ID_NAME} "${E2E_RESOURCE_TAGS[@]}")
  echo "Creating member-2 kubelet identity: ${MEMBER_CLUSTER_2_AKS_KUBELET_ID_NAME}"
  MEMBER_CLUSTER_2_AKS_KUBELET_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${MEMBER_CLUSTER_2_AKS_KUBELET_ID_NAME} "${E2E_RESOURCE_TAGS[@]}")

  export MEMBER_CLUSTER_2_AKS_IDENTITY_ID=$(echo ${MEMBER_CLUSTER_2_AKS_IDENTITY} | jq -r '. | .id')
  export MEMBER_CLUSTER_2_AKS_KUBELET_IDENTITY_ID=$(echo ${MEMBER_CLUSTER_2_AKS_KUBELET_IDENTITY} | jq -r '. | .id')
  export MEMBER_CLUSTER_2_KUBELET_PRINCIPAL_ID=$(echo ${MEMBER_CLUSTER_2_AKS_KUBELET_IDENTITY} | jq -r '. | .principalId')
  export MEMBER_CLUSTER_2_KUBELET_CLIENT_ID=$(echo ${MEMBER_CLUSTER_2_AKS_KUBELET_IDENTITY} | jq -r '. | .clientId')
fi

AZURE_NETWORK_SETTING="${AZURE_NETWORK_SETTING:-shared-vnet}"
case $AZURE_NETWORK_SETTING in
        shared-vnet)
                export MEMBER_1_LOCATION="${LOCATION}"
                export MEMBER_2_LOCATION="${LOCATION}"
                bash test/scripts/aks-shared-vnet.sh
                ;;
        dynamic-ip-allocation)
                export MEMBER_1_LOCATION="${LOCATION}"
                export MEMBER_2_LOCATION="${LOCATION}"
                bash test/scripts/aks-dynamic-ip-allocation.sh
                ;;
        peered-vnet)
                export MEMBER_1_LOCATION="${MEMBER_1_LOCATION:-eastus2}"
                export MEMBER_2_LOCATION="${MEMBER_2_LOCATION:-westus}"
                bash test/scripts/aks-peered-vnet.sh
                ;;
        unsupported)
                export MEMBER_1_LOCATION="${LOCATION}"
                export MEMBER_2_LOCATION="${LOCATION}"
                bash test/scripts/aks-no-networking.sh
                ;;
        perf-test)
                export MEMBER_LOCATION_1="${MEMBER_LOCATION_1:-eastus2}"
                export MEMBER_1_LOCATION=$MEMBER_LOCATION_1
                export MEMBER_2_LOCATION=$MEMBER_LOCATION_1
                export MEMBER_LOCATION_2="${MEMBER_LOCATION_2:=westus}"
                bash test/scripts/perf-test.sh
                ;;
        *)
                echo "$AZURE_NETWORK_SETTING is not supported"
                exit 1
                ;;
esac

az aks wait --created --interval 10 --name $HUB_AZURE_CLUSTER --resource-group $RESOURCE_GROUP --timeout 1800
az aks wait --created --interval 10 --name $MEMBER_1_AZURE_CLUSTER --resource-group $RESOURCE_GROUP --timeout 1800
az aks wait --created --interval 10 --name $MEMBER_2_AZURE_CLUSTER --resource-group $RESOURCE_GROUP --timeout 1800
if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]
then
    az aks wait --created --interval 10 --name $MEMBER_3_AZURE_CLUSTER --resource-group $RESOURCE_GROUP --timeout 1800
    az aks wait --created --interval 10 --name $MEMBER_4_AZURE_CLUSTER --resource-group $RESOURCE_GROUP --timeout 1800
fi

# hack: add role assignments to kubelet identity
if [ "$ENABLE_TRAFFIC_MANAGER" == "true" ]; then
  AKS_HUB_CLUSTER=$(az aks show -g $RESOURCE_GROUP -n $HUB_AZURE_CLUSTER)
  AKS_HUB_CLUSTER_NODE_RESOURCE_GROUP=$(echo ${AKS_HUB_CLUSTER} | jq -r '. | .nodeResourceGroup')

  echo "Assigning Azure Kubernetes Fleet Manager Hub Agent Role to hub kubelet identity on the resourceGroup of the hub cluster"
  az role assignment create --role "de2b316d-7a2c-4143-b4cd-c148f6a355a1" --assignee-object-id ${HUB_CLUSTER_KUBELET_PRINCIPAL_ID} --assignee-principal-type ServicePrincipal --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${RESOURCE_GROUP}" > /dev/null

  # Creating azure configuration file for the controllers
  echo "Generating azure configuration file:" $(pwd)/hub_azure_config.yaml
  cat << EOF > $(pwd)/hub_azure_config.yaml
  azureCloudConfig:
    cloud: "AzurePublicCloud"
    subscriptionId: "${AZURE_SUBSCRIPTION_ID}"
    useManagedIdentityExtension: true
    userAssignedIdentityID: "${HUB_CLUSTER_KUBELET_CLIENT_ID}"
    resourceGroup: "${AKS_HUB_CLUSTER_NODE_RESOURCE_GROUP}"
    location: "${LOCATION}"
EOF

  AKS_MEMBER_1=$(az aks show -g $RESOURCE_GROUP -n $MEMBER_1_AZURE_CLUSTER)
  AKS_MEMBER_1_NODE_RESOURCE_GROUP=$(echo ${AKS_MEMBER_1} | jq -r '. | .nodeResourceGroup')

  echo "Assigning Azure Kubernetes Fleet Manager Hub Agent Role to hub kubelet identity on the MC_resourceGroup of the member cluster"
  az role assignment create --role "de2b316d-7a2c-4143-b4cd-c148f6a355a1" --assignee-object-id ${HUB_CLUSTER_KUBELET_PRINCIPAL_ID} --assignee-principal-type ServicePrincipal --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${AKS_MEMBER_1_NODE_RESOURCE_GROUP}" > /dev/null

  echo "Assigning roles to member-1 kubelet identity on the MC_resourceGroup of the member cluster"
  az role assignment create --role "Network Contributor" --assignee-object-id ${MEMBER_CLUSTER_1_KUBELET_PRINCIPAL_ID} --assignee-principal-type ServicePrincipal --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${AKS_MEMBER_1_NODE_RESOURCE_GROUP}" > /dev/null

  # Creating azure configuration file for the controllers
  echo "Generating azure configuration file:" $(pwd)/member_1_azure_config.yaml
  cat << EOF > $(pwd)/member_1_azure_config.yaml
  azureCloudConfig:
    cloud: "AzurePublicCloud"
    subscriptionId: "${AZURE_SUBSCRIPTION_ID}"
    useManagedIdentityExtension: true
    userAssignedIdentityID: "${MEMBER_CLUSTER_1_KUBELET_CLIENT_ID}"
    resourceGroup: "${AKS_MEMBER_1_NODE_RESOURCE_GROUP}"
    location: "${LOCATION}"

EOF

  AKS_MEMBER_2=$(az aks show -g $RESOURCE_GROUP -n $MEMBER_2_AZURE_CLUSTER)
  AKS_MEMBER_2_NODE_RESOURCE_GROUP=$(echo ${AKS_MEMBER_2} | jq -r '. | .nodeResourceGroup')

  echo "Assigning Azure Kubernetes Fleet Manager Hub Agent Role to hub kubelet identity on the MC_resourceGroup of the member cluster"
  az role assignment create --role "de2b316d-7a2c-4143-b4cd-c148f6a355a1" --assignee-object-id ${HUB_CLUSTER_KUBELET_PRINCIPAL_ID} --assignee-principal-type ServicePrincipal --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${AKS_MEMBER_2_NODE_RESOURCE_GROUP}" > /dev/null

  echo "Assigning roles to member-2 kubelet identity on the MC_resourceGroup of the member cluster"
  az role assignment create --role "Network Contributor" --assignee-object-id ${MEMBER_CLUSTER_2_KUBELET_PRINCIPAL_ID} --assignee-principal-type ServicePrincipal --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${AKS_MEMBER_2_NODE_RESOURCE_GROUP}" > /dev/null

  # Creating azure configuration file for the controllers
  echo "Generating azure configuration file:" $(pwd)/member_2_azure_config.yaml
  cat << EOF > $(pwd)/member_2_azure_config.yaml
  azureCloudConfig:
    cloud: "AzurePublicCloud"
    subscriptionId: "${AZURE_SUBSCRIPTION_ID}"
    useManagedIdentityExtension: true
    userAssignedIdentityID: "${MEMBER_CLUSTER_2_KUBELET_CLIENT_ID}"
    resourceGroup: "${AKS_MEMBER_2_NODE_RESOURCE_GROUP}"
    location: "${LOCATION}"
EOF
fi

# Export kubeconfig.
az aks get-credentials --name $HUB_AZURE_CLUSTER -g $RESOURCE_GROUP --admin --context "$HUB_CLUSTER-admin" --overwrite-existing
az aks get-credentials --name $MEMBER_1_AZURE_CLUSTER -g $RESOURCE_GROUP --admin --context "$MEMBER_CLUSTER_1-admin" --overwrite-existing
az aks get-credentials --name $MEMBER_2_AZURE_CLUSTER -g $RESOURCE_GROUP --admin --context "$MEMBER_CLUSTER_2-admin" --overwrite-existing
if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]
then
    az aks get-credentials --name $MEMBER_3_AZURE_CLUSTER -g $RESOURCE_GROUP --admin --context "$MEMBER_CLUSTER_3-admin" --overwrite-existing
    az aks get-credentials --name $MEMBER_4_AZURE_CLUSTER -g $RESOURCE_GROUP --admin --context "$MEMBER_CLUSTER_4-admin" --overwrite-existing
fi
HUB_URL="$(kubectl --context "$HUB_CLUSTER-admin" config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
export HUB_URL

# Setup hub cluster credentials.
if [ "$ENABLE_TRAFFIC_MANAGER" == "false" ]; then
  export CLIENT_ID_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_1_AZURE_CLUSTER"_"$MEMBER_1_LOCATION" | jq --arg identity $MEMBER_1_AZURE_CLUSTER-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
  export PRINCIPAL_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_1_AZURE_CLUSTER"_"$MEMBER_1_LOCATION" | jq --arg identity $MEMBER_1_AZURE_CLUSTER-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
  export CLIENT_ID_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_2_AZURE_CLUSTER"_"$MEMBER_2_LOCATION" | jq --arg identity $MEMBER_2_AZURE_CLUSTER-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
  export PRINCIPAL_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_2_AZURE_CLUSTER"_"$MEMBER_2_LOCATION" | jq --arg identity $MEMBER_2_AZURE_CLUSTER-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
  if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]
  then
    export CLIENT_ID_FOR_MEMBER_3=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_3_AZURE_CLUSTER"_"$MEMBER_LOCATION_2" | jq --arg identity $MEMBER_3_AZURE_CLUSTER-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
    export PRINCIPAL_FOR_MEMBER_3=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_3_AZURE_CLUSTER"_"$MEMBER_LOCATION_2" | jq --arg identity $MEMBER_3_AZURE_CLUSTER-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
    export CLIENT_ID_FOR_MEMBER_4=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_4_AZURE_CLUSTER"_"$MEMBER_LOCATION_2" | jq --arg identity $MEMBER_4_AZURE_CLUSTER-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
    export PRINCIPAL_FOR_MEMBER_4=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_4_AZURE_CLUSTER"_"$MEMBER_LOCATION_2" | jq --arg identity $MEMBER_4_AZURE_CLUSTER-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
    fi
else
  export CLIENT_ID_FOR_MEMBER_1=$MEMBER_CLUSTER_1_KUBELET_CLIENT_ID
  export PRINCIPAL_FOR_MEMBER_1=$MEMBER_CLUSTER_1_KUBELET_PRINCIPAL_ID
  export CLIENT_ID_FOR_MEMBER_2=$MEMBER_CLUSTER_2_KUBELET_CLIENT_ID
  export PRINCIPAL_FOR_MEMBER_2=$MEMBER_CLUSTER_2_KUBELET_PRINCIPAL_ID
fi

kubectl config use-context $HUB_CLUSTER-admin
if [ "${AZURE_NETWORK_SETTING}" != "perf-test" ]
then
    helm install e2e-hub-resources \
        ./examples/getting-started/charts/hub \
        --set memberClusterConfigs[0].memberID=$MEMBER_CLUSTER_1 \
        --set memberClusterConfigs[0].principalID=$PRINCIPAL_FOR_MEMBER_1 \
        --set memberClusterConfigs[1].memberID=$MEMBER_CLUSTER_2 \
        --set memberClusterConfigs[1].principalID=$PRINCIPAL_FOR_MEMBER_2
else
    helm install e2e-hub-resources \
        ./examples/getting-started/charts/hub \
        --set memberClusterConfigs[0].memberID=$MEMBER_CLUSTER_1 \
        --set memberClusterConfigs[0].principalID=$PRINCIPAL_FOR_MEMBER_1 \
        --set memberClusterConfigs[1].memberID=$MEMBER_CLUSTER_2 \
        --set memberClusterConfigs[1].principalID=$PRINCIPAL_FOR_MEMBER_2 \
        --set memberClusterConfigs[2].memberID=$MEMBER_CLUSTER_3 \
        --set memberClusterConfigs[2].principalID=$PRINCIPAL_FOR_MEMBER_3 \
        --set memberClusterConfigs[3].memberID=$MEMBER_CLUSTER_4 \
        --set memberClusterConfigs[3].principalID=$PRINCIPAL_FOR_MEMBER_4
fi

kubectl config use-context $MEMBER_CLUSTER_1-admin
helm install e2e-member-resources \
    ./examples/getting-started/charts/members \
    --set memberID=$MEMBER_CLUSTER_1

kubectl config use-context $MEMBER_CLUSTER_2-admin
helm install e2e-member-resources \
    ./examples/getting-started/charts/members \
    --set memberID=$MEMBER_CLUSTER_2

if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]
then
    kubectl config use-context $MEMBER_CLUSTER_3-admin
    helm install e2e-member-resources \
        ./examples/getting-started/charts/members \
        --set memberID=$MEMBER_CLUSTER_3

    kubectl config use-context $MEMBER_CLUSTER_4-admin
    helm install e2e-member-resources \
        ./examples/getting-started/charts/members \
        --set memberID=$MEMBER_CLUSTER_4
fi

# Helm install charts for hub cluster.
kubectl config use-context $HUB_CLUSTER-admin
# Install the Gateway API standard channel CRDs at the version selected in go.mod.
GATEWAY_API_CRD_DIR="$(go list -m -f '{{.Dir}}' sigs.k8s.io/gateway-api)/config/crd/standard"
if [ ! -d "$GATEWAY_API_CRD_DIR" ]; then
  echo "error: Gateway API standard CRD directory not found: $GATEWAY_API_CRD_DIR"
  exit 1
fi
GATEWAY_API_CRDS=(
  gateway.networking.k8s.io_gatewayclasses.yaml
  gateway.networking.k8s.io_gateways.yaml
  gateway.networking.k8s.io_httproutes.yaml
  gateway.networking.k8s.io_referencegrants.yaml
)
for crd in "${GATEWAY_API_CRDS[@]}"; do
  kubectl apply --server-side -f "$GATEWAY_API_CRD_DIR/$crd"
done
kubectl wait --for=condition=Established --timeout=60s \
  crd/gatewayclasses.gateway.networking.k8s.io \
  crd/gateways.gateway.networking.k8s.io \
  crd/httproutes.gateway.networking.k8s.io \
  crd/referencegrants.gateway.networking.k8s.io
# need to make sure the version matches the one in the go.mod
# workaround mentioned in https://github.com/kubernetes-sigs/controller-runtime/issues/1191
kubectl apply -f `go env GOPATH`/pkg/mod/go.goms.io/fleet@v0.18.16/config/crd/bases/cluster.kubernetes-fleet.io_internalmemberclusters.yaml
helm install hub-net-controller-manager \
    ./charts/hub-net-controller-manager/ \
    --set image.repository=$REGISTRY/hub-net-controller-manager \
    --set image.tag=$TAG \
    --set crdInstaller.enabled=true \
    --set crdInstaller.image.repository=$REGISTRY/net-crd-installer \
    --set crdInstaller.image.tag=$TAG \
    --set crdInstaller.isE2ETest=true \
        $( [ "$ENABLE_TRAFFIC_MANAGER" = "true" ] && echo "--set enableTrafficManagerFeature=true -f hub_azure_config.yaml" )

# Helm install charts for member clusters.
kubectl config use-context $MEMBER_CLUSTER_1-admin
helm install mcs-controller-manager \
    ./charts/mcs-controller-manager \
    --set image.repository=$REGISTRY/mcs-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_1 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_1 \
    $( [ "$AZURE_NETWORK_SETTING" = "unsupported" ] && echo "--set enableNetworkingFeatures=false" )
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
    --set image.repository=$REGISTRY/member-net-controller-manager \
    --set image.tag=$TAG \
    --set crdInstaller.enabled=true \
    --set crdInstaller.image.repository=$REGISTRY/net-crd-installer \
    --set crdInstaller.image.tag=$TAG \
    --set crdInstaller.isE2ETest=true \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_1 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_1 \
    $( [ "$AZURE_NETWORK_SETTING" = "unsupported" ] && echo "--set enableNetworkingFeatures=false" ) \
    $( [ "$ENABLE_TRAFFIC_MANAGER" = "true" ] && echo "--set enableTrafficManagerFeature=true -f member_1_azure_config.yaml" )

kubectl config use-context $MEMBER_CLUSTER_2-admin
helm install mcs-controller-manager \
    ./charts/mcs-controller-manager \
    --set image.repository=$REGISTRY/mcs-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_2 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_2 \
    $( [ "$AZURE_NETWORK_SETTING" = "unsupported" ]  && echo "--set enableNetworkingFeatures=false" )
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
    --set image.repository=$REGISTRY/member-net-controller-manager \
    --set image.tag=$TAG \
    --set crdInstaller.enabled=true \
    --set crdInstaller.image.repository=$REGISTRY/net-crd-installer \
    --set crdInstaller.image.tag=$TAG \
    --set crdInstaller.isE2ETest=true \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_2 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_2 \
    $( [ "$AZURE_NETWORK_SETTING" = "unsupported" ] && echo "--set enableNetworkingFeatures=false" ) \
    $( [ "$ENABLE_TRAFFIC_MANAGER" = "true" ] && echo "--set enableTrafficManagerFeature=true -f member_2_azure_config.yaml" )

# TODO(mainred): Before the app image is publicly available in MCR, we build and publish the image to the test registry.
# Build and publish the app image dedicated for fleet networking test.
# Skip this step if running the performance test.
if [ "${AZURE_NETWORK_SETTING}" != "perf-test" ]
then
    export APP_IMAGE=$REGISTRY/app
    docker build --platform linux/amd64 -f ./examples/getting-started/app/Dockerfile ./examples/getting-started/app --tag $APP_IMAGE
    # Refresh the three-hour ACR token after provisioning and building the app.
    az acr login -n "$REGISTRY_NAME"
    docker push $APP_IMAGE
fi

if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]
then
    kubectl config use-context $MEMBER_CLUSTER_3-admin
    helm install mcs-controller-manager \
        ./charts/mcs-controller-manager \
        --set image.repository=$REGISTRY/mcs-controller-manager \
        --set image.tag=$TAG \
        --set config.hubURL=$HUB_URL \
        --set config.provider=azure \
        --set config.memberClusterName=$MEMBER_CLUSTER_3 \
        --set azure.clientid=$CLIENT_ID_FOR_MEMBER_3
    helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
        --set image.repository=$REGISTRY/member-net-controller-manager \
        --set image.tag=$TAG \
        --set crdInstaller.enabled=true \
        --set crdInstaller.image.repository=$REGISTRY/net-crd-installer \
        --set crdInstaller.image.tag=$TAG \
        --set crdInstaller.isE2ETest=true \
        --set config.hubURL=$HUB_URL \
        --set config.provider=azure \
        --set config.memberClusterName=$MEMBER_CLUSTER_3 \
        --set azure.clientid=$CLIENT_ID_FOR_MEMBER_3
    
    kubectl config use-context $MEMBER_CLUSTER_4-admin
    helm install mcs-controller-manager \
        ./charts/mcs-controller-manager \
        --set image.repository=$REGISTRY/mcs-controller-manager \
        --set image.tag=$TAG \
        --set config.hubURL=$HUB_URL \
        --set config.provider=azure \
        --set config.memberClusterName=$MEMBER_CLUSTER_4 \
        --set azure.clientid=$CLIENT_ID_FOR_MEMBER_4
    helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
        --set image.repository=$REGISTRY/member-net-controller-manager \
        --set image.tag=$TAG \
        --set crdInstaller.enabled=true \
        --set crdInstaller.image.repository=$REGISTRY/net-crd-installer \
        --set crdInstaller.image.tag=$TAG \
        --set crdInstaller.isE2ETest=true \
        --set config.hubURL=$HUB_URL \
        --set config.provider=azure \
        --set config.memberClusterName=$MEMBER_CLUSTER_4 \
        --set azure.clientid=$CLIENT_ID_FOR_MEMBER_4
fi

if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]
then
    bash test/scripts/prometheus.sh
fi

if [[ "$ENABLE_AZURE_FRONT_DOOR" == true ]]; then
    bash test/scripts/install-afd.sh
fi
