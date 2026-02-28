# Kernel API Contracts: Gert Ecosystem v0

## Engine Interfaces

### ApprovalProvider

```go
type ApprovalProvider interface {
    Submit(ctx context.Context, req ApprovalRequest) (*ApprovalTicket, error)
    Wait(ctx context.Context, ticket *ApprovalTicket) (*ApprovalResponse, error)
}
```

- `Submit` MUST return immediately (non-blocking)
- `Wait` MUST respect context cancellation
- For synchronous providers: `Submit` + `Wait` happen atomically inside `Wait`
- For async providers: `Submit` sends notification, `Wait` polls/listens

### InputResolver

```go
type InputResolver interface {
    Resolve(ctx context.Context, binding InputBinding) (string, error)
}
```

- Called by `ResolveInputs()` per binding
- MUST be stateless between calls
- MUST respect context cancellation

### ResolveInputs (kernel API)

```go
func ResolveInputs(
    ctx context.Context,
    rb *schema.Runbook,
    hostVars map[string]string,
    resolvers []InputResolver,
) (*ResolvedInputs, error)
```

- Resolution order: hostVars → resolvers → defaults → error
- MUST produce one `input_resolved` trace event per binding
- ALL hosts MUST call this — no host-specific resolution logic

### ToolExecutor (revised)

```go
type ToolExecutor interface {
    Execute(ctx context.Context, toolDef *schema.ToolDefinition, actionName string, inputs map[string]any, vars map[string]any) (*executor.Result, error)
}
```

- Gains `context.Context` as first parameter (breaking change from current)
- MUST respect context cancellation for timeout enforcement

### Engine.Run (revised)

```go
func (e *Engine) Run(ctx context.Context) *RunResult
```

- Gains `context.Context` parameter
- Propagates context to all step executions, tool calls, approval providers

## Trace Writer Contracts

### Hash Chaining

- Every event MUST include `prev_hash` field
- First event: `prev_hash = "0000000000000000000000000000000000000000000000000000000000000000"` (64 zero hex chars)
- Subsequent events: `prev_hash = SHA256(previous_event_json_bytes)`
- `Emit()` caches previous event bytes for efficiency

### Run Signing

- If `GERT_TRACE_SIGNING_KEY` is set, `run_complete` event includes:
  - `chain_hash`: SHA-256 of the final event's JSON
  - `signature`: HMAC-SHA256 of `chain_hash` with signing key
  - `signing_key_id`: label from `GERT_TRACE_SIGNING_KEY_ID` env var

### Principal Attribution

- `step_start`/`step_complete` for extension and manual steps MUST include `principal`
- `approval_submitted`/`approval_resolved` MUST include `principal`
- Default principal: `{ kind: "system", id: "kernel" }`

## Extension Runner Protocol

### Transport

JSON-RPC 2.0 over stdin/stdout of spawned process.

### Methods

| Method | Direction | Request | Response |
|--------|-----------|---------|----------|
| `initialize` | kernel → runner | `{ protocol_version: "1" }` | `{ capabilities: {} }` |
| `execute` | kernel → runner | `{ inputs, vars, contract }` | `{ outputs, exit_code, stderr }` |
| `shutdown` | kernel → runner | `{}` | `{}` |

### Lifecycle

- Spawn on first `extension` step referencing the runner
- Reuse for subsequent steps with same runner name
- Shutdown on engine completion
- Timeout: step's `timeout` field, default 60 seconds

### Output Enforcement

- Outputs checked against `contract.outputs`
- Undeclared outputs: stripped + `contract_violation` trace event
- Missing declared outputs: `contract_violation` trace event (warning)

## Governance Contracts

### Effects Matching

Governance rules gain `effects` field alongside existing `risk`, `contract`, `default`:

```yaml
governance:
  rules:
    - effects: [kubernetes]
      writes: [production]
      action: require-approval
```

- `effects` match: ANY listed effect present in step contract → rule matches
- `writes` match: ANY listed write present in step contract → rule matches
- Both required: both must match if both specified

### Contract Violations Matcher

```yaml
governance:
  rules:
    - contract_violations: deny
```

- When present: any `contract_violation` event promotes to step error and halts
- Default (absent): violations are warnings only

### Approval Timeout

```yaml
governance:
  approval_timeout: 30m
```

- Configurable per-runbook
- Default: 30 minutes
- Expired tickets treated as rejections
