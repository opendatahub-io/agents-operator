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
	"fmt"
	"strconv"
	"strings"

	"github.com/kagenti/operator/internal/webhook/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var builderLog = logf.Log.WithName("container-builder")

const (
	// Container names for AuthBridge sidecars
	EnvoyProxyContainerName = "envoy-proxy"
	ProxyInitContainerName  = "proxy-init"

	SharedVolumesFSGroup = 0
)

// ContainerBuilder creates container specs from resolved config.
// It supports two modes:
//   - Legacy mode: constructed with NewContainerBuilder(platformConfig) — uses
//     ValueFrom refs for env vars (backward compatible)
//   - Resolved mode: constructed with NewResolvedContainerBuilder(resolvedConfig)
//     — uses literal env var values read at admission time
type ContainerBuilder struct {
	cfg      *config.PlatformConfig
	resolved *ResolvedConfig
}

// NewContainerBuilder creates a ContainerBuilder that uses ValueFrom refs
// for environment variables (legacy behavior).
func NewContainerBuilder(cfg *config.PlatformConfig) *ContainerBuilder {
	if cfg == nil {
		cfg = config.CompiledDefaults()
	}
	return &ContainerBuilder{cfg: cfg}
}

// NewResolvedContainerBuilder creates a ContainerBuilder that uses literal
// env var values from the resolved config (admission-time resolution).
func NewResolvedContainerBuilder(resolved *ResolvedConfig) *ContainerBuilder {
	if resolved == nil {
		resolved = ResolveConfig(nil, nil, nil)
	}
	return &ContainerBuilder{
		cfg:      resolved.Platform,
		resolved: resolved,
	}
}

// BuildEnvoyProxyContainer creates the envoy-proxy sidecar container with SPIRE enabled (default).
func (b *ContainerBuilder) BuildEnvoyProxyContainer() corev1.Container {
	return b.BuildEnvoyProxyContainerWithSpireOption(true)
}

// BuildEnvoyProxyContainerWithSpireOption creates the envoy-proxy sidecar container.
// When spireEnabled is true, the svid-output volume is mounted (read-only) so the
// go-processor can read the SPIFFE JWT SVID for use as a subject token in RFC 8693
// token exchange on outbound requests.
func (b *ContainerBuilder) BuildEnvoyProxyContainerWithSpireOption(spireEnabled bool) corev1.Container {
	builderLog.Info("building EnvoyProxy Container", "spireEnabled", spireEnabled)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "envoy-config",
			MountPath: "/etc/envoy",
			ReadOnly:  true,
		},
		{
			Name:      "shared-data",
			MountPath: "/shared",
			ReadOnly:  true,
		},
		{
			Name:      "authproxy-routes",
			MountPath: "/etc/authproxy",
			ReadOnly:  true,
		},
		{
			Name:      "authbridge-runtime-config",
			MountPath: "/etc/authbridge",
			ReadOnly:  true,
		},
	}
	if spireEnabled {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "svid-output",
			MountPath: "/opt",
			ReadOnly:  true,
		})
	}

	if spireEnabled {
		// authbridge-envoy bundles spiffe-helper; the entrypoint reads
		// helper.conf from this mount. Without it, the bundled
		// spiffe-helper would fail to start on SPIRE_ENABLED=true.
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "spiffe-helper-config",
			MountPath: "/etc/spiffe-helper",
			ReadOnly:  true,
		})
	}

	var env []corev1.EnvVar
	if b.resolved != nil {
		env = b.buildEnvoyProxyEnvResolved()
	} else {
		env = b.buildEnvoyProxyEnvLegacy()
	}
	// SPIRE_ENABLED gates the bundled spiffe-helper inside the
	// combined image's entrypoint. Always set explicitly so the
	// container's behavior is deterministic regardless of the image's
	// own default.
	env = append(env, corev1.EnvVar{
		Name:  "SPIRE_ENABLED",
		Value: spireEnabledStr(spireEnabled),
	})

	return corev1.Container{
		Name:            EnvoyProxyContainerName,
		Image:           b.cfg.Images.EnvoyProxy,
		ImagePullPolicy: b.cfg.Images.PullPolicy,
		Args:            []string{"--config", "/etc/authbridge/config.yaml"},
		Resources:       b.cfg.Resources.EnvoyProxy,
		Ports: []corev1.ContainerPort{
			{
				Name:          "envoy-outbound",
				ContainerPort: b.cfg.Proxy.Port,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "envoy-inbound",
				ContainerPort: b.cfg.Proxy.InboundProxyPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "envoy-admin",
				ContainerPort: b.cfg.Proxy.AdminPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "ext-proc",
				ContainerPort: 9090,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env: env,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(b.cfg.Proxy.UID),
			RunAsGroup:               ptr.To(b.cfg.Proxy.UID),
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
		},
		VolumeMounts: volumeMounts,
	}
}

func spireEnabledStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// BuildProxySidecarContainer creates a combined authbridge container for proxy-sidecar mode.
// Uses the authbridge image (authbridge-proxy + spiffe-helper bundled, no Envoy).
// The app uses HTTP_PROXY env vars to route outbound traffic through the forward proxy.
// Inbound traffic goes through the reverse proxy.
func (b *ContainerBuilder) BuildProxySidecarContainer(spireEnabled bool) corev1.Container {
	return b.BuildProxySidecarContainerWithPorts(spireEnabled, 8080, 8000, 8081)
}

// BuildProxySidecarContainerWithPorts creates a proxy-sidecar container with dynamic ports.
// reverseProxyPort: where the reverse proxy listens (takes over the agent's original port)
// agentBackendPort: where the agent actually listens (moved to a free port)
// forwardProxyPort: where the forward proxy listens (HTTP_PROXY target)
func (b *ContainerBuilder) BuildProxySidecarContainerWithPorts(spireEnabled bool, reverseProxyPort, agentBackendPort, forwardProxyPort int32) corev1.Container {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "shared-data",
			MountPath: "/shared",
		},
		{
			Name:      "authbridge-runtime-config",
			MountPath: "/etc/authbridge",
			ReadOnly:  true,
		},
		{
			Name:      AuthproxyRoutesConfigMapName,
			MountPath: "/etc/authproxy",
			ReadOnly:  true,
		},
	}
	if spireEnabled {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "svid-output",
				MountPath: "/opt",
			},
			// authbridge bundles spiffe-helper; the entrypoint reads
			// helper.conf from this mount. Without it, the bundled
			// spiffe-helper would fail to start on SPIRE_ENABLED=true.
			corev1.VolumeMount{
				Name:      "spiffe-helper-config",
				MountPath: "/etc/spiffe-helper",
				ReadOnly:  true,
			},
		)
	}

	return corev1.Container{
		Name:            AuthBridgeProxyContainerName,
		Image:           b.cfg.Images.AuthBridge,
		ImagePullPolicy: b.cfg.Images.PullPolicy,
		Args: []string{
			"--config", "/etc/authbridge/config.yaml",
		},
		Env: []corev1.EnvVar{
			// Gates the bundled spiffe-helper inside the combined image.
			{Name: "SPIRE_ENABLED", Value: spireEnabledStr(spireEnabled)},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "reverse-proxy",
				ContainerPort: reverseProxyPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "forward-proxy",
				ContainerPort: forwardProxyPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Resources: b.cfg.Resources.AuthBridge,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(1001)),
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
		},
		VolumeMounts: volumeMounts,
	}
}

