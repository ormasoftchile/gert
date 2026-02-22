# Feature Specification: Governed Executable Runbook Engine v0

**Feature Branch**: `001-runbook-engine-v0`  
**Created**: 2026-02-11  
**Status**: Draft  
**Input**: User description: "Build governed executable runbook platform - schema, runtime, debugger, replay, compiler"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Validate a Runbook Definition (Priority: P1)

A runbook author writes a runbook YAML file describing an incident response procedure. Before sharing it with the team, the author validates the file against the runbook schema to ensure it is well-formed, all required fields are present, governance policies are defined, and no unknown fields are included.

**Why this priority**: Schema validation is the foundation of the entire platform. Every other capability — execution, debugging, replay, compilation — depends on a valid, well-defined runbook format. Without this, nothing else can function.

**Independent Test**: Can be fully tested by passing sample YAML files through the validator and verifying accept/reject decisions. Delivers immediate value by catching malformed runbooks before execution.

**Acceptance Scenarios**:

1. **Given** a runbook YAML with all required fields and valid step definitions, **When** the author validates it, **Then** the system reports the runbook as valid with no errors.
2. **Given** a runbook YAML with an unknown field (e.g., `priority: high`), **When** the author validates it, **Then** the system rejects it with a clear error identifying the unknown field and its location.
3. **Given** a runbook YAML missing a required field (e.g., a CLI step without `argv`), **When** the author validates it, **Then** the system rejects it with a clear error naming the missing field.
4. **Given** a runbook YAML with a command not listed in `allowed_commands`, **When** the author validates it, **Then** the system warns about the governance policy violation.
5. **Given** a runbook YAML referencing an undefined variable in a template expression, **When** the author validates it, **Then** the system reports the undefined variable reference.

---

### User Story 2 - Execute a Runbook in Real Mode (Priority: P1)

An on-call engineer (DRI) receives an incident and needs to follow a runbook to diagnose and mitigate the issue. The engineer loads the runbook and executes it step-by-step. CLI steps run actual commands with output captured. Manual steps pause execution and prompt the engineer for structured evidence (checklists, text notes, screenshots). The system records every step result, producing an immutable execution trace and state snapshots.

**Why this priority**: This is the core value proposition — governed, traceable execution of incident runbooks. Without execution, the platform is just a validator.

**Independent Test**: Can be tested by executing a sample runbook with at least 3 CLI steps and 1 manual step, verifying the execution trace (JSONL) and at least one state snapshot are produced correctly.

**Acceptance Scenarios**:

1. **Given** a valid runbook with CLI steps, **When** the engineer executes it in real mode, **Then** each CLI step runs the specified command and captures stdout/stderr.
2. **Given** a runbook with a manual step requiring a checklist and text evidence, **When** execution reaches that step, **Then** the system pauses and prompts the engineer to provide the required evidence before continuing.
3. **Given** a runbook with assertions (e.g., `not_contains: "CrashLoopBackOff"`), **When** a step's captured output violates the assertion, **Then** the step is marked as failed and the violation is clearly reported.
4. **Given** a runbook with governance policies including denied environment variables, **When** template resolution encounters a denied variable pattern, **Then** the system blocks resolution and reports the violation.
5. **Given** a runbook with redaction rules, **When** a CLI step produces output matching the redaction pattern, **Then** the stored output has the matched content replaced with the redaction placeholder.
6. **Given** any execution, **When** a step completes, **Then** a StepResult event is appended to the JSONL trace and a state snapshot is persisted.
7. **Given** a halted execution (step failure, timeout, or crash), **When** the engineer resumes the run, **Then** the system restores state from the last snapshot and re-executes from the failed step, continuing to append to the same trace.

---

### User Story 3 - Debug a Runbook Interactively (Priority: P2)

An engineer wants to walk through a runbook step-by-step with full visibility into the current state — variables, captured values, execution history, and evidence status. The engineer uses an interactive debugger to advance one step at a time, inspect state, submit evidence for manual steps, and take snapshots at any point.

**Why this priority**: The debugger is the primary user interface for the CLI. It transforms raw execution into a guided, inspectable experience. Depends on the runtime (P1) being functional first.

