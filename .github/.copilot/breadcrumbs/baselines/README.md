# ATM chart baseline fixtures (D9 zero-diff regression guard)

Captured with Helm v4.2.3 on 2026-07-18T23:23:52.8606428-07:00.
Chart: charts/hub-net-controller-manager (unmodified).

| File | SHA-256 | Purpose |
|---|---|---|
| hub-net-atm-default.yaml | D6243E35628EE77DAD3A3D204BEEE6CB33B41B1508967EEF55D6B78D5883BA6D | Default values (enableTrafficManagerFeature=false). Establishes the baseline that MUST remain byte-identical after AFD work lands. |
| hub-net-atm-enabled.yaml | 309205EEDE8A102962368861AA2BBC65C75FA44307E457E1252F76C1617857AB | ATM enabled with representative values. Establishes the baseline for the ATM-on upgrade path. |

## Regression check (run before merging any AFD PR)

```
helm template hub-net charts/hub-net-controller-manager > /tmp/atm-default.yaml
diff .github/.copilot/breadcrumbs/baselines/hub-net-atm-default.yaml /tmp/atm-default.yaml

helm template hub-net charts/hub-net-controller-manager \
  --set enableTrafficManagerFeature=true \
  --set azureCloudConfig.tenantId=00000000-0000-0000-0000-000000000000 \
  --set azureCloudConfig.subscriptionId=11111111-1111-1111-1111-111111111111 \
  --set azureCloudConfig.useManagedIdentityExtension=true \
  --set azureCloudConfig.userAssignedIdentityID=22222222-2222-2222-2222-222222222222 \
  --set azureCloudConfig.resourceGroup=rg-fleet \
  --set azureCloudConfig.location=eastus \
  > /tmp/atm-enabled.yaml
diff .github/.copilot/breadcrumbs/baselines/hub-net-atm-enabled.yaml /tmp/atm-enabled.yaml
```

Both diffs MUST be empty. Any output indicates a violation of D9's compatibility contract.
