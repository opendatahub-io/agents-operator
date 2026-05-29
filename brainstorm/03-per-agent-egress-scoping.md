# Brainstorm: Per-Agent Egress Scoping for AgentRuntime NetworkPolicies

**Date:** 2026-05-29
**Status:** active
**Issue:** [#385](https://github.com/kagenti/kagenti-operator/issues/385)
**Prior art:** [#224](https://github.com/kagenti/kagenti-operator/issues/224) (same feature, targeted AgentCard CRD)

## Problem Framing

The NetworkPolicy controller currently makes a binary egress decision based on agent verification status:

- **Verified agents:** allow-all egress (introduced in PR #221 as an OVN-Kubernetes workaround)
- **Unverified agents:** egress restricted to K8s API server only (port 6443, CIDRs auto-discovered)

Allow-all egress for verified agents violates the principle of least privilege. A compromised but verified agent could exfiltrate data to any destination. In enterprise multi-agent deployments, agents call specific external endpoints (LLM APIs, databases, internal services). Platform engineers should be able to declare which destinations an agent is allowed to reach.

## Approaches Considered

### A: Big-Bang Controller Replacement (selected)

Build a new `AgentRuntimeNetworkPolicyReconciler` as a direct replacement for `AgentCardNetworkPolicyReconciler`:

- Add `spec.networkPolicy.egress` to AgentRuntime CRD
- New controller watches AgentRuntime, owns NetworkPolicies
- Port full `ConditionVerified` evaluation into the AgentRuntime reconciler
- Always merge DNS + K8s API base rules with user-provided egress rules
- Remove the AgentCard NetworkPolicy controller

**Pros:** Clean cut, no coexistence complexity, single source of truth.
**Cons:** Larger PR, requires identity binding migration (brainstorm #02) to land first or concurrently.

### B: Phased Controller Migration

Ship in two PRs: first extend the existing AgentCard controller to cross-reference AgentRuntime for egress rules, then swap the controller entirely.

**Pros:** Smaller PRs, egress ships faster.
**Cons:** Temporary cross-resource coupling, more total code churn.

### C: Feature-Gated Parallel Controllers

Run both controllers simultaneously behind a feature gate.

**Pros:** Zero-downtime migration, operators control timing.
**Cons:** Most complex, risk of NetworkPolicy conflicts between two controllers.

## Decision

**Approach A: Big-Bang Controller Replacement.** The AgentRuntime controller already manages workload labels and has `status.card` with verification data. The identity binding migration (brainstorm #02) is a prerequisite anyway, so bundling the NetworkPolicy controller swap makes this a clean replacement.

## Key Design Decisions

### Egress config placement: AgentRuntime.spec field

Per-agent granularity, co-located with identity config. Matches the KubeRay pattern and aligns with the AgentCard deprecation. A separate policy CRD and namespace-level ConfigMap were rejected (CRD proliferation and lack of per-agent granularity, respectively).

### API shape: Standard K8s NetworkPolicy egress format

Reuse `netv1.NetworkPolicyEgressRule` directly. Portable across all CNIs (including OVN-Kubernetes), familiar to K8s users, and validated by the issue #224 evaluation. CIDR-based rules only, no FQDN support. Broad CIDRs (e.g., `0.0.0.0/0:443`) cover the primary use case of agents calling external APIs.

### Base rules: Always merge mandatory rules

The controller auto-injects DNS (port 53 UDP/TCP) and K8s API (port 6443, auto-discovered CIDRs) into every custom egress policy. Users cannot accidentally break their agents by omitting DNS. No opt-out mechanism.

### Verification signal: Replicate ConditionVerified logic

Port the full `ConditionVerified` evaluation (signature + identity binding + strict mode) into the AgentRuntime controller. Same semantics as the AgentCard controller, different home.

### Controller strategy: New controller replaces old

A new `AgentRuntimeNetworkPolicyReconciler` replaces `AgentCardNetworkPolicyReconciler` entirely. No coexistence period, no feature gate.

## Relationship to AgentMesh (ADR-0003)

The AgentMesh ADR (ODH-ADR-AgentOps-0003) argues that network topology, including external egress, is an application-level concern that belongs on a separate `AgentMesh` CRD rather than on individual AgentRuntimes. The ADR makes three specific arguments against per-agent topology:

1. Distributes topology across N resources with no holistic view
2. Creates consistency risk across related agents
3. Wrong abstraction level (connectivity is a relationship, not a property)

**This feature is an acknowledged interim step.** Per-agent egress on AgentRuntime ships now as a pragmatic solution. When AgentMesh lands, `spec.networkPolicy.egress` will be superseded by `AgentMesh.spec.externalEgress`. No design constraints from AgentMesh are imposed on this implementation. The standard K8s egress format is used because it serves the immediate need, not because it maps to the AgentMesh API.

The migration path from per-agent egress to AgentMesh is mechanical: read each AgentRuntime's egress rules, consolidate into a single AgentMesh resource with `externalEgress` entries keyed by agent name.

## Key Requirements

- Platform engineers can declare per-agent egress rules on `AgentRuntime.spec.networkPolicy.egress`
- Verified agents with custom egress rules get a scoped NetworkPolicy instead of allow-all
- Verified agents without custom egress rules retain allow-all behavior (backward-compatible)
- Custom egress rules work on OVN-Kubernetes (CIDR-based, not port-only)
- DNS and K8s API egress are always permitted regardless of custom rules
- The NetworkPolicy controller derives verification status from AgentRuntime (not AgentCard)
- The AgentCard NetworkPolicy controller is removed

## Dependencies

- **Identity binding migration (brainstorm #02, Phase 2):** The new controller needs `spec.identity.binding` (or equivalent) for `ConditionVerified` evaluation. Should land first or concurrently.

## Open Questions

- Should the controller emit a warning event when custom egress rules are set but the agent is unverified (and thus gets the restrictive policy regardless)?
- How should the controller handle the transition period where some agents still have AgentCards with NetworkPolicies owned by the old controller? Cleanup logic or documentation-only?
- Should `spec.networkPolicy` support future extension (e.g., custom ingress rules), or should the field be named `spec.egressPolicy` to keep scope narrow?
