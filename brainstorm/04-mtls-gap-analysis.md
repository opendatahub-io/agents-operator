# Brainstorm: mTLS Architecture Gap Analysis (proxy-sidecar mode)

**Date:** 2026-06-02
**Status:** active
**Jira Epic:** [RHAIENG-4921](https://redhat.atlassian.net/browse/RHAIENG-4921) - Enable Mutual TLS for Agent Communication
**Jira Story:** [RHAIENG-4930](https://redhat.atlassian.net/browse/RHAIENG-4930) - Refinement doc (replaced by this spec)
**Parent Feature:** [RHAISTRAT-1599](https://redhat.atlassian.net/browse/RHAISTRAT-1599) - TP Productize & Downstream the Agent Operator

## Problem Framing

The mTLS epic (RHAIENG-4921) contains 8 implementation stories (RHAIENG-4922 through 4929) plus a refinement doc story (4930). Significant implementation has already landed upstream: envoy-sidecar wiring, go-spiffe verified fetch, authbridge modes (proxy-sidecar, envoy-sidecar, lite), ConfigMap-driven mode selection, identity binding, and SPIFFE-based DCR.

However, no single document captures what's implemented versus what's missing. The stories were written before the architecture settled, so they describe work that's partially done, done differently than described, or superseded by the architecture that emerged.

Instead of a refinement doc, we need a **gap analysis spec** that:
1. Documents the actual mTLS architecture as implemented
2. Identifies gaps against production readiness for RHOAI 3.5 EA2
3. Defines acceptance criteria for the remaining work
4. Covers CI/E2E infrastructure requirements

## Approaches Considered

### A: Component-oriented (Chosen)

Structure the spec around the three mTLS components: agent-side certs (spiffe-helper in proxy-sidecar), controller-side certs (go-spiffe verified fetch), and E2E verification (SPIRE-enabled CI). Each section documents current state, gaps, and acceptance criteria. An end-to-end flow diagram at the top provides context.

- Pros: Mirrors code boundaries, gaps map to actionable work items, clear ownership per section
- Cons: Doesn't give a single narrative of a TLS handshake end-to-end

### B: Flow-oriented

Structure as a certificate flow narrative from SPIRE server to TLS handshake to status reporting.

- Pros: Good for team alignment, tells the full story
- Cons: Cuts across code boundaries, harder to turn into work items

### C: Matrix-based

Organize around the `mtlsMode` enum (permissive, strict, disabled) as the axis.

- Pros: Compact, exhaustive behavior coverage
- Cons: Repetitive, doesn't cover cert lifecycle or infrastructure

## Decision

**Approach A: Component-oriented**, with an end-to-end flow diagram at the top for context.

## Key Requirements

### Scope

- **Mode**: proxy-sidecar only (the settled single-sidecar default; HTTP_PROXY env + authbridge-proxy + spiffe-helper bundled, no Envoy, no iptables)
- **Both paths**: agent-to-agent mTLS (data plane) AND controller-to-agent mTLS (verified fetch)
- **Transport only**: mTLS certificate lifecycle, TLS handshake, and fallback behavior. Documents the SPIFFE ID handoff to authbridge for token exchange but does not spec the token exchange flow itself (separate concern)
- **E2E + CI**: includes test scenarios AND CI infrastructure requirements (SPIRE-enabled test cluster)
- **Architecture-first**: derives requirements from the actual implemented architecture, not from the original Jira story descriptions

### Spec structure (three components)

1. **Agent-side cert lifecycle** (spiffe-helper in proxy-sidecar)
   - SVID acquisition from SPIRE agent
   - Cert file paths and rotation
   - `mtlsMode` behavior (permissive, strict, disabled)
   - Fallback when SPIRE is unavailable
   - Current state vs gaps

2. **Controller-side cert lifecycle** (go-spiffe verified fetch)
   - X509Source initialization from SPIRE Workload API
   - mTLS HTTP client for agent card fetches
   - Fallback to plaintext HTTP
   - Status condition reporting (TLSStatus on AgentRuntime)
   - Current state vs gaps

3. **E2E verification** (CI infrastructure + test scenarios)
   - SPIRE deployment in CI (kind/k3s cluster config)
   - Test scenarios: two-agent mTLS handshake, negative test (no SVID rejected), status condition assertions
   - Acceptance criteria per scenario
   - CI infrastructure requirements

### Source code references

- **Operator**: `/Users/rhuss/Development/ai/kagenti-operator/` (controller, webhook, CRDs, injector, envoy templates)
- **Authbridge**: `/Users/rhuss/Development/ai/kagenti-extensions/` (proxy-sidecar, token exchange, DCR)

### Boundary with authbridge token exchange

The mTLS layer provides the transport and extracts the peer's SPIFFE ID from the client certificate. Authbridge consumes this SPIFFE ID for Dynamic Client Registration (DCR) and token exchange. The spec documents this handoff interface but does not specify the token exchange flow, Keycloak integration, or OAuth2 semantics.

## Open Questions

- What is the exact current state of `mtlsMode` support in proxy-sidecar mode? The CRD defines permissive/strict/disabled but implementation coverage may vary.
- Does the controller's verified fetch fallback correctly set status conditions today, or is that a gap?
- What SPIRE version and configuration is assumed for the CI cluster?
- Are there cert rotation edge cases (SVID expiry during active connections) that need explicit coverage?
