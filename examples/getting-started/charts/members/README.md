# Fleet Networking Getting Started Tutorial Member Cluster Resources Chart

## Before you begin

Install [Helm](https://helm.sh).

## Install the chart

```bash
helm install getting-started-tutorial-member-resources \
    ./examples/getting-started/charts/members \
    --set memberID=YOUR-MEMBER-CLUSTER
```

## Parameters

| Parameter | Description | Default |
|:-|:-|:-|
| `userNS` | The namespace for user workloads | `work` |
| `systemNS` | The namespace reserved for Fleet controllers and resources | `fleet-system` |
| `memberID` | The ID of the member cluster. | N/A |
