# DNS Based Global Load Balancing Troubleshooting Guide

This guide provides troubleshooting steps for common issues related to DNS Based Global Load Balancing.


## Troubleshoot why TrafficManagerProfile is not programmed

Common reasons and solutions for `TrafficManagerProfile` not being programmed:

1. Invalid resource group or not enough permissions to create/update Azure traffic manager profile in the resource group.
   - Ensure that the resource group exists and fleet networking controller has been configured correctly to access the resource group.
```yaml
# sample status
status:
  conditions:
  - lastTransitionTime: "2025-04-29T02:57:33Z"
    message: |
      Invalid profile: GET https://management.azure.com/subscriptions/xxx/resourceGroups/your-fleet-atm-rg/providers/Microsoft.Network/trafficmanagerprofiles/fleet-34ec2e40-5cc4-4a30-8c09-4b787169cef0
      --------------------------------------------------------------------------------
      RESPONSE 403: 403 Forbidden
      ERROR CODE: AuthorizationFailed
      --------------------------------------------------------------------------------
      {
        "error": {
          "code": "AuthorizationFailed",
          "message": "The client 'xxx' with object id 'xxx' does not have authorization to perform action 'Microsoft.Network/trafficmanagerprofiles/read' over scope '/subscriptions/xxx/resourceGroups/your-fleet-atm-rg/providers/Microsoft.Network/trafficmanagerprofiles/fleet-34ec2e40-5cc4-4a30-8c09-4b787169cef0' or the scope is invalid. If access was recently granted, please refresh your credentials."
        }
      }
      --------------------------------------------------------------------------------
    observedGeneration: 1
    reason: Invalid
    status: "False"
    type: Programmed
```
2. DNS name is not available.
   - The DNS may be occupied by other resources. Try to use a different profile name or namespace name.
```yaml
# sample status
 status:
    conditions:
    - lastTransitionTime: "2025-04-29T06:39:10Z"
      message: Domain name is not available. Please choose a different profile name
        or namespace
      observedGeneration: 2
      reason: DNSNameNotAvailable
      status: "False"
      type: Programmed
```
3. [Reach the Azure Traffic Manager limits](https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/azure-subscription-service-limits#azure-traffic-manager-limits).
   - 200 profiles are allowed per subscription. If the limit is reached, consider deleting unused profiles or requesting an increase in the limit.

Please check the `status` field of the `TrafficManagerProfile` or the `trafficmanagerprofile/controller.go` file in hub-net-controller-manager logs for more information.

## Troubleshoot why TrafficManagerBackend is not accepted

Common reasons and solutions for `TrafficManagerBackend` not being accepted:

1. Invalid `profile`
   - Ensure that the `TrafficManagerProfile` is created in the same namespace of the `TrafficManagerBackend`.
     ```yaml
     # sample status
     status:
      conditions:
      - lastTransitionTime: "2025-04-29T06:43:57Z"
      message: TrafficManagerProfile "invalid-profile" is not found
      observedGeneration: 1
      reason: Invalid
      status: "False"
      type: Accepted
     ```
   - Ensure that the programmed condition of `TrafficManagerProfile` is accepted. 
     ```yaml
     # sample status
     status:
       conditions:
       - lastTransitionTime: "2025-04-29T07:00:04Z"
       message: 'Invalid trafficManagerProfile "nginx-nginx-profile": Domain name is
       not available. Please choose a different profile name or namespace'
       observedGeneration: 1
       reason: Invalid
       status: "False"
       type: Accepted
     ```
   - Ensure that the Azure traffic manager profile exists, which could be manually deleted by other users. To recover this profile, you need to delete the `TrafficManagerBackend` and re-create it.
2. Invalid `backend`
   - Ensure that the `Service` exists in the same namespace of the `TrafficManagerBackend`.
   ```yaml
   # sample status
   status:
    conditions:
    - lastTransitionTime: "2025-04-29T07:50:49Z"
      message: ServiceImport "invalid-service" is not found
      observedGeneration: 1
      reason: Invalid
      status: "False"
      type: Accepted
   ```
   - Ensure that the at least `Service` of a member cluster is exported in the same namespace of the `TrafficManagerBackend` by creating `ServiceExport`.
   - Ensure that the exported `Service` is load balancer type and exposed via an Azure public IP address, which must have a DNS name assigned to be used in a Traffic Manager profile.
   ```yaml
   # sample status
   status:
    conditions:
    - lastTransitionTime: "2025-04-29T07:56:05Z"
      message: '1 service(s) exported from clusters cannot be exposed as the Azure
        Traffic Manager, for example, service exported from aks-member-5 is invalid:
        unsupported service type "ClusterIP"'
      observedGeneration: 1
      reason: Invalid
      status: "False"
      type: Accepted
   ```
3. Invalid resource group or not enough permissions to create/update Azure traffic manager endpoints in the resource group.
    - Ensure that the resource group exists and fleet hub networking controller has been configured correctly to access the resource group.
   ```yaml
   # sample status
   status:
    conditions:
    - lastTransitionTime: "2025-05-08T09:38:36Z"
      message: Azure Traffic Manager profile "fleet-6dd24764-0e46-4b52-b9c6-cc2a3f2535f9" under "your-fleet-atm-rg" is not found
      observedGeneration: 2
      reason: Invalid
      status: "False"
      type: Accepted
   ```
4. Not enough permissions to read the public IP address of the exported `Service` on the members.
   - Ensure fleet hub networking controller has been configured correctly to access public IP address of services on the members.
5. [Reach the Azure Traffic Manager limits](https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/azure-subscription-service-limits#azure-traffic-manager-limits).
    - 200 endpoints are allowed per profile. If the limit is reached, consider deleting unused endpoints or requesting an increase in the limit.
   
Please check the `status` field of the `TrafficManagerBackend` or the `trafficmanagerbackend/controller.go` hub-net-controller-manager logs for more information.
