# Core Isolation — Implementation Plan

## Overview

Decouple ICM and XTS from gert's core so the orchestrator is domain-agnostic.
5 phases, A through E. A and C are parallel. Total estimated: 2–3 weeks.

---

## Phase A: Extract ICM Input Resolution (1–2 days)

**Goal:** `pkg/serve` no longer imports `pkg/icm`. Input resolution dispatches
through a generic `pkg/inputs/` manager.

### Tasks

1. **Create `pkg/inputs/types.go`** — protocol types:
   ```go
   type ResolveRequest struct {
       Bindings map[string]InputBinding  // input name → {From, Pattern}
       Context  map[string]string        // e.g. {"icmId": "748724360"}
   }
   type InputBinding struct {
       From    string
       Pattern string
   }
   type ResolveResult struct {
       Resolved map[string]string  // input name → resolved value
       Warnings []string
   }
   type InputProvider interface {
       Prefixes() []string
       Resolve(ctx context.Context, req *ResolveRequest) (*ResolveResult, error)
       Shutdown() error
   }
   ```

2. **Create `pkg/inputs/manager.go`** — dispatches to providers by prefix:
   ```go
   type Manager struct {
       providers map[string]InputProvider  // prefix → provider
   }
   func (m *Manager) Register(provider InputProvider)
   func (m *Manager) Resolve(ctx context.Context, inputs map[string]*schema.InputDef,
       context map[string]string) (map[string]string, []string, error)
   func (m *Manager) Shutdown()
   ```
   `Resolve()` groups bindings by prefix, dispatches to matching provider,
   merges results. Inputs with `from: prompt` are skipped (handled by the
   serve layer's prompt flow).

3. **Move `resolveICMInputs()` into `pkg/icm/resolve.go`**:
   - Move from `serve.go` lines 1776–1912
   - Move `extractTitleFields()`, `snakeToPascal()` helpers
   - Keep the same function signatures
   - Move `resolve_icm_test.go` into `pkg/icm/resolve_test.go`

4. **Create `pkg/icm/provider.go`** — wraps ICM resolution as an `InputProvider`:
   ```go
   type ICMInputProvider struct{}
   func (p *ICMInputProvider) Prefixes() []string { return []string{"icm."} }
   func (p *ICMInputProvider) Resolve(ctx context.Context, req *inputs.ResolveRequest) (*inputs.ResolveResult, error) {
       icmID := req.Context["icmId"]
       client := New()
       incident, err := client.Get(icmID)
       // call resolveICMInputs with bindings...
   }
   ```

5. **Update `pkg/serve/serve.go`** — replace the hardcoded ICM block:
   ```go
   // Before (lines 252-281):
   if params.ICMID != "" && rb.Meta.Inputs != nil {
       icmClient := icm.New()
       incident, _ := icmClient.Get(icmID)
       resolved := resolveICMInputs(rb.Meta.Inputs, incident)
       ...
   }

   // After:
   if rb.Meta.Inputs != nil {
       inputMgr := inputs.NewManager()
       inputMgr.Register(&icm.ICMInputProvider{})  // ← still imported for now
       ctx := map[string]string{}
       if params.ICMID != "" { ctx["icmId"] = params.ICMID }
       resolved, warnings, _ := inputMgr.Resolve(s.ctx, rb.Meta.Inputs, ctx)
       ...
   }
   ```

6. **Update `cmd/gert/main.go`** — same pattern for `runExec` ICM resolution.

7. **Tests:**
   - `pkg/inputs/manager_test.go` — mock provider, prefix routing, merge
   - `pkg/icm/resolve_test.go` — existing tests moved, pass unchanged
   - `pkg/serve/` tests still pass (integration level)

### Verification
- `go test ./pkg/icm/ ./pkg/inputs/ ./pkg/serve/ ./pkg/testing/`
- `gert test` on login-success-rate runbook passes
- `pkg/serve` still imports `pkg/icm` (removed in Phase B)

---

## Phase B: Provider Protocol via Subprocess (2–3 days)

**Goal:** ICM provider runs as an external binary. `pkg/serve` has zero `pkg/icm` imports.

### Tasks

1. **Define `provider/v0` schema** in `pkg/schema/provider.go`:
   ```go
   type ProviderDefinition struct {
       APIVersion   string            `yaml:"apiVersion"`
       Meta         ProviderMeta      `yaml:"meta"`
       Transport    ToolTransport     `yaml:"transport"` // reuse from tool.go
       Capabilities ProviderCaps      `yaml:"capabilities"`
   }
   type ProviderMeta struct {
       Name        string `yaml:"name"`
       Description string `yaml:"description,omitempty"`
       Binary      string `yaml:"binary"`
   }
   type ProviderCaps struct {
       ResolveInputs *ResolveInputsCap `yaml:"resolve_inputs,omitempty"`
   }
   type ResolveInputsCap struct {
       Prefixes      []string `yaml:"prefixes"`
       ContextFields []string `yaml:"context_fields,omitempty"`
   }
   ```

2. **Create `pkg/inputs/jsonrpc_provider.go`** — subprocess provider:
   - Spawns binary using `tools.spawnJSONRPC` (reuse existing infra)
   - Sends `resolve` method with bindings + context
   - Parses response into `ResolveResult`
   - Process reused across calls, shutdown on runbook completion

3. **Create `pkg/inputs/loader.go`** — loads `.provider.yaml` files:
   ```go
   func LoadProvider(path string) (InputProvider, error)
   ```
   Returns either a `JSONRPCInputProvider` (external binary) or a
   builtin provider (for backward compat).

4. **Create workspace config loader** — `.gert/config.yaml`:
   ```go
   type WorkspaceConfig struct {
       Providers map[string]ProviderRef `yaml:"providers"`
       Tools     map[string]ToolRef     `yaml:"tools"`
   }
   type ProviderRef struct {
       Path   string            `yaml:"path"`
       Config map[string]string `yaml:"config,omitempty"`
   }
   ```

5. **Extract `gert-icm-provider` binary**:
   - New `cmd/gert-icm-provider/main.go`
   - JSON-RPC server over stdio
   - Handles `resolve` method using `pkg/icm/resolve.go` + `pkg/icm/client.go`
   - Ready signal: `"ready"` on stderr

6. **Update `pkg/inputs/manager.go`**:
   - On startup: load workspace config → load provider defs → register
   - Built-in ICM provider still works as fallback when no config exists

7. **Update `pkg/serve/serve.go`**:
   - Remove `icm` import
   - Load providers from workspace config (or use built-in fallback)
   - Pass provider manager to engine for lifecycle management

8. **Tests:**
   - `pkg/inputs/jsonrpc_provider_test.go` — mock provider binary integration
   - `cmd/gert-icm-provider/` — unit tests for resolve handler
   - End-to-end: `gert test` with ICM runbooks using external provider

### Verification
- `grep -r "pkg/icm" pkg/serve/` returns zero results
- `gert test` on ICM runbooks passes in both modes (built-in, external)
- Provider binary runs standalone: `echo '{"method":"resolve",...}' | gert-icm-provider`

---

## Phase C: Complete XTS Tool Migration (2–3 days)

**Goal:** `type: xts` routes through `ToolManager.Execute()`. Engine has no XTS code.

### Tasks

1. **Full engine desugar** — in `executeStep`, convert `type: xts` to tool call:
   ```go
   case "xts":
       toolStep := desugarXTSToToolStep(step, e.Runbook.Meta.XTS)
       e.executeToolStep(ctx, toolStep, result)
   ```
   Where `desugarXTSToToolStep` builds a synthetic `Step` with `Type: "tool"`,
   `Tool: &ToolStepConfig{Name: "__xts", Action: step.XTS.Mode, Args: ...}`.

2. **Register built-in XTS tool** — in `NewEngine()`, when `meta.xts` is present:
   ```go
   if rb.Meta.XTS != nil && e.ToolManager != nil {
       e.ToolManager.RegisterBuiltin("__xts", tools.BuiltinXTSToolDef())
   }
   ```
   Add `RegisterBuiltin(alias, def)` to `Manager` — loads a def without a file path.

3. **XTS tool binary resolution** — the tool def's `meta.binary` is `xts-cli`.
   The current `XTSMeta.CLIPath` override needs to flow through:
   - Add `binary_override` to `ToolStepConfig` args (resolved from `meta.xts.cli_path`)
   - Or: set env var `XTS_CLI_PATH` which the tool manager picks up

4. **XTS replay migration** — convert `XTSScenario.FindStepResponse` to
   standard command mocking:
   - When `XTSScenario` is present, convert `steps/*.json` to command mocks
     at load time: `{argv: ["xts-cli", ...], stdout: jsonContent, exit_code: 0}`
   - Inject into the `ReplayExecutor` alongside other command mocks
   - Remove `XTSScenario` check from engine

5. **Remove from engine**:
   - Delete `xtsProvider *providers.XTSProvider` field
   - Delete `XTSScenario *replay.XTSScenario` field
   - Delete `executeXTSStep()` method (~130 lines)
   - Delete XTS init in `NewEngine()` (~15 lines)
   - Delete XTS propagation in `chainToRunbook` and invoke paths

6. **Remove from providers**:
   - Delete `pkg/providers/xts.go` (or move to `gert-xts` package)
   - Keep `pkg/providers/provider.go` (generic interfaces)
   - Update `pkg/providers/xts_test.go` → move to external package

7. **Remove from replay**:
   - Delete `pkg/replay/xts_replay.go`
   - Keep base `pkg/replay/replay.go` and `scenario.go`

8. **Compiler update** — `pkg/compiler/`:
   - When compiling XTS steps from TSG markdown, emit `type: tool` + `tools:` import
   - Add `tools:` block to compiled runbook with XTS tool reference

9. **Tests:**
   - All existing XTS scenario tests pass (via tool routing)
   - `gert test` on login-success-rate works with desugared execution
   - Compiler output produces `type: tool` for XTS steps

### Verification
- `grep -r "xtsProvider\|XTSScenario\|executeXTSStep" pkg/runtime/` returns zero results
- `grep -r "xts.go" pkg/providers/` returns zero results
- All existing `gert test` scenarios pass
- Compile a TSG with XTS steps → output has `type: tool`

---

## Phase D: Schema Cleanup (1 day)

**Goal:** Remove XTS types from the core schema. Bump to `runbook/v1`.

### Tasks

1. **Remove from `schema.go`**:
   - Delete `XTSMeta` struct
   - Delete `XTSStepConfig` struct
   - Remove `XTS *XTSMeta` from `Meta`
   - Remove `XTS *XTSStepConfig` from `Step`
   - Remove `"xts"` from `Type` enum → `enum=cli,enum=manual,enum=invoke,enum=tool`
   - Add backward compat: `apiVersion: runbook/v0` still accepted,
     `type: xts` desugars at load time with a warning

2. **Add `apiVersion: runbook/v1`** — clean schema:
   - `runbook/v1` does not accept `type: xts`, `meta.xts`, or `step.xts`
   - `runbook/v0` continues to work with desugar at load time

3. **Update `validate.go`**:
   - Remove XTS-specific validation rules (`validateXTSStep`, etc.)
   - Remove `meta.xts` presence check
   - Remove XTS deprecation warning (no longer needed — `type: xts` not in v1)

4. **Update JSON Schema**:
   - Generate `schemas/runbook-v1.json` without XTS types
   - Keep `schemas/runbook-v0.json` for backward compat

5. **Migration CLI** — `gert migrate v0-to-v1`:
   - Rewrites `type: xts` → `type: tool` in YAML
   - Adds `tools:` block with XTS tool reference
   - Moves `meta.xts.environment` to step args
   - Bumps `apiVersion` to `runbook/v1`

6. **Tests:**
   - `runbook/v0` with `type: xts` still loads and runs (desugar)
   - `runbook/v1` rejects `type: xts` at validation
   - `gert migrate v0-to-v1` produces valid v1 output
   - All schema tests updated

### Verification
- `grep -r "XTSMeta\|XTSStepConfig" pkg/schema/` returns only backward-compat code
- `gert validate` on v1 runbook without XTS passes clean
- `gert validate` on v0 runbook with XTS still passes with warnings

---

## Phase E: Separate Repositories (1 day)

**Goal:** ICM and XTS live in their own repos. Core gert binary has zero
ICM/XTS imports.

### Tasks

1. **Create `github.com/ormasoftchile/gert-icm`**:
   - Move `pkg/icm/` → `gert-icm/pkg/icm/`
   - Move `cmd/gert-icm-provider/` → `gert-icm/cmd/gert-icm-provider/`
   - Move `configs/icm-field-mapping.yaml` → `gert-icm/configs/`
   - Move `docs/design/icm-collection-*.yaml` → `gert-icm/docs/`
   - Add `go.mod` with gert core as dependency (for `schema.InputDef` type)

2. **Create `github.com/ormasoftchile/gert-xts`**:
   - Move `pkg/providers/xts.go` → `gert-xts/pkg/xts/`
   - Move `pkg/replay/xts_replay.go` → `gert-xts/pkg/replay/`
   - Move `testdata/tools/xts.tool.yaml` → `gert-xts/tools/`
   - Move XTS test scenarios → `gert-xts/testdata/`
   - Add `go.mod`

3. **Update core gert**:
   - Remove `pkg/icm/` directory
   - Remove `pkg/providers/xts.go`
   - Remove `pkg/replay/xts_replay.go`
   - Remove `configs/icm-field-mapping.yaml`
   - Built-in ICM provider replaced by workspace config pointing to external binary
   - Update docs

4. **Update installation docs**:
   ```bash
   # Core
   go install github.com/ormasoftchile/gert/cmd/gert@latest

   # ICM integration (Azure SQL DB teams)
   go install github.com/ormasoftchile/gert-icm/cmd/gert-icm-provider@latest

   # XTS tool definition
   # Just reference the tool.yaml in your runbook:
   # tools:
   #   xts: github.com/ormasoftchile/gert-xts/tools/xts.tool.yaml
   ```

5. **CI/CD**: Core gert CI no longer depends on ICM API access or xts-cli in PATH.

### Verification
- Core gert builds with no ICM/XTS imports: `go build ./...` succeeds
- Core binary size decreased
- `gert test` on non-XTS runbooks works without xts-cli
- `gert test` on XTS runbooks works with gert-xts tool installed

---

## Dependency Graph

```
Phase A: Extract ICM input resolution
    │
    └──→ Phase B: Provider protocol via subprocess
              │
              └──→ Phase E: Separate repos (ICM side)

Phase C: Complete XTS tool migration   (parallel with A/B)
    │
    └──→ Phase D: Schema cleanup
              │
              └──→ Phase E: Separate repos (XTS side)
```

## Timeline

| Week | Phase | What ships |
|---|---|---|
| 1 | A + C (parallel) | Input manager abstraction + XTS full tool routing |
| 2 | B + D | External ICM provider binary + schema cleanup |
| 3 | E | Repository split, docs, CI |

## Risk Mitigation

| Risk | Mitigation |
|---|---|
| Phase C breaks XTS replay | Dual-mode test: run all XTS scenarios with both old and new paths during migration |
| Phase D schema break | Long deprecation: v0 desugars for 6+ months, v1 is opt-in |
| Phase B provider perf | Built-in provider is default; subprocess only when configured |
| Phase E install complexity | Bundle gert-icm-provider with gert releases initially |
| Teams have local xts.go modifications | `gert-xts` repo accepts PRs; forks supported |

## Success Criteria

- [x] `pkg/serve` has zero imports from `pkg/icm`
- [x] `pkg/runtime` routes XTS through tool manager when available
- [x] `pkg/schema` accepts `runbook/v1` and rejects XTS on v1
- [x] All existing scenario tests pass with new routing
- [x] All existing ICM input resolution tests pass
- [x] `gert migrate` command produces valid v1 output
- [x] `gert-icm-provider` binary builds and serves resolve protocol
- [ ] Core gert binary builds with zero ICM/XTS imports (Phase E — repo split)

## Phase E — Repository Split Manifest

Phase E requires creating separate GitHub repos. This manifest documents
exactly which files move where.

### `github.com/ormasoftchile/gert-icm`

| Source (gert repo) | Destination (gert-icm) |
|---|---|
| `pkg/icm/client.go` | `pkg/icm/client.go` |
| `pkg/icm/types.go` | `pkg/icm/types.go` |
| `pkg/icm/icm_test.go` | `pkg/icm/icm_test.go` |
| `pkg/icm/resolve.go` | `pkg/icm/resolve.go` |
| `pkg/icm/provider.go` | `pkg/icm/provider.go` |
| `cmd/gert-icm-provider/main.go` | `cmd/gert-icm-provider/main.go` |
| `configs/icm-field-mapping.yaml` | `configs/icm-field-mapping.yaml` |
| `configs/icm.provider.yaml` | `configs/icm.provider.yaml` |
| `pkg/serve/resolve_icm_test.go` | `pkg/icm/resolve_integration_test.go` |

**After split:** Remove `pkg/icm/`, `cmd/gert-icm-provider/`, `configs/icm*` from core.  
**core change:** `cmd/gert/main.go` removes `icm.ICMInputProvider` fallback —  
providers are loaded from workspace config only.

### `github.com/ormasoftchile/gert-xts`

| Source (gert repo) | Destination (gert-xts) |
|---|---|
| `pkg/providers/xts.go` | `pkg/xts/provider.go` |
| `pkg/providers/xts_test.go` | `pkg/xts/provider_test.go` |
| `pkg/replay/xts_replay.go` | `pkg/replay/xts_replay.go` |
| `testdata/tools/xts.tool.yaml` | `tools/xts.tool.yaml` |
| `testdata/valid/xts-query.yaml` | `testdata/xts-query.yaml` |
| `testdata/valid/xts-view.yaml` | `testdata/xts-view.yaml` |
| `testdata/valid/xts-activity.yaml` (if exists) | `testdata/xts-activity.yaml` |

**After split:** Remove `pkg/providers/xts.go`, `pkg/replay/xts_replay.go` from core.  
**core change:** `engine.go` removes `xtsProvider` field + `executeXTSStep` fallback —  
`type: xts` always desugars through tool manager.

### Post-split core changes

```go
// cmd/gert/main.go — remove:
import "github.com/ormasoftchile/gert/pkg/icm"
// replace ICMInputProvider registration with workspace config loader

// pkg/runtime/engine.go — remove:
xtsProvider *providers.XTSProvider
XTSScenario *replay.XTSScenario
executeXTSStep() method
// case "xts": always desugars, no fallback
```

### Execution steps

1. Create `github.com/ormasoftchile/gert-icm` repo with `go.mod`
2. Move files per manifest, update import paths
3. Publish v0.1.0 tag
4. Create `github.com/ormasoftchile/gert-xts` repo with `go.mod`
5. Move files, update import paths
6. Publish v0.1.0 tag
7. In core gert: delete moved files, remove imports, run `go mod tidy`
8. Verify `go build ./...` succeeds with zero ICM/XTS imports
9. Update docs with installation instructions
