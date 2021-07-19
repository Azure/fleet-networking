# Multi Cluster Networking (MCN) Operator for Azure

A multi-cluster networking operator for Kubernetes on Azure. The project is still under **DRAFT** status, and please do not use it in production.

## How to deploy the operator

### Pre-requisites

- All member clusters should support service annotation `service.beta.kubernetes.io/azure-additional-public-ips` (requires out-of-tree cloud provider Azure v0.7.5+, v1.0.2+ or above).

### Setup secrets

Create Azure service principal, assign Contributor role to LoadBalancer and Public IP Address for all member clusters, and then create the the following cloud-config file:

```json
{
  "cloud": "AzurePublicCloud",
  "tenantId": "<tenantId>",
  "subscriptionId": "<subscriptionId>",
  "aadClientId": "<aadClientId>",
  "aadClientSecret": "<aadClientSecret>",
  "globalLoadBalancerName": "<glbName>",
  "globalVIPLocation": "<region>",
  "globalLoadBalancerResourceGroup": "<resourceGroup>"
}
```

Then create a secret based on this config file:

```sh
kubectl create secret generic azure-mcn-config --from-file=cloud-config --namespace mcn-system
```

### Deploy the operator

After that, build the image and deploy the MCN operator in MCN cluster (it could be any Kubernetes cluster):

```sh
IMG=<your-image-registry/image-name> make docker-build docker-push
IMG=<your-image-registry/image-name> make deploy
```

## Cluster Management

Create ClusterSet and AKSCluster from kubeconfig:

```sh
kubectl create secret generic aks-cluster --from-file=kubeconfig

# create AKSCluster
cat <<EOF | kubectl apply -f -
apiVersion: networking.aks.io/v1alpha1
kind: AKSCluster
metadata:
  name: aks-cluster
  namespace: default
spec:
  kubeConfigSecret: aks-cluster
EOF

# create ClusterSet
cat <<EOF | kubectl apply -f -
apiVersion: networking.aks.io/v1alpha1
kind: ClusterSet
metadata:
  name: clusterset-sample
spec:
  clusters: ["aks-cluster"]
EOF
```

## GlobalService

Create a GlobalService:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: networking.aks.io/v1alpha1
kind: GlobalService
metadata:
  name: nginx
  namespace: default
spec:
  clusterSet: clusterset-sample
  ports:
  - name: http
    port: 80
    targetPort: 80
    protocol: TCP
EOF
```

Then deploy nginx service in member clusters (the MCN operator assumes the service names and namespaces are same by default in all member clusters):

```sh
kubectx aks-cluster
kubectl create deployment nginx --image nginx --save-config
kubectl expose deploy nginx --port=80 --type=LoadBalancer
kubectl get service nginx
```

Switch kubeconfig back to MCN cluster and then verify the VIP for the global service:

```sh
$ kubectl get globalservice
NAME    PORTS   VIP            STATE
nginx   80      x.x.x.x        ACTIVE
```

## Contributing

This project welcomes contributions and suggestions.  Most contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit https://cla.opensource.microsoft.com.

When you submit a pull request, a CLA bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., status check, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.
