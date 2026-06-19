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
	"testing"

	"github.com/kagenti/operator/internal/webhook/config"
	"k8s.io/utils/ptr"
)

func TestResolveConfig_NilInputs(t *testing.T) {
	resolved := ResolveConfig(nil, nil, nil)
	if resolved.Platform == nil {
		t.Fatal("expected Platform to be set to compiled defaults")
	}
	if resolved.SpiffeTrustDomain != "cluster.local" {
		t.Errorf("SpiffeTrustDomain = %q, want %q", resolved.SpiffeTrustDomain, "cluster.local")
	}
}

func TestResolveConfig_NamespaceOnly(t *testing.T) {
	ns := &NamespaceConfig{
		KeycloakURL:    "http://keycloak:8080",
		KeycloakRealm:  "kagenti",
		TokenURL:       "http://keycloak:8080/token",
		Issuer:         "http://keycloak:8080/realms/demo",
		TargetAudience: "my-audience",
		TargetScopes:   "openid",
	}

	resolved := ResolveConfig(config.CompiledDefaults(), ns, nil)
	if resolved.KeycloakURL != "http://keycloak:8080" {
		t.Errorf("KeycloakURL = %q", resolved.KeycloakURL)
	}
	if resolved.TokenURL != "http://keycloak:8080/token" {
		t.Errorf("TokenURL = %q", resolved.TokenURL)
	}
}

func TestResolveConfig_AgentRuntimeOverrides_Realm(t *testing.T) {
	ns := &NamespaceConfig{
		KeycloakRealm: "ns-realm",
	}
	ar := &AgentRuntimeOverrides{
		ClientRegistrationRealm: ptr.To("ar-realm"),
	}

	resolved := ResolveConfig(config.CompiledDefaults(), ns, ar)

	// AgentRuntime realm override should win
	if resolved.KeycloakRealm != "ar-realm" {
		t.Errorf("KeycloakRealm = %q, want AR override", resolved.KeycloakRealm)
	}
}

func TestResolveConfig_SpiffeTrustDomain_FromPlatform(t *testing.T) {
	platform := config.CompiledDefaults()
	platform.Spiffe.TrustDomain = "custom.domain"

	ns := &NamespaceConfig{}

	resolved := ResolveConfig(platform, ns, nil)
	if resolved.SpiffeTrustDomain != "custom.domain" {
		t.Errorf("SpiffeTrustDomain = %q, want %q", resolved.SpiffeTrustDomain, "custom.domain")
	}
}

func TestResolveConfig_SpiffeTrustDomain_AROverride(t *testing.T) {
	platform := config.CompiledDefaults()
	platform.Spiffe.TrustDomain = "platform.domain"

	ar := &AgentRuntimeOverrides{
		SpiffeTrustDomain: ptr.To("ar.domain"),
	}

	resolved := ResolveConfig(platform, &NamespaceConfig{}, ar)
	if resolved.SpiffeTrustDomain != "ar.domain" {
		t.Errorf("SpiffeTrustDomain = %q, want %q", resolved.SpiffeTrustDomain, "ar.domain")
	}
}

func TestResolveConfig_SidecarConfigs_NotOverridable(t *testing.T) {
	ns := &NamespaceConfig{
		SpiffeHelperConf:    "helper.conf content",
		EnvoyYAML:           "envoy.yaml content",
		AuthproxyRoutesYAML: "routes.yaml content",
	}
	// AR overrides don't have fields for these — they flow through from namespace
	ar := &AgentRuntimeOverrides{
		SpiffeTrustDomain: ptr.To("override"),
	}

	resolved := ResolveConfig(config.CompiledDefaults(), ns, ar)
	if resolved.SpiffeHelperConf != "helper.conf content" {
		t.Errorf("SpiffeHelperConf should come from namespace")
	}
	if resolved.EnvoyYAML != "envoy.yaml content" {
		t.Errorf("EnvoyYAML should come from namespace")
	}
	if resolved.AuthproxyRoutesYAML != "routes.yaml content" {
		t.Errorf("AuthproxyRoutesYAML should come from namespace")
	}
}

func TestResolveConfig_TokenExchange_NotOverridable(t *testing.T) {
	ns := &NamespaceConfig{
		TokenURL:       "http://keycloak:8080/token",
		TargetAudience: "my-audience",
		TargetScopes:   "openid",
	}
	// AR has no token exchange fields — they come from namespace only
	ar := &AgentRuntimeOverrides{}

	resolved := ResolveConfig(config.CompiledDefaults(), ns, ar)
	if resolved.TokenURL != "http://keycloak:8080/token" {
		t.Errorf("TokenURL = %q, want namespace value", resolved.TokenURL)
	}
	if resolved.TargetAudience != "my-audience" {
		t.Errorf("TargetAudience = %q, want namespace value", resolved.TargetAudience)
	}
	if resolved.TargetScopes != "openid" {
		t.Errorf("TargetScopes = %q, want namespace value", resolved.TargetScopes)
	}
}

func TestResolveConfig_TLSBridgeMode_Precedence(t *testing.T) {
	// CR override wins.
	ar := &AgentRuntimeOverrides{TLSBridgeMode: ptr.To("enabled")}
	if rc := ResolveConfig(config.CompiledDefaults(), nil, ar); rc.TLSBridgeMode != "enabled" {
		t.Errorf("CR override: got %q, want enabled", rc.TLSBridgeMode)
	}
	// Namespace value (tls_bridge.mode in the runtime YAML) when no CR override.
	ns := &NamespaceConfig{AuthBridgeRuntimeYAML: "tls_bridge:\n  mode: enabled\n"}
	if rc := ResolveConfig(config.CompiledDefaults(), ns, nil); rc.TLSBridgeMode != "enabled" {
		t.Errorf("namespace: got %q, want enabled", rc.TLSBridgeMode)
	}
	// Default when nothing sets it.
	if rc := ResolveConfig(config.CompiledDefaults(), nil, nil); rc.TLSBridgeMode != "disabled" {
		t.Errorf("default: got %q, want disabled", rc.TLSBridgeMode)
	}
}
