# Tasks: Gert Ecosystem v0

**Input**: Design documents from `/specs/002-ecosystem-v0/`
**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/

**Tests**: Constitution Principle III (NON-NEGOTIABLE): Every task MUST include supporting unit or regression tests.

**Organization**: Tasks grouped by user story for independent implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story (US1–US8)
- Exact file paths included

---

## Phase 1: Setup

**Purpose**: Project structure, shared infrastructure, dependency wiring

- [X] T001 Create `pkg/ecosystem/` directory structure per plan.md in pkg/ecosystem/tui/, pkg/ecosystem/mcp/, pkg/ecosystem/approval/stdin/, pkg/ecosystem/recorder/
- [X] T002 Create `cmd/gert/` directory with Cobra CLI wiring (copy from cmd/gert-kernel/, then extend) in cmd/gert/main.go
- [X] T003 [P] Create `cmd/gert-tui/` entrypoint with Bubble Tea bootstrap in cmd/gert-tui/main.go
- [X] T004 [P] Create `cmd/gert-mcp/` entrypoint with MCP server bootstrap in cmd/gert-mcp/main.go
- [X] T005 [P] Create `tools/` directory with 5 tool pack YAML stubs in tools/curl.tool.yaml, tools/kubectl.tool.yaml, tools/az.tool.yaml, tools/ping.tool.yaml, tools/jq.tool.yaml
- [X] T006 [P] Create `runbooks/` directory with 5 runbook YAML stubs in runbooks/service-health-diagnostic.yaml, runbooks/multi-endpoint-sweep.yaml, runbooks/k8s-pod-restart.yaml, runbooks/dns-http-chain.yaml, runbooks/incident-triage.yaml

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core kernel changes that ALL user stories depend on. context.Context threading must complete first.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T007 Add `context.Context` parameter to `ToolExecutor.Execute()` interface and update `defaultExecutor` in pkg/kernel/engine/engine.go
- [X] T008 Add `context.Context` parameter to `Engine.Run()` and thread through `executeSteps()` → `executeStep()` in pkg/kernel/engine/engine.go
- [X] T009 Update all engine tests to pass `context.Background()` to `Run()` in pkg/kernel/engine/engine_test.go
- [X] T010 Update `cmd/gert/main.go` to pass context to engine in cmd/gert/main.go
- [X] T011 [P] Update `pkg/kernel/testing/runner.go` to pass context through replay execution in pkg/kernel/testing/runner.go
- [X] T012 [P] Update `pkg/kernel/testing/integration_test.go` to pass context in pkg/kernel/testing/integration_test.go
- [X] T013 Verify all 72 existing tests pass with context threading — `go test ./pkg/kernel/... -count=1`

**Checkpoint**: Foundation ready — all existing tests pass with context.Context. User story work can begin.

---

## Phase 3: User Story 1 — Contract-Governed Tools (Priority: P1) 🎯 MVP

**Goal**: Effects taxonomy replaces side_effects. Governance matches on effects. Secrets declared and validated.

**Independent Test**: `gert validate` passes with effects-based tool definitions; governance rules trigger on effects.

### Tests for US1

- [ ] T014 [P] [US1] Test: effects field accepted by contract in pkg/kernel/contract/contract_test.go
- [ ] T015 [P] [US1] Test: side_effects deprecated — warning when used alone, error when both declared in pkg/kernel/validate/validate_test.go
- [ ] T016 [P] [US1] Test: governance matches on effects + writes in pkg/kernel/governance/governance_test.go
- [ ] T017 [P] [US1] Test: derived risk from effects + writes classification in pkg/kernel/contract/contract_test.go
- [ ] T018 [P] [US1] Test: secrets block validation — warning on missing env var in pkg/kernel/validate/validate_test.go
- [ ] T019 [P] [US1] Test: secrets auto-redaction in trace output in pkg/kernel/trace/trace_test.go
- [ ] T020 [P] [US1] Testdata fixture: tool with effects field only in pkg/kernel/validate/testdata/effects_valid.yaml
- [ ] T021 [P] [US1] Testdata fixture: tool with both side_effects and effects in pkg/kernel/validate/testdata/effects_conflict.yaml
- [ ] T022 [P] [US1] Testdata fixture: tool with side_effects only (deprecated) in pkg/kernel/validate/testdata/side_effects_deprecated.yaml

