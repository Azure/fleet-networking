/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

// Fleet is a collection of clusters for e2e tests.
type Fleet struct {
	memberClusters   []*Cluster
	mcsMemberCluster *Cluster
	hubCluster       *Cluster
}

// MemberClusters returns all member clusters.
func (c *Fleet) MemberClusters() []*Cluster {
	return c.memberClusters
}

// MCSMemberCluster returns a member cluster on which MCS will be hosted.
func (c *Fleet) MCSMemberCluster() *Cluster {
	return c.mcsMemberCluster
}

// HubCluster returns a hub cluster.
func (c *Fleet) HubCluster() *Cluster {
	return c.hubCluster
}

// Clusters returns all clusters including both member and hub.
func (c *Fleet) Clusters() []*Cluster {
	return append(c.memberClusters, c.hubCluster)
}

// NewFleet returns a collection of clusters for e2e tests.
func NewFleet(memberClusters []*Cluster, mcsMemberCluster, hubCluster *Cluster) *Fleet {
	return &Fleet{
		memberClusters:   memberClusters,
		mcsMemberCluster: mcsMemberCluster,
		hubCluster:       hubCluster,
	}
}
