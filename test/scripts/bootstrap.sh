#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

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

az account set -s ${AZURE_SUBSCRIPTION_ID}

# Create resource group to host hub and member clusters.
# RANDOM ID promises workflow runs don't interface one another.
export RESOURCE_GROUP="${AZURE_RESOURCE_GROUP:-fleet-networking-e2e-$RANDOM}"
export LOCATION="${LOCATION:-eastus2}"
az group create --name $RESOURCE_GROUP --location $LOCATION --tags "source=fleet-networking"

# Defer function to recycle created Azure resource.
function cleanup {
    az group delete -n $RESOURCE_GROUP --no-wait --yes
}
trap cleanup INT TERM

# Pubilsh fleet networking agent images.
# TODO(mainred): Once we have a specific Azure sub for fleet networking e2e test, we can reuse that registry.
# Registry name must conform to the following pattern: '^[a-zA-Z0-9]*$'.
export REGISTRY_NAME="${RESOURCE_GROUP//-}"
az acr create -g $RESOURCE_GROUP -n $REGISTRY_NAME --sku standard --tags "source=fleet-networking"
# Enable anonymous to not wait for the long-running AKS creation.
# When attach-acr and `--enable-managed-identity` are both specified, AKS requires us to wait until the whole operation
# succeeds, and as AuthN and AuthZ for member cluster agents to hub cluster in our test requires managed identity,
# we enable anonymous pull access to the registry instead of enabling attach-acr.
az acr update --name $REGISTRY_NAME --anonymous-pull-enabled -g $RESOURCE_GROUP
az acr login -n $REGISTRY_NAME
export REGISTRY=$REGISTRY_NAME.azurecr.io
export TAG=$(git rev-parse --short=7 HEAD)
make push

# Create hub and member clusters and wait until all clusters are ready.
export HUB_CLUSTER=${HUB_CLUSTER:-"hub"}
export MEMBER_CLUSTER_1=${MEMBER_CLUSTER_1:-"member-1"}
export MEMBER_CLUSTER_2=${MEMBER_CLUSTER_2:-"member-2"}
export NODE_COUNT=2

export HUB_CLUSTER_AKS_ID_NAME=${HUB_CLUSTER_AKS_ID_NAME:-"fleet-net-hub-aks-id"}
export HUB_CLUSTER_AKS_KUBELET_ID_NAME=${HUB_CLUSTER_AKS_KUBELET_ID_NAME:-"fleet-net-hub-aks-kubelet-id"}
export MEMBER_CLUSTER_1_AKS_ID_NAME=${MEMBER_CLUSTER_1_AKS_ID_NAME:-"fleet-net-member-1-aks-id"}
export MEMBER_CLUSTER_1_AKS_KUBELET_ID_NAME=${MEMBER_CLUSTER_1_AKS_KUBELET_ID_NAME:-"fleet-net-member-1-aks-kubelet-id"}
export MEMBER_CLUSTER_2_AKS_ID_NAME=${MEMBER_CLUSTER_2_AKS_ID_NAME:-"fleet-net-member-2-aks-id"}
export MEMBER_CLUSTER_2_AKS_KUBELET_ID_NAME=${MEMBER_CLUSTER_2_AKS_KUBELET_ID_NAME:-"fleet-net-member-2-aks-kubelet-id"}



if [ "$ENABLE_TRAFFIC_MANAGER" == "false" ]; then
  # Create aks hub cluster, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
  echo "Creating aks cluster: ${HUB_CLUSTER}"
  az aks create \
       --location $LOCATION \
       --resource-group $RESOURCE_GROUP \
       --name $HUB_CLUSTER \
       --node-count $NODE_COUNT \
       --node-vm-size Standard_A2_v2 \
       --generate-ssh-keys \
       --enable-aad \
       --enable-azure-rbac \
       --network-plugin azure \
       --no-wait
