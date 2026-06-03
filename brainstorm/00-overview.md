# Brainstorm Overview

Last updated: 2026-06-03

## Sessions

| # | Date | Topic | Status | Spec | Issue |
|---|------|-------|--------|------|-------|
| 02 | 2026-05-21 | identity-binding-migration | active | - | - |
| 03 | 2026-05-29 | per-agent-egress-scoping | active | - | [#385](https://github.com/kagenti/kagenti-operator/issues/385) |
| 04 | 2026-06-02 | mtls-gap-analysis | active | - | - |
| 05 | 2026-06-02 | mtls-status-conditions | active | - | - |
| 06 | 2026-06-03 | midstream-authbridge-sync | active | - | [#476](https://github.com/kagenti/kagenti-extensions/issues/476) |

## Open Threads

- Should `identity.spiffe.trustDomain` and `identity.binding.trustDomain` be unified? (from #02)
- Should binding evaluation in the AgentRuntime controller use `status.card` data or re-evaluate independently? (from #02)
- Is there a need for namespace-level binding policy via ConfigMap? (from #02)
- Should the `AgentCardSyncReconciler` get its own deprecation warning before removal? (from #02)
- Should the controller emit a warning event when custom egress rules are set but the agent is unverified? (from #03)
- How should the controller handle the transition period where AgentCards still own NetworkPolicies? (from #03)
- Should `spec.networkPolicy` support future extension, or use a narrower field name like `spec.egressPolicy`? (from #03)
- What is the exact current state of `mtlsMode` support in proxy-sidecar mode? (from #04)
- Does the controller's verified fetch fallback correctly set status conditions today? (from #04)
- What SPIRE version and configuration is assumed for the CI cluster? (from #04)
- Are there cert rotation edge cases (SVID expiry during active connections) that need explicit coverage? (from #04)
- Should `ControlPlaneMTLS` be set on AgentRuntime or AgentCard, given `AttestedAgentSpiffeID` already lives on AgentCard? (from #05)
- What happens to the condition when SPIRE is temporarily unavailable (grace period vs flapping)? (from #05)
- How does `DataPlaneMTLS` interact with existing injection-related conditions? (from #05)
- Should the SPIFFE ID in the condition message be full URI or workload identifier only? (from #05)
- What is the exact upstream tag/branch convention for kagenti-authbridge releases that the sync job should watch? (from #06)
- Should the sync script preserve upstream git history or treat each sync as a squashed snapshot? (from #06)
- How should we handle upstream go.mod module path changes when kagenti-extensions is renamed to kagenti-authbridge? (from #06)
- Should the midstream Dockerfile be a copy of upstream's or a fully midstream-owned file? (from #06)

## Parked Ideas

(none)
