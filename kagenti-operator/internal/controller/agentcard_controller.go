/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	agentv1alpha1 "github.com/kagenti/operator/api/v1alpha1"
	"github.com/kagenti/operator/internal/agentcard"
	"github.com/kagenti/operator/internal/signature"
)

const (
	LabelAgentType       = "kagenti.io/type"
	LabelAgentProtocol   = "kagenti.io/agent-protocol" // deprecated
	LabelKagentiProtocol = "kagenti.io/protocol"       // deprecated; use ProtocolLabelPrefix
	LabelValueAgent      = "agent"

	// ProtocolLabelPrefix is the label key prefix for multi-protocol support.
	// The existence of a label with this prefix implies support for the named protocol.
	// For example, protocol.kagenti.io/a2a on a workload means it speaks A2A.
	ProtocolLabelPrefix = "protocol.kagenti.io/"

	// LabelSignatureVerified is used by NetworkPolicy rules to gate traffic between verified agents.
	LabelSignatureVerified = "agent.kagenti.dev/signature-verified"

	// Deprecated: superseded by AnnotationVerifiedStatePrefix. Kept for cleanup on existing workloads.
	AnnotationLastVerifiedState = "agent.kagenti.dev/last-verified-state"

	// AnnotationVerifiedStatePrefix stores per-card verified state on the workload.
	// Multiple cards targeting the same workload are AND-aggregated for the label.
	AnnotationVerifiedStatePrefix = "verified-state.agent.kagenti.dev/"

	// AnnotationResignTrigger is patched onto the pod template to trigger a rolling restart
	// when the operator detects that the signing SVID is expiring or the CA has rotated.
	AnnotationResignTrigger = "agentcard.kagenti.dev/resign-trigger"

	// AnnotationResignLeafExpiry records the LeafNotAfter of the cert that prompted the
	// last resign-triggered restart. Prevents cyclic restarts when the SVID TTL is ≤ 2×grace:
	// the operator skips re-triggering if the current cert's expiry matches the stored value.
	AnnotationResignLeafExpiry = "agentcard.kagenti.dev/resign-leaf-expiry"

	// AnnotationBundleHash records the trust bundle hash at the time of the last signing.
	AnnotationBundleHash = "agentcard.kagenti.dev/bundle-hash"

	AgentCardFinalizer = "agentcard.kagenti.dev/finalizer"
	DefaultSyncPeriod  = 30 * time.Second

	DefaultSVIDExpiryGracePeriod = 30 * time.Minute

	ReasonBound         = "Bound"
	ReasonNotBound      = "NotBound"
	ReasonAgentNotFound = "AgentNotFound"

	ReasonSignatureValid        = "SignatureValid"
	ReasonSignatureInvalid      = "SignatureInvalid"
	ReasonSignatureInvalidAudit = "SignatureInvalidAudit"
)

var (
	agentCardLogger = ctrl.Log.WithName("controller").WithName("AgentCard")

	ErrWorkloadNotFound = errors.New("workload not found")
	ErrNotAgentWorkload = errors.New("resource is not a Kagenti agent")
)

type WorkloadInfo struct {
	Name        string
	Namespace   string
	APIVersion  string
	Kind        string
	Labels      map[string]string
	Ready       bool
	ServiceName string
}

type AgentCardReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	AgentFetcher agentcard.Fetcher

	SignatureProvider  signature.Provider
	RequireSignature   bool
	SignatureAuditMode bool

	// SpireTrustDomain can be overridden per-AgentCard via spec.identityBinding.trustDomain.
	SpireTrustDomain string

	// SVIDExpiryGracePeriod controls how far before the leaf cert expires the operator
	// triggers a proactive workload restart. Defaults to DefaultSVIDExpiryGracePeriod.
	SVIDExpiryGracePeriod time.Duration
}

// +kubebuilder:rbac:groups=agent.kagenti.dev,resources=agentcards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent.kagenti.dev,resources=agentcards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent.kagenti.dev,resources=agentcards/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes,verbs=get;list;watch