**Independent Test**: Can be tested by launching the debugger on a sample runbook and verifying each debugger command (`next`, `print vars`, `print captures`, `history`, `evidence set`, `snapshot`, etc.) produces correct output.

**Acceptance Scenarios**:

1. **Given** a runbook loaded in the debugger, **When** the engineer types `next`, **Then** the next step executes and the debugger shows the result.
2. **Given** a runbook mid-execution, **When** the engineer types `print vars`, **Then** all current variable names and values are displayed.
3. **Given** a runbook mid-execution, **When** the engineer types `print captures`, **Then** all captured values from previous steps are displayed.
4. **Given** a runbook mid-execution, **When** the engineer types `history`, **Then** all previously executed steps with their statuses are displayed.
5. **Given** a manual step requiring evidence, **When** the engineer uses `evidence set`, `evidence check`, or `evidence attach`, **Then** the evidence is recorded and the step can proceed once all required evidence is provided.
6. **Given** a runbook mid-execution, **When** the engineer types `snapshot`, **Then** a full state snapshot is persisted to disk immediately.
7. **Given** a manual step with approval requirements, **When** the engineer types `approve --as <name>`, **Then** the approval is recorded against the step.

---

### User Story 4 - Replay a Runbook Execution Offline (Priority: P2)

An engineer or reviewer wants to re-execute a runbook in a controlled environment without performing real operations. Using a scenario file and the `cli-replay` integration, the engineer replays the execution with pre-recorded command responses to verify correctness, test new runbooks, or audit past incidents.

**Why this priority**: Replay enables safe testing and deterministic verification. It depends on the runtime (P1) but can be developed independently from the debugger. Critical for building confidence in runbook correctness before real use.

**Independent Test**: Can be tested by providing a runbook and a scenario file, running in replay mode, and verifying the outputs match expected results deterministically.

**Acceptance Scenarios**:

1. **Given** a runbook and a scenario.yaml with pre-recorded CLI responses, **When** the engineer executes in replay mode, **Then** CLI steps are routed through `cli-replay` instead of actual command execution.
2. **Given** a replayed execution with identical inputs, **When** replayed multiple times, **Then** the execution trace and state transitions are identical each time.
3. **Given** a runbook with manual steps and pre-recorded evidence in the scenario, **When** replayed, **Then** the manual steps use the stored evidence without prompting.
4. **Given** a completed replay execution, **When** verification runs, **Then** the system confirms all required steps were satisfied, all assertions passed, and all evidence was present.

---

### User Story 5 - Compile a TSG into a Runbook (Priority: P3)

A runbook author has an existing prose-based TSG written in Markdown. The author wants to convert it into a schema-valid runbook without manually rewriting the entire document. The system extracts structure from the Markdown, interprets the steps, and produces a draft runbook along with a mapping report that explains how each section was translated.

**Why this priority**: The compiler is the highest-value convenience feature but depends on a stable schema and runtime to produce meaningful output. It is built last to ensure the target format is stable.

**Independent Test**: Can be tested by compiling 5-10 sample TSGs and verifying each produces a schema-valid runbook.yaml and a mapping.md with correct section-to-step mappings.

**Acceptance Scenarios**:

1. **Given** a Markdown TSG with headings and code blocks, **When** the author compiles it, **Then** the system produces a runbook.yaml that passes schema validation.
2. **Given** a compiled TSG, **When** the author reviews the mapping.md, **Then** each step in the runbook is mapped to the corresponding TSG section with explanations.
3. **Given** a TSG with ambiguous or potentially unsafe commands, **When** compiled, **Then** those steps are emitted as `type: manual` with TODO annotations.
4. **Given** a TSG with inline variable references (e.g., `$NAMESPACE`), **When** compiled, **Then** the variables are extracted into `meta.vars` with template expressions in step definitions.
5. **Given** a TSG with no explicit commands (pure prose instructions), **When** compiled, **Then** those sections are emitted as manual steps with the prose as instructions.

---

### User Story 6 - Dry-Run a Runbook (Priority: P3)

