package config

import (
	"fmt"
	"net"

	corev1 "k8s.io/api/core/v1"
)

// PlatformConfig represents the complete platform configuration
type PlatformConfig struct {
	Images        ImageConfig           `json:"images" yaml:"images"`
	Proxy         ProxyConfig           `json:"proxy" yaml:"proxy"`
	Resources     ResourcesConfig       `json:"resources" yaml:"resources"`
	TokenExchange TokenExchangeDefaults `json:"tokenExchange" yaml:"tokenExchange"`
	Spiffe        SpiffeConfig          `json:"spiffe" yaml:"spiffe"`
	Observability ObservabilityConfig   `json:"observability" yaml:"observability"`
}

type ImageConfig struct {
	// EnvoyProxy is the combined image for envoy-sidecar mode:
	// Envoy + authbridge (ext_proc) + spiffe-helper bundled.
	// Spiffe-helper starts conditionally based on SPIRE_ENABLED.
	EnvoyProxy string `json:"envoyProxy" yaml:"envoyProxy"`

	// AuthBridge is the combined image for proxy-sidecar mode (default):
	// authbridge-proxy + spiffe-helper bundled. No Envoy, no gRPC.
	// Spiffe-helper starts conditionally based on SPIRE_ENABLED.
	AuthBridge string `json:"authbridge" yaml:"authbridge"`

	// AuthBridgeLite is the size-optimized variant of AuthBridge:
	// authbridge-lite (jwt-validation + token-exchange only, parsers
	// dropped) + spiffe-helper bundled. Same listener layout as
	// AuthBridge, used for the "lite" mode.
	AuthBridgeLite string `json:"authbridgeLite" yaml:"authbridgeLite"`

	// ProxyInit is the iptables init container, used by envoy-sidecar
	// mode only.
	ProxyInit string `json:"proxyInit" yaml:"proxyInit"`

	PullPolicy corev1.PullPolicy `json:"pullPolicy" yaml:"pullPolicy"`
}

// EgressEnforcement values for ProxyConfig.EgressEnforcement.
const (
	EgressEnforcementOff     = "off"
	EgressEnforcementEnforce = "enforce"
)

type ProxyConfig struct {
	Port             int32 `json:"port" yaml:"port"`
	UID              int64 `json:"uid" yaml:"uid"`
	InboundProxyPort int32 `json:"inboundProxyPort" yaml:"inboundProxyPort"`
	AdminPort        int32 `json:"adminPort" yaml:"adminPort"`

	// EgressEnforcement controls the proxy-sidecar fail-closed egress guard.
	//   "off" (default): the workload routes egress through the forward proxy
	//     via HTTP_PROXY only — cooperative and bypassable.
	//   "enforce": a proxy-init container installs the enforce-drop iptables
	//     guard, forcing all external egress through the forward proxy regardless
	//     of whether the workload honors HTTP_PROXY.
	// envoy-sidecar mode is unaffected — it already enforces egress structurally
	// via the transparent redirect, so this knob is consulted only on the
	// proxy-sidecar / lite path. Migrate the default off->enforce in a future
	// release once ClusterCIDRs sourcing is validated across distros.
	EgressEnforcement string `json:"egressEnforcement" yaml:"egressEnforcement"`

	// ClusterCIDRs are the in-cluster ranges (pods / services / DNS) that the
	// enforce-drop guard allows direct; everything else egressing the pod is
	// dropped. The default 10.0.0.0/8 is Kind-shaped (pods 10.244/16 + services
	// 10.96/16). OCP/EKS MUST override this (e.g. OCP services 172.30.0.0/16,
	// pods 10.128.0.0/14 — 172.30/16 is outside 10/8) or in-cluster service
	// traffic will be dropped. Only used when EgressEnforcement == "enforce".
	ClusterCIDRs []string `json:"clusterCIDRs" yaml:"clusterCIDRs"`
}

type ResourcesConfig struct {
	EnvoyProxy corev1.ResourceRequirements `json:"envoyProxy" yaml:"envoyProxy"`
	ProxyInit  corev1.ResourceRequirements `json:"proxyInit" yaml:"proxyInit"`
	AuthBridge corev1.ResourceRequirements `json:"authbridge" yaml:"authbridge"`
}

type TokenExchangeDefaults struct {
	TokenURL        string   `json:"tokenUrl" yaml:"tokenUrl"`
	DefaultAudience string   `json:"defaultAudience" yaml:"defaultAudience"`
	DefaultScopes   []string `json:"defaultScopes" yaml:"defaultScopes"`
}

type SpiffeConfig struct {
	TrustDomain string `json:"trustDomain" yaml:"trustDomain"`
	SocketPath  string `json:"socketPath" yaml:"socketPath"`
}

