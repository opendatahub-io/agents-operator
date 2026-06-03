# Brainstorm: Midstream AuthBridge Sync

**Date:** 2026-06-03
**Status:** active

## Problem Framing

The kagenti community decided to keep the operator and authbridge in separate upstream repos (renaming `kagenti-extensions` to `kagenti-authbridge`). For RHOAI productization, we need to sync authbridge source into the midstream repo (`opendatahub-io/agents-operator`) to build a product sidecar image. Challenges:

1. **Plugin filtering**: AuthBridge ships experimental plugins (e.g., IBAC's LLM-judge) that aren't product-ready. We need build-time control over which plugins end up in the binary, beyond the existing runtime config.
2. **Two images, one repo**: Konflux onboarding is simpler with fewer repos. We want both the operator and sidecar images built from the same midstream repo.
3. **Sync automation**: Manual syncing creates drift and toil. We need an automated mechanism with human review.
4. **Upstream alignment**: Any build-time plugin selection mechanism should be contributed upstream so it benefits the whole community and reduces our maintenance burden.

## Approaches Considered

### A: Go build tags for plugin selection (chosen)

Each plugin gets an opt-out build tag (e.g., `//go:build !exclude_plugin_ibac`). The default build (no tags) includes everything, preserving upstream behavior. Downstream passes `-tags "exclude_plugin_ibac"` to exclude experimental plugins.

- Pros: Clean Go idiom, zero disruption to upstream, reduces CVE surface
- Cons: Negative tag names ("exclude_X") are slightly less intuitive

### B: Generated plugin imports file

A `make generate-plugins PLUGINS="..."` target writes a `plugins_imports.go` file from a template. Default includes all plugins.

- Pros: Very explicit, readable generated file
- Cons: Code generation step in build, more invasive upstream change

### C: Downstream-only custom main.go

Midstream carries its own `main.go` with curated imports. No upstream changes.

- Pros: Zero upstream dependency
- Cons: Fork maintenance burden, silent exclusion of new upstream plugins, merge friction

## Decision

**Approach A: Go build tags with opt-out pattern.**

The opt-out pattern means upstream builds are unchanged (all plugins included by default). Midstream passes build tags to exclude specific plugins. This was filed as upstream issue [kagenti/kagenti-extensions#476](https://github.com/kagenti/kagenti-extensions/issues/476) with an offer to implement.

## Key Requirements

### Midstream repo layout

```
opendatahub-io/agents-operator/
├── kagenti-operator/        # Synced from kagenti/kagenti-operator (existing)
├── kagenti-authbridge/      # Synced from kagenti/kagenti-authbridge (new)
│   ├── authlib/             # Core library + plugin framework
│   ├── cmd/authbridge-proxy/  # Proxy-sidecar binary (only this mode)
│   ├── proxy-init/          # iptables init container
│   └── Dockerfile           # Midstream Dockerfile with build tags
├── scripts/
│   └── sync-upstream.sh     # Sync automation script
└── ...
```

### Plugin inclusion (initial)

| Plugin | Included | Build tag |
|--------|----------|-----------|
| jwt-validation | yes | (none, always included) |
| token-exchange | yes | (none, always included) |
| token-broker | yes | (none, always included) |
| a2a-parser | yes | (none, always included) |
| mcp-parser | yes | (none, always included) |
| inference-parser | yes | (none, always included) |
| ibac | no | `exclude_plugin_ibac` |

### Sync automation

- Automated GitHub Actions workflow (daily or on upstream tag detection)
- Opens a PR against the midstream repo with the upstream delta
- Tracks last synced commit SHA in a `.sync-state` file per upstream repo
- Human review required before merge
- Separate sync jobs for kagenti-operator and kagenti-authbridge

### Container images

Two images built from the midstream repo:

1. **Operator image**: built from `kagenti-operator/Dockerfile`
2. **Sidecar image**: built from `kagenti-authbridge/cmd/authbridge-proxy/Dockerfile` with `exclude_plugin_ibac` build tag

### Scope

- Proxy-sidecar mode only (not envoy-sidecar or lite variants)
- Only `authbridge-proxy` binary and its dependencies (`authlib/`, `proxy-init/`)
- Demos, docs, and CLI tooling (`abctl`) are not synced

## Open Questions

- What is the exact upstream tag/branch convention for kagenti-authbridge releases that the sync job should watch?
- Should the sync script preserve upstream git history (via `git filter-repo`) or treat each sync as a squashed snapshot?
- How should we handle upstream go.mod module path changes when kagenti-extensions is renamed to kagenti-authbridge?
- Should the midstream Dockerfile be a copy of upstream's (with build tag additions) or a fully midstream-owned file?
