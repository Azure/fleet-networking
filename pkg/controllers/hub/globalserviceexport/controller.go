// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package globalserviceexport

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armdeploymentstacks"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	acv1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/apiclient"
	ac "go.goms.io/fleet-networking/pkg/applyconfigurations/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/krtutil"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	// ControllerName is the name of the GlobalServiceExport controller.
	ControllerName = "globalserviceexport-controller"

	globalAnnotation        = "globalLBName"
	targetGatewayAnnotation = "targetGateway"
	fieldManagerName        = "globalserviceexport-controller"
	// statusFieldManagerName is used for all status-subresource Apply calls.
	// It is intentionally distinct from fieldManagerName (used for the
	// finalizer, a main-resource field): a field manager's Apply call
	// declares its complete set of owned fields, and reusing the same
	// manager name across the main resource and the status subresource
	// would cause a later status-only Apply to prune the finalizer that an
	// earlier main-resource Apply had set under that same manager.
	statusFieldManagerName = "globalserviceexport-controller-status"

	// deploymentWorkerCount is the number of worker goroutines processing
	// queued global load balancer deployments concurrently.
	deploymentWorkerCount = 4

	// deploymentFailedEventReason is the Reason used on the Warning event
	// emitted against a MultiClusterLoadBalancer when its Azure global load
	// balancer deployment fails.
	deploymentFailedEventReason = "GlobalLoadBalancerDeploymentFailed"
)

type parameters struct {
	Name          string `json:"name"`
	rg            string
	Backends      []string `json:"backends"`
	targetGateway string
	serviceName   string
	namespace     string
}

// ResourceName implements krt.ResourceNamer.
func (p parameters) ResourceName() string {
	return fmt.Sprintf("%s.%s", p.rg, p.Name)
}

var _ krt.ResourceNamer = parameters{}

type Reconciler struct {
	client apiclient.Client

	resourceGroupName string // default resource group name to create public IP address
	deploymentClient  *armdeploymentstacks.Client
	resourceClient    *armresources.Client

	// Recorder emits Kubernetes events against MultiClusterLoadBalancer
	// objects, e.g. to surface Azure deployment failures to users running
	// kubectl describe/get events.
	Recorder record.EventRecorder

	// mclbs lets deployment workers re-fetch the freshest MultiClusterLoadBalancer
	// state for a given key before applying status/annotations.
	mclbs krt.Collection[*v1alpha1.MultiClusterLoadBalancer]
	// deployments holds the prepared, validated deployment parameters for each
	// MultiClusterLoadBalancer. Its transform does no blocking work; instead
	// changes are pushed onto queue below for asynchronous processing.
	deployments krt.Collection[parameters]
	// queue holds the ResourceName keys (see parameters.ResourceName) of
	// deployments that need to be created/updated via writeDeployment. It
	// decouples the slow, blocking Azure deployment-stack calls from krt's
	// reactive transform functions.
	queue workqueue.TypedRateLimitingInterface[string]

	// lastValidationMu guards lastValidation.
	lastValidationMu sync.Mutex
	// lastValidation records, per MultiClusterLoadBalancer, a signature of
	// the inputs (validity, resource group, target gateway, and backend
	// list) for which this controller has already applied Valid/Invalid
	// status and ensured the finalizer is present. The deployments
	// transform below patches the very mclb object it watches (status and
	// finalizer), which would otherwise re-trigger the transform on every
	// status-only update it makes; recomputing whether to skip from the
	// (possibly stale, not-yet-propagated) live object would be unreliable.
	// Comparing against our own last-applied signature instead avoids
	// reading back our own writes, while still reapplying status whenever a
	// real input (e.g. a newly available public IP) changes the outcome.
	lastValidation map[types.NamespacedName]string
}

