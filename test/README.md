# Local Developer E2E Guide

## Prerequisites

- [`go`](https://golang.org/dl) 1.18.0 or later
- [Azure CLI](https://docs.microsoft.com/en-us/cli/azure/)
- [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- An [Azure](https://azure.microsoft.com/en-us/) subscription

## Run E2E tests

Make sure you have owner role to the e2e test subscription.

### Set up e2e environment

Currently e2e resources are built on top of Azure, so provide your Azure resource setting:

```bash
export AZURE_RESOURCE_GROUP=<YOUR-RESOURCE-GROUP-NAME>
export AZURE_SUBSCRIPTION_ID=<YOUR-SUBSCRIPTION-ID>
# Available values for AZURE_NETWORK_SETTING are shared-vnet, dynamic-ip-allocation and peered-vnet,
# and detailed explanations for each network setting are provided in the scripts under folder "test/scripts".
export AZURE_NETWORK_SETTING=shared-vnet
```

Run Makefile Target to setup e2e environment:

```bash
make e2e-setup
```

Until now, you will have one hub cluster and two member clusters according to your setting, and you may switch between clusters using the following commands:

```bash
export HUB_CLUSTER=hub
export MEMBER_CLUSTER_1=member-1
export MEMBER_CLUSTER_2=member-2
# Use hub cluster kubeconfig context
kubectl config use-context $HUB_CLUSTER-admin
# Use member cluster $MEMBER_CLUSTER_1 kubeconfig context
kubectl config use-context $MEMBER_CLUSTER_1-admin
# Use member cluster $MEMBER_CLUSTER_2 kubeconfig context
kubectl config use-context $MEMBER_CLUSTER_2-admin
```

Run e2e tests with the following command, and you may reuse the test environment to run e2e tests multiple times to validate your new tests or debug test issues.

```bash
make e2e-tests
```

Clean up  e2e tests resources:

```bash
make e2e-cleanup
```
