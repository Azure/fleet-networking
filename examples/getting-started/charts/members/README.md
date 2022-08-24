# Fleet Networking Getting Started Tutorial Hub Cluster Resources Chart

## Before you begin

Install [Helm](https://helm.sh).

## Install the chart

```bash
helm install getting-started-tutorial-hub-resources \
    ./examples/getting-started/charts/hub \
    --set principalIDForMember1=YOUR-PRINCIPAL-ID-FOR-MEMBER-1 \
    --set principalIDForMember2=YOUR-PRINCIPAL-ID-FOR-MEMBER-2 \
    --set userNS=YOUR-USER-NS
```

## Parameters

| Parameter | Description | Default |
|:-|:-|:-|
| `userNS` | The namespace for user workloads | `` |
| `systemNS` | The namespace reserved for Fleet controllers and resources | `fleet-system` |
| `member1ID` | The ID of member cluster 1. | `member-1` |
| `member2ID` | The ID of member cluster 2. | `member-2` |
| `principalIDForMember1` | The principal ID of member cluster 1. | N/A |
| `principalIDForMember2` | The principal ID of member cluster 2. | N/A |
