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
	"testing"

	agentv1alpha1 "github.com/kagenti/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newAgentRuntimeScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = agentv1alpha1.AddToScheme(scheme)
	return scheme
}

func TestReadAgentRuntimeOverrides_NotFound(t *testing.T) {
	scheme := newAgentRuntimeScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	overrides, err := ReadAgentRuntimeOverrides(context.Background(), fakeClient, "ns1", "my-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overrides != nil {
		t.Fatalf("expected nil overrides, got %+v", overrides)
	}
}

func TestReadAgentRuntimeOverrides_MatchesByTargetRef(t *testing.T) {
	scheme := newAgentRuntimeScheme()

	cr := &agentv1alpha1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent-runtime",
			Namespace: "ns1",
		},
		Spec: agentv1alpha1.AgentRuntimeSpec{
			Type: agentv1alpha1.RuntimeTypeAgent,
			TargetRef: agentv1alpha1.TargetRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "my-agent",
			},
			Identity: &agentv1alpha1.IdentitySpec{
				SPIFFE: &agentv1alpha1.SPIFFEIdentity{
					TrustDomain: "override.local",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()

	overrides, err := ReadAgentRuntimeOverrides(context.Background(), fakeClient, "ns1", "my-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overrides == nil {
		t.Fatal("expected non-nil overrides")
	}

	// Identity — SPIFFE
	if overrides.SpiffeTrustDomain == nil || *overrides.SpiffeTrustDomain != "override.local" {
		t.Errorf("SpiffeTrustDomain = %v", overrides.SpiffeTrustDomain)
	}

	// ClientRegistration fields are not in the typed CRD yet — should be nil
	if overrides.ClientRegistrationProvider != nil {
		t.Errorf("expected nil ClientRegistrationProvider, got %v", overrides.ClientRegistrationProvider)
	}
	if overrides.ClientRegistrationRealm != nil {
		t.Errorf("expected nil ClientRegistrationRealm, got %v", overrides.ClientRegistrationRealm)
	}
}

func TestReadAgentRuntimeOverrides_PartialOverrides(t *testing.T) {
	scheme := newAgentRuntimeScheme()

	cr := &agentv1alpha1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent-rt",
			Namespace: "ns1",
		},
		Spec: agentv1alpha1.AgentRuntimeSpec{
			Type: agentv1alpha1.RuntimeTypeAgent,
			TargetRef: agentv1alpha1.TargetRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "my-agent",
			},
			Identity: &agentv1alpha1.IdentitySpec{
				SPIFFE: &agentv1alpha1.SPIFFEIdentity{
					TrustDomain: "custom.domain",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()

	overrides, err := ReadAgentRuntimeOverrides(context.Background(), fakeClient, "ns1", "my-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overrides == nil {
		t.Fatal("expected non-nil overrides")
	}
	if overrides.SpiffeTrustDomain == nil || *overrides.SpiffeTrustDomain != "custom.domain" {
		t.Errorf("SpiffeTrustDomain = %v", overrides.SpiffeTrustDomain)
	}
	// Other fields should be nil
	if overrides.ClientRegistrationProvider != nil {
		t.Errorf("expected nil ClientRegistrationProvider, got %v", overrides.ClientRegistrationProvider)
	}
}

func TestReadAgentRuntimeOverrides_NoTargetRefMatch(t *testing.T) {
	scheme := newAgentRuntimeScheme()

	cr := &agentv1alpha1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-runtime",
			Namespace: "ns1",
		},
		Spec: agentv1alpha1.AgentRuntimeSpec{
			Type: agentv1alpha1.RuntimeTypeAgent,
			TargetRef: agentv1alpha1.TargetRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "other-agent",
			},
			Identity: &agentv1alpha1.IdentitySpec{
				SPIFFE: &agentv1alpha1.SPIFFEIdentity{
					TrustDomain: "should-not-match",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()

	overrides, err := ReadAgentRuntimeOverrides(context.Background(), fakeClient, "ns1", "my-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overrides != nil {
		t.Fatalf("expected nil overrides for non-matching targetRef, got %+v", overrides)
	}
}

func TestReadAgentRuntimeOverrides_CRDNotInstalled(t *testing.T) {
	// Empty scheme — no AgentRuntime types registered.
	// The List call returns a NoKindMatch error, which should be treated as
	// "no matching CR" (nil, nil) so the webhook skips injection gracefully
	// instead of blocking pod creation.
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	overrides, err := ReadAgentRuntimeOverrides(context.Background(), fakeClient, "ns1", "my-agent")
	if err != nil {
		t.Fatalf("expected nil error for missing CRD (graceful skip), got: %v", err)
	}
	if overrides != nil {
		t.Fatalf("expected nil overrides, got %+v", overrides)
	}
}
