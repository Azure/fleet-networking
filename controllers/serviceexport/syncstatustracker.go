package serviceexport

type svcSyncStatusTrackerImpl struct{}

func NewSvcSyncStatusTracker() SvcSyncStatusTracker {
	return &svcSyncStatusTrackerImpl{}
}

type endpointSliceSyncStatusTrackerImpl struct{}

func NewEndpointSliceSyncStatusTracker() EndpointSliceSyncStatusTracker {
	return &endpointSliceSyncStatusTrackerImpl{}
}