### Implementation for US1

- [ ] T023 [US1] Add `Effects []string` field to `contract.Contract` struct in pkg/kernel/contract/contract.go
- [ ] T024 [US1] Update `Risk()` method to use effects + writes instead of side_effects in pkg/kernel/contract/contract.go
- [ ] T025 [US1] Add `Effects` matching to governance rule evaluation in pkg/kernel/governance/governance.go
- [ ] T026 [US1] Add `SecretRef` struct to schema types in pkg/kernel/schema/types.go
- [ ] T027 [US1] Add `Secrets []SecretRef` to `ToolMeta` and `Meta` in pkg/kernel/schema/types.go and pkg/kernel/schema/tool.go
- [ ] T028 [US1] Add validation rule: error if both `side_effects` and `effects` declared in pkg/kernel/validate/domain.go
- [ ] T029 [US1] Add validation rule: deprecation warning on `side_effects` usage in pkg/kernel/validate/domain.go
- [ ] T030 [US1] Add validation rule: secrets env var presence check (warning) in pkg/kernel/validate/domain.go
- [ ] T031 [US1] Add secrets redaction to trace writer output in pkg/kernel/trace/trace.go
- [ ] T032 [US1] Update dry-run mode to show effects + secrets info in pkg/kernel/engine/engine.go
- [ ] T033 Write/update 5 tool pack YAML files with effects taxonomy (update existing curl/ping, create new kubectl/az/jq) in tools/curl.tool.yaml, tools/kubectl.tool.yaml, tools/az.tool.yaml, tools/ping.tool.yaml, tools/jq.tool.yaml
- [ ] T034 [US1] Write service-health-diagnostic runbook with effects-based governance in runbooks/service-health-diagnostic.yaml
- [ ] T035 [US1] Write 2+ scenario tests for service-health-diagnostic in runbooks/scenarios/service-health-diagnostic/

**Checkpoint**: Effects taxonomy + secrets convention working. `gert validate` and `gert exec --mode dry-run` show correct behavior. ≥10 new tests pass.

---

## Phase 4: User Story 2 — Resumable Approval (Priority: P1)

**Goal**: Ticket-based ApprovalProvider with Submit/Wait. State persistence for resume.

**Independent Test**: Runbook with require-approval gate submits ticket, persists state, resumes.

### Tests for US2

- [ ] T036 [P] [US2] Test: ApprovalProvider Submit returns ticket immediately in pkg/kernel/engine/engine_test.go
- [ ] T037 [P] [US2] Test: stdin approval provider Submit+Wait atomic in pkg/kernel/engine/engine_test.go
- [ ] T038 [P] [US2] Test: state persistence SaveState/LoadState round-trip in pkg/kernel/engine/state_test.go
- [ ] T039 [P] [US2] Test: approval timeout expiry treated as rejection in pkg/kernel/engine/engine_test.go
- [ ] T040 [P] [US2] Test: signature verification rejects invalid signature in pkg/kernel/engine/engine_test.go
- [ ] T041 [P] [US2] Test: trace emits approval_submitted and approval_resolved events in pkg/kernel/trace/trace_test.go

### Implementation for US2

- [ ] T042 [US2] Define `ApprovalProvider` interface (Submit/Wait) in pkg/kernel/engine/engine.go
- [ ] T043 [US2] Define `ApprovalRequest`, `ApprovalTicket`, `ApprovalResponse` types in pkg/kernel/engine/engine.go
- [ ] T044 [US2] Implement `stdinApprovalProvider` (Submit+Wait atomic) in pkg/ecosystem/approval/stdin/provider.go
- [ ] T045 [US2] Add `ApprovalProvider` to `RunConfig` and wire into engine in pkg/kernel/engine/engine.go
- [ ] T046 [US2] Replace current `requestApproval()` with `ApprovalProvider.Submit()` + `Wait()` in pkg/kernel/engine/engine.go
- [ ] T047 [US2] Implement `SaveState()` and `LoadState()` for run persistence in pkg/kernel/engine/state.go
- [ ] T048 [US2] Add `approval_submitted` and `approval_resolved` trace events in pkg/kernel/trace/trace.go
- [ ] T049 [US2] Add `governance.approval_timeout` parsing to schema in pkg/kernel/schema/types.go
- [ ] T050 [US2] Add `gert resume --run <id>` command to CLI in cmd/gert/main.go
- [ ] T050a [P] [US2] Test: `gert resume` CLI command loads state and resumes in cmd/gert/resume_test.go
- [ ] T051 [US2] Write k8s-pod-restart runbook exercising approval gate in runbooks/k8s-pod-restart.yaml
- [ ] T052 [US2] Write 2+ scenario tests for k8s-pod-restart in runbooks/scenarios/k8s-pod-restart/

