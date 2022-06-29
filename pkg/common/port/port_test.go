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

func TestCompareServicePorts(t *testing.T) {
	appProtocolA := "app-protocol-a"
	appProtocolB := "app-protocol-a"
	tests := []struct {
		name string
		a    []corev1.ServicePort
		b    []corev1.ServicePort
		want bool
	}{
		{
			name: "exact equal",
			a: []corev1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:        "portB",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			b: []corev1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolB,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:        "portB",
					Protocol:    "TCP",
					AppProtocol: &appProtocolB,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			want: true,
		},
		{
			name: "equal with different order",
			a: []corev1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:        "portB",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			b: []corev1.ServicePort{
				{
					Name:        "portB",
					Protocol:    "TCP",
					AppProtocol: &appProtocolB,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolB,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			want: true,
		},
		{
			name: "having different length",
			a: []corev1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			b: []corev1.ServicePort{
				{
					Name:        "portB",
					Protocol:    "TCP",
					AppProtocol: &appProtocolB,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolB,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
		},
		{
			name: "same length with different value",
			a: []corev1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:        "portB",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			b: []corev1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:        "portB",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "target"},
				},
			},
		},
		{
			name: "same length with different port name",
			a: []corev1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:        "portB",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			b: []corev1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:        "portC",
					Protocol:    "TCP",
					AppProtocol: &appProtocolA,
					Port:        8080,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CompareServicePorts(tc.a, tc.b)
			if got != tc.want {
				t.Fatalf("CompareServicePorts() = %v, want %v", got, tc.want)
			}
		})
	}
}
