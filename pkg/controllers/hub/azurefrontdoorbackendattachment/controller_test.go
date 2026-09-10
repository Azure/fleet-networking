/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package azurefrontdoorbackendattachment

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestReconcile(t *testing.T) {
	now := metav1.NewTime(time.Now())
	later := metav1.NewTime(now.Add(time.Minute))

	tests := []struct {
		name             string
		objects          []client.Object
		attachmentName   string
		wantAccepted     metav1.ConditionStatus
		wantReason       string
		wantResolvedRefs metav1.ConditionStatus
	}{
		{
			name: "accept valid attachment",
			objects: validObjects(
				newAttachment("attachment", "attachment-uid", now),
			),
			attachmentName:   "attachment",
			wantAccepted:     metav1.ConditionTrue,
			wantReason:       fleetnetv1alpha1.AzureFrontDoorReasonAccepted,
			wantResolvedRefs: metav1.ConditionTrue,
		},
		{
			name: "accept explicit TCP service port",
			objects: []client.Object{
				newAttachment("attachment", "attachment-uid", now),
				newGateway(),
				newServiceImportWithProtocol(443, corev1.ProtocolTCP),
				newGatewayPolicy(fleetnetv1alpha1.AzureFrontDoorProfileSKUPremium),
			},
			attachmentName:   "attachment",
			wantAccepted:     metav1.ConditionTrue,
			wantReason:       fleetnetv1alpha1.AzureFrontDoorReasonAccepted,
			wantResolvedRefs: metav1.ConditionTrue,
		},
		{
			name: "reject attachment when Gateway is missing",
			objects: []client.Object{
				newAttachment("attachment", "attachment-uid", now),
				newServiceImport(),
				newGatewayPolicy(fleetnetv1alpha1.AzureFrontDoorProfileSKUPremium),
			},
			attachmentName:   "attachment",
			wantAccepted:     metav1.ConditionFalse,
			wantReason:       fleetnetv1alpha1.AzureFrontDoorReasonRefNotFound,
			wantResolvedRefs: metav1.ConditionFalse,
		},
		{
			name: "reject attachment when service port is missing",
			objects: []client.Object{
				newAttachment("attachment", "attachment-uid", now),
				newGateway(),
				newServiceImport(),
				newGatewayPolicy(fleetnetv1alpha1.AzureFrontDoorProfileSKUPremium),
			},
			attachmentName:   "attachment",
			wantAccepted:     metav1.ConditionFalse,
			wantReason:       fleetnetv1alpha1.AzureFrontDoorReasonInvalidPort,
			wantResolvedRefs: metav1.ConditionFalse,
		},
		{
			name: "reject UDP service port",
			objects: []client.Object{
				newAttachment("attachment", "attachment-uid", now),
				newGateway(),
				newServiceImportWithProtocol(443, corev1.ProtocolUDP),
				newGatewayPolicy(fleetnetv1alpha1.AzureFrontDoorProfileSKUPremium),
			},
			attachmentName:   "attachment",
			wantAccepted:     metav1.ConditionFalse,
			wantReason:       fleetnetv1alpha1.AzureFrontDoorReasonInvalidPort,
			wantResolvedRefs: metav1.ConditionFalse,
		},
		{
			name: "reject SCTP service port",
			objects: []client.Object{
				newAttachment("attachment", "attachment-uid", now),
				newGateway(),
				newServiceImportWithProtocol(443, corev1.ProtocolSCTP),
				newGatewayPolicy(fleetnetv1alpha1.AzureFrontDoorProfileSKUPremium),
			},
			attachmentName:   "attachment",
			wantAccepted:     metav1.ConditionFalse,
			wantReason:       fleetnetv1alpha1.AzureFrontDoorReasonInvalidPort,
			wantResolvedRefs: metav1.ConditionFalse,
		},
		{
			name: "reject newer duplicate",
			objects: validObjects(
				newAttachment("winner", "winner-uid", now),
				newAttachment("attachment", "attachment-uid", later),
			),
			attachmentName:   "attachment",
			wantAccepted:     metav1.ConditionFalse,
			wantReason:       fleetnetv1alpha1.AzureFrontDoorReasonConflicted,
			wantResolvedRefs: metav1.ConditionTrue,
		},
		{
			name: "keep the existing attachment accepted when a duplicate appears",
			objects: validObjects(
				newAttachment("attachment", "winner-uid", now),
				newAttachment("duplicate", "duplicate-uid", later),
			),
			attachmentName:   "attachment",
			wantAccepted:     metav1.ConditionTrue,
			wantReason:       fleetnetv1alpha1.AzureFrontDoorReasonAccepted,
			wantResolvedRefs: metav1.ConditionTrue,
		},
		{
			name: "reject Private Link with Standard profile",
			objects: []client.Object{
				newPrivateLinkAttachment("attachment", "attachment-uid", now),
				newGateway(),
				newServiceImport(443),
				newGatewayPolicy(fleetnetv1alpha1.AzureFrontDoorProfileSKUStandard),
			},
			attachmentName:   "attachment",
			wantAccepted:     metav1.ConditionFalse,
			wantReason:       fleetnetv1alpha1.AzureFrontDoorReasonUnsupportedConfig,
			wantResolvedRefs: metav1.ConditionTrue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := testScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&fleetnetv1alpha1.AzureFrontDoorBackendAttachment{}).
				WithObjects(tt.objects...).
				Build()
			reconciler := &Reconciler{
				Client:   fakeClient,
				Recorder: record.NewFakeRecorder(10),
			}

			_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: tt.attachmentName},
			})
			if err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}

			attachment := &fleetnetv1alpha1.AzureFrontDoorBackendAttachment{}
			if err := fakeClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: tt.attachmentName}, attachment); err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			accepted := meta.FindStatusCondition(attachment.Status.Conditions, fleetnetv1alpha1.AzureFrontDoorConditionAccepted)
			if accepted == nil {
				t.Fatal("Accepted condition is missing")
			}
			if accepted.Status != tt.wantAccepted || accepted.Reason != tt.wantReason {
				t.Errorf("Accepted condition = (%s, %s), want (%s, %s)", accepted.Status, accepted.Reason, tt.wantAccepted, tt.wantReason)
			}
			resolvedRefs := meta.FindStatusCondition(attachment.Status.Conditions, fleetnetv1alpha1.AzureFrontDoorConditionResolvedRefs)
			if resolvedRefs == nil || resolvedRefs.Status != tt.wantResolvedRefs {
				t.Errorf("ResolvedRefs condition = %#v, want status %s", resolvedRefs, tt.wantResolvedRefs)
			}
			if len(attachment.Finalizers) != 0 {
				t.Errorf("finalizers = %v, want none for read-only controller", attachment.Finalizers)
			}
		})
	}
}

