// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package v1alpha1

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestV1alpha1(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "V1alpha1 Suite")
}

var _ = Describe("v1alpha1 API", func() {
	Context("AKSCluster", func() {

		It("should be equal after DeepCopy", func() {
			cluster := &AKSCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "ns",
				},
				Spec: AKSClusterSpec{
					KubeConfigSecret: "secret",
					ResourceID:       "resourceID",
					ManagedCluster:   &ManagedCluster{},
				},
				Status: AKSClusterStatus{
					State:  "Active",
					Reason: "Succeed",
				},
			}

			newCluster := cluster.DeepCopy()
			Expect(cluster).To(Equal(newCluster))

			Expect(cluster.Spec.DeepCopy()).To(Equal(&newCluster.Spec))
			Expect(cluster.Status.DeepCopy()).To(Equal(&newCluster.Status))
			Expect(cluster).To(Equal(newCluster.DeepCopyObject()))
			clusterList := &AKSClusterList{Items: []AKSCluster{*cluster}}
			Expect(clusterList).To(Equal(clusterList.DeepCopy()))
			Expect(clusterList).To(Equal(clusterList.DeepCopyObject()))
		})

	})

	Context("ClusterSet", func() {

		It("should be equal after DeepCopy", func() {
			clusterSet := &ClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "ns",
				},
				Spec: ClusterSetSpec{
					Clusters: []string{"cluster1", "cluster2"},
				},
				Status: ClusterSetStatus{
					ClusterStatuses: []ClusterStatus{
						{
							Name:  "cluster1",
							State: "Active",
						},
						{
							Name:  "cluster2",
							State: "Active",
						},
					},
				},
			}

			newClusterSet := clusterSet.DeepCopy()
			Expect(clusterSet).To(Equal(newClusterSet))
			Expect(clusterSet.Spec.DeepCopy()).To(Equal(&newClusterSet.Spec))
			Expect(clusterSet.Status.DeepCopy()).To(Equal(&newClusterSet.Status))
			Expect(clusterSet).To(Equal(newClusterSet.DeepCopyObject()))
			clusterSetList := &ClusterSetList{Items: []ClusterSet{*clusterSet}}
			Expect(clusterSetList).To(Equal(clusterSetList.DeepCopy()))
			Expect(clusterSetList).To(Equal(clusterSetList.DeepCopyObject()))
		})

	})

	Context("GlobalService", func() {

		It("should be equal after DeepCopy", func() {
			globalService := &GlobalService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "ns",
				},
				Spec: GlobalServiceSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{"service": "web"},
					},
					Ports: []GlobalServicePort{
						{
							Name:       "web",
							Protocol:   "TCP",
							Port:       80,
							TargetPort: 8080,
						},
					},
					ClusterSet: "clusterSet",
				},
				Status: GlobalServiceStatus{
					VIP:   "1.2.3.4",
					State: "Succeeded",
					Endpoints: []GlobalEndpoint{
						{
							Cluster:   "test",
							Service:   "web",
							IP:        "2.3.4.5",
							Endpoints: []string{"10.240.0.3:8080"},
						},
					},
				},
			}

			newGlobalService := globalService.DeepCopy()
			Expect(globalService).To(Equal(newGlobalService))
			Expect(globalService.Spec.DeepCopy()).To(Equal(&newGlobalService.Spec))
			Expect(globalService.Status.DeepCopy()).To(Equal(&newGlobalService.Status))
			Expect(globalService).To(Equal(newGlobalService.DeepCopyObject()))
			globalServiceList := &GlobalServiceList{Items: []GlobalService{*globalService}}
			Expect(globalServiceList).To(Equal(globalServiceList.DeepCopy()))
			Expect(globalServiceList).To(Equal(globalServiceList.DeepCopyObject()))
		})

	})
})
