# Research: Midstream AuthBridge Sync

**Date**: 2026-06-03
**Feature**: [spec.md](./spec.md)

## R1: Sync tooling evaluation - deviate vs custom script

### Decision: Custom sync script (deviate does not fit)

### Rationale

`openshift-knative/deviate` was initially considered but rejected after team feedback (Christian Zaccaria). The key issue: deviate assumes the downstream repo is a fork of the upstream repo. In our case, `opendatahub-io/agents-operator` is a fork of `kagenti/kagenti-operator`, not of `kagenti/kagenti-extensions`. AuthBridge is being brought in as a sibling module, not as a forked codebase.

Additionally, the operator sync from `kagenti/kagenti-operator` to `opendatahub-io/agents-operator` is already handled by `red-hat-data-services/rhods-devops-infra` (entry `agents-operator-upstream` in `upstream-source-map.yaml`). This infrastructure runs on a 2-hour schedule and handles the complete sync lifecycle.

A simpler standalone bash script for authbridge sync is preferred because:
- It handles only authbridge (one upstream, not a fork)
- It can be called from `rhods-devops-infra` workflows if needed
- It avoids a Go tool dependency for a simple file-copy + patch-apply workflow
- The carried-patch pattern from deviate is adopted (it's just `git apply`), without needing deviate itself

### Alternatives considered

| Option | Pros | Cons |
|--------|------|------|
| **deviate** | Battle-tested, patch management built-in | Assumes fork relationship, doesn't fit our topology |
| **git subtree** | Built into git, preserves history | Merge conflicts are painful, subtree squash loses history anyway |
| **git submodule** | Clean separation | Notorious usability issues, doesn't support file filtering |
| **rhods-devops-infra entry** | Consistent with operator sync | May not support sibling-module-into-subdirectory pattern; needs investigation |

## R2: Go build tag pattern for plugin exclusion

### Decision: Opt-out build tags using `//go:build !exclude_plugin_<name>`

### Rationale

The Go build tag system supports conditional compilation via `//go:build` directives. The opt-out pattern means:

- **Default (no tags)**: All plugins compiled in. Zero change for upstream users.
- **With tag**: `go build -tags "exclude_plugin_ibac"` excludes the IBAC plugin.

Implementation per plugin:
1. Move `RegisterPlugin` call from the plugin's main file into a dedicated `register.go`
2. Add `//go:build !exclude_plugin_ibac` to `register.go`
3. The `init()` function in `register.go` handles registration
4. When the tag is set, the file is excluded from compilation, so `init()` never runs

This is analogous to how `database/sql` drivers work in the stdlib: side-effect imports pull in drivers, and build tags can gate them.

**Precedent in the codebase**: `authbridge-lite` already uses a reduced plugin set by simply not importing certain packages. Build tags formalize this at the plugin level rather than requiring a separate binary.

### Alternatives considered

| Option | Pros | Cons |
|--------|------|------|
| **Opt-in tags** (`//go:build plugin_ibac`) | More explicit | Breaks upstream default build (must pass tags) |
| **Generated imports file** | Very explicit, readable | Requires code generation step in build |
| **Separate binary per plugin set** | Already exists (lite) | Doesn't scale, duplicates main.go |

## R3: Sync state tracking

### Decision: JSON state file at repo root (`.sync-state`)

### Rationale

A simple JSON file tracking last synced commit per upstream:

```json
{
  "kagenti-authbridge": {
    "last_synced_sha": "def456...",
    "last_synced_date": "2026-06-03T15:00:00Z",
    "upstream_url": "https://github.com/kagenti/kagenti-extensions.git"
  }
}
```

This file is committed to the midstream repo so any maintainer can see the sync state. The operator sync state is managed by `rhods-devops-infra` separately.

### Alternatives considered

| Option | Pros | Cons |
|--------|------|------|
| **Branch-based state** | No extra files, implicit | Harder to inspect without tooling |
| **GitHub Actions cache** | Doesn't pollute repo | Volatile, hard to debug |
| **Git tag on upstream** | Visible | Requires write access to upstream |

## R4: Patch directory structure

### Decision: `patches/<upstream-name>/*.patch` at repo root

### Rationale

Patches are organized per upstream repo:

```
patches/
└── authbridge/
    └── 001-ibac-build-tag.patch
```

Operator patches are not needed since the operator sync is handled by `rhods-devops-infra`.

Patch naming convention: `NNN-description.patch` with numeric prefix for ordering. Patches are applied in lexicographic order.

Patches are created via `git diff` or `git format-patch` against the upstream source, and applied via `git apply` after each sync.

## R5: File exclusion list for authbridge sync

### Decision: Exclude demos, docs, CLI tooling, envoy and lite binaries

### Rationale

Files synced from upstream kagenti-authbridge:
- `authlib/` (full, including all plugins)
- `cmd/authbridge-proxy/` (proxy-sidecar binary + Dockerfile)
- `proxy-init/` (iptables init container)

Files excluded:
- `cmd/authbridge-envoy/` (envoy mode, not used)
- `cmd/authbridge-lite/` (lite variant, not used)
- `cmd/abctl/` (CLI tooling, not shipped)
- `demos/` (demo configurations)
- `docs/` (developer documentation)
- `tests/` (upstream integration tests, may be adapted separately)
- Top-level files: `Makefile`, `pyproject.toml`, `*.sh`, `*.md` (upstream-specific)

These are configured in `scripts/sync/config-authbridge.yaml` and enforced by the sync script.
