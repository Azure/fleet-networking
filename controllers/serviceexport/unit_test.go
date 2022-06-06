package serviceexport_test

import (
	"testing"
	"time"

	"go.goms.io/fleet-networking/controllers/serviceexport"
	"k8s.io/client-go/dynamic/dynamicinformer"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestNewController(t *testing.T) {
	memberKubeClient := fakeclientset.NewSimpleClientset()
	memberDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme.Scheme)
	hubDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme.Scheme)

	memberSharedInformerFactory := informers.NewSharedInformerFactory(memberKubeClient, time.Second*30)
	memberDynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(memberDynamicClient, time.Second*30)
	hubDynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(hubDynamicClient, time.Second*30)

	_, err := serviceexport.New("fake-cluster", memberKubeClient, memberDynamicClient, hubDynamicClient,
		memberSharedInformerFactory, memberDynamicInformerFactory, hubDynamicInformerFactory)

	if err != nil {
		t.Errorf("failed to create a new serviceexport controller: %v", err)
	}
}
