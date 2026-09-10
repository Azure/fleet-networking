# Local Developer E2E Guide

The E2E suite provisions one hub AKS cluster and two member AKS clusters, installs the Fleet
networking components, and runs the Ginkgo tests under `test/e2e`.

## Prerequisites

- [Go](https://go.dev/dl/) at the version declared in `go.mod`
- [Azure CLI](https://learn.microsoft.com/cli/azure/install-azure-cli)
- [Docker](https://docs.docker.com/engine/install/)
- [Helm](https://helm.sh/docs/intro/install/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [`jq`](https://jqlang.github.io/jq/download/)
- [`yq`](https://github.com/mikefarah/yq#install)
- Bash and GNU Make
- An Azure subscription in which the signed-in identity has the Owner role

Sign in and select the subscription before starting:

```bash
az login
export AZURE_SUBSCRIPTION_ID=<YOUR-SUBSCRIPTION-ID>
az account set --subscription "${AZURE_SUBSCRIPTION_ID}"
```

The setup builds and pushes test images, creates an Azure Container Registry and AKS clusters, and
assigns Azure roles. Use a test subscription rather than a production subscription.

## Choose an E2E scenario

All scenarios use the same setup, test, and cleanup targets. The environment variables determine
the cluster network topology and whether Traffic Manager reconciliation is enabled.

### Baseline Fleet networking

Use this scenario for ServiceExport, ServiceImport, MultiClusterService, and networking behavior
without Azure Traffic Manager:

```bash
export AZURE_RESOURCE_GROUP=<YOUR-RESOURCE-GROUP-NAME>
export AZURE_SUBSCRIPTION_ID=<YOUR-SUBSCRIPTION-ID>
export AZURE_NETWORK_SETTING=shared-vnet
export ENABLE_TRAFFIC_MANAGER=false
```

`AZURE_NETWORK_SETTING` supports:

- `shared-vnet`
- `dynamic-ip-allocation`
- `peered-vnet`
- `unsupported`

The scripts under `test/scripts` define each topology. `unsupported` verifies behavior when member
clusters do not have supported network connectivity.

### Azure Traffic Manager

Traffic Manager E2E provisions and validates real Azure Traffic Manager resources. It is supported
only with the shared-VNet topology:

```bash
export AZURE_RESOURCE_GROUP=<YOUR-RESOURCE-GROUP-NAME>
export AZURE_SUBSCRIPTION_ID=<YOUR-SUBSCRIPTION-ID>
export AZURE_NETWORK_SETTING=shared-vnet
export ENABLE_TRAFFIC_MANAGER=true
```

The setup configures the additional AKS identities, Azure role assignments, and controller feature
flags required by `test/e2e/traffic_manager_test.go`. Tests in that file skip when
`ENABLE_TRAFFIC_MANAGER` is not `true`.

### Azure Front Door and Gateway API

The current AFD-oriented E2E coverage is a live Kubernetes API contract test. Setup installs the
Gateway API CRDs in the hub, and `test/e2e/gateway_api_test.go` verifies that the API server accepts
and round-trips:

- `GatewayClass`, `Gateway`, and `HTTPRoute`
- Same-namespace Fleet `ServiceImport` backends
- Cross-namespace Fleet `ServiceImport` backends authorized by `ReferenceGrant`
- AFD annotations and the exact backend group, kind, port, and weight

Use the baseline environment:

```bash
export AZURE_RESOURCE_GROUP=<YOUR-RESOURCE-GROUP-NAME>
export AZURE_SUBSCRIPTION_ID=<YOUR-SUBSCRIPTION-ID>
export AZURE_NETWORK_SETTING=shared-vnet
export ENABLE_TRAFFIC_MANAGER=false
```

There is no `ENABLE_AFD` E2E flag yet. The Gateway controller manager currently has no AFD
reconcilers, and the E2E setup does not deploy it or create and validate Azure Front Door resources.
Real AFD reconciliation E2E will additionally need controller deployment, Azure identity and RBAC,
AFD configuration, Azure SDK validators, Gateway API status assertions, and Azure resource cleanup.

## Set up the E2E environment

After exporting the variables for one scenario, run:

```bash
make e2e-setup
```

The default cluster names are:

```bash
export HUB_CLUSTER=hub
export MEMBER_CLUSTER_1=member-1
export MEMBER_CLUSTER_2=member-2
```

Switch kubeconfig contexts with:

```bash
kubectl config use-context "${HUB_CLUSTER}-admin"
kubectl config use-context "${MEMBER_CLUSTER_1}-admin"
kubectl config use-context "${MEMBER_CLUSTER_2}-admin"
```

## Run the tests

Run the complete E2E suite:

```bash
make e2e-tests
```

The environment can be reused for repeated test runs. To run only the current Gateway API contract
scenario:

```bash
go test -timeout 50m -tags=e2e -v ./test/e2e \
  -args -ginkgo.v -ginkgo.focus='Gateway API contract'
```

## Collect logs

Collect controller and agent logs before cleanup when investigating a failure:

```bash
export LOG_DIR=agent-logs
make e2e-collect-logs
```

## Clean up

Delete the Azure resource group created for the E2E environment:

```bash
make e2e-cleanup
```

Cleanup requires `AZURE_RESOURCE_GROUP` to still identify the test resource group.
