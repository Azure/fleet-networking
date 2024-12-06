# Tutorial: Migrating Application Resources to Clusters without Downtime

This tutorial will guide you through the process of migrating application resources to a new cluster without any downtime using [KubeFleet](https://github.com/Azure/fleet) and networking related features. 
This is useful when you need to migrate resources to a new cluster for any reason, such as upgrading the cluster version or moving to a different region.

## Scenario
Your fleet consists of the following clusters:

1. Member Cluster 1 with label "cluster-name: member-1"

You have a set of application resources running on Member Cluster 1 that you want to migrate to Member Cluster 2.
Meanwhile, you want to ensure that the application remains available and accessible during the migration process.

## Current Application Resources
![](before-migrate.png)

The following resources are currently deployed in the hub cluster and use clusterResourcePlacement API to place them to the Member Cluster 1:

### Service
> Note: Service test file located [here](./testfiles/nginx-service.yaml).

```yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-service
  namespace: test-app
spec:
  selector:
    app: nginx
  ports:
    - protocol: TCP
      port: 80
      targetPort: 80
  type: LoadBalancer
---
apiVersion: networking.fleet.azure.com/v1alpha1
kind: ServiceExport
metadata:
  name: nginx-service
  namespace: test-app
```
The service is exposed using public IP and assigned a DNS name using [ro-nginx-service.yaml](./testfiles/ro-nginx-service.yaml)
and is visible to the fleet by creating ServiceExport.

```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  name: ro-nginx-service
  namespace: test-app
spec:
  resourceSelectors:
    -  group: ""
       kind: Service
       version: v1
       name: nginx-service
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  cluster-name: member-1
        jsonPatchOverrides:
          - op: add
            path: /metadata/annotations
            value:
              {"service.beta.kubernetes.io/azure-dns-label-name":"fleet-test-member-1"}
```
Summary:
- This defines a Kubernetes Service named `nginx-service` in the `test-app` namespace.
- The service is of type LoadBalancer with a public ip address and a DNS name assigned.
- It targets pods with the label app: nginx and forwards traffic to port 80 on the pods.

#### Deployment

> Note: Deployment test file located [here](./testfiles/envelop-object.yaml).

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: envelope-configmap
  namespace: test-app
  annotations:
    kubernetes-fleet.io/envelope-configmap: "true"
data:
  nginx-deployment.yaml: |
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: nginx-deployment
      namespace: test-app
    spec:
      replicas: 2
      selector:
        matchLabels:
          app: nginx
      template:
        metadata:
          labels:
            app: nginx
        spec:
          containers:
            - name: nginx
              image: nginx:1.16.1
              ports:
               - containerPort: 80
```
> Note: The current deployment has 2 replicas.

Summary:
- This defines a Kubernetes Deployment named `nginx-deployment` in the `test-app` namespace using envelop object wrapper, so that
it won't create workloads in the hub cluster.
- It creates 2 replicas of the nginx pod, each running the `nginx:1.16.1` image.
- The deployment ensures that the specified number of pods (replicas) are running and available.
- The pods are labeled with `app: nginx` and expose port 80.

#### ClusterResourcePlacement

> Note: CRP Availability test file located [here](./testfiles/crp-availability.yaml)

```yaml
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterResourcePlacement
metadata:
  name: crp-availability
spec:
  resourceSelectors:
    - group: ""
      kind: Namespace
      name: test-app
      version: v1
  policy:
    placementType: PickAll
```

Summary:
- This defines a ClusterResourcePlacement named `crp-availability`.
- The placement policy selects all the existing cluster, member-1.
- It targets resources in the `test-app` namespace.

### TrafficManagerProfile

To expose the service via Traffic Manager, you need to create a trafficManagerProfile resource in the `test-app` namespace.

> Note: Profile test file located [here](./testfiles/nginx-profile.yaml) and please make sure the profile name (be part of the DNS name) is global unique.

```yaml
apiVersion: networking.fleet.azure.com/v1alpha1
kind: TrafficManagerProfile
metadata:
  name: nginx-profile
  namespace: test-app
spec:
  monitorConfig:
    port: 80
```
Summary:
- This defines a Traffic Manager Profile named `nginx-profile` in the `test-app` namespace.
- It listens on the specified port (80) for health checks.

```bash
kubectl get tmp -n test-app
NAME            DNS-NAME                                    IS-PROGRAMMED   AGE
nginx-profile   test-app-nginx-profile.trafficmanager.net   True            6s
```

### Exposing the Service as a Traffic Manager Endpoint

> Note:  nginx-backend file located [here](./testfiles/nginx-backend.yaml)

```yaml
apiVersion: networking.fleet.azure.com/v1alpha1
kind: TrafficManagerBackend
metadata:
  name: nginx-backend
  namespace: test-app
spec:
  profile:
    name: "nginx-profile"
  backend:
    name: "nginx-service"
  weight: 100
```
Summary:
- It defines a Traffic Manager Backend named `nginx-backend` in the `test-app` namespace.
- The weight is set to 100, which means all traffic is routed to this backend.

## Migrating Application Resources To Member Cluster 2

![](during-migrate.png)


To migrate the application resources to the new cluster, you need to add the new cluster Member Cluster 2 with label "cluster-name: member-2" 
as part of the fleet by installing fleet agents and creating MemberCluster API ([sample MemberCluster](./testfiles/member-cluster-2.yaml)) following [this document](https://github.com/Azure/fleet/blob/main/docs/howtos/clusters.md).

```bash
kubectl get membercluster -l cluster-name=member-2
NAME       JOINED   AGE   MEMBER-AGENT-LAST-SEEN   NODE-COUNT   AVAILABLE-CPU   AVAILABLE-MEMORY
member-2   True     16h   38s                      2            1848m           10318332Ki
```

### Validate the Placement Status For Member Cluster 2

Before migrating the resources, you need to validate the placement status on Member Cluster 2 to ensure that the resources are placed correctly and available.

```bash
kubectl get crp crp-availability -o yaml
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterResourcePlacement
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"placement.kubernetes-fleet.io/v1","kind":"ClusterResourcePlacement","metadata":{"annotations":{},"name":"crp-availability"},"spec":{"policy":{"affinity":{"clusterAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"clusterSelectorTerms":[{"labelSelector":{"matchLabels":{"cluster-name":"member-1"}}}]}}},"placementType":"PickAll"},"resourceSelectors":[{"group":"","kind":"Namespace","name":"test-app","version":"v1"}]}}
  creationTimestamp: "2024-11-21T08:35:49Z"
  finalizers:
  - kubernetes-fleet.io/crp-cleanup
  - kubernetes-fleet.io/scheduler-cleanup
  generation: 3
  name: crp-availability
  resourceVersion: "11491938"
  uid: df7dc703-6af0-401a-b41b-17e76f8383af
spec:
  policy:
    placementType: PickAll
  resourceSelectors:
  - group: ""
    kind: Namespace
    name: test-app
    version: v1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
status:
  conditions:
  - lastTransitionTime: "2024-12-06T08:55:45Z"
    message: found all cluster needed as specified by the scheduling policy, found
      2 cluster(s)
    observedGeneration: 3
    reason: SchedulingPolicyFulfilled
    status: "True"
    type: ClusterResourcePlacementScheduled
  - lastTransitionTime: "2024-12-06T08:55:46Z"
    message: All 2 cluster(s) start rolling out the latest resource
    observedGeneration: 3
    reason: RolloutStarted
    status: "True"
    type: ClusterResourcePlacementRolloutStarted
  - lastTransitionTime: "2024-12-06T08:55:46Z"
    message: The selected resources are successfully overridden in 2 cluster(s)
    observedGeneration: 3
    reason: OverriddenSucceeded
    status: "True"
    type: ClusterResourcePlacementOverridden
  - lastTransitionTime: "2024-12-06T08:55:46Z"
    message: Works(s) are succcesfully created or updated in 2 target cluster(s)'
      namespaces
    observedGeneration: 3
    reason: WorkSynchronized
    status: "True"
    type: ClusterResourcePlacementWorkSynchronized
  - lastTransitionTime: "2024-12-06T08:55:46Z"
    message: The selected resources are successfully applied to 2 cluster(s)
    observedGeneration: 3
    reason: ApplySucceeded
    status: "True"
    type: ClusterResourcePlacementApplied
  - lastTransitionTime: "2024-12-06T08:56:01Z"
    message: The selected resources in 2 cluster(s) are available now
    observedGeneration: 3
    reason: ResourceAvailable
    status: "True"
    type: ClusterResourcePlacementAvailable
  observedResourceIndex: "11"
  placementStatuses:
  - clusterName: member-2
    conditions:
    - lastTransitionTime: "2024-12-06T08:55:45Z"
      message: 'Successfully scheduled resources for placement in "member-2" (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 3
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-12-06T08:55:46Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 3
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-12-06T08:55:46Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 3
      reason: NoOverrideSpecified
      status: "True"
      type: Overridden
    - lastTransitionTime: "2024-12-06T08:55:46Z"
      message: All of the works are synchronized to the latest
      observedGeneration: 3
      reason: AllWorkSynced
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-12-06T08:55:46Z"
      message: All corresponding work objects are applied
      observedGeneration: 3
      reason: AllWorkHaveBeenApplied
      status: "True"
      type: Applied
    - lastTransitionTime: "2024-12-06T08:56:01Z"
      message: All corresponding work objects are available
      observedGeneration: 3
      reason: AllWorkAreAvailable
      status: "True"
      type: Available
  - applicableResourceOverrides:
    - name: ro-nginx-service-9
      namespace: test-app
    clusterName: member-1
    conditions:
    - lastTransitionTime: "2024-12-06T08:55:45Z"
      message: 'Successfully scheduled resources for placement in "member-1" (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
      observedGeneration: 3
      reason: Scheduled
      status: "True"
      type: Scheduled
    - lastTransitionTime: "2024-12-06T08:55:46Z"
      message: Detected the new changes on the resources and started the rollout process
      observedGeneration: 3
      reason: RolloutStarted
      status: "True"
      type: RolloutStarted
    - lastTransitionTime: "2024-12-06T08:55:46Z"
      message: Successfully applied the override rules on the resources
      observedGeneration: 3
      reason: OverriddenSucceeded
      status: "True"
      type: Overridden
    - lastTransitionTime: "2024-12-06T08:55:46Z"
      message: All of the works are synchronized to the latest
      observedGeneration: 3
      reason: AllWorkSynced
      status: "True"
      type: WorkSynchronized
    - lastTransitionTime: "2024-12-06T08:55:46Z"
      message: All corresponding work objects are applied
      observedGeneration: 3
      reason: AllWorkHaveBeenApplied
      status: "True"
      type: Applied
    - lastTransitionTime: "2024-12-06T08:55:46Z"
      message: All corresponding work objects are available
      observedGeneration: 3
      reason: AllWorkAreAvailable
      status: "True"
      type: Available
  selectedResources:
  - kind: Namespace
    name: test-app
    version: v1
  - group: networking.fleet.azure.com
    kind: ServiceExport
    name: nginx-service
    namespace: test-app
    version: v1alpha1
  - kind: Service
    name: nginx-service
    namespace: test-app
    version: v1
  - kind: ConfigMap
    name: envelope-configmap
    namespace: test-app
    version: v1
```

### Validate the TrafficManagerBackend nginx-backend

Before migrating the resources, you need to validate the TrafficManagerBackend resource `nginx-backend` to ensure that the traffic is being routed to the correct cluster.

```bash
kubectl get tmb nginx-backend -n test-app -o yaml

apiVersion: networking.fleet.azure.com/v1alpha1
kind: TrafficManagerBackend
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"networking.fleet.azure.com/v1alpha1","kind":"TrafficManagerBackend","metadata":{"annotations":{},"name":"nginx-backend","namespace":"test-app"},"spec":{"backend":{"name":"nginx-service"},"profile":{"name":"nginx-profile"},"weight":100}}
  creationTimestamp: "2024-11-21T05:29:59Z"
  finalizers:
  - networking.fleet.azure.com/traffic-manager-backend-cleanup
  generation: 1
  name: nginx-backend
  namespace: test-app
  resourceVersion: "1240027"
  uid: 073a01e7-6f07-49c9-abce-8ce14748984e
spec:
  backend:
    name: nginx-service
  profile:
    name: nginx-profile
  weight: 100
status:
  conditions:
  - lastTransitionTime: "2024-11-21T05:32:28Z"
    message: '1 service(s) exported from clusters cannot be exposed as the Azure Traffic
      Manager, for example, service exported from member-2 is invalid: DNS label is
      not configured to the public IP'
    observedGeneration: 1
    reason: Invalid
    status: "False"
    type: Accepted
  endpoints:
  - cluster:
      cluster: member-1
    name: fleet-073a01e7-6f07-49c9-abce-8ce14748984e#nginx-service#member-1
    target: fleet-test-member-1.westcentralus.cloudapp.azure.com
    weight: 100
```
Summary:
- Since we have not assigned a DNS label for the nginx-service created in the member-2 cluster, the traffic cannot be routed
to the member-2.
- The traffic is currently being routed to the nginx-service in Member Cluster 1 only.

### Exposing The deployment In Member Cluster 2 Using A Different Service Name

You will stop serving the traffic via the existing service `nginx-service` in Member Cluster 2  and the nginx deployment in Member Cluster 2 will
be exposed using a different service name `nginx-service-2` with a different DNS name. All the traffic will be routed via the new Service `nginx-service-2` in Member Cluster 2 instead of `nginx-service`.

#### Stop Exposing nginx-service In Member Cluster 2

Before creating the new service in Member Cluster 2, you need to stop exposing the existing service `nginx-service` in Member Cluster 2 by deleting the ServiceExport resource via creating override.
> Note:  override file located [here,](./testfiles/ro-nginx-service-export.yaml).
```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  name: ro-nginx-service
  namespace: test-app
spec:
  resourceSelectors:
    -  group: networking.fleet.azure.com
       kind: ServiceExport
       version: v1alpha1
       name: nginx-service
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  cluster-name: member-2
        overrideType: Delete

```
Summary:
- The override makes sure serviceExport `nginx-service` won't be applied to the Member Cluster 2.

#### Stop Exposing Deployment via nginx-service Member Cluster 2

To stop exposing the deployment, you need to update ro-nginx-service Override.

> Note:  override file located [here,](./testfiles/ro-nginx-service.yaml).

```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  name: ro-nginx-service
  namespace: test-app
spec:
  resourceSelectors:
    -  group: ""
       kind: Service
       version: v1
       name: nginx-service
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  cluster-name: member-1
        jsonPatchOverrides:
          - op: add
            path: /metadata/annotations
            value:
              {"service.beta.kubernetes.io/azure-dns-label-name":"fleet-test-member-1"}
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  cluster-name: member-2
        overrideType: Delete
```

Summary:
- The override makes sure the nginx-service won't be applied to the Member Cluster 2 cluster.

#### Create ro-nginx-service-export-2 Override

> Note:  override file located [here,](./testfiles/ro-nginx-service-export-2.yaml) and it should be created before the new service.
> So that the overrides can be applied to these resources.

To ensure the new service is exposed in Member Cluster 2 only, you need to create an override to delete the serviceExport in Member Cluster 1 when propagating.
```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  name: ro-nginx-service
  namespace: test-app
spec:
  resourceSelectors:
    -  group: networking.fleet.azure.com
       kind: ServiceExport
       version: v1alpha1
       name: nginx-service-2
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  cluster-name: member-1
        overrideType: Delete
```

#### Create ro-nginx-service-2 Override
> Note:  override file located [here,](./testfiles/ro-nginx-service-2.yaml) and it should be created before the new service.
> So that the overrides can be applied to these resources.

```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  name: ro-nginx-service
  namespace: test-app
spec:
  resourceSelectors:
    -  group: ""
       kind: Service
       version: v1
       name: nginx-service-2
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  cluster-name: member-1
        overrideType: Delete
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  cluster-name: member-2
        jsonPatchOverrides:
          - op: add
            path: /metadata/annotations
            value:
              {"service.beta.kubernetes.io/azure-dns-label-name":"fleet-test-member-2"}
```
Summary:
- It deletes the service so that the service won't be applied to the Member Cluster 1.
- It adds a DNS label for Member Cluster 2 so that the service can be added as Traffic Manager Endpoint.

#### New Service for Member Cluster 2
> Note:  service file located [here](./testfiles/nginx-service-2.yaml)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-service-2
  namespace: test-app
spec:
  selector:
    app: nginx
  ports:
    - protocol: TCP
      port: 80
      targetPort: 80
  type: LoadBalancer
---
apiVersion: networking.fleet.azure.com/v1alpha1
kind: ServiceExport
metadata:
  name: nginx-service-2
  namespace: test-app
```
Summary:
- Create another service named `nginx-service-2` in the `test-app` namespace.

#### Exposing the New Service as a Traffic Manager Endpoint using TrafficManagerBackend

When the new resources are available in the member-cluster by checking the CRP status, you can create a TrafficManagerBackend resource to expose the new service as a Traffic Manager endpoint.

> Note:  nginx-backend-2 file located [here](./testfiles/nginx-backend-2.yaml)

```yaml
apiVersion: networking.fleet.azure.com/v1alpha1
kind: TrafficManagerBackend
metadata:
  name: nginx-backend-2
  namespace: test-app
spec:
  profile:
    name: "nginx-profile"
  backend:
    name: "nginx-service-2"
  weight: 100
```

```bash
kubectl get tmb nginx-backend-2 -n test-app -o yaml
apiVersion: networking.fleet.azure.com/v1alpha1
kind: TrafficManagerBackend
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"networking.fleet.azure.com/v1alpha1","kind":"TrafficManagerBackend","metadata":{"annotations":{},"name":"nginx-backend-2","namespace":"test-app"},"spec":{"backend":{"name":"nginx-service-2"},"profile":{"name":"nginx-profile"},"weight":100}}
  creationTimestamp: "2024-11-21T05:44:33Z"
  finalizers:
  - networking.fleet.azure.com/traffic-manager-backend-cleanup
  generation: 1
  name: nginx-backend-2
  namespace: test-app
  resourceVersion: "1244573"
  uid: 9bd86bd4-c0d8-4303-a7f9-f20122b18abc
spec:
  backend:
    name: nginx-service-2
  profile:
    name: nginx-profile
  weight: 100
status:
  conditions:
  - lastTransitionTime: "2024-11-21T05:44:35Z"
    message: '1 service(s) exported from clusters cannot be exposed as the Azure Traffic
      Manager, for example, service exported from member-1 is invalid: DNS label is
      not configured to the public IP'
    observedGeneration: 1
    reason: Invalid
    status: "False"
    type: Accepted
  endpoints:
  - cluster:
      cluster: member-2
    name: fleet-9bd86bd4-c0d8-4303-a7f9-f20122b18abc#nginx-service-2#member-2
    target: fleet-test-member-2.westcentralus.cloudapp.azure.com
    weight: 100
```

Summary:
- Similar to the previous TrafficManagerBackend resource, this one routes all traffic to the new service `nginx-service-2` in Member Cluster 2 since the new service
of member-1 cannot be added as the Traffic Manager endpoint.
- Now nginx-profile has two backends now. Each backend has a weight of 100, which means all traffic will be evenly distributed to these two clusters.
- Adjusting the weight of the backends will allow you to control the traffic distribution between the two clusters.

#### Stop Serving Traffic from Member Cluster 1

![](after-migrate.png)


After the new service is up and running in Member Cluster 2, you can stop serving traffic from Member Cluster 1 by removing the TrafficManagerBackend resource.

> Note:  Existing client/application may still connect to member cluster 1 caused by a stale DNS query.

```bash
kubectl delete trafficmanagerbackend nginx-backend -n test-app
```

You can delete Member Cluster 1 and all the placed resources (excluding fleet networking resources, for example, serviceExport) will be left on the Member Cluster 1.
Make sure all the client DNS cache is reset before you destroy the service and deployment on the member cluster.

```bash
kubectl delete membercluster member-1
```

Lastly, clean up the old service, service export and their overrides which are not used anymore in member-2 by deleting them in the hub cluster.

```bash
kubectl delete resourceoverrides ro-nginx-service-export -n test-app
kubectl delete resourceoverrides ro-nginx-service -n test-app
kubectl delete serviceexport nginx-service -n test-app
kubectl delete service nginx-service -n test-app
```

## Conclusion
This tutorial demonstrated how to migrate applications and shifting the traffic using fleet from one cluster to another by using
clusterResourcePlacement, resourceOverrides, trafficManagerProfile and trafficManagerBackend APIs without any downtime.
