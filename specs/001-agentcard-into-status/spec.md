# Feature Specification: Consolidate AgentCard Data Into AgentRuntime Status

**Feature Branch**: `001-agentcard-into-status`
**Created**: 2026-05-21
**Status**: Draft
**Input**: Brainstorm document `brainstorm/01-agentcard-into-agentruntime.md`

## Clarifications

### Session 2026-05-21

- Q: How should the controller discover the Service endpoint for a given AgentRuntime's targetRef workload? → A: Selector match: resolve the workload's Pod selector, find Services whose selector matches, use the first match.
- Q: What happens to previously populated status.card data when a card fetch fails? → A: Retain last successful card data; rely on the CardSynced condition and fetchedAt timestamp to signal staleness.
- Q: What happens to existing status.card data when the feature flag is toggled off? → A: Clear status.card on the next reconcile of each AgentRuntime when the flag is disabled.
- Q: What should status.card contain, given mTLS is in scope? → A: Card payload fields, fetch metadata (fetchedAt, cardId, protocol), and verification fields (signature validation, SPIFFE identity). mTLS reuses infrastructure from PR #284.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Discover Agent Capabilities from AgentRuntime (Priority: P1)

A platform operator queries a single resource (AgentRuntime) to see what an agent can do, including its name, description, skills, supported protocols, and endpoint URL. Today this requires cross-referencing AgentRuntime and AgentCard, which doubles the lookup effort and makes scripting harder.

**Why this priority**: This is the core value of the consolidation. Without card data surfaced on AgentRuntime, the entire feature has no observable benefit.

**Independent Test**: Deploy an agent workload with a valid `/.well-known/agent-card.json` endpoint, create an AgentRuntime targeting it, and confirm `status.card` is populated with the card data via `kubectl get agentruntime -o yaml`.

**Acceptance Scenarios**:

1. **Given** an AgentRuntime targeting a workload (Deployment, StatefulSet, or Sandbox) whose Pods serve a valid A2A agent card at `/.well-known/agent-card.json`, **When** the controller reconciles the AgentRuntime, **Then** `status.card` contains the agent's name, description, skills, capabilities, endpoint URL, fetchedAt timestamp, cardId content hash, and detected protocol.
2. **Given** an AgentRuntime whose `status.card` is already populated, **When** the backing workload rolls out a new Pod template (hash change or generation change), **Then** the controller re-fetches the card and updates `status.card` with the new content.
3. **Given** an AgentRuntime targeting a workload, **When** the agent's card endpoint is unreachable or returns invalid JSON, **Then** `status.card` retains the last successfully fetched data and a `CardSynced` condition indicates the fetch failure with a human-readable reason.

---

### User Story 2 - Verified Card Discovery via mTLS (Priority: P1)

A platform operator deploying agents with SPIFFE identity (via SPIRE) can see the card's signature verification status and the attested SPIFFE ID directly on the AgentRuntime, confirming the card was fetched over a verified mTLS connection and its JWS signature is valid.

**Why this priority**: Identity verification is a core platform security requirement. Reusing the mTLS infrastructure from PR #284 makes this achievable without significant new code.

**Independent Test**: Deploy an agent with SPIRE identity configured, create an AgentRuntime with mTLS mode enabled, and verify `status.card` includes signature validation result and attested SPIFFE ID.

**Acceptance Scenarios**:

1. **Given** an AgentRuntime with mTLS enabled and a backing agent that presents a valid SPIFFE certificate, **When** the controller fetches the card over mTLS, **Then** `status.card` includes the attested SPIFFE ID extracted from the peer certificate and the signature validation result.
2. **Given** an AgentRuntime with mTLS enabled and a backing agent whose SPIFFE certificate does not match the expected trust domain, **When** the controller attempts to fetch the card, **Then** the fetch fails, `status.card` retains stale data, and the `CardSynced` condition reports an identity verification failure.
3. **Given** an AgentRuntime without mTLS configured (or mTLS disabled), **When** the controller fetches the card, **Then** the fetch uses plain HTTP and verification fields in `status.card` remain empty.

---

### User Story 3 - Deprecation Warning on AgentCard Creation (Priority: P2)

A platform operator who still creates AgentCard CRs receives a clear deprecation warning so they know to migrate to the AgentRuntime-based discovery path.

**Why this priority**: Backward compatibility is essential during the transition period. Operators need a signal to migrate without breaking existing workflows.

**Independent Test**: Create an AgentCard CR and check controller logs for the deprecation warning message.

