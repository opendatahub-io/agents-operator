package config

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// CompiledDefaults returns hardcoded defaults used when no config is provided
func CompiledDefaults() *PlatformConfig {
	return &PlatformConfig{
		// Compiled defaults are overridden at runtime by the platform-config
		// ConfigMap (kagenti-platform-config). These serve as fallbacks only.
		Images: ImageConfig{
			// authbridge-envoy: combined image for envoy-sidecar mode
			// (Envoy + ext_proc authbridge + spiffe-helper bundled).
			EnvoyProxy: "ghcr.io/kagenti/kagenti-extensions/authbridge-envoy:latest",
			// authbridge: combined image for proxy-sidecar mode (default
			// deployment shape) — authbridge-proxy + spiffe-helper
			// bundled, no Envoy, no gRPC.
			AuthBridge: "ghcr.io/kagenti/kagenti-extensions/authbridge:latest",
			// authbridge-lite: size-optimized variant for the "lite"
			// mode. Same listener layout as AuthBridge but parsers
			// (a2a/mcp/inference) are dropped.
			AuthBridgeLite: "ghcr.io/kagenti/kagenti-extensions/authbridge-lite:latest",
			// proxy-init: iptables init container, used by
			// envoy-sidecar mode only.
			ProxyInit:  "ghcr.io/kagenti/kagenti-extensions/proxy-init:latest",
			PullPolicy: corev1.PullIfNotPresent,
		},
		Proxy: ProxyConfig{
			Port:             15123,
			UID:              1337,
			InboundProxyPort: 15124,
			AdminPort:        9901,
		},
		Resources: ResourcesConfig{
			EnvoyProxy: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
			ProxyInit: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("10Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("10Mi"),
				},
			},
			AuthBridge: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("300m"),
					corev1.ResourceMemory: resource.MustParse("384Mi"),
				},
			},
		},
		TokenExchange: TokenExchangeDefaults{
			DefaultScopes: []string{"openid"},
		},
		Spiffe: SpiffeConfig{
			TrustDomain: "cluster.local",
			SocketPath:  "unix:///spiffe-workload-api/spire-agent.sock",
		},
		Observability: ObservabilityConfig{
			LogLevel:      "info",
			EnableMetrics: true,
			EnableTracing: false,
		},
	}
}
