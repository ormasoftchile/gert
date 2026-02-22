# gert — Runbook Authoring, Testing & Adoption Guide

## Vision

The runbook YAML is the single source of truth for operational procedures. It is both executable (runs in the VS Code extension with live Kusto queries) and publishable (renders to human-readable TSG documentation). No more drift between what the docs say and what the tool does.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                    Runbook YAML                         │
│              (single source of truth)                   │
├─────────────┬──────────────┬────────────────────────────┤
│  gert run   │ gert render  │  gert test                 │
│  (execute)  │ (publish)    │  (validate)                │
├─────────────┼──────────────┼────────────────────────────┤
│  VS Code    │  eng.ms      │  CI / PR gates             │
│  Extension  │  Markdown    │  Replay scenarios          │
└─────────────┴──────────────┴────────────────────────────┘
```

---

## 1. Authoring Experience

### 1.1 Schema-Aware YAML Editing

The runbook YAML schema provides autocomplete, validation, and documentation directly in VS Code via the YAML extension (`redhat.vscode-yaml`).

**Setup** — add to `.vscode/settings.json`:
```json
{
  "yaml.schemas": {
    "./schemas/runbook.schema.json": "*.runbook.yaml"
  }
}
```

**What you get:**
- Autocomplete for all fields (`type:` → `xts`, `manual`, `cli`)
- Red squiggles for unknown fields or wrong types
- Hover documentation for every field
- Snippet insertion for common patterns

### 1.2 Live Preview While Editing

The TSG prose panel renders the runbook as a formatted document (like eng.ms) in real-time. When editing a `.runbook.yaml`:

1. Open the runbook in the text editor (left panel)
2. The TSG prose panel (right) shows the rendered TSG
3. On save, the preview updates automatically

This gives immediate visual feedback on how the prose will look when published.

### 1.3 Runbook Structure

```yaml
apiVersion: runbook/v0
meta:
    name: my-tsg-name
    kind: mitigation                    # mitigation | reference | composable | rca
    description: One-line summary

    # ── Inputs: what data is needed ───────────────────
    inputs:
        server_name:
            from: icm.customFields.ServerName
            description: Logical SQL server name
        database_name:
            from: icm.customFields.DatabaseName
            description: Database name

    # ── XTS configuration ─────────────────────────────
    xts:
        environment: '{{ .environment }}'

    # ── Prose: human-readable documentation ───────────
    prose:
        safety: |
            When using CAS commands, refer to the R2D certified CAS TSG.
        background: |
            This alert indicates that the login success rate is below 99%.
            It evaluates every 5 minutes...
        prerequisites: |
            - Server and Database names
            - Login failure cause
        post_mitigation: |
            Verify login success rate has recovered above 99%.
        references:
            - title: Availability Alerts Code Repository
              url: https://msdata.visualstudio.com/...
        ownership:
            team: Gateway SREs
            email: gatewaysres@microsoft.com

    # ── Governance: safety rules ──────────────────────
    governance:
        allowed_commands:
            - Get-FabricNode
            - Kill-Process

# ── Execution tree ────────────────────────────────────
tree:
    - step:
        id: query_login_failures
        type: xts
        title: Query login failures via Kusto
        xts:
            mode: query
            query_type: kusto
            query: |
                MonLogin
                | where TIMESTAMP > ago(4h)
                | where logical_server_name =~ "{{ .server_name }}"
                ...
        capture:
            failure_row_count: row_count
        outcomes:
            - when: 'failure_row_count == "0"'
              state: no_action
              recommendation: Mitigate as transient.
      branches:
        - condition: 'failure_row_count != "0"'
          label: Failures detected
          steps:
            - step:
                id: next_step
                ...
```

### 1.4 Expression Syntax for Conditions

Conditions in `when:` and `condition:` fields use [expr-lang](https://expr-lang.org/) — a clean, readable expression language. No Go template syntax needed.

**Comparison:**
```yaml
condition: 'login_failure_cause == "HasDumps"'
condition: 'failure_row_count != "0"'
when: 'failure_row_count == "0"'
```

**String operations:**
```yaml
condition: 'login_failure_cause startsWith "LoginErrorsFound"'
condition: 'top_app_type contains "MySQL"'
condition: 'login_failure_cause endsWith "_Master"'
```

**Set membership:**
```yaml
condition: 'login_failure_cause in ["HasDumps", "HasIoError", "HasTDEErrors"]'
condition: 'top_app_type in ["Gateway.PDC", "Worker.CL"]'
```

**Boolean logic:**
```yaml
condition: 'failure_row_count != "0" && top_app_type contains "MySQL"'
condition: '!(login_failure_cause startsWith "LoginErrorsFound")'
condition: 'login_failure_cause in ["IsUndoOfRedoInterrupted", "IsFailoverDueToPLBConstraints"]'
```

**Complex guard (escalation fallback):**
```yaml
when: '!(login_failure_cause startsWith "LoginErrorsFound") && !(login_failure_cause in ["HasDumps", "HasIoError", "IsUnplacedReplica"])'
```

> **Note:** Go template syntax `{{ eq .var "value" }}` is still supported for backwards compatibility but should not be used in new runbooks. The `{{ .var }}` syntax is still used for **string interpolation** in queries and instructions — that's the correct use.

**Full reference:** [expr-lang documentation](https://expr-lang.org/docs/language-definition)

### 1.5 Imports and Tools Declaration Styles

Runbooks can be authored with convention-first shorthand or explicit verbose declarations.

**Minimal shorthand (convention-based):**

```yaml
imports: dns-check
tools: [curl, nslookup]
```

**Equivalent expanded form:**

```yaml
imports:
  dns-check: ../dns-check/dns-check.runbook.yaml