func NewReconciler(c apiclient.Client, dc *armdeploymentstacks.Client, rc *armresources.Client, defaultRG string, recorder record.EventRecorder) *Reconciler {
	ob := krtutil.NewKrtOptions(make(chan struct{}), new(krt.DebugHandler))

	mclbs := krt.WrapClient(kclient.New[*v1alpha1.MultiClusterLoadBalancer](c), ob.ToOptions("multiclusterloadbalancers")...)
	ises := krt.WrapClient(kclient.New[*v1alpha1.InternalServiceExport](c), ob.ToOptions("internalserviceexports")...)
	r := &Reconciler{
		client:            c,
		resourceClient:    rc,
		deploymentClient:  dc,
		resourceGroupName: defaultRG,
		Recorder:          recorder,
		mclbs:             mclbs,
		queue: workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[string](),
		),
		lastValidation: make(map[types.NamespacedName]string),
	}

	iseIndex := krt.NewIndex(ises, "internal service export by service reference name", func(export *v1alpha1.InternalServiceExport) []NameKey {
		return []NameKey{{
			Named: krt.Named{
				Name:      export.Spec.ServiceReference.Name,
				Namespace: export.Spec.ServiceReference.Namespace,
			},
		}}
	})

	// deployments validates each MultiClusterLoadBalancer and prepares the
	// Azure deployment-stack parameters for it. The transform intentionally
	// performs no blocking work (no ARM deployment calls): it only talks to
	// the k8s API (fast) and returns the desired parameters. The actual,
	// potentially long-running deployment is performed asynchronously by a
	// worker pool, driven off the RegisterBatch handler registered below.
	deployments := krt.NewCollection(mclbs, func(kctx krt.HandlerContext, mclb *v1alpha1.MultiClusterLoadBalancer) *parameters {
		nsName := types.NamespacedName{Namespace: mclb.Namespace, Name: mclb.Name}
		targetGateway := mclb.Annotations[targetGatewayAnnotation]
		internalServiceExports := krt.Fetch(kctx, ises, krt.FilterIndex(iseIndex, NewNameKey(mclb)))
		rg := strings.TrimSpace(mclb.Annotations[objectmeta.ServiceAnnotationLoadBalancerResourceGroup])
		if len(rg) < 1 {
			rg = r.resourceGroupName
		}
		if mclb.DeletionTimestamp != nil {
			err := r.deleteDeployment(mclb.Name, rg)
			if err != nil {
				klog.ErrorS(err, "Failed to delete global service deployment", "name", mclb.Name, "namespace", mclb.Namespace)
				kctx.DiscardResult()
				return nil
			}
			// remove finalizer
			r.RemoveFinalizer(mclb)
			if err != nil {
				klog.ErrorS(err, "Failed to remove mclb finalizer", "name", mclb.Name, "namespace", mclb.Namespace)
				kctx.DiscardResult()
			}
			r.forgetValidation(nsName)
			return nil
		}
		out := &parameters{
			Name:          fmt.Sprintf("%s-%s", mclb.Namespace, mclb.Name),
			rg:            rg,
			targetGateway: targetGateway,
			serviceName:   mclb.Name,
			namespace:     mclb.Namespace,
		}
		for _, ise := range internalServiceExports {
			// for each public ip, get frontend config id
			pip, err := r.resourceClient.GetByID(context.Background(), *ise.Spec.PublicIPResourceID, "2024-10-01", &armresources.ClientGetByIDOptions{})
			if err != nil {
				klog.ErrorS(err, "No Public IP found for InternalServiceExport", "name", ise.Spec.ServiceReference.NamespacedName)
				sig := "invalid|" + ise.Spec.ServiceReference.NamespacedName
				if r.shouldApplyValidation(nsName, sig) {
					r.ApplyStatusInvalid(mclb, ise.Spec.ServiceReference.NamespacedName)
				}
				kctx.DiscardResult()
				return nil
			}

			ipConfig := pip.Properties.(map[string]interface{})["ipConfiguration"].(map[string]interface{})
			cfgID, _ := ipConfig["id"].(string)
			out.Backends = append(out.Backends, cfgID)
		}
		sig := fmt.Sprintf("valid|rg=%s|gw=%s|backends=%s", rg, targetGateway, strings.Join(out.Backends, ","))
		if r.shouldApplyValidation(nsName, sig) {
			r.ApplyStatusValid(mclb)
			// Add finalizer if not present. This must run synchronously
			// (not in its own goroutine) so it cannot race with the worker
			// pool's later ApplyStatusDeployed call on the same object.
			r.ApplyFinalizer(mclb)
		}
		return out
	})
	r.deployments = deployments

	// enqueueDeployment pushes the ResourceName key of any added/updated
	// deployment parameters onto the queue, so a worker can perform the
	// (potentially slow) Azure deployment asynchronously. Delete events are
	// ignored: stack teardown is already handled synchronously above, in the
	// deletion branch of the transform.
	deployments.RegisterBatch(func(events []krt.Event[parameters]) {
		for _, e := range events {
			if e.Event == controllers.EventDelete {
				continue
			}
			r.queue.Add(e.Latest().ResourceName())
		}
	}, true)

	return r
}

