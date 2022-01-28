# Push based Fleet Management Design

The proposed work would support our initial push based fleet management solution in a way that is compatible with the pull model used in OCM.


## Motivation

Currently, the open cluster management default solution requires the user to install multiple controllers in both the hub and the 
member cluster. The member clusters join the hub cluster through a CertificateSigningRequest,
and it also requires the hub controller to accept before the member cluster mark itself as joined.
While this is a more secure approach and scale better, the initial setup can be a little daunting for a casual user to give our solution a try.
Therefore, we are proposing to simplify the overall architecture in our initial open source solution to only support push model.

This ability will bring some additional possible benefits:

* The hub cluster controller can directly pull the status of any object it applies to the member cluster. 
Thus, we can get a single panel view of any distributed resources.
* This setup matches well with managed/hosted hub clusters where they are hosted in a cloud provider's private virtual network 
while the member clusters are running in the user space. 
* This setup can also address the problem that some users cannot run CertificateSigningRequest(CSR) in their environment.

### Goals

* Design the CRD of the push based managedCluster and functionality of its controller
* Support as much existing functionalities of the OCM as possible

### Non-Goals

* Support running managed controllers outside the hub/member cluster
* Support private managed cluster unreachable from hub cluster
* Authenticate managed cluster without using CSR(CertificateSigningRequest) in the original pull model.

## Proposal

With the push based approach, we simplified the architect of a fleet to only have one controller, sitting in the hub cluster,
that manages the fleet membership and work/policy distribution. Luckily, there is an existing CRD called `ManagedCluster` 
in OCM that provides (almost) all the necessary information. In this design, we propose to reuse this customer resource as 
is in the push model.

However, in order to differentiate from the existing pull based model, we will implement a new controller to watch the `ManagedCluster` custom resources.
In this way, we can start our project with a clean sheet but still keep compatible with most the existing OCM solutions. 

### Architecture
```
        +----------------------------------------------------------+                                               
        |  hub-cluster                                             |                                               
        |                                                          |                                               
        |   +------------+  +----------------+                     |
        |   | kubeconfig |  | deployment:    |  +---------------+  |
        |   | secrets    |  |                |  | member cluster|  | 
        |   +------------+  | hubCluster     |  | namespaces    |  |
        |                   | controller     |  +---------------+  |
        |                   +--------|-------+                     |                                               
        |                            |                             |                                               
        +----------------------------|-----------------------------+                                               
                                     |                                                                          
                                     |                                                           
                                     |watch,create,update,delete...                                                   
                                     |                                                                                
            +------------------------v-----------------------+                                               
            |  member-cluster                                |       
            |  +------------------+    +--------------+      |   
            |  | ClusterClaims    |    | member lease | etc. |                                               
            |  +------------------+    +--------------+      |
            +------------------------------------------------+
```

### API Design
Just to make this document more self-contained. Here I paste the golang definition of the `ManagedCluster` CRD spec.

```golang

type ManagedClusterSpec struct {
	// +optional
	ManagedClusterClientConfigs []ClientConfig `json:"managedClusterClientConfigs,omitempty"`

	// +required
	HubAcceptsClient bool `json:"hubAcceptsClient"`

	// +optional
	LeaseDurationSeconds int32 `json:"leaseDurationSeconds,omitempty"`
}

// TODO: we can add the cert information here so we don't need a kubeconfig
type ClientConfig struct {
    // +required
    URL string `json:"url"`

    // CABundle is the ca bundle to connect to apiserver of the managed cluster.
    // System certs are used if it is not set.
    // +optional
    CABundle []byte `json:"caBundle,omitempty"`
}

// ManagedClusterStatus represents the current status of joined managed cluster.
type ManagedClusterStatus struct {
    // Conditions contains the different condition statuses for this managed cluster.
    Conditions []metav1.Condition `json:"conditions"`
    
    // Capacity represents the total resource capacity from all nodeStatuses
    // on the managed cluster.
    Capacity ResourceList `json:"capacity,omitempty"`
    
    // Allocatable represents the total allocatable resources on the managed cluster.
    Allocatable ResourceList `json:"allocatable,omitempty"`
    
    // Version represents the kubernetes version of the managed cluster.
    Version ManagedClusterVersion `json:"version,omitempty"`
    
    // +optional
    ClusterClaims []ManagedClusterClaim `json:"clusterClaims,omitempty"`
}

type ManagedClusterClaim struct {
    // Name is the name of a ClusterClaim resource on managed cluster. It's a well known
    // or customized name to identify the claim.
    Name string `json:"name,omitempty"`
    
    // Value is a claim-dependent string
    Value string `json:"value,omitempty"`
}

// ResourceList defines a map for the quantity of different resources, the definition
// matches the ResourceList defined in k8s.io/api/core/v1.
type ResourceList map[ResourceName]resource.Quantity

```

