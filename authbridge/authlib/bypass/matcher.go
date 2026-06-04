// Package bypass provides path pattern matching for skipping JWT validation
// on public endpoints (e.g., health probes, agent card discovery).
package bypass

import (
	"fmt"
	"path"
	"strings"
)

// DefaultPatterns are paths that skip inbound JWT validation by default.
var DefaultPatterns = []string{"/.well-known/*", "/healthz", "/readyz", "/livez"}

// Matcher checks request paths against a set of bypass patterns.
// Patterns use Go's path.Match syntax (e.g., "/.well-known/*").
// Note: path.Match's "*" does NOT cross "/" separators, so "/.well-known/*"
// matches "/.well-known/agent.json" but NOT "/.well-known/foo/bar".
type Matcher struct {
	patterns []string
}

// NewMatcher creates a Matcher from the given patterns.
// Returns an error if any pattern has invalid path.Match syntax.
func NewMatcher(patterns []string) (*Matcher, error) {
	for _, p := range patterns {
		if _, err := path.Match(p, "/"); err != nil {
			return nil, fmt.Errorf("invalid bypass pattern %q: %w", p, err)
		}
	}
	return &Matcher{patterns: patterns}, nil
}

// Match checks if the given request path matches any bypass pattern.
// Query strings are stripped and the path is normalized via path.Clean
// to prevent bypass via non-canonical forms (e.g., //healthz, /./healthz).
func (m *Matcher) Match(requestPath string) bool {
	if idx := strings.IndexByte(requestPath, '?'); idx >= 0 {
		requestPath = requestPath[:idx]
	}
	requestPath = path.Clean(requestPath)
	for _, pattern := range m.patterns {
		if matched, _ := path.Match(pattern, requestPath); matched {
			return true
		}
	}
	return false
}
