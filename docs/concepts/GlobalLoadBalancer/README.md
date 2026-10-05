# Multi-cluster Anycast-based Global Load Balancing

## Overview

Fleet-networking provides an automated way to expose multi-cluster applications via the [Azure Global Load Balancer](https://learn.microsoft.com/en-us/azure/load-balancer/cross-region-overview).  Global Load Balancers use a single anycast address to represent a service in one or more member clusters.  Requests to the anycast address will be directed to the nearest participating region, and from there, to the nearest member cluster exposing the service in question.

`MulticlusterLoadBalancer` (mclb) is a custom resource representing an Azure Global Load Balancer.  When combined with `ServiceExport` and Fleet's `ClusterResourcePlacement`, `MulticlusterLoadbalancer` represents an end-to-end declarative solution for exposing services of type LoadBalancer across multiple clusters and regions as a single application.

## Prerequisites

1. Several member clusters in a fleet, with a fleet hub
1. A `Service` defined in the hub (deployment and other resources may optionally be defined in the hub as well), with type `LoadBalancer`
1. A `ServiceExport`, with the same name as the Service above, defined in the hub
1. A `ClusterResourcePlacement` placing the `Service` and `ServiceExport` on one or more member clusters

## Object Model

The user will create a `MulticlusterLoadBalancer`, which shares the name and namespace as the `ServiceExport`, and has no other special properties.  
This will cause a Global Load Balancer to be created, with one backend for each of the member clusters having the `ServiceExport`.  
The Public IP Address of the Global LoadBalancer will be written to the Status field of the `MulticlusterLoadBalancer`, and will be added to the `Service` object as an annotation.
Behind the scenes, Fleet-Networking uses the `InternalServiceExport` objects, which each represent the public ip of an exported service in a particular member cluster to creat the Global Loadbalancer Backends.

## Example

[example.yaml](./example.yaml) contains namespace 'mclb-demo', a deployment and LoadBalancer service called 'helloworld', and a ServiceExport and ClusterResourcePlacement to distribute them to all member clusters. 
For more information on these resources see [Exporting A Service](../ExportingService/README.md) and [ClusterResourcePlacement](TODO).  
Create these prerequisite resources on your hub cluster by downloading the file and running `kubectl apply -f example.yaml`.

Next, create a MulticlusterLoadBalancer named 'helloworld' like so:
```
cat <<EOF | kubectl apply -f -
apiVersion: networking.fleet.azure.com/v1alpha1
kind: MultiClusterLoadBalancer
metadata:
  name: helloworld
  namespace: mclb-demo
EOF
```

To check on the progress, you can run `kubectl get mclbs -n mclb-demo`.  Within a few minutes, you should see that the mclb is valid, is deployed, and has a new External IP.

## Troubleshooting

If the mclb does not reach the deployed state, you can see error messages by running `kubectl describe mclb -n mclb-demo`.  You can also check that your ServiceExport is functioning correctly by running `kubectl get InternalServiceExports -A`, and seeing one InternalServiceExport per member cluster.


## Constraints

The exported `Service` must be exposed via an Azure public ip address.  Private addresses are not yet supported.

## Authentication and Authorization

TBD

## Further Reading

For the controller's internal design (krt-based reconciliation, Azure
Deployment Stack usage, status conditions, known issues), see
[architecture.md](./architecture.md).
