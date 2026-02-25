# Phase 0 Research: Governed Executable Runbook Engine v0

**Branch**: `001-runbook-engine-v0` | **Date**: 2026-02-11 | **Plan**: [plan.md](plan.md)

---

## Topic 1: Strict YAML Parsing — `gopkg.in/yaml.v3` with `KnownFields(true)`

### Decision

Use `gopkg.in/yaml.v3` with `Decoder.KnownFields(true)` for strict YAML deserialization into Go structs. This rejects any YAML key that does not map to a struct field.

### Rationale

- **`KnownFields(true)`** causes the decoder to return an error when a YAML key has no corresponding struct field (or `yaml` tag). This directly satisfies FR-001 (reject unknown fields) without a separate validation pass.
- **API**: `yaml.NewDecoder(reader)` → `dec.KnownFields(true)` → `dec.Decode(&target)`. The decoder returns `*yaml.TypeError` with per-field error messages including line/column context.
- **Struct tags**: Use `yaml:"fieldname"` and `yaml:"fieldname,omitempty"` to control mapping. Required-field enforcement is not built into yaml.v3 — that must be handled by schema validation (Topic 3).
- **Anchor/alias support**: yaml.v3 handles YAML anchors and aliases natively, which matters if runbooks use YAML references.
- **Performance**: Single-pass decode + reject. No intermediate `map[string]interface{}` needed.
- **Stability**: yaml.v3 is the canonical Go YAML library (canonical import path `gopkg.in/yaml.v3`), actively maintained, 7.4k+ stars.

### Alternatives Considered

| Alternative | Why Not |
|---|---|
| `sigs.k8s.io/yaml` | Wrapper around `encoding/json` + yaml.v2. Converts YAML → JSON → Go struct. Supports `DisallowUnknownFields` (via `encoding/json`), but loses YAML-specific features (anchors, comments, multi-doc). Adds indirection. |
| `github.com/goccy/go-yaml` | Faster in benchmarks, supports `yaml.DisallowUnknownField()` as a decode option. Less widely adopted (1.2k stars vs 7.4k). API is less conventional. Viable alternative if performance bottlenecks appear, but unlikely for sub-500-step runbooks. |
| Manual validation via `map[string]interface{}` | Decode to generic map, then diff keys against a known set. Works but loses type safety, requires manual type coercion, and duplicates what `KnownFields` provides for free. |

---

## Topic 2: JSON Schema Generation from Go Structs — `github.com/invopop/jsonschema`

### Decision

Use `github.com/invopop/jsonschema` (v0.13.0) to generate the canonical JSON Schema from Go struct definitions. The generated schema is written to `schemas/runbook-v0.json` and shared with the TypeScript VS Code extension.

### Rationale

