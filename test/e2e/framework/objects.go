package framework

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// Namespace returns a Namespace object.
func Namespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

// ClusterIPServiceWithNoSelector returns a Cluster IP type Service object with no selector specified.
func ClusterIPServiceWithNoSelector(namespace, name, portName string, port, targetPort int) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       portName,
					Port:       int32(port),
					TargetPort: intstr.FromInt(targetPort),
				},
			},
		},
	}
}

// ServiceExport returns a ServiceExport object.
func ServiceExport(namespace, name string) *fleetnetv1alpha1.ServiceExport {
	return &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
}

// ManuallyManagedIPv4EndpointSlice returns a manually managed EndpointSlice object.
func ManuallyManagedIPv4EndpointSlice(namespace, name, svcName, portName string, port int32, addresses []string) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: svcName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{
				Name: &portName,
				Port: &port,
			},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: addresses,
			},
		},
	}
}

// MultiClusterService returns a MultiClusterService object.
func MultiClusterService(namespace, name, svcName string) *fleetnetv1alpha1.MultiClusterService {
	return &fleetnetv1alpha1.MultiClusterService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
			ServiceImport: fleetnetv1alpha1.ServiceImportRef{
				Name: svcName,
			},
		},
	}
}
