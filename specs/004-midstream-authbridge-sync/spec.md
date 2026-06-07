# Feature Specification: Midstream AuthBridge Sync

**Feature Branch**: `004-midstream-authbridge-sync`
**Created**: 2026-06-03
**Status**: Draft
**Input**: Sync upstream kagenti-authbridge into the midstream repo (opendatahub-io/agents-operator) as kagenti-authbridge/ directory, with build-tag based plugin selection and automated PR-based sync. Context in brainstorm/06-midstream-authbridge-sync.md

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Automated upstream sync with human review (Priority: P1)

A midstream maintainer wants upstream authbridge changes to appear as reviewable PRs in the midstream repo without manual effort. A scheduled job detects new commits in the upstream kagenti-authbridge repo and opens a PR containing the delta. The maintainer reviews the diff, verifies that no unwanted experimental code leaked in, and merges.

**Why this priority**: Without automated sync, upstream changes accumulate and create large, risky catch-up merges. This is the foundation that all other stories depend on.

**Independent Test**: Run the sync script against the current upstream state and verify it produces a valid PR with the expected file set under `kagenti-authbridge/`.

**Acceptance Scenarios**:

1. **Given** upstream kagenti-authbridge has new commits since the last sync, **When** the sync job runs, **Then** a PR is opened in the midstream repo containing only the changed files under `kagenti-authbridge/`.
2. **Given** upstream has no new commits since the last sync, **When** the sync job runs, **Then** no PR is created and the job exits cleanly.
3. **Given** a sync PR already exists and is open, **When** the sync job runs with new upstream commits, **Then** the existing PR is updated rather than creating a duplicate.
4. **Given** a sync PR is merged, **When** the sync job runs next, **Then** it uses the merged commit as the new baseline.

---

### User Story 2 - Build-time plugin exclusion for sidecar image (Priority: P1)

A build engineer building the midstream sidecar image needs to exclude experimental plugins (initially IBAC) from the binary. The midstream Dockerfile passes build tags so that excluded plugins are not compiled in, reducing binary size and CVE surface.

**Why this priority**: Equal to P1 because the sidecar image with correct plugin set is the primary deliverable of this feature. Without plugin control, syncing upstream source is incomplete.

**Independent Test**: Build the sidecar image with the exclusion tag and verify the IBAC plugin is not in the binary (e.g., the binary does not register "ibac" in its plugin catalog).

**Acceptance Scenarios**:

1. **Given** the midstream Dockerfile includes `exclude_plugin_ibac` as a build tag, **When** the sidecar image is built, **Then** the resulting binary does not contain the IBAC plugin code.
2. **Given** the upstream default build (no tags), **When** the binary is built, **Then** all plugins including IBAC are compiled in (backward compatibility).
3. **Given** a new plugin is added upstream without a build tag, **When** the midstream sync imports it, **Then** the new plugin is included in the midstream build by default (opt-out model).

---

### User Story 3 - Two container images from one repo (Priority: P2)

A Konflux pipeline engineer onboarding the midstream repo needs to produce two distinct container images: one for the operator and one for the sidecar proxy. Both images are built from the same repository using separate Dockerfiles and build contexts.

**Why this priority**: Konflux onboarding is a downstream concern that can be addressed after the sync mechanism and plugin selection are working. The repo structure enables this, but the pipeline configuration is a follow-on task.

**Independent Test**: From the midstream repo root, build both images independently using their respective Dockerfiles and verify each produces a working container.

**Acceptance Scenarios**:

1. **Given** the midstream repo contains both `kagenti-operator/` and `kagenti-authbridge/`, **When** the operator Dockerfile is built, **Then** the operator image is produced without depending on authbridge source.
2. **Given** the midstream repo layout, **When** the sidecar Dockerfile is built, **Then** the sidecar image is produced using only files under `kagenti-authbridge/`.
3. **Given** changes to authbridge source only, **When** CI runs, **Then** only the sidecar image is rebuilt (the operator image build is not triggered).

---

### User Story 4 - Runtime plugin configuration (Priority: P3)

An operator deploying the sidecar in a cluster configures which compiled-in plugins are active via the pipeline YAML configuration. Plugins not listed in the YAML are dormant. Plugins can be disabled at runtime via `on_error: off` without redeployment.

**Why this priority**: This capability already exists in upstream authbridge. This story confirms it works correctly with the midstream plugin subset and documents the expected behavior.

**Independent Test**: Deploy the sidecar with a YAML config that omits certain plugins and verify they are not active, then add `on_error: off` to a configured plugin and verify it stops processing.

**Acceptance Scenarios**:

1. **Given** the sidecar image includes plugins A, B, and C, **When** the YAML config only lists A and B, **Then** plugin C is not active and does not process requests.
2. **Given** a running sidecar with plugin A active, **When** the config is updated to set A's `on_error: off`, **Then** plugin A stops processing after config reload.

---

### Edge Cases