**Checkpoint**: Approval Submit/Wait works. State persists and resumes. ≥6 new tests pass.

---

## Phase 5: User Story 3 — Scoped State and Keyed Fan-Out (Priority: P2)

**Goal**: scope, export, visibility, for_each.key, repeat block — MAD-ready primitives.

**Independent Test**: Runbook with scoped state + keyed fan-out produces map outputs. Trace includes scope/visibility events.

### Tests for US3

- [ ] T053 [P] [US3] Test: scope field normalizes `/` to `.` in pkg/kernel/schema/schema_test.go
- [ ] T054 [P] [US3] Test: export promotes scope-local outputs to global in pkg/kernel/engine/engine_test.go
- [ ] T055 [P] [US3] Test: export collision with existing global → runtime error in pkg/kernel/engine/engine_test.go
- [ ] T056 [P] [US3] Test: for_each.key produces map-structured outputs in pkg/kernel/engine/engine_test.go
- [ ] T057 [P] [US3] Test: for_each.key duplicate keys → runtime error in pkg/kernel/engine/engine_test.go
- [ ] T058 [P] [US3] Test: visibility_applied trace event emitted in pkg/kernel/trace/trace_test.go
- [ ] T059 [P] [US3] Test: scope_export trace event emitted in pkg/kernel/trace/trace_test.go
- [ ] T060 [P] [US3] Test: repeat block iterates max times in pkg/kernel/engine/engine_test.go
- [ ] T061 [P] [US3] Test: repeat block stops on until condition in pkg/kernel/engine/engine_test.go
- [ ] T062 [P] [US3] Test: visibility glob matching (* and **) in pkg/kernel/engine/visibility_test.go

### Implementation for US3

- [ ] T063 [US3] Add `Scope`, `Export`, `Visibility` fields to Step struct in pkg/kernel/schema/types.go
- [ ] T064 [US3] Add `ForEachKey` field to ForEach struct in pkg/kernel/schema/types.go
- [ ] T065 [US3] Add `RepeatBlock` struct and `StepRepeat` type to schema in pkg/kernel/schema/types.go
- [ ] T066 [US3] Implement scope path normalization (`/` → `.`) in schema loader in pkg/kernel/schema/loader.go
- [ ] T067 [US3] Implement scoped variable namespace in engine (global/step/scope) in pkg/kernel/engine/engine.go
- [ ] T068 [US3] Implement export promotion (scope → global) with collision detection in pkg/kernel/engine/engine.go
- [ ] T069 [US3] Implement for_each.key producing map-structured outputs in pkg/kernel/engine/engine.go
- [ ] T070 [US3] Implement visibility glob matching engine in pkg/kernel/engine/visibility.go
- [ ] T071 [US3] Implement repeat block execution with max + until in pkg/kernel/engine/engine.go
- [ ] T072 [US3] Add `scope_export`, `visibility_applied`, `repeat_start`, `repeat_iteration` trace events in pkg/kernel/trace/trace.go
- [ ] T073 [US3] Add validation rules for scope/export/visibility/repeat in pkg/kernel/validate/domain.go
- [ ] T074 [US3] Write multi-endpoint-sweep runbook using for_each.key in runbooks/multi-endpoint-sweep.yaml
- [ ] T075 [US3] Write 2+ scenario tests for multi-endpoint-sweep in runbooks/scenarios/multi-endpoint-sweep/

**Checkpoint**: Scoped state + keyed fan-out + repeat working. MAD skeleton pattern can execute. ≥10 new tests pass.

---