- **Draft 2020-12**: Generates JSON Schema Draft 2020-12 by default, which is the latest standard and aligns with the project's target.
- **Reflection-based**: Uses Go reflection to walk struct fields. Reads `json` struct tags for property names (not `yaml` tags — yaml tags were removed in recent versions). This means Go structs need both `yaml` and `json` tags, or a `KeyNamer` function to map names.
- **Struct tag constraints**: The `jsonschema` struct tag supports: `title`, `description`, `enum`, `pattern`, `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `multipleOf`, `minLength`, `maxLength`, `minItems`, `maxItems`, `uniqueItems`, `minProperties`, `maxProperties`, `oneof_required`, `anyof_required`, `oneof_ref`, `oneof_type`. Example: `jsonschema:"title=Step Type,enum=cli:manual:group"`.
- **Custom schema hooks**: Types implementing `JSONSchema() *Schema` or `JSONSchemaExtend(schema *Schema)` can provide/modify their own schema definitions — useful for union types like `StepDefinition`.
- **`additionalProperties: false`**: Controlled via `Reflector.AllowAdditionalProperties`. Default is `false` (disallows additional properties), which aligns with strict validation requirements.
- **Go comments as descriptions**: `Reflector.AddGoComments(base, path)` extracts Go source comments and injects them as `description` fields in the schema — useful for self-documenting schemas.
- **Adoption**: 900+ stars, 1063 importers on pkg.go.dev.

### Key Consideration: YAML ↔ JSON Field Name Alignment

Since `invopop/jsonschema` reads `json` tags (not `yaml` tags), the Go structs must use matching `json` and `yaml` tags:

```go
type Step struct {
    ID   string `yaml:"id" json:"id" jsonschema:"required"`
    Type string `yaml:"type" json:"type" jsonschema:"required,enum=cli:manual:group"`
}
```

Alternatively, use `Reflector.KeyNamer` to transform field names, but dual tags are clearer and more explicit.

### Alternatives Considered

| Alternative | Why Not |
|---|---|
| Hand-written JSON Schema | Maximum control but high maintenance burden. Schema and Go structs drift. Every struct change requires manual schema update. |
| `github.com/alecthomas/jsonschema` | Older, less maintained predecessor. `invopop/jsonschema` is its actively maintained successor with Draft 2020-12 support. |
| `github.com/swaggest/jsonschema-go` | Generates OpenAPI-flavored JSON Schema. More opinionated, targets API documentation. Less suitable for standalone schema validation. |

---

## Topic 3: Go-side JSON Schema Validation — `github.com/santhosh-tekuri/jsonschema/v6`

### Decision

Use `github.com/santhosh-tekuri/jsonschema/v6` (v6.0.2) for validating runbook data against the generated JSON Schema at runtime.

### Rationale

- **Full draft support**: Passes the official JSON-Schema-Test-Suite for Draft-04, -06, -07, 2019-09, and 2020-12. This ensures correct validation behavior for Draft 2020-12 schemas generated by `invopop/jsonschema`.
- **API flow**: `NewCompiler()` → `c.AddResource(url, doc)` → `c.Compile(schemaURL)` → `schema.Validate(value)`. The compiled schema is reusable — compile once at startup, validate many runbooks.
- **Rich error reporting**: `ValidationError` provides `SchemaURL`, `InstanceLocation` (JSON Pointer into the document), `ErrorKind` (typed error interface), and `Causes` (recursive child errors). This enables precise error messages like "field 'steps[2].argv' is required" with location context.
- **Output formats**: Supports `flag`, `basic`, and `detailed` output formats per JSON Schema specification, enabling both quick pass/fail checks and detailed error reports.
- **Custom vocabulary support**: Can register custom keywords and vocabulary — useful if the project needs governance-specific schema extensions.
- **Format assertions**: Supports format validation (e.g., `date-time`, `uri`, `regex`) with opt-in enforcement.
- **No external dependencies**: Pure Go implementation.
- **Adoption**: 1.2k stars, 140 importers.

### Validation Pipeline Design

```
YAML file → yaml.v3 Decode (KnownFields) → Go struct → json.Marshal → JSON value → jsonschema.Validate
```

The two-phase approach (yaml.v3 strict decode + JSON Schema validation) provides defense-in-depth: yaml.v3 catches unknown fields at the YAML level, and JSON Schema validation enforces types, required fields, patterns, enums, and cross-field constraints.

### Alternatives Considered

| Alternative | Why Not |
|---|---|
| `github.com/xeipuuv/gojsonschema` | Popular (3.2k stars) but only supports up to Draft-07. No Draft 2020-12 support. Also unmaintained since 2023. |
| `github.com/qri-io/jsonschema` | Supports Draft-07 and 2019-09. No Draft 2020-12 support. Less active. |
| Manual validation in Go code | Loses schema-as-data benefit. Cannot share validation rules with TypeScript. Every rule change requires code changes in both stacks. |

---

## Topic 4: Variable/Template Resolution — Go `text/template`

### Decision

Use Go's standard library `text/template` with `Option("missingkey=error")` for resolving `{{ .varName }}` expressions in step definitions (argv, instructions, assertions).

### Rationale

- **Zero dependencies**: Part of Go's standard library. No external dependency needed.
- **`missingkey=error`**: `template.New("").Option("missingkey=error")` causes execution to return an error when a template references an undefined variable. This directly satisfies FR-005 (detect undefined variable references) and enables schema-time and runtime variable checks.
- **`FuncMap`**: Custom functions can be registered via `template.FuncMap` — useful for potential future built-in functions (e.g., `{{ env "NAMESPACE" }}`, `{{ capture "prev_step" "stdout" }}`).
- **Security model**: `text/template` (not `html/template`) is appropriate because template authors are trusted (runbook authors are on-call engineers). No auto-escaping needed.
- **Delimiter customization**: `template.New("").Delims("{{", "}}")` — default delimiters match the spec's template syntax. Can be changed if conflicts arise with YAML or shell syntax.
- **Thread safety**: Templates are safe for concurrent execution after parsing (relevant if future versions support parallel steps).
- **Parseability**: Templates can be parsed without execution via `template.New("").Parse(text)` — this enables static analysis of variable references during schema validation (FR-005) without needing runtime values.

### Template Variable Extraction Pattern

For static validation (detecting undefined variables before execution):

```go
t, err := template.New("").Option("missingkey=error").Parse(expr)
// Walk t.Tree.Root to extract {{.varName}} references
// Compare against declared vars in meta.vars
```

### Alternatives Considered

| Alternative | Why Not |
|---|---|
| `github.com/Masterminds/sprig` | Function library for `text/template`. Adds 100+ functions (string manipulation, math, crypto). Overkill for v0 — but can be added later via `FuncMap` if needed. Not a template engine itself. |
| `github.com/valyala/fasttemplate` | Simpler `{{varName}}` substitution (no dot prefix). Faster but no control flow, no `missingkey` option, no AST walking for static analysis. Too limited. |
| Shell-style `$VAR` or `${VAR}` substitution | Conflicts with shell interpolation in `argv` fields. Would require escaping complexity. Go template syntax `{{ .var }}` is visually distinct from shell variables. |
| `github.com/flosch/pongo2` | Django/Jinja2-style templates. More powerful (filters, inheritance) but heavier, non-standard in Go ecosystem, and unnecessary for simple variable substitution. |

---

## Topic 5: Interactive CLI REPL — `github.com/chzyer/readline`

### Decision

Use `github.com/chzyer/readline` (v1.5.1) for the interactive debugger REPL, providing line editing, history, and tab completion.

### Rationale

- **Mature and battle-tested**: Used by CockroachDB, otto, usql, and other production CLIs. Handles cross-platform terminal differences.
- **Features needed for debugger**:
  - **Line editing**: Arrow keys, Home/End, word-level movement — expected UX for a REPL.
  - **History**: `HistoryFile` persists command history across sessions. `HistoryLimit` controls size.
  - **Tab completion**: `AutoComplete` interface for dynamic completions. Can complete debugger commands (`next`, `print`, `vars`, `captures`, `history`, `evidence`, `snapshot`, `approve`, `quit`) and subcommands.
  - **Prompt customization**: Supports ANSI-colored prompts (e.g., `gert[step 3/10]> `).
  - **Password input**: `readline.ReadPasswordEx()` for secure evidence input if needed.
- **API simplicity**: `readline.NewEx(&Config{...})` → `rl.Readline()` in a loop. Clean lifecycle.
- **Vi mode**: Optional Vi key bindings via `VimMode: true` — nice for power users.

### Alternatives Considered

| Alternative | Why Not |
|---|---|
| `github.com/c-bata/go-prompt` (v0.2.6) | Rich dropdown completion UI with fuzzy filtering. More visually impressive but heavier — pulls in VT100 rendering, has known issues with terminal resize. Better suited for dedicated TUI apps than a debugger REPL. |
| `bufio.Scanner` (stdlib) | Zero dependencies but no line editing, no history, no completion. Unacceptable UX for an interactive debugger. |
| `github.com/peterh/liner` | Similar to readline. Fewer features (no Vi mode, less active). readline is more widely adopted. |
| `github.com/charmbracelet/bubbletea` | Full TUI framework (Elm architecture). Extremely powerful but architectural overkill for a simple REPL. Would constrain the debugger to a specific UI paradigm. Consider only if the debugger evolves into a full-screen TUI. |

---

## Topic 6: `cli-replay` Integration Pattern

### Decision

Integrate with `cli-replay` via a **command executor interface** (strategy pattern). The runtime defines a `CommandExecutor` interface; the real executor runs commands via `os/exec`, and the replay executor routes commands through `cli-replay`.

### Rationale

- **Interface design**:

  ```go
  type CommandExecutor interface {
      Execute(ctx context.Context, cmd string, args []string, env []string) (stdout, stderr []byte, exitCode int, err error)
  }
  ```

- **Two implementations**:
  1. `RealExecutor` — wraps `os/exec.CommandContext`. Used in normal execution mode.
  2. `ReplayExecutor` — invokes `cli-replay match` or uses `cli-replay` as a library to look up the matching scenario response for the given command+args. Used in replay mode.

- **Injection**: The runtime engine receives a `CommandExecutor` at construction time. Mode selection (real vs. replay) happens at CLI flag parsing (`--replay scenario.yaml`), not inside the engine. This keeps the engine mode-agnostic.

- **Scenario file parsing**: The replay executor loads the scenario YAML file (same format as `cli-replay` scenarios) at startup. Each CLI step's command+args are matched against scenario entries. If no match is found, the executor returns an error (fail-closed, per constitution principle IV).

- **Manual step replay**: For replay mode, manual step evidence is also pre-populated from the scenario file. The manual provider checks for pre-recorded evidence before prompting.

- **Determinism guarantee**: The replay executor is stateless per invocation — same input always produces same output. Combined with the sequential state machine (FR-007), this guarantees deterministic replay (FR-028).

### Alternatives Considered

| Alternative | Why Not |
|---|---|
| Environment variable switching (`GERT_MODE=replay`) | Implicit coupling. The engine would need to check the env var internally. Less testable than constructor injection. |
| Separate binary for replay | Duplicates the runtime. Maintenance burden. The executor interface achieves the same separation without code duplication. |
| Mock `os/exec` via `PATH` manipulation | Fragile. Depends on filesystem state. Difficult to test. `cli-replay` already solves this properly. |
| Record/playback proxy (man-in-the-middle) | Adds network complexity. `cli-replay` with scenario files is simpler and fully deterministic. |

---

## Topic 7: JSONL Trace Writing

### Decision

Use `encoding/json` with a buffered file writer (`bufio.Writer`) to append one JSON object per line to the trace file. Flush after each step completion. Use `os.File.Sync()` at critical boundaries.

### Rationale

- **JSONL format** (JSON Lines): One JSON object per line, newline-delimited. Simple, streamable, appendable, grep-friendly. Standard format for structured event logs.
- **Write pattern**:

  ```go
  type TraceWriter struct {
      file *os.File
      buf  *bufio.Writer
      enc  *json.Encoder
  }

  func (tw *TraceWriter) WriteEvent(event TraceEvent) error {
      if err := tw.enc.Encode(event); err != nil { // Encode writes JSON + newline
          return err
      }
      if err := tw.buf.Flush(); err != nil {
          return err
      }
      return tw.file.Sync() // fsync for durability
  }
  ```

- **`json.Encoder`**: `json.NewEncoder(writer).Encode(v)` writes JSON followed by a newline — exactly JSONL format. No manual newline handling needed.
- **Buffered I/O**: `bufio.Writer` reduces syscall overhead. Flush after each event ensures the trace is up-to-date even if the process crashes.
- **`Sync()` for durability**: `os.File.Sync()` issues an fsync to ensure data is persisted to disk. Called after each step completion to prevent trace loss on crash. Can be made configurable (`--trace-sync=step|run`) if performance is a concern.
- **File path**: `.runbook/runs/<run_id>/trace.jsonl` per the project structure.
- **Append mode**: Open with `os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)` to support resumption (FR-009) — new events are appended to the existing trace file.
- **No external dependencies**: Pure stdlib (`encoding/json`, `bufio`, `os`).

### Trace Event Schema

```go
type TraceEvent struct {
    Timestamp   time.Time       `json:"timestamp"`
    EventType   string          `json:"event_type"` // "step_start", "step_result", "snapshot", "error"
    StepIndex   int             `json:"step_index"`
    StepID      string          `json:"step_id"`
    Status      string          `json:"status,omitempty"`   // "success", "failure", "skipped"
    Duration    time.Duration   `json:"duration,omitempty"`
    Detail      json.RawMessage `json:"detail,omitempty"`   // step-type-specific payload
}
```

### Alternatives Considered

| Alternative | Why Not |
|---|---|
| SQLite for trace storage | Structured queries are nice but overkill for append-only event logs. Adds CGo dependency (or pure-Go SQLite which is slower). JSONL is human-readable and trivially parseable. |
| Protocol Buffers (binary) | Compact but not human-readable. Adds protobuf toolchain dependency. JSONL is inspectable with `cat`, `jq`, `grep`. |
| `log/slog` structured logger | Go 1.21+ structured logging. Could work but is designed for application logs, not domain event streams. Mixing concerns. Also harder to parse back into typed events. |
| CSV/TSV | Not suitable for nested/complex event data. No standard for nested objects. |

---

## Topic 8: TypeScript JSON Schema Validation — `ajv`

### Decision

Use `ajv` (v8.17.1) for JSON Schema validation in the TypeScript VS Code extension, validated against the same `schemas/runbook-v0.json` that the Go CLI uses.

### Rationale

- **Industry standard**: 184M+ weekly npm downloads. The most widely used JSON Schema validator in the JavaScript/TypeScript ecosystem.
- **Draft 2020-12 support**: ajv v8 supports Draft 2020-12 natively — matching the schema generated by `invopop/jsonschema` on the Go side.
- **TypeScript types**: Ships with built-in TypeScript type definitions. Full type safety in the VS Code extension.
- **Compilation model**: `ajv.compile(schema)` produces an optimized validation function. Compile once at extension activation, reuse for all document validations.
- **Error reporting**: Returns detailed errors with `instancePath` (JSON Pointer), `schemaPath`, `keyword`, `message`, and `params`. Maps cleanly to VS Code Diagnostics API for in-editor error highlighting.
- **API**:

  ```typescript
  import Ajv from "ajv";
  const ajv = new Ajv({ allErrors: true });
  const validate = ajv.compile(runbookSchema);
  const valid = validate(parsedYaml);
  if (!valid) {
      // validate.errors contains detailed error objects
  }
  ```

- **Browser + Node.js**: Works in both environments. VS Code extension host is Node.js-based, so this is straightforward.
- **Schema contract parity**: Using the same JSON Schema file (`schemas/runbook-v0.json`) in both Go (`santhosh-tekuri/jsonschema`) and TypeScript (`ajv`) ensures identical accept/reject decisions — satisfying constitution principle VI (Dual-Stack Contract Parity).

### Alternatives Considered

| Alternative | Why Not |
|---|---|
| `@hyperjump/json-schema` | Spec-compliant, supports all drafts. Much smaller community (< 1k weekly downloads vs 184M). Less battle-tested. |
| `zod` | TypeScript-first schema library. Excellent DX but defines schemas in TypeScript code, not JSON Schema. Cannot share schema with Go side. Violates dual-stack contract parity. |
| `joi` | Hapi ecosystem validator. No JSON Schema support — uses its own schema DSL. Same sharing problem as zod. |
| `jsonschema` (npm) | Simpler API but fewer features, Draft-07 max, much lower adoption. |
| VS Code built-in JSON validation | VS Code has built-in JSON Schema validation for JSON files (via `json.schemas` setting). Does not work for YAML files without an extension. Also provides no programmatic API for the extension to use. |

---

## Topic 9: Markdown Parsing in Go — `github.com/yuin/goldmark`

### Decision

Use `github.com/yuin/goldmark` (v1.7.16) for parsing Markdown TSGs into an AST during the compilation phase (TSG → runbook.yaml).

### Rationale

- **CommonMark compliant**: Passes the CommonMark spec tests. Predictable, standards-based parsing behavior.
- **AST-based**: Parses to a full AST with typed nodes (`ast.Heading`, `ast.FencedCodeBlock`, `ast.Paragraph`, `ast.List`, `ast.ListItem`, etc.). This enables structured extraction of:
  - **Headings** → Step boundaries and group structure
  - **Fenced code blocks** → CLI step `argv` candidates
  - **Paragraphs** → Manual step instructions
  - **Lists** → Checklist evidence items
- **Source positions**: AST nodes retain byte offsets into the source, enabling the mapping report (mapping.md) to reference exact TSG locations.
- **Extension ecosystem**: Supports GFM (GitHub Flavored Markdown) tables, footnotes, definition lists, and critically:
  - `goldmark-frontmatter` — parses YAML frontmatter (useful if TSGs have metadata headers)
  - `goldmark-toc` — extracts table of contents (useful for structural overview)
- **Pure Go**: No CGo, no external dependencies.
- **Adoption**: 4.6k stars, 1774 importers on pkg.go.dev. Used by Hugo, Gitea, and other major projects.
- **API for compiler**:

  ```go
  import (
      "github.com/yuin/goldmark"
      "github.com/yuin/goldmark/ast"
      "github.com/yuin/goldmark/text"
  )

  source := []byte(tsgMarkdown)
  reader := text.NewReader(source)
  parser := goldmark.DefaultParser()
  doc := parser.Parse(reader)

  // Walk the AST
  ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
      if !entering { return ast.WalkContinue, nil }
      switch n := node.(type) {
      case *ast.Heading:
          // Extract heading text → step name / group boundary
      case *ast.FencedCodeBlock:
          // Extract code → CLI step argv candidate
      case *ast.Paragraph:
          // Extract prose → manual step instructions
      }
      return ast.WalkContinue, nil
  })
  ```

### Alternatives Considered

| Alternative | Why Not |
|---|---|
| `github.com/gomarkdown/markdown` | AST-based, supports CommonMark + extensions. Less actively maintained. API is less ergonomic — uses a single `Node` type with type assertions rather than typed node structs. goldmark's typed AST is cleaner for structured extraction. |
| `github.com/russross/blackfriday/v2` | Fast, popular (5.5k stars). But renders HTML directly — not AST-oriented. Extension support is via callbacks during rendering, not tree walking. Poor fit for structured extraction. |
| `github.com/charmbracelet/glamour` | Markdown *rendering* library (terminal-formatted output). Not a parser — wraps goldmark internally. Wrong tool for extraction. |
| Regex-based Markdown parsing | Fragile. Markdown has too many edge cases (nested lists, indented code blocks, setext headings). Guaranteed to break on real-world TSGs. |
| `pandoc` via subprocess | Extremely powerful but requires external binary installation. Adds runtime dependency. Overkill when goldmark provides sufficient AST access. |

---

## Cross-Cutting Observations

### Schema Sharing Pipeline

```
Go structs (yaml + json + jsonschema tags)
    ↓ invopop/jsonschema (Topic 2)
