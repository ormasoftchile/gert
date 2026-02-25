# Provider Interface Contract v0

**Branch**: `001-runbook-engine-v0` | **Date**: 2026-02-11

---

## Provider Interface

Every step type in the runbook is handled by a **Provider** — an isolated executor implementing a strict two-method interface.

### Interface Definition

```
Provider
├── Validate(step Step) → ValidationResult
└── Execute(ctx ExecutionContext, step Step) → StepResult
```

### `Validate(step Step) → ValidationResult`

Called during schema validation (before execution). Validates step-type-specific fields.

**Input**: The Step definition from the runbook.

**Output**: `ValidationResult`
| Field | Type | Description |
|-------|------|-------------|
| valid | boolean | Whether the step definition is valid for this provider |
| errors | string[] | Validation error messages (empty if valid) |
| warnings | string[] | Non-blocking warnings (e.g., governance advisories) |

**Invariants**:
- MUST NOT perform any side effects
- MUST NOT access network or filesystem (beyond reading the step definition)
- MUST complete in <100ms

---

### `Execute(ctx ExecutionContext, step Step) → StepResult`

Called during runbook execution. Executes the step and returns the result.

**Input**:
- `ctx`: Execution context providing variables, captures, governance policies, and the command executor
- `step`: The Step definition from the runbook

**Output**: `StepResult` (see data-model.md)

**Invariants** (Constitution Principle V — Provider Sovereignty):
- MUST NOT mutate global state outside the returned `StepResult`
- MUST NOT alter execution flow (no skipping, branching, or re-ordering other steps)
- MUST NOT bypass governance checks (enforcement happens in the engine before dispatch)
- MUST return a `StepResult` for every invocation (even on internal errors)
- MUST respect context cancellation (timeout, user interrupt)

---

## ExecutionContext

Provided to the provider's `Execute` method by the engine.

| Field | Type | Description |
|-------|------|-------------|
| run_id | string | Current run identifier |
| mode | enum: `real`, `replay`, `dry-run` | Execution mode |
| vars | map[string]string | Resolved variables (after template expansion) |
| captures | map[string]string | Accumulated captures from prior steps |
| command_executor | CommandExecutor | Interface for running CLI commands (real or replay) |
| evidence_collector | EvidenceCollector | Interface for prompting/collecting evidence |
| governance | GovernancePolicy | Active governance policies |

---

## CommandExecutor Interface

Injected into the execution context. Abstracts real vs. replay command execution.

```
CommandExecutor
└── Execute(ctx, command string, args []string, env []string) → CommandResult
```

**CommandResult**:
| Field | Type | Description |
|-------|------|-------------|
| stdout | bytes | Standard output |
| stderr | bytes | Standard error |
| exit_code | integer | Process exit code |
| duration | duration | Execution time |

**Implementations**:
- `RealExecutor`: Runs commands via OS process execution (`os/exec`)
- `ReplayExecutor`: Matches commands against scenario file, returns pre-recorded responses

---

## EvidenceCollector Interface

Injected into the execution context. Abstracts interactive vs. pre-recorded evidence collection.

```
EvidenceCollector
├── PromptText(name string, instructions string) → string
├── PromptChecklist(name string, items []string) → map[string]boolean
├── PromptAttachment(name string, instructions string) → AttachmentInfo
└── PromptApproval(roles []string, min int) → Approval[]
```

**Implementations**:
- `InteractiveCollector`: Prompts the user via CLI (used in real + debug modes)
- `ScenarioCollector`: Returns pre-recorded evidence from scenario file (used in replay mode)
- `DryRunCollector`: Returns placeholder values without prompting (used in dry-run mode)

---

## v0 Providers

### `cli` Provider

**Validates**:
- `with.argv` is present and non-empty
- `with.argv[0]` (command) exists

**Executes**:
1. Resolve template expressions in `argv` elements using `ctx.vars`
2. Delegate to `ctx.command_executor.Execute()` with resolved argv
3. Apply redaction patterns from `ctx.governance` to stdout/stderr
4. Extract captures per step's `capture` configuration
5. Evaluate assertions against captured output
6. Return `StepResult` with status, captures, assertion results

### `manual` Provider

**Validates**:
- `instructions` is present and non-empty
- `required_evidence[].kind` values are recognized
- `required_evidence[].name` values are unique within the step

**Executes**:
1. Display step title and instructions
2. For each evidence requirement, delegate to `ctx.evidence_collector`
3. If approvals required, delegate to `ctx.evidence_collector.PromptApproval()`
4. Verify all evidence and approvals are satisfied
5. Return `StepResult` with status and collected evidence
