# Azure Fleet Member Net Controller Manager Helm Chart

## Hub cluster setup

Make sure hub cluster has [managed Azure AD integration and Azure RBAC enabled.](https://docs.microsoft.com/en-us/azure/aks/manage-azure-rbac#create-a-new-cluster-using-azure-rbac-and-managed-azure-ad-integration)

### setup rbac in hub cluster

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
| enablePrivateLinkOriginDiscovery | Discover ILB/PLS origins for Zero-Public-Exposure Ingress independently of Traffic Manager. | `false` |
| enableNetworkingFeatures | Set to true to enable Networking Controllers on member cluster. | `true` |
| azureCloudConfig | The Azure cloud provider configuration | **required if Traffic Manager or private-origin discovery is enabled** |

## Override Azure cloud config

**If Traffic Manager or private-origin discovery is enabled, an Azure cloud configuration is required.** It is embedded into a Kubernetes secret and mounted to the member controller. Set the top-level `azureCloudConfig` values or supply them in a separate values file. Private-origin discovery reads Azure resources; it does not create or approve them.

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

## Private-origin discovery for Zero-Public-Exposure Ingress

Install the updated hub InternalServiceExport CRD before enabling new members;
older hub schemas would prune the new optional `spec.privateLinkOrigin` metadata.
The public ServiceImport CRD is unchanged. Older members omit private metadata
and remain compatible with baseline networking, but their origins cannot yet
resolve for private ingress.

Enable discovery independently of Traffic Manager:

```bash
helm upgrade member-net-controller-manager ./charts/member-net-controller-manager/ \
    --reuse-values \
    --set enableTrafficManagerFeature=false \
    --set enablePrivateLinkOriginDiscovery=true \
    -f azure.yaml
```

The binary flag is `--enable-private-link-origin-discovery=true`. Set
`azureCloudConfig.resourceGroup` to the AKS node resource group containing the
internal load balancer and, by default, the PLS. The Service's
`azure-load-balancer-resource-group` and `azure-pls-resource-group` annotations
can identify different resource groups; the discovered PLS must reference the
verified internal frontend, not merely have a matching name.

Grant the member controller identity these Azure read operations at the required
resource-group scopes:

- `Microsoft.Network/loadBalancers/read`
- `Microsoft.Network/privateLinkServices/read`

No Azure write or private endpoint approval permission is required for discovery.
Kubernetes permissions are the existing Service read/watch and reserved member
namespace InternalServiceExport write permissions. Customers do not edit generated
hub ServiceImports.

Discovery currently supports a single IPv4 load-balancer ingress address. It
checks the frontend's private subnet association and absence of a public IP,
then the matching PLS's identity, location and provisioning state. Unknown,
missing, public, ambiguous or unprovisioned origins are published as not ready,
without invalidating baseline MCS export.

Each enabled export is rediscovered every two minutes plus up to 20% jitter;
Service changes still enqueue immediately. Azure discovery calls have a shared
30-second context deadline and use the existing retry/rate-limit policies.
Errors are logged and returned for controller retries after publishing a
not-ready discovery result. Disabling discovery clears private metadata on
subsequent export reconciliation.

| Discovery reason | Operator action |
|---|---|
| `NotInternalLoadBalancer`, `UnsupportedIngress` | Use an internal LoadBalancer Service with one IPv4 ingress address. |
| `WaitingForLoadBalancer`, `LoadBalancerNotFound`, `LoadBalancerNotReady` | Check AKS LB provisioning, resource-group selection and the Service frontend. |
| `PrivateLinkServiceNotFound`, `PrivateLinkServiceNotReady` | Check PLS creation and its frontend association/provisioning. |
| `AmbiguousLoadBalancer`, `AmbiguousPrivateLinkService` | Resolve duplicate/ambiguous associations; discovery will not guess. |
| `InvalidPrivateLinkService` | Check the PLS resource ID, resource group and Azure location. |
| `AzureDiscoveryFailed` | Check member controller logs, identity permissions, Azure throttling and configuration. |

`spec.privateLinkOrigin.ready=true` means only that member ILB/PLS discovery is
ready. It does not mean AFD has been provisioned or an AFD private endpoint
connection has been approved. **AFD approval remains asynchronous and separately
owned.** Gateway reconciliation and traffic programming are later implementation
phases.

## Contributing Changes
