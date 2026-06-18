package config

import "testing"

func TestDefaultFeatureGates_TLSBridgeOff(t *testing.T) {
	g := DefaultFeatureGates()
	if g.TLSBridge {
		t.Errorf("TLSBridge feature gate must default to false")
	}
}
