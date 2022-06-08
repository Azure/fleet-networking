package serviceexport

import (
	"testing"
	"time"

	"k8s.io/client-go/dynamic/dynamicinformer"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

// TestNewController calls serviceexport.New() with fake Kubernetes clients, checking for an error.
func TestNewController(t *testing.T) {
	memberKubeClient := fakeclientset.NewSimpleClientset()
	memberDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme.Scheme)
	hubDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme.Scheme)

	memberSharedInformerFactory := informers.NewSharedInformerFactory(memberKubeClient, time.Second*30)
	memberDynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(memberDynamicClient, time.Second*30)
	hubDynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(hubDynamicClient, time.Second*30)

	fakeClusterID := "fake-cluster"

	c, err := New(fakeClusterID, memberKubeClient, memberDynamicClient, hubDynamicClient,
		memberSharedInformerFactory, memberDynamicInformerFactory, hubDynamicInformerFactory)

	if c.memberClusterID != "fake-cluster" {
		t.Errorf("member cluster id does not match: got %s, expected %s", c.memberClusterID, fakeClusterID)
	}
	if c.memberKubeClient != memberKubeClient {
		t.Errorf("member kube client does not match")
	}
	if c.memberDynamicClient != memberDynamicClient {
		t.Errorf("member dynamic client does not match")
	}
	if c.hubDynamicClient != hubDynamicClient {
		t.Errorf("hub dynamic client does not match")
	}
	if err != nil {
		t.Errorf("failed to create a new serviceexport controller: %v", err)
	}
}