else
  # Create aks identity
  echo "Creating hub control-plane identity: ${HUB_CLUSTER_AKS_ID_NAME}"
  HUB_CLUSTER_AKS_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${HUB_CLUSTER_AKS_ID_NAME})
  echo "Creating hub kubelet identity: ${HUB_CLUSTER_AKS_KUBELET_ID_NAME}"
  HUB_CLUSTER_AKS_KUBELET_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${HUB_CLUSTER_AKS_KUBELET_ID_NAME})

  HUB_CLUSTER_AKS_IDENTITY_ID=$(echo ${HUB_CLUSTER_AKS_IDENTITY} | jq -r '. | .id')
  HUB_CLUSTER_AKS_KUBELET_IDENTITY_ID=$(echo ${HUB_CLUSTER_AKS_KUBELET_IDENTITY} | jq -r '. | .id')
  HUB_CLUSTER_KUBELET_PRINCIPAL_ID=$(echo ${HUB_CLUSTER_AKS_KUBELET_IDENTITY} | jq -r '. | .principalId')
  HUB_CLUSTER_KUBELET_CLIENT_ID=$(echo ${HUB_CLUSTER_AKS_KUBELET_IDENTITY} | jq -r '. | .clientId')

  echo "Creating aks cluster: ${HUB_CLUSTER}"
  # Create aks hub cluster, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
  az aks create \
         --location $LOCATION \
         --resource-group $RESOURCE_GROUP \
         --name $HUB_CLUSTER \
         --node-count $NODE_COUNT \
         --node-vm-size Standard_A2_v2 \
         --generate-ssh-keys \
         --enable-aad \
         --enable-azure-rbac \
         --network-plugin azure \
         --enable-managed-identity --assign-identity ${HUB_CLUSTER_AKS_IDENTITY_ID} --assign-kubelet-identity ${HUB_CLUSTER_AKS_KUBELET_IDENTITY_ID} \
         --no-wait

  # Create aks identity for member-1
  echo "Creating member-1 control-plane identity: ${MEMBER_CLUSTER_1_AKS_ID_NAME}"
  MEMBER_CLUSTER_1_AKS_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${MEMBER_CLUSTER_1_AKS_ID_NAME})
  echo "Creating member-1 kubelet identity: ${MEMBER_CLUSTER_1_AKS_KUBELET_ID_NAME}"
  MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${MEMBER_CLUSTER_1_AKS_KUBELET_ID_NAME})

  export MEMBER_CLUSTER_1_AKS_IDENTITY_ID=$(echo ${MEMBER_CLUSTER_1_AKS_IDENTITY} | jq -r '. | .id')
  export MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY_ID=$(echo ${MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY} | jq -r '. | .id')
  export MEMBER_CLUSTER_1_KUBELET_PRINCIPAL_ID=$(echo ${MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY} | jq -r '. | .principalId')
  export MEMBER_CLUSTER_1_KUBELET_CLIENT_ID=$(echo ${MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY} | jq -r '. | .clientId')

  # Create aks identity for member-2
  echo "Creating member-2 control-plane identity: ${MEMBER_CLUSTER_2_AKS_ID_NAME}"
  MEMBER_CLUSTER_2_AKS_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${MEMBER_CLUSTER_2_AKS_ID_NAME})
  echo "Creating member-2 kubelet identity: ${MEMBER_CLUSTER_2_AKS_KUBELET_ID_NAME}"
  MEMBER_CLUSTER_2_AKS_KUBELET_IDENTITY=$(az identity create -g ${RESOURCE_GROUP} -n ${MEMBER_CLUSTER_2_AKS_KUBELET_ID_NAME})

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
                export MEMBER_CLUSTER_3=member-3
                export MEMBER_CLUSTER_4=member-4
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

az aks wait --created --interval 10 --name $HUB_CLUSTER --resource-group $RESOURCE_GROUP --timeout 1800
az aks wait --created --interval 10 --name $MEMBER_CLUSTER_1 --resource-group $RESOURCE_GROUP --timeout 1800
az aks wait --created --interval 10 --name $MEMBER_CLUSTER_2 --resource-group $RESOURCE_GROUP --timeout 1800
if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]
then
    az aks wait --created --interval 10 --name $MEMBER_CLUSTER_3 --resource-group $RESOURCE_GROUP --timeout 1800
    az aks wait --created --interval 10 --name $MEMBER_CLUSTER_4 --resource-group $RESOURCE_GROUP --timeout 1800
fi