func validObjects(attachments ...*fleetnetv1alpha1.AzureFrontDoorBackendAttachment) []client.Object {
	objects := []client.Object{
		newGateway(),
		newServiceImport(443),
		newGatewayPolicy(fleetnetv1alpha1.AzureFrontDoorProfileSKUPremium),
	}
	for _, attachment := range attachments {
		objects = append(objects, attachment)
	}
	return objects
}

const testNamespace = "app"

func newGateway() *gatewayv1.Gateway {
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "global",
			UID:       "gateway-uid",
		},
	}
}

func newServiceImport(ports ...int32) *fleetnetv1alpha1.ServiceImport {
	servicePorts := make([]fleetnetv1alpha1.ServicePort, 0, len(ports))
	for _, port := range ports {
		servicePorts = append(servicePorts, fleetnetv1alpha1.ServicePort{Port: port})
	}
	return &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "store",
			UID:       "service-import-uid",
		},
		Status: fleetnetv1alpha1.ServiceImportStatus{Ports: servicePorts},
	}
}

func newServiceImportWithProtocol(port int32, protocol corev1.Protocol) *fleetnetv1alpha1.ServiceImport {
	serviceImport := newServiceImport(port)
	serviceImport.Status.Ports[0].Protocol = protocol
	return serviceImport
}