// shouldApplyValidation reports whether the deployments transform should
// (re-)apply Valid/Invalid status and the finalizer for key, given sig, a
// signature of the inputs that drove that decision. It returns false (skip)
// only when sig matches the signature from the last time this method
// returned true for key, i.e. when nothing this controller cares about has
// actually changed since its own last write. See lastValidation's doc
// comment for why this is tracked in memory rather than read back from the
// (possibly stale) live object.
func (r *Reconciler) shouldApplyValidation(key types.NamespacedName, sig string) bool {
	r.lastValidationMu.Lock()
	defer r.lastValidationMu.Unlock()
	if r.lastValidation[key] == sig {
		return false
	}
	r.lastValidation[key] = sig
	return true
}

// forgetValidation clears any recorded signature for key, e.g. once it has
// been deleted, so a future recreation with the same name is revalidated
// from scratch.
func (r *Reconciler) forgetValidation(key types.NamespacedName) {
	r.lastValidationMu.Lock()
	defer r.lastValidationMu.Unlock()
	delete(r.lastValidation, key)
}

func (r *Reconciler) Start(ctx context.Context) error {
	for i := 0; i < deploymentWorkerCount; i++ {
		go r.runWorker(ctx)
	}
	go func() {
		<-ctx.Done()
		r.queue.ShutDown()
	}()

	r.client.RunAndWait(ctx.Done())
	return nil
}

// runWorker continually dequeues and processes deployment keys until the
// queue is shut down (see Start).
func (r *Reconciler) runWorker(ctx context.Context) {
	for r.processNextDeployment(ctx) {
	}
}

// processNextDeployment pops a single key off the queue and deploys it.
// It returns false once the queue has been shut down, signaling the worker
// to exit.
func (r *Reconciler) processNextDeployment(ctx context.Context) bool {
	key, shutdown := r.queue.Get()
	if shutdown {
		return false
	}
	defer r.queue.Done(key)
	defer r.queue.Forget(key)

	if err := r.deployForKey(ctx, key); err != nil {
		klog.ErrorS(err, "Failed to process queued global load balancer deployment", "key", key)
	}
	return true
}

// deployForKey looks up the freshest deployment parameters and
// MultiClusterLoadBalancer for key, performs the (blocking) Azure deployment,
// and applies the resulting status/annotations. It is safe to call
// concurrently from multiple workers for different keys.
func (r *Reconciler) deployForKey(ctx context.Context, key string) error {
	params := r.deployments.GetKey(key)
	if params == nil {
		// The deployment parameters no longer exist, e.g. the
		// MultiClusterLoadBalancer was deleted or became invalid since this
		// key was enqueued. Nothing to do.
		return nil
	}
	mclbPtr := r.mclbs.GetKey(fmt.Sprintf("%s/%s", params.namespace, params.serviceName))
	if mclbPtr == nil {
		return nil
	}
	mclb := *mclbPtr

	ipAddress, err := r.writeDeployment(*params)
	if err != nil {
		klog.ErrorS(err, "Failed to deploy global load balancer", "name", params.Name)
		r.ApplyStatusFailed(mclb)
		if r.Recorder != nil {
			r.Recorder.Eventf(mclb, corev1.EventTypeWarning, deploymentFailedEventReason,
				"Failed to deploy global load balancer %s: %v", params.Name, err)
		}
		return err
	}

	r.ApplyStatusDeployed(mclb, len(params.Backends), ipAddress)
	return r.annotateTarget(ctx, params.namespace, params.serviceName, params.targetGateway, ipAddress)
}

// annotateTarget patches the target Service (or Gateway, if targetGateway is
// set) with the resulting global anycast IP address.
func (r *Reconciler) annotateTarget(ctx context.Context, namespace, serviceName, targetGateway, ipAddress string) error {
	patchStr := fmt.Sprintf(`{"metadata":{"annotations":{"service.beta.kubernetes.io/azure-additional-public-ips": %q}}}`, ipAddress)
	var err error
	if targetGateway == "" {
		_, err = r.client.Kube().CoreV1().Services(namespace).Patch(ctx, serviceName, types.MergePatchType,
			[]byte(patchStr), v1.PatchOptions{})
	} else {
		_, err = r.client.GatewayAPI().GatewayV1beta1().Gateways(namespace).Patch(ctx, targetGateway, types.MergePatchType,
			[]byte(patchStr), v1.PatchOptions{})
	}
	if err != nil {
		klog.ErrorS(err, "Failed to annotate target", "name", serviceName, "gateway", targetGateway)
	}
	return err
}

