# Azure Fleet MultiClusterService Controller Manager Helm Chart

## Install CRD in member cluster

```bash
# Go to root folder of fleet-networking repo
cd <REPO_DIRECTORY>/fleet-networking
kubectl apply -f config/crd/*
```

## Install Chart in member cluster

```bash
# Helm install under root directory of fleet-networking repo
helm install mcs-controller-manager ./charts/mcs-controller-manager/
```

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Upgrade Chart in member cluster

```bash
# Helm upgrade under root directory of fleet-networking repo
helm upgrade mcs-controller-manager ./charts/mcs-controller-manager/
```

## Parameters

| Parameter | Description | Default |
|:-|:-|:-|
| replicaCount | The number of mcs-controller-manager replicas to deploy | `1` |
| image.repository | Image repository | `ghcr.io/azure/fleet-networking/mcs-controller-manager` |
| image.pullPolicy | Image pullPolicy | `IfNotPresent` |
| image.tag | The image tag to use | `v0.1.0` |
| logVerbosity | Log level. Uses V logs (klog) | `2` |
| fleetSystemNamespace | Namespace that this Helm chart is installed on and reserved by fleet. | `fleet-system` |
| leaderElectionNamespace | The namespace in which the leader election resource will be created. | `fleet-system` |
| azure.clientid | Azure AAD client ID to obtain token to request hub cluster, required when config.provider is `azure` | `[]` |
| secret.name | The name of Kuberentes Secret storing credential to hub cluster, required when config.provider is `secret` | `[]` |
| secret.namespace | The namespace of Kuberentes Secret storing credential to hub cluster, required when config.provider is `secret` | `[]` |
| config.provider | Auth token provider to request hub cluster, can be either `azure` or `secret` | `secret` |
| config.hubURL | Hub cluster endpoint in format `https://<hub_cluster_api_server_ip>:<hub_cluster_port` | `""` |
| config.memberClusterName | Unique identifier of the member cluster  | `""` |
| config.hubCA | Trusted root certificates for insecure requests to hub cluster| `""` |
| resources | The resource request/limits for the container image | limits: 500m CPU, 1Gi, requests: 100m CPU, 128Mi |
| podAnnotations | Pod Annotations | `{}` |
| affinity | The node affinity to use for pod scheduling | `{}` |
| tolerations | The toleration to use for pod scheduling | `[]` |
| enableNetworkingFeatures | Set to true to enable Networking Controllers on member cluster. | `true`|

## Contributing Changes