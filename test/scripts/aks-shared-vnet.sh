#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

# Create virutal network and subnet for both member clusters.
export VNET=fleet
export MEMBER_1_SUBNET=member-1
export MEMBER_2_SUBNET=member-2

az network vnet create \
    --name $VNET \
    --location $LOCATION \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.0.0.0/8

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

# Create aks member cluster1.
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_1 \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_1_SUBNET" \
    --no-wait

# Create aks member cluster2.
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_2 \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_2_SUBNET" \
    --no-wait
