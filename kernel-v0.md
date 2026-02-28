# Gert Kernel v0 — Design Document

> **gert executes structured, governed, replayable runbooks with safe extensibility.**

**Date**: 2026-02-27
**Status**: Draft
**apiVersion**: `kernel/v0`

---

## 1. Vision

Make gert a lean execution kernel for runbooks that are:

- **Auditable** — every decision, branch, and outcome is traced
- **Governed** — contracts drive policy, not hardcoded rules
- **Extensible** — escape hatches with declared behavior, extension metadata everywhere
- **Deterministic** — same inputs produce same outputs, including across parallel branches
- **Parallel-safe** — concurrent execution with contract-based conflict detection

Everything else is outside the kernel.

---

## 2. Kernel Boundary

### The kernel IS responsible for

| Responsibility | Description |
|----------------|-------------|
| **Executing steps** | Deterministic step dispatch based on type and contract |
| **Enforcing governance** | Contract-driven policy evaluation (allow / require-approval / deny) |
| **Maintaining state** | Variable scoping, state snapshots, isolated parallel branches |
| **Producing traces** | Append-only JSONL audit trail with rich event types |
| **Validating structure** | 3-phase pipeline: structural → semantic → domain |

### The kernel deliberately does NOT do

| Concern | Where it lives |
|---------|---------------|
| Scheduling | Host platform |
| RBAC | Host platform |
| Distributed orchestration | Host platform |
| Secrets storage | Host platform |
| UI frameworks | Ecosystem (TUI, VS Code extension, web) |
| Interactive debugging | Ecosystem (wraps kernel API) |
| JSON-RPC server | Ecosystem (wraps kernel API) |
| TSG compilation | Ecosystem (separate tool) |
| Diagram generation | Ecosystem (reads schema) |

---

## 3. Step Types

Seven primitive step types. Small, stable core. Infinite extensibility without core changes.

| Type | Purpose | Contract Source |
|------|---------|----------------|
| **`tool`** | Execute a tool action | From tool definition file |
| **`manual`** | Human evidence collection + approvals | Implicit (kernel-known), author can tighten |
| **`assert`** | First-class assertion evaluation | Implicit: `side_effects=false`, `deterministic=true`, `idempotent=true` |
| **`branch`** | Conditional flow fork — multiple paths, one executes | None (structural) |
| **`parallel`** | Concurrent execution with state isolation | None (structural) |
| **`end`** | Explicit structured outcome declaration | None (structural) |
| **`extension`** | Escape hatch for custom behavior | Declared inline on the step |

### Step type rationale

- **`tool`** absorbs what was `cli`. A shell command is just a tool with `transport: stdio`. No privileged `os/exec` step type.
- **`manual`** remains because human actions are fundamentally different — they collect evidence, not output.
- **`assert`** becomes first-class. Assertions aren't post-hoc checks on other steps; they're explicit evaluation points that can drive branching and outcomes.
  - **Assert semantics:** An assert step evaluates its expressions and produces a boolean output `{{ .<step_id>.passed }}` (true/false). A *false* result sets step status to `failed`. By default, a failed assert **halts execution** (same as any failed step — see §9.5). To use an assert as a non-fatal probe that feeds into a downstream `branch`, guard the assert with `continue_on_fail: true`, which records the failure but allows execution to proceed. The `branch` step can then inspect `{{ .evaluate_health.passed }}`.
- **`branch`** makes conditional flow visible in the graph structure, not hidden in step fields.
- **`parallel`** makes concurrency explicit and enables contract-based safety analysis.
- **`end`** makes outcomes explicit and visible. Every terminal state is a step you can see.
- **`extension`** is the escape hatch. Unknown behavior with a declared contract. Enables ecosystem growth without kernel changes.

---

## 4. Contract Model

The foundational primitive. Everything — governance, parallelism, extensions, replay — depends on contracts.

### Contract shape

```yaml
contract:
  inputs:
    hostname: { type: string, required: true }
    timeout:  { type: int, default: 30 }
  outputs:
    status_code: { type: int }
    body:        { type: string }
  side_effects: true        # does it change external state?
  deterministic: false      # same inputs → same outputs?
  idempotent: true          # safe to re-run?
  reads:  [network]         # resources read (for parallel safety)
  writes: [service]         # resources mutated (for parallel safety)
```

### Contract source by step type

| Step type | Contract source | Rationale |
|-----------|----------------|-----------|
| **tool** | From tool definition file, step can tighten | Tools are reusable; contract lives with the definition |
| **extension** | Declared inline on the step | Unknown behavior — author must declare it |
| **manual** | Implicit defaults, author can tighten | Defaults: `side_effects=true`, `deterministic=false`, `idempotent=false`, `reads=[]`, `writes=[]` |
| **assert** | Implicit, fixed | Always: `side_effects=false`, `deterministic=true`, `idempotent=true` |
| **branch / parallel / end** | None | Structural steps don't execute — they direct flow |

### Contract inheritance rules

