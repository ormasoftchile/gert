# Gert Core Isolation — Extracting ICM and XTS as Integrations

## Status: proposed

## Problem

Gert's core orchestrator is coupled to two Azure-specific verticals:

1. **ICM** — Microsoft's incident management system. `pkg/icm/` contains an OData
   client, `pkg/serve/` has a 130-line `resolveICMInputs()` function, and `schema.InputDef`
   hardcodes `from: icm.*` binding prefixes. A team using gert for Kubernetes or
   PagerDuty incidents cannot use gert's input resolution without ICM code in the binary.

2. **XTS** — Azure Service Fabric query tool. `pkg/providers/xts.go` implements a
   full execution provider, `schema.XTSMeta` and `schema.XTSStepConfig` define a
   dedicated step type in the YAML contract, and `engine.go` has a private `xtsProvider`
   field with a 130-line `executeXTSStep()` method. `type: xts` is a first-class
   citizen in the engine alongside `cli` and `manual`.

Neither is essential to what gert fundamentally does: **parse a runbook, walk a tree,
execute steps, evaluate conditions, reach an outcome.** The core should be
domain-agnostic.

## Goals

1. **Gert core has zero ICM/XTS imports** — clean orchestrator
2. **ICM becomes an input provider** — pluggable, replaceable with PagerDuty/ServiceNow
3. **XTS becomes a tool definition** — standard `.tool.yaml`, no special engine code
4. **Existing runbooks keep working** during migration via compatibility shims
5. **New integration pattern** — any team can build a gert plugin without modifying core

## Non-goals

- Rewriting the ICM client or XTS provider from scratch
- Supporting multiple incident systems simultaneously in one runbook
- Building a plugin marketplace

## Current Coupling Assessment

| Component | Coupled to... | Extraction difficulty |
|---|---|---|
| `pkg/icm/` (client, types) | `pkg/serve` only | **Trivial** — already isolated |
| `resolveICMInputs()` | `pkg/serve`, `schema.InputDef` | **Low** — 130 lines, self-contained |
| `configs/icm-field-mapping.yaml` | Nothing (reference data) | **Zero** |
| `pkg/providers/xts.go` | `schema.XTSMeta`, `schema.XTSStepConfig` | **Medium** |
| `engine.xtsProvider` field | `runtime.Engine` struct | **Medium** |
| `engine.executeXTSStep()` | `runtime.Engine`, `providers.XTSProvider`, `replay.XTSScenario` | **Medium** |
| `schema.XTSMeta`, `schema.XTSStepConfig` | `schema.Meta.XTS`, `schema.Step.XTS` | **High** — YAML contract |
| `replay.XTSScenario` | `engine.XTSScenario` field, `serve.handleExecStart` | **Medium** |
| `engine.ICMID` field | `runtime.Engine` — stored, never read by engine | **Trivial** |

## Architecture: Before and After

### Before (current)

```
┌─────────────────────────────────────────────────┐
│                  gert binary                     │
│                                                  │
│  cmd/gert/                                       │
│  pkg/schema/     ← XTSMeta, XTSStepConfig       │
│  pkg/runtime/    ← xtsProvider, executeXTSStep   │
│  pkg/providers/  ← XTSProvider, xts.go           │
│  pkg/replay/     ← XTSScenario, xts_replay.go   │
│  pkg/serve/      ← resolveICMInputs, icm.New()   │
│  pkg/icm/        ← OData client, types           │
│  pkg/tools/      ← tool manager                  │
│  pkg/governance/ ← allowlist, redaction           │
└─────────────────────────────────────────────────┘
```

### After (target)

```
┌────────────────────────────────────┐
│          gert core binary          │
│                                    │
│  cmd/gert/                         │
│  pkg/schema/     ← runbook/v0     │   (no XTSMeta, no XTSStepConfig)
│  pkg/runtime/    ← engine          │   (no xtsProvider, no executeXTSStep)
│  pkg/providers/  ← interfaces      │   (no xts.go)
│  pkg/replay/     ← base scenario   │   (no xts_replay.go)
│  pkg/serve/      ← JSON-RPC server│   (no resolveICMInputs, no icm import)
│  pkg/tools/      ← tool manager   │
│  pkg/governance/ ← allowlist       │
│  pkg/inputs/     ← input provider  │   (NEW: generic input dispatch)
│                  manager           │
└────────────────────────────────────┘
         │                    │
         │ tool/v0            │ provider/v0
         ▼                    ▼
┌──────────────┐    ┌──────────────────┐
│  gert-xts    │    │  gert-icm        │
│              │    │                  │
│ xts.tool.yaml│    │ icm-provider     │
│ (stdio/jsonrpc)   │ (jsonrpc binary) │
│              │    │ resolveICMInputs │
│ xts_replay   │    │ icm client       │
│ (bundled)    │    │ field-mapping    │
└──────────────┘    └──────────────────┘
```

