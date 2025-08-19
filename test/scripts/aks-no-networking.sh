#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x

# aks-no-networking creates two member clusters

# Member cluster setup steps:
# 1. Create member cluster 1
# 2. Create member cluster 2

# Create aks member cluster1, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.

az aks create \
    --location $MEMBER_1_LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_1 \
    --node-count $NODE_COUNT \
    --node-vm-size Standard_A2_v2 \
    --generate-ssh-keys \
    --no-wait


# Create aks member cluster2, specifying amd64 VM size to ensure linux/amd64 docker images can be consumed.
  az aks create \
    --location $MEMBER_2_LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_2 \
    --node-count $NODE_COUNT \
    --node-vm-size Standard_A2_v2 \
    --generate-ssh-keys \
    --no-wait