1. **One-way tightening only.** A step referencing a tool can add restrictions (add reads/writes, change `deterministic: true` → `false`), but can never relax the tool's contract.
2. **Manual tightening.** Authors can declare `side_effects: false` on a manual step like "verify this dashboard" to enable its use in parallel blocks.
3. **Tool-level contract is the default.** Per-action contracts in tool definitions can tighten the tool-level contract.

**Tightening rules for each property:**

| Property | Tighten means | Relax (rejected) |
|----------|--------------|------------------|
| `side_effects` | `false` → `true` | `true` → `false` |
| `deterministic` | `true` → `false` | `false` → `true` |
| `idempotent` | `true` → `false` | `false` → `true` |
| `reads` | Add tags (superset) | Remove tags (subset) |
| `writes` | Add tags (superset) | Remove tags (subset) |

For `reads`/`writes`, tightening means declaring **more** resource dependencies (the step claims it touches more than the tool-level contract says). Removing a tag would claim the step is safer than the tool — that's relaxation and is rejected at validation.

### Resource tags

- **Convention, not enforcement.** Resource tag strings (`network`, `service`, `database`, `filesystem`) are opaque to the kernel.
- The kernel performs **set intersection math** on reads/writes for parallel safety. It never interprets what a tag means.
- Ecosystem conventions suggest common names. The kernel doesn't enforce a vocabulary.

---

## 5. Structured Outcomes

Runbooks must end with a structured outcome, carried by `end` steps.

### Outcome shape

```yaml
- type: end
  outcome:
    category: resolved        # fixed enum for policy & metrics
    code: dns_fixed           # domain-specific meaning
    meta:                     # optional extensible details
      ttd: 4m
      root_cause: stale_cache
```

### Outcome categories (fixed enum)

| Category | Meaning |
|----------|---------|
| `resolved` | Problem fixed |
| `escalated` | Handed off to another team/process |
| `no_action` | No intervention needed |
| `needs_rca` | Mitigated but root cause unknown |

### Rules

- **Multiple `end` steps per runbook.** Different paths lead to different outcomes (resolved via one branch, escalated via another).
- **Exactly one `end` executes per run.** Execution stops immediately when an `end` is reached.
- **Every reachable path must lead to an `end`.** Validated statically at validation time. If execution completes without hitting an `end`, it's a runtime error.

### Purpose

- Analytics — outcome categories enable dashboards and trends
- Policy decisions — governance rules keyed on outcome categories
- Incident timelines — structured record of what happened and how it ended
- Future intelligence — `code` and `meta` enable domain-specific learning

---

## 6. Flow Control

Four mechanisms, each solving a different problem.

### 6.1 `when` — Step-level guard

"Should I execute this step?" Run or skip, flow continues either way.

```yaml
- id: restart
  type: tool
  tool: restart-service
  when: "{{ eq .status \"unhealthy\" }}"
```

Not a flow-control structure — a filter on an individual step.

### 6.2 `branch` — Flow-level fork

"Which path does the runbook take?" Multiple paths, exactly one executes.

```yaml
- type: branch
  branches:
    - condition: "{{ eq .severity \"critical\" }}"
      label: critical_path
      steps:
        - ...
    - condition: default
      label: standard_path
      steps:
        - ...
```

Validation: the kernel can verify that branch conditions are exhaustive (at least one always matches, or a `default` exists).

### 6.3 `next` — Constrained goto

Non-linear flow without arbitrary jumps.

```yaml
- id: apply_fix
  type: tool
  tool: restart-service
  next: verify_fix              # forward jump — always allowed

- id: verify_fix
  type: assert
  ...
  next: { step: diagnose, max: 3 }  # backward jump — bounded
```

| Rule | Purpose |
|------|---------|
| Forward jumps always allowed | Skip ahead safely |
| Backward jumps require a `max` bound | Guarantees termination |
| Backward without `max` rejected at validation | No unbounded loops |
| `next` on a branch path applies to that branch | Each arm can have its own target |
| **`next` targets must be scope-local** | Cannot jump across branch arms or into/out of parallel blocks |

**Scoping rule:** A `next` target must be reachable within the same scope — the same top-level step list, or the same branch arm. Jumping from one branch arm to a step in a sibling arm is rejected at validation time. This preserves the invariant that exactly one branch arm executes per `branch` step. To share logic across branches, extract it to a step after the `branch` block and use a forward `next` to reach it.

**What this replaces:** the v0/v1 `IterateBlock` (max + until). Convergence loops become a backward `next` with `max` and a `when` guard on the target step.

**Implicit loop variable:** When a step is the target of a backward `next`, the kernel provides `{{ .<step_id>.retry_count }}` — the number of times execution has jumped back to this step (starts at 0). This replaces the need for a separate iteration counter.

### 6.4 `for_each` — List iteration modifier

Iterate over a list, on any step or block. Not a step type — a modifier like `when`.

