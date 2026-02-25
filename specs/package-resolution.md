# Package Resolution Spec

**Status:** Draft  
**Scope:** Scoped Alt 3 — packages with local-only resolution (no registry, no versioning, no transitive deps)

---

## Problem

Today, referencing shared tools and runbooks requires relative filesystem paths (`../../../tools/nslookup.tool.yaml`). This couples runbook content to directory layout, breaks on reorganization, and forces two competing mechanisms (`imports` + `tools` override maps) that confuse authors.

## Goals

1. **Modular** — runbooks, tools, and packages are composable units.
2. **Minimal syntax** — convention over configuration; no file extensions in references.
3. **No relative path hell** — references are logical names, not filesystem paths.
4. **Future-ready** — syntax supports registry/versioning later without breaking changes.

## Non-goals (this phase)

- Remote package registry
- Version constraints or lockfiles
- Transitive dependency resolution
- Package publishing workflow

---

## Concepts

### Project

A directory tree anchored by a `gert.yaml` manifest. A project is also a package (the "root package").

### Package

A self-contained unit containing any combination of:
- Tool definitions (`tools/<name>.tool.yaml`)
- Runbooks (`runbooks/<name>.runbook.yaml` or nested `runbooks/<group>/<name>.runbook.yaml`)
- A manifest (`gert.yaml`)

### Tool definition

