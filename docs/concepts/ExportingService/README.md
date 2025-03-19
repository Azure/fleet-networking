# Exporting Services

## Overview
Services will not be visible to other clusters in the fleet by default. They must be explicitly marked for export by the user. This allows users to decide exactly which services should be visible outside of the local cluster.

ServiceExport is a custom resource definition (CRD) that allows you to export a service to the fleet. 

To mark a service for export to the fleet, a user will create a `ServiceExport` CR:

```yaml
apiVersion: networking.fleet.azure.com/v1alpha1
kind: ServiceExport
metadata:
  name: nginx-service
  namespace: test-app
```

To export a service, a `ServiceExport` should be created within the cluster and namespace that the service resides in, name-mapped to the service for export - that is, they reference the `Service` with the same name as the export. 
If multiple clusters within the fleet have ServiceExports with the same name and namespace, these will be considered the same service and will be combined at the fleet level.

This requires that within a fleet, a given namespace is governed by a single authority across all clusters. 
It is that authority’s responsibility to ensure that a name is shared by multiple services within the namespace if and only if they are instances of the same service.

Deleting a `ServiceExport` will stop exporting the name-mapped `Service`.

The `ServiceExport` itself can be propagated from the fleet cluster to a member cluster using the fleet resource propagation feature,
or it can be created directly on the member cluster. Once this `ServiceExport` resource is created, it results in a `ServiceImport` 
being created on the fleet cluster, and all other member clusters to build the awareness of the service.

## User stories
**Single Service Deployed to Multiple Clusters**

I have deployed my service to multiple clusters for redundancy or scale. Requests to my replicated service should 

seamlessly transition (within SLO for dropped requests) between instances of my service in case of failure or removal without action 
by or impact on the caller. Routing to my replicated service should optimize for cost metric (e.g. prioritize traffic local to zone, region).

## Constraints and Conflict Resolution
If the service falls into one of these situations, the serviceExport will be marked as "Valid" as false.
* `Service` does not exist.
* `Service` is ExternalName type.
* `Service` is headless type.

Exported services are derived from the properties of each component service. However, if the service specification is
different from others, the serviceExport will be marked as "Conflict" as true.

A valid and no-conflict serviceExport sample:

```yaml
status:
    conditions:
    - lastTransitionTime: "2025-03-19T08:40:17Z"
      message: service my-ns-ftbmj/hello-world-service is valid for export
      reason: ServiceIsValid
      status: "True"
      type: Valid
    - lastTransitionTime: "2025-03-19T08:40:17Z"
      message: service my-ns-ftbmj/hello-world-service is exported without conflict
      reason: NoConflictFound
      status: "False"
      type: Conflict
```