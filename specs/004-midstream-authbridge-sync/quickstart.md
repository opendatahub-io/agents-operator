# Quickstart: Midstream AuthBridge Sync

## Prerequisites

- Git 2.30+
- Go 1.24+ (for building sidecar image locally)
- `gh` CLI (for PR operations)
- Access to `opendatahub-io/agents-operator` (midstream repo)
- Access to `kagenti/kagenti-operator` and `kagenti/kagenti-extensions` (upstream repos)

## Initial Setup

### 1. Clone the midstream repo

```bash
git clone git@github.com:opendatahub-io/agents-operator.git
cd agents-operator
```

### 2. Add upstream remotes

```bash
git remote add upstream-operator https://github.com/kagenti/kagenti-operator.git
git remote add upstream-authbridge https://github.com/kagenti/kagenti-extensions.git
```

### 3. Run the initial sync manually

```bash
# Sync authbridge for the first time
./scripts/sync-upstream.sh authbridge

# Sync operator (if not already present)
./scripts/sync-upstream.sh operator
```

### 4. Verify the repo layout

```bash
ls kagenti-operator/    # Operator source
ls kagenti-authbridge/  # AuthBridge source (authlib/, cmd/authbridge-proxy/, proxy-init/)
ls patches/             # Carried patches
```

## Building Images

### Operator image

```bash
podman build -t agents-operator:latest -f kagenti-operator/Dockerfile kagenti-operator/
```

### Sidecar image (with plugin exclusion)

```bash
podman build -t authbridge-proxy:latest \
  --build-arg GO_BUILD_TAGS="exclude_plugin_ibac" \
  -f kagenti-authbridge/cmd/authbridge-proxy/Dockerfile \
  kagenti-authbridge/
```

### Verify plugin exclusion

```bash
# Run the sidecar and check the plugin catalog
podman run --rm authbridge-proxy:latest --config /dev/null 2>&1 | grep -i ibac
# Should produce no output (IBAC not registered)
```

## Creating a Carried Patch

When you need a midstream-specific change before upstream accepts it:

```bash
# 1. Make the change in the synced source
cd kagenti-authbridge
# ... edit files ...

# 2. Generate the patch
git diff > ../patches/authbridge/001-description.patch

# 3. Commit the patch file (not the source change)
cd ..
git checkout -- kagenti-authbridge/  # revert source change
git add patches/authbridge/001-description.patch
git commit -m "Add carried patch: description"
```

The sync automation will apply this patch after each upstream sync.

## Automated Sync (CI)

The GitHub Actions workflow `.github/workflows/sync-upstream.yml` runs on a schedule:

- **Trigger**: Daily at 06:00 UTC (or on upstream tag)
- **Action**: Checks each upstream for new commits, creates/updates a sync PR
- **Review**: A maintainer reviews and merges the PR

To trigger a manual sync:

```bash
gh workflow run sync-upstream.yml
```
