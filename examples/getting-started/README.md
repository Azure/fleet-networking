# Fleet Networking Getting Started Tutorial

This document features a tutorial that explains how to set up and make use of the networking capabilities
provided by Fleet, specifically:

- how to install Fleet networking components to a fleet of Kubernetes clusters
- how to export a Kubernetes service from a single cluster
- how to create a multi-cluster service that exposes endpoints from a number of Kubernetes clusters

## Before you begin

- This tutorial assumes that you will be trying Fleet networking out with clusters from [Azure Kubernetes
Service (AKS)][AKS]. To learn more about AKS, see [AKS Documentation][AKS doc].

- Install the following prerequisites:

    - [`git`][git]
    - [Azure CLI][Azure CLI]
    - [Docker][docker]
    - [`kubectl`][kubectl]
    - [`helm`][helm]

- Sign into your Azure account and pick an Azure subscription. Resources needed for this tutorial will be created in this subscription. If you do not have an Azure subscription, [create a free account][free Azure account].

    > Costs may be incurred on your Azure subscription based on the resources you will use in this
    > tutorial. The amount should be trivial if you clean up the resources used in this tutorial
    > as soon as you finish it.

    ```sh
    az login
    # Replace YOUR-SUBSCRIPTION-ID with a value of your own.
    export SUBSCRIPTION_ID=YOUR-SUBSCRIPTION-ID
    az account set --subscription $SUBSCRIPTION_ID
    ```
- Clone the Fleet networking GitHub repository and change the work directory to the root path of the project:

    `git clone https://github.com/Azure/fleet-networking.git && cd fleet-networking`

## Bootstrapping the environment

To run this tutorial, you will need to set up:

- A resource group, where Azure resources are deployed and managed
- A virtual network, which the Kubernetes clusters use
- A container registry, from which the Kubernetes clusters will pull images
- Three Kubernetes clusters on AKS, one serving as the hub cluster and the other two member clusters;
all three clusters should be inter-connected on the network, i.e. Pods from different clusters can 
communicate directly with each other using their assigned Pod IP addresses.

**For your convenience, the tutorial provides a helper script, `bootstrap.sh`, which automatically provisions
the Azure resources needed**. To use the script, run

```sh
# You can use other resource group names and locations as you see fit.
export RESOURCE_GROUP=fleet-networking-tutorial
export LOCATION=eastus
# Replace YOUR-CONTAINER-REGISTRY with a name of your own; do NOT use the full URL, i.e. specify
# `bravelion` rather than `bravelion.azurecr.io`.
export REGISTRY=YOUR-CONTAINER-REGISTRY
chmod +x ./examples/getting-started/bootstrap.sh
# Run the script within existing shell as it sets a few variables that will be used in later steps.
. ./examples/getting-started/bootstrap.sh
```

