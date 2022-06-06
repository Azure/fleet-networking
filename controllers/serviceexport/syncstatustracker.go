package serviceexport

// TO-DO (chenyu1): Refactor this utility with generics when Go 1.18 becomes available

type SvcSyncStatusTracker interface{}

type svcSyncStatusTrackerImpl struct{}

func NewSvcSyncStatusTracker() SvcSyncStatusTracker {
	return &svcSyncStatusTrackerImpl{}
}

type EndpointSliceSyncStatusTracker interface{}

type endpointSliceSyncStatusTrackerImpl struct{}

func NewEndpointSliceSyncStatusTracker() EndpointSliceSyncStatusTracker {
	return &endpointSliceSyncStatusTrackerImpl{}
}
