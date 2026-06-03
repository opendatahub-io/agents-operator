# Research: Midstream AuthBridge Sync

**Date**: 2026-06-03
**Feature**: [spec.md](./spec.md)

## R1: Sync tooling evaluation - deviate vs custom script

### Decision: Evaluate deviate first; fall back to custom script if it doesn't fit

### Rationale

`openshift-knative/deviate` (v0.4.0, Go, Apache-2.0) implements the exact sync pattern we need:

1. **Mirror upstream releases** to downstream branches
2. **Delete unwanted upstream files** via `deleteFromUpstream` config filters
3. **Overlay midstream files** via `copyFromMidstream` config (Dockerfiles, CI, etc.)
4. **Apply carried patches** from `openshift/patches/*.patch` via `git apply`
5. **Generate Dockerfile image references** (optional, for OpenShift registry)
6. **Create sync PRs** with configurable labels and messages
7. **Tag synchronization** across upstream/downstream

Key config fields from deviate's `Config` struct:
- `upstream`: upstream repo URL
- `downstream`: downstream repo URL
- `copyFromMidstream`: file filters for midstream-specific overlay
- `deleteFromUpstream`: file filters for excluding upstream content
- `syncLabels`: labels for sync PRs
- `branches`: main, release-next, release templates
- `tags`: synchronize flag and refSpec
- `messages`: commit and PR message templates

**Potential gap**: Deviate is designed for single-upstream-to-single-downstream sync. We need two upstreams (operator + authbridge) into one downstream. This may require either:
- Two deviate configs (one per upstream), or
- A wrapper script that runs deviate twice with different configs

### Alternatives considered

| Option | Pros | Cons |
|--------|------|------|
| **Custom bash script** | Full control, simple | No patch management, no release mirroring, reinventing existing tooling |
| **git subtree** | Built into git, preserves history | Merge conflicts are painful, subtree squash loses history anyway |
| **git submodule** | Clean separation | Notorious usability issues, doesn't support file filtering |

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
  "kagenti-operator": {
    "last_synced_sha": "abc123...",
    "last_synced_date": "2026-06-03T15:00:00Z",
    "upstream_url": "https://github.com/kagenti/kagenti-operator.git"
  },
  "kagenti-authbridge": {
    "last_synced_sha": "def456...",
    "last_synced_date": "2026-06-03T15:00:00Z",
    "upstream_url": "https://github.com/kagenti/kagenti-extensions.git"
  }
}
```

This file is committed to the midstream repo so any maintainer can see the sync state. If deviate is adopted, it manages its own state via branch references, and this file may not be needed (deviate uses the downstream branch HEAD as the implicit state).

### Alternatives considered

| Option | Pros | Cons |
|--------|------|------|
| **Branch-based state** (deviate default) | No extra files, implicit | Harder to inspect without deviate tooling |
| **GitHub Actions cache** | Doesn't pollute repo | Volatile, hard to debug |
| **Git tag on upstream** | Visible | Requires write access to upstream |

## R4: Patch directory structure

### Decision: `patches/<upstream-name>/*.patch` at repo root

### Rationale

Patches are organized per upstream repo:

```
patches/
├── authbridge/
│   └── 001-ibac-build-tag.patch
└── operator/
    └── (empty initially)
```

Deviate expects patches in `openshift/patches/` by convention. We may need to configure or adapt the path. If we use deviate, we follow its convention. If custom scripting, we use the structure above.

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

For deviate, this maps to the `deleteFromUpstream` filter config.
