# Invoke Step — Mid-Tree Sub-Runbook Invocation

## Status: proposed

## Problem

Runbooks often need to run a reusable check (DNS, SSL, connectivity) as a gate before
proceeding. Today, this requires either:

1. **Inline branching** — copy-pasting the check logic into every parent runbook, creating
   deep nesting and duplication.
2. **Outcome chaining** (`next_runbook`) — only works at the end of a runbook, not mid-tree.

Neither supports the pattern: "call this sub-runbook, get its result, decide whether to continue."

## Proposal

A new step type `invoke` that runs a child runbook inline, waits for its outcome, and
optionally stops the parent based on the result.

### Schema: `imports` (top-level)

A map of aliases to runbook file paths, declared at the top of the document:

```yaml
apiVersion: runbook/v0
imports:
    dns-check: ../dns-check/dns-check.runbook.yaml
    ssl-check: ../ssl-check/ssl-check.runbook.yaml
meta:
    name: connectivity-check
    kind: mitigation
```

- **Key**: short alias used in `with.runbook`
- **Value**: path relative to the importing runbook's directory
- Resolved and validated at compile time (or `gert validate`)
- Cycle detection: A → B → A is a compile error

### Schema: `invoke` step type

```yaml
- step:
    id: dns_gate
    type: invoke
    title: Verify DNS resolves
    with:
        runbook: dns-check          # alias from imports (or raw path)
        inputs:
            hostname: "{{ .hostname }}"
    gate:
        stop_if: escalated          # string or list of outcome states
        on_error: skip              # optional: "skip" (with warning) or omit (default: stop)
    capture:
        dns_output: dns_output      # child_capture_name: parent_var_name
```

### `InvokeConfig` struct

```go
type InvokeConfig struct {
    Runbook string            `yaml:"runbook"          json:"runbook"          jsonschema:"required"`
    Inputs  map[string]string `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

type Gate struct {
    StopIf  StringOrList `yaml:"stop_if,omitempty"  json:"stop_if,omitempty"`
    OnError string       `yaml:"on_error,omitempty" json:"on_error,omitempty" jsonschema:"enum=skip"`
}
```

### Step struct additions

```go
type Step struct {
    // ... existing fields ...
    Invoke *InvokeConfig `yaml:"invoke,omitempty" json:"invoke,omitempty"`
    Gate   *Gate         `yaml:"gate,omitempty"   json:"gate,omitempty"`
}
```

### Runbook struct additions

```go
type Runbook struct {
    APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
    Imports    map[string]string `yaml:"imports,omitempty" json:"imports,omitempty"`
    Meta       Meta              `yaml:"meta" json:"meta"`
    Tree       []TreeNode        `yaml:"tree,omitempty" json:"tree,omitempty"`
    Steps      []Step            `yaml:"steps,omitempty" json:"steps,omitempty"`
}
```

## Execution Flow

```
Parent tree step-by-step:
  → step 1 (cli)        ✓ passed
  → step 2 (invoke)     dns-check
      │
      ├─ Load child runbook (from imports or raw path)
      ├─ Resolve input templates against parent vars/captures
      ├─ Create child engine (same executor, fresh state)
      ├─ Run child tree to completion
      │    └─ Child steps appear in workflow map under an "invoke" group
      ├─ Child reaches outcome: resolved / escalated / ...
      │
      ├─ Gate check:
      │    ├─ outcome in stop_if? → propagate outcome, stop parent
      │    └─ outcome not in stop_if? → continue parent
      │
      ├─ Copy captures per capture map into parent scope
      └─ Mark invoke step passed/failed
  → step 3 (cli)        ← only reached if gate didn't stop
```

## Error Handling

| Scenario | Default | `on_error: skip` |
|---|---|---|
| Child runbook file not found | Parent stops with error | Step marked `skipped (error)`, warning shown, parent continues |
| Child validation fails | Parent stops with error | Same as above |
| Child step crashes (exit code ≠ 0) | Parent stops with error | Same as above |
| Child reaches outcome in `stop_if` | Parent stops with child's outcome | Same — `on_error` doesn't apply to outcomes |
| Child reaches outcome NOT in `stop_if` | Parent continues | Parent continues |

**Philosophy:** Outcomes are control flow (expected). Errors are crashes (unexpected).
`on_error: skip` is for optional checks where failure shouldn't block the operator.
A visible `⚠ skipped (error)` badge appears in the workflow map so no failure is silent.

## Workflow Map Rendering

```
✓  ⚡ 1.1  Resolve DNS for hostname
   ▼ invoke: dns-check
      ✓  ⚡ 1.1.1  Resolve DNS for hostname
      ✓  🧑 1.1.2  Assess DNS resolution result
         ▼ DNS resolved
            ✓  🧑 1.1.3  DNS resolution healthy
               └─ resolved
   └─ gate: passed (resolved ∉ [escalated])
●  ⚡ 1.2  Check service health
```

Child steps are indented under the invoke step with sub-numbering (`1.1.x`).
The gate result is shown as a one-liner after the child tree.

## Validation Rules

1. `type: invoke` requires `invoke.runbook` (or `with.runbook` — TBD field location)
2. If `invoke.runbook` is an alias, it must exist in `imports`
3. Imports must resolve to existing `.runbook.yaml` files
4. Cycle detection: transitive import graph must be acyclic
5. Child `meta.inputs` must be satisfiable from `invoke.inputs`
6. `gate.on_error` only accepts `skip` (or omitted)
7. `gate.stop_if` values must be valid outcome states

## Relationship to Existing Features

| Feature | Scope | When it fires |
|---|---|---|
| **`precondition`** | Single command, pass/fail | Before a step — auto-skip if satisfied |
| **`invoke`** | Full sub-runbook | Inline, mid-tree — runs to completion |
| **`next_runbook`** | Outcome-triggered chain | Terminal — parent is done, child takes over |

They compose naturally:
- `precondition` on an `invoke` step: skip the entire sub-runbook if a quick probe passes
- `invoke` with `gate.stop_if`: run a full diagnostic, stop if it escalates
- `next_runbook` on the final outcome: hand off to the next team's runbook

## Kind Convention

Runbooks designed to be invoked should use `kind: composable`:

```yaml
meta:
    name: dns-check
    kind: composable
    description: Reusable DNS resolution check
    inputs:
        hostname:
            from: parent
            description: Hostname to resolve
```

`from: parent` signals the input comes from the invoking runbook, not from a prompt or ICM.
Not enforced — `from: prompt` still works for standalone testing.

## Open Questions

- [ ] Field location: `with.runbook` + `with.inputs` vs dedicated `invoke:` block?
- [ ] Should `imports` support glob patterns for runbook libraries?
- [ ] Maximum invoke depth limit (e.g., 5)?
- [ ] How does resume work? Resume parent at the invoke step, re-run child from scratch?
- [ ] Should child captures be auto-exported (all) or explicit-only (capture map)?
- [ ] How do traces link? Same trace with child as a sub-span, or separate trace?
- [ ] Compiler: should `gert compile` inline the child tree for offline use?

## Test Plan

1. **Unit**: import resolution, cycle detection, gate evaluation
2. **Integration**: parent invokes child, child reaches outcome, gate stops/continues
3. **Scenario**: dns-check as composable, parent invokes it, replays work
4. **Extension**: workflow map renders child steps nested, gate badge visible
5. **Edge cases**: missing import, cycle, child error with `on_error: skip`