func newGatewayPolicy(sku fleetnetv1alpha1.AzureFrontDoorProfileSKU) *fleetnetv1alpha1.AzureFrontDoorGatewayPolicy {
	return &fleetnetv1alpha1.AzureFrontDoorGatewayPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "global",
		},
		Spec: fleetnetv1alpha1.AzureFrontDoorGatewayPolicySpec{
			TargetRef: gatewayv1.LocalObjectReference{
				Group: gatewayv1.Group(gatewayv1.GroupName),
				Kind:  gatewayv1.Kind("Gateway"),
				Name:  "global",
			},
			Profile: fleetnetv1alpha1.AzureFrontDoorProfileSpec{
				Mode:          fleetnetv1alpha1.AzureFrontDoorProfileModeManaged,
				SKU:           sku,
				ResourceGroup: "fleet-global",
				Name:          "fleet-global",
			},
			WAF: fleetnetv1alpha1.AzureFrontDoorWAFSpec{
				Required:         ptr.To(true),
				PolicyResourceID: "/subscriptions/sub/resourceGroups/security/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies/fleet-waf",
			},
		},
	}
}

func newAttachment(name string, uid types.UID, creationTime metav1.Time) *fleetnetv1alpha1.AzureFrontDoorBackendAttachment {
	return &fleetnetv1alpha1.AzureFrontDoorBackendAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         testNamespace,
			Name:              name,
			UID:               uid,
			CreationTimestamp: creationTime,
		},
		Spec: fleetnetv1alpha1.AzureFrontDoorBackendAttachmentSpec{
			GatewayRef: gatewayv1.LocalObjectReference{
				Group: gatewayv1.Group(gatewayv1.GroupName),
				Kind:  gatewayv1.Kind("Gateway"),
				Name:  "global",
			},
			BackendRef: fleetnetv1alpha1.AzureFrontDoorBackendReference{
				LocalObjectReference: gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group),
					Kind:  "ServiceImport",
					Name:  "store",
				},
				Port: 443,
			},
			Connectivity: fleetnetv1alpha1.AzureFrontDoorConnectivitySpec{
				Mode: fleetnetv1alpha1.AzureFrontDoorConnectivityModePublic,
			},
			Origin: fleetnetv1alpha1.AzureFrontDoorOriginSpec{
				Protocol:                    fleetnetv1alpha1.AzureFrontDoorOriginProtocolHTTPS,
				HostHeader:                  "store.internal.contoso.example",
				CertificateSubjectNameCheck: ptr.To(true),
			},
			HealthProbe: fleetnetv1alpha1.AzureFrontDoorHealthProbeSpec{
				Protocol:                  fleetnetv1alpha1.AzureFrontDoorOriginProtocolHTTPS,
				Method:                    fleetnetv1alpha1.AzureFrontDoorHealthProbeMethodHEAD,
				Path:                      "/healthz",
				IntervalSeconds:           30,
				SampleSize:                4,
				SuccessfulSamplesRequired: 3,
			},
			Traffic: fleetnetv1alpha1.AzureFrontDoorTrafficSpec{
				DefaultPriority: 1,
				DefaultWeight:   1000,
			},
			MemberFailurePolicy: fleetnetv1alpha1.AzureFrontDoorMemberFailurePolicyPartial,
		},
	}
}

func newPrivateLinkAttachment(name string, uid types.UID, creationTime metav1.Time) *fleetnetv1alpha1.AzureFrontDoorBackendAttachment {
	attachment := newAttachment(name, uid, creationTime)
	attachment.Spec.Connectivity = fleetnetv1alpha1.AzureFrontDoorConnectivitySpec{
		Mode: fleetnetv1alpha1.AzureFrontDoorConnectivityModePrivateLink,
		PrivateLink: &fleetnetv1alpha1.AzureFrontDoorPrivateLinkSpec{
			Approval:        "Manual",
			RegionSelection: "ClosestSupported",
		},
	}
	return attachment
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatalf("gatewayv1.Install() error = %v", err)
	}
	if err := fleetnetv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("fleetnetv1alpha1.AddToScheme() error = %v", err)
	}
	return scheme
}
