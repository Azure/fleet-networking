# How-to Guide: Traffic Manager Permissions setup

This guide provides an overview of how to set up permissions for Azure Traffic Manager in order to use the DNS based global
load balancing feature.

## Get the hub and member agents identity

Figure out the identity used by hub-net-controller-manager and member-net-controller-manager.
There are various ways to set up the fleet networking, and the recommended way is to use [workload-identity](https://learn.microsoft.com/en-us/azure/aks/workload-identity-deploy-cluster).

```bash
export HUB_IDENTITY_PRINCIPAL_ID=$(az identity show \
    --name "${HUB_USER_ASSIGNED_IDENTITY_NAME}" \
    --resource-group "${RESOURCE_GROUP}" \
    --query principalId \
    --output tsv)
export MEMBER_IDENTITY_PRINCIPAL_ID=$(az identity show \
    --name "${MEMBER_USER_ASSIGNED_IDENTITY_NAME}" \
    --resource-group "${RESOURCE_GROUP}" \
    --query principalId \
    --output tsv)
```

## Create the role assignment for the hub agent

### Create the role assignment for the hub agent to manage the Azure Traffic Manager
Assign role “[Traffic Manager Contributor](https://learn.microsoft.com/en-us/azure/role-based-access-control/built-in-roles/networking#traffic-manager-contributor)” to hub cluster identity at the Azure Traffic Manager resource group scope
```bash
az role assignment create --assignee "${HUB_IDENTITY_PRINCIPAL_ID}" --role "a4b10055-b0c7-44c2-b00f-c7b5b3550cf7" --scope "/subscriptions/mySubscriptions/resourceGroups/MyAzureTrafficManagerResourceGroup"
```

### Create the role assignment for the hub agent to read the public IP address used by the member cluster

Grant Public IP address read permission to the hub cluster identity so that the hub networking agent can read the public IP address of the members (either BYO or MC_rg). 

> Note: You can create your own customized role to restrict access or restrict the scope based on your security requirements.

For example, the following command grants the “[Reader](https://learn.microsoft.com/en-us/azure/role-based-access-control/built-in-roles/general#reader)” to the hub cluster identity at the resource group of the public ip scope for testing purpose.

```bash
az role assignment create --assignee "${HUB_IDENTITY_PRINCIPAL_ID}" --role "acdd72a7-3385-48ef-bd42-f606fba81ae7" --scope "/subscriptions/mySubscriptions/resourceGroups/MyPIPResourceGroup"
```

## Create the role assignment for the member agent
Grant Public IP address read permission to the member cluster identity so that the member networking agent can read the public IP address of the members (either BYO or MC_rg).

> Note: You can create your own customized role to restrict access or restrict the scope based on your security requirements.

For example, the following command grants the “[Reader](https://learn.microsoft.com/en-us/azure/role-based-access-control/built-in-roles/general#reader)” to the member cluster identity at the resource group of the public ip scope for testing purpose.

```bash
az role assignment create --assignee "${MEMBER_IDENTITY_PRINCIPAL_ID}" --role "acdd72a7-3385-48ef-bd42-f606fba81ae7" --scope "/subscriptions/mySubscriptions/resourceGroups/MyPIPResourceGroup"
```
