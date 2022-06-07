// Package serviceexport features the ServiceExport controller for syncing Services and EndpointSlices
// between member clusters and the hub cluster in a fleet.
package serviceexport

import (
	"time"

	apicorev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	discoveryinformers "k8s.io/client-go/informers/discovery/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

// TO-DO (chenyu1): Update the API information when the design is finalized
// TO-DO (chenyu1): Refactor the GVR specifications when the API definition package becomes available
const (
	// ServiceExport GVR information
	svcExportGroup    = "mcs.networking.fleet.azure.io"
	svcExportVersion  = "v1alpha1"
	svcExportResource = "ServiceExport"

	// ServiceImport GVR information
	svcImportGroup    = "mcs.networking.fleet.azure.io"
	svcImportVersion  = "v1alpha1"
	svcImportResource = "ServiceImport"

	// Min. and max. delay for workqueue rate limiters
	minQueueRateLimiterRetryDelay = time.Second * 5
	maxQueueRateLimiterRetryDelay = time.Second * 30
)

// ServiceExport and ServiceImport GVR
// TO-DO (chenyu1): Refactor the GVR specifications when the API definition package becomes available
var svcExportGVR schema.GroupVersionResource = schema.GroupVersionResource{
	Group:    svcExportGroup,
	Version:  svcExportVersion,
	Resource: svcExportResource,
}
var svcImportGVR schema.GroupVersionResource = schema.GroupVersionResource{
	Group:    svcImportGroup,
	Version:  svcImportVersion,
	Resource: svcImportResource,
}

// TO-DO (chenyu1): Refactor these two interfaces with generics when Go 1.18 becomes available
type SvcSyncStatusTracker interface{}
type EndpointSliceSyncStatusTracker interface{}

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

// New returns a ServiceExport controller to sync Services and EndpointSlices between member clusters and
// the hub cluster.
func New(
	memberClusterID string,
	memberKubeClient clientset.Interface, memberDynamicClient dynamic.Interface,
	hubDynamicClient dynamic.Interface,
	memberSharedInformerFactory informers.SharedInformerFactory, memberDynamicInformerFactory dynamicinformer.DynamicSharedInformerFactory,
	hubDynamicInformerFactory dynamicinformer.DynamicSharedInformerFactory,
) (*Controller, error) {
	// Initialize informers
	memberSvcExportInformer := memberDynamicInformerFactory.ForResource(svcExportGVR)
	memberSvcInformer := memberSharedInformerFactory.Core().V1().Services()
	memberEndpointSliceInformer := memberSharedInformerFactory.Discovery().V1().EndpointSlices()
	hubSvcImportInformer := hubDynamicInformerFactory.ForResource(svcImportGVR)

	// Initialize workqueues
	memberSvcSyncWorkqueue := workqueue.NewNamedRateLimitingQueue(
		workqueue.NewItemExponentialFailureRateLimiter(minQueueRateLimiterRetryDelay, maxQueueRateLimiterRetryDelay),
		"memberService",
	)
	hubSvcSyncWorkqueue := workqueue.NewNamedRateLimitingQueue(
		workqueue.NewItemExponentialFailureRateLimiter(minQueueRateLimiterRetryDelay, maxQueueRateLimiterRetryDelay),
		"hubService",
	)
	memberEndpointSliceSyncWorkqueue := workqueue.NewNamedRateLimitingQueue(
		workqueue.NewItemExponentialFailureRateLimiter(minQueueRateLimiterRetryDelay, maxQueueRateLimiterRetryDelay),
		"memberEndpointSlices",
	)

	// Initializer status trackers
	svcSyncStatusTracker := NewSvcSyncStatusTracker()
	endpointSliceSyncStatusTracker := NewEndpointSliceSyncStatusTracker()

	// Initialize event broadcaster and recorder
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: memberKubeClient.CoreV1().Events("")})
	eventRecorder := eventBroadcaster.NewRecorder(scheme.Scheme,
		apicorev1.EventSource{Component: "serviceexport-controller"})

	// TO-DO (chenyu1): metrics

	c := Controller{
		memberClusterID:                  memberClusterID,
		memberKubeClient:                 memberKubeClient,
		memberDynamicClient:              memberDynamicClient,
		hubDynamicClient:                 hubDynamicClient,
		memberSvcExportInformer:          memberSvcExportInformer,
		memberSvcInformer:                memberSvcInformer,
		memberEndpointSliceInformer:      memberEndpointSliceInformer,
		hubSvcImportInformer:             hubSvcImportInformer,
		memberSvcSyncWorkqueue:           memberSvcSyncWorkqueue,
		hubSvcSyncWorkqueue:              hubSvcSyncWorkqueue,
		memberEndpointSliceSyncWorkqueue: memberEndpointSliceSyncWorkqueue,
		svcSyncStatusTracker:             svcSyncStatusTracker,
		endpointSliceSyncStatusTracker:   endpointSliceSyncStatusTracker,
		eventBroadcaster:                 eventBroadcaster,
		eventRecorder:                    eventRecorder,
	}

	return &c, nil
}
