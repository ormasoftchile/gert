# Tasks: Governed Executable Runbook Engine v0

**Input**: Design documents from `/specs/001-runbook-engine-v0/`
**Prerequisites**: plan.md ✓, spec.md ✓, research.md ✓, data-model.md ✓, contracts/ ✓, quickstart.md ✓

**Tests**: Included per Constitution Principle VII (Test-First Development). Test tasks are marked and should be written to FAIL before implementation begins.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Go CLI**: `cmd/gert/` (entry point), `pkg/` (library packages)
- **VS Code Extension**: `vscode/src/`
- **Shared Schema**: `schemas/`
- **Test Fixtures**: `testdata/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization, Go module, and Cobra CLI skeleton

- [X] T001 Create project directory structure per plan.md (cmd/gert/, pkg/schema/, pkg/runtime/, pkg/governance/, pkg/providers/, pkg/debugger/, pkg/replay/, pkg/compiler/, pkg/evidence/, pkg/assertions/, schemas/, testdata/valid/, testdata/invalid/, testdata/scenarios/, testdata/tsgs/)
- [X] T002 Initialize Go module and install dependencies (cobra, gopkg.in/yaml.v3, github.com/invopop/jsonschema, github.com/santhosh-tekuri/jsonschema/v6, github.com/chzyer/readline, github.com/yuin/goldmark) in go.mod
- [X] T003 [P] Create Cobra CLI skeleton with root command and subcommand stubs (validate, exec, debug, compile, schema export, version) in cmd/gert/main.go

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core type definitions and interfaces that ALL user stories depend on

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T004 Define all runbook schema Go structs (Runbook, Meta, Defaults, GovernancePolicy, RedactionRule, EvidencePolicy, Step, CLIStepConfig, EvidenceRequirement, ApprovalRequirement, Assertion, JSONPathAssertion) with yaml/json/jsonschema struct tags in pkg/schema/schema.go
- [X] T005 [P] Define Provider, CommandExecutor, EvidenceCollector interfaces and shared types (CommandResult, ValidationResult, AttachmentInfo, Approval) in pkg/providers/provider.go
- [X] T006 [P] Define runtime state types (RunState, StepResult, EvidenceValue, AssertionResult, TraceEvent) in pkg/runtime/types.go
- [X] T007 [P] Implement JSON Schema generation from Go structs using invopop/jsonschema and generate initial schemas/runbook-v0.json in pkg/schema/export.go
- [X] T008 [P] Create golden-file test fixtures: valid runbooks in testdata/valid/ and invalid runbooks (unknown fields, missing required, governance violations) in testdata/invalid/

**Checkpoint**: Foundation ready — all types, interfaces, schema, and fixtures in place. User story implementation can begin.

---

## Phase 3: User Story 1 — Validate a Runbook Definition (Priority: P1) 🎯 MVP

**Goal**: A runbook author can validate a YAML file against the schema, receiving clear errors for unknown fields, missing required fields, governance violations, and undefined variable references.

**Independent Test**: Pass sample YAML files through `gert validate` and verify accept/reject decisions match golden-file expectations. Run `gert schema export` and confirm Draft 2020-12 JSON Schema output.

### Tests for User Story 1

> **Write these tests FIRST, ensure they FAIL before implementation**

- [X] T009 [P] [US1] Write schema parsing tests (accept valid YAML, reject unknown fields, reject missing required fields, reject invalid types) in pkg/schema/schema_test.go
- [X] T010 [P] [US1] Write domain validation tests (step ID uniqueness, undefined variable references, governance consistency, invalid regex patterns, empty argv) in pkg/schema/validate_test.go

### Implementation for User Story 1

- [X] T011 [US1] Implement strict YAML parsing with yaml.v3 Decoder.KnownFields(true) and runbook loading function in pkg/schema/schema.go
- [X] T012 [US1] Implement 3-phase validation pipeline (structural yaml.v3 decode, semantic JSON Schema validation via santhosh-tekuri/jsonschema/v6, domain-level custom rules) in pkg/schema/validate.go
- [X] T013 [US1] Implement `gert validate` command with structured error output (error count, per-error location, severity) in cmd/gert/main.go
- [X] T014 [US1] Implement `gert schema export` command (output JSON Schema Draft 2020-12 to stdout) in cmd/gert/main.go
- [X] T015 [US1] Implement `gert version` command (print version, build commit) in cmd/gert/main.go

**Checkpoint**: `gert validate` accepts valid runbooks and rejects invalid ones with clear errors. `gert schema export` outputs the canonical JSON Schema. User Story 1 is fully functional and independently testable.

---

## Phase 4: User Story 2 — Execute a Runbook in Real Mode (Priority: P1)

**Goal**: An on-call engineer executes a runbook step-by-step. CLI steps run real commands with captured output. Manual steps pause for structured evidence. The system produces an immutable JSONL trace, state snapshots, and enforces governance (allowlists, redaction, env var blocking). Execution halts on failure with resumption support.

**Independent Test**: Execute a sample runbook with 3+ CLI steps and 1+ manual step. Verify trace.jsonl contains all StepResults, snapshots/ contains per-step snapshots, governance violations are blocked, and resumption from snapshot works.

### Tests for User Story 2

> **Write these tests FIRST, ensure they FAIL before implementation**

- [X] T016 [P] [US2] Write governance engine tests (command allowlist acceptance/rejection, denylist blocking, env var pattern matching, overlapping allow/deny detection) in pkg/governance/governance_test.go
- [X] T017 [P] [US2] Write assertion evaluation tests for all 7 types (contains, not_contains, matches, exit_code, equals, not_equals, json_path) in pkg/assertions/assertions_test.go
- [X] T018 [P] [US2] Write trace writer tests (JSONL append, flush after write, valid JSON per line) and snapshot tests (RunState serialization round-trip) in pkg/runtime/trace_test.go and pkg/runtime/snapshot_test.go
- [X] T019 [P] [US2] Write runtime engine integration test (multi-step execution, capture accumulation, halt-on-failure, per-step timeout behavior, run ID format validation, trace verification) in pkg/runtime/engine_test.go

### Implementation for User Story 2

- [X] T020 [P] [US2] Implement command allowlist/denylist evaluation (argv[0] check, allow-only mode, deny-only mode, combined mode) in pkg/governance/allowlist.go
- [X] T021 [P] [US2] Implement output redaction engine (compile regex patterns, apply replacements to captured output before persistence) in pkg/governance/redaction.go
- [X] T022 [P] [US2] Implement denied env var pattern blocking (glob pattern matching, block during template resolution) in pkg/governance/envblock.go
- [X] T023 [P] [US2] Implement assertion evaluation engine (contains, not_contains, matches, exit_code, equals, not_equals, json_path with JSONPath query) in pkg/assertions/assertions.go
- [X] T024 [P] [US2] Implement evidence types (text, checklist, attachment) and SHA256 hashing with file size recording in pkg/evidence/evidence.go and pkg/evidence/hash.go
- [X] T025 [P] [US2] Implement JSONL trace writer with bufio.Writer, json.Encoder, and os.File.Sync() at step boundaries in pkg/runtime/trace.go
- [X] T026 [P] [US2] Implement state snapshot persistence (RunState JSON serialization to .runbook/runs/<run_id>/snapshots/step-NNNN.json) in pkg/runtime/snapshot.go
- [X] T027 [US2] Implement RealExecutor (os/exec.CommandContext wrapper with per-step timeout support) and CLI provider (Validate argv, Execute with governance enforcement, capture extraction, assertion evaluation, redaction) in pkg/providers/cli.go
- [X] T028 [US2] Implement InteractiveCollector (CLI prompts for text input, checklist completion, file attachment with hash verification) and Manual provider (Validate instructions/evidence reqs, Execute with evidence collection and approval recording) in pkg/providers/manual.go
- [X] T029 [US2] Implement template variable resolution using text/template with Option("missingkey=error") and variable extraction for static analysis in pkg/runtime/engine.go
- [X] T030 [US2] Implement runtime engine state machine (run ID generation as timestamp+suffix, step scheduling, variable resolution, capture accumulation, governance pre-check, provider dispatch, assertion evaluation, halt-on-failure) in pkg/runtime/engine.go
- [X] T031 [US2] Implement execution resumption from most recent snapshot (restore RunState, re-open trace for append, continue from failed step) in pkg/runtime/resume.go
- [X] T032 [US2] Implement `gert exec` command for real mode (--as flag, --resume flag, progress output, artifact paths, exit codes per CLI contract) in cmd/gert/main.go

**Checkpoint**: `gert exec runbook.yaml --as engineer` runs CLI + manual steps, produces trace.jsonl + snapshots, enforces governance, halts on failure, and supports `--resume`. User Story 2 is fully functional and independently testable.

---

## Phase 5: User Story 3 — Debug a Runbook Interactively (Priority: P2)

**Goal**: An engineer uses an interactive CLI debugger to step through a runbook one step at a time, inspecting variables, captures, and history, submitting evidence for manual steps, and taking snapshots at any point.

**Independent Test**: Launch `gert debug` on a sample runbook and verify each debugger command (next, print vars, print captures, history, evidence set/check/attach, approve, snapshot, dump, help, quit) produces correct output.

### Tests for User Story 3

> **Write these tests FIRST, ensure they FAIL before implementation**

- [X] T033 [P] [US3] Write debugger command handler tests (next advances step, print vars/captures shows state, history shows results, evidence commands record evidence, snapshot persists state) in pkg/debugger/debugger_test.go

### Implementation for User Story 3

- [X] T034 [US3] Implement interactive REPL loop with chzyer/readline (line editing, history file, tab completion for commands, colored prompt format `gert[step N/total | step_id]> `) in pkg/debugger/debugger.go
- [X] T035 [US3] Implement all debugger command handlers (next, continue, dump, print vars, print captures, history, evidence set, evidence check, evidence attach, approve --as, snapshot, help, quit) in pkg/debugger/commands.go
- [X] T036 [US3] Implement `gert debug` command (--mode real, --as flag, REPL initialization, engine integration) in cmd/gert/main.go

**Checkpoint**: `gert debug runbook.yaml` starts an interactive session where the engineer can step through execution, inspect state, submit evidence, and take snapshots. User Story 3 is fully functional and independently testable.

---

## Phase 6: User Story 4 — Replay a Runbook Execution Offline (Priority: P2)

**Goal**: An engineer replays a runbook execution using pre-recorded CLI responses and evidence from a scenario file. Replay produces deterministic, identical traces across multiple runs.

**Independent Test**: Execute a runbook in replay mode with a scenario file, run it twice, and verify both traces are bit-identical. Confirm CLI steps route through ReplayExecutor and manual steps use pre-recorded evidence.

### Tests for User Story 4

> **Write these tests FIRST, ensure they FAIL before implementation**

- [X] T037 [P] [US4] Write replay tests (scenario parsing, command matching, deterministic output, missing command fail-closed) in pkg/replay/replay_test.go
- [X] T038 [P] [US4] Create replay scenario test fixtures (commands + evidence for sample runbooks) in testdata/scenarios/

### Implementation for User Story 4

- [X] T039 [US4] Implement scenario file YAML parsing (ScenarioCommand list with argv/stdout/stderr/exit_code, evidence map keyed by step_id) in pkg/replay/scenario.go
- [X] T040 [US4] Implement ReplayExecutor (match command+args against scenario entries, return pre-recorded response, fail-closed on no match) in pkg/replay/replay.go
- [X] T041 [US4] Implement ScenarioCollector (return pre-recorded evidence for manual steps from scenario file, reuse_evidence mode) in pkg/providers/manual.go
- [X] T042 [US4] Wire replay mode into `gert exec --mode replay --scenario <file>` and `gert debug --mode replay --scenario <file>` in cmd/gert/main.go

**Checkpoint**: `gert exec runbook.yaml --mode replay --scenario scenario.yaml` produces deterministic traces using pre-recorded responses. User Story 4 is fully functional and independently testable.

---

## Phase 7: User Story 6 — Dry-Run a Runbook (Priority: P3)

**Goal**: An engineer previews what a runbook will do without executing any commands. Dry-run resolves variables, evaluates governance, and reports the execution plan with zero side effects.

**Independent Test**: Run a sample runbook in dry-run mode and verify zero commands are executed, no files are modified outside `.runbook/`, and a complete execution plan is produced showing resolved variables and governance evaluation.

### Tests for User Story 6

> **Write these tests FIRST, ensure they FAIL before implementation**

- [X] T043 [P] [US6] Write dry-run mode tests (zero side effects, variable resolution shown, governance violations reported without halting, complete plan output) in pkg/runtime/engine_test.go

### Implementation for User Story 6

- [X] T044 [US6] Implement DryRunCollector (return placeholder evidence values without user prompts) and dry-run command executor (report commands without executing) in pkg/providers/manual.go
- [X] T045 [US6] Wire dry-run mode into runtime engine (skip real execution, report planned actions) and `gert exec --mode dry-run` in cmd/gert/main.go

**Checkpoint**: `gert exec runbook.yaml --mode dry-run` shows the full execution plan with resolved variables and governance evaluation, with zero side effects. User Story 6 is fully functional and independently testable.

---

## Phase 8: User Story 5 — Compile a TSG into a Runbook (Priority: P3)

**Goal**: A runbook author converts an existing Markdown TSG into a schema-valid runbook.yaml and a mapping.md report. Ambiguous or unsafe steps are emitted as `type: manual`. Variables are extracted into `meta.vars`.

**Independent Test**: Compile 5+ sample TSGs from testdata/tsgs/ and verify each produces a schema-valid runbook.yaml (passes `gert validate`) and a mapping.md with correct section-to-step mappings.

### Tests for User Story 5

> **Write these tests FIRST, ensure they FAIL before implementation**

- [X] T046 [P] [US5] Create sample TSG Markdown test fixtures (headings + code blocks, pure prose, mixed, ambiguous commands, variable references) in testdata/tsgs/
- [X] T047 [P] [US5] Write compiler tests (TSG → valid runbook.yaml, mapping.md correctness, manual fallback for unsafe steps, variable extraction) in pkg/compiler/compile_test.go

### Implementation for User Story 5

- [X] T048 [US5] Implement Markdown AST extraction using yuin/goldmark (walk AST for headings → step boundaries, fenced code blocks → CLI argv candidates, paragraphs → manual instructions, lists → checklist items) in pkg/compiler/ir.go
- [X] T049 [US5] Define LLM prompt contract template (inputs: TSG text + schema + ontology + governance defaults; outputs: runbook.yaml + mapping.md; rules: manual for unsafe, no invented credentials) in pkg/compiler/prompt.go
- [X] T050 [US5] Implement IR → runbook.yaml + mapping.md compilation pipeline (TSG-IR to structured runbook, variable extraction to meta.vars, mapping report generation) in pkg/compiler/compile.go
- [X] T051 [US5] Implement `gert compile` command (--out, --mapping flags, progress output, post-compilation validation) in cmd/gert/main.go

**Checkpoint**: `gert compile tsg.md --out runbook.yaml --mapping mapping.md` converts a Markdown TSG into a schema-valid runbook with accurate mapping report. User Story 5 is fully functional and independently testable.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: VS Code extension schema parity, cross-stack validation, end-to-end verification

- [X] T052 [P] Initialize VS Code extension project structure (package.json with dependencies, tsconfig.json with strict mode, esbuild config) in vscode/
- [X] T053 [P] Implement TypeScript schema validation using ajv v8 against schemas/runbook-v0.json (compile schema at activation, validate documents, map errors to VS Code Diagnostics) in vscode/src/schema/validate.ts
- [X] T054 [P] Write cross-stack golden-file parity tests (feed testdata/valid/ and testdata/invalid/ through both Go and TS validators, assert identical accept/reject decisions) in vscode/src/schema/validate.test.ts
- [X] T055 Run quickstart.md end-to-end validation (build cmd/gert/, create sample runbook, validate, exec real, debug, dry-run, replay, compile, schema export)
- [X] T056 Code cleanup, error message polish, and README documentation in cmd/gert/ and pkg/

---

## Dependencies & Execution Order

### Phase Dependencies

```
Phase 1: Setup ─────────────────────────────────────────────┐
                                                            │