func (r *AgentCardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	agentCardLogger.V(1).Info("Reconciling AgentCard", "namespacedName", req.NamespacedName)

	agentCard := &agentv1alpha1.AgentCard{}
	err := r.Get(ctx, req.NamespacedName, agentCard)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !agentCard.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, agentCard)
	}

	if !controllerutil.ContainsFinalizer(agentCard, AgentCardFinalizer) {
		controllerutil.AddFinalizer(agentCard, AgentCardFinalizer)
		if err := r.Update(ctx, agentCard); err != nil {
			agentCardLogger.Error(err, "Failed to add finalizer to AgentCard")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	workload, err := r.getWorkload(ctx, agentCard)
	if err != nil {
		agentCardLogger.Error(err, "Failed to get workload", "agentCard", agentCard.Name)

		var message, conditionReason string
		switch {
		case errors.Is(err, ErrWorkloadNotFound):
			message = "No matching workload found"
			conditionReason = "WorkloadNotFound"
		case errors.Is(err, ErrNotAgentWorkload):
			message = "Referenced resource is not an agent"
			conditionReason = "NotAgentWorkload"
		default:
			message = err.Error()
			conditionReason = "WorkloadError"
		}

		if condErr := r.updateCondition(ctx, agentCard, "Synced", metav1.ConditionFalse, conditionReason, err.Error()); condErr != nil {
			return ctrl.Result{}, condErr
		}

		if agentCard.Spec.IdentityBinding != nil {
			if bindErr := r.updateBindingStatus(ctx, agentCard, false, ReasonAgentNotFound, message, ""); bindErr != nil {
				return ctrl.Result{}, bindErr
			}
			if r.Recorder != nil {
				r.Recorder.Event(agentCard, corev1.EventTypeWarning, ReasonAgentNotFound, message)
			}
		}
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	if !workload.Ready {
		agentCardLogger.Info("Workload not ready yet, skipping sync", "workload", workload.Name, "kind", workload.Kind)
		if condErr := r.updateCondition(ctx, agentCard, "Synced", metav1.ConditionFalse, "WorkloadNotReady",
			fmt.Sprintf("%s %s is not ready", workload.Kind, workload.Name)); condErr != nil {
			return ctrl.Result{}, condErr
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	protocol := getWorkloadProtocol(workload.Labels)
	if protocol == "" {
		if condErr := r.updateCondition(ctx, agentCard, "Synced", metav1.ConditionFalse, "NoProtocol",
			"Workload does not have a protocol.kagenti.io/<name> label"); condErr != nil {
			return ctrl.Result{}, condErr
		}
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	service, err := r.getService(ctx, agentCard.Namespace, workload.ServiceName)
	if err != nil {
		agentCardLogger.Error(err, "Failed to get service", "service", workload.ServiceName)
		if condErr := r.updateCondition(ctx, agentCard, "Synced", metav1.ConditionFalse, "ServiceNotFound", err.Error()); condErr != nil {
			return ctrl.Result{}, condErr
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	servicePort := r.getServicePort(service)
	serviceURL := agentcard.GetServiceURL(workload.ServiceName, agentCard.Namespace, servicePort)

	cardData, err := r.AgentFetcher.Fetch(ctx, protocol, serviceURL, workload.ServiceName, agentCard.Namespace)
	if err != nil {
		agentCardLogger.Error(err, "Failed to fetch agent card", "workload", workload.Name, "url", serviceURL)
		if condErr := r.updateCondition(ctx, agentCard, "Synced", metav1.ConditionFalse, "FetchFailed", err.Error()); condErr != nil {
			return ctrl.Result{}, condErr
		}
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	var verificationResult *signature.VerificationResult
	if r.RequireSignature {
		var verifyErr error
		verificationResult, verifyErr = r.verifySignature(ctx, cardData)

		if verifyErr != nil {
			agentCardLogger.Error(verifyErr, "Signature verification error", "workload", workload.Name)
		}

		if verificationResult != nil {
			if verificationResult.Verified {
				if r.Recorder != nil {
					r.Recorder.Event(agentCard, corev1.EventTypeNormal, "SignatureEvaluated",
						fmt.Sprintf("Signature verified successfully (keyID=%s)", verificationResult.KeyID))
				}
			} else {
				reason := ReasonSignatureInvalid
				if r.SignatureAuditMode {
					reason = ReasonSignatureInvalidAudit
				}
				agentCardLogger.Info("Signature verification failed",
					"workload", workload.Name,
					"reason", reason,
					"details", verificationResult.Details)
				if r.Recorder != nil {
					r.Recorder.Event(agentCard, corev1.EventTypeWarning, "SignatureFailed", verificationResult.Details)
				}
			}
		}
	}

	if r.RequireSignature && verificationResult != nil && verificationResult.Verified {
		r.maybeRestartForResign(ctx, agentCard, workload, verificationResult)
	}

	cardData.URL = serviceURL

	cardId := r.computeCardId(cardData)
	if cardId != "" && agentCard.Status.CardId != "" && agentCard.Status.CardId != cardId {
		if r.Recorder != nil {
			r.Recorder.Event(agentCard, corev1.EventTypeWarning, "CardContentChanged",
				fmt.Sprintf("Agent card content changed: previous=%s, current=%s", agentCard.Status.CardId, cardId))
		}
		agentCardLogger.Info("Card content changed", "agentCard", agentCard.Name, "previousCardId", agentCard.Status.CardId, "newCardId", cardId)
	}

	resolvedTargetRef := &agentv1alpha1.TargetRef{
		APIVersion: workload.APIVersion,
		Kind:       workload.Kind,
		Name:       workload.Name,
	}

	var bindingPassed bool
	var binding *bindingResult
	var identityMatch *bool
	sigVerified := verificationResult != nil && verificationResult.Verified
	if agentCard.Spec.IdentityBinding != nil {
		var verifiedSpiffeID string
		if verificationResult != nil && verificationResult.Verified && verificationResult.SpiffeID != "" {
			verifiedSpiffeID = verificationResult.SpiffeID
		}
		binding = r.computeBinding(agentCard, verifiedSpiffeID)
		bindingPassed = binding != nil && binding.Bound
		match := sigVerified && bindingPassed
		identityMatch = &match
	}

	var vr *signature.VerificationResult
	if r.RequireSignature {
		vr = verificationResult
	}
	if err := r.updateAgentCardStatus(ctx, agentCard, cardData, protocol, cardId, resolvedTargetRef, vr, binding, identityMatch); err != nil {
		agentCardLogger.Error(err, "Failed to update AgentCard status")
		return ctrl.Result{}, err
	}

	// Both signature and binding (if configured) must pass for the label.
	if r.RequireSignature {
		isVerified := sigVerified
		if agentCard.Spec.IdentityBinding != nil {
			isVerified = isVerified && bindingPassed
		}
		if err := r.propagateSignatureLabel(ctx, agentCard.Name, workload, isVerified); err != nil {
			agentCardLogger.Error(err, "Failed to propagate signature label to workload",
				"workload", workload.Name, "verified", isVerified)
			return ctrl.Result{}, err
		}

		if verificationResult != nil && !verificationResult.Verified && !r.SignatureAuditMode {
			agentCardLogger.Info("Signature verification failed, rejecting agent card",
				"workload", workload.Name,
				"details", verificationResult.Details)
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
		}
	}

	syncPeriod := r.getSyncPeriod(agentCard)
	agentCardLogger.V(1).Info("Successfully synced agent card", "workload", workload.Name, "kind", workload.Kind, "nextSync", syncPeriod)

	return ctrl.Result{RequeueAfter: syncPeriod}, nil
}

func (r *AgentCardReconciler) getWorkload(ctx context.Context, agentCard *agentv1alpha1.AgentCard) (*WorkloadInfo, error) {
	targetRef := agentCard.Spec.TargetRef
	if targetRef == nil {
		return nil, fmt.Errorf("spec.targetRef is required: specify the workload backing this agent")
	}

	namespace := agentCard.Namespace
	gv, err := schema.ParseGroupVersion(targetRef.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid apiVersion %s: %w", targetRef.APIVersion, err)
	}
	gvk := gv.WithKind(targetRef.Kind)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	key := client.ObjectKey{Namespace: namespace, Name: targetRef.Name}
	if err := r.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s %s not found in namespace %s",
				ErrWorkloadNotFound, targetRef.APIVersion, targetRef.Kind, targetRef.Name, namespace)
		}
		return nil, err
	}

	labels := obj.GetLabels()
	if !isAgentWorkload(labels) {
		return nil, fmt.Errorf("%w: %s %s does not have kagenti.io/type=agent label",
			ErrNotAgentWorkload, targetRef.Kind, targetRef.Name)
	}

	ready := r.isWorkloadReady(obj, targetRef.Kind)

	return &WorkloadInfo{
		Name:        targetRef.Name,
		Namespace:   namespace,
		APIVersion:  targetRef.APIVersion,
		Kind:        targetRef.Kind,
		Labels:      labels,
		Ready:       ready,
		ServiceName: targetRef.Name,
	}, nil
}

func (r *AgentCardReconciler) isWorkloadReady(obj *unstructured.Unstructured, kind string) bool {
	switch kind {
	case "Deployment":
		return isDeploymentReadyFromUnstructured(obj)
	case "StatefulSet":
		return isStatefulSetReadyFromUnstructured(obj)
	default:
		return hasReadyCondition(obj)
	}
}

func isAgentWorkload(labels map[string]string) bool {
	return labels != nil && labels[LabelAgentType] == LabelValueAgent
}

func isDeploymentReadyFromUnstructured(obj *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, c := range conditions {
		condition, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if condition["type"] == "Available" && condition["status"] == "True" {
			return true
		}
	}
	return false
}

// isStatefulSetReadyFromUnstructured returns true when readyReplicas > 0 and all replicas are ready.
// A StatefulSet scaled to 0 intentionally returns false (not ready to serve).
func isStatefulSetReadyFromUnstructured(obj *unstructured.Unstructured) bool {
	readyReplicas, _, err := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
	if err != nil {
		return false
	}
	replicas, _, err := unstructured.NestedInt64(obj.Object, "status", "replicas")
	if err != nil {
		return false
	}
	return readyReplicas > 0 && readyReplicas == replicas
}

func hasReadyCondition(obj *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, c := range conditions {
		condition, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := condition["type"].(string)
		status, _ := condition["status"].(string)
		if (condType == "Ready" || condType == "Available") && status == "True" {
			return true
		}
	}
	return false
}

// getWorkloadProtocols returns all protocols declared on a workload via the
// protocol.kagenti.io/<name> label prefix. Falls back to the deprecated
// kagenti.io/protocol and kagenti.io/agent-protocol single-value labels.
func getWorkloadProtocols(labels map[string]string) []string {
	if labels == nil {
		return nil
	}

	var protocols []string
	for k := range labels {
		if strings.HasPrefix(k, ProtocolLabelPrefix) {
			name := strings.TrimPrefix(k, ProtocolLabelPrefix)
			if name != "" {
				protocols = append(protocols, name)
			}
		}
	}
	if len(protocols) > 0 {
		return protocols
	}

	// Fall back to deprecated single-value labels.
	if protocol := labels[LabelKagentiProtocol]; protocol != "" {
		agentCardLogger.V(1).Info("Deprecated label kagenti.io/protocol in use; migrate to protocol.kagenti.io/<name>",
			"protocol", protocol)
		return []string{protocol}
	}
	if protocol := labels[LabelAgentProtocol]; protocol != "" {
		agentCardLogger.V(1).Info("Deprecated label kagenti.io/agent-protocol in use; migrate to protocol.kagenti.io/<name>",
			"protocol", protocol)
		return []string{protocol}
	}
	return nil
}

// getWorkloadProtocol returns the first declared protocol for a workload.
// Prefer getWorkloadProtocols when the full set of protocols is needed.
func getWorkloadProtocol(labels map[string]string) string {
	protocols := getWorkloadProtocols(labels)
	if len(protocols) == 0 {
		return ""
	}
	return protocols[0]
}

// hasProtocolLabels reports whether any protocol label is present on the workload,
// using either the new prefix or the deprecated single-value labels.
func hasProtocolLabels(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	for k := range labels {
		if strings.HasPrefix(k, ProtocolLabelPrefix) {
			return true
		}
	}
	return labels[LabelKagentiProtocol] != "" || labels[LabelAgentProtocol] != ""
}

func (r *AgentCardReconciler) getService(ctx context.Context, namespace, name string) (*corev1.Service, error) {
	service := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, service)

	if err != nil {
		return nil, fmt.Errorf("failed to get service %s: %w", name, err)
	}

	return service, nil
}

// getServicePort returns the first port, defaulting to 8000 (A2A default).
func (r *AgentCardReconciler) getServicePort(service *corev1.Service) int32 {
	if len(service.Spec.Ports) > 0 {
		return service.Spec.Ports[0].Port
	}
	agentCardLogger.Info("No ports defined, using default 8000",
		"service", service.Name, "namespace", service.Namespace)
	return 8000
}

func (r *AgentCardReconciler) getSyncPeriod(agentCard *agentv1alpha1.AgentCard) time.Duration {
	if agentCard.Spec.SyncPeriod == "" {
		return DefaultSyncPeriod
	}

	duration, err := time.ParseDuration(agentCard.Spec.SyncPeriod)
	if err != nil {
		agentCardLogger.Error(err, "Invalid sync period, using default",
			"syncPeriod", agentCard.Spec.SyncPeriod)
		return DefaultSyncPeriod
	}

	return duration
}

// updateAgentCardStatus persists all status fields atomically with retry.
func (r *AgentCardReconciler) updateAgentCardStatus(ctx context.Context, agentCard *agentv1alpha1.AgentCard, cardData *agentv1alpha1.AgentCardData, protocol, cardId string, targetRef *agentv1alpha1.TargetRef, verificationResult *signature.VerificationResult, binding *bindingResult, identityMatch *bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &agentv1alpha1.AgentCard{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      agentCard.Name,
			Namespace: agentCard.Namespace,
		}, latest); err != nil {
			return err
		}

		latest.Status.Card = cardData
		latest.Status.Protocol = protocol
		latest.Status.TargetRef = targetRef
		if cardId != "" && cardId != latest.Status.CardId {
			latest.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
			latest.Status.CardId = cardId
		} else if latest.Status.LastSyncTime == nil {
			latest.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
		}

		if verificationResult != nil {
			latest.Status.ValidSignature = &verificationResult.Verified
			latest.Status.SignatureVerificationDetails = verificationResult.Details
			latest.Status.SignatureKeyID = verificationResult.KeyID
			if verificationResult.Verified {
				latest.Status.SignatureSpiffeID = verificationResult.SpiffeID
			} else {
				latest.Status.SignatureSpiffeID = ""
			}

			sigCondition := metav1.Condition{
				Type: "SignatureVerified",
			}
			if verificationResult.Verified {
				sigCondition.Status = metav1.ConditionTrue
				sigCondition.Reason = ReasonSignatureValid
				sigCondition.Message = verificationResult.Details
			} else {
				sigCondition.Status = metav1.ConditionFalse
				if r.SignatureAuditMode {
					sigCondition.Reason = ReasonSignatureInvalidAudit
					sigCondition.Message = verificationResult.Details + " (audit mode: allowed)"
				} else {
					sigCondition.Reason = ReasonSignatureInvalid
					sigCondition.Message = verificationResult.Details
				}
			}
			meta.SetStatusCondition(&latest.Status.Conditions, sigCondition)
		}

		if verificationResult != nil && !verificationResult.Verified && !r.SignatureAuditMode {
			meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
				Type:    "Synced",
				Status:  metav1.ConditionFalse,
				Reason:  ReasonSignatureInvalid,
				Message: verificationResult.Details,
			})
		} else {
			message := fmt.Sprintf("Successfully fetched agent card for %s", cardData.Name)
			if verificationResult != nil && !verificationResult.Verified && r.SignatureAuditMode {
				message = fmt.Sprintf("Fetched agent card for %s (signature verification failed but audit mode enabled)", cardData.Name)
			}
			meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
				Type:    "Synced",
				Status:  metav1.ConditionTrue,
				Reason:  "SyncSucceeded",
				Message: message,
			})
		}

		meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  "ReadyToServe",
			Message: "Agent index is ready for queries",
		})

		if binding != nil {
			existingBound := meta.FindStatusCondition(latest.Status.Conditions, "Bound")

			if existingBound == nil {
				agentCardLogger.Info("Identity binding is allowlist-only; SPIFFE trust bundle verification not yet available",
					"agentCard", latest.Name)
				if r.Recorder != nil {
					r.Recorder.Event(agentCard, corev1.EventTypeWarning, "AllowlistOnly",
						"Identity binding is allowlist-only; SPIFFE trust bundle verification not yet available")
				}
			}

			newConditionStatus := metav1.ConditionFalse
			if binding.Bound {
				newConditionStatus = metav1.ConditionTrue
			}
			if existingBound == nil || existingBound.Status != newConditionStatus {
				if r.Recorder != nil {
					if binding.Bound {
						r.Recorder.Event(agentCard, corev1.EventTypeNormal, "BindingEvaluated", binding.Message)
					} else {
						r.Recorder.Event(agentCard, corev1.EventTypeWarning, "BindingFailed", binding.Message)
					}
				}
			}

			bindingChanged := latest.Status.BindingStatus == nil ||
				latest.Status.BindingStatus.Bound != binding.Bound ||
				latest.Status.BindingStatus.Reason != binding.Reason ||
				latest.Status.BindingStatus.Message != binding.Message
			var evalTime *metav1.Time
			if latest.Status.BindingStatus != nil {
				evalTime = latest.Status.BindingStatus.LastEvaluationTime
			}
			if bindingChanged || evalTime == nil {
				now := metav1.Now()
				evalTime = &now
			}
			latest.Status.BindingStatus = &agentv1alpha1.BindingStatus{
				Bound:              binding.Bound,
				Reason:             binding.Reason,
				Message:            binding.Message,
				LastEvaluationTime: evalTime,
			}
			if binding.SpiffeID != "" {
				latest.Status.ExpectedSpiffeID = binding.SpiffeID
			}
			meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
				Type:    "Bound",
				Status:  newConditionStatus,
				Reason:  binding.Reason,
				Message: binding.Message,
			})
		}

		latest.Status.SignatureIdentityMatch = identityMatch

		return r.Status().Update(ctx, latest)
	})
}

