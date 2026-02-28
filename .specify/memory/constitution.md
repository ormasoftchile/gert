<!--
Sync Impact Report
Version: 0.0.0 → 1.0.0 (MAJOR: initial ratification)
Added principles:
  I. Kernel-First Architecture
  II. Contract-Driven Governance
  III. Test-Driven Quality (NON-NEGOTIABLE)
  IV. Deterministic Execution
  V. Trace Everything
  VI. Simplicity and YAGNI
Added sections:
  Quality Gates (pre-commit, pre-merge, commit messages)
  Development Workflow (build commands, file conventions)
  Governance (amendment process)
Templates requiring updates:
  ✅ plan-template.md — Constitution Check section present
  ✅ spec-template.md — no constitution references needed
  ✅ tasks-template.md — no constitution references needed
Follow-up TODOs: none
-->
# Gert Constitution

## Core Principles

### I. Kernel-First Architecture
Every feature starts as a kernel primitive or composes existing primitives. The kernel (`pkg/kernel/`) is the single source of execution semantics — validation, governance, trace, state. Ecosystem packages (`pkg/ecosystem/`) consume kernel interfaces but never modify kernel behavior. The dependency arrow is always ecosystem → kernel, never reversed.

- Kernel packages MUST be independently importable without ecosystem dependencies
- New capabilities MUST be exposed as interfaces in the kernel, with implementations in ecosystem
- No ecosystem package may be imported by any kernel package
- If a feature requires kernel changes, those changes are Track 1; everything else is Track 2

### II. Contract-Driven Governance
Governance derives from declared behavior (effects, writes, idempotency, determinism), not from identity, command names, or allowlists. Every tool and extension declares a behavioral contract. The kernel evaluates governance rules against resolved contracts — the kernel never interprets command semantics.

- Tools and extensions MUST declare `effects` and `writes` in their contracts
- Governance rules MUST match on contract properties, not on tool names
- Derived risk is informational only — enforcement is policy-driven
- The `side_effects` boolean is deprecated; `effects: []` is the canonical field

### III. Test-Driven Quality (NON-NEGOTIABLE)
No task is considered finished without a supporting unit test or regression test. Every change to kernel packages MUST include tests that verify the new behavior. Every bug fix MUST include a test that reproduces the bug before the fix.

- **Red-Green-Refactor**: Write failing test → implement → pass → refactor
- Every kernel package change MUST include corresponding test changes
- Every new validation rule MUST have a testdata fixture exercising it
- Every new trace event MUST have a test verifying its emission
- Every new CLI command MUST have a test verifying its output
- Scenario replay tests MUST cover every runbook in the repository
- The test suite MUST pass before any commit lands on the feature branch
- No PR may reduce test coverage of modified packages

### IV. Deterministic Execution
Same inputs produce same outputs. Runbook execution is deterministic given the same variable state, tool responses, and governance policy. This is the foundation for replay, scenario testing, and audit trust.

- Parallel branches merge in declaration order, not completion order
- Variable namespaces (global, step, scope) have defined merge rules
- Input resolution order is kernel-defined and host-independent
- Trace events are ordered deterministically, including for parallel execution
- `for_each` output order matches declaration order, not execution order

### V. Trace Everything
Every decision the kernel makes is recorded in the append-only JSONL trace. No implicit behavior — if the kernel did it, the trace shows it.

- Every step execution emits `step_start` and `step_complete`
- Every governance decision emits `governance_decision`
- Every contract resolution emits `contract_evaluated`
- Every approval emits `approval_submitted` and `approval_resolved`
- Every scope export emits `scope_export`
- Every visibility constraint emits `visibility_applied`
- Hash chaining (`prev_hash`) on every event for tamper evidence
- Secret values MUST never appear in traces; secret names are recorded

### VI. Simplicity and YAGNI
Start simple. Don't add features, abstractions, or configuration for hypothetical future requirements. The right amount of complexity is the minimum needed for the current task.

- Don't add error handling for scenarios that can't happen
- Don't create abstractions for one-time operations
- Don't design for hypothetical future requirements
- Prefer flat structures over nested hierarchies
- Prefer explicit code over clever indirection
- If a feature isn't in the current spec, don't build it

## Quality Gates

### Pre-Commit
- `go build ./pkg/kernel/...` MUST succeed
- `go test ./pkg/kernel/... -count=1` MUST pass (all existing + new tests)
- `go vet ./pkg/kernel/...` MUST report no issues
- No `NEEDS CLARIFICATION` markers in committed code

### Pre-Merge
- All scenario replay tests (`gert test`) MUST pass for affected runbooks
- Test coverage of modified kernel packages MUST not decrease
- New kernel interfaces MUST have at least one integration test exercising the full path

### Commit Messages
- Format: `type: short description` (e.g., `feat:`, `fix:`, `docs:`, `test:`, `refactor:`)
- Body explains what changed and why, not how
- Breaking changes get `BREAKING:` prefix

## Development Workflow

### Build and Test Commands
```bash
# Build all kernel packages
go build ./pkg/kernel/...

# Run all kernel tests
go test ./pkg/kernel/... -v -count=1

# Build kernel CLI
go build ./cmd/gert-kernel/

# Run scenario tests
./gert-kernel test examples/service-health-check.yaml

# Validate a runbook
./gert-kernel validate examples/service-health-check.yaml
```

### File Conventions
- Kernel source: `pkg/kernel/<package>/<file>.go`
- Kernel tests: `pkg/kernel/<package>/<file>_test.go` (same directory)
- Test fixtures: `pkg/kernel/<package>/testdata/<fixture>.yaml`
- Tool definitions: `tools/<name>.tool.yaml` or `examples/tools/<name>.tool.yaml`
- Runbooks: `examples/<name>.yaml` or `runbooks/<name>.yaml`
- Scenarios: `scenarios/<runbook-name>/<scenario-name>/scenario.yaml`

## Governance

This constitution supersedes all ad-hoc practices. Amendments require:
1. Written justification documenting what changes and why
2. Review confirming no existing invariants are broken
3. Version bump following semver (MAJOR: principle removal/redefinition, MINOR: new principle/section, PATCH: wording/clarification)

All implementation tasks MUST verify compliance with these principles. Complexity beyond what's specified MUST be justified in a Complexity Tracking table.

**Version**: 1.0.0 | **Ratified**: 2026-02-28 | **Last Amended**: 2026-02-28
