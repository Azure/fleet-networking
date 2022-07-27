# Azure Fleet Member Net Controller Manager Helm Chart

## Install CRD

```bash
# Go to root folder of fleet-networking repo
cd <REPO_DIRECTORY>/fleet-networking
kubectl apply -f config/crd/*
```

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
| secret.namespace | The namespace of Kuberentes Secret storing credential to hub cluster, required when config.provider is `secret` | `[]` |
| config.provider | Auth token provider to request hub cluster, can be either `azure` or `secret` | `secret` |
| config.hubURL | Hub cluster endpoint in format https://<hub_cluster_api_server_ip>:<hub_cluster_port>  | `""` |
| config.memberClusterName | Unique identifier of the member cluster  | `""` |
| config.hubCA | Trusted root certificates for insecure requests to hub cluster| `""` |
| podAnnotations | Pod Annotations | `{}` |
| affinity | The node affinity to use for pod scheduling | `{}` |
| tolerations | The toleration to use for pod scheduling | `[]` |

config:
  provider: secret
  hubURL : 
  memberClusterName: membercluster-sample
  hubCA: <certificate-authority-data>
## Contributing Changes