## Design details
Here is how we handle the following functionalities in the OCM in our new controller. The overall idea is that 
a `managedCluster` CR represents a member cluster. A cluster joins the fleet when a corresponding `managedCluster` CR is
successfully applied to the hub cluster. Similarly, a member cluster leaves the hub cluster when its corresponding 
`managedCluster` CR is removed from the cluster. 

### Join a member cluster to the fleet
The managedCluster will look for a secret named `name of the cluster`-`kubeconfig`. 
For example, the controller that reconciles a `managedCluster` called "member-cluster-A" will look for
a secret named `member-cluster-A-kubeconfig`. Initially, the secret can simply contain the entire kubeconfig data.

The controller keeps an in-memory map that keys on the cluster name to track all the information belong to the cluster. 
One of the information can be the kubeclient created from the secret. Here are a list of operations a controller will 
perform in the member cluster joining process

1. The controller "claims" the member cluster by creating a "lease" configMap in the member cluster.
   * The configMap's name is a well known name by convention.
   * It contains the fleet UID so that one cluster cannot join multiple fleet. We can use the UID of the hub cluster as the fleet UID for now.
   * The controller will reject the member cluster if the configMap already contains a fleet ID not the same as its ID.
   * We can add more information to the configMap such as the join time, last health check result, last health check time, etc.
2. The controller creates a namespace of the same name as the member cluster in the hub cluster.
3. The controller starts a health check loop for each member cluster which
   * Checks the health of the member cluster by updating the last health check time in the lease configMap
   * Lists all the nodes in a member cluster and aggregates their `Capacity` and `Allocatable`
   * Updating the `managedCluster` condition status

#### member cluster lease
Since a configMAP has no schema, an alternative way is to create a CRD as the lease so that we can have a more defined
lease object. The downside is that we have to install something on the member cluster, since claims are optional. 

Here is the proposed lease spec. The hub controller will reconcile each managedCluster with an interval. 
The reconciler logic does the following:

* it fetches the corresponding MemberClusterLease object in the member cluster.
  * it marks the member cluster as unhealthy/unreachable in the mangedCluster CR status if it cannot fetch the lease
* it checks if the fleetID in the lease object matches its own. 
* it updates the LastLeaseRenewTime in the lease
* it updates the LastJoinTime in the lease if the member cluster is not healthy before

```go
type MemberClusterLeaseSpec struct {
    // the unique ID of the fleet this member cluster belongs to
    fleetID string `json:"fleetID"`

    // LastLeaseRenewTime is the last time the hub cluster controller can reach the member cluster
	LastLeaseRenewTime metav1.Time `json:"lastLeaseRenewTime"`

    // LastJoinTime is the last time the hub cluster controller re-establish connection to the member cluster
    LastJoinTime metav1.Time `json:"lastJoinTime"`
}
```

### API definition change proposal
There is actually a better way to provide the credentials of member clusters to the hub cluster than using a naming convention.
There is a `ClientConfig` field in the `managedCluster` definition, described above, that is designed to contain the member cluster access info.
However, the upstream OCM project currently is not utilizing it so that it does not contain the necessary fields to save
the user credentials. We will propose to add a path to a file that contains the user credentials. The file will be
on volume mounted to the secret described above. 