func (r *Reconciler) deleteDeployment(name, rg string) error {
	p, err := r.deploymentClient.BeginDeleteAtResourceGroup(context.Background(), rg, name, nil)
	if err != nil {
		return err
	}
	_, err = p.PollUntilDone(context.Background(), nil)
	if respErr, ok := err.(*azcore.ResponseError); ok {
		if respErr.StatusCode == 404 {
			// already deleted
			return nil
		}
	}
	return err
}

func (r *Reconciler) writeDeployment(params parameters) (string, error) {
	template := make(map[string]interface{})
	if err := json.Unmarshal([]byte(templateInline), &template); err != nil {
		return "", err
	}
	foo := armdeploymentstacks.DenySettingsModeNone
	p, err := r.deploymentClient.BeginCreateOrUpdateAtResourceGroup(context.Background(), params.rg, params.Name, armdeploymentstacks.DeploymentStack{
		Properties: &armdeploymentstacks.DeploymentStackProperties{
			ActionOnUnmanage: &armdeploymentstacks.ActionOnUnmanage{
				Resources:        ptr.Of(armdeploymentstacks.DeploymentStacksDeleteDetachEnumDelete),
				ResourceGroups:   ptr.Of(armdeploymentstacks.DeploymentStacksDeleteDetachEnumDetach),
				ManagementGroups: ptr.Of(armdeploymentstacks.DeploymentStacksDeleteDetachEnumDetach),
			},
			DenySettings: &armdeploymentstacks.DenySettings{
				Mode: &foo,
			},
			Template: template,
			Parameters: map[string]*armdeploymentstacks.DeploymentParameter{
				"name": {
					Value: params.Name,
				},
				"backends": {
					Value: params.Backends,
				},
			},
		},
	}, nil)
	if err != nil {
		return "", err
	}
	res, err := p.PollUntilDone(context.Background(), nil)
	log.Default().Printf("Deployment result: %v\n", res)
	outputs := res.DeploymentStack.Properties.Outputs.(map[string]interface{})
	ipAddress := outputs["publicGlobalIPAddress"].(map[string]interface{})["value"].(string)
	return ipAddress, err
}

func (r *Reconciler) RemoveFinalizer(mclb *v1alpha1.MultiClusterLoadBalancer) error {
	index := -1
	for i, f := range mclb.Finalizers {
		if f == "mclb" {
			index = i
			break
		}
	}
	if index == -1 {
		// finalizer not found
		return nil
	}
	_, err := r.client.Networking().NetworkingV1alpha1().MultiClusterLoadBalancers(mclb.Namespace).Patch(
		context.Background(), mclb.Name, types.JSONPatchType, []byte(fmt.Sprintf("[ { \"op\": \"remove\", \"path\": \"/metadata/finalizers/%d\" } ]", index)),
		v1.PatchOptions{},
	)
	if err != nil {
		klog.ErrorS(err, "Failed to remove finalizer from MultiClusterLoadBalancer", "name", mclb.Name, "namespace", mclb.Namespace)
	}
	return err
}

func (r *Reconciler) ApplyFinalizer(mclb *v1alpha1.MultiClusterLoadBalancer) error {
	for _, f := range mclb.Finalizers {
		if f == "mclb" {
			// Already present; skip the Apply call to avoid re-triggering
			// our own watch (see statusUpToDate).
			return nil
		}
	}
	x := ac.MultiClusterLoadBalancer(mclb.Name, mclb.Namespace).
		WithFinalizers("mclb")
	_, err := r.client.Networking().NetworkingV1alpha1().MultiClusterLoadBalancers(mclb.Namespace).Apply(
		context.Background(),
		x,
		v1.ApplyOptions{
			FieldManager: fieldManagerName,
		},
	)
	if err != nil {
		klog.ErrorS(err, "Failed to apply finalizer to MultiClusterLoadBalancer", "name", mclb.Name, "namespace", mclb.Namespace)
	}
	return err
}

func conditionExists(conditions []v1.Condition, condType string, status *v1.ConditionStatus) (v1.Condition, bool) {
	for _, c := range conditions {
		if c.Type == condType && string(c.Status) == string(*status) {
			return c, true
		}
	}
	return v1.Condition{}, false
}

