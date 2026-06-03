# Review Guide: Midstream AuthBridge Sync

**Generated**: 2026-06-03 | **Spec**: [spec.md](spec.md)

## Why This Change

The kagenti community decided to keep the operator and AuthBridge in separate upstream repos. For RHOAI productization, we need to sync both upstream repos into the midstream repo (`opendatahub-io/agents-operator`) and build two container images (operator + sidecar) from it. Today there is no sync mechanism and no way to exclude experimental upstream plugins from the product sidecar image. Without this, every upstream sync is a manual, error-prone process that risks shipping untested experimental code.

## What Changes

Two new capabilities are added to the midstream repo:

1. **Automated upstream sync**: A scheduled CI job syncs both `kagenti/kagenti-operator` and `kagenti/kagenti-authbridge` into the midstream repo as sibling directories (`kagenti-operator/`, `kagenti-authbridge/`), opening PRs for human review. Midstream-specific patches are maintained separately and reapplied after each sync.

2. **Build-time plugin selection**: Go build tags (opt-out pattern) control which AuthBridge plugins are compiled into the sidecar binary. Initially, only the IBAC (Intent-Based Access Control) plugin is excluded. This is both an upstream contribution (PR to kagenti-extensions) and a carried midstream patch until upstream merges.

No breaking changes. The existing operator source under `kagenti-operator/` continues to work as before.

## How It Works

The sync infrastructure evaluates `openshift-knative/deviate` as the sync engine. Deviate mirrors upstream releases, deletes unwanted files (demos, docs, CLI tooling, envoy/lite variants), overlays midstream-specific files, applies carried `.patch` files, and creates PRs.

For each upstream, a config file defines include/exclude paths and the upstream URL. A `.sync-state` JSON file at the repo root tracks the last synced commit SHA per upstream. The CI workflow runs daily and supports manual triggers.

For plugin exclusion, each tagged plugin gets a `register.go` file with a `//go:build !exclude_plugin_ibac` constraint. The midstream Dockerfile passes `-tags "exclude_plugin_ibac"` to the Go build, excluding the plugin at compile time. The default upstream build (no tags) continues to include all plugins.

## When It Applies

**Applies when**:
- Building the RHOAI sidecar image from midstream source
- Syncing upstream kagenti changes into the midstream repo
- Adding or removing plugins from the product sidecar image
- Creating or maintaining midstream-specific patches

**Does not apply when**:
- Working directly in the upstream kagenti repos (no change to upstream workflows)
- Building the envoy-sidecar or lite AuthBridge variants (out of scope, not synced)
- Configuring runtime plugin behavior via YAML (existing capability, unchanged)
- Onboarding the repo in Konflux/Tekton (downstream CI, separate effort)

## Key Decisions

1. **Opt-out build tags over opt-in**: Default build includes all plugins (no tags needed). Midstream passes `exclude_plugin_ibac` to exclude. This preserves upstream backward compatibility. Alternative considered: opt-in tags, rejected because they would break upstream's zero-config build.

2. **Full source sync with build-time exclusion over sync-time filtering**: All plugin source code (including IBAC) is synced to midstream. Exclusion is handled solely by build tags. Alternative considered: also skipping excluded plugin directories during sync, rejected because two exclusion mechanisms would be fragile and harder to maintain.

3. **Upstream-wins with carried patches over three-way merge**: The sync always takes upstream content as-is. Midstream-specific modifications live as `.patch` files reapplied after each sync. Alternative considered: three-way merge with conflict detection, rejected because upstream-wins is simpler and matches the established OpenShift Serverless pattern.

4. **Both upstreams use same tooling**: The operator and authbridge syncs use identical infrastructure (same script/deviate config, same CI workflow). Alternative considered: authbridge-only with operator sync handled separately, rejected because consistent tooling reduces maintenance.

5. **Evaluate deviate before custom scripting**: `openshift-knative/deviate` already implements the exact sync pattern needed (mirror, delete, overlay, patch, PR). Alternative considered: custom bash script from scratch, rejected because deviate is battle-tested in the OpenShift Serverless project.

## Areas Needing Attention

- **Deviate fit**: The evaluation task (T004) may reveal that deviate doesn't support multi-upstream-to-single-downstream cleanly. If so, a custom sync script is the fallback, and T009/T010 configs change format. The task dependencies account for this, but reviewers should verify.

- **Upstream PR dependency**: The build-tag mechanism (T005-T007) requires upstream community approval. The carried patch (T008) provides the same functionality independently, but adds maintenance overhead until upstream merges. Track [kagenti/kagenti-extensions#476](https://github.com/kagenti/kagenti-extensions/issues/476).

- **Go module path during rename**: When upstream renames `kagenti-extensions` to `kagenti-authbridge`, the Go module paths in `go.mod` files will change. The sync must handle this transition. The spec notes this as an open question but no task explicitly addresses module path rewriting.

- **Force-push handling**: T011 includes force-push detection, but the exact behavior (block sync? special label on PR? manual intervention required?) could use more specificity during implementation.

## Open Questions

- What is the exact upstream tag/branch convention for kagenti-authbridge releases that the sync job should watch?
- How should the sync handle upstream `go.mod` module path changes during the kagenti-extensions to kagenti-authbridge rename?
- Should the midstream Dockerfile be a copy of upstream's (with build tag additions) or a fully midstream-owned file?
- What is the exact behavior when force-push is detected: block, warn, or create a specially labeled PR?

## Review Checklist

- [ ] Key decisions are justified
- [ ] Breaking changes are documented with migration guidance
- [ ] Scope matches the stated boundaries
- [ ] Success criteria are achievable
- [ ] No unstated assumptions
- [ ] Upstream PR (build tags) has clear fallback via carried patch
- [ ] Sync configs correctly separate include/exclude paths per upstream
- [ ] Both upstreams are covered by the same tooling
- [ ] Force-push edge case is handled in sync script

---

<!-- Code phase sections are appended below this line by the phase-manager command -->
