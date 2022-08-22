#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

# create virutal network
export VNET=fleet
export HUB_SUBNET=hub
export MEMBER_1_SUBNET=member-1
export MEMBER_2_SUBNET=member-2

az network vnet create \
    --name $VNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.0.0.0/8
az network vnet subnet create \
    --vnet-name $VNET \
    --name $HUB_SUBNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.1.0.0/16
az network vnet subnet create \
    --vnet-name $VNET \
    --name $MEMBER_1_SUBNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.2.0.0/16
az network vnet subnet create \
    --vnet-name $VNET \
    --name $MEMBER_2_SUBNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.3.0.0/16

# create aks hub cluster
export NODE_COUNT=2
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $HUB_CLUSTER \
    --attach-acr $REGISTRY_NAME \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$HUB_SUBNET" \
    --yes

# create aks member cluster1
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_1 \
    --attach-acr $REGISTRY_NAME \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_1_SUBNET" \
    --yes

# create aks member cluster2
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_2 \
    --attach-acr $REGISTRY_NAME \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_2_SUBNET" \
    --yes