```yaml
- id: check_node
  type: tool
  tool: health-check
  for_each:
    as: node
    over: "{{ .nodes }}"
    parallel: true        # optional — expand as parallel branches
```

- **Default:** sequential iteration.
- **`parallel: true`:** the kernel expands into a `parallel` block — same conflict detection rules apply (contract reads/writes checked per iteration).
- **Scoping:** `{{ .node }}` (the `as` variable) is in scope within each iteration.

### `for_each` output accumulation

When a step with `for_each` produces outputs, the kernel collects them into an **ordered list** keyed by the step ID.

| Mode | Result shape | Example reference |
|------|-------------|------------------|
| Sequential | `{{ .check_node }}` → `[{status_code: 200}, {status_code: 503}, ...]` | `{{ index .check_node 0 "status_code" }}` |
| Parallel | Same — declaration order, not completion order | Same |

- The singular iteration variable (`{{ .node }}`) is **not available** after the loop.
- Downstream steps reference the accumulated list via the step ID.
- Static analysis verifies that post-loop references use the list form, not the scalar form.

### `for_each` with keyed outputs

When iterations need to be referenced by a meaningful key (not just index), use the `key` field:

```yaml
- id: agent_responses
  type: extension
  extension: llm-agent
  for_each:
    as: agent
    over: "{{ .agents }}"
    key: "{{ .agent.id }}"          # key each output by agent ID
    parallel: true
  inputs:
    prompt: "{{ .question }}"
```

- With `key`: outputs are stored as a **map** under the step ID: `{{ .agent_responses.skeptic.answer }}`
- Without `key`: outputs are stored as a **list** (existing behavior): `{{ index .agent_responses 0 "answer" }}`
- Key collisions are a **runtime error** — each key must be unique across iterations
- The engine guarantees **deterministic key ordering** in trace and replay (insertion order = declaration order)

### `for_each` + `parallel: true` + contract interaction