// verifySignature delegates to the Provider and records metrics.
func (r *AgentCardReconciler) verifySignature(ctx context.Context, cardData *agentv1alpha1.AgentCardData) (*signature.VerificationResult, error) {
	if r.SignatureProvider == nil {
		return &signature.VerificationResult{
			Verified: false,
			Details:  "no signature provider configured",
		}, nil
	}

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		signature.SignatureVerificationDuration.WithLabelValues(r.SignatureProvider.Name()).Observe(duration)
	}()

	result, err := r.SignatureProvider.VerifySignature(ctx, cardData, cardData.Signatures)

	if result == nil {
		result = &signature.VerificationResult{
			Verified: false,
			Details:  "Verification returned null result",
		}
	}

	signature.RecordVerification(r.SignatureProvider.Name(), result.Verified, r.SignatureAuditMode)
	if err != nil {
		signature.RecordError(r.SignatureProvider.Name(), "verification_error")
	}

	return result, err
}

type podTemplateAccessor struct {
	obj       client.Object
	getLabels func(client.Object) map[string]string
	setLabels func(client.Object, map[string]string)
}

func newPodTemplateAccessor(kind string) (*podTemplateAccessor, bool) {
	switch kind {
	case "Deployment":
		return &podTemplateAccessor{
			obj:       &appsv1.Deployment{},
			getLabels: func(o client.Object) map[string]string { return o.(*appsv1.Deployment).Spec.Template.Labels },
			setLabels: func(o client.Object, l map[string]string) { o.(*appsv1.Deployment).Spec.Template.Labels = l },
		}, true
	case "StatefulSet":
		return &podTemplateAccessor{
			obj:       &appsv1.StatefulSet{},
			getLabels: func(o client.Object) map[string]string { return o.(*appsv1.StatefulSet).Spec.Template.Labels },
			setLabels: func(o client.Object, l map[string]string) { o.(*appsv1.StatefulSet).Spec.Template.Labels = l },
		}, true
	default:
		return nil, false
	}
}