A `*.tool.yaml` file (unchanged from today's `tool/v0` schema).

### Runbook

A `*.runbook.yaml` file (unchanged from today's schema, except `imports` is removed and `tools` is simplified).

---

## Manifest: `gert.yaml`

One file, one place. `gert.yaml` is the single configuration surface for a package — identity, dependencies, path conventions, runtime config, and tool exports all live here. There is no separate `.gert/config.yaml`.

```yaml
name: my-tsg                       # required: package identity

paths:                              # optional: override convention directories
  tools: tools                      # default
  runbooks: TSG                     # override (e.g., TSG repo stores runbooks under TSG/)

require:                            # optional: external package dependencies
  gert-tools: ../gert              # name → local path (relative to this gert.yaml)
  dep-pkg: ../dep-pkg              # name → local path
  # dep-pkg: registry:dep/pkg@v2   # future: registry URI (not implemented)

exports:                            # optional: virtual tool aliases exposed to consumers
  tools:
    kusto: queries/kusto-query      # maps logical name "kusto" → tools/queries/kusto-query.tool.yaml
```

**Rules:**
- `name` is the package's own identity. Used as namespace prefix when other packages reference it.
- `paths` overrides convention directories. Defaults: `tools: tools`, `runbooks: runbooks`. Allows repos with non-standard layouts (e.g., TSG repo) to work without restructuring.
- `require` maps logical package names to local filesystem paths.
- The path points to the package root (directory containing `gert.yaml`) or directly to a `tools/` directory (for tool-only packages without their own manifest).
- `exports.tools` maps virtual names to actual paths within the package's tools directory. Consumers use the virtual name; the package controls internal layout. If omitted, all tools are auto-exported by filename.
- `config` holds runtime settings (integrations, environment). Replaces the former `.gert/config.yaml` — everything in one file.
- A project without `require` has no external dependencies; tools/runbooks resolve locally only.

---

## Directory conventions

```
my-tsg/
  gert.yaml
  tools/                          # local tool definitions
    custom-check.tool.yaml
  runbooks/                       # local runbooks
    connectivity/
      dns-check.runbook.yaml
    auth/
      login-failures.runbook.yaml
  scenarios/                      # test scenarios (unchanged)
    dns-check/
      healthy/
      unhealthy/
```

**Convention paths (searched automatically, overridable via `paths:` in manifest):**
- Tools: `<package-root>/<paths.tools>/<name>.tool.yaml` (default: `tools/<name>.tool.yaml`)
- Runbooks: `<package-root>/<paths.runbooks>/<name>.runbook.yaml` or `<package-root>/<paths.runbooks>/<group>/<name>.runbook.yaml` (default: `runbooks/...`)

**Example with TSG repo layout:**
```
TSG-SQL-DB-Connectivity/
  gert.yaml                       # paths.runbooks: TSG
  tools/
    query.tool.yaml
  TSG/
    connection/
      login-failures.runbook.yaml
    alias/
      alias-db-failure.runbook.yaml
```

---

## Runbook syntax changes

### Before (current)

```yaml
apiVersion: runbook/v0

imports:
  - ../shared/reboot-node
  - ../../../tools/nslookup.tool.yaml

tools:
  nslookup: ../../../tools/nslookup.tool.yaml
  # or
  # - nslookup

meta:
  name: dns-check
```

### After (proposed)

```yaml
apiVersion: runbook/v1

tools:
  - nslookup                    # local: my-tsg/tools/nslookup.tool.yaml
  - dep-pkg/query               # package: dep-pkg/tools/query.tool.yaml

meta:
  name: dns-check

tree:
  - step:
      id: resolve
      type: tool
      tool:
        name: nslookup           # matches tools list entry
        action: lookup
      invoke: auth/login-failures           # local runbook
      # invoke: other-pkg/some-runbook      # cross-package runbook
```

**Changes:**
- `imports` is **removed** (replaced by `require` in manifest + qualified names).
- `tools` is **always a list of names** (never a map, never a path).
- Unqualified name → local package.
- `package/name` → tool or runbook from a required package.
- No file extensions anywhere in references.

---

## Resolution algorithm

### Tool resolution

Given a tool reference `<ref>` from a runbook in package `P`:

```
1. Split ref on first "/" → (prefix, name)
2. If no "/" (unqualified):
     → Search P/<paths.tools>/<ref>.tool.yaml
     → If not found: error "tool '<ref>' not found in package '<P.name>'"
3. If "/" (qualified):
     → Look up prefix in P's require map → package Q
     → If Q has exports.tools[name]: resolve via exported virtual path
     → Else: search Q/<Q.paths.tools>/<name>.tool.yaml
     → If not found: error "tool '<name>' not found in package '<prefix>'"
4. If prefix not in require: error "unknown package '<prefix>'"
```

### Runbook resolution (invoke / next_runbook)

Given a runbook reference `<ref>` from a runbook in package `P`:

```
1. Split ref on first "/" → (prefix, rest)
2. If no "/" (unqualified):
     → Search P/runbooks/<ref>.runbook.yaml
     → If not found: error
3. If has "/" but prefix is NOT a known package name:
     → Treat entire ref as a local path: P/runbooks/<ref>.runbook.yaml
     (This handles group paths like "connectivity/dns-check")
4. If prefix IS a known package name:
     → Look up prefix in require → package Q
     → Search Q/runbooks/<rest>.runbook.yaml
```

### Ambiguity rule

If a local runbook group directory shares a name with a required package, the **local path wins**. To force package resolution, use the package's full name explicitly. This should emit a warning during validation.

---

## Backward compatibility

### Migration path

| Current syntax | New syntax | Notes |
|---------------|------------|-------|
| `tools: [nslookup]` | `tools: [nslookup]` | No change if tool is local |
| `tools: {nslookup: ../path}` | Add to `require:`, use `tools: [pkg/nslookup]` | Map form removed |
| `imports: [../shared/reboot]` | `invoke: pkg/reboot` or local convention path | `imports` removed |
| `invoke: {runbook: ../path/file.yaml}` | `invoke: {runbook: group/name}` | Logical name |
| `next_runbook: {file: ../path}` | `next_runbook: {file: group/name}` | Logical name |

### Transition period

For `runbook/v0`:
- Old syntax (`imports`, tool maps, relative paths) continues to work unchanged.
- Resolution falls back to raw filesystem paths when logical resolution fails.

For `runbook/v1`:
- Only the new syntax is accepted.
- `imports` is rejected at parse time.
- `tools` must be a list (map form rejected).

---

## Impact on existing projects

### `gert` (engine + shared tools)

```yaml
# gert/gert.yaml
name: gert-tools     # this package provides shared tool definitions
```

Has `tools/curl.tool.yaml`, `tools/nslookup.tool.yaml`, `tools/ping.tool.yaml`. No runbooks to reference externally.

### `dep-pkg` (query tool)

```yaml
# dep-pkg/gert.yaml
name: dep-pkg
```

Has `tools/query.tool.yaml`. Runbooks that need the query tool reference `dep-pkg/query`.

### `TSG-SQL-DB-Connectivity` (consumer)

```yaml
# TSG-SQL-DB-Connectivity/gert.yaml
name: sql-connectivity

paths:
  runbooks: TSG                    # runbooks live under TSG/, not runbooks/

require:
  gert-tools: ../gert              # for curl, nslookup, ping
  dep-pkg: ../dep-pkg              # for query tool
```

Runbook references become:
```yaml
tools:
  - dep-pkg/query
  - gert-tools/curl
```

---

## What `gert.yaml` is NOT

- Not a lockfile (no checksums, no resolved versions).
- Not a build config (no build steps, no output paths).

`gert.yaml` is a **project manifest** — it names the package, declares its dependencies, and holds runtime configuration. One file, one place.

---

## Engine changes required

| Component | Change |
|-----------|--------|
| `pkg/schema` | New `Project` type; parse `gert.yaml` (including `paths`, `exports`, `config`); remove `imports` from v1; `tools` always `[]string` |
| `pkg/schema` | `ResolveToolPath` → takes project context, applies resolution algorithm with `paths` + `exports` |
| `pkg/schema/validate.go` | Validate tool/runbook refs against project + required packages |
| `pkg/runtime/engine.go` | Project-aware resolution for invoke/next_runbook; respects `paths.runbooks` |
| `cmd/gert/main.go` | Walk up from runbook to find `gert.yaml`; build project context; load `config` section for integrations |
| `pkg/serve`, `pkg/testing` | Pass project context through |
| JSON schemas | v1: remove `imports`, restrict `tools` to array-of-string |
| VS Code extension | Schema update; optional: autocomplete tool/runbook names from project |
| Migration | Remove `.gert/config.yaml` support; migrate existing configs into `gert.yaml` `config:` section |

---

## Resolved questions

1. **`paths` override** — Yes. `gert.yaml` supports `paths.tools` and `paths.runbooks` to override convention directories. Defaults: `tools`, `runbooks`.

2. **Flat tool namespace** — Yes, filesystem stays flat within `tools/`. Packages expose virtual names to consumers via `exports.tools` in their manifest. Internal directory structure is the package author's concern.

3. **No-manifest fallback** — Yes. When no `gert.yaml` is found, the engine treats the runbook's own directory as the package root with local-only resolution. This keeps standalone runbooks and `testdata/` fixtures working without ceremony.

4. **Unified manifest** — Yes. Everything lives in `gert.yaml` — identity, dependencies, paths, exports, and runtime config. The former `.gert/config.yaml` is merged into the `config:` section. One file, one place.

