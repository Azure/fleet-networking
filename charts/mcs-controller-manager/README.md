# Azure Fleet MultiClusterService Controller Manager Helm Chart

## Install CRD

```bash
# Go to root folder of fleet-networking repo
cd <REPO_DIRECTORY>/fleet-networking
kubectl apply -f config/crd/*
```

## Install Chart

```bash
# Helm install under root directory of fleet-networking repo
helm install mcs-controller-manager ./charts/mcs-controller-manager/
```

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Upgrade Chart

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
| fleetSystemNamespace | Namespace that this Helm chart is installed on and reserved by fleet. | `fleet-system` |
| logVerbosity | Log level. Uses V logs (klog) | `1` |
| affinity | The node affinity to use for pod scheduling | `{}` |
| tolerations | The toleration to use for pod scheduling | `[]` |
| resources | The resource request/limits for the container image   | limits: "2" CPU, 4Gi, requests: 100m CPU, 128Mi |

## Contributing Changes
