package serviceexport

// defaultSvcSyncStatusTracker is the default implementation of SvcSyncStatusTracker.
type defaultSvcSyncStatusTracker struct{}

// NewSvcSyncStatusTracker returns a SvcSyncStatusTracker for tracking Service synchronization status.
func NewSvcSyncStatusTracker() SvcSyncStatusTracker {
	return &defaultSvcSyncStatusTracker{}
}

// defaultEndpointSliceSyncStatusTracker is the default implementation of EndpointSliceSyncStatusTracker.
type defaultEndpointSliceSyncStatusTracker struct{}

// NewEndpointSliceSyncStatusTracker returns an EndpointSliceSyncStatusTracker for tracking EndpointSlice
// synchronization status.
func NewEndpointSliceSyncStatusTracker() EndpointSliceSyncStatusTracker {
	return &defaultEndpointSliceSyncStatusTracker{}
}
