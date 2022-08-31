if [ -z ${SUBSCRIPTION_ID+x} ]; then
    echo "Subscription ID is not set; to use the script, set the SUBSCRIPTION_ID variable."
    exit 1
fi

if [ -z ${RESOURCE_GROUP+x} ]; then
    echo "Resource group is not set; to use the script, set the RESOURCE_GROUP variable."
    exit 1
fi

if [ -z ${LOCATION+x} ]; then
    echo "Location is not set; to use the script, set the LOCATION variable."
    exit 1
fi

if [ -z ${REGISTRY+x} ]; then
    echo "Registry is not set; to use the script, set the REGISTRY variable."
    exit 1
fi

if [[ $REGISTRY == *.azurecr.io ]]; then
    echo "Registry is set with the full URL; to use the script, update the REGISTRY variable to use the registry name only."
    exit 1
fi

echo "Creating the resource group..."
az group create --name $RESOURCE_GROUP --location $LOCATION

echo "Creating the virtual network..."
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

echo "Creating the container registry..."
az acr create -g $RESOURCE_GROUP -n $REGISTRY --sku basic
sleep 10
az acr login -n $REGISTRY

echo "Creating the hub cluster..."
export NODE_COUNT=2
export HUB_CLUSTER=hub
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $HUB_CLUSTER \
    --attach-acr $REGISTRY \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$HUB_SUBNET" \
    --yes

echo "Creating the first member cluster..."
export MEMBER_CLUSTER_1=member-1
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_1 \
    --attach-acr $REGISTRY \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_1_SUBNET" \
    --yes

echo "Creating the second member cluster..."
export MEMBER_CLUSTER_2=member-2
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_2 \
    --attach-acr $REGISTRY \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_2_SUBNET" \
    --yes

echo "Retrieving credentials for the Kubernetes clusters..."
az aks get-credentials --name $HUB_CLUSTER -g $RESOURCE_GROUP --admin
az aks get-credentials --name $MEMBER_CLUSTER_1 -g $RESOURCE_GROUP --admin
az aks get-credentials --name $MEMBER_CLUSTER_2 -g $RESOURCE_GROUP --admin