tools:
  - curl
  - nslookup
```

**Verbose overrides (when defaults are not enough):**

```yaml
imports:
  - name: dns-check
    path: ../shared/dns-check.runbook.yaml

tools:
  curl:
    path: custom/tools/curl.tool.yaml
  nslookup: tools/nslookup.tool.yaml
```

**Resolution rules:**
- If `imports` path is omitted, gert resolves `../<alias>/<alias>.runbook.yaml`.
- If tool path is omitted, gert resolves `tools/<name>.tool.yaml`.
- All supported forms normalize internally to canonical `imports` and `tools`.

---

## 2. Publishing: `gert render`

Generate a human-readable markdown TSG from a runbook:

```
gert render my-tsg.runbook.yaml --out my-tsg.md
```

**What it produces:**
- Title and background from `meta.prose.background`
- Safety warnings from `meta.prose.safety`
- Triage section from XTS query steps (Kusto code blocks included)
- Mitigation section from manual/routing steps
- Post-mitigation from `meta.prose.post_mitigation`
- Escalation from outcome recommendations
- References and ownership from `meta.prose`

**JIT rendering:** No need to persist `.md` files. Render on demand:
- eng.ms build pipeline calls `gert render` at publish time
- The VS Code extension renders in the TSG prose panel
- CI generates previews for PR review

---

## 3. Scenario Management

### 3.1 Creating Scenarios

**From an ICM incident (recommended):**
```
gert scenario capture 749083743 --runbook login-success-rate-below-target.runbook.yaml
```

This:
1. Fetches the ICM via API
2. Extracts inputs (ServerName, DatabaseName, environment, LoginFailureCause)
3. Creates `scenarios/icm-749083743/inputs.yaml`
4. Adds it to the runbook's `scenarios:` section

**From the extension:** Run a TSG, then click "Save for Replay" — the scenario is saved automatically with inputs and step responses.

**Bulk capture from ICM:**
```
gert scenario bulk --team "SQL DB Availability" --days 30 --runbook login-success-rate-below-target.runbook.yaml
```

### 3.2 Scenario Structure

Scenarios are discovered by convention — no configuration needed in the runbook.

```
connection/
  login-success-rate-below-target.runbook.yaml
  scenarios/
    login-success-rate-below-target/     ← matches runbook name
      icm-748724360/
        inputs.yaml
        steps/                           ← saved Kusto responses (for replay)
          query_login_failures.json
      icm-749083743/
        inputs.yaml
