package config

import "testing"

// When egressEnforcement is "enforce", ClusterCIDRs must be present and valid —
// an empty list (silent 10/8 fallback in the init script) or a malformed entry
// (init-container CrashLoop under set -e) must be rejected at config load.
func TestValidate_ClusterCIDRs_EnforceMode(t *testing.T) {
	base := func() *PlatformConfig {
		c := CompiledDefaults()
		c.Proxy.EgressEnforcement = EgressEnforcementEnforce
		return c
	}
	tests := []struct {
		name    string
		mutate  func(*PlatformConfig)
		wantErr bool
	}{
		{"default CIDRs ok", func(c *PlatformConfig) {}, false},
		{"empty CIDRs rejected", func(c *PlatformConfig) { c.Proxy.ClusterCIDRs = nil }, true},
		{"malformed CIDR rejected", func(c *PlatformConfig) {
			c.Proxy.ClusterCIDRs = []string{"10.0.0.0/8", "garbage"}
		}, true},
		{"valid OCP-shaped CIDRs ok", func(c *PlatformConfig) {
			c.Proxy.ClusterCIDRs = []string{"10.128.0.0/14", "172.30.0.0/16"}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.mutate(c)
			err := c.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

// When enforcement is off, ClusterCIDRs is unused and must not be validated.
func TestValidate_ClusterCIDRs_OffMode_NotValidated(t *testing.T) {
	c := CompiledDefaults()
	c.Proxy.EgressEnforcement = EgressEnforcementOff
	c.Proxy.ClusterCIDRs = []string{"garbage"}
	if err := c.Validate(); err != nil {
		t.Errorf("off mode must not validate ClusterCIDRs, got: %v", err)
	}
}
