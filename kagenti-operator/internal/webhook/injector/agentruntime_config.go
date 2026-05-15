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

	// Observability — from .spec.trace
	TraceEndpoint     *string
	TraceProtocol     *string  // "grpc" or "http"
	TraceSamplingRate *float64 // 0.0–1.0

	// AuthBridge deployment shape — from .spec.authBridgeMode
	// Nil = no per-workload override; the namespace's
	// authbridge-runtime-config mode (if set) or the cluster fallback
	// applies.
	AuthBridgeMode *string
}

// ReadAgentRuntimeOverrides reads the AgentRuntime CR for a given workload
// using typed access. It lists AgentRuntimes in the namespace and finds the
// one whose spec.targetRef.name matches workloadName.
// Returns (nil, nil) if no matching AgentRuntime CR is found.
func ReadAgentRuntimeOverrides(ctx context.Context, c client.Reader, namespace, workloadName string) (*AgentRuntimeOverrides, error) {
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

	// Find the AgentRuntime whose spec.targetRef.name matches the workload
	for i := range list.Items {
		rt := &list.Items[i]
		if rt.Spec.TargetRef.Name != workloadName {
			continue
		}

		arConfigLog.Info("Found matching AgentRuntime CR",
			"namespace", namespace, "crName", rt.Name, "targetRef.name", workloadName)
		return extractOverrides(rt), nil
	}

	arConfigLog.V(1).Info("No AgentRuntime CR targets this workload",
		"namespace", namespace, "workloadName", workloadName)
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

	// .spec.trace.endpoint
	if rt.Spec.Trace != nil && rt.Spec.Trace.Endpoint != "" {
		ep := rt.Spec.Trace.Endpoint
		overrides.TraceEndpoint = &ep
	}

	// .spec.trace.protocol
	if rt.Spec.Trace != nil && rt.Spec.Trace.Protocol != "" {
		p := string(rt.Spec.Trace.Protocol)
		overrides.TraceProtocol = &p
	}

	// .spec.trace.sampling.rate
	if rt.Spec.Trace != nil && rt.Spec.Trace.Sampling != nil {
		rate := rt.Spec.Trace.Sampling.Rate
		overrides.TraceSamplingRate = &rate
	}

	// .spec.authBridgeMode
	if rt.Spec.AuthBridgeMode != "" {
		mode := rt.Spec.AuthBridgeMode
		overrides.AuthBridgeMode = &mode
	}

	arConfigLog.Info("AgentRuntime overrides extracted",
		"hasSpiffeTrustDomain", overrides.SpiffeTrustDomain != nil,
		"hasClientRegistration", overrides.ClientRegistrationProvider != nil,
		"hasTrace", overrides.TraceEndpoint != nil,
		"hasAuthBridgeMode", overrides.AuthBridgeMode != nil)

	return overrides
}
