#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x
source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"

# This script creates 4 member clusters in 2 separate groups; each group has its own
# virtual network. The two virtual networks are peer-linked, so as to allow connectivity
# between clusters in different groups.

# Create the two virtual networks; each has two subnets for two member clusters.
export VNET_1="${E2E_RESOURCE_PREFIX}-vnet-1"
export VNET_2="${E2E_RESOURCE_PREFIX}-vnet-2"
export MEMBER_SUBNET_1=subnet-1
export MEMBER_SUBNET_2=subnet-2

az network vnet create \
    --name $VNET_1 \
    --location $MEMBER_LOCATION_1 \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.1.0.0/16 "${E2E_RESOURCE_TAGS[@]}"
az network vnet subnet create \
    --vnet-name $VNET_1 \
    --name $MEMBER_SUBNET_1 \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.1.1.0/24
az network vnet subnet create \
    --vnet-name $VNET_1 \
    --name $MEMBER_SUBNET_2 \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.1.2.0/24

az network vnet create \
    --name $VNET_2 \
    --location $MEMBER_LOCATION_2 \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.2.0.0/16 "${E2E_RESOURCE_TAGS[@]}"
az network vnet subnet create \
    --vnet-name $VNET_2 \
    --name $MEMBER_SUBNET_1 \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.2.1.0/24
az network vnet subnet create \
    --vnet-name $VNET_2 \
    --name $MEMBER_SUBNET_2 \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.2.2.0/24

# Peer link the two virtual networks.
VNET_1_ID=$(az network vnet show \
  --resource-group $RESOURCE_GROUP \
  --name $VNET_1 \
  --query id --out tsv)
VNET_2_ID=$(az network vnet show \
  --resource-group $RESOURCE_GROUP \
  --name $VNET_2 \
  --query id --out tsv)

az network vnet peering create \
    --resource-group $RESOURCE_GROUP \
    -n "${VNET_1}-to-${VNET_2}" \
    --vnet-name $VNET_1 \
    --remote-vnet $VNET_2_ID \
    --allow-vnet-access
az network vnet peering create \
    --resource-group $RESOURCE_GROUP \
    --name "${VNET_2}-to-${VNET_1}" \
    --vnet-name $VNET_2 \
    --remote-vnet $VNET_1_ID \
    --allow-vnet-access

# Create the member clusters, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
az aks create \
    --location $MEMBER_LOCATION_1 \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_1_AZURE_CLUSTER \
    --node-count $NODE_COUNT \
    --node-vm-size Standard_A2_v2 \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET_1/subnets/$MEMBER_SUBNET_1" \
    --no-wait "${E2E_RESOURCE_TAGS[@]}"

az aks create \
    --location $MEMBER_LOCATION_1 \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_2_AZURE_CLUSTER \
    --node-count $NODE_COUNT \
    --node-vm-size Standard_A2_v2 \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET_1/subnets/$MEMBER_SUBNET_2" \
    --no-wait "${E2E_RESOURCE_TAGS[@]}"

az aks create \
    --location $MEMBER_LOCATION_2 \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_3_AZURE_CLUSTER \
    --node-count $NODE_COUNT \
    --node-vm-size Standard_A2_v2 \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET_2/subnets/$MEMBER_SUBNET_1" \
    --no-wait "${E2E_RESOURCE_TAGS[@]}"

az aks create \
    --location $MEMBER_LOCATION_2 \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_4_AZURE_CLUSTER \
    --node-count $NODE_COUNT \
    --node-vm-size Standard_A2_v2 \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET_2/subnets/$MEMBER_SUBNET_2" \
    --no-wait "${E2E_RESOURCE_TAGS[@]}"
