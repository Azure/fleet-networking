#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

# Reference of the setting up:
# https://docs.microsoft.com/en-us/azure/aks/configure-azure-cni#configure-networking---cli-with-dynamic-allocation-of-ips-and-enhanced-subnet-support

# Create virutal network and podsubnet and nodesubnet for both member clusters.
export VNET=fleet
export MEMBER_1_NODE_SUBNET=member-1-node
export MEMBER_1_POD_SUBNET=member-1-pod
export MEMBER_2_NODE_SUBNET=member-2-node
export MEMBER_2_POD_SUBNET=member-2-pod
az network vnet create -g $RESOURCE_GROUP --location $LOCATION --name $VNET --address-prefixes 10.0.0.0/8 -o none
az network vnet subnet create -g $RESOURCE_GROUP --vnet-name $VNET --name $MEMBER_1_NODE_SUBNET --address-prefixes 10.242.0.0/16 -o none
az network vnet subnet create -g $RESOURCE_GROUP --vnet-name $VNET --name $MEMBER_1_POD_SUBNET --address-prefixes 10.243.0.0/16 -o none
az network vnet subnet create -g $RESOURCE_GROUP --vnet-name $VNET --name $MEMBER_2_NODE_SUBNET --address-prefixes 10.244.0.0/16 -o none
az network vnet subnet create -g $RESOURCE_GROUP --vnet-name $VNET --name $MEMBER_2_POD_SUBNET --address-prefixes 10.245.0.0/16 -o none

# Create aks member cluster1
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_1 \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_1_NODE_SUBNET" \
    --pod-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_1_POD_SUBNET" \
    --no-wait

# Create aks member cluster2
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_2 \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_2_NODE_SUBNET" \
    --pod-subnet-id "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_2_POD_SUBNET" \
    --no-wait
