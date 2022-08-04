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
| resources | The resource request/limits for the container image | limits: 500m CPU, 1Gi, requests: 100m CPU, 128Mi |
| azure.clientid | Azure AAD client ID to obtain token to request hub cluster, required when config.provider is `azure` | `[]` |
| secret.name | The name of Kuberentes Secret storing credential to hub cluster, required when config.provider is `secret` | `[]` |
| secret.namespace | The namespace of Kuberentes Secret storing credential to hub cluster, required when config.provider is `secret` | `default` |
| config.provider | Auth token provider to request hub cluster, can be either `azure` or `secret` | `secret` |
| config.hubURL | Hub cluster endpoint in format `https://<hub_cluster_api_server_ip>:<hub_cluster_port` | `""` |
| config.memberClusterName | Unique identifier of the member cluster  | `""` |
| config.hubCA | Trusted root certificates for insecure requests to hub cluster| `""` |
| podAnnotations | Pod Annotations | `{}` |
| affinity | The node affinity to use for pod scheduling | `{}` |
| tolerations | The toleration to use for pod scheduling | `[]` |

## Contributing Changes
