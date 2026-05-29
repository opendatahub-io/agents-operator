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
	"github.com/kagenti/operator/internal/webhook/config"
)

// ResolvedConfig is the fully-merged configuration for a single workload injection.
// It combines PlatformConfig (images, ports, resources) with namespace ConfigMap
// values and optional AgentRuntime CR overrides.
type ResolvedConfig struct {
	// Platform config (images, ports, resources) — from PlatformConfig
	Platform *config.PlatformConfig

	// Identity — merged from namespace CMs + AgentRuntime overrides
	KeycloakURL                string
	KeycloakRealm              string
	AdminCredentialsSecretName string // Secret name for KEYCLOAK_ADMIN_USERNAME/PASSWORD (default: "keycloak-admin-secret")
	SpireEnabled               string
	SpiffeTrustDomain          string
	PlatformClientIDs          string

	// Token exchange — from namespace CMs (not overridable by AgentRuntime v1alpha1)
	TokenURL              string
	Issuer                string
	ExpectedAudience      string
	AllowedAudiences      []string // from AgentRuntime .spec.identity.allowedAudiences or namespace CM
	TargetAudience        string
	TargetScopes          string
	DefaultOutboundPolicy string
	ClientAuthType        string // "client-secret" or "federated-jwt"
	SpiffeIdpAlias        string // Keycloak SPIFFE Identity Provider alias

	// Sidecar configs — from namespace CMs (not overridable by AgentRuntime v1alpha1)
	SpiffeHelperConf    string
	EnvoyYAML           string // empty = use template
	AuthproxyRoutesYAML string

	// AuthBridge runtime config — from namespace "authbridge-runtime-config" ConfigMap
	AuthBridgeRuntimeYAML string // raw config.yaml (base for per-agent ConfigMap)

	// AuthBridgeMode and MTLSMode are the resolved values from the chain
	// CR > namespace ConfigMap > default. They're populated alongside the
	// raw AuthBridgeRuntimeYAML so callers (e.g. RenderEnvoyConfig) can
	// branch on the resolved values without re-parsing the YAML.
	// AuthBridgeMode is "" when no source set it (caller picks the default).
	// MTLSMode is "" when no source set it (caller treats as "disabled").
	AuthBridgeMode string
	MTLSMode       string
}

// ResolveConfig merges all three configuration layers into a single ResolvedConfig.
// Merge precedence (highest wins): AgentRuntime > namespace CMs > platform defaults.
func ResolveConfig(platform *config.PlatformConfig, ns *NamespaceConfig, ar *AgentRuntimeOverrides) *ResolvedConfig {
	if platform == nil {
		platform = config.CompiledDefaults()
	}
	if ns == nil {
		ns = &NamespaceConfig{}
	}

	resolved := &ResolvedConfig{
		Platform: platform,

		// Start with namespace CM values
		KeycloakURL:                ns.KeycloakURL,
		KeycloakRealm:              ns.KeycloakRealm,
		AdminCredentialsSecretName: KeycloakAdminSecretName,
		SpireEnabled:               ns.SpireEnabled,
		SpiffeTrustDomain:          platform.Spiffe.TrustDomain,
		PlatformClientIDs:          ns.PlatformClientIDs,
		TokenURL:                   ns.TokenURL,
		Issuer:                     ns.Issuer,
		ExpectedAudience:           ns.ExpectedAudience,
		TargetAudience:             ns.TargetAudience,
		TargetScopes:               ns.TargetScopes,
		DefaultOutboundPolicy:      ns.DefaultOutboundPolicy,
		ClientAuthType:             ns.ClientAuthType,
		SpiffeIdpAlias:             ns.SpiffeIdpAlias,
		SpiffeHelperConf:           ns.SpiffeHelperConf,
		EnvoyYAML:                  ns.EnvoyYAML,
		AuthproxyRoutesYAML:        ns.AuthproxyRoutesYAML,
		AuthBridgeRuntimeYAML:      ns.AuthBridgeRuntimeYAML,
	}

	// Apply AgentRuntime identity overrides (highest precedence)
	if ar != nil {
		if len(ar.AllowedAudiences) > 0 {
			resolved.AllowedAudiences = ar.AllowedAudiences
		}
		if ar.SpiffeTrustDomain != nil {
			resolved.SpiffeTrustDomain = *ar.SpiffeTrustDomain
		}
		if ar.ClientRegistrationRealm != nil {
			resolved.KeycloakRealm = *ar.ClientRegistrationRealm
		}
	}

	// Resolve AuthBridgeMode + MTLSMode along the same CR > namespace > ""
	// chain that pod_mutator uses. Keep this resolution local to
	// ResolveConfig so consumers (e.g. RenderEnvoyConfig) can read the
	// already-merged values straight off ResolvedConfig instead of
	// re-implementing the chain.
	if ar != nil && ar.AuthBridgeMode != nil {
		resolved.AuthBridgeMode = *ar.AuthBridgeMode
	} else if m := ExtractMode(resolved.AuthBridgeRuntimeYAML); m != "" {
		resolved.AuthBridgeMode = m
	}
	if ar != nil && ar.MTLSMode != nil {
		resolved.MTLSMode = *ar.MTLSMode
	} else if m := ExtractMTLSMode(resolved.AuthBridgeRuntimeYAML); m != "" {
		resolved.MTLSMode = m
	}

	return resolved
}
