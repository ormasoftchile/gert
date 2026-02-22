# Next Steps

## Build Order

Ordered by least dependencies to most. Each item builds on the ones before it.

| Order | Topic | Status | Dependencies | What to build |
|-------|-------|--------|-------------|---------------|
| 1 | Execution evidence | Collecting | None — extends existing engine | Run manifest (run.yaml), `--icm` flag, auto-save step responses as scenario |
| 2 | Runbook chaining | Collecting | #1 (run manifest for parent/child linking) | `outcome.next_runbook` schema, engine loads + runs child, linked traces |
| 3 | Scenario capture | Mostly built | #1 (auto-save makes capture automatic) | `--record` flag on exec, ICM snapshot at run start, scenario.yaml generation |
| 4 | Three-panel layout | Designing | #1 #2 #3 (needs engine protocol) | `gert serve` JSON-RPC backend, Lit webview (workflow map + active step panel) |
| 5 | Debug mode (F5) | Designing | #4 (shares webview + engine protocol) | VS Code launch config, DAP or custom protocol, variable watch panel |
| 6 | xts4vscode integration | Designing | #4 (needs webview to receive data) | Extension API on xts4vscode, Send Cell/Table buttons, session routing |

## Deferred

| Topic | Reason | Revisit when |
|-------|--------|-------------|
| DSConsole provider | Needs JIT lifecycle research, PowerShell session management | After #4 — the webview could host a terminal panel for DSConsole |
| SFE integration | Needs SFE REST API investigation, webview/iframe feasibility | After #6 — same extension-to-extension pattern as xts4vscode |

## Future Direction: Onboarding & Getting-Started Guides

The engine was born for ICM incident response, but the same primitives apply to **onboarding, ramp-up guides, and prerequisite installation**. An incident runbook is a guide you run under pressure; a getting-started guide is a runbook you run at your own pace. Same engine, different `kind`, a few extra primitives.

### What already maps

| Gert concept | ICM use | Onboarding use |
|---|---|---|
| `tree:` with branches | Escalate vs resolve | Skip if already installed |
| `manual` steps | "Copy CAS command, run in DS Console" | "Clone this repo, open in VS Code" |
| `cli` steps | Automated queries | `winget install`, `choco install`, `dotnet tool install` |
| `xts` steps | Kusto telemetry | Could generalize to any "query environment" |
| `outcomes` | resolved / escalated | installed / already-present / failed |
| `inputs` | From ICM fields | From environment detection |
| `replay` mode | Test scenarios | Demo/walkthrough without side effects |
| Evidence collection | Audit trail | Completion certificate / checklist proof |

### New primitives needed

**1. Precondition checks (biggest gap)**

Onboarding steps need idempotent probes — run a check, skip if already satisfied:

```yaml
- step:
    id: install_git
    type: cli
    title: Install Git
    precondition:
      check: ["git", "--version"]
      skip_if_succeeds: true  # already installed → auto-skip
    with:
      argv: ["winget", "install", "Git.Git"]
```

This is a natural `cli` variant. The engine runs the probe before the step; if it passes, auto-skip with state `already_satisfied`.

**2. New `kind: guide`**

Distinct from `mitigation` / `rca`. Signals different UX behavior:
- No urgency indicators, no ICM integration
- Progress bar instead of incident timeline
- "Resume where I left off" persistence
- Shareable completion status

**3. Environment detection as input source**

Today inputs come from `icm.*` or `prompt`. Onboarding needs:

```yaml
inputs:
  os:
    from: env.os          # windows, darwin, linux
  shell:
    from: env.shell       # pwsh, bash, zsh
  has_docker:
    from: probe
    check: ["docker", "--version"]
```

This lets the tree branch on OS or existing tooling automatically.

**4. Progress persistence**

ICM runbooks are one-shot. Onboarding is interruptible — someone starts day 1, finishes day 3. The engine needs to serialize cursor position + completed steps to a `.gert-state.yaml` next to the runbook for resume.

**5. Aggregation / team dashboards**

"Which new hires have completed the SAW setup guide?" — the evidence/trace output could feed a team-level completion view. Layer above the engine but the data model already captures it.

### Concrete examples

**SAW Setup Guide:**
1. `[cli]` Check if SAW is already provisioned → branch
2. `[manual]` Request SAW access via MyAccess
3. `[manual]` Wait for approval (with polling/reminder?)
4. `[cli]` Verify SAW connectivity
5. `[cli]` Install XTS on SAW
6. `[cli]` Install DS Console
7. `[cli]` Verify Kusto access with test query

**Team Onboarding Checklist:**
1. `[cli]` Clone required repos
2. `[cli]` Install Node.js, Go, .NET SDK (with precondition checks)
3. `[manual]` Set up VPN / corp network
4. `[cli]` Verify access to ADO repos
5. `[cli]` Run build on each repo
6. `[manual]` Join Teams channels (with links)

### Implementation order

1. **Add `kind: guide`** — minimal schema change
2. **Add `precondition` to Step** — `check` command + `skip_if_succeeds`. Engine runs probe, auto-skips with `already_satisfied` state
3. **Add `from: probe` and `from: env.*`** to InputDef — environment-aware inputs
4. **Add state persistence** — save/resume cursor to `.gert-state.yaml` (biggest lift, most impactful)

## Rationale

**#1 Execution evidence** first because it's pure Go engine work with no UI dependencies.
The run manifest and auto-save are needed by everything downstream.

**#2 Runbook chaining** next because it's a schema + engine change that the UI will render.
Better to have the data model right before building the visual layer.

**#3 Scenario capture** is mostly done (3 scenarios captured, replay working).
The remaining work (auto-save during real execution, `--record` flag) completes the loop
so every real run produces a replayable scenario.

**#4 Three-panel layout** is the big one — the VS Code extension with `gert serve` backend.
This is where the webview, workflow map, and active step panel come together.
Depends on #1-#3 because the UI renders run manifests, chained runs, and scenario data.

**#5 Debug mode** shares the webview infrastructure from #4. Adds step-through controls,
variable watch, breakpoints. Could reuse `gert serve` protocol with debug extensions.

**#6 xts4vscode** requires the webview (#4) to be running so it can receive data from
xts4vscode Send Cell events. The session routing and toolbar buttons need a gert UI to talk to.