Phase 2: Foundational ──────────────────────────────────────┤ BLOCKS all stories
                                                            │
    ┌───────────────────────────┬───────────────────────────┘
    │                           │
Phase 3: US1 (P1)          Phase 4: US2 (P1)
Validate Runbook            Execute Real Mode
    │                           │
    │   ┌───────────────────────┼───────────────────────────┐
    │   │                       │                           │
    │  Phase 5: US3 (P2)   Phase 6: US4 (P2)               │
    │  Debug Interactively  Replay Offline                  │
    │   │                       │                           │
    │   │                       │                           │
    │  Phase 7: US6 (P3)   Phase 8: US5 (P3)               │
    │  Dry-Run              Compile TSG                     │
    │   │                       │                           │
    └───┴───────────────────────┴───────────────────────────┘
                                                            │
Phase 9: Polish ────────────────────────────────────────────┘
```

### User Story Dependencies

- **US1 (Validate)**: Depends on Foundational (Phase 2) only. No dependencies on other stories.
- **US2 (Execute)**: Depends on Foundational (Phase 2) only. No dependencies on other stories. Can be developed in parallel with US1 if team capacity allows.
- **US3 (Debug)**: Depends on US2 (runtime engine, providers). The debugger wraps the runtime in an interactive REPL.
- **US4 (Replay)**: Depends on US2 (runtime engine, providers). Adds ReplayExecutor and ScenarioCollector to existing interfaces.
- **US6 (Dry-Run)**: Depends on US2 (runtime engine). Adds DryRunCollector and no-op executor to existing interfaces.
- **US5 (Compile)**: Depends on US1 (schema validation for post-compilation check). Can be developed after or in parallel with US3/US4/US6.

### Within Each User Story

1. Tests written FIRST (must FAIL before implementation)
2. Models/types before services
3. Package-level implementations before engine integration
4. Engine integration before CLI command wiring
5. CLI command wiring last (ties everything together)

### Parallel Opportunities

**Phase 2 (Foundational)**: T005, T006, T007, T008 can all run in parallel after T004 completes.

**US1**: T009, T010 (tests) in parallel. Then T011 → T012 sequential. T013, T014, T015 sequential (same file).

**US2**: T016–T019 (tests) all in parallel. T020–T026 (governance, assertions, evidence, trace, snapshot) all in parallel (different packages). Then T027, T028 (providers). Then T029 → T030 (engine). Then T031, T032.

**US3**: T033 (test) alone. Then T034 → T035 → T036 sequential.

**US4**: T037, T038 in parallel. Then T039 → T040 → T041 → T042 sequential.

**US5**: T046, T047 in parallel. Then T048 → T049 → T050 → T051 sequential.

**Polish**: T052, T053, T054 all in parallel.

---

## Parallel Examples

### User Story 1 — Parallel Test Launch
```
# Launch all US1 tests together:
T009: "Schema parsing tests in pkg/schema/schema_test.go"
T010: "Domain validation tests in pkg/schema/validate_test.go"
```

### User Story 2 — Parallel Implementation Batch
```
# Launch all independent US2 packages together:
T020: "Command allowlist/denylist in pkg/governance/allowlist.go"
T021: "Output redaction engine in pkg/governance/redaction.go"
T022: "Denied env var blocking in pkg/governance/envblock.go"
T023: "Assertion evaluation in pkg/assertions/assertions.go"
T024: "Evidence types + hashing in pkg/evidence/evidence.go + hash.go"
T025: "JSONL trace writer in pkg/runtime/trace.go"
T026: "State snapshot persistence in pkg/runtime/snapshot.go"
```

### Cross-Story Parallelism
```
# After Foundational phase, US1 and US2 can start simultaneously:
Developer A: US1 (T009–T015) — Schema validation
Developer B: US2 (T016–T032) — Runtime execution
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001–T003)
2. Complete Phase 2: Foundational (T004–T008)
3. Complete Phase 3: User Story 1 — Validate (T009–T015)
4. **STOP and VALIDATE**: `gert validate` works on all testdata/ fixtures
5. Schema contract is stable — proceed to runtime