func (r *AgentCardReconciler) propagateSignatureLabel(ctx context.Context, cardName string, workload *WorkloadInfo, verified bool) error {
	if workload == nil {
		return nil
	}

	acc, ok := newPodTemplateAccessor(workload.Kind)
	if !ok {
		agentCardLogger.V(1).Info("Cannot propagate signature label to unsupported workload kind",
			"kind", workload.Kind, "workload", workload.Name)
		return nil
	}

	key := types.NamespacedName{Name: workload.Name, Namespace: workload.Namespace}
	return r.propagateLabelToWorkload(ctx, cardName, key, workload, verified, acc)
}

// propagateLabelToWorkload writes the per-card annotation and AND-aggregates all cards
// to compute the workload-level signature-verified label.
func (r *AgentCardReconciler) propagateLabelToWorkload(
	ctx context.Context,
	cardName string,
	key types.NamespacedName,
	workload *WorkloadInfo,
	verified bool,
	acc *podTemplateAccessor,
) error {
	perCardAnno := AnnotationVerifiedStatePrefix + cardName
	desiredState := "false"
	if verified {
		desiredState = "true"
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, key, acc.obj); err != nil {
			return err
		}

		annotations := acc.obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}

		labels := acc.getLabels(acc.obj)
		if labels == nil {
			labels = make(map[string]string)
			acc.setLabels(acc.obj, labels)
		}

		if annotations[perCardAnno] == desiredState {
			aggregated := r.aggregateVerifiedState(annotations)
			currentLabel := labels[LabelSignatureVerified]
			labelCorrect := (aggregated && currentLabel == "true") || (!aggregated && currentLabel == "")
			if labelCorrect {
				return nil
			}
		}

		annotations[perCardAnno] = desiredState

		delete(annotations, AnnotationLastVerifiedState)

		acc.obj.SetAnnotations(annotations)

		aggregated := r.aggregateVerifiedState(annotations)
		if aggregated {
			labels[LabelSignatureVerified] = "true"
		} else {
			delete(labels, LabelSignatureVerified)
		}

		agentCardLogger.Info("Propagating signature-verified label to pod template",
			"kind", workload.Kind,
			"workload", workload.Name,
			"card", cardName,
			"cardVerified", verified,
			"aggregatedVerified", aggregated)
		return r.Update(ctx, acc.obj)
	})
}