type ObservabilityConfig struct {
	LogLevel       string `json:"logLevel" yaml:"logLevel"`
	EnableMetrics  bool   `json:"enableMetrics" yaml:"enableMetrics"`
	EnableTracing  bool   `json:"enableTracing" yaml:"enableTracing"`
	TracingBackend string `json:"tracingBackend" yaml:"tracingBackend"`
}

// DeepCopy creates a copy of the config
func (c *PlatformConfig) DeepCopy() *PlatformConfig {
	if c == nil {
		return nil
	}
	result := *c

	if c.TokenExchange.DefaultScopes != nil {
		result.TokenExchange.DefaultScopes = make([]string, len(c.TokenExchange.DefaultScopes))
		copy(result.TokenExchange.DefaultScopes, c.TokenExchange.DefaultScopes)
	}

	if c.Proxy.ClusterCIDRs != nil {
		result.Proxy.ClusterCIDRs = make([]string, len(c.Proxy.ClusterCIDRs))
		copy(result.Proxy.ClusterCIDRs, c.Proxy.ClusterCIDRs)
	}

	// Deep copy ResourceRequirements — ResourceList is a map that would be shared
	result.Resources.EnvoyProxy = deepCopyResourceRequirements(c.Resources.EnvoyProxy)
	result.Resources.ProxyInit = deepCopyResourceRequirements(c.Resources.ProxyInit)
	result.Resources.AuthBridge = deepCopyResourceRequirements(c.Resources.AuthBridge)

	return &result
}

func deepCopyResourceRequirements(rr corev1.ResourceRequirements) corev1.ResourceRequirements {
	out := corev1.ResourceRequirements{}
	if rr.Requests != nil {
		out.Requests = make(corev1.ResourceList, len(rr.Requests))
		for k, v := range rr.Requests {
			out.Requests[k] = v.DeepCopy()
		}
	}
	if rr.Limits != nil {
		out.Limits = make(corev1.ResourceList, len(rr.Limits))
		for k, v := range rr.Limits {
			out.Limits[k] = v.DeepCopy()
		}
	}
	return out
}

// Validate checks if the config is valid
func (c *PlatformConfig) Validate() error {
	if c.Proxy.Port < 1024 || c.Proxy.Port > 65535 {
		return fmt.Errorf("proxy.port must be between 1024 and 65535")
	}
	if c.Proxy.InboundProxyPort < 1024 || c.Proxy.InboundProxyPort > 65535 {
		return fmt.Errorf("proxy.inboundProxyPort must be between 1024 and 65535")
	}
	if c.Proxy.AdminPort < 1024 || c.Proxy.AdminPort > 65535 {
		return fmt.Errorf("proxy.adminPort must be between 1024 and 65535")
	}
	// The enforce-drop guard exempts this UID (--uid-owner) and the proxy
	// container runs as it; it must be a real non-root user.
	if c.Proxy.UID < 1 {
		return fmt.Errorf("proxy.uid must be >= 1 (got %d): the proxy must not run as root and the enforce-drop exemption keys on this UID", c.Proxy.UID)
	}
	switch c.Proxy.EgressEnforcement {
	case "", EgressEnforcementOff, EgressEnforcementEnforce:
	default:
		return fmt.Errorf("proxy.egressEnforcement must be \"off\" or \"enforce\" (got %q)", c.Proxy.EgressEnforcement)
	}
	// When enforce is on, ClusterCIDRs drive the only in-cluster allowance in the
	// enforce-drop guard. Validate them at load time so a misconfig fails fast with
	// a clear message rather than: (a) an empty list silently falling back to the
	// Kind-shaped 10.0.0.0/8 default in init-iptables.sh, or (b) a malformed entry
	// crashing the proxy-init container under `set -e` with a cryptic iptables error.
	if c.Proxy.EgressEnforcement == EgressEnforcementEnforce {
		if len(c.Proxy.ClusterCIDRs) == 0 {
			return fmt.Errorf("proxy.clusterCIDRs must be non-empty when proxy.egressEnforcement is %q (set the cluster's pod+service CIDRs)", EgressEnforcementEnforce)
		}
		// Syntactic validation only — overlapping ranges and IPv4/IPv6 mixing are
		// accepted (iptables handles both, and the init script splits v4/v6 itself);
		// we only reject malformed strings here.
		for _, cidr := range c.Proxy.ClusterCIDRs {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("proxy.clusterCIDRs entry %q is not a valid CIDR: %w", cidr, err)
			}
		}
	}
	if c.Images.EnvoyProxy == "" {
		return fmt.Errorf("images.envoyProxy is required")
	}
	if c.Images.AuthBridge == "" {
		return fmt.Errorf("images.authbridge is required")
	}
	if c.Images.AuthBridgeLite == "" {
		return fmt.Errorf("images.authbridgeLite is required")
	}
	if c.Images.ProxyInit == "" {
		return fmt.Errorf("images.proxyInit is required")
	}
	return nil
}
