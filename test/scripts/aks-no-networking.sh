#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail
set -x
source "$(dirname "${BASH_SOURCE[0]}")/e2e-config.sh"

# aks-no-networking creates two member clusters

# Member cluster setup steps:
# 1. Create member cluster 1
# 2. Create member cluster 2

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
    --no-wait "${E2E_RESOURCE_TAGS[@]}"
