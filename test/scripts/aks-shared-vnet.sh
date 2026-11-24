#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x
source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"

# shared-vnet creates two member clusters from subnets within one vnet.

# Member cluster setup steps:
# 1. Create one virtual network and two subnets, each for one member cluster
# 2. Create member cluster 1 with Azure CNI and subnet from step 1
# 3. Create member cluster 2 with Azure CNI and the other subnet from step 1

# Reference of the vnet peer setup:
# https://docs.microsoft.com/en-us/azure/virtual-network/tutorial-connect-virtual-networks-cli

# Create virutal network and subnet for both member clusters.
export VNET="${E2E_RESOURCE_PREFIX:+${E2E_RESOURCE_PREFIX}-}fleet"
export MEMBER_1_SUBNET=member-1
export MEMBER_2_SUBNET=member-2

az network vnet create \
    --name $VNET \
    --location $MEMBER_1_LOCATION \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.0.0.0/8 "${E2E_RESOURCE_TAGS[@]}"

az network vnet subnet create \
    --vnet-name $VNET \
    --name $MEMBER_1_SUBNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.1.0.0/16
az network vnet subnet create \
    --vnet-name $VNET \
    --name $MEMBER_2_SUBNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.2.0.0/16

# Create aks member cluster1, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
if [ "$ENABLE_TRAFFIC_MANAGER" == "false" ]; then
  az aks create \
    --location $MEMBER_1_LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_1_AZURE_CLUSTER \
    --node-count $NODE_COUNT \
    --node-vm-size Standard_A2_v2 \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_1_SUBNET" \
    --no-wait "${E2E_RESOURCE_TAGS[@]}"
else
  az aks create \
     --location $MEMBER_1_LOCATION \
     --resource-group $RESOURCE_GROUP \
     --name $MEMBER_1_AZURE_CLUSTER \
     --node-count $NODE_COUNT \
     --node-vm-size Standard_A2_v2 \
     --generate-ssh-keys \
     --enable-aad \
     --enable-azure-rbac \
     --network-plugin azure \
     --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_1_SUBNET" \
     --enable-managed-identity --assign-identity ${MEMBER_CLUSTER_1_AKS_IDENTITY_ID} --assign-kubelet-identity ${MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY_ID} \
     --no-wait "${E2E_RESOURCE_TAGS[@]}"
fi

# Create aks member cluster2, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
if [ "$ENABLE_TRAFFIC_MANAGER" == "false" ]; then
  az aks create \
    --location $MEMBER_2_LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_2_AZURE_CLUSTER \
    --node-count $NODE_COUNT \
    --node-vm-size Standard_A2_v2 \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_2_SUBNET" \
    --no-wait "${E2E_RESOURCE_TAGS[@]}"
else
  az aks create \
      --location $MEMBER_2_LOCATION \
      --resource-group $RESOURCE_GROUP \
      --name $MEMBER_2_AZURE_CLUSTER \
      --node-count $NODE_COUNT \
      --node-vm-size Standard_A2_v2 \
      --generate-ssh-keys \
      --enable-aad \
      --enable-azure-rbac \
      --network-plugin azure \
      --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_2_SUBNET" \
      --enable-managed-identity --assign-identity ${MEMBER_CLUSTER_2_AKS_IDENTITY_ID} --assign-kubelet-identity ${MEMBER_CLUSTER_2_AKS_KUBELET_IDENTITY_ID} \
      --no-wait "${E2E_RESOURCE_TAGS[@]}"
fi
