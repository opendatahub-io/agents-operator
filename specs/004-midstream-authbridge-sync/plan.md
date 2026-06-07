# Implementation Plan: Midstream AuthBridge Sync

**Branch**: `004-midstream-authbridge-sync` | **Date**: 2026-06-07 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/004-midstream-authbridge-sync/spec.md`

## Summary

Set up automated upstream-to-midstream synchronization for kagenti-authbridge into the midstream repo (`opendatahub-io/agents-operator`). A standalone sync script copies authbridge source into `kagenti-authbridge/`, applies carried patches, and creates PRs for human review. The sidecar image uses Go build tags (opt-out pattern) to exclude experimental plugins at compile time. The operator sync is out of scope (already handled by `rhods-devops-infra`).

## Technical Context

**Language/Version**: Bash (sync script), Go 1.24 (build tags, sidecar binary), YAML (CI workflows)
**Primary Dependencies**: `gh` CLI (PR creation), `git` (sync operations), Go build tags
**Storage**: `.sync-state` JSON file tracking last synced upstream commit SHA
**Testing**: Manual sync validation, CI build verification, plugin catalog inspection
**Target Platform**: GitHub Actions (CI), Konflux/Tekton (downstream builds)
**Project Type**: Build/CI infrastructure + upstream Go build tag PR
**Performance Goals**: Sync completes in under 5 minutes
**Constraints**: No automatic merges; human review required. Must integrate with existing `rhods-devops-infra` infrastructure.
**Scale/Scope**: 1 upstream repo (kagenti-authbridge), 1 new container image (sidecar), 1 existing image (operator, out of scope)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Applicable? | Status |
|-----------|-------------|--------|
| I. Reconciler Status Integrity | No | N/A |
| II. Spec-Anchored Testing | No | N/A |
| III. Controller-Runtime Safety | No | N/A |
| IV. CRD-First Design | No | N/A |
| V. Feature-Gated Rollout | No | N/A |

All principles are not applicable. This is build infrastructure and CI automation, not operator controller code.

## Project Structure

### Documentation (this feature)

```text
specs/004-midstream-authbridge-sync/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── tasks.md             # Task breakdown
├── REVIEWERS.md         # Review guide
└── checklists/
    └── requirements.md  # Quality checklist
```

### Source Code (midstream repo: opendatahub-io/agents-operator)

```text
kagenti-operator/           # Synced by rhods-devops-infra (existing, out of scope)
├── ...
└── Dockerfile

kagenti-authbridge/         # Synced from kagenti/kagenti-authbridge (NEW)
├── authlib/                # Core library + all plugins (including IBAC source)
├── cmd/authbridge-proxy/   # Proxy-sidecar binary
├── proxy-init/             # iptables init container
├── Dockerfile.midstream    # Midstream sidecar Dockerfile with build tags
└── BUILD.md                # Build-tag documentation

patches/                    # Midstream-carried patches (NEW)
└── authbridge/
    └── 001-ibac-build-tag.patch

scripts/
└── sync/
    ├── config-authbridge.yaml  # Sync configuration
    └── sync-authbridge.sh      # Sync script (callable from rhods-devops-infra)

.github/workflows/
└── sync-authbridge.yml     # Scheduled sync job

.sync-state                 # Last synced commit SHA for kagenti-authbridge
SYNC.md                     # Sync documentation
```

**Structure Decision**: AuthBridge source lives under `kagenti-authbridge/` as a sibling to `kagenti-operator/`, mirroring the upstream repo name. Sync infrastructure lives under `scripts/sync/`. Carried patches under `patches/authbridge/`. This keeps upstream content cleanly separated from midstream additions and is compatible with the existing `rhods-devops-infra` infrastructure that handles operator sync.
