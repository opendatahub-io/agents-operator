# Implementation Plan: Midstream AuthBridge Sync

**Branch**: `004-midstream-authbridge-sync` | **Date**: 2026-06-03 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/004-midstream-authbridge-sync/spec.md`

## Summary

Set up automated upstream-to-midstream synchronization for both kagenti-operator and kagenti-authbridge repositories into the midstream repo (`opendatahub-io/agents-operator`). The sync uses `openshift-knative/deviate` (or a similar tool) to mirror upstream releases, apply carried patches, and create PRs for human review. AuthBridge sidecar images use Go build tags (opt-out pattern) to exclude experimental plugins at compile time.

## Technical Context

**Language/Version**: Go 1.24, Bash (sync scripts), YAML (CI workflows)
**Primary Dependencies**: `openshift-knative/deviate` (sync engine), `gh` CLI (PR creation), Go build tags
**Storage**: Git state files tracking last synced upstream commit SHAs
**Testing**: Manual sync validation, CI build verification, plugin catalog inspection
**Target Platform**: GitHub Actions (CI), Konflux/Tekton (downstream builds)
**Project Type**: Build/CI infrastructure + upstream Go build tag PR
**Performance Goals**: Sync completes in under 5 minutes
**Constraints**: No automatic merges; human review required for all sync PRs
**Scale/Scope**: 2 upstream repos (kagenti-operator, kagenti-authbridge), 2 container images

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Applicable? | Status |
|-----------|-------------|--------|
| I. Reconciler Status Integrity | No - no reconciler code touched | N/A |
| II. Spec-Anchored Testing | No - no controller tests | N/A |
| III. Controller-Runtime Safety | No - no controller code | N/A |
| IV. CRD-First Design | No - no CRD changes | N/A |
| V. Feature-Gated Rollout | No - infrastructure/build changes only | N/A |

All principles are not applicable to this feature. This is build infrastructure and CI automation, not operator controller code.

## Project Structure

### Documentation (this feature)

```text
specs/004-midstream-authbridge-sync/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
└── tasks.md             # Phase 2 output (via /speckit-tasks)
```

### Source Code (repository root)

```text
# Midstream repo layout (opendatahub-io/agents-operator)
kagenti-operator/           # Synced from kagenti/kagenti-operator
├── ...                     # Existing operator source
└── Dockerfile              # Operator image build

kagenti-authbridge/         # Synced from kagenti/kagenti-authbridge (NEW)
├── authlib/                # Core library + all plugins (including IBAC source)
├── cmd/authbridge-proxy/   # Proxy-sidecar binary
├── proxy-init/             # iptables init container
└── Dockerfile              # Sidecar image build (with build tags)

patches/                    # Midstream-carried patches (NEW)
├── authbridge/             # Patches for kagenti-authbridge
│   └── 001-ibac-build-tag.patch  # Adds //go:build !exclude_plugin_ibac
└── operator/               # Patches for kagenti-operator (if any)

scripts/                    # Sync and build scripts
├── sync-upstream.sh        # Manual sync entry point (or deviate config)
└── ...

.github/workflows/          # CI automation
└── sync-upstream.yml       # Scheduled sync job

deviate.yaml                # Deviate configuration (if adopted)
.sync-state                 # Last synced commit SHAs per upstream repo
```

**Structure Decision**: Sibling directories mirroring upstream repo names (`kagenti-operator/`, `kagenti-authbridge/`), with midstream-specific files (`patches/`, `deviate.yaml`, `.sync-state`) at the repo root. This keeps upstream content cleanly separated from midstream additions.