// aggregateVerifiedState returns true only when all per-card annotations are "true".
func (r *AgentCardReconciler) aggregateVerifiedState(annotations map[string]string) bool {
	found := false
	for k, v := range annotations {
		if strings.HasPrefix(k, AnnotationVerifiedStatePrefix) {
			found = true
			if v != "true" {
				return false
			}
		}
	}
	return found
}

func (r *AgentCardReconciler) updateCondition(ctx context.Context, agentCard *agentv1alpha1.AgentCard, conditionType string, status metav1.ConditionStatus, reason, message string) error {
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &agentv1alpha1.AgentCard{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      agentCard.Name,
			Namespace: agentCard.Namespace,
		}, latest); err != nil {
			return err
		}

		meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:    conditionType,
			Status:  status,
			Reason:  reason,
			Message: message,
		})

		return r.Status().Update(ctx, latest)
	}); err != nil {
		agentCardLogger.Error(err, "Failed to update condition", "type", conditionType)
		return err
	}
	return nil
}

func (r *AgentCardReconciler) handleDeletion(ctx context.Context, agentCard *agentv1alpha1.AgentCard) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(agentCard, AgentCardFinalizer) {
		agentCardLogger.Info("Cleaning up AgentCard", "name", agentCard.Name)

		r.cleanupPerCardAnnotation(ctx, agentCard)

		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := &agentv1alpha1.AgentCard{}
			if err := r.Get(ctx, types.NamespacedName{
				Name:      agentCard.Name,
				Namespace: agentCard.Namespace,
			}, latest); err != nil {
				return err
			}

			controllerutil.RemoveFinalizer(latest, AgentCardFinalizer)
			return r.Update(ctx, latest)
		}); err != nil {
			agentCardLogger.Error(err, "Failed to remove finalizer from AgentCard")
			return ctrl.Result{}, err
		}

		agentCardLogger.Info("Removed finalizer from AgentCard")
	}

	return ctrl.Result{}, nil
}

