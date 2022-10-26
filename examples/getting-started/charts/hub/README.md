# Fleet Networking Getting Started Tutorial Hub Cluster Resources Chart

## Before you begin

Install [Helm](https://helm.sh).

## Install the chart

```bash
helm install getting-started-tutorial-hub-resources \
    ./examples/getting-started/charts/hub \
    --set userNS=YOUR-USER-NS
    --set memberClusterConfigs[0].memberID=YOUR-MEMBER-CLUSTER-1
    --set memberClusterConfigs[0].principalID=YOUR-MEMBER-CLUSTER-1-PRINCIPAL-ID
    --set memberClusterConfigs[1].memberID=YOUR-MEMBER-CLUSTER-2
    --set memberClusterConfigs[1].principalID=YOUR-MEMBER-CLUSTER-2-PRINCIPAL-ID
```

## Parameters

| Parameter | Description | Default |
|:-|:-|:-|
| `userNS` | The namespace for user workloads | `` |
| `systemNS` | The namespace reserved for Fleet controllers and resources | `fleet-system` |
| `memberClusterConfigs` | The member cluster configurations; each member cluster should have an ID and a principal ID. | N/A |
| `memberClusterConfigs[*].memberID` | The ID of the member cluster. | N/A |
| `memberClusterConfigs[*].principalID` | The principal ID of the member cluster. | N/A |