func (r *Reconciler) ApplyStatusDeployed(mclb *v1alpha1.MultiClusterLoadBalancer, i int, ipAddress string) {
	statusAC := ac.MultiClusterLoadBalancerStatus().WithLoadBalancer(corev1.LoadBalancerStatus{
		Ingress: []corev1.LoadBalancerIngress{
			{
				IP: ipAddress,
			},
		},
	})
	conds := processConditions(mclb.Status.Conditions, mclb.Generation, getValidCondition(), getDeployedCondition(i))
	if statusUpToDate(mclb.Status.Conditions, conds) && loadBalancerUpToDate(mclb.Status.LoadBalancer, ipAddress) {
		// Avoid patching the MultiClusterLoadBalancer (and re-triggering our
		// own watch) when the desired status is already in place.
		return
	}
	statusAC.WithConditions(conds...)
	_, err := r.client.Networking().NetworkingV1alpha1().MultiClusterLoadBalancers(mclb.Namespace).ApplyStatus(context.Background(),
		ac.MultiClusterLoadBalancer(mclb.Name, mclb.Namespace).WithStatus(
			statusAC,
		),
		v1.ApplyOptions{
			FieldManager: statusFieldManagerName,
		},
	)
	if err != nil {
		klog.ErrorS(err, "Failed to apply deployed status to MultiClusterLoadBalancer", "name", mclb.Name, "namespace", mclb.Namespace)
	}
}

func (r *Reconciler) ApplyStatusFailed(mclb *v1alpha1.MultiClusterLoadBalancer) {
	conds := processConditions(mclb.Status.Conditions, mclb.Generation, getInvalidCondition(mclb.Name), getDeploFailedCondition())
	if statusUpToDate(mclb.Status.Conditions, conds) {
		return
	}
	statusAC := ac.MultiClusterLoadBalancerStatus()
	statusAC.WithConditions(conds...)
	_, err := r.client.Networking().NetworkingV1alpha1().MultiClusterLoadBalancers(mclb.Namespace).ApplyStatus(context.Background(),
		ac.MultiClusterLoadBalancer(mclb.Name, mclb.Namespace).WithStatus(
			statusAC,
		),
		v1.ApplyOptions{
			FieldManager: statusFieldManagerName,
		},
	)
	if err != nil {
		klog.ErrorS(err, "Failed to apply failed status to MultiClusterLoadBalancer", "name", mclb.Name, "namespace", mclb.Namespace)
	}
}

func (r *Reconciler) ApplyStatusInvalid(mclb *v1alpha1.MultiClusterLoadBalancer, seName string) {
	conds := processConditions(mclb.Status.Conditions, mclb.Generation, getInvalidCondition(seName))
	if statusUpToDate(mclb.Status.Conditions, conds) {
		return
	}
	statusAC := ac.MultiClusterLoadBalancerStatus()
	statusAC.WithConditions(conds...)
	_, err := r.client.Networking().NetworkingV1alpha1().MultiClusterLoadBalancers(mclb.Namespace).ApplyStatus(context.Background(),
		ac.MultiClusterLoadBalancer(mclb.Name, mclb.Namespace).WithStatus(
			statusAC,
		),
		v1.ApplyOptions{
			FieldManager: statusFieldManagerName,
		},
	)
	if err != nil {
		klog.ErrorS(err, "Failed to apply invalid status to MultiClusterLoadBalancer", "name", mclb.Name, "namespace", mclb.Namespace)
	}
}

func (r *Reconciler) ApplyStatusValid(mclb *v1alpha1.MultiClusterLoadBalancer) {
	conds := processConditions(mclb.Status.Conditions, mclb.Generation, getValidCondition())
	if statusUpToDate(mclb.Status.Conditions, conds) {
		return
	}
	statusAC := ac.MultiClusterLoadBalancerStatus()
	statusAC.WithConditions(conds...)
	_, err := r.client.Networking().NetworkingV1alpha1().MultiClusterLoadBalancers(mclb.Namespace).ApplyStatus(context.Background(),
		ac.MultiClusterLoadBalancer(mclb.Name, mclb.Namespace).WithStatus(
			statusAC,
		),
		v1.ApplyOptions{
			FieldManager: statusFieldManagerName,
		},
	)
	if err != nil {
		klog.ErrorS(err, "Failed to apply valid status to MultiClusterLoadBalancer", "name", mclb.Name, "namespace", mclb.Namespace)
	}
}