When the kernel expands `for_each parallel: true`, each iteration becomes a parallel branch with the **same contract** (inherited from the step's tool). Since every branch has identical `reads`/`writes`, they will always conflict under the standard set-intersection rule.

**Resolution:** The kernel treats `for_each` parallel iterations as **logically independent by default** — each iteration operates on a different item, so the kernel skips conflict detection between iterations of the same `for_each`. This is safe because:
- Each iteration receives only its own `as` variable.
- Cross-iteration data sharing is impossible (no shared write target within the loop).

If the author knows iterations actually contend on a shared resource, they should use sequential mode (`parallel: false` or omit the flag).

### 6.5 `repeat` — Bounded iteration block

Execute a block of steps multiple times until a condition is met or max iterations reached.

```yaml
- id: debate
  type: repeat
  repeat:
    max: 3
    until: '{{ eq .consensus "reached" }}'
  steps:
    - id: argue
      type: extension
      extension: llm-agent
      scope: "round/{{ .repeat.index }}"
      # ...
    - id: evaluate
      type: extension
      extension: judge
      export: ["consensus"]
```

| Field | Required | Description |
|-------|----------|-------------|
| `repeat.max` | yes | Maximum iterations (guarantees termination) |
| `repeat.until` | no | Expression evaluated after each iteration; true = stop |
| `steps` | yes | Steps to execute per iteration |

**Semantics:**

- `{{ .repeat.index }}` — zero-based iteration counter, available inside the block
- `{{ .repeat.round }}` — alias for index (readability for debate patterns)
- The `until` expression is evaluated after each iteration with the current global state
- If `until` is omitted, the block runs exactly `max` times
- Each iteration is a fresh scope: `scope.repeat/<step_id>/<index>`
- Steps inside the block can `export` variables to make them visible to subsequent iterations and the `until` expression
- Trace records `repeat_start` (with max, step_id) and `repeat_iteration` (with index) events deterministically — replay is stable

**What this replaces:** `repeat` is a structured alternative to backward `next` with `max` for multi-step loops. Use `next` for single-step retry; use `repeat` for multi-step iteration patterns (debate rounds, convergence loops).

---

## 7. Variable & State Model

Unified around contract outputs. No separate "capture" concept.

### Variable sources

| Source | When available | Declared where |
|--------|---------------|---------------|
| **Runbook inputs** | From the start | `meta.inputs` in the runbook |
| **Constants** | From the start (immutable) | `meta.constants` in the runbook |
| **Step outputs** | After the producing step completes | `contract.outputs` in the step's contract |

### Constants

Values baked into the runbook that do not vary per-execution.

```yaml
meta:
  constants:
    health_endpoint: "/healthz"
    max_retries: 3
    target_config:                  # object-valued constant
      port: 443
      protocol: https
      timeout: 30
```

**Rules:**

- Constants are available from the start of execution, like inputs.
- Constants are **immutable** — a step output that shadows a constant name is a **validation error**.
- Constants can hold **scalars or objects**. Object constants enable the `inputs_from` pattern (see below).
- Constants participate in static variable resolution: `{{ .health_endpoint }}`, `{{ .target_config.port }}`.
- Constants are recorded in the trace (`run_start` event) for auditability.

**When to use constants vs. inputs:**

| Use case | Mechanism |
|----------|----------|
| Value the caller provides or overrides | `meta.inputs` |
| Value the author fixes for this runbook | `meta.constants` |
| Shared resource descriptor (endpoint, config block) | `meta.constants` with an object value |

### `inputs_from` — Input spreading

When multiple steps need the same set of inputs, repeating them is verbose and error-prone. `inputs_from` pulls keys from a named object (constant or step output) into a step's inputs.

```yaml
meta:
  constants:
    service_target:
      hostname: "{{ .hostname }}"
      port: 443
      environment: production

steps:
  - id: check
    type: tool
    tool: health-check
    inputs_from: service_target         # spreads hostname, port, environment
    inputs:
      timeout: 30                       # additional input, not in the group

  - id: restart
    type: tool
    tool: restart-service
    inputs_from: service_target         # same three inputs, declared once
    inputs:
      force: true
```

**Rules:**

- `inputs_from` references a named constant or a prior step output that is an object.
- Keys from `inputs_from` are merged into `inputs`. Explicit `inputs` entries **win** over `inputs_from` keys (step-level override).
- Multiple `inputs_from` sources: if needed, `inputs_from` accepts a list. Keys are merged left-to-right; later sources override earlier ones. Explicit `inputs` still wins over all.
  ```yaml
  inputs_from: [service_target, auth_config]
  inputs:
    timeout: 60     # overrides anything from either source
  ```
- Static analysis validates that every key spread by `inputs_from` matches a declared contract input on the step's tool.
- `inputs_from` is syntactic sugar — the kernel resolves it to a flat `inputs` map before execution. Trace events record the resolved inputs, not the `inputs_from` reference.

### Rules

- **Static analysis.** At validation time, the kernel walks the step graph, accumulates declared outputs and constants, and verifies that every variable reference (`{{ .name }}`) resolves to a declared input, constant, or a prior step's output.
- **No runtime surprises.** If a variable doesn't resolve statically, the runbook is invalid.
- **Constant immutability.** A step output name that collides with a constant name is a validation error.
- **Parallel merge.** Two parallel branches that both declare an output with the same name → **validation error** (see §8).
- **Extraction plumbing lives in tool definitions**, not in the kernel. A tool's contract says "I produce `status_code: int`." The tool definition's `extract` block describes how to map stdout to that output. The kernel sees only the contract.
- **Parallel merge.** Two parallel branches that both declare an output with the same name → **validation error**. The kernel rejects ambiguous merges statically. Authors must use distinct output names or restructure branches. No silent precedence rules.

### Variable Namespaces

Variables live in three namespaces:

| Namespace | Syntax | Lifetime | Use case |
|-----------|--------|----------|----------|
| **global** | `{{ .name }}` | Entire run | Runbook inputs, constants, promoted outputs |
| **step** | `{{ .step.<step_id>.name }}` | After producing step completes | Step outputs (already supported via step ID) |
| **scope** | `{{ .scope.<name>.var }}` | Within the scope block | Round-based iteration, debate phases, isolated contexts |

#### Scope blocks

A step may declare a `scope` — an explicit namespace for grouping related state:

```yaml
- id: round_0_agent_a
  type: extension
  extension: llm-agent
  scope: "round/0"
  inputs:
    prompt: "{{ .question }}"
```

- Variables written by a step with `scope: "round/0"` are stored under `scope.round/0.*`
- Scope vars persist within the scope but are **not visible to other scopes** unless exported
- Steps in the same scope can read each other's outputs via `{{ .scope.round/0.var }}`

#### Export

A step may export scope-local or step-local variables to the global namespace:

```yaml
- id: summarize
  type: extension
  extension: reducer
  scope: "round/1"
  export: ["decision", "scores"]
```

- `export: [<var_names>]` promotes the listed variables from the step/scope namespace → global
- Exported vars become available to all subsequent steps as `{{ .decision }}`, `{{ .scores }}`
- Without `export`, scope/step vars die with their scope

#### Merge rules

- **Step-local:** dies at step end unless referenced by step ID (`{{ .step_id.output }}`) or exported
- **Scope-local:** persists within scope, invisible to other scopes, dies at scope end unless exported
- **Global:** persists for entire run
- **Parallel safety:** parallel steps must not write to the same global key unless a subsequent reducer step merges. Two parallel branches writing the same global key → validation error (unchanged).

### Visibility Intent

Steps may declare visibility constraints on their inputs — which variables they are **allowed** to see:

```yaml
- id: blind_review
  type: extension
  extension: llm-agent
  visibility:
    allow: ["question", "scope.round/0.*"]
    deny: ["scope.round/0.agent_b.*"]
```

- `visibility.allow` — glob patterns of variable paths this step can access (whitelist)
- `visibility.deny` — glob patterns explicitly hidden from this step (blacklist, applied after allow)
- **Kernel behavior (v0):** recorded in trace as `visibility_applied` event. The kernel passes visibility metadata to executors/resolvers. **Enforcement is optional in v0** — hosts and extension runners may enforce it, but the kernel does not filter variables. Future versions may enforce at the engine level.
- **Trace event:** `{ type: "visibility_applied", data: { step_id, allow, deny } }`
- Purpose: enables debate patterns where agents must not see each other's outputs until a specific phase.

---

## 8. Parallel Execution

### Mechanics

1. `parallel` step contains `branches`, each with a list of steps.
2. Each branch receives a **forked state snapshot** (copy of all variables at the fork point).
3. Branches execute concurrently (goroutines).
4. **No implicit merge precedence.** If two branches declare an output with the same name, it is a **validation error**. Authors must use distinct output names. This prevents silent data loss where one branch's result masks another's.
5. When all branches complete, execution continues after the `parallel` block.

### Safety via contracts

**Conflict rule:** two branches can run in parallel if neither writes to a resource the other reads or writes.

```
Branch A: reads=[network], writes=[]
Branch B: reads=[network], writes=[]
→ Safe (both read, neither writes)

Branch A: reads=[], writes=[service]
Branch B: reads=[service], writes=[]
→ Conflict (A writes what B reads)
```

The kernel computes set intersections from contracts. If a conflict is detected:
- At validation time (static): **warning** (contracts may be conservative)
- At runtime: **serialize the conflicting branches** (execute sequentially, not concurrently)

### Governance interaction

- Steps with `side_effects: true` + `idempotent: false` in a parallel block → governance can require approval or force serialization.
- Contract properties drive the decision, not step types.

### Trace

Parallel execution produces:
- `parallel_fork` event with branch list and forked state hash
- Per-branch events with branch context (index, parent block ID)
- `parallel_merge` event with merged state and branch outcomes

---

## 9. Governance

Contract-driven, not command-name-driven.

### Model

- **Input:** step's resolved contract + runbook-level governance policy
- **Output:** `allow` | `require-approval` | `deny`

### Risk classification

Derived from contract properties:

| Property combination | Risk level |
|---------------------|------------|
| `side_effects=false` | Low |
| `side_effects=true` + `idempotent=true` | Medium |
| `side_effects=true` + `idempotent=false` + `deterministic=true` | High |
| `side_effects=true` + `idempotent=false` + `deterministic=false` | Critical |

### Policy document

Governance rules are declarative. The kernel evaluates them; it doesn't hardcode them.

```yaml
governance:
  rules:
    - risk: critical
      action: require-approval
      min_approvers: 2
    - risk: high
      action: require-approval
    - contract:
        writes: [production]
      action: require-approval
    - default: allow
```

### Approval gates

- Triggered by governance evaluation, not step type.
- Any step with a contract can require approval (not just manual steps).
- Approval results recorded in trace.

### Governance policy precedence

When a runbook declares `meta.governance` and an external policy document also applies:

1. **External policy is the floor.** It sets the minimum governance requirements.
2. **Runbook-level policy can only tighten.** A runbook can escalate `allow` → `require-approval`, but cannot relax `require-approval` → `allow`.
3. **Most restrictive wins.** If external policy says `require-approval` for critical risk and the runbook says `allow`, the external policy prevails.

This mirrors the contract tightening principle: inner scopes restrict, never relax.

---

## 9.5. Error Model

Defines what happens when things go wrong at runtime.

### Step status values

Every step execution produces one of four statuses:

| Status | Meaning |
|--------|--------|
| `success` | Step completed normally, outputs available |
| `failed` | Step executed but produced a failure result (e.g., non-zero exit, assertion false) |
| `skipped` | Step was not executed (`when` guard was false, or governance denied) |
| `error` | Step could not execute (infrastructure failure, timeout, contract violation) |

### Failure semantics by step type

| Step type | What constitutes `failed` | What constitutes `error` |
|-----------|--------------------------|-------------------------|
| **tool** | Non-zero exit code (stdio), error response (jsonrpc/mcp) | Binary not found, transport timeout, extract pattern mismatch |
| **manual** | Operator explicitly rejects / provides negative evidence | Evidence collection infrastructure failure |
| **assert** | Assertion evaluates to false | Expression evaluation error (undefined variable that passed static analysis due to conditional paths) |
| **extension** | Extension reports failure via its contract | Extension process crash, communication failure |

### Failure propagation

| Context | Behavior |
|---------|----------|
| **Top-level step fails** | Execution halts. Trace records the failure. Runbook ends without reaching an `end` step → runtime error outcome. |
| **Step inside a branch arm fails** | Branch arm halts. Execution does not continue to sibling arms. |
| **Step inside a parallel branch fails** | That branch is marked failed. Other branches continue to completion. `parallel_merge` records per-branch outcomes. Execution after the parallel block halts if any branch failed. |
| **Governance `deny`** | Step status is `skipped` with reason `governance_denied`. Execution halts (a denied step is a hard stop, not a skip-and-continue). |
| **Governance `require-approval` + rejected** | Same as `deny` — step is `skipped`, execution halts. |
| **Contract violation at runtime** | Step status is `error`. Example: a tool produces an output not declared in its contract. |

### `on_failure` (future consideration)

The kernel does not currently support per-step error handlers. The failure propagation rules above provide predictable halt semantics. If needed, authors can model error handling with `branch` steps that check for error conditions. A future kernel version may introduce `on_failure` as a step modifier.

### Trace events for failures

- `step_complete` with `status: failed` or `status: error` includes a `failure` object: `{ kind: "exit_code" | "assertion" | "denied" | "contract_violation" | "timeout" | ..., message: "..." }`
- `governance_decision` with `decision: deny` or `decision: require-approval` + `approval_result: rejected`

---

## 10. Extension Pockets

### Extensibility metadata

```yaml
meta:
  name: my-runbook
  extensions:
    x-team: platform-eng
    x-priority: P1
    x-custom-metadata:
      anything: goes here

steps:
  - id: step1
    type: tool
    extensions:
      x-slo-target: 99.9
      x-dashboard: https://grafana/d/abc
```

### Rules

- `extensions` is `map[string]any` at runbook and step level.
- Kernel **preserves** extension data through execution.
- Kernel **audits** extension data in trace (recorded in relevant events).
- Kernel **never interprets** extension data by default.
- Enables ecosystem evolution without schema churn.

---

## 11. Trace — Audit Backbone

Append-only JSONL. Synced at every event boundary.

### Principal Attribution

Every trace event MAY include a `principal` field, and MUST include it when the action is attributable to a non-system actor:

```json
{
  "principal": {
    "kind": "agent",
    "id": "agent-skeptic-01",
    "role": "skeptic",
    "model": "claude-4-opus"
  }
}
```

| Field | Required | Values | Description |
|-------|----------|--------|-------------|
| `kind` | yes | `system`, `human`, `agent` | Who performed the action |
| `id` | yes | stable string | Unique identifier (email, agent ID, "kernel") |
| `role` | no | free-form | Functional role (e.g., "skeptic", "judge", "oncall") |
| `model` | no | free-form | For agents: model identifier (e.g., "claude-4-opus") |

**Where principal appears:**

| Event | Principal |
|-------|-----------|
| `step_start` / `step_complete` (tool) | The actor who triggered the step (system for automated, agent for AI-driven) |
| `step_start` / `step_complete` (extension) | The extension runner's declared principal |
| `step_complete` (manual) | The human who provided evidence |
| `governance_decision` | System (kernel governance engine) |
| `approval_submitted` / `approval_resolved` | The approver (human or agent) |

If no principal is specified, the default is `{ kind: "system", id: "kernel" }`.

### Event types

| Event | When emitted | Key fields |
|-------|-------------|------------|
| `step_start` | Before step execution | step_id, type, contract_summary |
| `step_complete` | After step execution | step_id, status, outputs, duration |
| `branch_enter` | Entering a branch arm | branch_label, condition |
| `branch_exit` | Leaving a branch arm | branch_label |
| `parallel_fork` | Starting parallel block | branch_count, forked_state_hash |
| `parallel_merge` | All branches done | merged_outputs, branch_outcomes |
| `outcome_resolved` | `end` step reached | structured_outcome (category, code, meta) |
| `contract_evaluated` | Contract resolved for a step | step_id, resolved_contract |
| `governance_decision` | Policy evaluated | step_id, risk_level, decision (allow/deny/require-approval) |
| `redaction_applied` | Output sanitized | step_id, pattern_count |
| `for_each_start` | Iteration begins | over, item_count, parallel |
| `for_each_item` | Per-item iteration | index, value |
| `repeat_start` | Repeat block begins | step_id, max |
| `repeat_iteration` | Each iteration | step_id, index, until_result |
| `visibility_applied` | Visibility constraints recorded | step_id, allow, deny |
| `scope_export` | Variables exported from scope to global | step_id, scope, exported_vars |

### Purpose

- **Auditability** — who did what, when, and what did governance say
- **Replay** — deterministic re-execution from trace
- **Analytics** — structured outcomes + trace events enable intelligence
- **Trust** — every decision is recorded, nothing is implicit

---

## 12. Tool Definition Format

Tools carry contracts. The definition file centers on the contract.

```yaml
apiVersion: tool/v0
meta:
  name: health-check
  description: Check service health via HTTP
  transport: stdio              # stdio | jsonrpc | mcp
  binary: curl                  # what to spawn (overrides argv[0] for process lookup)
  platform: [linux, darwin, windows]  # optional — declares OS compatibility

contract:
  inputs:
    url: { type: string, required: true }
    timeout: { type: int, default: 30 }
  outputs:
    status_code: { type: int }
    body: { type: string }
  side_effects: false
  deterministic: true
  idempotent: true
  reads: [network]
  writes: []

actions:
  check:
    description: GET request and return status
    argv: ["curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "{{ .url }}"]
    extract:                        # maps tool output to declared outputs
      status_code: { from: stdout, pattern: "^(\\d+)$" }

  post:
    description: POST data to endpoint
    argv: ["curl", "-X", "POST", "-d", "{{ .payload }}", "{{ .url }}"]
    contract:                       # per-action tightening only
      inputs:
        payload: { type: string, required: true }
      side_effects: true
      writes: [network]
```

### Design principles

- **Contract is top-level** — the first thing you see, not buried in action details.
- **Tool-level contract is the default** — actions inherit and can only tighten.
- **`extract` maps output to contract** — this is plumbing. The kernel sees only `contract.outputs`.
- **Three transports:** `stdio` (spawn process), `jsonrpc` (persistent process), `mcp` (Model Context Protocol).

### 12.1 Platform Awareness

Tools can declare platform constraints via `meta.platform`:

```yaml
meta:
  name: registry-check
  binary: reg.exe
  platform: [windows]
```

**Rules:**

- `platform` is an optional list of GOOS values: `linux`, `darwin`, `windows`, `freebsd`, `openbsd`.
- **Omitting `platform` means the tool is platform-agnostic** (the common case for tools like `curl`, `jq`, etc.).
- Validation emits a **warning** (not an error) when the current OS isn't in the list. The runbook is still valid — the author may be targeting a different deployment environment, or running in WSL/Docker.
- Dry-run reports the platform constraint alongside the contract.
- At runtime, execution proceeds regardless. If the binary isn't found, the tool step fails with `error` status per §9.5 (binary not found).

### 12.2 Binary Resolution

The `meta.binary` field specifies which executable to spawn:

- If `meta.binary` is set, it overrides `argv[0]` for process lookup. The remaining `argv[1:]` are still used as arguments.
- If `meta.binary` is omitted, `argv[0]` is used directly.
- On Windows, Go's `exec.LookPath` appends `.exe` / `.cmd` / `.bat` via `PATHEXT` automatically.

This means a tool can use `binary: curl` and `argv: ["curl", "-s", "{{ .url }}"]` — the binary field controls what gets resolved, the argv controls the full argument list. Or the tool can omit `binary` entirely and rely on `argv[0]`.

### 12.3 Tool Resolution

The `tools` field in a runbook is an **enforced allow-list**. Only tools declared in this field can be referenced by steps. A step referencing an undeclared tool is rejected at validation time. This provides a security boundary — reviewers can see every tool a runbook may invoke at a glance.

**Resolution order** (first match wins):

1. **Relative path:** `tools/<name>.tool.yaml` relative to the runbook file.
2. **Project-level tools directory:** `<project-root>/tools/<name>.tool.yaml` (where project root is the nearest directory containing a `gert-project.yaml`).
3. **Explicit path:** If the entry is a path (contains `/` or `\`), resolve it directly.

No remote registry in kernel/v0. Tool files must be locally available. Ecosystem tooling can implement fetch/sync on top of this convention.

---

## 13. CLI

Four commands. The kernel CLI is four verbs.

| Command | Purpose | Key flags |
|---------|---------|-----------|
| `gert validate <file>` | 3-phase validation. Exit 0/1. | |
| `gert exec <file>` | Execute a runbook. Produce trace + outcome. | `--var`, `--input`, `--mode` (real/dry-run/replay), `--scenario` |
| `gert test <file...>` | Scenario replay tests with assertions. | `--scenario`, `--json`, `--fail-fast`, `--timeout` |
| `gert schema` | Export JSON Schema to stdout. | |

`gert --version` for version info (flag, not command).

Everything else (debug, tui, serve, diagram, compile, migrate) is ecosystem tooling that imports the kernel's Go packages.

---

## 14. What Survives from the Current Codebase

### Patterns (not code)

| Pattern | How it maps |
|---------|------------|
| Provider interface abstraction | Becomes contract-driven step dispatch |
| 3-phase validation pipeline | Same architecture: structural → semantic → domain |
| Append-only JSONL trace | Same mechanics, richer events |
| Replay with canned responses | Same concept, understands parallel + contracts |
| Scenario-based test harness | Same concept, asserts on structured outcomes |

### Keep as-is

| Package | Reason |
|---------|--------|
| `cmd/gert/main.go` shell | Cobra wiring pattern, commands change |
| `pkg/evidence/` | SHA256 hashing, evidence value factory — still needed |
| Redaction logic | Regex-based sanitization — still needed, also traced |

### Delete entirely

| Package / artifact | Reason |
|--------------------|--------|
| `pkg/compiler/` | Outside kernel |
| `pkg/debugger/` | Outside kernel |
| `pkg/tui/` | Outside kernel |
| `pkg/serve/` | Outside kernel |
| `pkg/icm/` | Outside kernel |
| `vscode/` | Separate project |
| v0/v1 schemas | Replaced by `kernel/v0` |
| All existing runbooks, fixtures, scenarios | Incompatible with new schema |

---

## 15. Build Order

```
Phase 1: Foundation
  ├── 1. Contract model (Go types)
  └── 2. Schema (new structs, YAML tags)

Phase 2: Validation
  └── 3. 3-phase validation pipeline
       ├── Structural (YAML decode, strict fields + extension passthrough)
       ├── Semantic (JSON Schema Draft 2020-12)
       └── Domain (path analysis, variable resolution, contract consistency,
                    parallel conflict detection, end-step reachability)

Phase 3: Core Engine
  ├── 4. Governance engine (contract-based)
  ├── 5. Sequential execution engine
  └── 6. Trace system (all event types)

Phase 4: Advanced Execution
  ├── 7. Parallel execution (state forking, deterministic merge)
  ├── 8. for_each expansion (sequential + parallel modes)
  └── 9. next/goto (forward always, backward bounded)

Phase 5: Ecosystem
  ├── 10. Replay system (trace + contracts + parallel-aware)
  └── 11. Test harness (replay + structured outcome assertions)
```

**Dependencies:**
- Phase 1 → everything depends on it
- Phase 2 → depends on Phase 1 (can validate without executing)
- Phase 3 steps 4+5 can proceed in parallel
- Phase 4 items 7+8+9 are independent of each other
- Phase 5 depends on Phase 3+4

---

## 16. Example Runbook

```yaml
apiVersion: kernel/v0

meta:
  name: service-health-check
  description: Diagnose and remediate service health issues
  inputs:
    hostname: { type: string, required: true, description: "Target service hostname" }
    threshold: { type: int, default: 200, description: "Max response time in ms" }
  constants:
    health_endpoint: "/healthz"
    max_retries: 2
  governance:
    rules:
      - risk: critical
        action: require-approval
      - default: allow
  extensions:
    x-team: platform-eng
    x-runbook-type: mitigation

tools:
  - health-check
  - restart-service

steps:
  - id: check_dns
    type: tool
    tool: health-check
    action: check
    inputs:
      url: "https://{{ .hostname }}{{ .health_endpoint }}"

  - id: evaluate_health
    type: assert
    continue_on_fail: true          # non-fatal — result feeds into triage branch
    assert:
      - type: equals
        value: "{{ .status_code }}"
        expected: "200"

  - id: triage
    type: branch
    branches:
      - condition: "{{ eq .status_code \"200\" }}"
        label: healthy
        steps:
          - type: end
            outcome:
              category: no_action
              code: service_healthy
              meta:
                status_code: "{{ .status_code }}"

      - condition: "{{ eq .status_code \"503\" }}"
        label: degraded
        steps:
          - id: restart
            type: tool
            tool: restart-service
            action: restart
            inputs:
              service: "{{ .hostname }}"

          - id: verify
            type: tool
            tool: health-check
            action: check
            inputs:
              url: "https://{{ .hostname }}{{ .health_endpoint }}"
            next: { step: restart, max: "{{ .max_retries }}" }
            when: "{{ ne .status_code \"200\" }}"

          - type: end
            outcome:
              category: resolved
              code: service_restarted
              meta:
                attempts: "{{ .verify.retry_count }}"

      - condition: default
        label: unknown
        steps:
          - id: check_failed
            type: manual
            instructions: "Unexpected status {{ .status_code }}. Investigate manually."
            contract:
              side_effects: false
            required_evidence:
              - kind: text
                name: investigation_notes

          - type: end
            outcome:
              category: escalated
              code: unknown_failure
              meta:
                status_code: "{{ .status_code }}"
```

---

## 17. Open Items / Future

| Item | Notes |
|------|-------|
| ~~Tool discovery & loading~~ | Resolved — see §12.3 |
| MCP transport details | `transport: mcp` — server config, capability negotiation. Future: kernel provides MCP client plumbing; ecosystem wraps it. |
| ~~Error model~~ | Resolved — see §9.5 |
| ~~Extension step runtime~~ | Deferred to ecosystem. The kernel stubs extension steps (emits trace, returns error). Ecosystem libraries can implement `engine.ToolExecutor`-style interfaces over external processes. |
| ~~Replay format for parallel~~ | Resolved — the `ReplayExecutor` consumes canned `tool_responses` in order, independently per branch. Parallel branches that call the same tool consume responses sequentially by declaration order. |
| ~~Outcome category extensibility~~ | The four categories (resolved, escalated, no_action, needs_rca) are **fixed**. Domain-specific meaning lives in `outcome.code` and `outcome.meta`, which are free-form. Governance rules can key on categories; extending the enum would break policy contracts. |
| ~~Error model (dup)~~ | See §9.5 |
| ~~Dry-run mode~~ | Resolved — dry-run skips tool execution and manual prompts. For each step it still evaluates: contract resolution, governance policy, template resolution for inputs. Trace records `contract_evaluated` and `governance_decision` events. Tool steps report resolved inputs + contract properties to stdout. |
