#!/usr/bin/env bash
# sync-authbridge.sh - Sync authbridge/ from kagenti/kagenti-extensions upstream.
#
# Can be run standalone for local debugging or called from the GitHub Actions
# workflow (.github/workflows/sync-authbridge.yaml).
#
# Usage:
#   ./scripts/sync/sync-authbridge.sh [--branch <branch>] [--commit <sha>] [--dry-run]
#
# Options:
#   --branch <branch>   Upstream branch to sync from (default: main)
#   --commit <sha>      Specific upstream commit SHA to sync to (overrides branch tip)
#   --dry-run           Show what would change without modifying the working tree
#
# Environment variables (set by GHA or manually):
#   SOURCE_BRANCH       Same as --branch (CLI flag takes precedence)
#   UPSTREAM_COMMIT     Same as --commit (CLI flag takes precedence)
#   DRY_RUN             Set to "true" for dry-run mode
#   REPO_ROOT           Repository root (default: auto-detected via git)

set -euo pipefail

# Disable git pager so commands never drop into less/more
export GIT_PAGER=cat

# --------------------------------------------------------------------------- #
# Configuration
# --------------------------------------------------------------------------- #
UPSTREAM_REPO="https://github.com/kagenti/kagenti-extensions.git"
TARGET_DIR="authbridge"
PATCHES_DIR="patches/authbridge"
CLONE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/kagenti-extensions.XXXXXXXX")"
trap 'rm -rf "${CLONE_DIR}"' EXIT

# ODH-specific files that live inside authbridge/ and must survive rsync --delete.
# These are files added by the midstream that do not exist upstream.
ODH_EXCLUDES=(
)

# --------------------------------------------------------------------------- #
# Parse arguments
# --------------------------------------------------------------------------- #
source_branch="${SOURCE_BRANCH:-main}"
upstream_commit="${UPSTREAM_COMMIT:-}"
dry_run="${DRY_RUN:-false}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --branch)
      [[ $# -ge 2 ]] || { echo "ERROR: --branch requires a value" >&2; exit 1; }
      source_branch="$2"; shift 2 ;;
    --commit)
      [[ $# -ge 2 ]] || { echo "ERROR: --commit requires a value" >&2; exit 1; }
      upstream_commit="$2"; shift 2 ;;
    --dry-run) dry_run="true"; shift ;;
    *)         echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

# --------------------------------------------------------------------------- #
# Resolve repo root
# --------------------------------------------------------------------------- #
REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "${REPO_ROOT}"

echo "==> Sync configuration"
echo "    Upstream repo:   ${UPSTREAM_REPO}"
echo "    Source branch:   ${source_branch}"
echo "    Upstream commit: ${upstream_commit:-<latest>}"
echo "    Target dir:      ${TARGET_DIR}"
echo "    Patches dir:     ${PATCHES_DIR}"
echo "    Dry run:         ${dry_run}"
echo ""

# --------------------------------------------------------------------------- #
# Step 1: Clone upstream (sparse - authbridge/ only)
# --------------------------------------------------------------------------- #
echo "==> Cloning upstream (sparse - ${TARGET_DIR}/ only)..."

if [[ -n "${upstream_commit}" ]]; then
  git clone --filter=blob:none --sparse \
    "${UPSTREAM_REPO}" \
    "${CLONE_DIR}"
  git -C "${CLONE_DIR}" sparse-checkout set "${TARGET_DIR}"
  git -C "${CLONE_DIR}" checkout "${upstream_commit}"
else
  git clone --depth 1 --filter=blob:none --sparse \
    --branch "${source_branch}" \
    "${UPSTREAM_REPO}" \
    "${CLONE_DIR}"
  git -C "${CLONE_DIR}" sparse-checkout set "${TARGET_DIR}"
fi

upstream_sha=$(git -C "${CLONE_DIR}" rev-parse --short HEAD)
upstream_sha_full=$(git -C "${CLONE_DIR}" rev-parse HEAD)

echo "    Upstream SHA: ${upstream_sha} (${upstream_sha_full})"

# Export for GHA consumption
if [[ -n "${GITHUB_ENV:-}" ]]; then
  echo "UPSTREAM_SHA=${upstream_sha}" >> "${GITHUB_ENV}"
  echo "UPSTREAM_SHA_FULL=${upstream_sha_full}" >> "${GITHUB_ENV}"
fi

# --------------------------------------------------------------------------- #
# Step 2: Guard against unexpected deletions
# --------------------------------------------------------------------------- #
# Dry-run rsync to find all files --delete would remove. Files that came from
# upstream (introduced by a sync or import commit) are safe to delete — upstream
# removed them intentionally. Only files added by non-sync commits
# (midstream-only) or listed in ODH_EXCLUDES need protection.
#
# A file is considered upstream-origin if the commit that first added it to the
# local repo matches one of these patterns:
#   - sync: *           (regular sync commits)
#   - feat: *add*authbridge*from*kagenti*  (initial imports)
#   - Squashed '*'*     (git subtree squash imports)
is_upstream_origin_commit() {
  local msg="$1"
  case "${msg}" in
    sync:*) return 0 ;;
    feat:*[Aa]dd*authbridge*from*kagenti*) return 0 ;;
    "Squashed '"*) return 0 ;;
    *) return 1 ;;
  esac
}