After the script completes successfully, **skip** to the [Set up Kubernetes resources](#set-up-kubernetes-resources)
section below.

Alternatively, you can follow the step-by-step instructions below to create the Azure resources in your
subscription manually:

<details>
<summary>Instructions for manually creating Azure resources needed for this tutorial</summary>

### Create a resource group

Run the commands below to create a resource group named `fleet-networking-tutorial` in the `eastus` location;
you can use a different name or location as you see fit.

```sh
export RESOURCE_GROUP=fleet-networking-tutorial
export LOCATION=eastus
az group create --name $RESOURCE_GROUP --location $LOCATION
```

It may take a while before all commands complete.

### Create a virtual network

AKS clusters are inter-connected if they reside on the same virtual network, or they reside on different
virtual networks that are [peer-linked][peering]. For simplicity reasons, this tutorial uses the same
virtual network for all three clusters, each of which uses a subnet of their own on the network; run the
command below to create the virtual network and its subnets:

```sh
export VNET=fleet
export HUB_SUBNET=hub
export MEMBER_1_SUBNET=member-1
export MEMBER_2_SUBNET=member-2

az network vnet create \
    --name $VNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.0.0.0/8

az network vnet subnet create \
    --vnet-name $VNET \
    --name $HUB_SUBNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.1.0.0/16

az network vnet subnet create \
    --vnet-name $VNET \
    --name $MEMBER_1_SUBNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.2.0.0/16

az network vnet subnet create \
    --vnet-name $VNET \
    --name $MEMBER_2_SUBNET \
    -g $RESOURCE_GROUP \
    --address-prefixes 10.3.0.0/16
```

It may take a while before all commands complete.

### Create a container registry

Run the commands below to create a container registry, which Kubernetes clusters in this tutorial will
pull images from:

```sh
# Replace YOUR-CONTAINER-REGISTRY with a name of your own; do NOT use the full URL, i.e. specify `bravelion` rather
# than `bravelion.azurecr.io`.
export REGISTRY=YOUR-CONTAINER-REGISTRY
az acr create -g $RESOURCE_GROUP -n $REGISTRY --sku basic
az acr login -n $REGISTRY
```

It may take a while before all commands complete. Note that you may not be able to log into a container
registry right after it is created as it takes time for information to propagate; retry in a few seconds
if an error message that reports the container registry cannot be found is returned when running the
`az acr login` command.

### Create the AKS clusters

Run the commands below to create the three AKS clusters needed; the three clusters are named `hub` (the hub
cluster), `member-1`, and `member-2` (the member clusters) respectively. Note that it may take a long while
before a cluster spins up.

#### Create the hub cluster

```sh
export NODE_COUNT=2
export HUB_CLUSTER=hub
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $HUB_CLUSTER \
    --attach-acr $REGISTRY \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$HUB_SUBNET" \
    --yes
```

#### Create the first member cluster

```sh
export MEMBER_CLUSTER_1=member-1
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_1 \
    --attach-acr $REGISTRY \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_1_SUBNET" \
    --yes
```

#### Create the second member cluster

```sh
export MEMBER_CLUSTER_2=member-2
az aks create \
    --location $LOCATION \
    --resource-group $RESOURCE_GROUP \
    --name $MEMBER_CLUSTER_2 \
    --attach-acr $REGISTRY \
    --node-count $NODE_COUNT \
    --generate-ssh-keys \
    --enable-aad \
    --enable-azure-rbac \
    --network-plugin azure \
    --vnet-subnet-id "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP/providers/Microsoft.Network/virtualNetworks/$VNET/subnets/$MEMBER_2_SUBNET" \
    --yes
```

### Retrieve credentials for accessing the Kubernetes clusters

Run the commands below to retrieve the credentials for the AKS clusters provisioned in the previous step:

```sh
az aks get-credentials --name $HUB_CLUSTER -g $RESOURCE_GROUP --admin
az aks get-credentials --name $MEMBER_CLUSTER_1 -g $RESOURCE_GROUP --admin
az aks get-credentials --name $MEMBER_CLUSTER_2 -g $RESOURCE_GROUP --admin
```
</details>

## Set up Kubernetes resources

After all three AKS clusters spin up, you will need to create a few Kubernetes resources in each cluster,
which Fleet networking and this tutorial will use, specifically:

- A namespace, `work`, shared among all clusters, for putting user-end workloads, e.g. Kubernetes services
- A config map in the user namespace (`work`) of each member cluster, which denotes the ID of the member cluster.
- A namespace, `fleet-system`, reserved in all clusters, for running Fleet networking controllers and
putting internal resources
- One namespace for each member cluster in the hub cluster, where a member cluster can access to communicate
with the hub cluster
- RBAC resources (`Roles` and `RoleBindings`), which grant member clusters access to the hub cluster in their
respective namespaces

**For your convenience, this tutorial provides a number of Helm charts that allocate the resources automatically
to the hub and member clusters**. To use these charts, follow the steps below:

- Find the client IDs and principal IDs of managed identifies for the two member clusters. Member clusters
will use these IDs to authenticate themselves with the hub cluster.

    If you have [`jq`][jq] installed in your environment, run the commands below to retrieve the client
    IDs and principal IDs for the member cluster managed identifies:

    ```sh
    # To check if jq is available, run `which jq` in the shell. A path to the jq executable should be printed
    # out if jq is present in your system.
    export CLIENT_ID_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_1"_"$LOCATION" | jq --arg identity $MEMBER_CLUSTER_1-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
    export PRINCIPAL_FOR_MEMBER_1=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_1"_"$LOCATION" | jq --arg identity $MEMBER_CLUSTER_1-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
    export CLIENT_ID_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_2"_"$LOCATION" | jq --arg identity $MEMBER_CLUSTER_2-agentpool -r -c 'map(select(.name | contains($identity)))[].clientId')
    export PRINCIPAL_FOR_MEMBER_2=$(az identity list -g MC_"$RESOURCE_GROUP"_"$MEMBER_CLUSTER_2"_"$LOCATION" | jq --arg identity $MEMBER_CLUSTER_2-agentpool -r -c 'map(select(.name | contains($identity)))[].principalId')
    # Verify that the client IDs and principal IDs have been retrieved.
    echo $CLIENT_ID_FOR_MEMBER_1
    echo $PRINCIPAL_FOR_MEMBER_1
    echo $CLIENT_ID_FOR_MEMBER_2
    echo $PRINCIPAL_FOR_MEMBER_2
    ```

    Alternatively, you can find the client IDs and principal IDs via Azure portal:

    <details>
    <summary>Instructions for retrieving client IDs and principal IDs from Azure portal</summary>

    - Open [Azure portal][portal], and type `resource groups` in the search box at the top of the page. 
    - Click `Resource groups` in the list of matching services.
    - Find the resource group named `MC_[YOUR-RESOURCE-GROUP]_[YOUR-MEMBER-CLUSTER-1]_[YOUR-LOCATION]`, e.g.
        `MC_fleet-networking-tutorial_member-1_eastus`, in the list, and click on the name.
    - Find a managed identity resource named `[YOUR-MEMBER-CLUSTER-1]-agentpool`, e.g. `member-1-agentpool`, in
        the list of resources shown at the popped panel, and click on the name.
    - The client ID and principal ID is listed on the new page. Write down the two values:

        ```sh
        export CLIENT_ID_FOR_MEMBER_1=YOUR-CLIENT-ID-FOR-MEMBER-1
        export PRINCIPAL_FOR_MEMBER_1=YOUR-PRINCIPAL-ID-FOR-MEMBER-1
        ```

    - Repeat the steps above to find the client ID and the principal ID of the managed identity for the other
        member cluster.

        ```sh
        export CLIENT_ID_FOR_MEMBER_2=YOUR-CLIENT-ID-FOR-MEMBER-2
        export PRINCIPAL_FOR_MEMBER_2=YOUR-PRINCIPAL-ID-FOR-MEMBER-2
        ```
    
    </details>

- Apply the Helm chart for hub cluster resources:

    ```sh
    kubectl config use-context $HUB_CLUSTER-admin
    helm install getting-started-tutorial-hub-resources \
        ./examples/getting-started/charts/hub \
        --set principalIDForMemberA=$PRINCIPAL_FOR_MEMBER_1 \
        --set principalIDForMemberB=$PRINCIPAL_FOR_MEMBER_2
    ```

- Apply the Helm chart for the two member clusters:

    ```sh
    kubectl config use-context $MEMBER_CLUSTER_1-admin
    helm install getting-started-tutorial-member-resources \
        ./examples/getting-started/charts/members \
        --set memberID=$MEMBER_CLUSTER_1
    ```

    ```sh
    kubectl config use-context $MEMBER_CLUSTER_2-admin
    helm install getting-started-tutorial-member-resources \
        ./examples/getting-started/charts/members \
        --set memberID=$MEMBER_CLUSTER_2
    ```


After all Helm charts are installed successfully, **skip** to the [Build and install artifacts](#build-and-install-artifacts) section below.

Alternatively, you can follow the step-by-step instructions below to create the Kubernetes resources in each
cluster manually:

<details>
<summary>Instructions for manually creating Azure resources needed for this tutorial</summary>

### Retrieve client IDs and principal IDs of managed identifies for member clusters

See the instructions in the [previous section](#set-up-kubernetes-resources).

### Create resources in the hub cluster

- Switch to the hub cluster context.

    ```sh
    kubectl config use-context $HUB_CLUSTER-admin
    ```

- Run the commands below to create namespaces used by Fleet networking components and this tutorial in the hub cluster:

    ```sh
    kubectl create ns work
    kubectl create ns fleet-system
    kubectl create ns fleet-member-$MEMBER_CLUSTER_1
    kubectl create ns fleet-member-$MEMBER_CLUSTER_2
    ```

- Edit `./examples/getting-started/artifacts/hub-rbac.yaml`; replace the following values with your own:

    - `[YOUR-MEMBER-CLUSTER-1]` => the ID of the first member cluster (`$MEMBER_CLUSTER_1`)
    - `[YOUR-MEMBER-CLUSTER-2]` => the ID of the second member cluster (`$MEMBER_CLUSTER_2`)
    - `[YOUR-MEMBER-CLUSTER-1-PRINCIPAL-ID]` => the principal ID of the first member cluster (`$PRINCIPAL_FOR_MEMBER_1`)
    - `[YOUR-MEMBER-CLUSTER-2-PRINCIPAL-ID]` => the principal ID of the second member cluster (`$PRINCIPAL_FOR_MEMBER_2`)

- Apply the roles and role bindings:

    ```sh
    kubectl apply -f ./examples/getting-started/artifacts/hub-rbac.yaml
    ```

### Create resources in the member clusters

Run the commands below to create namespaces used by Fleet networking components and this tutorial in the two
member clusters:

```sh
kubectl config use-context $MEMBER_CLUSTER_1-admin
kubectl create ns fleet-system
kubectl create ns work
kubectl create configmap member-cluster-id --from-literal=id=$MEMBER_CLUSTER_1
kubectl config use-context $MEMBER_CLUSTER_2-admin
kubectl create ns fleet-system
kubectl create ns work
kubectl create configmap member-cluster-id --from-literal=id=$MEMBER_CLUSTER_2
```

</details>

## Build and install artifacts

Fleet networking consists of a number of custom resource definitions (CRDs) and controllers, some running in the
member clusters and some in the hub cluster. This tutorial also features a simple Python web application, which
helps showcase how endpoints are exposed from member clusters via a multi-cluster service.

**For your convenience, the tutorial provides a helper script, `build-install.sh`, which automatically builds
the images for the controllers + the web application, pushes the images to your container registry, and
installs the CRDs + controllers to the Kubernetes clusters you create**. To use the script, follow the steps
below:

- Find the URL of the hub cluster API server. Member clusters will talk with the hub cluster at this URL.

    If you have [`yq`][yq] installed in your environment, run the commands below to retrieve the hub cluster
    API server URL from your local `kubeconfig` file:

    ```sh
    # To check if yq is available, run `which yq` in the shell. A path to the yq executable should be printed
    # out if yq is present in your system.
    export HUB_URL=$(cat ~/.kube/config | yq eval ".clusters | .[] | select(.name=="\"$HUB_CLUSTER\"") | .cluster.server")
    # Verify that the URL has been retrieved.
    echo $HUB_URL
    ```

    Alternatively, you can find the URL by reading the `kubeconfig` file yourself:

    <details>
    <summary>Instructions for retrieving hub cluster API server URL from local `kubeconfig` file</summary>

    - Run `cat ~/.kube/config` to print the `kubeconfig` file.
    - In the `.clusters` array, find the struct with the name field set to the name of your hub cluster (`$HUB_CLUSTER`).
        The value kept in the `.cluster.server` field is the URL of the hub cluster API server. The struct should
        look similar to:

        ```yaml
        apiVersion: v1
        clusters:
        - cluster:
            certificate-authority-data: some-data
            server: https://YOUR-HUB-CLUSTER-API-SERVER.azmk8s.io:443
          name: hub
        ...
        ```
    
    - Write down the URL (e.g. `https://YOUR-HUB-CLUSTER-API-SERVER.azmk8s.io:443`):

        ```sh
        # Replace YOUR-HUB-URL with the value of your own.
        export HUB_URL=YOUR-HUB-URL
        ```
    
    </details>

- Run the helper script:

    ```sh
    chmod +x ./examples/getting-started/build-install.sh
    # Run the script within existing shell as it sets a few variables that will be used in later steps.
    . ./examples/getting-started/build-install.sh
    ```

After the script completes successfully, **skip** to the [Export services from member clusters](#export-services-from-member-clusters) section below.

Alternatively, you can follow the step-by-step instructions below to create the Azure resources in your
subscription manually:

<details>
<summary>Instructions for building and installing artifacts</summary>

### Retrieve the hub cluster API server URL

See the instructions in the [previous section](#build-and-install-artifacts).

### Build the controller images

Run the commands below to build the images for Fleet networking controllers, and push them to the container
registry you create:

```sh
export TAG=v0.1.0.test
export REGISTRY=$REGISTRY.azurecr.io
make docker-build-hub-net-controller-manager
make docker-build-member-net-controller-manager
make docker-build-mcs-controller-manager
```

It may take a long while before all commands complete.

### Build the application image

Run the commands below to build the image for the Python web application, and push it to the container
registry you create:

```sh
docker build -f ./examples/getting-started/app/Dockerfile ./examples/getting-started/app --tag $REGISTRY/app
docker push $REGISTRY/app
```

It may take a long while before all commands complete.

### Install the CRDs and the Fleet networking controllers

Run the commands below to install Fleet networking controllers in the hub cluster:

```sh
kubectl config use-context $HUB_CLUSTER-admin
kubectl apply -f config/crd/*
kubectl apply -f examples/getting-started/artifacts/crd.yaml
helm install hub-net-controller-manager \
    ./charts/hub-net-controller-manager/ \
    --set image.repository=$REGISTRY/hub-net-controller-manager \
    --set image.tag=$TAG
```

It may take a long while before all commands complete.

And run the commands below to install Fleet networking controllers in the member clusters:

```sh
kubectl config use-context $MEMBER_CLUSTER_1-admin
kubectl apply -f config/crd/*
helm install mcs-controller-manager \
    ./charts/mcs-controller-manager \
    --set image.repository=$REGISTRY/mcs-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_1 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_1
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
    --set image.repository=$REGISTRY/member-net-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_1 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_1
```

```sh
kubectl config use-context $MEMBER_CLUSTER_2-admin
kubectl apply -f config/crd/*
helm install mcs-controller-manager \
    ./charts/mcs-controller-manager \
    --set image.repository=$REGISTRY/mcs-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_2 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_2
helm install member-net-controller-manager ./charts/member-net-controller-manager/ \
    --set image.repository=$REGISTRY/member-net-controller-manager \
    --set image.tag=$TAG \
    --set config.hubURL=$HUB_URL \
    --set config.provider=azure \
    --set config.memberClusterName=$MEMBER_CLUSTER_2 \
    --set azure.clientid=$CLIENT_ID_FOR_MEMBER_2
```

It may take a long while before all commands complete.

</details>

## Export services from member clusters

Fleet networking components are now up and running in all clusters. It allows one to export a service from a
single member cluster to the fleet; once successfully exported, Fleet networking will sync the spec of the
service and all endpoints behind it to the hub, which other member clusters can consume.

- Switch to the first member cluster:

    ```sh
    kubectl config use-context $MEMBER_CLUSTER_1-admin
    ```

- Run the commands below to deploy the Python web application you build earlier to a member cluster, and expose
the application with a service:

    ```sh
    kubectl apply -f ./examples/getting-started/artifacts/app-svc.yaml
    ```

    Verify that all application pods are up and running with `kubectl get pods --namespace work`, and check the
    external load balancer address of the service with `kubectl get svc app --namespace work`. It may take a while
    before the external IP address becomes available.

    Open a browser window and visit the IP address. You should see a `Hello World!` message returned by the application.

- To export the service, create a `ServiceExport` object:

    ```sh
    kubectl apply -f ./examples/getting-started/artifacts/svc-export.yaml
    ```

    The `ServiceExport` object looks as follows:

    ```yaml
    apiVersion: networking.fleet.azure.com/v1alpha1
    kind: ServiceExport
    metadata:
        name: app
        namespace: work
    ```

    Verify that the service is successfully exported with `kubectl get svcexport app --namespace work`. You should
    see that the service is valid for export (`IS-VALID` is true) and has no conflicts with other exports
    (`IS-CONFLICT` is false). It may take a while before the export completes.

When exporting a service, Fleet networking follows the **namespace sameness** rule. That is, if two services, from
two different member clusters, are exported **from the same namespace with the same name**, and their specifications
are compatible, Fleet networking will consider them to be the same service. This makes it really easy to
deploy an application across multiple clusters and consume the endpoints as while. To try this out:

- Switch to the second member cluster:

    ```sh
    kubectl config use-context $MEMBER_CLUSTER_2-admin
    ```

- Run the commands below to deploy the Python web application you build earlier to the cluster, and expose
the application with a service:

    ```sh
    kubectl apply -f ./examples/getting-started/artifacts/app-svc.yaml
    ```

    Verify that all application pods are up and running with `kubectl get pods --namespace work`, and check the
    external load balancer address of the service with `kubectl get svc app --namespace work`. It may take a while
    before the external IP address becomes available.

    Open a browser window and visit the IP address. You should see a `Hello World!` message returned by the application.

- To export the service, create a `ServiceExport` object:

    ```sh
    kubectl apply -f ./examples/getting-started/artifacts/svc-export.yaml
    ```

    The service to export has the same name (`app`) as the one created earlier in the first member cluster, and is from
    the same namespace (`work`). Fleet networking will treat them as one unified service.

    Verify that the service is successfully exported with `kubectl get svcexport app --namespace work`. You should
    see that the service is valid for export (`IS-VALID` is true) and has no conflicts with other exports
    (`IS-CONFLICT` is false). It may take a while before the export completes.


## Expose fleet-wide endpoints from exported services with a multi-cluster service

To consume unified services exported across the fleet, Fleet networking provides the `MultiClusterService` API.
A `MultiClusterService` object specifies the name of a service in a specific namespace to **import** from the
hub cluster, and will sync its specification and endpoints with the hub cluster. The endpoints will then be
exposed with a load balancer. To test out the `MultiClusterService` API:

- Switch to a member cluster; here you will use the second member cluster:

    ```sh
    kubectl config use-context $MEMBER_CLUSTER_2-admin
    ```

    Note that you can import a service with the `MultiClusterService` API from any member cluster in the fleet,
    regardless of whether the member cluster has exported the service or not.

- Create a `MultiClusterService` object:

    ```sh
    kubectl apply -f ./examples/getting-started/artifacts/mcs.yaml --namespace work
    ```

    The `MultiClusterService` looks as follows:

    ```yaml
    apiVersion: networking.fleet.azure.com/v1alpha1
    kind: MultiClusterService
    metadata:
        name: app
        namespace: work
    spec:
        serviceImport:
            name: app
    ```

    Verify that the import is successful with `kubectl get mcs app --namespace work` (`IS-VALID` should be true). 
    Check out the external load balancer IP address (`EXTERNAL-IP`) in the output. It may take a while before
    the import is fully processed and the IP address becomes available.

    Open a browser window and visit the IP address. You should see a `Hello World!` message returned by the
    application; the message should also include the namespace + name of the endpoint (a pod), and the cluster
    where the pod comes from. Refresh the page for multiple times; you should see that pods from both
    member clusters are exposed by the `MultiClusterService`.

## Cleanup

You have now completed this getting started tutorial for Fleet networking. To clean up the resources you create
during this tutorial, run the commands below:

```sh
az group delete --name $RESOURCE_GROUP --yes
```

It may take a long while before all resources used are deleted.

You can learn more about Fleet networking at its [GitHub repository][fleet networking github repo].

[AKS]: https://azure.microsoft.com/en-us/services/kubernetes-service/
[AKS doc]: https://docs.microsoft.com/en-us/azure/aks/
[git]: https://git-scm.com/
[Azure CLI]: https://docs.microsoft.com/en-us/cli/azure/install-azure-cli
[docker]: https://www.docker.com/
[kubectl]: https://kubernetes.io/docs/tasks/tools/
[helm]: https://helm.sh
[free Azure account]: https://azure.microsoft.com/free/
[peering]: https://docs.microsoft.com/en-us/azure/virtual-network/virtual-network-peering-overview
[jq]: https://stedolan.github.io/jq/
[portal]: https://portal.azure.com
[yq]: https://github.com/mikefarah/yq
[fleet networking github repo]: https://github.com/azure/fleet-networking