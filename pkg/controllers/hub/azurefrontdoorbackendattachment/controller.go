/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package azurefrontdoorbackendattachment validates AFD backend attachments without writing Azure resources.
package azurefrontdoorbackendattachment

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet/pkg/utils/controller"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	// ControllerName is the name of the AFD backend attachment controller.
	ControllerName = "azurefrontdoorbackendattachment-controller"

	gatewayRefField   = ".spec.gatewayRef.name"
	backendRefField   = ".spec.backendRef.name"
	policyTargetField = ".spec.targetRef.name"

	eventReasonAccepted = "Accepted"
	eventReasonRejected = "Rejected"
)

// Reconciler validates AzureFrontDoorBackendAttachment dependencies and publishes status.
type Reconciler struct {
	client.Client
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=azurefrontdoorbackendattachments,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=azurefrontdoorbackendattachments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=azurefrontdoorgatewaypolicies,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile resolves an attachment and records whether a later Azure-writing controller may consume it.
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	startTime := time.Now()
	attachmentRef := klog.KRef(req.Namespace, req.Name)
	klog.V(2).InfoS("Reconciliation starts", "azureFrontDoorBackendAttachment", attachmentRef)
	defer func() {
		klog.V(2).InfoS("Reconciliation ends", "azureFrontDoorBackendAttachment", attachmentRef, "latency", time.Since(startTime).Milliseconds())
	}()

	attachment := &fleetnetv1alpha1.AzureFrontDoorBackendAttachment{}
	if err := r.Get(ctx, req.NamespacedName, attachment); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("Ignoring NotFound AzureFrontDoorBackendAttachment", "azureFrontDoorBackendAttachment", attachmentRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get AzureFrontDoorBackendAttachment", "azureFrontDoorBackendAttachment", attachmentRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	original := attachment.DeepCopy()
	if err := r.resolve(ctx, attachment); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.updateStatus(ctx, original, attachment)
}

func (r *Reconciler) resolve(ctx context.Context, attachment *fleetnetv1alpha1.AzureFrontDoorBackendAttachment) error {
	attachment.Status.Gateway = nil
	attachment.Status.Backend = nil

	if !hasSupportedReferences(attachment) {
		setAttachmentConditions(attachment, metav1.ConditionFalse, fleetnetv1alpha1.AzureFrontDoorReasonUnsupportedRef,
			"GatewayRef must reference a Gateway and backendRef must reference a ServiceImport", false)
		return nil
	}

	gateway := &gatewayv1.Gateway{}
	gatewayKey := types.NamespacedName{Namespace: attachment.Namespace, Name: string(attachment.Spec.GatewayRef.Name)}
	if err := r.Get(ctx, gatewayKey, gateway); err != nil {
		if apierrors.IsNotFound(err) {
			setAttachmentConditions(attachment, metav1.ConditionFalse, fleetnetv1alpha1.AzureFrontDoorReasonRefNotFound,
				fmt.Sprintf("Gateway %q was not found", gatewayKey.Name), false)
			return nil
		}
		klog.ErrorS(err, "Failed to get Gateway", "azureFrontDoorBackendAttachment", klog.KObj(attachment), "gateway", gatewayKey)
		return controller.NewAPIServerError(true, err)
	}
	attachment.Status.Gateway = resolvedReference(gateway)

	serviceImport := &fleetnetv1alpha1.ServiceImport{}
	backendKey := types.NamespacedName{Namespace: attachment.Namespace, Name: string(attachment.Spec.BackendRef.Name)}
	if err := r.Get(ctx, backendKey, serviceImport); err != nil {
		if apierrors.IsNotFound(err) {
			setAttachmentConditions(attachment, metav1.ConditionFalse, fleetnetv1alpha1.AzureFrontDoorReasonRefNotFound,
				fmt.Sprintf("ServiceImport %q was not found", backendKey.Name), false)
			return nil
		}
		klog.ErrorS(err, "Failed to get ServiceImport", "azureFrontDoorBackendAttachment", klog.KObj(attachment), "serviceImport", backendKey)
		return controller.NewAPIServerError(true, err)
	}
	attachment.Status.Backend = resolvedReference(serviceImport)

	if !serviceImportHasTCPPort(serviceImport, attachment.Spec.BackendRef.Port) {
		setAttachmentConditions(attachment, metav1.ConditionFalse, fleetnetv1alpha1.AzureFrontDoorReasonInvalidPort,
			fmt.Sprintf("ServiceImport %q does not expose TCP port %d", backendKey.Name, attachment.Spec.BackendRef.Port), false)
		return nil
	}

	winner, err := r.attachmentWinner(ctx, attachment)
	if err != nil {
		return err
	}
	if winner.UID != attachment.UID {
		setAttachmentConditions(attachment, metav1.ConditionFalse, fleetnetv1alpha1.AzureFrontDoorReasonConflicted,
			fmt.Sprintf("Attachment %q has precedence for this Gateway, ServiceImport, and port", winner.Name), true)
		return nil
	}

	policy, reason, message, err := r.resolveGatewayPolicy(ctx, attachment)
	if err != nil {
		return err
	}
	if policy == nil {
		setAttachmentConditions(attachment, metav1.ConditionFalse, reason, message, true)
		return nil
	}
	if attachment.Spec.Connectivity.Mode == fleetnetv1alpha1.AzureFrontDoorConnectivityModePrivateLink &&
		policy.Spec.Profile.SKU != fleetnetv1alpha1.AzureFrontDoorProfileSKUPremium {
		setAttachmentConditions(attachment, metav1.ConditionFalse, fleetnetv1alpha1.AzureFrontDoorReasonUnsupportedConfig,
			"Private Link requires a Premium_AzureFrontDoor profile", true)
		return nil
	}

	setAttachmentConditions(attachment, metav1.ConditionTrue, fleetnetv1alpha1.AzureFrontDoorReasonAccepted,
		"Attachment references and configuration are valid", true)
	return nil
}

func hasSupportedReferences(attachment *fleetnetv1alpha1.AzureFrontDoorBackendAttachment) bool {
	return attachment.Spec.GatewayRef.Group == gatewayv1.Group(gatewayv1.GroupName) &&
		attachment.Spec.GatewayRef.Kind == gatewayv1.Kind("Gateway") &&
		string(attachment.Spec.BackendRef.Group) == fleetnetv1alpha1.GroupVersion.Group &&
		attachment.Spec.BackendRef.Kind == gatewayv1.Kind("ServiceImport")
}

func resolvedReference(object client.Object) *fleetnetv1alpha1.AzureFrontDoorResolvedReference {
	return &fleetnetv1alpha1.AzureFrontDoorResolvedReference{
		Name: object.GetName(),
		UID:  object.GetUID(),
	}
}

func serviceImportHasTCPPort(serviceImport *fleetnetv1alpha1.ServiceImport, port int32) bool {
	for i := range serviceImport.Status.Ports {
		servicePort := serviceImport.Status.Ports[i]
		if servicePort.Port == port && (servicePort.Protocol == "" || servicePort.Protocol == corev1.ProtocolTCP) {
			return true
		}
	}
	return false
}

func (r *Reconciler) attachmentWinner(ctx context.Context, attachment *fleetnetv1alpha1.AzureFrontDoorBackendAttachment) (*fleetnetv1alpha1.AzureFrontDoorBackendAttachment, error) {
	attachments := &fleetnetv1alpha1.AzureFrontDoorBackendAttachmentList{}
	if err := r.List(ctx, attachments, client.InNamespace(attachment.Namespace)); err != nil {
		klog.ErrorS(err, "Failed to list AzureFrontDoorBackendAttachments", "azureFrontDoorBackendAttachment", klog.KObj(attachment))
		return nil, controller.NewAPIServerError(true, err)
	}

	candidates := make([]*fleetnetv1alpha1.AzureFrontDoorBackendAttachment, 0, len(attachments.Items))
	for i := range attachments.Items {
		candidate := &attachments.Items[i]
		if candidate.Spec.GatewayRef == attachment.Spec.GatewayRef &&
			candidate.Spec.BackendRef == attachment.Spec.BackendRef {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if !left.CreationTimestamp.Time.Equal(right.CreationTimestamp.Time) {
			return left.CreationTimestamp.Time.Before(right.CreationTimestamp.Time)
		}
		if left.UID != right.UID {
			return string(left.UID) < string(right.UID)
		}
		return left.Name < right.Name
	})
	if len(candidates) == 0 {
		return nil, controller.NewUnexpectedBehaviorError(fmt.Errorf("attachment %s was absent from its namespace list", klog.KObj(attachment)))
	}
	return candidates[0], nil
}

func (r *Reconciler) resolveGatewayPolicy(
	ctx context.Context,
	attachment *fleetnetv1alpha1.AzureFrontDoorBackendAttachment,
) (*fleetnetv1alpha1.AzureFrontDoorGatewayPolicy, string, string, error) {
	policies := &fleetnetv1alpha1.AzureFrontDoorGatewayPolicyList{}
	if err := r.List(ctx, policies, client.InNamespace(attachment.Namespace)); err != nil {
		klog.ErrorS(err, "Failed to list AzureFrontDoorGatewayPolicies", "azureFrontDoorBackendAttachment", klog.KObj(attachment))
		return nil, "", "", controller.NewAPIServerError(true, err)
	}

	matches := make([]*fleetnetv1alpha1.AzureFrontDoorGatewayPolicy, 0, 1)
	for i := range policies.Items {
		policy := &policies.Items[i]
		if policy.Spec.TargetRef.Group == gatewayv1.Group(gatewayv1.GroupName) &&
			policy.Spec.TargetRef.Kind == gatewayv1.Kind("Gateway") &&
			policy.Spec.TargetRef.Name == attachment.Spec.GatewayRef.Name {
			matches = append(matches, policy)
		}
	}
	if len(matches) == 0 {
		return nil, fleetnetv1alpha1.AzureFrontDoorReasonRefNotFound,
			fmt.Sprintf("No AzureFrontDoorGatewayPolicy targets Gateway %q", attachment.Spec.GatewayRef.Name), nil
	}
	if len(matches) > 1 {
		return nil, fleetnetv1alpha1.AzureFrontDoorReasonConflicted,
			fmt.Sprintf("Multiple AzureFrontDoorGatewayPolicies target Gateway %q", attachment.Spec.GatewayRef.Name), nil
	}
	return matches[0], "", "", nil
}

func setAttachmentConditions(
	attachment *fleetnetv1alpha1.AzureFrontDoorBackendAttachment,
	acceptedStatus metav1.ConditionStatus,
	acceptedReason, message string,
	resolved bool,
) {
	resolvedStatus := metav1.ConditionFalse
	resolvedReason := acceptedReason
	resolvedMessage := message
	if resolved {
		resolvedStatus = metav1.ConditionTrue
		resolvedReason = fleetnetv1alpha1.AzureFrontDoorReasonAccepted
		resolvedMessage = "Gateway and ServiceImport references are valid"
	}
	meta.SetStatusCondition(&attachment.Status.Conditions, metav1.Condition{
		Type:               fleetnetv1alpha1.AzureFrontDoorConditionResolvedRefs,
		Status:             resolvedStatus,
		ObservedGeneration: attachment.Generation,
		Reason:             resolvedReason,
		Message:            resolvedMessage,
	})
	meta.SetStatusCondition(&attachment.Status.Conditions, metav1.Condition{
		Type:               fleetnetv1alpha1.AzureFrontDoorConditionAccepted,
		Status:             acceptedStatus,
		ObservedGeneration: attachment.Generation,
		Reason:             acceptedReason,
		Message:            message,
	})
	meta.SetStatusCondition(&attachment.Status.Conditions, metav1.Condition{
		Type:               fleetnetv1alpha1.AzureFrontDoorConditionProgrammed,
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: attachment.Generation,
		Reason:             fleetnetv1alpha1.AzureFrontDoorReasonPending,
		Message:            "Azure resource programming is not enabled in the foundation release",
	})
}

func (r *Reconciler) updateStatus(
	ctx context.Context,
	original, attachment *fleetnetv1alpha1.AzureFrontDoorBackendAttachment,
) error {
	if equality.Semantic.DeepEqual(original.Status, attachment.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, attachment, client.MergeFrom(original)); err != nil {
		klog.ErrorS(err, "Failed to update AzureFrontDoorBackendAttachment status", "azureFrontDoorBackendAttachment", klog.KObj(attachment))
		return controller.NewUpdateIgnoreConflictError(err)
	}

	accepted := meta.FindStatusCondition(attachment.Status.Conditions, fleetnetv1alpha1.AzureFrontDoorConditionAccepted)
	if accepted != nil && accepted.Status == metav1.ConditionTrue {
		r.Recorder.Event(attachment, corev1.EventTypeNormal, eventReasonAccepted, accepted.Message)
		klog.V(2).InfoS("Accepted AzureFrontDoorBackendAttachment", "azureFrontDoorBackendAttachment", klog.KObj(attachment))
	} else if accepted != nil {
		r.Recorder.Event(attachment, corev1.EventTypeWarning, eventReasonRejected, accepted.Message)
		klog.V(2).InfoS("Rejected AzureFrontDoorBackendAttachment", "azureFrontDoorBackendAttachment", klog.KObj(attachment), "reason", accepted.Reason)
	}
	return nil
}

// SetupWithManager registers indexes and watches for every dependency that can change attachment validity.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	indexes := []struct {
		object    client.Object
		field     string
		extractor client.IndexerFunc
	}{
		{
			object: &fleetnetv1alpha1.AzureFrontDoorBackendAttachment{},
			field:  gatewayRefField,
			extractor: func(object client.Object) []string {
				attachment := object.(*fleetnetv1alpha1.AzureFrontDoorBackendAttachment)
				return []string{string(attachment.Spec.GatewayRef.Name)}
			},
		},
		{
			object: &fleetnetv1alpha1.AzureFrontDoorBackendAttachment{},
			field:  backendRefField,
			extractor: func(object client.Object) []string {
				attachment := object.(*fleetnetv1alpha1.AzureFrontDoorBackendAttachment)
				return []string{string(attachment.Spec.BackendRef.Name)}
			},
		},
		{
			object: &fleetnetv1alpha1.AzureFrontDoorGatewayPolicy{},
			field:  policyTargetField,
			extractor: func(object client.Object) []string {
				policy := object.(*fleetnetv1alpha1.AzureFrontDoorGatewayPolicy)
				return []string{string(policy.Spec.TargetRef.Name)}
			},
		},
	}
	for _, index := range indexes {
		if err := mgr.GetFieldIndexer().IndexField(ctx, index.object, index.field, index.extractor); err != nil {
			klog.ErrorS(err, "Failed to create AFD field index", "field", index.field)
			return err
		}
	}

	generationChanged := builder.WithPredicates(predicate.GenerationChangedPredicate{})
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.AzureFrontDoorBackendAttachment{}, generationChanged).
		Watches(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(r.attachmentsForGateway), generationChanged).
		Watches(&fleetnetv1alpha1.ServiceImport{}, handler.EnqueueRequestsFromMapFunc(r.attachmentsForBackend)).
		Watches(&fleetnetv1alpha1.AzureFrontDoorGatewayPolicy{}, handler.EnqueueRequestsFromMapFunc(r.attachmentsForPolicy), generationChanged).
		Watches(&fleetnetv1alpha1.AzureFrontDoorBackendAttachment{}, handler.EnqueueRequestsFromMapFunc(r.attachmentsForPeer), generationChanged).
		Complete(r)
}

