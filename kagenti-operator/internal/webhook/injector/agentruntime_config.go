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

package injector

import (
	"context"
	"fmt"
	"slices"

	agentv1alpha1 "github.com/kagenti/operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var arConfigLog = logf.Log.WithName("agentruntime-config")

// AgentRuntimeOverrides holds the per-workload overrides extracted from an
// AgentRuntime CR (agent.kagenti.dev/v1alpha1). Nil pointer fields mean
// "no override".
type AgentRuntimeOverrides struct {
	// Identity — from .spec.identity.spiffe
	SpiffeTrustDomain *string

	// Identity — from .spec.identity.clientRegistration
	// Note: These fields are not yet in the typed CRD. They are retained for
	// forward compatibility and will always be nil until the CRD is extended.
	ClientRegistrationProvider      *string
	ClientRegistrationRealm         *string
	AdminCredentialsSecretName      *string
	AdminCredentialsSecretNamespace *string

	// Identity — from .spec.identity.allowedAudiences
	AllowedAudiences []string

	// AuthBridge deployment shape — from .spec.authBridgeMode
	// Nil = no per-workload override; the namespace's
	// authbridge-runtime-config mode (if set) or the cluster fallback
	// applies.
	AuthBridgeMode *string

	// mTLS posture — from .spec.mtlsMode
	// Nil = no per-workload override; the namespace's
	// authbridge-runtime-config mtls.mode (if set) or "permissive"
	// applies.
	MTLSMode *string

	// TLSBridgeMode is the per-workload override for the outbound TLS bridge.
	// Nil = no override; the namespace tls_bridge.mode (if set) or "disabled"
	// applies.
	TLSBridgeMode *string

	// Egress enforcement — from .spec.egressEnforcement
	// Nil = no per-workload override; the namespace's
	// authbridge-runtime-config egressEnforcement (if set) or
	// "enforce-redirect" (default) applies.
	EgressEnforcement *string
}

// ReadAgentRuntimeOverrides reads the AgentRuntime CR for a given workload
// using typed access. It lists AgentRuntimes in the namespace and finds the
// one whose spec.targetRef.name and spec.targetRef.kind match. When
// workloadKind is empty, only the name is matched (backward-compatible).
// Returns (nil, nil) if no matching AgentRuntime CR is found.
func ReadAgentRuntimeOverrides(ctx context.Context, c client.Reader, namespace, workloadName, workloadKind string) (*AgentRuntimeOverrides, error) {
	list := &agentv1alpha1.AgentRuntimeList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		// If the AgentRuntime CRD is not installed, there are no CRs to find.
		// Treat this the same as "no matching CR" so the webhook skips injection
		// gracefully instead of blocking pod creation.
		// meta.IsNoMatchError catches real API server responses;
		// runtime.IsNotRegisteredError catches scheme-level errors (e.g. fake client).
		if meta.IsNoMatchError(err) || runtime.IsNotRegisteredError(err) {
			arConfigLog.V(1).Info("AgentRuntime CRD not installed, skipping",
				"namespace", namespace)
			return nil, nil
		}
		return nil, fmt.Errorf("listing AgentRuntime CRs in %s: %w", namespace, err)
	}

	// Find the AgentRuntime whose spec.targetRef matches the workload.
	// When workloadKind is known, both name and kind must match.
	// When workloadKind is empty (bare pods, unrecognized owners), we
	// fall back to name-only matching for backward compatibility.
	var match *agentv1alpha1.AgentRuntime
	nameMatches := 0
	for i := range list.Items {
		rt := &list.Items[i]
		if rt.Spec.TargetRef.Name != workloadName {
			continue
		}
		nameMatches++
		if workloadKind != "" && rt.Spec.TargetRef.Kind != workloadKind {
			continue
		}
		match = rt
		break
	}

	if match != nil {
		if workloadKind == "" && nameMatches > 1 {
			arConfigLog.V(0).Info("workloadKind unknown and multiple AgentRuntimes match by name — using first match",
				"namespace", namespace, "workloadName", workloadName, "nameMatches", nameMatches)
		}
		arConfigLog.Info("Found matching AgentRuntime CR",
			"namespace", namespace, "crName", match.Name,
			"targetRef.name", workloadName, "targetRef.kind", match.Spec.TargetRef.Kind)
		return extractOverrides(match), nil
	}

	arConfigLog.V(1).Info("No AgentRuntime CR targets this workload",
		"namespace", namespace, "workloadName", workloadName, "workloadKind", workloadKind)
	return nil, nil
}

// extractOverrides reads the overridable fields from a typed AgentRuntime CR.
func extractOverrides(rt *agentv1alpha1.AgentRuntime) *AgentRuntimeOverrides {
	overrides := &AgentRuntimeOverrides{}

	// .spec.identity.spiffe.trustDomain
	if rt.Spec.Identity != nil && rt.Spec.Identity.SPIFFE != nil && rt.Spec.Identity.SPIFFE.TrustDomain != "" {
		td := rt.Spec.Identity.SPIFFE.TrustDomain
		overrides.SpiffeTrustDomain = &td
	}

	// .spec.identity.allowedAudiences — clone to decouple from CR memory
	if rt.Spec.Identity != nil && len(rt.Spec.Identity.AllowedAudiences) > 0 {
		overrides.AllowedAudiences = slices.Clone(rt.Spec.Identity.AllowedAudiences)
	}

	// .spec.authBridgeMode
	if rt.Spec.AuthBridgeMode != "" {
		mode := rt.Spec.AuthBridgeMode
		overrides.AuthBridgeMode = &mode
	}

	// .spec.mtlsMode
	if rt.Spec.MTLSMode != "" {
		mode := rt.Spec.MTLSMode
		overrides.MTLSMode = &mode
	}

	// .spec.tlsBridgeMode
	if rt.Spec.TLSBridgeMode != "" {
		mode := rt.Spec.TLSBridgeMode
		overrides.TLSBridgeMode = &mode
	}

	// .spec.egressEnforcement
	if rt.Spec.EgressEnforcement != "" {
		ee := rt.Spec.EgressEnforcement
		overrides.EgressEnforcement = &ee
	}

	arConfigLog.Info("AgentRuntime overrides extracted",
		"hasSpiffeTrustDomain", overrides.SpiffeTrustDomain != nil,
		"hasClientRegistration", overrides.ClientRegistrationProvider != nil,
		"hasAuthBridgeMode", overrides.AuthBridgeMode != nil,
		"hasMTLSMode", overrides.MTLSMode != nil,
		"hasEgressEnforcement", overrides.EgressEnforcement != nil)

	return overrides
}
