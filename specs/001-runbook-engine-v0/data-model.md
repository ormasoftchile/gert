# Data Model: Governed Executable Runbook Engine v0

**Branch**: `001-runbook-engine-v0` | **Date**: 2026-02-11 | **Plan**: [plan.md](plan.md)

---

## Entity Relationship Overview

```
Runbook (1) ──────── (*) Step
   │                    │
   │                    ├── CLIStepConfig
   │                    ├── ManualStepConfig
   │                    │      └── (*) EvidenceRequirement
   │                    ├── (*) Assertion
   │                    └── (*) Capture
   │
   ├── Meta
   │    ├── Vars (map)
   │    ├── Defaults
   │    └── GovernancePolicy
   │         ├── (*) RedactionRule
   │         └── EvidencePolicy
   │
   └── executed by ──→ RunState (1)
                          ├── (*) StepResult
                          │      └── Evidence
                          └── (*) Snapshot
```

---

## Entities

### Runbook

The top-level document defining an incident response procedure.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| apiVersion | string | yes | Schema version identifier (e.g., `runbook/v0`) |
| meta | Meta | yes | Runbook metadata, variables, and governance policies |
| steps | Step[] | yes | Ordered list of steps to execute (minimum 1) |

**Validation rules**:
- `apiVersion` MUST be a recognized version string
- `steps` MUST contain at least one entry
- Unknown fields at any level MUST be rejected

---

### Meta

Runbook-level metadata and configuration.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | yes | Unique runbook identifier |
| description | string | no | Human-readable description |
| vars | map[string]string | no | User-defined variables for template resolution |
| defaults | Defaults | no | Default settings for step execution |
| governance | GovernancePolicy | no | Safety and compliance policies |

---

### Defaults

Default execution settings applied to all steps unless overridden.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| timeout | duration (string) | no | Global default timeout for CLI steps (e.g., `"300s"`, `"5m"`) |

---

### GovernancePolicy

Safety rules evaluated before and during execution.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| allowed_commands | string[] | no | Commands permitted for CLI steps (allowlist) |
| denied_commands | string[] | no | Commands explicitly blocked (denylist) |
| deny_env_vars | string[] | no | Glob patterns for environment variables blocked during template resolution |
| redact | RedactionRule[] | no | Patterns applied to captured output before persistence |
| evidence | EvidencePolicy | no | Global evidence requirements |

**Validation rules**:
- `allowed_commands` and `denied_commands` MUST NOT both contain the same command
- `deny_env_vars` patterns MUST be valid glob expressions
- `redact[].pattern` MUST be a valid regular expression

---

### RedactionRule

A pattern-replacement pair for sanitizing captured output.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| pattern | string (regex) | yes | Regular expression to match sensitive content |
| replace | string | yes | Replacement text (e.g., `"password: <redacted>"`) |

---

### EvidencePolicy

Global settings for evidence collection.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| require_for_manual | boolean | no | Whether all manual steps require evidence (default: false) |
| store_full_stdout | boolean | no | Whether to store full stdout vs. hash+preview (default: false) |

---

### Step

A single unit of work in a runbook. Dispatched to a Provider based on `type`.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| id | string | yes | Unique identifier within the runbook |
| type | enum: `cli`, `manual` | yes | Step type determining which provider executes it |
| title | string | no | Human-readable step title |
| with | CLIStepConfig | conditional | Configuration for CLI steps (required when type=cli) |
| instructions | string | conditional | Prose instructions for manual steps (required when type=manual) |
| required_evidence | EvidenceRequirement[] | no | Evidence the operator must provide for manual steps |
| approvals | ApprovalRequirement | no | Approval gates for manual steps |
| capture | map[string]string | no | Named captures from step output (e.g., `pods: stdout`) |
| assertions | Assertion[] | no | Post-execution checks on captured output |
| timeout | duration (string) | no | Per-step timeout override (CLI steps only) |
| replay_mode | enum: `reuse_evidence` | no | Behavior in replay mode for manual steps |

**Validation rules**:
- `id` MUST be unique across all steps in the runbook
- When `type=cli`, `with` MUST be present with a non-empty `argv`
- When `type=manual`, `instructions` MUST be present
- `capture` keys MUST be valid identifiers (used in template expressions)
- `timeout` applies only to `type=cli` steps

---

### CLIStepConfig

Configuration for a CLI step's command execution.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| argv | string[] | yes | Command and arguments to execute (minimum 1 element) |

**Validation rules**:
- `argv[0]` (the command) MUST be checked against `governance.allowed_commands` / `governance.denied_commands`
- `argv` elements MAY contain template expressions (`{{ .varName }}`)

---

### EvidenceRequirement

A single evidence item required for a manual step.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| kind | enum: `text`, `checklist`, `attachment` | yes | Type of evidence to collect |
| name | string | yes | Unique name for this evidence item within the step |
| items | string[] | conditional | Checklist items (required when kind=checklist) |

**Validation rules**:
- When `kind=checklist`, `items` MUST be present with at least one entry
- `name` MUST be unique within the step's `required_evidence` list

---

### ApprovalRequirement

Approval gates for manual steps.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| min | integer | no | Minimum number of approvals required (default: 0) |
| roles | string[] | no | Roles authorized to approve |

---

### Assertion