if [[ -d "${TARGET_DIR}" ]]; then
  echo ""
  echo "==> Checking for unexpected deletions..."

  exclude_pattern="$(printf '%s\n' "${ODH_EXCLUDES[@]}")"
  unexpected_deletions=()
  allowed_deletions=()

  while IFS= read -r line; do
    [[ "${line}" == \*deleting* ]] || continue
    file="${line#\*deleting }"; file="${file#"${file%%[! ]*}"}"; file="${file%/}"
    [[ -d "${TARGET_DIR}/${file}" ]] && continue
    echo "${exclude_pattern}" | grep -qxF "${file}" && continue

    # Check if the file was originally introduced by an upstream sync/import
    # commit. Walk all commits that added this file (across renames, rebases)
    # and check the earliest one.
    first_commit_msg=$(git log --all --diff-filter=A --format='%s' \
      -- "${TARGET_DIR}/${file}" 2>/dev/null | tail -1)
    if [[ -n "${first_commit_msg}" ]] && is_upstream_origin_commit "${first_commit_msg}"; then
      echo "    Allowing upstream deletion: ${file}"
      allowed_deletions+=("${file}")
      continue
    fi

    unexpected_deletions+=("${file}")
  done < <(rsync -a --delete --dry-run --itemize-changes \
    "${CLONE_DIR}/${TARGET_DIR}/" "${TARGET_DIR}/" 2>/dev/null || true)

  if [[ ${#unexpected_deletions[@]} -gt 0 ]]; then
    echo "ERROR: Files in ${TARGET_DIR}/ would be deleted but are not in ODH_EXCLUDES:" >&2
    printf '         - %s\n' "${unexpected_deletions[@]}" >&2
    echo "" >&2
    echo "  To fix, either:" >&2
    echo "    1. Add them to ODH_EXCLUDES in this script (if they should be preserved)" >&2
    echo "    2. Move them outside ${TARGET_DIR}/" >&2
    echo "    3. Delete them before running the sync" >&2
    exit 1
  fi
  echo "    No unexpected deletions."

  # Export allowed deletions for commit/PR metadata
  if [[ ${#allowed_deletions[@]} -gt 0 ]]; then
    allowed_deletions_text=$(printf '  - %s\n' "${allowed_deletions[@]}")
    echo ""
    echo "    Upstream deletions (${#allowed_deletions[@]} file(s)):"
    printf '      %s\n' "${allowed_deletions[@]}"
    if [[ -n "${GITHUB_ENV:-}" ]]; then
      {
        echo "UPSTREAM_DELETIONS<<EOF_DELETIONS"
        echo "${allowed_deletions_text}"
        echo "EOF_DELETIONS"
      } >> "${GITHUB_ENV}"
    fi
  fi
fi

# --------------------------------------------------------------------------- #
# Step 3: Rsync with --delete, preserving ODH-specific files
# --------------------------------------------------------------------------- #
echo ""
echo "==> Syncing ${TARGET_DIR}/ via rsync..."

rsync_args=(-a --delete)
for exclude in "${ODH_EXCLUDES[@]}"; do
  rsync_args+=(--exclude="${exclude}")
done

if [[ "${dry_run}" == "true" ]]; then
  rsync_args+=(--dry-run --itemize-changes)
fi

rsync "${rsync_args[@]}" \
  "${CLONE_DIR}/${TARGET_DIR}/" \
  "${TARGET_DIR}/"

# --------------------------------------------------------------------------- #
# Step 4: Apply carried patches (if any)
# --------------------------------------------------------------------------- #
if [[ -d "${PATCHES_DIR}" ]]; then
  shopt -s nullglob
  patches=("${PATCHES_DIR}"/*.patch)
  shopt -u nullglob
  if [[ ${#patches[@]} -gt 0 ]]; then
    echo ""
    echo "==> Applying ${#patches[@]} carried patch(es)..."
    for patch_file in "${patches[@]}"; do
      echo "    Applying: ${patch_file}"
      if [[ "${dry_run}" == "true" ]]; then
        if ! git apply --check "${patch_file}"; then
          echo "ERROR: Patch would fail to apply: ${patch_file}" >&2
          echo "  The upstream change likely conflicts with this carried patch." >&2
          echo "  Please update or remove the patch and re-run the sync." >&2
          exit 1
        fi
        echo "      (would apply cleanly)"
      else
        if ! git apply "${patch_file}"; then
          echo "ERROR: Patch failed to apply: ${patch_file}" >&2
          echo "  The upstream change likely conflicts with this carried patch." >&2
          echo "  Please update or remove the patch and re-run the sync." >&2
          exit 1
        fi
      fi
    done
  else
    echo ""
    echo "==> No carried patches found in ${PATCHES_DIR}/"
  fi  # end patch count check
else
  echo ""
  echo "==> No patches directory (${PATCHES_DIR}/) - skipping patch step"
fi

# --------------------------------------------------------------------------- #
# Step 5: Detect changes
# --------------------------------------------------------------------------- #
echo ""
git add -A "${TARGET_DIR}/"

if git diff --cached --quiet; then
  echo "==> No changes detected - ${TARGET_DIR}/ is already up to date."
  # Export for GHA consumption
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    echo "has_changes=false" >> "${GITHUB_OUTPUT}"
  fi
  exit 0
else
  echo "==> Changes detected in ${TARGET_DIR}/:"
  git diff --cached --stat
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    echo "has_changes=true" >> "${GITHUB_OUTPUT}"
  fi
fi

if [[ "${dry_run}" == "true" ]]; then
  echo ""
  echo "==> Dry run complete. No changes committed."
  git reset HEAD -- "${TARGET_DIR}/" >/dev/null 2>&1 || true
  exit 0
fi

echo ""
echo "==> Sync complete. Changes staged and ready for commit."
echo "    Upstream SHA: ${upstream_sha_full}"
echo "    Source branch: ${source_branch}"
