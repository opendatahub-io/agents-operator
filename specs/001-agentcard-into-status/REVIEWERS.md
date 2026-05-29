# Review Guide: Consolidate AgentCard Data Into AgentRuntime Status

**Generated**: 2026-05-21 | **Spec**: [spec.md](spec.md)

## Why This Change

AgentRuntime and AgentCard are two CRDs with identical cardinality, the same namespace, the same lifecycle, and the same owner. Operators currently need to cross-reference both resources to understand what an agent can do. AgentCard also conflates observation with policy (it's entirely controller-managed with no room for admin-authored policy fields), and its JWS signing pipeline signs a skeleton card with empty skills. This change consolidates the card data into AgentRuntime's status, reducing the API surface and simplifying the operator experience.

## What Changes

AgentRuntime gains a new `status.card` field that holds the A2A agent card payload (name, description, skills, capabilities), fetch metadata (timestamp, content hash, protocol), and identity verification results (SPIFFE ID, JWS signature validation). The controller fetches the card from `/.well-known/agent-card.json` on rollout events. mTLS verified fetch is included, reusing the infrastructure from PR #284. The entire feature is gated behind `--enable-card-discovery` (default: disabled). AgentCard CRD remains functional but emits deprecation warnings on new CR creation.

## How It Works

A new `fetchAndUpdateCard` phase is added to the existing AgentRuntime reconcile loop, after config hash computation. It resolves the agent's Service endpoint by name (matching existing convention) with selector-match fallback, fetches the card via the existing `agentcard.Fetcher` interface (plain HTTP) or `agentcard.AuthenticatedFetcher` (mTLS with SPIFFE), runs JWS signature verification via the existing `signature.Provider`, and writes the results to `status.card`. Re-fetch is triggered only by pod template hash changes, not periodic polling. The controller reuses all existing fetch, parse, and verify code from the AgentCard controller without creating new packages.

## When It Applies

**Applies when**:
- The operator is started with `--enable-card-discovery=true`
- An AgentRuntime targets a workload (Deployment, StatefulSet, or Sandbox) whose Pods serve `/.well-known/agent-card.json`
- The backing workload rolls out (pod template hash or generation changes)

**Does not apply when**:
- `--enable-card-discovery` is not set (default). No card fetch occurs.
- AgentRuntime targets a workload without an A2A card endpoint. The controller sets a `CardSynced=False` condition and moves on.
- mTLS policy fields, AgentCard CRD removal, and migration tooling are deferred to future iterations.

## Key Decisions

1. **Extend existing controller (not a new one)**: The card fetch is a single HTTP GET added to the existing reconcile loop. Creating a separate controller would add coordination complexity for minimal isolation benefit. If performance becomes an issue at scale, extraction is a clean refactor.

2. **Selector match for service resolution**: Resolves the workload's (Deployment, StatefulSet, Sandbox) Pod selector labels to find the matching Service. Falls back from name-based convention (matching AgentCard behavior) to selector matching. No annotations required.

3. **Retain stale data on fetch failure**: When a fetch fails, the last successful card data is kept in `status.card`. The `CardSynced` condition and `fetchedAt` timestamp signal staleness. This avoids disruption for tools that consume `status.card`.

4. **Clear data when feature flag is disabled**: When `--enable-card-discovery` is toggled off, `status.card` is cleared on the next reconcile. This prevents stale data from lingering when the feature is explicitly turned off.

5. **mTLS included in this iteration**: Rather than deferring mTLS to a follow-up, the recently merged PR #284 infrastructure is reused directly. This avoids building a plain-HTTP-only path that would be immediately replaced.

## Areas Needing Attention

- The `fetchAndUpdateCard` method adds network I/O (HTTP GET) to a controller that currently only does Kubernetes API calls. The existing 10s timeout from `doHTTPFetch` applies, but a slow or unreachable agent endpoint will delay the reconcile loop by up to 10s.
- Pod template hash change detection is the trigger mechanism. If the agent updates its card content without a pod rollout, the controller will not re-fetch. This is an intentional tradeoff (no polling) documented in the spec.
- The `CardStatus` struct embeds `AgentCardData` which includes the `Signatures` field (JWS array). For agents that sign their cards, this means the raw JWS data is stored in AgentRuntime status, which could be large.
- Service resolution assumes at most one Service matches a Deployment's selector. Multiple matching Services will use the first one found, which may not be deterministic.

## Open Questions

No open questions identified. All clarifications were resolved during the spec clarification phase.

## Review Checklist

- [ ] Key decisions are justified
- [ ] Breaking changes are documented with migration guidance
- [ ] Scope matches the stated boundaries
- [ ] Success criteria are achievable
- [ ] No unstated assumptions
- [ ] `CardStatus` struct fields match the A2A agent card spec
- [ ] Feature flag default (disabled) is correct for backward compatibility
- [ ] Deprecation warning is non-disruptive (log + event, no behavior change)
- [ ] mTLS reuse from PR #284 is compatible with the AgentRuntime controller's lifecycle

---

<!-- Code phase sections are appended below this line by the phase-manager command -->