## Phase 6: User Story 4 — Kernel Input Resolution (Priority: P2)

**Goal**: ResolveInputs() kernel API used by all hosts. Consistent resolution order + trace provenance.

**Independent Test**: ResolveInputs produces same results from CLI and programmatic call; trace records sources.

### Tests for US4

- [ ] T076 [P] [US4] Test: ResolveInputs resolution order (CLI → provider → default) in pkg/kernel/engine/resolve_test.go
- [ ] T077 [P] [US4] Test: ResolveInputs emits input_resolved trace events in pkg/kernel/engine/resolve_test.go
- [ ] T078 [P] [US4] Test: missing required input returns error in pkg/kernel/engine/resolve_test.go
- [ ] T079 [P] [US4] Test: CLI var overrides provider binding in pkg/kernel/engine/resolve_test.go

### Implementation for US4

- [ ] T080 [US4] Define `InputResolver` interface and `InputBinding` type in pkg/kernel/engine/resolve.go
- [ ] T081 [US4] Implement `ResolveInputs()` kernel API with resolution order in pkg/kernel/engine/resolve.go
- [ ] T082 [US4] Add `input_resolved` trace event type in pkg/kernel/trace/trace.go
- [ ] T083 [US4] Add `From` field to `contract.ParamDef` in schema in pkg/kernel/contract/contract.go
- [ ] T084 [US4] Wire ResolveInputs into cmd/gert exec flow in cmd/gert/main.go
- [ ] T085 [US4] Write dns-http-chain runbook with mix of from: bindings in runbooks/dns-http-chain.yaml
- [ ] T086 [US4] Write 2+ scenario tests for dns-http-chain in runbooks/scenarios/dns-http-chain/

**Checkpoint**: Input resolution deterministic. Trace records sources. ≥4 new tests pass.

---

## Phase 7: User Story 5 — Trace Integrity (Priority: P2)

**Goal**: Hash chaining + signing on trace. `gert trace verify` command.

**Independent Test**: Trace file has prev_hash on every event. Tampered trace detected. Signature verified.

### Tests for US5

- [ ] T087 [P] [US5] Test: every trace event includes prev_hash in pkg/kernel/trace/trace_test.go
- [ ] T088 [P] [US5] Test: first event has zero-hash genesis in pkg/kernel/trace/trace_test.go
- [ ] T089 [P] [US5] Test: modified event breaks chain verification in pkg/kernel/trace/trace_test.go
- [ ] T090 [P] [US5] Test: run_complete includes chain_hash and signature in pkg/kernel/trace/trace_test.go
- [ ] T091 [P] [US5] Test: run_start includes actor, host, gert_version, runbook_hash, tool_hashes in pkg/kernel/trace/trace_test.go
- [ ] T092 [P] [US5] Test: principal attribution on step events in pkg/kernel/trace/trace_test.go

### Implementation for US5

- [ ] T093 [US5] Add `prevJSON` and `prevHash` to trace.Writer; compute SHA-256 in Emit() in pkg/kernel/trace/trace.go
- [ ] T094 [US5] Add `chain_hash` and `signature` fields to run_complete event in pkg/kernel/trace/trace.go
- [ ] T095 [US5] Add `Principal` struct to trace event types in pkg/kernel/trace/trace.go
- [ ] T096 [US5] Add run identity (actor, host, version, hashes) to run_start emission in pkg/kernel/engine/engine.go
- [ ] T097 [US5] Implement `gert trace verify` command in cmd/gert/main.go
- [ ] T097a [P] [US5] Test: `gert trace verify` detects broken chain and validates signature in cmd/gert/trace_verify_test.go
- [ ] T098 [US5] Add `GERT_TRACE_SIGNING_KEY` / `GERT_TRACE_SIGNING_KEY_ID` env var support in pkg/kernel/trace/trace.go
- [ ] T099 [US5] Update RunConfig to include Actor and Host fields in pkg/kernel/engine/engine.go

**Checkpoint**: All traces have hash chains. Signing + verification works. ≥6 new tests pass.

---

## Phase 8: User Story 6 — TUI (Priority: P3)

**Goal**: `gert-tui` binary with step list, output panel, approval UX.

**Independent Test**: `gert-tui` completes a replay runbook with visual progress.