**Acceptance Scenarios**:

1. **Given** the operator is running with card discovery enabled, **When** a new AgentCard CR is created, **Then** the controller emits a deprecation log warning indicating that AgentCard is deprecated and card data should be consumed from AgentRuntime `status.card`.
2. **Given** an existing AgentCard CR, **When** the controller reconciles it, **Then** the AgentCard continues to function normally (both CRDs coexist).

---

### User Story 4 - Feature-Gated Card Discovery (Priority: P2)

A cluster administrator controls whether the new card discovery behavior is active via a feature flag, allowing gradual rollout without code changes.

**Why this priority**: Operators need to opt in during the transition period. Disabling by default prevents surprises for existing installations.

**Independent Test**: Start the operator with the feature flag disabled and verify no card fetch occurs; enable the flag and verify card fetching activates.

**Acceptance Scenarios**:

1. **Given** the operator is started without enabling the card discovery feature flag, **When** an AgentRuntime is reconciled, **Then** no card fetch is attempted and `status.card` remains empty.
2. **Given** the operator is started with the card discovery feature flag enabled, **When** an AgentRuntime is reconciled, **Then** the controller fetches `/.well-known/agent-card.json` from the agent's Service endpoint and populates `status.card`.
3. **Given** an AgentRuntime with populated `status.card` data, **When** the operator restarts with the feature flag disabled, **Then** `status.card` is cleared on the next reconcile of each AgentRuntime.

---

### Edge Cases

- What happens when the agent's Service has multiple ports? The controller uses selector matching to find the Service, then targets the port serving the A2A protocol (by well-known port name or the first HTTP port).
- How does the system handle a card endpoint that returns a valid JSON response but not a valid A2A agent card structure? The controller treats it as a fetch failure, retains any previously fetched data, and surfaces the parsing error in the `CardSynced` condition.
- What happens when the backing workload has zero ready Pods? The controller skips the card fetch and sets a condition indicating the workload is not ready.
- What happens if the card response is excessively large? The controller enforces a size limit on the response body (1 MiB) to prevent resource exhaustion.
- What happens when no Service matches the workload's Pod selector? The controller sets the `CardSynced` condition to false with reason "ServiceNotFound" and skips the fetch.
- What happens when the mTLS handshake fails (e.g., certificate expired, wrong trust domain)? The controller retains stale card data and reports the TLS error in the `CardSynced` condition.
- What happens if the card fetch hangs or is slow? The controller enforces a 10-second timeout on the HTTP/mTLS request. Timeout is treated as a fetch failure (stale data retained, `CardSynced=False`).
- What happens when an agent updates its card content without a workload rollout (e.g., hot-reloading skills)? The controller does not detect this. Card data in `status.card` reflects the state at the last rollout. This is an intentional constraint: event-driven fetch (no polling) trades freshness of dynamic card changes for reduced API server and network load.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST add a `card` field to AgentRuntime status that holds: A2A agent card payload (name, description, version, URL, skills, capabilities, provider, input/output modes), fetch metadata (fetchedAt timestamp, cardId content hash, detected protocol), and verification fields (signature validation result, attested SPIFFE ID).
- **FR-002**: The system MUST fetch the agent card from `/.well-known/agent-card.json` on the agent's Service endpoint when reconciling an AgentRuntime.
- **FR-003**: The system MUST trigger card re-fetch only on Pod template hash changes (rollout events), not on a periodic timer.
- **FR-004**: The system MUST record a `CardSynced` condition on AgentRuntime status indicating the result of the last fetch attempt (success, failure with reason, or skipped).
- **FR-005**: The system MUST gate the card fetch behavior behind a feature flag that defaults to disabled.
- **FR-006**: The system MUST emit a deprecation log warning when a new AgentCard CR is created.
- **FR-007**: The system MUST record a `fetchedAt` timestamp in `status.card` so operators can see when the card data was last refreshed.
- **FR-008**: The system MUST resolve the agent's Service endpoint by matching the workload's Pod selector labels to Services in the same namespace (selector match), using the first matching Service. This applies to all supported workload types (Deployment, StatefulSet, Sandbox).
- **FR-009**: The system MUST enforce a maximum response body size when fetching the card to prevent resource exhaustion.
- **FR-010**: The system MUST support mTLS for the card fetch, reusing the SPIFFE/SPIRE infrastructure from PR #284. When mTLS is configured on the AgentRuntime, the controller uses the workload's SVID to establish a verified connection.
- **FR-011**: The system MUST populate verification fields in `status.card` (attested SPIFFE ID, signature validation result) when the card is fetched over mTLS and the card contains JWS signatures.
- **FR-012**: The system MUST clear `status.card` when the feature flag is disabled, on the next reconcile of each AgentRuntime.
- **FR-013**: The system MUST retain the last successfully fetched card data when a fetch attempt fails, relying on the `CardSynced` condition and `fetchedAt` timestamp to signal staleness.

