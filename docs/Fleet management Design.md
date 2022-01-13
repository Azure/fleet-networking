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
            |  +------------------+                          |   
            |  | ClusterClaims    |    etc.                  |                                               
            |  +------------------+                          |
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
    // URL is the URL of apiserver endpoint of the managed cluster.
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

### Design details
Here is how we handle the following functionalities in the OCM in our new controller. The overall idea is that 
a `managedCluster` CR represents a member cluster. A cluster joins the fleet when a corresponding `managedCluster` CR is
successfully applied to the hub cluster. Similarly, a member cluster leaves the hub cluster when its corresponding 
`managedCluster` CR is removed from the cluster. 

#### Join a member cluster to the fleet
The managedCluster will look for a secret named `name of the cluster`-`kubeconfig`. 
For example, the controller that reconciles a `managedCluster` called "member-cluster-A" will look for
a secret named `member-cluster-A-kubeconfig`. The secret contains the entire kubeconfig data. The controller will then 
create a namespace of the same name if it does not already exist.

There is another way to provide the credentials of member clusters to the hub cluster. There is a `ClientConfig`
field in the `managedCluster` definition, described above, that is designed to contain the access info.
However, the upstream OCM project currently is not utilizing it so that it does not contain the necessary fields to keep
the certificate and key data. We may propose to add the credential data fields to the CRD in the future so that it saves

#### Collect the cluster status and claims
In the current OCM, the Klusterlet controller updates the status of a `managedCluster` which includes the following fields

* Capacity
* Allocatable
* Version (ManagedClusterVersion)
* ClusterClaims

The first three fields contains information that one can get from kube client by querying all the nodes periodically.
The last one has to be provided by the member cluster. There is already a customer object named `ClusterClaim` that contains
the required information.  Therefore, we propose that the `managedCluster` controller to just list the `ClusterClaim` objects in the 
member cluster to fill this part.

#### Work with `work` API
The `work` API is the way how OCM manage "applications". Its controller is running in the member cluster in the current pull
model. To make it work with push model, we need to implement it with the `managedCluster` controller. Here is how it works

* The `managedCluster` controller reconciler keeps an in-memory map of member cluster -> manifestwork controller
* for each new member cluster, it creates a new manifestwork controller that watches all the `ManifestWork` objects in the 
corresponding namespace and run it in separate go routine.


#### Remove a member cluster from the fleet
We will add a finalizer in the `managedCluster` object to make sure that we will clean up the resources in the corresponding
member cluster when one removes the object itself. Potential things to clean up

1. Any in memory object associated with the member cluster such as the worker reconcile loop
2. Fleet related Claims/Secrete/configMap in the member cluster
3. Any metadata recording on the side (i.e a database)



## Graduation Criteria
- [ ] All functions in this design implemented
- [ ] CI pipeline setup on Github Action running all unit/integration test suite running on Azure
- [ ] Helm chart created and hosted in Azure
- [ ] v0.1.0 release cut with working quick start and proper release documents with examples

### Test Plan

* Unit tests that make sure the project's fundamental quality.
* Integration tests using Gingko build-in test suite
* Manual E2E test