func (r *Reconciler) attachmentsForGateway(ctx context.Context, object client.Object) []reconcile.Request {
	return r.listAttachmentRequests(ctx, object.GetNamespace(), gatewayRefField, object.GetName())
}

func (r *Reconciler) attachmentsForBackend(ctx context.Context, object client.Object) []reconcile.Request {
	return r.listAttachmentRequests(ctx, object.GetNamespace(), backendRefField, object.GetName())
}

func (r *Reconciler) attachmentsForPolicy(ctx context.Context, object client.Object) []reconcile.Request {
	policy, ok := object.(*fleetnetv1alpha1.AzureFrontDoorGatewayPolicy)
	if !ok {
		return nil
	}
	return r.listAttachmentRequests(ctx, policy.Namespace, gatewayRefField, string(policy.Spec.TargetRef.Name))
}

func (r *Reconciler) attachmentsForPeer(ctx context.Context, object client.Object) []reconcile.Request {
	attachment, ok := object.(*fleetnetv1alpha1.AzureFrontDoorBackendAttachment)
	if !ok {
		return nil
	}
	requests := r.listAttachmentRequests(ctx, attachment.Namespace, gatewayRefField, string(attachment.Spec.GatewayRef.Name))
	filtered := requests[:0]
	for _, request := range requests {
		candidate := &fleetnetv1alpha1.AzureFrontDoorBackendAttachment{}
		if err := r.Get(ctx, request.NamespacedName, candidate); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to get peer AzureFrontDoorBackendAttachment", "azureFrontDoorBackendAttachment", request.NamespacedName)
			}
			continue
		}
		if candidate.Spec.BackendRef == attachment.Spec.BackendRef {
			filtered = append(filtered, request)
		}
	}
	return filtered
}

func (r *Reconciler) listAttachmentRequests(ctx context.Context, namespace, field, value string) []reconcile.Request {
	attachments := &fleetnetv1alpha1.AzureFrontDoorBackendAttachmentList{}
	if err := r.List(ctx, attachments, client.InNamespace(namespace), client.MatchingFields{field: value}); err != nil {
		klog.ErrorS(err, "Failed to list AzureFrontDoorBackendAttachments for dependency event", "namespace", namespace, "field", field, "value", value)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(attachments.Items))
	for i := range attachments.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&attachments.Items[i])})
	}
	return requests
}
