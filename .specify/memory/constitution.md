<!--
  Sync Impact Report
  ==================
  Version change: N/A → 1.0.0 (initial ratification)
  Modified principles: N/A (initial)
  Added sections:
    - Core Principles (7 principles)
    - Technology & Architecture Constraints
    - Development Workflow
    - Governance
  Removed sections: N/A
  Templates requiring updates:
    - .specify/templates/plan-template.md ✅ reviewed (no changes needed)
    - .specify/templates/spec-template.md ✅ reviewed (no changes needed)
    - .specify/templates/tasks-template.md ✅ reviewed (no changes needed)
    - .specify/templates/checklist-template.md ✅ reviewed (no changes needed)
    - .specify/templates/agent-file-template.md ✅ reviewed (no changes needed)
  Follow-up TODOs: None
-->

# gert Constitution

## Core Principles

### I. Schema-First Design
All features and capabilities MUST derive from the runbook schema.
The schema is the single source of truth for runbook structure,
step types, governance policies, and validation rules.

- Schema definitions MUST use strict parsing that rejects unknown
  fields.
- Schema changes MUST include migration guidance and version bumps
  to the `apiVersion` field.
- Both Go structs and TypeScript types MUST be generated from or
  validated against the canonical JSON Schema.
- No runtime behavior may depend on data not expressible in the
  schema.

### II. Governance by Default
Every execution path MUST enforce governance policies before
performing any action. Governance is not optional or opt-in.

- Command allowlists and denylists MUST be evaluated before CLI
  step execution.
- Denied environment variables MUST be blocked during template
  resolution.
- Redaction patterns MUST be applied before storing or displaying
  command output.
- Manual steps MUST require structured evidence when configured.
- Approval requirements MUST be satisfied before a manual step
  is marked complete.

### III. Deterministic Execution
The runtime MUST operate as a deterministic state machine.
Given identical inputs and mode, execution MUST produce identical
state transitions and outputs.

- All state transitions MUST be recorded in the execution trace
  (JSONL).
- Replay mode MUST produce results identical to original execution
  when given the same scenario and evidence.
- State snapshots ("memory dumps") MUST capture the full `RunState`
  at each step boundary.
- Variable resolution, capture accumulation, and assertion
  evaluation MUST follow a fixed, documented evaluation order.

### IV. Safe by Default
The system MUST prevent unintended side effects. Human confirmation
is required before any potentially destructive or ambiguous action.

- No automated remediation MUST occur without explicit human
  confirmation.
- The compiler MUST emit `type: manual` for any step where
  automation is unsafe or ambiguous.
- Dry-run mode MUST produce zero side effects.
- Providers MUST NOT execute commands outside the governance
  allowlist.
- Unknown or unrecognized step types MUST halt execution with a
  clear error.

### V. Provider Sovereignty
The execution engine is sovereign over all providers. Providers
are isolated executors that implement a strict interface and MUST
NOT influence engine behavior.

- Providers MUST implement only `Validate(step)` and
  `Execute(step)`.
- Providers MUST NOT mutate global state outside the returned
  `StepResult`.
- Providers MUST NOT alter execution flow (ordering, skipping,
  branching).
- Providers MUST NOT bypass governance checks.
- Provider schema fragments MAY be registered for editor
  autocomplete but MUST NOT override core schema validation.

### VI. Dual-Stack Contract Parity
The Go CLI and TypeScript VS Code extension MUST share schema
definitions, validation logic, and behavioral contracts. Users
MUST experience consistent behavior regardless of interface.

- The runbook JSON Schema MUST be the single canonical contract
  shared between both stacks.
- Validation rules MUST produce identical accept/reject decisions
  in both Go and TypeScript implementations.
- Step execution semantics (state transitions, capture behavior,
  assertion evaluation) MUST be equivalent across stacks.
