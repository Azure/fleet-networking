#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x
source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"


# peered-vnet creates two member clusters with network peered by virtual network peering. The vnet and member clusters
# are hosted in different regions.

# Member cluster setup steps:
# 1. Create one virtual network and one subnet for each member cluster
# 2. Create a virtual network peering connection to peer the above two virtual networks
# 3. Create member cluster 1 with Azure CNI and subnet from step 1
# 4. Create member cluster 2 with Azure CNI and subnet from step 1

# Reference of the vnet peer setup:
# https://docs.microsoft.com/en-us/azure/virtual-network/tutorial-connect-virtual-networks-cli

# Create virutal network and subnet for both member clusters.
export MEMBER_1_TO_MEMBER_2=member-1-to-member-2
export MEMBER_2_TO_MEMBER_1=member-2-to-member-1
export MEMBER_1_VNET="${E2E_RESOURCE_PREFIX}-member-1-vnet"
export MEMBER_1_SUBNET=member-1-subnet
export MEMBER_2_VNET="${E2E_RESOURCE_PREFIX}-member-2-vnet"
export MEMBER_2_SUBNET=member-2-subnet

az network vnet create \
    --location $MEMBER_1_LOCATION \
    --address-prefixes 10.1.0.0/16 \
    --name $MEMBER_1_VNET \
    --resource-group $RESOURCE_GROUP \
    --subnet-name $MEMBER_1_SUBNET \
    --subnet-prefixes 10.1.0.0/24 "${E2E_RESOURCE_TAGS[@]}"

az network vnet create \
    --location $MEMBER_2_LOCATION \
    --address-prefixes 10.2.0.0/16 \
    --name $MEMBER_2_VNET \
    --resource-group $RESOURCE_GROUP \
    --subnet-name $MEMBER_2_SUBNET \
    --subnet-prefixes 10.2.0.0/24 "${E2E_RESOURCE_TAGS[@]}"

# Get the id for MEMBER_1_VNET.
MEMBER_1_VNET_ID=$(az network vnet show \
  --resource-group $RESOURCE_GROUP \
  --name $MEMBER_1_VNET \
  --query id --out tsv)

# Get the id for MEMBER_2_VNET.
MEMBER_2_VNET_ID=$(az network vnet show \
  --resource-group $RESOURCE_GROUP \
  --name $MEMBER_2_VNET \
  --query id \
  --out tsv)

az network vnet peering create \
    --resource-group $RESOURCE_GROUP \
    -n $MEMBER_1_TO_MEMBER_2 \
    --vnet-name $MEMBER_1_VNET \
    --remote-vnet $MEMBER_2_VNET_ID \
    --allow-vnet-access

az network vnet peering create \
  --name $MEMBER_2_TO_MEMBER_1 \
  --resource-group $RESOURCE_GROUP \
  --vnet-name $MEMBER_2_VNET \
  --remote-vnet $MEMBER_1_VNET_ID \
  --allow-vnet-access

# Create aks member cluster1, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
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
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$MEMBER_1_VNET/subnets/$MEMBER_1_SUBNET" \
    --no-wait "${E2E_RESOURCE_TAGS[@]}"

# Create aks member cluster2, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
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
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$MEMBER_2_VNET/subnets/$MEMBER_2_SUBNET" \
    --no-wait "${E2E_RESOURCE_TAGS[@]}"