Cert rotation is also another concern in the real production environment. In order to support member cluster api-server
cert rotation, we will have to make the CABundle a list so that it can contain both the new and old server CA certificate.

Here is the new ClientConfig we will propose

```golang
type ClientConfig struct {
    // Server URL
	// +required 
	URL string `json:"url"`

    // CABundle is a list of ca bundle to connect to apiserver of the managed cluster.
    // System certs are used if it is not set.
    // +optional 
	CABundles []byte `json:"caBundles,omitempty"`
	
	// CredentialPath is the path to the file that contains the user credential 
	CredentialPath string `json:"credential"`
}

```

### Cluster status and claims

In the current OCM, the Klusterlet controller updates the status of a `managedCluster` which includes the following fields

* Capacity
* Allocatable
* Version (ManagedClusterVersion)
* ClusterClaims

The first three fields contains information that one can get from the member cluster. In our push version, the hub controller
will update those fields in its periodical health check loop describe above. It also set the condition of the member cluster
with types like below.

```
Status:
  Allocatable:
    Cpu:     3800m
    Memory:  9131Mi
  Capacity:
    Cpu:     4
    Memory:  13907Mi
  Conditions:
    Last Transition Time:  2021-10-19T01:24:58Z
    Message:               Managed cluster joined
    Reason:                ManagedClusterJoined
    Status:                True
    Type:                  ManagedClusterJoined
    Last Transition Time:  2021-12-26T08:11:38Z
    Message:               Managed cluster is available
    Reason:                ManagedClusterAvailable
    Status:                True
    Type:                  ManagedClusterConditionAvailable
  Version:
    Kubernetes:  v1.20.9
```

The last one has to be provided by the member cluster. There is already a customer object named `ClusterClaim` that contains
the [required information] (https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/4-cluster-claims). 
Each claim contains just one of the required property. Therefore, the hub controller can just list the `ClusterClaim` objects in the
member cluster and fill the `ClusterClaim` fields in the status.

### Work with `work` API
The `work` API is the way how OCM manage "applications". Its controller is running in the member cluster in the current pull
model. To make it work with push model, we need to implement it with the `managedCluster` controller. Here is how it works

* The `managedCluster` controller reconciler keeps an in-memory map of member cluster -> manifestwork controller
* for each new member cluster, it creates a new manifestwork sub-controller that watches all the `ManifestWork` objects in the 
corresponding namespace and run it in a separate go routine.
* The manifestwork sub-controller functions pretty much like the existing worker controller groups in the member cluster which
  * for each `ManifestWork` object in the namespace, it extracts the content of the objects and apply them to the corresponding member cluster
  * it merges the status of the applied object in the member cluster back to the `ManifestWork` object in the hub cluster.
  * It uses a finalizer to make sure that when a `ManifestWork` object is deleted from the hub cluster, all the resources used in the member cluster is also removed. 


### Remove a member cluster from the fleet
We will add a finalizer in the `managedCluster` object to make sure that we will clean up the resources in the corresponding
member cluster when one removes the object itself. Potential things to clean up

1. Any member cluster related object in the hub cluster such as its kubeconfig secret, the corresponding namespace
2. Any in memory object associated with the member cluster such as the worker reconcile loop
3. The lease configMap in the member cluster
4. Fleet related Claims/Secrete/configMap in the member cluster



## Graduation Criteria
- [ ] All functions in this design implemented
- [ ] CI pipeline setup on Github Action running all unit/integration test suite running on Azure
- [ ] Helm chart created and hosted in Azure
- [ ] v0.1.0 release cut with working quick start and proper release documents with examples

### Test Plan

* Unit tests that make sure the project's fundamental quality.
* Integration tests using Gingko build-in test suite
* Manual E2E test