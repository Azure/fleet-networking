package serviceexport

import (
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	discoveryinformers "k8s.io/client-go/informers/discovery/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

// Controller (ServiceExport controller) runs on a member cluster and syncs Services
// (via ServiceExport and ServiceImport objects) and endpointSlices (via EndpointSliceExport objects)
// with the hub cluster.
type Controller struct {
	// Unique ID of the member cluster in the fleet
	memberClusterID string
	// Clients for operating on the built-in and dynamic resources in the member cluster
	memberKubeClient    clientset.Interface
	memberDynamicClient dynamic.Interface
	// Client for operating on the dynamic resources in the hub cluster
	hubDynamicClient dynamic.Interface
	// Informers for ServiceExport, Service, and EndpointSlice resources in the member cluster
	memberSvcExportInformer     informers.GenericInformer
	memberSvcInformer           coreinformers.ServiceInformer
	memberEndpointSliceInformer discoveryinformers.EndpointSliceInformer
	// Informer for ServiceImport resources in the hub cluster
	hubSvcImportInformer informers.GenericInformer
	// Workqueues for syncing Services with the hub cluster
	memberSvcSyncWorkqueue workqueue.RateLimitingInterface
	hubSvcSyncWorkqueue    workqueue.RateLimitingInterface
	// Workqueue for syncing EndpointSlices with the hub cluster
	memberEndpointSliceSyncWorkqueue workqueue.RateLimitingInterface
	// Trackers that track if a resource has been successfully sync'd before and its last sync'd spec
	svcSyncStatusTracker           SvcSyncStatusTracker
	endpointSliceSyncStatusTracker EndpointSliceSyncStatusTracker
	// Broadcaster and recorder for handling events
	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder
}
