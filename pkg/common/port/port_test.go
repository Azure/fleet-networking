package port

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestToServicePort(t *testing.T) {
	appProtocol := "app-protocol"
	input := fleetnetv1alpha1.ServicePort{
		Name:        "svc-port",
		Protocol:    "TCP",
		AppProtocol: &appProtocol,
		Port:        8080,
		TargetPort:  intstr.IntOrString{StrVal: "8080"},
	}
	want := corev1.ServicePort{
		Name:        "svc-port",
		Protocol:    "TCP",
		AppProtocol: &appProtocol,
		Port:        8080,
		TargetPort:  intstr.IntOrString{StrVal: "8080"},
	}
	got := ToServicePort(input)
	if !cmp.Equal(got, want) {
		t.Errorf("ToServicePort(%+v) = %+v, want %+v", input, got, want)
	}
}
