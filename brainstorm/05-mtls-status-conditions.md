# Brainstorm: mTLS Status Conditions on AgentRuntime

**Date:** 2026-06-02
**Status:** active
**Jira Story:** [RHAIENG-4928](https://redhat.atlassian.net/browse/RHAIENG-4928) - Fallback to plaintext HTTP when SPIRE unavailable, report in status conditions
**Parent Epic:** [RHAIENG-4921](https://redhat.atlassian.net/browse/RHAIENG-4921) - Enable Mutual TLS for Agent Communication

## Problem Framing

RHAIENG-4928 requires the operator to report TLS status on AgentRuntime so operators can see which agents are running with or without mTLS. Today, the controller falls back to plaintext HTTP when SPIRE is unavailable (via `DefaultFetcher` in `fetcher.go`), but this is **invisible**: no status condition, no log, no signal to the platform user.

Additionally, there are two separate mTLS dimensions that need status reporting:

1. **Control-plane mTLS** (observed): Whether the controller's agent card fetch used mTLS or plaintext. The controller knows this because it either uses `SpiffeFetcher` (mTLS) or `DefaultFetcher` (plaintext).

2. **Data-plane mTLS** (configured): Whether the authbridge sidecar is configured for mTLS between agents. The operator knows this because it injects the sidecar with the `mtlsMode` from the CR spec.

Neither dimension is currently surfaced in status conditions.

## Approaches Considered

### A: Two separate conditions on AgentRuntime (Chosen)

Add two distinct status conditions to AgentRuntime:

**`ControlPlaneMTLS`** (observed state from verified fetch):
- `True` / reason=`mTLS` / message="Peer SPIFFE ID: spiffe://trust-domain/ns/default/sa/agent"
- `False` / reason=`PlainHTTP` / message="SPIRE unavailable, using plaintext HTTP"
- `False` / reason=`Disabled` / message="Verified fetch not enabled"

**`DataPlaneMTLS`** (configured state from sidecar injection):
- `True` / reason=`Strict` / message="mTLS strict mode configured"
- `True` / reason=`Permissive` / message="mTLS permissive mode, accepts both TLS and plaintext"
- `False` / reason=`Disabled` / message="mTLS disabled"
- `False` / reason=`NoSidecar` / message="No authbridge sidecar injected"

- Pros: Granular, maps cleanly to two separate concerns, operators can alert on either independently, clear observed-vs-configured semantics
- Cons: Two conditions to manage, slightly more implementation work

### B: Single compound condition

One `TLSStatus` condition combining both dimensions with compound reasons (e.g., `FullMTLS`, `PartialMTLS`, `NoMTLS`).

- Pros: Single condition, simpler to query
- Cons: Compound state harder to alert on, conflates observed and configured state

### C: Split across objects

`DataPlaneMTLS` on AgentRuntime, fetch verification status on AgentCard (extending existing `Verified` condition and `AttestedAgentSpiffeID`).

- Pros: Follows existing object ownership model
- Cons: Splits mTLS visibility across two objects, operators need to check both

## Decision

**Approach A: Two separate conditions on AgentRuntime.** Keeps all mTLS status in one place with clear operational semantics. Each condition answers a distinct question:
- "Is the controller talking securely to this agent?" (ControlPlaneMTLS)
- "Are other agents talking securely to this agent?" (DataPlaneMTLS)

## Key Requirements

### ControlPlaneMTLS condition

- **Set on every reconcile** that triggers an agent card fetch
- **Source**: The `SpiffeFetcher.FetchAuthenticated()` return value. If it succeeds, mTLS was used. If it falls back to `DefaultFetcher`, plaintext was used.
- **SPIFFE ID propagation**: On mTLS success, include the peer SPIFFE ID in the condition message. This already exists as `AttestedAgentSpiffeID` on AgentCard; the condition message provides human-readable status.
- **Disabled case**: When `--enable-verified-fetch` is false, set `False`/`Disabled`. This is the default in dev environments without SPIRE.
- **Transition**: When SPIRE becomes available (e.g., SPIRE deployed, controller restarted), the next reconcile updates the condition from `PlainHTTP` to `mTLS`.

### DataPlaneMTLS condition

- **Set by the webhook injector** during sidecar injection, reflected on the AgentRuntime during reconcile
- **Source**: The resolved `mtlsMode` from the injection config chain (cluster defaults, namespace ConfigMap, CR override)
- **NoSidecar case**: When `authBridgeMode` is not set or sidecar injection is disabled
- **Not an observed state**: The operator configures the mode but cannot verify TLS handshakes are happening. The condition reports "configured" not "verified". The message should make this clear.

### Implementation scope (proxy-sidecar mode only)

- Only proxy-sidecar authBridgeMode is in scope
- Envoy-sidecar and lite modes may follow the same pattern later but are not part of this spec
- The condition names and semantics should be mode-agnostic (they work for any authBridgeMode)

### Warning logging

- RHAIENG-4928 requires "warning log emitted on each plaintext fetch"
- The controller should log at warning level when falling back to plaintext, including the agent name and namespace
- This exists partially (SPIRE unavailable is logged) but per-fetch warning may be missing

## Open Questions

- Should `ControlPlaneMTLS` be set on AgentRuntime or AgentCard? The story says AgentRuntime, and Approach A chose AgentRuntime, but `AttestedAgentSpiffeID` already lives on AgentCard. Duplicating SPIFFE ID info across both objects needs consideration.
- What happens to the condition when verified fetch is enabled but SPIRE is temporarily unavailable (e.g., SPIRE agent pod restart)? Should the condition flap, or should there be a grace period?
- How does `DataPlaneMTLS` interact with the existing `Injected` condition on AgentRuntime (if one exists)? Need to check for overlapping status reporting.
- Should the SPIFFE ID in the `ControlPlaneMTLS` message be the full SPIFFE URI or just the workload identifier portion?