# hack: add role assignments to kubelet identity
if [ "$ENABLE_TRAFFIC_MANAGER" == "true" ]; then
  AKS_HUB_CLUSTER=$(az aks show -g $RESOURCE_GROUP -n $HUB_CLUSTER)
  AKS_HUB_CLUSTER_NODE_RESOURCE_GROUP=$(echo ${AKS_HUB_CLUSTER} | jq -r '. | .nodeResourceGroup')

  echo "Assigning Azure Kubernetes Fleet Manager Hub Agent Role to hub kubelet identity on the resourceGroup of the hub cluster"
  az role assignment create --role "de2b316d-7a2c-4143-b4cd-c148f6a355a1" --assignee ${HUB_CLUSTER_KUBELET_PRINCIPAL_ID} --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${RESOURCE_GROUP}" > /dev/null

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

  AKS_MEMBER_1=$(az aks show -g $RESOURCE_GROUP -n $MEMBER_CLUSTER_1)
  AKS_MEMBER_1_NODE_RESOURCE_GROUP=$(echo ${AKS_MEMBER_1} | jq -r '. | .nodeResourceGroup')

  echo "Assigning Azure Kubernetes Fleet Manager Hub Agent Role to hub kubelet identity on the MC_resourceGroup of the member cluster"
  az role assignment create --role "de2b316d-7a2c-4143-b4cd-c148f6a355a1" --assignee ${HUB_CLUSTER_KUBELET_PRINCIPAL_ID} --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${AKS_MEMBER_1_NODE_RESOURCE_GROUP}" > /dev/null

  echo "Assigning roles to member-1 kubelet identity on the MC_resourceGroup of the member cluster"
  az role assignment create --role "Network Contributor" --assignee ${MEMBER_CLUSTER_1_KUBELET_PRINCIPAL_ID} --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${AKS_MEMBER_1_NODE_RESOURCE_GROUP}" > /dev/null

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

  AKS_MEMBER_2=$(az aks show -g $RESOURCE_GROUP -n $MEMBER_CLUSTER_2)
  AKS_MEMBER_2_NODE_RESOURCE_GROUP=$(echo ${AKS_MEMBER_2} | jq -r '. | .nodeResourceGroup')

  echo "Assigning Azure Kubernetes Fleet Manager Hub Agent Role to hub kubelet identity on the MC_resourceGroup of the member cluster"
  az role assignment create --role "de2b316d-7a2c-4143-b4cd-c148f6a355a1" --assignee ${HUB_CLUSTER_KUBELET_PRINCIPAL_ID} --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${AKS_MEMBER_2_NODE_RESOURCE_GROUP}" > /dev/null

  echo "Assigning roles to member-2 kubelet identity on the MC_resourceGroup of the member cluster"
  az role assignment create --role "Network Contributor" --assignee ${MEMBER_CLUSTER_2_KUBELET_PRINCIPAL_ID} --scope "/subscriptions/${AZURE_SUBSCRIPTION_ID}/resourceGroups/${AKS_MEMBER_2_NODE_RESOURCE_GROUP}" > /dev/null

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
az aks get-credentials --name $HUB_CLUSTER -g $RESOURCE_GROUP --admin --overwrite-existing
az aks get-credentials --name $MEMBER_CLUSTER_1 -g $RESOURCE_GROUP --admin --overwrite-existing
az aks get-credentials --name $MEMBER_CLUSTER_2 -g $RESOURCE_GROUP --admin --overwrite-existing
if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]
then
    az aks get-credentials --name $MEMBER_CLUSTER_3 -g $RESOURCE_GROUP --admin --overwrite-existing
    az aks get-credentials --name $MEMBER_CLUSTER_4 -g $RESOURCE_GROUP --admin --overwrite-existing
fi
export HUB_URL=$(cat ~/.kube/config | yq eval ".clusters | .[] | select(.name=="\"$HUB_CLUSTER\"") | .cluster.server")

# Setup hub cluster credentials.
if [ "$ENABLE_TRAFFIC_MANAGER" == "false" ]; then
  export CLIENT_ID_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_1"_"$MEMBER_1_LOCATION" | jq --arg identity $MEMBER_CLUSTER_1-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
  export PRINCIPAL_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_1"_"$MEMBER_1_LOCATION" | jq --arg identity $MEMBER_CLUSTER_1-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
  export CLIENT_ID_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_2"_"$MEMBER_2_LOCATION" | jq --arg identity $MEMBER_CLUSTER_2-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
  export PRINCIPAL_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_2"_"$MEMBER_2_LOCATION" | jq --arg identity $MEMBER_CLUSTER_2-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
  if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]
  then
    export CLIENT_ID_FOR_MEMBER_3=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_3"_"$MEMBER_LOCATION_2" | jq --arg identity $MEMBER_CLUSTER_3-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
    export PRINCIPAL_FOR_MEMBER_3=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_3"_"$MEMBER_LOCATION_2" | jq --arg identity $MEMBER_CLUSTER_3-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
    export CLIENT_ID_FOR_MEMBER_4=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_4"_"$MEMBER_LOCATION_2" | jq --arg identity $MEMBER_CLUSTER_4-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
    export PRINCIPAL_FOR_MEMBER_4=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_4"_"$MEMBER_LOCATION_2" | jq --arg identity $MEMBER_CLUSTER_4-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
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
# need to make sure the version matches the one in the go.mod
# workaround mentioned in https://github.com/kubernetes-sigs/controller-runtime/issues/1191
kubectl apply -f `go env GOPATH`/pkg/mod/go.goms.io/fleet@v0.14.0/config/crd/bases/cluster.kubernetes-fleet.io_internalmemberclusters.yaml
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
