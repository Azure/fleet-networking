# Fleet Networking Getting Started Tutorial Hub Cluster Resources Chart

## Before you begin

Install [Helm](https://helm.sh).

## Install the chart

```bash
helm install getting-started-tutorial-hub-resources \
    ./examples/getting-started/charts/hub \
    --set principalIDForMemberA=YOUR-PRINCIPAL-ID-FOR-MEMBER-1 \
    --set principalIDForMemberB=YOUR-PRINCIPAL-ID-FOR-MEMBER-2 \
    --set userNS=YOUR-USER-NS
```

## Parameters

| Parameter | Description | Default |
|:-|:-|:-|
| `userNS` | The namespace for user workloads | `` |
| `systemNS` | The namespace reserved for Fleet controllers and resources | `fleet-system` |
| `memberAID` | The ID of member cluster 1. | `member-1` |
| `memberBID` | The ID of member cluster 2. | `member-2` |
| `principalIDForMemberA` | The principal ID of member cluster 1. | N/A |
| `principalIDForMemberB` | The principal ID of member cluster 2. | N/A |