- Any behavioral divergence between CLI and extension is a bug.
- Shared test fixtures MUST be maintained to verify cross-stack
  parity.

### VII. Test-First Development
Tests MUST be written before implementation code. The red-green-
refactor cycle is mandatory for all functional changes.

- Tests MUST be written and confirmed to fail before the
  corresponding implementation is written.
- Contract tests MUST cover schema validation boundaries
  (valid/invalid runbooks).
- Integration tests MUST cover end-to-end execution flows
  including trace and snapshot output.
- Replay-mode tests MUST verify deterministic reproducibility.
- Cross-stack parity tests MUST validate Go and TypeScript
  produce identical results for shared test fixtures.

## Technology & Architecture Constraints

### Go CLI (`gert`)
- Language: Go (latest stable)
- CLI framework: Cobra
- YAML parsing: `gopkg.in/yaml.v3` with strict mode
- Templating: `text/template`
- Schema validation: strict struct decoding or JSON Schema
  library
- Build: standard `go build` / `go test` toolchain

### TypeScript VS Code Extension
- Language: TypeScript (strict mode enabled)
- Framework: VS Code Extension API
- Package manager: npm or pnpm
- Testing: VS Code extension test framework
- Build: esbuild or webpack for bundling

### Shared Artifacts
- Canonical runbook JSON Schema lives in a single location and
  is consumed by both stacks.
- Shared golden-file test fixtures live in a common directory
  accessible to both Go and TypeScript test suites.
- Filesystem layout for runtime artifacts follows the
  `.runbook/` convention defined in the spec.

### Repository Structure
```
cmd/            # Go CLI entry points (Cobra commands)
pkg/            # Go library packages (schema, runtime, etc.)
vscode/         # TypeScript VS Code extension source
schemas/        # Canonical JSON Schema definitions
testdata/       # Shared golden-file test fixtures
.runbook/       # Runtime artifact directory (gitignored)
```

## Development Workflow

### Change Process
1. Every feature MUST have a specification (`spec.md`) before
   implementation begins.
2. Implementation plans MUST pass a Constitution Check gate
   before Phase 0 research.
3. Schema changes MUST be reviewed and merged before dependent
   runtime or extension changes.
4. All PRs MUST include tests that exercise the changed behavior.

### Code Review Requirements
- All changes MUST be submitted via pull request.
- PRs MUST pass CI (lint, build, test) before merge.
- Reviewers MUST verify constitutional compliance (governance
  enforcement, schema-first design, test-first evidence).
- Schema-affecting changes require review from both Go and
  TypeScript maintainers.

### Quality Gates
- No merge without passing tests in both Go and TypeScript
  where applicable.
- Contract test failures MUST block merge.
- Replay-mode parity failures MUST block merge.
- Linting and formatting MUST pass (`go vet`, `golangci-lint`,
  `eslint`, `prettier`).

## Governance

This constitution is the highest-authority document for the gert
project. It supersedes all other practices, conventions, and
ad-hoc decisions.

### Amendment Procedure
1. Propose amendment via pull request modifying this file.
2. Amendment PR MUST include rationale and impact assessment.
3. If the amendment changes or removes a principle, all dependent
   templates and specs MUST be updated in the same PR or a
   linked follow-up.
4. Version MUST be incremented per semantic versioning:
   - **MAJOR**: Principle removal or backward-incompatible
     redefinition.
   - **MINOR**: New principle or materially expanded guidance.
   - **PATCH**: Clarifications, wording, or typo fixes.

### Compliance Review
- Every implementation plan MUST include a Constitution Check
  section verifying alignment with active principles.
- Quarterly review of constitution relevance is recommended as
  the project matures.

### Versioning Policy
- Version follows MAJOR.MINOR.PATCH semantic versioning.
- The version line at the bottom of this document is the
  authoritative version identifier.

**Version**: 1.0.0 | **Ratified**: 2026-02-11 | **Last Amended**: 2026-02-11