### Key Entities

- **AgentRuntime**: Existing CRD that attaches runtime configuration to a workload. Extended with `status.card` to hold discovered agent card data, fetch metadata, and verification results.
- **AgentCard (existing, deprecated)**: Existing CRD that caches A2A card data. Continues to function but emits deprecation warnings. Will be removed in a future iteration.
- **A2A Agent Card**: The JSON document served by agents at `/.well-known/agent-card.json` per the A2A protocol specification. Source of truth for card data.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Operators can retrieve agent capabilities (name, skills, endpoint) and identity verification status from a single `kubectl get agentruntime` command instead of cross-referencing two resources.
- **SC-002**: Card data in `status.card` reflects the running agent's actual capabilities within one reconciliation cycle after a rollout.
- **SC-003**: Existing AgentCard-based workflows continue to function without modification during the transition period.
- **SC-004**: The feature can be enabled or disabled at operator startup without redeployment of agent workloads. Disabling clears stale card data from all AgentRuntimes.
- **SC-005**: When mTLS is configured, the card fetch verifies the agent's SPIFFE identity and validates JWS signatures, with results visible in `status.card`.

## Out of Scope (with migration path)

The following are explicitly out of scope for this iteration. Each item includes the intended migration path so the deprecation trajectory is visible.

### Identity binding policy (`spec.identityBinding`)

Identity binding is a **workload identity policy** that validates whether an agent's SPIFFE ID belongs to a configured trust domain. It is orthogonal to card discovery: the card fetch is merely the mechanism that surfaces the SPIFFE ID, but the binding evaluation and enforcement are about the workload, not the card content.

**Current home**: `AgentCard.spec.identityBinding` (trustDomain, strict)
**Intended destination**: `AgentRuntime.spec` in a follow-up iteration, alongside the existing `spec.identity.spiffe.trustDomain` field.
**During coexistence**: Identity binding policy stays on AgentCard. The AgentCard controller continues to evaluate binding and propagate the `signature-verified` label. No enforcement behavior changes.
**Brainstorm**: See `brainstorm/02-identity-binding-migration.md` for detailed analysis.

### Enforcement actions (label propagation, NetworkPolicy)

The current AgentCard controller propagates the `signature-verified` label to workloads based on identity verification results. The NetworkPolicy controller uses this label to gate inter-agent traffic.

**This PR**: `status.card` on AgentRuntime is purely observational. It surfaces verification results but does not drive enforcement actions.
**During coexistence**: The AgentCard controller continues to handle all enforcement (label propagation, NetworkPolicy). No enforcement behavior changes.
**Future iteration**: When identity binding moves to AgentRuntime.spec, the enforcement logic (label propagation) moves to the AgentRuntime controller. This is a separate spec.

### AgentCardSyncReconciler

The `AgentCardSyncReconciler` auto-creates AgentCard CRs for labelled agent workloads. It continues to function during coexistence. Its deprecation and removal will be part of the AgentCard CRD removal iteration (after identity binding migration is complete).

### AgentCard CRD removal and migration tooling

Full CRD removal, ValidatingAdmissionPolicy for label restriction, and migration tooling are deferred until identity binding and enforcement have migrated to AgentRuntime.

## Assumptions

- Each AgentRuntime targets exactly one workload (Deployment, StatefulSet, or Sandbox), and there is at most one Service matching that workload's Pod selector in the same namespace.
- The card fetch adds negligible latency to the reconcile loop (single HTTP/mTLS GET, typically sub-second).
- The existing `AgentCardData` Go struct (already defined in the codebase) can be extended or wrapped to include fetch metadata and verification fields for `status.card`.
- IBM maintainers have agreed to the AgentCard deprecation path (confirmed 2026-05-15 per brainstorm context).
- The mTLS verified fetch infrastructure from PR #284 (merged 2026-05-20) is stable and reusable for the AgentRuntime controller's card fetch.
- Agents with SPIRE identity configured already have SVIDs available via spiffe-helper in the operator Pod or via the SPIRE agent socket.