- What happens when the upstream repo is renamed from kagenti-extensions to kagenti-authbridge mid-sync? The sync script must handle remote URL changes gracefully.
- How does the sync behave when upstream force-pushes or rebases its main branch? The script should detect non-fast-forward changes and flag them for manual review.
- What happens when upstream restructures its directory layout (e.g., moving plugin code to a new path)? The sync should capture the full delta without special handling.
- What if a build tag is misspelled in the Dockerfile? The build should succeed but include the unintentionally included plugin; a CI check should validate known tags.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The sync script MUST track the last synced upstream commit SHA for kagenti-authbridge within the midstream repo.
- **FR-002**: The sync script MUST copy upstream authbridge source into `kagenti-authbridge/` in the midstream repo, preserving the internal directory structure including all plugin source code (plugin exclusion is handled solely by build tags, not by the sync script).
- **FR-003**: The sync script MUST exclude files not needed for the product build (demos, docs, CLI tooling like `abctl`, envoy and lite binary directories).
- **FR-004**: The sync automation MUST open a GitHub PR with a descriptive title indicating the upstream commit range being synced.
- **FR-005**: The sync automation MUST NOT merge PRs automatically; human review is required.
- **FR-006**: Each authbridge plugin MUST support an opt-out build tag (`exclude_plugin_<name>`) that prevents compilation when the tag is passed.
- **FR-007**: The default build (no tags) MUST include all plugins, preserving upstream backward compatibility.
- **FR-008**: The midstream Dockerfile for the sidecar MUST pass the appropriate build tags to exclude non-product plugins.
- **FR-009**: The midstream repo MUST produce two independent container images: operator and sidecar (proxy-sidecar mode).
- **FR-010**: The sync script MUST be runnable both manually (for initial setup and debugging) and via CI automation.
- **FR-011**: Midstream-specific patches (e.g., the IBAC build-tag patch before upstream acceptance) MUST be maintained as `.patch` files in a dedicated directory and reapplied automatically after each upstream sync.
- **FR-012**: The sync script SHOULD be a standalone shell script living in the midstream repo (not relying on external tools like deviate), since agents-operator is not a fork of kagenti-extensions and deviate assumes a fork relationship. The script should integrate with or be callable from the existing `rhods-devops-infra` sync infrastructure.

### Key Entities

- **Sync State**: Tracks the last synced upstream commit SHA for kagenti-authbridge. Stored as a file in the midstream repo.
- **Carried Patch**: A `.patch` file in a dedicated directory (e.g., `patches/`) that captures midstream-specific modifications to upstream source. Reapplied automatically after each sync via `git apply`.
- **Plugin Build Tag**: A Go build constraint that controls whether a plugin package is compiled into the binary. Named `exclude_plugin_<name>`.
- **Upstream Source Snapshot**: The set of files copied from upstream authbridge into `kagenti-authbridge/`, scoped to the proxy-sidecar binary and its dependencies.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Upstream changes are available as a midstream PR within 24 hours of being committed upstream.
- **SC-002**: The sidecar image built from midstream does not contain excluded plugin code (verifiable by checking the plugin catalog endpoint).
- **SC-003**: Both container images (operator and sidecar) build successfully from a clean checkout of the midstream repo.
- **SC-004**: The sync script completes in under 5 minutes for a typical upstream delta.
- **SC-005**: A new upstream plugin added without a build tag is automatically included in midstream builds without sync script changes.

## Clarifications

### Session 2026-06-03

- Q: Should the sync copy the full `authlib/` directory (including IBAC source), relying solely on build tags for exclusion, or also skip excluded plugin directories? → A: Sync full `authlib/` including all plugin source; build tags are the sole exclusion mechanism.
- Q: How should conflicts between upstream changes and midstream patches be handled? → A: Upstream wins for synced files; midstream patches maintained in a separate `patches/` directory and reapplied after each sync. Deviate was initially considered but is not a fit because agents-operator is not a fork of kagenti-extensions (deviate assumes a fork relationship). A simpler standalone sync script is preferred.
- Q: Should this spec also formalize the operator sync so both upstreams use the same mechanism? → A: No. The operator sync from kagenti/kagenti-operator to opendatahub-io/agents-operator is already handled by `rhods-devops-infra` (entry `agents-operator-upstream` in `upstream-source-map.yaml`). This spec covers authbridge sync only. The authbridge sync script should be compatible with or callable from the existing `rhods-devops-infra` infrastructure.

## Assumptions

- The upstream kagenti-extensions repo will be renamed to kagenti-authbridge. The sync script should work with either name during the transition.
- Only the proxy-sidecar binary (`cmd/authbridge-proxy/`) and its dependencies (`authlib/`, `proxy-init/`) are in scope for midstream sync. The envoy and lite variants are not synced.
- The build-tag mechanism requires an upstream PR ([kagenti/kagenti-extensions#476](https://github.com/kagenti/kagenti-extensions/issues/476)) to be accepted and merged. Until then, midstream can carry a downstream patch for the IBAC exclusion tag.
- The existing runtime plugin configuration via pipeline YAML and `on_error` policies works correctly and does not need modification.
- Git history from upstream is not preserved in the sync (squashed snapshot per sync). Full history remains available in the upstream repo.
- The midstream repo's CI will be configured separately (Konflux/Tekton pipelines) and is out of scope for this spec, though the repo layout must support it.
- The kagenti-operator upstream-to-midstream sync is already handled by `red-hat-data-services/rhods-devops-infra` and is out of scope for this spec.
- Deviate (`openshift-knative/deviate`) is not suitable for the authbridge sync because agents-operator is not a fork of kagenti-extensions. A simpler standalone script is preferred.