### Tests for US6

- [ ] T100 [P] [US6] Test: TUI model initializes from runbook in pkg/ecosystem/tui/model_test.go
- [ ] T101 [P] [US6] Test: TUI updates step status on trace events in pkg/ecosystem/tui/model_test.go

### Implementation for US6

- [ ] T102 [US6] Implement TUI Bubble Tea model (app state, message types) in pkg/ecosystem/tui/model.go
- [ ] T102a [US6] Add migration task: deprecate existing `pkg/tui/` — port reusable components (styles, layout patterns) to `pkg/ecosystem/tui/`, mark old package as legacy
- [ ] T103 [US6] Implement step list view with status icons in pkg/ecosystem/tui/views.go
- [ ] T104 [US6] Implement output panel view in pkg/ecosystem/tui/views.go
- [ ] T105 [US6] Implement status bar view in pkg/ecosystem/tui/views.go
- [ ] T106 [US6] Implement TUI ToolExecutor wrapper (feeds output to panel) in pkg/ecosystem/tui/executor.go
- [ ] T107 [US6] Implement TUI ApprovalProvider (modal prompt in TUI) in pkg/ecosystem/tui/approval.go
- [ ] T108 [US6] Wire TUI model to kernel engine in cmd/gert-tui/main.go
- [ ] T109 [US6] Add key bindings (q, v, c, t, /) in pkg/ecosystem/tui/keys.go

**Checkpoint**: `gert-tui` runs a replay scenario with visual output. ≥2 new tests pass.

---

## Phase 9: User Story 7 — MCP Server (Priority: P3)

**Goal**: `gert-mcp` binary exposing validate/exec/test/schema as MCP tools.

**Independent Test**: MCP client calls gert/validate and receives structured result.

### Tests for US7

- [ ] T110 [P] [US7] Test: MCP validate handler returns correct result in pkg/ecosystem/mcp/handlers_test.go
- [ ] T111 [P] [US7] Test: MCP exec handler returns outcome in pkg/ecosystem/mcp/handlers_test.go

### Implementation for US7

- [ ] T112 [US7] Implement MCP tool handlers (validate, exec, test, schema) in pkg/ecosystem/mcp/handlers.go
- [ ] T113 [US7] Register MCP tools and resources in pkg/ecosystem/mcp/server.go
- [ ] T114 [US7] Wire MCP server to stdio transport in cmd/gert-mcp/main.go
- [ ] T115 [US7] Add MCP SDK dependency to go.mod

**Checkpoint**: `gert-mcp` responds to all 4 standard tool calls. ≥2 new tests pass.

---

## Phase 10: User Story 8 — Watch Mode (Priority: P3)

**Goal**: `gert watch` loop with interval and stop-on semantics.

**Independent Test**: `gert watch` runs 3+ times and stops on escalated outcome.

### Tests for US8

- [ ] T116 [P] [US8] Test: watch loop runs N times with interval in cmd/gert/watch_test.go
- [ ] T117 [P] [US8] Test: watch stops on matching outcome category in cmd/gert/watch_test.go
- [ ] T118 [P] [US8] Test: watch stops on engine error in cmd/gert/watch_test.go

### Implementation for US8

- [ ] T119 [US8] Implement `gert watch` command with interval loop in cmd/gert/main.go
- [ ] T120 [US8] Implement `--stop-on` category matching in cmd/gert/watch.go
- [ ] T121 [US8] Implement per-run trace file naming with timestamps in cmd/gert/watch.go
- [ ] T122 [US8] Add one-line summary output per run in cmd/gert/watch.go

**Checkpoint**: Watch mode works with all stop semantics. ≥3 new tests pass.

---

## Phase 11: Polish & Cross-Cutting Concerns

**Purpose**: Remaining runbooks, cleanup, documentation.