An engineer wants to preview what a runbook will do without executing any commands. The dry-run mode walks through each step, resolves variables, evaluates governance policies, and reports what would happen — without performing any side effects.

**Why this priority**: Dry-run is a safe preview mode that adds confidence before real execution. It depends on the runtime but is a simpler execution mode.

**Independent Test**: Can be tested by running a sample runbook in dry-run mode and verifying zero side effects occur and a complete execution plan is produced.

**Acceptance Scenarios**:

1. **Given** a valid runbook, **When** executed in dry-run mode, **Then** no commands are actually run and no external side effects occur.
2. **Given** a runbook with template variables, **When** dry-run executes, **Then** all variable resolutions are shown in the output.
3. **Given** a runbook with governance violations (disallowed commands), **When** dry-run executes, **Then** the violations are reported without halting.

---

### Edge Cases

- ~~What happens when a CLI step's command times out or hangs indefinitely?~~ Resolved: per-step optional timeout with global default; exceeded timeout terminates the command and halts execution.
- ~~How does the system handle a step that references a capture from a previous step that failed?~~ Resolved: execution halts on step failure, so subsequent steps referencing failed captures are never reached.
- What happens when attachment evidence files are missing or inaccessible at the specified path?
- How does the system behave when the `.runbook/` directory is on a read-only filesystem?
- What happens when a runbook YAML uses an `apiVersion` that is not recognized?
- How does the system handle a step with an empty `argv` list?
- What happens when redaction patterns are themselves invalid regular expressions?
- How does the system behave when the same step `id` is used more than once in a runbook?
- What happens when a manual step's approval requirement cannot be met (e.g., required role not available)?

## Requirements *(mandatory)*

### Functional Requirements

**Schema & Validation**:
- **FR-001**: System MUST parse runbook YAML with strict mode, rejecting any unknown or unrecognized fields.
- **FR-002**: System MUST validate all required fields are present for each step type (`cli`, `manual`).
- **FR-003**: System MUST validate governance policies (allowed_commands, deny_env_vars, redaction patterns) are well-formed.
- **FR-004**: System MUST validate that all variable references in template expressions resolve to defined variables in `meta.vars`.
- **FR-005**: System MUST validate that step `id` values are unique within a runbook.
- **FR-006**: System MUST support exporting the runbook schema as a JSON Schema document.

**Runtime & Execution**:
- **FR-007**: System MUST execute runbook steps sequentially as a deterministic state machine.
- **FR-008**: System MUST support three execution modes: `real`, `replay`, and `dry-run`.
- **FR-009**: System MUST resolve template variables (e.g., `{{ .namespace }}`) before executing each step.
- **FR-010**: System MUST capture step outputs (stdout/stderr) and store them according to the step's `capture` configuration.
- **FR-011**: System MUST evaluate assertions after each step and report pass/fail status. Supported assertion types in v0: `contains` (substring present), `not_contains` (substring absent), `matches` (regex match), `exit_code` (expected exit code), `equals` (exact string match), `not_equals` (exact string non-match), `json_path` (query structured JSON output by path and compare value).
- **FR-011a**: System MUST halt execution immediately when a step fails (assertion violation or non-zero exit code), persisting the execution trace up to the failure point.
- **FR-012**: System MUST enforce governance policies before executing CLI steps — blocked commands MUST halt execution.
- **FR-013**: System MUST apply redaction patterns to all captured output before persisting it.
- **FR-014**: System MUST block denied environment variable patterns during template resolution.
- **FR-014a**: System MUST support a global default timeout for CLI steps via `meta.defaults.timeout`, and allow individual steps to override the timeout via a per-step `timeout` field. When a CLI step exceeds its timeout, the system MUST terminate the command, mark the step as failed, and halt execution.
- **FR-015**: System MUST persist a StepResult (JSONL) for every executed step.
- **FR-016**: System MUST persist state snapshots (memory dumps) capturing the full RunState at step boundaries.
- **FR-016a**: System MUST support resuming a halted execution from the last completed step by restoring state from the most recent persisted snapshot. The resumed run MUST continue appending to the same trace and run directory.
- **FR-017**: System MUST store execution artifacts under `.runbook/runs/<run_id>/` with subdirectories for trace, snapshots, and attachments. Run IDs MUST use a timestamp-plus-random-suffix format (e.g., `20260211T153042-a7f3`) to ensure chronological sortability and collision resistance.