## Design: Input Provider Protocol

### The problem today

```go
// pkg/serve/serve.go — handleExecStart, lines 252-281
if params.ICMID != "" && rb.Meta.Inputs != nil {
    icmClient := icm.New()                    // ← hardcoded ICM
    incident, err := icmClient.Get(icmID)     // ← hardcoded ICM API
    resolved := resolveICMInputs(...)          // ← hardcoded ICM logic
}
```

This block needs to become generic. The engine shouldn't care whether inputs
come from ICM, PagerDuty, or a static file.

### The `provider/v0` schema

```yaml
# .gert/providers/icm.provider.yaml
apiVersion: provider/v0

meta:
  name: icm
  description: Microsoft ICM incident input provider
  binary: gert-icm-provider

transport:
  mode: jsonrpc
  startup:
    ready_signal: "ready"
    timeout: 10s
    shutdown_method: "shutdown"

capabilities:
  resolve_inputs:
    prefixes:
      - "icm."            # handles from: icm.*
    context_fields:
      - icmId             # requires icmId in execution context

  enrich:
    methods:
      - icm/getIncident   # on-demand incident fetch
      - icm/getTeam       # team lookup
```

### Input resolution protocol

When the engine starts and finds `meta.inputs` with `from:` bindings, it
dispatches to the matching provider:

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "resolve",
  "params": {
    "bindings": {
      "hostname": { "from": "icm.customFields.ServerName", "pattern": "" },
      "region": { "from": "icm.location.Region" },
      "severity": { "from": "icm.severity" }
    },
    "context": {
      "icmId": "748724360"
    }
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "resolved": {
      "hostname": "sql-prod-west-01.contoso.com",
      "region": "West US 2",
      "severity": "3"
    },
    "warnings": [
      "icm.customFields.DatabaseName: field not found on incident"
    ]
  }
}
```

### The `pkg/inputs/` package (new)

```go
// pkg/inputs/manager.go

// ProviderDef represents a loaded provider/v0 definition.
type ProviderDef struct {
    Name     string
    Binary   string
    Prefixes []string  // which from: prefixes this provider handles
    Context  []string  // what context fields it needs (e.g. icmId)
}

// Manager loads provider definitions and dispatches input resolution.
type Manager struct {
    providers map[string]*ProviderDef
    processes map[string]*jsonrpcProcess  // reuse tools/jsonrpc
}

// Resolve dispatches input bindings to the appropriate provider(s).
// Returns resolved values keyed by input name.
func (m *Manager) Resolve(ctx context.Context, inputs map[string]*schema.InputDef,
    context map[string]string) (map[string]string, []string, error)
