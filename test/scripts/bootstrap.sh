#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

# check required variables
[[ -z "${E2E_AZURE_CLIENT_ID}" ]] && echo "E2E_AZURE_CLIENT_ID is not set" && exit 1
[[ -z "${E2E_AZURE_CLIENT_SECRET}" ]] && echo "E2E_AZURE_CLIENT_SECRET is not set" && exit 1
[[ -z "${AZURE_TENANT_ID}" ]] && echo "AZURE_TENANT_ID is not set" && exit 1
[[ -z "${E2E_SUBSCRIPTION_ID}" ]] && echo "E2E_SUBSCRIPTION_ID is not set" && exit 1

# az login
az login --service-principal -u "${E2E_AZURE_CLIENT_ID}" -p "${E2E_AZURE_CLIENT_SECRET}" --tenant "${AZURE_TENANT_ID}"
az account set -s ${E2E_SUBSCRIPTION_ID}

# create resource group
# RANDOM ID promises workflow runs don't interface one another.
export RESOURCE_GROUP="fleet-networking-e2e-$RANDOM"
export LOCATION=eastus
az group create --name $RESOURCE_GROUP --location $LOCATION --tags "source=fleet-networking"

# defer function to recycle created Azure resource
function cleanup {
    #az group delete -n $RESOURCE_GROUP --no-wait --yes
    echo "resource group name $RESOURCE_GROUP"
}
trap cleanup EXIT

# pubilsh image
# TODO(mainred): once we have a specific Azure sub for fleet networking e2e test, we can reuse this registry
# Parameter 'registry_name' must conform to the following pattern: '^[a-zA-Z0-9]*$'.
export REGISTRY_NAME="fleetnetworkinge2e$RANDOM"
az acr create -g $RESOURCE_GROUP -n $REGISTRY_NAME --sku basic --tags "source=fleet-networking"
az acr login -n $REGISTRY_NAME
export REGISTRY=$REGISTRY_NAME.azurecr.io
export TAG=`git rev-parse --short=7 HEAD`
make docker-build-hub-net-controller-manager
make docker-build-member-net-controller-manager
make docker-build-mcs-controller-manager

# create hub and member clusters and wait until all clusters are ready
export HUB_CLUSTER=hub
export MEMBER_CLUSTER_1=member-1
export MEMBER_CLUSTER_2=member-2

NETWORK_SETTING="${NETWORK_SETTING:-shared-vnet}"
case $NETWORK_SETTING in
        shared-vnet)
                bash test/scripts/aks-shared-vnet.sh
                ;;
        *)
                echo "$NETWORK_SETTING is supported"
                exit 1
                ;;
esac

az aks wait --created --interval 10 --name $HUB_CLUSTER --resource-group $RESOURCE_GROUP --timeout 1800
az aks wait --created --interval 10 --name $MEMBER_CLUSTER_1 --resource-group $RESOURCE_GROUP --timeout 1800
az aks wait --created --interval 10 --name $MEMBER_CLUSTER_2 --resource-group $RESOURCE_GROUP --timeout 1800

# export kubeconfig
az aks get-credentials --name $HUB_CLUSTER -g $RESOURCE_GROUP --admin
az aks get-credentials --name $MEMBER_CLUSTER_1 -g $RESOURCE_GROUP --admin
az aks get-credentials --name $MEMBER_CLUSTER_2 -g $RESOURCE_GROUP --admin
export HUB_URL=$(cat ~/.kube/config | yq eval ".clusters | .[] | select(.name=="\"$HUB_CLUSTER\"") | .cluster.server")

# setup hub cluster credentials
export CLIENT_ID_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_1"_"$LOCATION" | jq --arg identity $MEMBER_CLUSTER_1-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
export PRINCIPAL_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_1"_"$LOCATION" | jq --arg identity $MEMBER_CLUSTER_1-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
export CLIENT_ID_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_2"_"$LOCATION" | jq --arg identity $MEMBER_CLUSTER_2-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
export PRINCIPAL_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_2"_"$LOCATION" | jq --arg identity $MEMBER_CLUSTER_2-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')

kubectl config use-context $HUB_CLUSTER-admin
helm install e2e-hub-resources \
    ./test/charts/hub \
    --set principalIDForMemberA=$PRINCIPAL_FOR_MEMBER_1 \
    --set principalIDForMemberB=$PRINCIPAL_FOR_MEMBER_2

kubectl config use-context $MEMBER_CLUSTER_1-admin
helm install e2e-member-resources \
    ./test/charts/member \
    --set memberID=$MEMBER_CLUSTER_1

kubectl config use-context $MEMBER_CLUSTER_2-admin
helm install e2e-member-resources \
    ./test/charts/member \
    --set memberID=$MEMBER_CLUSTER_2

# helm install charts for hub cluster
kubectl config use-context $HUB_CLUSTER-admin
kubectl apply -f config/crd/*
helm install hub-net-controller-manager \
    ./charts/hub-net-controller-manager/ \
    --set image.repository=$REGISTRY/hub-net-controller-manager \
    --set image.tag=$TAG

# TODO: apply internalmembercluster CRDs
# helm install charts for member clusters
kubectl config use-context $MEMBER_CLUSTER_1-admin
kubectl apply -f config/crd/*
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