- [ ] T123 [P] Write incident-triage runbook with extension metadata in runbooks/incident-triage.yaml
- [ ] T124 [P] Write 2+ scenario tests for incident-triage in runbooks/scenarios/incident-triage/
- [ ] T125 [P] Implement contract violation detection in engine after tool step execution in pkg/kernel/engine/engine.go
- [ ] T125a [P] Implement `contract_violations: deny` governance matcher — promotes violations to step errors in pkg/kernel/governance/governance.go
- [ ] T125b [P] Test: `contract_violations: deny` halts on violation in pkg/kernel/governance/governance_test.go
- [ ] T126 [P] Test contract violation detection (undeclared outputs, missing outputs) in pkg/kernel/engine/engine_test.go
- [ ] T127 [P] Implement probe mode (`--mode probe`) in engine in pkg/kernel/engine/engine.go
- [ ] T128 [P] Test probe mode (writes skipped, read-only executed) in pkg/kernel/engine/engine_test.go
- [ ] T129 [P] Implement extension runner JSON-RPC client in pkg/kernel/executor/extension.go
- [ ] T130 [P] Test extension runner protocol (initialize/execute/shutdown) in pkg/kernel/executor/extension_test.go
- [ ] T131 [P] Implement auto-record mode (Recorder wrapping ToolExecutor) in pkg/ecosystem/recorder/recorder.go
- [ ] T131a [P] Wire secret redaction into recorder output — scenario files must never contain secret values in pkg/ecosystem/recorder/recorder.go
- [ ] T131b [P] Test: recorded scenario redacts secrets from tool stdout in pkg/ecosystem/recorder/recorder_test.go
- [ ] T132 [P] Test auto-record captures tool responses into scenario.yaml in pkg/ecosystem/recorder/recorder_test.go
- [ ] T133 [P] Implement `gert diff` command for scenario diffing in cmd/gert/main.go
- [ ] T133a [P] Test: `gert diff` detects outcome changes across scenarios in cmd/gert/diff_test.go
- [ ] T134 [P] Implement `gert outcomes` command for aggregation in cmd/gert/main.go
- [ ] T134a [P] Test: `gert outcomes` aggregates from trace files correctly in cmd/gert/outcomes_test.go
- [ ] T135 Run quickstart.md validation — execute all scenarios in quickstart end-to-end
- [ ] T136 Verify all existing 72 tests + new tests pass: `go test ./pkg/kernel/... -count=1`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies — start immediately
- **Phase 2 (Foundation)**: Depends on Phase 1 — BLOCKS all user stories
- **Phases 3-4 (P1 stories)**: Depend on Phase 2. Can run in parallel if staffed.
- **Phases 5-7 (P2 stories)**: Depend on Phase 2. Can run in parallel with P1 if different packages.
- **Phases 8-10 (P3 stories)**: Depend on Phase 2 + stable kernel interfaces from P1/P2.
- **Phase 11 (Polish)**: Depends on all desired user stories being complete.

### User Story Independence

- **US1 (Effects + Secrets)**: Independent — modifies contract, governance, validate, trace
- **US2 (Approval)**: Independent — modifies engine, trace. May integrate with US1 governance.
- **US3 (Scoped State)**: Independent — modifies engine, schema, trace. No US1/US2 dependency.
- **US4 (Input Resolution)**: Independent — new engine module. No other US dependency.
- **US5 (Trace Integrity)**: Independent — modifies trace. Can run after or parallel to US1-US4.
- **US6 (TUI)**: Depends on stable engine interfaces (after US1-US5 kernel changes settle)
- **US7 (MCP)**: Depends on stable engine interfaces (after US1-US5)
- **US8 (Watch)**: Independent — simple CLI loop, no kernel changes

### Within Each User Story

1. Tests MUST be written and FAIL before implementation (Constitution Principle III)
2. Schema types before engine logic
3. Engine logic before CLI wiring
4. Core implementation before integration
5. Story complete before moving to next priority

---

## Summary

| Metric | Count |
|--------|-------|
| Total tasks | 147 |
| Setup tasks | 6 |
| Foundation tasks | 7 |
| US1 tasks (P1) | 22 |
| US2 tasks (P1) | 18 |
| US3 tasks (P2) | 23 |
| US4 tasks (P2) | 11 |
| US5 tasks (P2) | 14 |
| US6 tasks (P3) | 11 |
| US7 tasks (P3) | 6 |
| US8 tasks (P3) | 7 |
| Polish tasks | 22 |
| Parallelizable tasks | 82 |
| MVP scope (US1 only) | 35 tasks (Setup + Foundation + US1) |

