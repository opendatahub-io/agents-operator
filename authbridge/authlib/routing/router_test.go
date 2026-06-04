package routing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_ExactMatch(t *testing.T) {
	r, err := NewRouter("passthrough", []Route{
		{Host: "auth-target-service", Audience: "auth-target", Scopes: "openid"},
	})
	if err != nil {
		t.Fatal(err)
	}

	resolved := r.Resolve("auth-target-service")
	if resolved == nil {
		t.Fatal("expected match")
	}
	if !resolved.Matched {
		t.Error("expected Matched=true for explicit route match")
	}
	if resolved.Audience != "auth-target" {
		t.Errorf("audience = %q, want %q", resolved.Audience, "auth-target")
	}
	if resolved.Scopes != "openid" {
		t.Errorf("scopes = %q, want %q", resolved.Scopes, "openid")
	}
}

func TestResolve_GlobMatch(t *testing.T) {
	r, _ := NewRouter("passthrough", []Route{
		{Host: "*.example.com", Audience: "example"},
	})

	if resolved := r.Resolve("api.example.com"); resolved == nil || resolved.Audience != "example" {
		t.Error("expected glob match for api.example.com")
	}
	// Single-level glob should NOT match nested
	if resolved := r.Resolve("api.sub.example.com"); resolved != nil {
		t.Error("single glob should not match nested subdomain")
	}
}

func TestResolve_PortStripping(t *testing.T) {
	r, _ := NewRouter("passthrough", []Route{
		{Host: "service", Audience: "svc"},
	})
	if resolved := r.Resolve("service:8081"); resolved == nil || resolved.Audience != "svc" {
		t.Error("expected match after port stripping")
	}
}

func TestResolve_FirstMatchWins(t *testing.T) {
	r, _ := NewRouter("passthrough", []Route{
		{Host: "service", Audience: "first"},
		{Host: "service", Audience: "second"},
	})
	resolved := r.Resolve("service")
	if resolved == nil || resolved.Audience != "first" {
		t.Error("expected first-match-wins")
	}
}

func TestResolve_NoMatch_Passthrough(t *testing.T) {
	r, _ := NewRouter("passthrough", []Route{
		{Host: "known-service", Audience: "known"},
	})
	if resolved := r.Resolve("unknown-service"); resolved != nil {
		t.Error("expected nil for unmatched host with passthrough default")
	}
}

func TestResolve_NoMatch_Exchange(t *testing.T) {
	r, _ := NewRouter("exchange", []Route{})
	resolved := r.Resolve("any-service")
	if resolved == nil {
		t.Fatal("expected non-nil for exchange default")
	}
	if resolved.Matched {
		t.Error("expected Matched=false for default action fallback")
	}
	if resolved.Passthrough {
		t.Error("expected passthrough=false for exchange default")
	}
}

func TestResolve_PassthroughAction(t *testing.T) {
	r, _ := NewRouter("passthrough", []Route{
		{Host: "internal-svc", Action: "passthrough"},
	})
	resolved := r.Resolve("internal-svc")
	if resolved == nil || !resolved.Passthrough {
		t.Error("expected passthrough=true for passthrough action")
	}
}

func TestNewRouter_InvalidPattern(t *testing.T) {
	_, err := NewRouter("passthrough", []Route{
		{Host: "[invalid"},
	})
	if err == nil {
		t.Error("expected error for invalid glob pattern")
	}
}

func TestLoadRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.yaml")
	content := `- host: "auth-target"
  target_audience: "auth-target"
  token_scopes: "openid"
- host: "internal"
  action: "passthrough"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	routes, err := LoadRoutes(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}
	if routes[0].Audience != "auth-target" {
		t.Errorf("route[0].Audience = %q, want %q", routes[0].Audience, "auth-target")
	}
}

func TestLoadRoutes_FileNotFound(t *testing.T) {
	routes, err := LoadRoutes("/nonexistent/routes.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if routes != nil {
		t.Error("expected nil routes for missing file")
	}
}
