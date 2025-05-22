# Azure Fleet Member Net Controller Manager Helm Chart

## Hub cluster setup

Make sure hub cluster has [managed Azure AD integration and Azure RBAC enabled.](https://docs.microsoft.com/en-us/azure/aks/manage-azure-rbac#create-a-new-cluster-using-azure-rbac-and-managed-azure-ad-integration)

### setup rbac in hub cluster

## Install CRD in member cluster

The CRDs can be installed in two ways:

1. Manually before installing the Helm chart:

```bash
# Go to root folder of fleet-networking repo
cd <REPO_DIRECTORY>/fleet-networking
kubectl apply -f config/crd/*
```

2. Automatically during Helm chart installation using the init container (enabled by default):

The chart includes an init container that can automatically install or update the CRDs required by the controller.
This feature is enabled by default and can be configured in the values.yaml file.

## Install Chart

```bash
# Helm install under root directory of fleet-networking repo
helm install member-net-controller-manager ./charts/member-net-controller-manager/
```

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Upgrade Chart

```bash
# Helm upgrade under root directory of fleet-networking repo
helm upgrade member-net-controller-manager ./charts/member-net-controller-manager/
```

```bash
MEMBER_CLUSTER_NAME=membercluster-sample
SERVICE_PRINCIPAL_ID=<principle_id_of_member_cluster_agentpool_managed_identity>

# create namespace
kubectl create ns fleet-member-$MEMBER_CLUSTER_NAME

# create rbac in hub cluster for member-net-controller-manager to
# access hub cluster resources
cat <<EOF | kubectl apply --filename -
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: fleet-role-$MEMBER_CLUSTER_NAME
  namespace: fleet-member-$MEMBER_CLUSTER_NAME
rules:
- apiGroups:
  - coordination.k8s.io
  resources:
  - leases
  verbs:
  - create
  - get
  - list
  - update
- apiGroups:
  - ""
  resources:
  - events
  verbs:
  - create
  - get
  - list
  - update
  - watch
  - patch
- apiGroups: 
  - networking.fleet.azure.com
  resources: ["*"]
  verbs: ["*"]
---
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: fleet-rolebinding-$MEMBER_CLUSTER_NAME
  namespace: fleet-member-$MEMBER_CLUSTER_NAME
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: fleet-role-$MEMBER_CLUSTER_NAME
subjects:
  - kind: User
    name: $SERVICE_PRINCIPAL_ID
EOF
```

## Install CRD in member cluster

```bash
# Go to root folder of fleet-networking repo
cd <REPO_DIRECTORY>/fleet-networking
kubectl apply -f config/crd/*
```

## Install Chart

```bash
HUB_CLUSTER_ENDPOINT=<Hub Cluster endpoint>
CLIENT_ID=<client_id_of_member_cluster_agentpool_managed_identity>
# Helm install under root directory of fleet-networking repo
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
    --set config.hubURL=$HUB_CLUSTER_ENDPOINT \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_NAME \
    --set azure.clientid=$CLIENT_ID
```

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Upgrade Chart

```bash
# Helm upgrade under root directory of fleet-networking repo
helm upgrade member-net-controller-manager ./charts/member-net-controller-manager/
```

## Parameters

| Parameter | Description | Default |
|:-|:-|:-|
| replicaCount | The number of member-net-controller-manager replicas to deploy | `1` |
| image.repository | Image repository | `ghcr.io/azure/fleet-networking/member-net-controller-manager` |
| image.pullPolicy | Image pullPolicy | `IfNotPresent` |
| image.tag | The image tag to use | `v0.1.0` |
| crdInit.enabled | Enable automatic CRD installation via init container | `true` |
| crdInit.forceUpdate | Force update existing CRDs | `false` |
| crdInit.image.repository | Repository for the kubectl image used to apply CRDs | `bitnami/kubectl` |
| crdInit.image.tag | Tag for the kubectl image | `latest` |
| crdInit.image.pullPolicy | Pull policy for the kubectl image | `IfNotPresent` |
| logVerbosity | Log level. Uses V logs (klog) | `2` |
| fleetSystemNamespace | Namespace that this Helm chart is installed on and reserved by fleet. | `fleet-system` |
| leaderElectionNamespace | The namespace in which the leader election resource will be created. | `fleet-system` |
| resources | The resource request/limits for the container image | limits: 500m CPU, 1Gi, requests: 100m CPU, 128Mi |
| azure.clientid | Azure AAD client ID to obtain token to request hub cluster, required when config.provider is `azure` | `[]` |
| secret.name | The name of Kuberentes Secret storing credential to hub cluster, required when config.provider is `secret` | `[]` |
| secret.namespace | The namespace of Kuberentes Secret storing credential to hub cluster, required when config.provider is `secret` | `[]` |
| config.provider | Auth token provider to request hub cluster, can be either `azure` or `secret` | `secret` |
| config.hubURL | Hub cluster endpoint in format `https://<hub_cluster_api_server_ip>:<hub_cluster_port` | `""` |
| config.memberClusterName | Unique identifier of the member cluster  | `""` |
| config.hubCA | Trusted root certificates for insecure requests to hub cluster| `""` |
| podAnnotations | Pod Annotations | `{}` |
| affinity | The node affinity to use for pod scheduling | `{}` |
| tolerations | The toleration to use for pod scheduling | `[]` |
| enableTrafficManagerFeature | Set to true to enable the Azure Traffic Manager feature. | `false` |
| azureCloudConfig | The Azure cloud provider configuration | **required if AzureTrafficManager feature is enabled (enableTrafficManagerFeature == true)** |

## Override Azure cloud config

**If AzureTrafficManager feature is enabled, then an Azure cloud configuration is required.** Azure cloud configuration provides resource metadata and credentials for `fleet-hub-net-controller-manager` and `fleet-member-net-controller-manager` to manipulate Azure resources. It's embedded into a Kubernetes secret and mounted to the pods. The values can be modified under `config.azureCloudConfig` section in values.yaml or can be provided as a separate file.

| configuration value                                   | description | Remark                                                                               |
|-------------------------------------------------------| --- |--------------------------------------------------------------------------------------|
| `cloud`                       | The cloud where Azure resources belong. Choose from `AzurePublicCloud`, `AzureChinaCloud`, and `AzureGovernmentCloud`. | Required, helm chart defaults to `AzurePublicCloud`                                  |
| `tenantId`                    | The AAD Tenant ID for the subscription where the Azure resources are deployed. |                                                                                      |
| `subscriptionId`              | The ID of the subscription where Azure resources are deployed. |                                                                                      |
| `useManagedIdentityExtension` | Boolean indicating whether or not to use a managed identity. | `true` or `false`                                                                    |
| `userAssignedIdentityID`      | ClientID of the user-assigned managed identity with RBAC access to Azure resources. | Required for UserAssignedIdentity and ommited for SystemAssignedIdentity. |
| `aadClientId`                 | The ClientID for an AAD application with RBAC access to Azure resources. | Required if `useManagedIdentityExtension` is set to `false`.                         |
| `aadClientSecret`             | The ClientSecret for an AAD application with RBAC access to Azure resources. | Required if `useManagedIdentityExtension` is set to `false`.                         |
| `resourceGroup`               | The name of the resource group where cluster resources are deployed. |                                                                                      |
| `userAgent`                   | The userAgent provided to Azure when accessing Azure resources. | |
| `location`                    | The azure region where resource group and its resources is deployed. |  |

You can create a file `azure.yaml` with the following content, and pass it to `helm install` command: `helm install <release-name> <chart-name> --set enableTrafficManagerFeature=true -f azure.yaml`

```yaml
azureCloudConfig:
  cloud: "AzurePublicCloud"
  tenantId: "00000000-0000-0000-0000-000000000000"
  subscriptionId: "00000000-0000-0000-0000-000000000000"
  useManagedIdentityExtension: false
  userAssignedIdentityID: "00000000-0000-0000-0000-000000000000"
  aadClientId: "00000000-0000-0000-0000-000000000000"
  aadClientSecret: "<your secret>"
  userAgent: "fleet-member-net-controller"
  resourceGroup: "<resource group name>"
  location: "<resource group location>"
```

## Contributing Changes