```

### Provider dispatch flow

```
1. gert loads .gert/providers/*.provider.yaml
2. For each meta.input with from: "icm.*":
   - Find provider with matching prefix "icm."
   - Batch all icm.* bindings into one resolve call
3. Spawn provider process (or reuse)
4. Send resolve request with bindings + context
5. Merge resolved values into vars
6. Log warnings for unresolvable inputs
```

### Workspace configuration

```yaml
# .gert/config.yaml
providers:
  icm:
    path: ./providers/icm.provider.yaml
    config:
      api_url: https://portal.microsofticm.com
      tenant: microsoft

  pagerduty:
    path: ./providers/pagerduty.provider.yaml
    config:
      api_key_env: PAGERDUTY_API_KEY

tools:
  xts:
    path: ./tools/xts.tool.yaml
  kubectl:
    path: ./tools/kubectl.tool.yaml
```

## Design: XTS as a Tool Definition

### What changes

| Before | After |
|---|---|
| `type: xts` step type | `type: tool` with `tool.name: xts` |
| `meta.xts.environment` | RunA arg or workspace config |
| `schema.XTSMeta` struct | Removed from core schema |
| `schema.XTSStepConfig` struct | Removed — args defined in `xts.tool.yaml` |
| `engine.xtsProvider` field | Removed — tool manager handles execution |
| `engine.executeXTSStep()` | Removed — `executeToolStep` via tool manager |
| `providers.XTSProvider.Execute()` | `gert-xts` binary (stdio or jsonrpc) |
| `replay.XTSScenario` | Tool replay via scenario mock commands |

### The `gert-xts` package

Published as a separate module/binary. Contains:

```
gert-xts/
  tools/
    xts.tool.yaml          # tool definition (query, view, activity actions)
  cmd/
    gert-xts-server/       # optional jsonrpc server mode
      main.go
  pkg/
    xts/                   # moved from gert/pkg/providers/xts.go
      provider.go
      output.go
      replay.go            # moved from gert/pkg/replay/xts_replay.go
  configs/
    environments.yaml      # XTS environment registry
```

### XTS tool definition

```yaml
# gert-xts/tools/xts.tool.yaml
apiVersion: tool/v0
meta:
  name: xts
  version: "2.0"
  description: XTS query execution for Azure Service Fabric
  binary: xts-cli

transport:
  mode: stdio              # or jsonrpc for persistent mode

governance:
  read_only: true

actions:
  query:
    description: Run an ad-hoc query
    argv: ["query", "--type", "{{ .query_type }}", "-e", "{{ .environment }}",
           "-q", "{{ .query }}", "--format", "json"]
    args:
      query_type:
        type: string
        required: true
        enum: [sql, kusto, cms, mds]
      environment:
        type: string
        required: true
      query:
        type: string
        required: true

  view:
    description: Execute an XTS view
    argv: ["execute", "--file", "{{ .file }}",
           "--environment", "{{ .environment }}", "--format", "json"]
    args:
      file:
        type: string
        required: true
      environment:
        type: string
        required: true
      auto_select:
        type: bool
        default: "false"
      sql_timeout:
        type: int
        default: "30"

  activity:
    description: Execute a view activity
    argv: ["execute-activity", "--file", "{{ .file }}",
           "--activity", "{{ .activity }}",
           "--environment", "{{ .environment }}", "--format", "json"]
    args:
      file:
        type: string
        required: true
      activity:
        type: string
        required: true
      environment:
        type: string
        required: true
```

### Runbook migration

```yaml
# Before
apiVersion: runbook/v0
meta:
  name: login-check
  xts:
    environment: ProdAuce1a
  inputs:
    hostname:
      from: icm.customFields.ServerName

tree:
  - step:
      id: query_logins
      type: xts
      xts:
        mode: query
        query_type: kusto
        query: "MonLogin | where ServerName == '{{ .hostname }}' | take 10"
      capture:
        output: stdout

# After
apiVersion: runbook/v0
meta:
  name: login-check
  inputs:
    hostname:
      from: icm.customFields.ServerName        # resolved by icm provider

tools:
  xts: @gert-xts/xts.tool.yaml                 # explicit import

tree:
  - step:
      id: query_logins
      type: tool
      title: Query login data
      tool:
        name: xts
        action: query
        args:
          environment: ProdAuce1a               # was in meta.xts
          query_type: kusto
          query: "MonLogin | where ServerName == '{{ .hostname }}' | take 10"
      capture:
        output: stdout
```

### XTS replay migration

Today: `XTSScenario.FindStepResponse()` returns pre-recorded JSON from
`steps/*.json` files, bypassing `xts-cli` execution.

After: The tool manager uses the standard `ReplayExecutor` which mocks commands
by argv matching. XTS JSON responses become standard scenario command mocks:

```yaml
# scenario.yaml
commands:
  - argv: ["xts-cli", "query", "--type", "kusto", "-e", "ProdAuce1a",
           "-q", "MonLogin | where ServerName == 'sql-prod-01' | take 10",
           "--format", "json"]
    stdout: |
      {"success": true, "rowCount": 3, "data": [...]}
    exit_code: 0
```

This is **already how CLI tool steps are replayed**. XTS steps just become tool
steps with the same replay mechanism.

## Implementation Plan

### Phase A: Extract ICM input resolution (1–2 days)

1. Create `pkg/inputs/manager.go` — `Manager` struct with `LoadProvider`,
   `Resolve`, `Shutdown`
2. Create `pkg/inputs/protocol.go` — request/response types for the resolve protocol
3. Move `resolveICMInputs()` + helpers from `serve.go` into `pkg/icm/resolve.go`
4. Create a **built-in ICM provider** that calls `resolveICMInputs` directly
   (no subprocess — just a Go function wrapped in the provider interface)
5. Update `serve.go` to use `inputs.Manager.Resolve()` instead of direct ICM call
6. Tests: existing `resolve_icm_test.go` tests pass unchanged

**Result:** `pkg/serve` no longer imports `pkg/icm`. Input resolution is generic.

### Phase B: Provider protocol via subprocess (2–3 days)

1. Extract `gert-icm-provider` binary from `pkg/icm/` + `pkg/icm/resolve.go`
2. Implement JSON-RPC `resolve` method in the provider binary
3. Define `provider/v0` YAML schema
4. Update `pkg/inputs/manager.go` to spawn providers as subprocesses
5. Workspace config: `.gert/config.yaml` with provider registration
6. Tests: integration test with mock provider binary

**Result:** ICM is a standalone binary. Other providers can be built.

### Phase C: Complete XTS tool migration (2–3 days)

1. Full engine routing: `type: xts` internally desugars to `type: tool` and
   routes through `ToolManager.Execute()` (not just metadata desugar)
2. XTS replay via standard command mocking (scenario.yaml commands)
3. Remove `engine.xtsProvider`, `engine.XTSScenario`, `engine.executeXTSStep()`
4. Remove `providers.XTSProvider` from `pkg/providers/`
5. Remove `replay.XTSScenario` from `pkg/replay/`
6. Compiler emits `type: tool` for XTS steps
7. Tests: existing XTS scenarios pass with tool routing

**Result:** Engine has no XTS-specific code.

### Phase D: Schema cleanup (1 day)

1. Remove `schema.XTSMeta` from `Meta` struct
2. Remove `schema.XTSStepConfig` from `Step` struct
3. Remove `type: xts` from the Type enum (or keep as alias that desugars)
4. Remove `meta.xts` validation rules
5. Update JSON schema
6. Major version bump: `runbook/v1`

**Result:** Clean schema with only `cli`, `manual`, `invoke`, `tool`.

### Phase E: Separate repositories (1 day)

1. Move `gert-icm-provider` to `github.com/ormasoftchile/gert-icm`
2. Move `xts.tool.yaml` + replay helpers to `github.com/ormasoftchile/gert-xts`
3. Move `configs/icm-field-mapping.yaml` to `gert-icm`
4. Update docs with integration installation instructions
5. Publish provider/tool packages

**Result:** Core gert has zero ICM/XTS imports.

## Dependency Graph

```
Phase A: Extract ICM input resolution
    │
    └──→ Phase B: Provider protocol via subprocess
              │
              └──→ Phase E: Separate repositories (ICM)

Phase C: Complete XTS tool migration (parallel with A/B)
    │
    └──→ Phase D: Schema cleanup
              │
              └──→ Phase E: Separate repositories (XTS)
```

- A → B are sequential (must abstract before extracting)
- C is independent of A/B (can run in parallel)
- D depends on C (schema changes after code removal)
- E depends on B + D (both must be done before splitting repos)

## Compatibility Strategy

### `from: icm.*` bindings

- **Phase A:** Built-in ICM provider wraps existing code. Zero changes needed.
- **Phase B:** External provider binary. Runbooks unchanged — the `from:` syntax
  is the same, only the resolution method changes.
- **No breaking change** — the `from: icm.*` syntax is part of the provider
  contract, not the core schema.

### `type: xts` steps

- **Today:** Deprecation warning in `gert validate`. Steps still execute.
- **Phase C:** Internal desugar routes through tool manager. Steps still execute.
- **Phase D:** `type: xts` removed from schema. This is the **only breaking change**.
  Requires `runbook/v1` apiVersion.
- **Migration tool:** `gert migrate v0-to-v1` rewrites `type: xts` → `type: tool`
  in YAML files.

### Replay/scenario format

- **Phase C:** XTS step JSON responses in `steps/*.json` are converted to
  `scenario.yaml` command mocks via a one-time migration. The scenario format
  doesn't change — new entries are just added to the `commands:` array.
- **Migration tool:** `gert migrate-scenarios --xts` generates command mocks
  from `steps/*.json` files.

## Risk Assessment

| Risk | Impact | Mitigation |
|---|---|---|
| ICM provider subprocess perf | Input resolution slower | Built-in provider first (Phase A); subprocess only in Phase B |
| XTS replay fidelity | Edge cases in JSON → argv mock conversion | Dual-mode period: both paths active during Phase C |
| Teams depend on `meta.xts` | Breaking change in Phase D | Long deprecation window, migration tool, v0 → v1 apiVersion |
| Provider protocol too complex | Nobody builds providers | Keep it simple: one method (`resolve`), JSON-RPC |
| Binary distribution | Teams need to install provider binaries | Bundle common providers with gert initially |

## Success Criteria

- [ ] `pkg/serve` has zero imports from `pkg/icm`
- [ ] `pkg/runtime` has zero XTS-specific fields or methods
- [ ] `pkg/schema` has no `XTSMeta` or `XTSStepConfig`
- [ ] All existing XTS runbook scenarios pass
- [ ] All existing ICM input resolution tests pass
- [ ] At least one non-ICM input provider exists (even if test-only)
- [ ] `gert validate` and `gert test` work on XTS-free runbooks without XTS binary
- [ ] Core gert binary size decreases measurably

## Open Questions

- [ ] Should `from:` prefixes be registered at the schema level or purely at runtime?
- [ ] Should providers support enrichment (on-demand data fetch during execution)?
- [ ] Should `provider/v0` support MCP transport (reuse existing MCP infra)?
- [ ] How do team-specific providers get discovered? PATH? `.gert/config.yaml`? Both?
- [ ] Should `gert init` scaffold a workspace config with default providers?
- [ ] Versioning: bump to `runbook/v1` or use a feature flag on `runbook/v0`?