// cleanupPerCardAnnotation removes this card's annotation from the workload and re-aggregates the label.
func (r *AgentCardReconciler) cleanupPerCardAnnotation(ctx context.Context, agentCard *agentv1alpha1.AgentCard) {
	if agentCard.Spec.TargetRef == nil {
		return
	}
	ref := agentCard.Spec.TargetRef

	acc, ok := newPodTemplateAccessor(ref.Kind)
	if !ok {
		return
	}

	key := types.NamespacedName{Name: ref.Name, Namespace: agentCard.Namespace}
	perCardAnno := AnnotationVerifiedStatePrefix + agentCard.Name

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, key, acc.obj); err != nil {
			return client.IgnoreNotFound(err)
		}
		annotations := acc.obj.GetAnnotations()
		if annotations == nil {
			return nil
		}
		if _, exists := annotations[perCardAnno]; !exists {
			return nil
		}

		delete(annotations, perCardAnno)
		acc.obj.SetAnnotations(annotations)

		labels := acc.getLabels(acc.obj)
		if labels == nil {
			labels = make(map[string]string)
			acc.setLabels(acc.obj, labels)
		}

		aggregated := r.aggregateVerifiedState(annotations)
		if aggregated {
			labels[LabelSignatureVerified] = "true"
		} else {
			delete(labels, LabelSignatureVerified)
		}

		agentCardLogger.Info("Cleaned up per-card annotation on workload deletion",
			"card", agentCard.Name, "workload", ref.Name, "aggregatedVerified", aggregated)
		return r.Update(ctx, acc.obj)
	})
	if err != nil {
		agentCardLogger.Error(err, "Failed to clean up per-card annotation",
			"card", agentCard.Name, "workload", ref.Name)
	}
}

func (r *AgentCardReconciler) mapWorkloadToAgentCard(apiVersion, kind string) handler.MapFunc {
	return mapWorkloadToAgentCards(r.Client, apiVersion, kind, agentCardLogger)
}

func agentLabelPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		labels := obj.GetLabels()
		return labels != nil && labels[LabelAgentType] == LabelValueAgent
	})
}

// ignoreOperatorLabelUpdatePredicate suppresses Update events caused by the operator's own
// label/annotation propagation, preventing reconciliation loops.
func ignoreOperatorLabelUpdatePredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return true
			}
			oldAnnos := e.ObjectOld.GetAnnotations()
			newAnnos := e.ObjectNew.GetAnnotations()

			if oldAnnos[AnnotationLastVerifiedState] != newAnnos[AnnotationLastVerifiedState] {
				return false
			}

			if oldAnnos[AnnotationBundleHash] != newAnnos[AnnotationBundleHash] {
				return false
			}

			for k, newVal := range newAnnos {
				if strings.HasPrefix(k, AnnotationVerifiedStatePrefix) {
					if oldAnnos[k] != newVal {
						return false
					}
				}
			}
			for k := range oldAnnos {
				if strings.HasPrefix(k, AnnotationVerifiedStatePrefix) {
					if _, exists := newAnnos[k]; !exists {
						return false
					}
				}
			}

			return true
		},
	}
}

