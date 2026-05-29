# Brainstorm Overview

Last updated: 2026-05-29

## Sessions

| # | Date | Topic | Status | Spec |
|---|------|-------|--------|------|
| 02 | 2026-05-21 | identity-binding-migration | active | - |
| 03 | 2026-05-29 | per-agent-egress-scoping | active | - |

## Open Threads

- Should `identity.spiffe.trustDomain` and `identity.binding.trustDomain` be unified? (from #02)
- Should binding evaluation in the AgentRuntime controller use `status.card` data or re-evaluate independently? (from #02)
- Is there a need for namespace-level binding policy via ConfigMap? (from #02)
- Should the `AgentCardSyncReconciler` get its own deprecation warning before removal? (from #02)
- Should the controller emit a warning event when custom egress rules are set but the agent is unverified? (from #03)
- How should the controller handle the transition period where AgentCards still own NetworkPolicies? (from #03)
- Should `spec.networkPolicy` support future extension, or use a narrower field name like `spec.egressPolicy`? (from #03)

## Parked Ideas

(none)
