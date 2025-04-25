# DNS Based Global Load Balancing Troubleshooting Guide

This guide provides troubleshooting steps for common issues related to DNS Based Global Load Balancing.


## Troubleshoot why TrafficManagerProfile is not programmed

Common reasons and solutions for `TrafficManagerProfile` not being programmed:

1. Invalid resource group or not enough permissions to create/update Azure traffic manager profile in the resource group.
   - Ensure that the resource group exists and fleet networking controller has been configured correctly to access the resource group.
2. DNS name is not available.
   - The DNS may be occupied by other resources. Try to use a different profile name or namespace name.
3. [Reach the Azure Traffic Manager limits](https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/azure-subscription-service-limits#azure-traffic-manager-limits).
   - 200 profiles are allowed per subscription. If the limit is reached, consider deleting unused profiles or requesting an increase in the limit.

Please check the `status` field of the `TrafficManagerProfile` or the `trafficmanagerprofile/controller.go` file in hub-net-controller-manager logs for more information.

## Troubleshoot why TrafficManagerBackend is not accepted

Common reasons and solutions for `TrafficManagerBackend` not being accepted:

1. Invalid `profile`
   - Ensure that the `TrafficManagerProfile` is created in the same namespace of the `TrafficManagerBackend`.
   - Ensure that the programmed condition of `TrafficManagerProfile` is accepted. 
   - Ensure that the Azure traffic manager profile exists, which could be manually deleted by other users. To recover this profile, you need to delete the `TrafficManagerBackend` and re-create it.
2. Invalid `backend`
   - Ensure that the `Service` exists in the same namespace of the `TrafficManagerBackend`.
   - Ensure that the at least `Service` of a member cluster is exported in the same namespace of the `TrafficManagerBackend` by creating `ServiceExport`.
   - Ensure that the exported `Service` is load balancer type and exposed via an Azure public IP address, which has a DNS name assigned to be used in a Traffic Manager profile.
3. Invalid resource group or not enough permissions to create/update Azure traffic manager endpoints in the resource group.
    - Ensure that the resource group exists and fleet hub networking controller has been configured correctly to access the resource group.
4. Not enough permissions to read the public IP address of the exported `Service` on the members.
   - Ensure fleet hub networking controller has been configured correctly to access public IP address of services on the members.
5. [Reach the Azure Traffic Manager limits](https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/azure-subscription-service-limits#azure-traffic-manager-limits).
    - 200 endpoints are allowed per profile. If the limit is reached, consider deleting unused endpoints or requesting an increase in the limit.
   
Please check the `status` field of the `TrafficManagerBackend` or the `trafficmanagerbackend/controller.go` hub-net-controller-manager logs for more information.