A post-execution check evaluated against captured output.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| contains | string | conditional | Assert output contains this substring |
| not_contains | string | conditional | Assert output does NOT contain this substring |
| matches | string (regex) | conditional | Assert output matches this regular expression |
| exit_code | integer | conditional | Assert the command exited with this code |
| equals | string | conditional | Assert output exactly equals this string |
| not_equals | string | conditional | Assert output does NOT exactly equal this string |
| json_path | JSONPathAssertion | conditional | Assert a value at a JSON path in the output |

**Validation rules**:
- Exactly one assertion field MUST be present per Assertion object
- `matches` MUST be a valid regular expression
- `json_path` requires structured JSON output in the captured data

---

### JSONPathAssertion

A structured query into JSON output.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| path | string | yes | JSON path expression (e.g., `$.status.phase`) |
| equals | string | yes | Expected value at the path |

---

## Runtime Entities (Execution State)

### RunState

The complete execution state at a point in time ("memory dump").

| Field | Type | Description |
|-------|------|-------------|
| run_id | string | Unique run identifier (format: `20260211T153042-a7f3`) |
| runbook_path | string | Path to the runbook YAML file being executed |
| mode | enum: `real`, `replay`, `dry-run` | Execution mode |
| started_at | datetime (ISO 8601) | When the run began |
| actor | string | Identity of the person executing (from `--as` flag) |
| current_step_index | integer | Index of the currently executing step (0-based) |
| vars | map[string]string | Current variable values (initial + runtime updates) |
| captures | map[string]string | Accumulated captured values from completed steps |
| history | StepResult[] | Results of all completed steps |

**Serialization**: JSON, persisted as `.runbook/runs/<run_id>/snapshots/step-NNNN.json`

---

### StepResult

The outcome of executing a single step. Uniform envelope for all step types.

| Field | Type | Description |
|-------|------|-------------|
| run_id | string | Parent run identifier |
| step_id | string | Step identifier from the runbook |
| step_index | integer | Step position (0-based) |
| status | enum: `passed`, `failed`, `skipped` | Outcome of the step |
| actor | enum: `engine`, `human` | Who/what executed the step |
| started_at | datetime (ISO 8601) | When step execution began |
| ended_at | datetime (ISO 8601) | When step execution completed |
| evidence | map[string]EvidenceValue | Evidence collected (manual steps) |
| captures | map[string]string | Captured values (hash or preview, per governance) |
| assertions | AssertionResult[] | Results of each assertion evaluation |
| error | string or null | Error message if step failed |

**Serialization**: JSONL, appended to `.runbook/runs/<run_id>/trace.jsonl`

---

### EvidenceValue

A single piece of evidence collected during execution.

| Field | Type | Description |
|-------|------|-------------|
| kind | enum: `text`, `checklist`, `attachment` | Evidence type |
| value | string | Text content or checklist state |
| items | map[string]boolean | Checklist item completion state (kind=checklist only) |
| path | string | File path for attachments |
| sha256 | string | SHA256 hash of attachment file |
| size | integer | File size in bytes for attachments |

---

### AssertionResult

The outcome of evaluating a single assertion.

| Field | Type | Description |
|-------|------|-------------|
| type | string | Assertion type (contains, not_contains, matches, etc.) |
| expected | string | Expected value from the assertion definition |
| actual | string | Actual value from the captured output (truncated for display) |
| passed | boolean | Whether the assertion passed |
| message | string | Human-readable explanation of the result |

---

### Scenario

Replay configuration file providing pre-recorded responses.

| Field | Type | Description |
|-------|------|-------------|
| commands | ScenarioCommand[] | Pre-recorded CLI command responses |
| evidence | map[string]EvidenceValue | Pre-recorded evidence for manual steps (keyed by step_id) |

---

### ScenarioCommand

A single pre-recorded command response for replay mode.

| Field | Type | Description |
|-------|------|-------------|
| argv | string[] | Command and arguments to match |
| stdout | string | Pre-recorded stdout response |
| stderr | string | Pre-recorded stderr response |
| exit_code | integer | Pre-recorded exit code |

---

## State Transitions

### Step Execution State Machine

```
              ┌─────────────┐
              │   PENDING    │
              └──────┬───────┘
                     │ execute()
              ┌──────▼───────┐
              │  EXECUTING   │
              └──────┬───────┘
                     │
         ┌───────────┼───────────┐
         │           │           │
   ┌─────▼──┐ ┌─────▼──┐ ┌─────▼──┐
   │ PASSED  │ │ FAILED │ │SKIPPED │
   └────┬────┘ └────┬───┘ └────┬───┘
        │           │          │
        ▼           ▼          ▼
   next step   HALT run   next step
```

### Run Lifecycle

```
NOT_STARTED → RUNNING → COMPLETED
                │
                ├──→ FAILED (step failure/timeout)
                │        │
                │        └──→ RESUMED → RUNNING
                │
                └──→ CRASHED (process exit)
                         │
                         └──→ RESUMED → RUNNING
```

---

## Filesystem Layout

```
.runbook/
└── runs/
    └── 20260211T153042-a7f3/         # run_id
        ├── trace.jsonl                # append-only step results
        ├── snapshots/
        │   ├── step-0000.json         # RunState after step 0
        │   ├── step-0001.json         # RunState after step 1
        │   └── ...
        └── attachments/
            ├── <sha256-prefix>.bin    # attachment evidence files
            └── ...
```
