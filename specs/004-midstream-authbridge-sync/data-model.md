# Data Model: Midstream AuthBridge Sync

**Date**: 2026-06-03

## Entities

### Sync Configuration

Represents the sync setup for one upstream repository.

| Field | Type | Description |
|-------|------|-------------|
| upstream_url | string | Git URL of the upstream repo |
| upstream_branch | string | Branch to sync from (default: `main`) |
| target_directory | string | Destination directory in midstream repo |
| include_paths | string[] | Paths to sync (e.g., `authlib/`, `cmd/authbridge-proxy/`) |
| exclude_paths | string[] | Paths to exclude (e.g., `demos/`, `docs/`) |
| patches_directory | string | Path to carried patches for this upstream |

### Sync State

Tracks the last successful sync per upstream.

| Field | Type | Description |
|-------|------|-------------|
| upstream_name | string | Identifier (e.g., `kagenti-operator`) |
| last_synced_sha | string | Commit SHA of last synced upstream commit |
| last_synced_date | datetime | Timestamp of last sync |
| upstream_url | string | Git URL (for reference, may change during rename) |

### Carried Patch

A midstream-specific modification applied on top of synced upstream source.

| Field | Type | Description |
|-------|------|-------------|
| filename | string | Patch file name (e.g., `001-ibac-build-tag.patch`) |
| description | string | What the patch does (in commit message or header) |
| upstream_issue | string | Link to upstream issue/PR that would eliminate this patch |
| apply_order | integer | Numeric prefix determines application order |

### Plugin Build Tag

A Go build constraint controlling plugin compilation.

| Field | Type | Description |
|-------|------|-------------|
| plugin_name | string | Plugin registry name (e.g., `ibac`) |
| tag_name | string | Build tag (e.g., `exclude_plugin_ibac`) |
| default_included | boolean | Always `true` (opt-out model) |
| excluded_in_midstream | boolean | Whether midstream builds pass this tag |

## Relationships

```
Sync Configuration 1──* Carried Patch (one config has zero or more patches)
Sync Configuration 1──1 Sync State (one state per config)
Plugin Build Tag *──1 Sidecar Image (multiple tags control one image build)
```

## State Transitions

### Sync Job Lifecycle

```
idle → checking upstream → [no changes] → idle
                         → [changes found] → creating/updating PR → idle
```

### Carried Patch Lifecycle

```
created → applied (each sync) → [upstream PR merged] → removed
```

When the upstream PR that addresses a carried patch is merged, the patch becomes a no-op (applies cleanly with no diff) or fails to apply (upstream changed the code differently). Either way, the patch should be removed from the patches directory.