type bindingResult struct {
	Bound    bool
	Reason   string
	Message  string
	SpiffeID string
}

// computeBinding evaluates trust-domain identity binding. verifiedSpiffeID is empty when unsigned.
func (r *AgentCardReconciler) computeBinding(agentCard *agentv1alpha1.AgentCard, verifiedSpiffeID string) *bindingResult {
	binding := agentCard.Spec.IdentityBinding
	if binding == nil {
		return nil
	}

	if verifiedSpiffeID == "" {
		return &bindingResult{
			Bound:   false,
			Reason:  ReasonNotBound,
			Message: "No SPIFFE ID from x5c certificate chain: ensure the card is signed with a SPIRE-issued SVID",
		}
	}

	trustDomain := binding.TrustDomain
	if trustDomain == "" {
		trustDomain = r.SpireTrustDomain
	}
	if trustDomain == "" {
		return &bindingResult{
			Bound:   false,
			Reason:  ReasonNotBound,
			Message: "No trust domain configured (set --spire-trust-domain or spec.identityBinding.trustDomain)",
		}
	}

	prefix := "spiffe://" + trustDomain + "/"
	bound := strings.HasPrefix(verifiedSpiffeID, prefix) && len(verifiedSpiffeID) > len(prefix)

	if !bound {
		agentCardLogger.Info("Trust domain mismatch",
			"verifiedSpiffeID", verifiedSpiffeID,
			"expectedTrustDomain", trustDomain)
		signature.IncrementTrustDomainMismatch()
	}

	var reason, message string
	if bound {
		reason = ReasonBound
		message = fmt.Sprintf("SPIFFE ID %s belongs to trust domain %s", verifiedSpiffeID, trustDomain)
	} else {
		reason = ReasonNotBound
		message = fmt.Sprintf("SPIFFE ID %s does not belong to trust domain %s", verifiedSpiffeID, trustDomain)
	}

	return &bindingResult{Bound: bound, Reason: reason, Message: message, SpiffeID: verifiedSpiffeID}
}

// updateBindingStatus writes binding status when the main status path is unreachable.
func (r *AgentCardReconciler) updateBindingStatus(ctx context.Context, agentCard *agentv1alpha1.AgentCard, bound bool, reason, message, expectedSpiffeID string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &agentv1alpha1.AgentCard{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      agentCard.Name,
			Namespace: agentCard.Namespace,
		}, latest); err != nil {
			return err
		}

		bindingChanged := latest.Status.BindingStatus == nil ||
			latest.Status.BindingStatus.Bound != bound ||
			latest.Status.BindingStatus.Reason != reason ||
			latest.Status.BindingStatus.Message != message
		var evalTime *metav1.Time
		if latest.Status.BindingStatus != nil {
			evalTime = latest.Status.BindingStatus.LastEvaluationTime
		}
		if bindingChanged || evalTime == nil {
			now := metav1.Now()
			evalTime = &now
		}
		latest.Status.BindingStatus = &agentv1alpha1.BindingStatus{
			Bound:              bound,
			Reason:             reason,
			Message:            message,
			LastEvaluationTime: evalTime,
		}
		if expectedSpiffeID != "" {
			latest.Status.ExpectedSpiffeID = expectedSpiffeID
		}

		conditionStatus := metav1.ConditionFalse
		if bound {
			conditionStatus = metav1.ConditionTrue
		}
		meta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{
			Type:    "Bound",
			Status:  conditionStatus,
			Reason:  reason,
			Message: message,
		})

		return r.Status().Update(ctx, latest)
	})
}