**Manual Steps & Evidence**:
- **FR-018**: System MUST pause execution at manual steps and prompt for required structured evidence.
- **FR-019**: System MUST support three evidence types: `text`, `checklist`, and `attachment`.
- **FR-020**: System MUST record sha256 hash and file size for attachment evidence.
- **FR-021**: System MUST support approval recording with actor identity (via `--as` flag).
- **FR-022**: System MUST not mark a manual step complete until all required evidence and approvals are provided.

**Debugger**:
- **FR-023**: System MUST provide an interactive CLI debugger with step-by-step execution.
- **FR-024**: System MUST support debugger commands: `next`, `continue`, `dump`, `print vars`, `print captures`, `history`, `evidence set`, `evidence check`, `evidence attach`, `approve --as`, `snapshot`.
- **FR-025**: System MUST display current step information, variable state, and capture state at each debugger prompt.

**Replay**:
- **FR-026**: System MUST route CLI steps through `cli-replay exec` when operating in replay mode.
- **FR-027**: System MUST require a scenario.yaml file for replay mode execution.
- **FR-028**: System MUST produce identical state transitions when given identical replay inputs.
- **FR-029**: System MUST reuse stored evidence for manual steps when replaying (via `replay_mode: reuse_evidence`).

**Compiler**:
- **FR-030**: System MUST accept a Markdown TSG file as input and produce a schema-valid runbook.yaml.
- **FR-031**: System MUST produce a mapping.md explaining how each TSG section maps to runbook steps, including extracted commands, inferred variables, and manual steps.
- **FR-032**: System MUST emit `type: manual` for any TSG section where automation is unsafe or ambiguous.
- **FR-033**: System MUST extract variables from TSG prose and place them in `meta.vars` as placeholders.
- **FR-034**: System MUST never invent credentials or destructive commands during compilation.

**Provider Model**:
- **FR-035**: System MUST support a provider interface for step execution, with each provider implementing `Validate` and `Execute`.
- **FR-036**: Providers MUST NOT mutate global state outside of the returned StepResult.
- **FR-037**: Providers MUST NOT alter execution flow or bypass governance checks.
- **FR-038**: System MUST include `cli` and `manual` providers in v0.

### Key Entities

- **Runbook**: A structured, schema-valid definition of an incident response procedure. Contains metadata (name, description, variables, governance policies) and an ordered list of steps. Identified by `apiVersion` and `meta.name`.

- **Step**: A single unit of work within a runbook. Has a unique `id`, a `type` (cli, manual), type-specific configuration, optional captures, and optional assertions. CLI steps carry an `argv` array. Manual steps carry instructions, evidence requirements, and approval requirements.

- **RunState**: The complete execution state at a point in time. Contains the run identifier (timestamp + short random suffix format, e.g., `20260211T153042-a7f3`), current step index, all resolved variables, all accumulated captures, and the history of step results. Serialized as state snapshots ("memory dumps").

- **StepResult**: The outcome of executing a single step. Contains step identity, step index (0-based position), status (passed/failed/skipped), actor (engine/human), timestamps, evidence collected, captures produced, and any error information. Persisted as JSONL trace entries.

- **Evidence**: Structured proof that a manual step was performed correctly. Comes in three kinds — `text` (free-form input), `checklist` (a set of items to check off), and `attachment` (a file with recorded hash and size).

- **Governance Policy**: Rules that constrain execution for safety. Includes command allowlists/denylists, denied environment variable patterns, output redaction patterns, and evidence requirements. Evaluated before and during step execution.

- **Provider**: An isolated executor responsible for a specific step type. Implements `Validate(step)` and `Execute(step)`. Cannot influence engine behavior beyond returning a StepResult.

