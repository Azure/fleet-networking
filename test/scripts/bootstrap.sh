#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

# Check required variables.
[[ -z "${AZURE_SUBSCRIPTION_ID}" ]] && echo "AZURE_SUBSCRIPTION_ID is not set" && exit 1

az account set -s ${AZURE_SUBSCRIPTION_ID}

# Create resource group to host hub and member clusters.
# RANDOM ID promises workflow runs don't interface one another.
export RESOURCE_GROUP="${AZURE_RESOURCE_GROUP:-fleet-networking-e2e-$RANDOM}"
export LOCATION=eastus
az group create --name $RESOURCE_GROUP --location $LOCATION --tags "source=fleet-networking"

# Defer function to recycle created Azure resource.
function cleanup {
    az group delete -n $RESOURCE_GROUP --no-wait --yes
}
trap cleanup INT TERM

# Pubilsh fleet networking agent images.
# TODO(mainred): Once we have a specific Azure sub for fleet networking e2e test, we can reuse that registry.
# Registry name must conform to the following pattern: '^[a-zA-Z0-9]*$'.
export REGISTRY_NAME="fleetnetworkinge2e$RANDOM"
az acr create -g $RESOURCE_GROUP -n $REGISTRY_NAME --sku standard --tags "source=fleet-networking"
# Enable anonymous to not wait for the long-running AKS creation.
# When attach-acr and `--enable-managed-identity` are both specified, AKS requires us to wait until the whole operation
# succeeds, and as AuthN and AuthZ for member cluster agents to hub cluster in our test requires managed identity,
# we enable anonymous pull access to the registry instead of enabling attach-acr.
az acr update --name $REGISTRY_NAME --anonymous-pull-enabled
az acr login -n $REGISTRY_NAME
export REGISTRY=$REGISTRY_NAME.azurecr.io
export TAG=`git rev-parse --short=7 HEAD`
make docker-build-hub-net-controller-manager
make docker-build-member-net-controller-manager
make docker-build-mcs-controller-manager

# Create hub and member clusters and wait until all clusters are ready.
export HUB_CLUSTER=hub
export MEMBER_CLUSTER_1=member-1
export MEMBER_CLUSTER_2=member-2
export NODE_COUNT=2

# Create aks hub cluster
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $HUB_CLUSTER \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --no-wait

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
                export MEMBER_1_LOCATION="${MEMBER_1_LOCATION:-eastus}"
                export MEMBER_2_LOCATION="${MEMBER_2_LOCATION:-westus}"
                bash test/scripts/aks-peered-vnet.sh
                ;;
        *)
                echo "$AZURE_NETWORK_SETTING is supported"
                exit 1
                ;;
esac

az aks wait --created --interval 10 --name $HUB_CLUSTER --resource-group $RESOURCE_GROUP --timeout 1800
az aks wait --created --interval 10 --name $MEMBER_CLUSTER_1 --resource-group $RESOURCE_GROUP --timeout 1800
az aks wait --created --interval 10 --name $MEMBER_CLUSTER_2 --resource-group $RESOURCE_GROUP --timeout 1800

# Export kubeconfig.
az aks get-credentials --name $HUB_CLUSTER -g $RESOURCE_GROUP --admin --overwrite-existing
az aks get-credentials --name $MEMBER_CLUSTER_1 -g $RESOURCE_GROUP --admin --overwrite-existing
az aks get-credentials --name $MEMBER_CLUSTER_2 -g $RESOURCE_GROUP --admin --overwrite-existing
export HUB_URL=$(cat ~/.kube/config | yq eval ".clusters | .[] | select(.name=="\"$HUB_CLUSTER\"") | .cluster.server")

# Setup hub cluster credentials.
export CLIENT_ID_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_1"_"$MEMBER_1_LOCATION" | jq --arg identity $MEMBER_CLUSTER_1-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
export PRINCIPAL_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_1"_"$MEMBER_1_LOCATION" | jq --arg identity $MEMBER_CLUSTER_1-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
export CLIENT_ID_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_2"_"$MEMBER_2_LOCATION" | jq --arg identity $MEMBER_CLUSTER_2-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
export PRINCIPAL_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_2"_"$MEMBER_2_LOCATION" | jq --arg identity $MEMBER_CLUSTER_2-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')

kubectl config use-context $HUB_CLUSTER-admin
helm install e2e-hub-resources \
    ./examples/getting-started/charts/hub \
    --set principalIDForMemberA=$PRINCIPAL_FOR_MEMBER_1 \
    --set principalIDForMemberB=$PRINCIPAL_FOR_MEMBER_2

kubectl config use-context $MEMBER_CLUSTER_1-admin
helm install e2e-member-resources \
    ./examples/getting-started/charts/members \
    --set memberID=$MEMBER_CLUSTER_1

kubectl config use-context $MEMBER_CLUSTER_2-admin
helm install e2e-member-resources \
    ./examples/getting-started/charts/members \
    --set memberID=$MEMBER_CLUSTER_2

# Helm install charts for hub cluster.
kubectl config use-context $HUB_CLUSTER-admin
kubectl apply -f config/crd/*
helm install hub-net-controller-manager \
    ./charts/hub-net-controller-manager/ \
    --set image.repository=$REGISTRY/hub-net-controller-manager \
    --set image.tag=$TAG

# Helm install charts for member clusters.
kubectl config use-context $MEMBER_CLUSTER_1-admin
kubectl apply -f config/crd/*
kubectl apply -f `go env GOPATH`/pkg/mod/go.goms.io/fleet@v0.3.0/config/crd/bases/fleet.azure.com_internalmemberclusters.yaml
helm install mcs-controller-manager \
    ./charts/mcs-controller-manager \
    --set image.repository=$REGISTRY/mcs-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_1 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_1
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
    --set image.repository=$REGISTRY/member-net-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_1 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_1

kubectl config use-context $MEMBER_CLUSTER_2-admin
kubectl apply -f config/crd/*
kubectl apply -f `go env GOPATH`/pkg/mod/go.goms.io/fleet@v0.3.0/config/crd/bases/fleet.azure.com_internalmemberclusters.yaml
helm install mcs-controller-manager \
    ./charts/mcs-controller-manager \
    --set image.repository=$REGISTRY/mcs-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_2 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_2
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
    --set image.repository=$REGISTRY/member-net-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_2 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_2