// computeCardId returns a SHA-256 hash of the card data for drift detection.
func (r *AgentCardReconciler) computeCardId(cardData *agentv1alpha1.AgentCardData) string {
	if cardData == nil {
		return ""
	}
	data, err := json.Marshal(cardData)
	if err != nil {
		agentCardLogger.Error(err, "Failed to marshal card data for hash computation")
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// maybeRestartForResign checks two conditions and triggers a rolling restart if either is true:
//  1. The leaf SVID cert is approaching expiry (within SVIDExpiryGracePeriod).
//  2. The trust bundle hash changed since the workload was last (re)started.
//
// Both feed into the same mechanism: patch the pod template annotation to trigger a rollout.
// The init-container re-runs, fetches a fresh SVID, and re-signs the card.
func (r *AgentCardReconciler) maybeRestartForResign(ctx context.Context, agentCard *agentv1alpha1.AgentCard, workload *WorkloadInfo, vr *signature.VerificationResult) {
	if workload == nil || r.SignatureProvider == nil {
		return
	}

	acc, ok := newPodTemplateAccessor(workload.Kind)
	if !ok {
		return
	}

	key := types.NamespacedName{Name: workload.Name, Namespace: workload.Namespace}
	if err := r.Get(ctx, key, acc.obj); err != nil {
		return
	}

	annotations := acc.obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	currentBundleHash := r.SignatureProvider.BundleHash()
	workloadBundleHash := annotations[AnnotationBundleHash]

	grace := r.SVIDExpiryGracePeriod
	if grace == 0 {
		grace = DefaultSVIDExpiryGracePeriod
	}

	podAnnotations := getPodTemplateAnnotations(acc)
	if ts, ok := podAnnotations[AnnotationResignTrigger]; ok {
		lastTrigger, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			agentCardLogger.Info("Ignoring malformed resign-trigger annotation",
				"value", ts, "error", err.Error())
		} else if time.Since(lastTrigger) < grace {
			return
		}
	}

	needsRestart := false
	reason := ""

	if !vr.LeafNotAfter.IsZero() && time.Until(vr.LeafNotAfter) < grace {
		// Skip if we already triggered a restart for this exact cert expiry.
		// This prevents cyclic restarts when SVID TTL ≤ 2×grace (issue #246).
		storedExpiry := podAnnotations[AnnotationResignLeafExpiry]
		if storedExpiry == vr.LeafNotAfter.Format(time.RFC3339) {
			return
		}
		needsRestart = true
		reason = fmt.Sprintf("SVID leaf cert expiring at %s", vr.LeafNotAfter.Format(time.RFC3339))
	}

	if workloadBundleHash != "" && currentBundleHash != "" && workloadBundleHash != currentBundleHash {
		needsRestart = true
		reason = "trust bundle changed (CA rotation)"
	}

	if !needsRestart {
		if workloadBundleHash == "" && currentBundleHash != "" {
			if err := r.patchBundleHashAnnotation(ctx, acc, key, currentBundleHash); err != nil {
				agentCardLogger.Error(err, "Failed to set initial bundle hash annotation")
			}
		}
		return
	}

	agentCardLogger.Info("Triggering proactive workload restart for re-signing",
		"workload", workload.Name, "kind", workload.Kind, "reason", reason)
	if r.Recorder != nil {
		r.Recorder.Event(agentCard, corev1.EventTypeNormal, "ResignTriggered", reason)
	}

	r.triggerRolloutRestart(ctx, acc, key, currentBundleHash, vr.LeafNotAfter)
}

func (r *AgentCardReconciler) triggerRolloutRestart(ctx context.Context, acc *podTemplateAccessor, key types.NamespacedName, bundleHash string, leafNotAfter time.Time) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, key, acc.obj); err != nil {
			return err
		}

		podAnnotations := getPodTemplateAnnotations(acc)
		if podAnnotations == nil {
			podAnnotations = make(map[string]string)
		}
		podAnnotations[AnnotationResignTrigger] = time.Now().Format(time.RFC3339)
		if !leafNotAfter.IsZero() {
			podAnnotations[AnnotationResignLeafExpiry] = leafNotAfter.Format(time.RFC3339)
		}
		setPodTemplateAnnotations(acc, podAnnotations)

		objAnnotations := acc.obj.GetAnnotations()
		if objAnnotations == nil {
			objAnnotations = make(map[string]string)
		}
		objAnnotations[AnnotationBundleHash] = bundleHash
		acc.obj.SetAnnotations(objAnnotations)

		return r.Update(ctx, acc.obj)
	})
	if err != nil {
		agentCardLogger.Error(err, "Failed to trigger rollout restart", "workload", key.Name)
	}
}

func (r *AgentCardReconciler) patchBundleHashAnnotation(ctx context.Context, acc *podTemplateAccessor, key types.NamespacedName, bundleHash string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, key, acc.obj); err != nil {
			return err
		}
		annotations := acc.obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		if annotations[AnnotationBundleHash] == bundleHash {
			return nil
		}
		annotations[AnnotationBundleHash] = bundleHash
		acc.obj.SetAnnotations(annotations)
		return r.Update(ctx, acc.obj)
	})
}

func getPodTemplateAnnotations(acc *podTemplateAccessor) map[string]string {
	switch o := acc.obj.(type) {
	case *appsv1.Deployment:
		return o.Spec.Template.Annotations
	case *appsv1.StatefulSet:
		return o.Spec.Template.Annotations
	}
	return nil
}

func setPodTemplateAnnotations(acc *podTemplateAccessor, annotations map[string]string) {
	switch o := acc.obj.(type) {
	case *appsv1.Deployment:
		o.Spec.Template.Annotations = annotations
	case *appsv1.StatefulSet:
		o.Spec.Template.Annotations = annotations
	}
}

func (r *AgentCardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.AgentFetcher == nil {
		r.AgentFetcher = agentcard.NewConfigMapFetcher(mgr.GetAPIReader())
	}

	if err := RegisterAgentCardTargetRefIndex(mgr); err != nil {
		return err
	}

	workloadPredicates := predicate.And(agentLabelPredicate(), ignoreOperatorLabelUpdatePredicate())

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&agentv1alpha1.AgentCard{}).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.mapWorkloadToAgentCard("apps/v1", "Deployment")),
			builder.WithPredicates(workloadPredicates),
		).
		Watches(
			&appsv1.StatefulSet{},
			handler.EnqueueRequestsFromMapFunc(r.mapWorkloadToAgentCard("apps/v1", "StatefulSet")),
			builder.WithPredicates(workloadPredicates),
		)

	if SandboxCRDExists(mgr.GetConfig()) {
		sandboxObj := &unstructured.Unstructured{}
		sandboxObj.SetGroupVersionKind(sandboxGVK)
		controllerBuilder = controllerBuilder.Watches(
			sandboxObj,
			handler.EnqueueRequestsFromMapFunc(r.mapWorkloadToAgentCard("agents.x-k8s.io/v1alpha1", "Sandbox")),
			builder.WithPredicates(workloadPredicates),
		)
	}

	return controllerBuilder.
		Named("AgentCard").
		Complete(r)
}