- **Scenario**: A replay configuration file that provides pre-recorded CLI responses and evidence for deterministic offline execution.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A runbook author can validate a runbook file and receive clear, actionable error messages for all schema violations within 5 seconds for files up to 500 steps.
- **SC-002**: An on-call engineer can execute a runbook with 3+ CLI steps and 1+ manual steps, producing a complete JSONL trace and state snapshots at every step boundary, in under 5 minutes (excluding wait time for manual evidence).
- **SC-003**: 100% of governance policy violations (disallowed commands, denied env vars, redaction requirements) are detected and enforced — zero violations slip through to execution.
- **SC-004**: Replay mode produces bit-identical execution traces when given the same runbook and scenario inputs across multiple runs.
- **SC-005**: An engineer using the interactive debugger can inspect variables, captures, and history at any step without leaving the debugger session.
- **SC-006**: Compiling a Markdown TSG of up to 50 sections produces a schema-valid runbook.yaml and a mapping.md in under 60 seconds.
- **SC-007**: Manual steps cannot be marked complete until all required evidence (text, checklist items, attachments) and approvals are provided — zero incomplete manual steps in the trace.
- **SC-008**: Dry-run mode produces zero side effects — no commands executed, no files modified outside `.runbook/`.
- **SC-009**: All execution artifacts (traces, snapshots, attachments) are persisted to the correct filesystem locations and are readable for audit after execution completes.
- **SC-010**: A new runbook with a previously unseen governance violation type fails validation with a clear error message — the system never silently accepts unknown constraints.

## Clarifications

### Session 2026-02-11

- Q: When a CLI step fails (assertion violation or non-zero exit code), what should the runtime do? → A: Halt immediately, persist trace up to failure point.
- Q: Should CLI steps have a timeout mechanism? → A: Per-step optional timeout with a global default (`meta.defaults.timeout`), overridable per step via `timeout` field.
- Q: If execution halts due to failure, timeout, or crash, can the engineer resume from where it stopped? → A: Resume from last completed step using persisted snapshot to restore state.
- Q: What assertion types should the schema support in v0? → A: `contains`, `not_contains`, `matches` (regex), `exit_code`, `equals`, `not_equals`, `json_path`.
- Q: How should run IDs be generated? → A: Timestamp prefix + short random suffix (e.g., `20260211T153042-a7f3`), sortable and human-readable.

## Assumptions

- The `cli-replay` tool is an external dependency that is already available and functional. This platform integrates with it but does not implement it.
- v0 targets local single-user execution only — no multi-user concurrency, no networked execution, no cloud storage for artifacts.
- Steps execute sequentially; parallel step execution is explicitly out of scope for v0.
- Identity for approvals is provided by the `--as` CLI flag with no external identity provider integration in v0.
- Attachments are stored locally on disk; no remote storage integration in v0.
- The compiler's LLM integration uses a prompt contract with an external model; prompt engineering and model selection are configuration concerns, not implementation scope.
- The `group` step type is optional in v0 and may be deferred.
- When the `governance` block is absent from a runbook's `meta`, governance enforcement still runs but applies no constraints (no allowlist = all commands permitted, no redaction = no output sanitization). Governance is always *evaluated*, never bypassed — absence means permissive defaults, not disabled enforcement.
- The interactive debugger (`gert debug`) supports `real` and `replay` modes only. Dry-run is excluded because it produces no executable steps to interactively inspect — dry-run is a non-interactive preview available via `gert exec --mode dry-run`.

## Scope Boundaries

### In Scope (v0)
- Runbook schema definition and strict validation
- CLI-based execution in real, replay, and dry-run modes
- Interactive CLI debugger with state inspection
- Manual step evidence collection (text, checklist, attachment)
- JSONL execution traces and state snapshots
- Governance enforcement (command allowlists, env var denylists, output redaction)
- `cli-replay` integration for replay mode
- TSG-to-runbook compiler with mapping report
- `cli` and `manual` step type providers

### Out of Scope (v0)
- Incident management integration (paging, ticketing)
- Automatic remediation without human confirmation
- Parallel or concurrent step execution
- Complex workflow language or Turing-complete DSL
- Cloud provider-specific step types (aws.*, azure.*, k8s.*, ssh.*)
- External identity provider integration
- Remote artifact storage
- VS Code extension UX (v0 is CLI-first; extension is a future surface)
- Fine-tuning or training of compiler models
