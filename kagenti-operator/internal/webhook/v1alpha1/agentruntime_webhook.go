/*
Copyright 2026.

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

package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	agentv1alpha1 "github.com/kagenti/operator/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var agentruntimelog = ctrl.Log.WithName("agentruntime-webhook")

func SetupAgentRuntimeWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &agentv1alpha1.AgentRuntime{}).
		WithValidator(&AgentRuntimeValidator{Reader: mgr.GetAPIReader()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-agent-kagenti-dev-v1alpha1-agentruntime,mutating=false,failurePolicy=fail,sideEffects=None,groups=agent.kagenti.dev,resources=agentruntimes,verbs=create;update,versions=v1alpha1,name=vagentruntime.kb.io,admissionReviewVersions=v1

type AgentRuntimeValidator struct {
	// Reader is an uncached client for authoritative reads from the API server.
	// Used for duplicate targetRef checks during admission. Nil-safe: the check
	// is skipped when Reader is nil (e.g., in unit tests without a real API server).
	Reader client.Reader
}

func (v *AgentRuntimeValidator) ValidateCreate(ctx context.Context, rt *agentv1alpha1.AgentRuntime) (admission.Warnings, error) {
	agentruntimelog.Info("validate create", "name", rt.Name)

	if err := v.checkDuplicateTargetRef(ctx, rt); err != nil {
		return nil, err
	}
	if err := checkMTLSCompatibleWithMode(rt); err != nil {
		return nil, err
	}
	if err := validateSkills(rt.Spec.Skills); err != nil {
		return nil, err
	}

	return nil, nil
}

func (v *AgentRuntimeValidator) ValidateUpdate(ctx context.Context, _ *agentv1alpha1.AgentRuntime, rt *agentv1alpha1.AgentRuntime) (admission.Warnings, error) {
	agentruntimelog.Info("validate update", "name", rt.Name)

	if err := v.checkDuplicateTargetRef(ctx, rt); err != nil {
		return nil, err
	}
	if err := checkMTLSCompatibleWithMode(rt); err != nil {
		return nil, err
	}
	if err := validateSkills(rt.Spec.Skills); err != nil {
		return nil, err
	}

	return nil, nil
}

func (v *AgentRuntimeValidator) ValidateDelete(_ context.Context, rt *agentv1alpha1.AgentRuntime) (admission.Warnings, error) {
	agentruntimelog.Info("validate delete", "name", rt.Name)

	return nil, nil
}

// checkMTLSCompatibleWithMode validates the mtlsMode + authBridgeMode
// combination. Empty / "disabled" mtlsMode is permitted for any
// authBridgeMode (today's plaintext default). All three deployment
// shapes (proxy-sidecar, lite, envoy-sidecar) now support permissive
// and strict — proxy-sidecar / lite via the authbridge listener,
// envoy-sidecar via TLS blocks rendered into a per-agent
// envoy-config ConfigMap (DownstreamTlsContext on the inbound
// listener, UpstreamTlsContext on original_destination_tls in
// strict). The webhook keeps this function as the central enum
// gate so future incompatible combinations can land here without
// scattering the check.
func checkMTLSCompatibleWithMode(rt *agentv1alpha1.AgentRuntime) error {
	// TODO(future-incompatibility): re-enable cross-field rejections
	// here when a new authBridgeMode (e.g. waypoint, sidecarless) lands
	// that needs different mTLS semantics. Today the matrix is fully
	// supported — CRD enum validation pins mtlsMode and authBridgeMode
	// to their enums, and every combination is implemented. The
	// function intentionally stays as a single grep-target for the
	// next incompatible combination so the rejection can land here
	// instead of getting scattered across the validator.
	_ = rt
	return nil
}

// checkDuplicateTargetRef rejects creation/update if another AgentRuntime already
// targets the same workload (apiVersion + kind + name) in the same namespace.
func (v *AgentRuntimeValidator) checkDuplicateTargetRef(ctx context.Context, rt *agentv1alpha1.AgentRuntime) error {
	if v.Reader == nil {
		return nil
	}

	ref := rt.Spec.TargetRef

	rtList := &agentv1alpha1.AgentRuntimeList{}
	// fail-open: allow creation if we can't verify uniqueness
	if err := v.Reader.List(ctx, rtList, client.InNamespace(rt.Namespace)); err != nil {
		agentruntimelog.Error(err, "failed to list AgentRuntimes for duplicate check")
		return nil
	}

	for i := range rtList.Items {
		existing := &rtList.Items[i]
		if existing.Name == rt.Name {
			continue
		}
		if existing.Spec.TargetRef.APIVersion == ref.APIVersion &&
			existing.Spec.TargetRef.Kind == ref.Kind &&
			existing.Spec.TargetRef.Name == ref.Name {
			return fmt.Errorf(
				"an AgentRuntime already targets %s %s in namespace %s: %s",
				ref.Kind, ref.Name, rt.Namespace, existing.Name,
			)
		}
	}

	return nil
}

var deniedMountPrefixes = []string{"/proc", "/sys", "/dev", "/var/run/secrets"}

func validateSkills(skills []agentv1alpha1.SkillImageRef) error {
	if len(skills) == 0 {
		return nil
	}

	seenNames := make(map[string]bool, len(skills))
	seenPaths := make(map[string]bool, len(skills))
	for i, skill := range skills {
		if seenNames[skill.Name] {
			return fmt.Errorf("spec.skills[%d]: duplicate skill name %q", i, skill.Name)
		}
		seenNames[skill.Name] = true

		if seenPaths[skill.MountPath] {
			return fmt.Errorf("spec.skills[%d]: duplicate mountPath %q", i, skill.MountPath)
		}
		seenPaths[skill.MountPath] = true

		if strings.Contains(skill.MountPath, "..") {
			return fmt.Errorf("spec.skills[%d]: mountPath %q must not contain path traversal (..)", i, skill.MountPath)
		}
		for _, prefix := range deniedMountPrefixes {
			if strings.HasPrefix(skill.MountPath, prefix) {
				return fmt.Errorf("spec.skills[%d]: mountPath %q overlaps protected path %q", i, skill.MountPath, prefix)
			}
		}
	}
	return nil
}