### Incremental Delivery

1. **Setup + Foundational** → Types, interfaces, schema, fixtures ready
2. **+ US1 (Validate)** → `gert validate` + `gert schema export` (MVP!)
3. **+ US2 (Execute)** → `gert exec --mode real` with full governance and tracing
4. **+ US3 (Debug)** → `gert debug` interactive REPL
5. **+ US4 (Replay)** → `gert exec --mode replay --scenario` deterministic offline testing
6. **+ US6 (Dry-Run)** → `gert exec --mode dry-run` safe preview
7. **+ US5 (Compile)** → `gert compile` TSG conversion (schema must be stable first)
8. **+ Polish** → VS Code extension parity, cross-stack tests, end-to-end validation

### Parallel Team Strategy

With multiple developers after Foundational phase:

- **Developer A**: US1 (Validate) → US5 (Compile)
- **Developer B**: US2 (Execute) → US3 (Debug) → US4 (Replay) → US6 (Dry-Run)

Each story is independently testable and delivers incremental value.

---

## Notes

- **[P]** tasks target different files with no dependencies on incomplete tasks in the same phase
- **[Story]** label maps each task to a specific user story for traceability
- Each user story checkpoint is independently verifiable
- Commit after each task or logical group
- The implementation order follows gert-spec.md §15: Schema → Runtime → Debugger → cli-replay → Compiler
- All tests use Go's built-in `go test` — no additional test framework needed
- VS Code extension (Phase 9) is a cross-cutting concern, not a user story — it shares the schema contract