// buildEnvoyProxyEnvResolved returns literal env vars from resolved config.
func (b *ContainerBuilder) buildEnvoyProxyEnvResolved() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "KEYCLOAK_URL", Value: b.resolved.KeycloakURL},
		{Name: "KEYCLOAK_REALM", Value: b.resolved.KeycloakRealm},
		{Name: "TOKEN_URL", Value: b.resolved.TokenURL},
		{Name: "ISSUER", Value: b.resolved.Issuer},
		{Name: "EXPECTED_AUDIENCE", Value: b.resolved.ExpectedAudience},
		{Name: "TARGET_AUDIENCE", Value: b.resolved.TargetAudience},
		{Name: "TARGET_SCOPES", Value: b.resolved.TargetScopes},
		{Name: "CLIENT_ID_FILE", Value: "/shared/client-id.txt"},
		{Name: "CLIENT_SECRET_FILE", Value: "/shared/client-secret.txt"},
		{Name: "ROUTES_CONFIG_PATH", Value: "/etc/authproxy/routes.yaml"},
		{Name: "DEFAULT_OUTBOUND_POLICY", Value: b.resolved.DefaultOutboundPolicy},
	}
}

// buildEnvoyProxyEnvLegacy returns ValueFrom-based env vars (backward compat).
func (b *ContainerBuilder) buildEnvoyProxyEnvLegacy() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "KEYCLOAK_URL",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: AuthBridgeConfigMapName},
					Key:                  "KEYCLOAK_URL",
					Optional:             ptr.To(true),
				},
			},
		},
		{
			Name: "KEYCLOAK_REALM",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: AuthBridgeConfigMapName},
					Key:                  "KEYCLOAK_REALM",
					Optional:             ptr.To(true),
				},
			},
		},
		{
			Name: "TOKEN_URL",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "authbridge-config"},
					Key:                  "TOKEN_URL",
					Optional:             ptr.To(true),
				},
			},
		},
		{
			Name: "ISSUER",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "authbridge-config"},
					Key:                  "ISSUER",
					Optional:             ptr.To(false),
				},
			},
		},
		{
			Name: "EXPECTED_AUDIENCE",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "authbridge-config"},
					Key:                  "EXPECTED_AUDIENCE",
					Optional:             ptr.To(true),
				},
			},
		},
		{
			Name: "TARGET_AUDIENCE",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "authbridge-config"},
					Key:                  "TARGET_AUDIENCE",
					Optional:             ptr.To(true),
				},
			},
		},
		{
			Name: "TARGET_SCOPES",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "authbridge-config"},
					Key:                  "TARGET_SCOPES",
					Optional:             ptr.To(true),
				},
			},
		},
		{
			Name:  "CLIENT_ID_FILE",
			Value: "/shared/client-id.txt",
		},
		{
			Name:  "CLIENT_SECRET_FILE",
			Value: "/shared/client-secret.txt",
		},
		{
			Name:  "ROUTES_CONFIG_PATH",
			Value: "/etc/authproxy/routes.yaml",
		},
		{
			Name: "DEFAULT_OUTBOUND_POLICY",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "authbridge-config"},
					Key:                  "DEFAULT_OUTBOUND_POLICY",
					Optional:             ptr.To(true),
				},
			},
		},
	}
}

// BuildProxyInitContainer creates the init container that sets up iptables
// to redirect outbound traffic to the Envoy proxy.
//
// SECURITY NOTE: This init container requires elevated capabilities:
//   - RunAsUser: 0 (root) - Required to modify network namespace iptables rules
//   - RunAsNonRoot: false - Explicitly allows root execution
//   - NET_ADMIN capability - Required for iptables manipulation
//   - NET_RAW capability - Required for raw socket operations used by iptables
//
// The init container does NOT require privileged mode. It uses DNAT to the pod's
// own IP instead of REDIRECT for the ztunnel inbound interception rule, which
// avoids the need for sysctl route_localnet=1 (which would require privileged
// mode to write to read-only /proc/sys). All other capabilities are dropped.
//
// Risk mitigations:
//   - This runs as an init container (not a long-running sidecar), limiting exposure window
//   - The container exits immediately after configuring iptables rules
//   - Minimal resource limits are applied (10m CPU, 10Mi memory)
//   - Only NET_ADMIN and NET_RAW capabilities are granted (all others dropped)
//   - The container image should be regularly updated and scanned for vulnerabilities
//
// mandatoryOutboundExclude is always prepended so that Keycloak traffic
// (port 8080) is never intercepted by Envoy.
const mandatoryOutboundExclude = "8080"

