package serviceexport

type defaultSvcSyncStatusTracker struct{}

func NewSvcSyncStatusTracker() SvcSyncStatusTracker {
	return &defaultSvcSyncStatusTracker{}
}

type defaultEndpointSliceSyncStatusTracker struct{}

func NewEndpointSliceSyncStatusTracker() EndpointSliceSyncStatusTracker {
	return &defaultEndpointSliceSyncStatusTracker{}
}
