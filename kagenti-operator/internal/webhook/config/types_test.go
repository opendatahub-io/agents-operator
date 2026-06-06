package config

import "testing"

// ClusterCIDRs drive the only in-cluster allowance in the always-on
// enforce-redirect guard, so they must always be present and valid — an empty
// list (silent 10/8 fallback in the init script) or a malformed entry
// (init-container CrashLoop under set -e) must be rejected at config load.
func TestValidate_ClusterCIDRs(t *testing.T) {
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
			c := CompiledDefaults()
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

// TransparentPort is the REDIRECT target for the enforce-redirect guard; it must
// be a valid, non-privileged port (the proxy binds it as a non-root user).
func TestValidate_TransparentPort(t *testing.T) {
	tests := []struct {
		name    string
		port    int32
		wantErr bool
	}{
		{"default 8082 ok", 8082, false},
		{"zero rejected", 0, true},
		{"privileged rejected", 80, true},
		{"too large rejected", 70000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := CompiledDefaults()
			c.Proxy.TransparentPort = tt.port
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