// BuildProxyInitContainer creates the proxy-init container.
// outboundPortsExclude is a comma-separated list of additional ports to
// exclude from outbound interception (mandatory 8080 is always included).
// inboundPortsExclude is a comma-separated list of ports to exclude from
// inbound interception (only set when non-empty). Both come from the
// kagenti.io/outbound-ports-exclude and kagenti.io/inbound-ports-exclude
// pod annotations.
func (b *ContainerBuilder) BuildProxyInitContainer(outboundPortsExclude, inboundPortsExclude string) corev1.Container {
	outboundValue := buildOutboundExcludeValue(outboundPortsExclude)
	inboundValue := buildPortExcludeValue(inboundPortsExclude, "inbound-ports-exclude")

	builderLog.Info("building ProxyInit Container",
		"resolvedOutboundPortsExclude", outboundValue,
		"resolvedInboundPortsExclude", inboundValue)

	env := []corev1.EnvVar{
		{
			Name:  "PROXY_PORT",
			Value: fmt.Sprintf("%d", b.cfg.Proxy.Port),
		},
		{
			Name:  "INBOUND_PROXY_PORT",
			Value: fmt.Sprintf("%d", b.cfg.Proxy.InboundProxyPort),
		},
		{
			Name:  "PROXY_UID",
			Value: fmt.Sprintf("%d", b.cfg.Proxy.UID),
		},
		{
			Name:  "OUTBOUND_PORTS_EXCLUDE",
			Value: outboundValue,
		},
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
	}
	if inboundValue != "" {
		env = append(env, corev1.EnvVar{
			Name:  "INBOUND_PORTS_EXCLUDE",
			Value: inboundValue,
		})
	}

	return corev1.Container{
		Name:            ProxyInitContainerName,
		Image:           b.cfg.Images.ProxyInit,
		ImagePullPolicy: b.cfg.Images.PullPolicy,
		Resources:       b.cfg.Resources.ProxyInit,
		Env:             env,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:    ptr.To(int64(0)),
			RunAsNonRoot: ptr.To(false),
			Privileged:   ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Add:  []corev1.Capability{"NET_ADMIN", "NET_RAW"},
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}
}

// validateAndDeduplicatePorts parses a comma-separated port string, validates
// each token (numeric, 1-65535), deduplicates, and returns the clean list.
// initialPorts are prepended and excluded from duplicates.
func validateAndDeduplicatePorts(raw, annotationName string, initialPorts []string) []string {
	seen := map[string]bool{}
	ports := make([]string, 0, len(initialPorts)+4)
	for _, p := range initialPorts {
		seen[p] = true
		ports = append(ports, p)
	}

	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		p, err := strconv.Atoi(tok)
		if err != nil || p < 1 || p > 65535 {
			builderLog.V(0).Info("WARNING: ignoring invalid port in "+annotationName+" annotation", "value", tok)
			continue
		}
		normalized := strconv.Itoa(p)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		ports = append(ports, normalized)
	}
	return ports
}

// buildOutboundExcludeValue merges the mandatory 8080 with validated
// user-supplied ports. Invalid tokens (non-numeric, out of range) are
// silently dropped and logged. Duplicates of 8080 are removed.
func buildOutboundExcludeValue(extra string) string {
	if extra == "" {
		return mandatoryOutboundExclude
	}
	return strings.Join(validateAndDeduplicatePorts(extra, "outbound-ports-exclude", []string{mandatoryOutboundExclude}), ",")
}

// buildPortExcludeValue validates and deduplicates a comma-separated port
// list. Returns "" when the input is empty. Used for inbound port exclusion
// where there is no mandatory port.
func buildPortExcludeValue(raw, annotationName string) string {
	if raw == "" {
		return ""
	}
	return strings.Join(validateAndDeduplicatePorts(raw, annotationName, nil), ",")
}
