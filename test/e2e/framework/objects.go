package framework

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
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
func ClusterIPServiceWithNoSelector(namespace, name, portName string, port, targetPort int32) *corev1.Service {
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
					Port:       port,
					TargetPort: intstr.FromInt32(targetPort),
				},
			},
		},
	}
}

// ServiceExport returns a ServiceExport object.
func ServiceExport(namespace, name string) *fleetnetv1beta1.ServiceExport {
	return &fleetnetv1beta1.ServiceExport{
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

// GatewayClass returns a GatewayClass object.
func GatewayClass(name string, controllerName gatewayv1.GatewayController) *gatewayv1.GatewayClass {
	return &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: controllerName,
		},
	}
}

// Gateway returns an HTTP Gateway object.
func Gateway(namespace, name, className, hostname string, annotations map[string]string) *gatewayv1.Gateway {
	listenerHostname := gatewayv1.Hostname(hostname)
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Annotations: annotations,
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(className),
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Hostname: &listenerHostname,
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				},
			},
		},
	}
}

// InternalServiceExport returns the hub-side export used to derive a Fleet ServiceImport.
func InternalServiceExport(namespace, name, serviceNamespace, serviceName, clusterID string, port int32) *fleetnetv1alpha1.InternalServiceExport {
	return &fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: fleetnetv1alpha1.InternalServiceExportSpec{
			Ports: []fleetnetv1alpha1.ServicePort{
				{
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
					Port:     port,
				},
			},
			ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       clusterID,
				APIVersion:      "v1",
				Kind:            "Service",
				Namespace:       serviceNamespace,
				Name:            serviceName,
				ResourceVersion: "1",
				Generation:      1,
				UID:             types.UID(name),
				NamespacedName:  types.NamespacedName{Namespace: serviceNamespace, Name: serviceName}.String(),
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

// HTTPRouteToFleetServiceImport returns an HTTPRoute with one Fleet ServiceImport backend.
func HTTPRouteToFleetServiceImport(
	namespace, name, gatewayName, backendNamespace, backendName string,
	port gatewayv1.PortNumber,
	weight int32,
) *gatewayv1.HTTPRoute {
	fleetGroup := gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group)
	serviceImportKind := gatewayv1.Kind("ServiceImport")
	backendRef := gatewayv1.BackendObjectReference{
		Group: &fleetGroup,
		Kind:  &serviceImportKind,
		Name:  gatewayv1.ObjectName(backendName),
		Port:  &port,
	}
	if backendNamespace != "" {
		ns := gatewayv1.Namespace(backendNamespace)
		backendRef.Namespace = &ns
	}

	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: gatewayv1.ObjectName(gatewayName)},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: backendRef,
								Weight:                 &weight,
							},
						},
					},
				},
			},
		},
	}
}

// ReferenceGrantForFleetServiceImport permits HTTPRoutes in routeNamespace to reference one Fleet ServiceImport.
func ReferenceGrantForFleetServiceImport(namespace, name, routeNamespace, serviceImportName string) *gatewayv1beta1.ReferenceGrant {
	serviceImportObjectName := gatewayv1beta1.ObjectName(serviceImportName)
	return &gatewayv1beta1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: gatewayv1beta1.ReferenceGrantSpec{
			From: []gatewayv1beta1.ReferenceGrantFrom{
				{
					Group:     gatewayv1beta1.Group(gatewayv1.GroupVersion.Group),
					Kind:      "HTTPRoute",
					Namespace: gatewayv1beta1.Namespace(routeNamespace),
				},
			},
			To: []gatewayv1beta1.ReferenceGrantTo{
				{
					Group: gatewayv1beta1.Group(fleetnetv1alpha1.GroupVersion.Group),
					Kind:  "ServiceImport",
					Name:  &serviceImportObjectName,
				},
			},
		},
	}
}