// statusUpToDate reports whether applying conds would be a no-op given the
// existing conditions on the object. Each status Apply call patches the
// watched MultiClusterLoadBalancer, which would otherwise immediately
// re-trigger this controller's own transform function via the informer
// watch, causing an unthrottled self-triggering reconcile loop. Skipping
// no-op applies breaks that cycle.
func statusUpToDate(existing []v1.Condition, conds []*acv1.ConditionApplyConfiguration) bool {
	if len(existing) != len(conds) {
		return false
	}
	for _, cond := range conds {
		e, ok := conditionExists(existing, *cond.Type, cond.Status)
		if !ok {
			return false
		}
		if e.Reason != *cond.Reason || e.Message != *cond.Message || e.ObservedGeneration != *cond.ObservedGeneration {
			return false
		}
	}
	return true
}

// loadBalancerUpToDate reports whether the existing LoadBalancer status
// already has a single ingress entry with the given IP, i.e. whether
// applying it would be a no-op (see statusUpToDate).
func loadBalancerUpToDate(existing corev1.LoadBalancerStatus, ipAddress string) bool {
	return len(existing.Ingress) == 1 && existing.Ingress[0].IP == ipAddress
}

// processConditions returns the complete set of conditions this field
// manager should own after applying newConds: existing condition types not
// present in newConds are carried forward unchanged, and types present in
// newConds are updated/added. This is required because each ApplyStatus*
// call declares, via server-side apply, the COMPLETE list of conditions it
// owns; submitting only the conditions relevant to the call (e.g. just
// Valid) would prune any other condition type (e.g. Deployed) previously
// written by this same field manager.
func processConditions(existing []v1.Condition, generation int64, newConds ...*acv1.ConditionApplyConfiguration) []*acv1.ConditionApplyConfiguration {
	touched := make(map[string]bool, len(newConds))
	for _, cond := range newConds {
		touched[*cond.Type] = true
	}
	result := make([]*acv1.ConditionApplyConfiguration, 0, len(existing)+len(newConds))
	for _, e := range existing {
		if touched[e.Type] {
			continue
		}
		result = append(result, acv1.Condition().
			WithType(e.Type).
			WithStatus(e.Status).
			WithReason(e.Reason).
			WithMessage(e.Message).
			WithObservedGeneration(e.ObservedGeneration).
			WithLastTransitionTime(e.LastTransitionTime))
	}
	for _, cond := range newConds {
		cond.WithObservedGeneration(generation)
		if prior, exists := conditionExists(existing, *cond.Type, cond.Status); !exists {
			cond.WithLastTransitionTime(v1.Now())
		} else {
			cond.WithLastTransitionTime(prior.LastTransitionTime)
		}
		result = append(result, cond)
	}
	return result
}

// This is boilerplate we'd like to get rid of when istio 1.29 ships in Feb 26.
type NameKey struct {
	krt.Named
}

func (n NameKey) String() string {
	return fmt.Sprintf("%s/%s", n.Namespace, n.Name)
}

func NewNameKey(o v1.Object) NameKey {
	return NameKey{
		Named: krt.NewNamed(o),
	}
}

func getValidCondition() *acv1.ConditionApplyConfiguration {
	return acv1.Condition().WithType("Valid").WithStatus(v1.ConditionTrue).WithMessage("multicluster load balancer is valid.").WithReason("IsValid")
}

func getInvalidCondition(seName string) *acv1.ConditionApplyConfiguration {
	return acv1.Condition().WithType("Valid").WithStatus(v1.ConditionFalse).
		WithReason("PublicIPNotFound").WithMessage(
		fmt.Sprintf("No Public IP found for InternalServiceExport %s", seName))
}

func getDeployedCondition(numBackends int) *acv1.ConditionApplyConfiguration {
	return acv1.Condition().WithType("Deployed").WithStatus(v1.ConditionTrue).WithMessage(
		fmt.Sprintf("multicluster load balancer deployed successfully with %d backends.", numBackends)).WithReason("DeploymentSucceeded")
}

func getDeploFailedCondition() *acv1.ConditionApplyConfiguration {
	return acv1.Condition().WithType("Deployed").WithStatus(v1.ConditionFalse).
		WithReason("DeploymentFailed").WithMessage(
		"Failed to deploy global load balancer")
}
