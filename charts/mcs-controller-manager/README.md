# Azure Fleet MultiClusterService Controller Manager Helm Chart

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
| crdInit.enabled | Enable automatic CRD installation via init container | `true` |
| crdInit.forceUpdate | Force update existing CRDs | `false` |
| crdInit.image.repository | Repository for the kubectl image used to apply CRDs | `bitnami/kubectl` |
| crdInit.image.tag | Tag for the kubectl image | `latest` |
| crdInit.image.pullPolicy | Pull policy for the kubectl image | `IfNotPresent` |
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

## Contributing Changes