schemas/runbook-v0.json (Draft 2020-12)
    ↓                          ↓
santhosh-tekuri/jsonschema     ajv (Topic 8)
(Go validation, Topic 3)      (TS validation)
```

This pipeline ensures a single source of truth (Go structs) with deterministic schema generation and cross-stack validation parity.

### Two-Phase Validation (Go CLI)

```
Phase 1: yaml.v3 + KnownFields(true)  → Rejects unknown YAML keys (structural)
Phase 2: JSON Schema validation        → Enforces types, required, enums, patterns (semantic)
Phase 3: Custom Go validation          → Governance rules, cross-field logic, variable resolution checks
```

### Dependency Summary

| Component | Dependency | Version | Purpose |
|---|---|---|---|
| YAML parsing | `gopkg.in/yaml.v3` | v3.0.1 | Strict YAML decode |
| Schema generation | `github.com/invopop/jsonschema` | v0.13.0 | Go structs → JSON Schema |
| Schema validation (Go) | `github.com/santhosh-tekuri/jsonschema/v6` | v6.0.2 | Validate against JSON Schema |
| Template resolution | `text/template` (stdlib) | — | Variable substitution |
| CLI REPL | `github.com/chzyer/readline` | v1.5.1 | Interactive debugger |
| Trace writing | `encoding/json` + `bufio` (stdlib) | — | JSONL event log |
| Schema validation (TS) | `ajv` | v8.17.1 | VS Code extension validation |
| Markdown parsing | `github.com/yuin/goldmark` | v1.7.16 | TSG compilation |
| CLI framework | `github.com/spf13/cobra` | (per plan) | Command structure |