```

The engine discovers scenarios at: `scenarios/{runbook-name}/*/inputs.yaml`

**inputs.yaml:**
```yaml
# ICM 748724360 — LoginErrorsFound_40613_127
# State: ACTIVE | Region: Germany West Central (ProdGeWc1a)
server_name: "spartan-srv-ger-bcprodweu-919dae62a58b"
database_name: "db_bcprodweu_t47243237_20251118_04355555_318e"
environment: "ProdGeWc1a"
login_failure_cause: "LoginErrorsFound_40613_127"
```

### 3.3 Running Scenarios

**Replay mode (offline, deterministic):**
```
gert run --replay --scenario icm-748724360 login-success-rate-below-target.runbook.yaml
```

**Live mode with saved inputs (online, real Kusto):**
```
gert run --scenario icm-748724360 login-success-rate-below-target.runbook.yaml
```

---

## 4. Testing & Validation

### 4.1 Structural Validation

```
gert validate my-tsg.runbook.yaml
```

Checks:
- YAML schema compliance
- Unique step IDs
- Variable flow: every `{{ .var }}` is either an input, a capture from a prior step, or a meta.vars entry
- Dangling references: every `next_runbook.file` resolves to an existing file
- Outcome coverage: every terminal path ends in an outcome
- Branch completeness: warn about known LoginFailureCause values not covered

### 4.2 Scenario Testing (Replay)

```yaml
# test-spec: scenarios/icm-748724360/test.yaml
scenario: icm-748724360
expected_outcome: no_action
expected_chain:
  - login-success-rate-below-target
  - error-40613-state-127
expected_captures:
  failure_row_count: ">0"
  login_failure_cause: "LoginErrorsFound_40613_127"
must_not_reach:
  - escalate_unknown_cause
```

```
gert test login-success-rate-below-target.runbook.yaml
```

Runs all scenarios in replay mode, asserts expected outcomes. Deterministic, no auth needed, runs in CI.

### 4.3 Live Smoke Tests

```
gert test --live login-success-rate-below-target.runbook.yaml
```

Executes real Kusto queries against live environments. Catches:
- Kusto schema changes (column renamed, table moved)
- Template resolution failures with real data
- XTS provider changes

### 4.4 Coverage Metrics

```
gert coverage login-success-rate-below-target.runbook.yaml
```

Output:
```
Branch coverage:   17/42 (40%)   ← causes with scenarios
Step coverage:     28/59 (47%)   ← steps exercised by any scenario
Incident coverage: 37/40 (93%)   ← ICMs with matching branches

Missing branches:
  - IsDW (0 scenarios)
  - HasHighLatencyLoginsTopWaitStat_HADR_LOGPR (0 scenarios)

Untested steps:
  - transfer_to_oss
  - transfer_to_mi
```

---

## 5. Certification Levels

| Level | Name | Requirements |
|---|---|---|
| 0 | **Draft** | Parses and validates structurally (`gert validate` passes) |
| 1 | **Tested** | Has ≥1 scenario that replays successfully |
| 2 | **Verified** | All branches have scenarios, all replay tests pass, coverage >80% |
| 3 | **Certified** | Live-tested against ≥3 real incidents, peer-reviewed, coverage >90% |

Stored in the runbook:
```yaml
meta:
    certification:
        level: 2
        verified_at: "2026-02-17"
        verified_by: cristiano
        scenarios_passed: 37
        branch_coverage: 0.93
```

Displayed in the extension: `MITIGATION` `CERTIFIED ✓`

---

## 6. Regression Prevention

### On Pull Request
CI runs:
1. `gert validate` — structural checks
2. `gert test --replay` — all scenarios pass
3. `gert coverage` — coverage doesn't decrease
4. `gert render --check` — generated docs are up to date (if persisted)

### On Incident Resolution
When a DRI completes a TSG run and clicks "Save for Replay," the scenario automatically becomes a test case. The test corpus grows organically from real operations.

### Staleness Detection
`gert validate` compares `meta.source.source_hash` against the current source file. If the source TSG markdown has changed since the runbook was compiled, it flags the runbook as potentially stale.

---

## 7. Adoption Path

### Phase 1: Author (Week 1-2)
1. Configure YAML schema for autocomplete
2. Author runbooks for the highest-frequency TSGs
3. Include `prose` sections for publishable documentation
4. Add image references in `instructions` fields

### Phase 2: Test (Week 3-4)
1. Capture scenarios from recent ICM incidents (`gert scenario capture`)
2. Save replays from live TSG executions
3. Write test specs for critical paths
4. Set up CI with `gert validate` + `gert test --replay`

### Phase 3: Certify (Week 5-6)
1. Run coverage reports, fill gaps
2. Live-test against active incidents
3. Peer review runbooks
4. Graduate to Certified level

### Phase 4: Publish (Week 7+)
1. Replace source `.md` TSGs with `gert render` output
2. eng.ms build pipeline calls `gert render` instead of serving raw markdown
3. TSG prose panel becomes the primary reading experience
4. `.md` files are generated artifacts, not authored sources

---

## 8. TSG Chain Example

For the `login-success-rate-below-target` TSG, the full chain looks like:

```
login-success-rate-below-target.runbook.yaml
  ├── 42+ LoginFailureCause branches
  ├── LoginErrorsFound_* catch-all (template-resolved file paths)
  ├── Prefix catch-alls (IsGWProxyThrottled*, LookupErrorBackend*)
  │
  ├── → error-40613-state-127.runbook.yaml
  │     ├── Query by AppTypeName
  │     ├── Check DB availability
  │     ├── Check wrong-node routing
  │     └── → is-login-landing-on-wrong-node.runbook.yaml
  │           └── → error-40613-state-10-logins-landing-on-wrong-node.runbook.yaml
  │                 ├── Check recent 40613/10 errors
  │                 ├── Gateway node distribution
  │                 └── Kill-Process mitigation
  │
  ├── → is-partition-in-reconfiguration-mostly.runbook.yaml
  │     ├── Check Premium RS
  │     ├── Check TDE BYOK
  │     ├── Verify DB health (self-healed?)
  │     └── Check quorum loss
  │
  ├── → gateway-26078-33.runbook.yaml
  │     ├── Query 26078/33 by GW node
  │     └── Kill-Process mitigation
  │
  └── 28 more child runbooks...

Scenarios: 40 real ICM incidents
Coverage: 17 distinct LoginFailureCause variants
```
