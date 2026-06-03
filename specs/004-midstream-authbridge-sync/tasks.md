# Tasks: Midstream AuthBridge Sync

**Input**: Design documents from `/specs/004-midstream-authbridge-sync/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, quickstart.md

**Tests**: No test tasks included (not requested in spec). Validation is via manual sync runs and image builds.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Prepare the midstream repo structure and tooling

- [ ] T001 Create `kagenti-authbridge/` directory on midstream branch
- [ ] T002 Create `patches/authbridge/` and `patches/operator/` directories on midstream branch
- [ ] T003 [P] Create `.sync-state` JSON file with initial empty state at repo root on midstream branch
- [ ] T004 [P] Evaluate `openshift-knative/deviate` (install, test with a dry-run against kagenti-operator upstream) and document findings in `scripts/sync/README.md`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Upstream PR and carried patch that enable build-time plugin selection

**Note**: This phase includes an upstream contribution. Tasks T005-T007 target the upstream `kagenti-extensions` repo. If the upstream PR is not yet merged when later phases begin, the carried patch (T008) provides the same functionality in midstream.

- [ ] T005 [Upstream] Create `authbridge/authlib/plugins/ibac/register.go` in kagenti-extensions, moving `RegisterPlugin` call from `plugin.go` with `//go:build !exclude_plugin_ibac` build constraint
- [ ] T006 [Upstream] Update `authbridge/authlib/plugins/ibac/plugin.go` in kagenti-extensions to remove the `init()` + `RegisterPlugin` call (now in `register.go`)
- [ ] T007 [Upstream] Submit PR to `kagenti/kagenti-extensions` implementing T005-T006 (references issue [#476](https://github.com/kagenti/kagenti-extensions/issues/476))
- [ ] T008 Generate carried patch `patches/authbridge/001-ibac-build-tag.patch` from the changes in T005-T006 for midstream use until upstream merges

**Checkpoint**: Build-tag mechanism ready (either via upstream merge or carried patch)

---

## Phase 3: User Story 1 - Automated Upstream Sync (Priority: P1) MVP

**Goal**: Upstream changes from both kagenti-operator and kagenti-authbridge appear as reviewable PRs in the midstream repo automatically.

**Independent Test**: Run the sync script manually against current upstream state; verify it creates a PR with the expected files under `kagenti-authbridge/` and `kagenti-operator/`.

### Implementation for User Story 1

- [ ] T009 [US1] Create sync configuration for kagenti-authbridge: define include paths (`authlib/`, `cmd/authbridge-proxy/`, `proxy-init/`), exclude paths (`cmd/authbridge-envoy/`, `cmd/authbridge-lite/`, `cmd/abctl/`, `demos/`, `docs/`, `tests/`), and upstream URL in `scripts/sync/config-authbridge.yaml` (or `deviate.yaml` if using deviate)
- [ ] T010 [P] [US1] Create sync configuration for kagenti-operator: define include/exclude paths and upstream URL in `scripts/sync/config-operator.yaml`
- [ ] T011 [US1] Create sync script `scripts/sync/sync-upstream.sh` that: (a) reads config for a named upstream, (b) fetches upstream, (c) computes diff since last synced SHA, (d) copies changed files into target directory, (e) applies carried patches from `patches/<upstream>/`, (f) updates `.sync-state`, (g) creates/updates a GitHub PR via `gh`
- [ ] T012 [US1] Run initial manual sync of kagenti-authbridge into `kagenti-authbridge/` on midstream branch and verify file layout
- [ ] T013 [US1] Run initial manual sync of kagenti-operator into `kagenti-operator/` on midstream branch (updating existing content) and verify
- [ ] T014 [US1] Create GitHub Actions workflow `.github/workflows/sync-upstream.yml` that runs `scripts/sync/sync-upstream.sh` on a daily schedule (06:00 UTC) for both upstreams, with manual trigger support

**Checkpoint**: Both upstreams sync automatically via PR. Human review required before merge.

---

## Phase 4: User Story 2 - Build-Time Plugin Exclusion (Priority: P1)

**Goal**: The midstream sidecar image excludes IBAC plugin at build time via Go build tags.

**Independent Test**: Build the sidecar image with `exclude_plugin_ibac` tag; query the plugin catalog endpoint and verify IBAC is not listed.

### Implementation for User Story 2

- [ ] T015 [US2] Create midstream Dockerfile at `kagenti-authbridge/Dockerfile.midstream` (or modify synced Dockerfile via carried patch) that passes `--build-arg GO_BUILD_TAGS="exclude_plugin_ibac"` and uses `go build -tags "${GO_BUILD_TAGS}"` in the build stage
- [ ] T016 [US2] Verify that building with `exclude_plugin_ibac` tag produces a binary where the IBAC plugin is not registered (run binary with `--config /dev/null` and check for "ibac" in error output or logs)
- [ ] T017 [US2] Verify that building without any tags (upstream default) still includes all plugins including IBAC
- [ ] T018 [P] [US2] Document the build-tag mechanism and plugin inclusion/exclusion in `kagenti-authbridge/BUILD.md`

**Checkpoint**: Sidecar image builds with correct plugin subset. IBAC excluded, all others included.

---

## Phase 5: User Story 3 - Two Container Images From One Repo (Priority: P2)

**Goal**: Both operator and sidecar images build independently from the same midstream repo.

**Independent Test**: From the midstream repo root, build both images using their respective Dockerfiles; verify each produces a working container.

### Implementation for User Story 3

- [ ] T019 [US3] Verify operator Dockerfile at `kagenti-operator/Dockerfile` builds successfully from midstream repo root (build context = `kagenti-operator/`)
- [ ] T020 [US3] Verify sidecar Dockerfile at `kagenti-authbridge/Dockerfile.midstream` builds successfully from midstream repo root (build context = `kagenti-authbridge/`)
- [ ] T021 [P] [US3] Add path-based CI trigger configuration to `.github/workflows/` so that authbridge-only changes trigger only the sidecar build, and operator-only changes trigger only the operator build
- [ ] T022 [US3] Document the two-image build process in the midstream repo's `README.md`

**Checkpoint**: Both images build independently. Path-based CI triggers avoid unnecessary rebuilds.

---

## Phase 6: User Story 4 - Runtime Plugin Configuration (Priority: P3)

**Goal**: Confirm that runtime plugin activation/deactivation via YAML config works correctly with the midstream plugin subset.

**Independent Test**: Deploy the midstream sidecar with a config that omits a plugin and verify it is not active; use `on_error: off` to disable another and verify.

### Implementation for User Story 4

- [ ] T023 [US4] Create a sample pipeline YAML config for the midstream sidecar at `kagenti-authbridge/config/midstream-default.yaml` listing the product plugin set (jwt-validation, token-exchange, token-broker, a2a-parser, mcp-parser, inference-parser)
- [ ] T024 [US4] Verify that deploying with this config activates only listed plugins (check `/stats` or `/v1/pipeline` endpoint output)
- [ ] T025 [US4] Verify `on_error: off` disables a plugin at runtime without redeployment (modify config, trigger reload, check endpoint)

**Checkpoint**: Runtime plugin configuration works as expected with midstream plugin subset.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Documentation and cleanup

- [ ] T026 [P] Update midstream repo `README.md` with sync process overview, build instructions, and links to upstream repos
- [ ] T027 [P] Update `quickstart.md` in specs with actual paths and commands based on implementation
- [ ] T028 Validate full end-to-end flow: sync both upstreams, apply patches, build both images, verify plugin exclusion
- [ ] T029 Create `SYNC.md` at repo root documenting: how to add a new upstream, how to create/remove carried patches, how to add/remove plugin exclusions

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies, can start immediately
- **Foundational (Phase 2)**: Independent of Phase 1, can start in parallel (upstream PR work)
- **User Story 1 (Phase 3)**: Depends on Phase 1 (directories exist) and Phase 2 (carried patch available for initial sync)
- **User Story 2 (Phase 4)**: Depends on Phase 2 (build tags exist) and Phase 3 (authbridge source synced)
- **User Story 3 (Phase 5)**: Depends on Phase 3 (both sources synced) and Phase 4 (sidecar Dockerfile ready)
- **User Story 4 (Phase 6)**: Depends on Phase 4 (sidecar image buildable)
- **Polish (Phase 7)**: Depends on all previous phases

### User Story Dependencies

- **US1 (Sync)**: Depends on Setup + Foundational. No dependency on other stories.
- **US2 (Plugin Exclusion)**: Depends on US1 (source must be synced first) and Foundational (build tags).
- **US3 (Two Images)**: Depends on US1 + US2. Integration validation of both images.
- **US4 (Runtime Config)**: Depends on US2 (needs buildable sidecar image). Mostly validation work.

### Parallel Opportunities

- T003 and T004 can run in parallel (setup phase)
- T009 and T010 can run in parallel (sync configs for different upstreams)
- T015 and T018 can run in parallel (Dockerfile + docs)
- T021 and T022 can run in parallel (CI config + docs)
- T026 and T027 can run in parallel (docs)
- Foundational phase (T005-T008) can run in parallel with Setup (T001-T004)

---

## Parallel Example: User Story 1

```bash
# Launch sync configs in parallel (different files):
Task: "Create sync config for kagenti-authbridge in scripts/sync/config-authbridge.yaml"
Task: "Create sync config for kagenti-operator in scripts/sync/config-operator.yaml"

# Then sequentially:
Task: "Create sync script scripts/sync/sync-upstream.sh"
Task: "Run initial sync of authbridge"
Task: "Run initial sync of operator"
Task: "Create GitHub Actions workflow"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (directories, state file)
2. Complete Phase 2: Foundational (upstream PR + carried patch)
3. Complete Phase 3: User Story 1 (sync automation)
4. **STOP and VALIDATE**: Run sync manually, verify PRs created correctly
5. Deploy CI workflow

### Incremental Delivery

1. Setup + Foundational -> Infrastructure ready
2. Add US1 (Sync) -> Both upstreams sync via automated PRs (MVP!)
3. Add US2 (Plugin Exclusion) -> Sidecar builds with correct plugin set
4. Add US3 (Two Images) -> Both images build from one repo, path-based CI
5. Add US4 (Runtime Config) -> Validation of runtime behavior
6. Polish -> Documentation, end-to-end validation

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- The upstream PR (T005-T007) is an external dependency; if it blocks, the carried patch (T008) provides the same capability in midstream
- All sync work targets the `midstream` branch in `opendatahub-io/agents-operator`
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
