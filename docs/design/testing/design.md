!!!AI Generated.  To be verified!!!

# Testing Framework

## Status: ready

## Problem

Runbooks can be replayed today (`gert exec --mode replay --scenario <dir>`), and the extension can "Save for Replay." But there is no way to assert that a replay produced the *expected* result. Without assertions:

- An author replays a scenario, eyeballs the outcome, and assumes it's correct.
- A CI pipeline can run a replay, but cannot detect regressions (wrong branch, changed outcome, missing capture).
- Coverage is invisible — nobody knows which branches lack scenarios.

The missing pieces are:
1. A **test assertion schema** (`test.yaml`) — what the replay *should* produce.
2. A **`gert test` CLI command** — replays all scenarios, compares against `test.yaml`, reports pass/fail.
3. A **`gert coverage` CLI command** — reports which branches and steps are exercised by scenarios.
4. **Extension features** — generate `test.yaml`, batch replay, on-save testing, coverage overlay, diff view.

---

## 1. `test.yaml` Schema

### 1.1 Location

```
scenarios/{runbook-name}/{scenario-name}/test.yaml
```

Co-located with `inputs.yaml` and `steps/`. Discovered by convention — any scenario directory containing `test.yaml` is a test case. Scenarios without `test.yaml` are replay-only (not asserted).

### 1.2 Full Schema

```yaml
# test.yaml — declares expected behavior for a scenario replay
# All fields are optional. Omitted fields are not asserted.

# --- Outcome ---
# The terminal state the runbook should reach.
# Compared as exact string match against the outcome.state from the engine.
expected_outcome: "escalated"

# --- Chain ---
# Ordered list of runbook names visited during chained execution.
# Verified in order. Partial prefixes allowed: "login-success" matches "login-success-rate-below-target".
expected_chain:
  - login-success-rate-below-target
  - error-40613-state-127

# --- Captures ---
# Key-value assertions on the final captures map.
# Values support three forms:
#   exact string:     "LoginErrorsFound_40613_127"  — exact match
#   numeric compare:  ">0"  "<100"  ">=1"  "<=50"  "==0"  "!=0"
#   regex:            "/^LoginErrors.*/"
expected_captures:
  failure_row_count: ">0"
  login_failure_cause: "LoginErrorsFound_40613_127"
  top_app_type: "/^Gateway/"

# --- Step reachability ---
# Steps that MUST be visited during replay (at least entered).
must_reach:
  - query_login_failures
  - query_failure_breakdown
  - route_mitigation

# Steps that MUST NOT be visited.
must_not_reach:
  - escalate_unknown_cause

# --- Step status ---
# Assert that specific steps ended with a specific status.
# Useful for negative tests (step should fail) or skip tests.
expected_step_status:
  query_login_failures: passed
  check_application_scope: skipped

# --- Metadata (informational, not asserted) ---
description: "ICM 748724360 — LoginErrorsFound_40613_127 from ProdGeWc1a"
tags:
  - gateway
  - error-40613
```

### 1.3 Assertion Semantics

#### `expected_outcome`

Compared as exact string match against `outcome.state` from the engine.
If the runbook completes without reaching a terminal outcome (all steps pass but no outcome node), the actual outcome is `"completed"`.
If the runbook errors out, the actual outcome is `"error"`.

#### `expected_captures`

Each key is looked up in the final `captures` map. Missing key = assertion failure.

Value comparison rules:
| Value form | Example | Semantics |
|---|---|---|
| Plain string | `"LoginErrorsFound_40613_127"` | Exact string equality |
| Numeric prefix | `">0"`, `"<=50"`, `"!=0"` | Parse both sides as float64; compare. If either side cannot be parsed as a number, fall back to lexicographic comparison and fail with a descriptive error. |
| Regex | `"/^Gateway.*/"` | Delimited by `/`. Compiled as Go `regexp.MustCompile`. Match against the capture value. |

#### `must_reach` / `must_not_reach`

Step IDs collected from the engine's history (all steps that entered `running` state). Unrecognized step IDs in the assertion produce a warning, not a failure (the step may have been renamed).

#### `expected_step_status`

Each key is a step ID. Value is one of: `passed`, `failed`, `skipped`, `error`. Compared against the step's final status in the engine history.

#### `expected_chain`

The engine records which runbook files were loaded during chained execution. `expected_chain` is compared as an ordered prefix match — each entry must appear in order, but the chain may be longer. Matching is by runbook meta.name (exact) or by substring match against the runbook file path.

### 1.4 JSON Schema

The `test.yaml` schema will be exported alongside the runbook schema at `schemas/test-v0.json` for IDE autocompletion. The Go struct:

```go
// TestSpec defines expected outcomes for a scenario replay.
type TestSpec struct {
    ExpectedOutcome    string            `yaml:"expected_outcome,omitempty"    json:"expected_outcome,omitempty"`
    ExpectedChain      []string          `yaml:"expected_chain,omitempty"      json:"expected_chain,omitempty"`
    ExpectedCaptures   map[string]string `yaml:"expected_captures,omitempty"   json:"expected_captures,omitempty"`
    MustReach          []string          `yaml:"must_reach,omitempty"          json:"must_reach,omitempty"`
    MustNotReach       []string          `yaml:"must_not_reach,omitempty"      json:"must_not_reach,omitempty"`
    ExpectedStepStatus map[string]string `yaml:"expected_step_status,omitempty" json:"expected_step_status,omitempty"`
    Description        string            `yaml:"description,omitempty"         json:"description,omitempty"`
    Tags               []string          `yaml:"tags,omitempty"                json:"tags,omitempty"`
}
```

---

## 2. `gert test` CLI Command

### 2.1 Interface

```
gert test [runbook.yaml...]

Flags:
  --scenario <name>       Run only the named scenario (default: all)
  --json                  Output results as structured JSON (for extension / CI parsing)
  --fail-fast             Stop after first failure
  --timeout <duration>    Per-scenario timeout (default: 30s)
  --live                  Run in real mode (live Kusto) instead of replay
  --var key=value         Override variables (repeatable)
```

### 2.2 Discovery

Given a runbook file path, discover scenarios by convention:

```
{runbook-dir}/scenarios/{runbook-name}/*/inputs.yaml
```

Where `{runbook-name}` is derived from the filename by stripping `.runbook.yaml`.

For each discovered scenario directory:
- If `test.yaml` exists → test case (replay + assert)
- If `test.yaml` does not exist → skip (or include in `--all` mode as replay-only)

If multiple runbook paths are provided (or a glob), iterate each.

### 2.3 Execution Per Scenario

```
1. Load runbook (validate)
2. Load inputs.yaml → merge into meta.vars
3. Create engine in replay mode with scenario directory
4. Run to completion (or timeout)
5. Collect: outcome, captures, visited steps, step statuses, chain
6. Load test.yaml
7. Compare each assertion field
8. Produce TestResult
```

### 2.4 Output: Human-Readable (default)

```
gert test login-success-rate-below-target.runbook.yaml

  login-success-rate-below-target
    ✓ icm-748724360  (no_action)                    0.12s
    ✓ icm-749083743  (escalated)                    0.18s
    ✗ icm-751000123  (expected: mitigate, got: escalated)  0.09s
        expected_outcome: want "mitigate", got "escalated"
        must_reach: step "kill_gateway_process" was not visited
    ○ icm-752000000  (no test.yaml — replay only)   0.11s

  3 scenarios, 2 passed, 1 failed, 1 skipped
```

Exit codes:
- `0` — all asserted tests passed
- `1` — at least one asserted test failed
- `2` — runbook validation failed (no tests ran)

### 2.5 Output: JSON (`--json`)

```json
{
  "runbook": "login-success-rate-below-target",
  "scenarios": [
    {
      "name": "icm-748724360",
      "status": "passed",
      "duration_ms": 120,
      "outcome": { "actual": "no_action", "expected": "no_action" },
      "assertions": []
    },
    {
      "name": "icm-751000123",
      "status": "failed",
      "duration_ms": 90,
      "outcome": { "actual": "escalated", "expected": "mitigate" },
      "assertions": [
        {
          "type": "expected_outcome",
          "expected": "mitigate",
          "actual": "escalated",
          "passed": false
        },
        {
          "type": "must_reach",
          "step_id": "kill_gateway_process",
          "passed": false,
          "message": "step was not visited"
        }
      ]
    }
  ],
  "summary": {
    "total": 3,
    "passed": 2,
    "failed": 1,
    "skipped": 1
  }
}
```

The extension will parse this JSON to populate test results in the UI.

### 2.6 Implementation Location

```
pkg/testing/
  spec.go          — TestSpec struct, parse test.yaml
  assert.go        — assertion evaluation logic
  runner.go        — discover scenarios, run engine, collect results
  runner_test.go   — unit tests
  types.go         — TestResult, AssertionResult structs

cmd/gert/main.go   — add testCmd to cobra
```

### 2.7 Dependency on Engine

The `gert` binary **must be present** on the CI agent to run tests. The engine is what:
- Resolves template expressions (`{{ .var }}`)
- Evaluates condition expressions (expr-lang)
- Walks the tree state machine
- Resolves captures from JSON-path against step responses
- Tracks visited steps and final outcome

There is no "light" alternative. The full engine is required. See the testing-runbooks guide Section 5.3 for CI distribution options.

---

## 3. `gert coverage` CLI Command

### 3.1 Interface

```
gert coverage [runbook.yaml...]

Flags:
  --json                  Output as structured JSON
  --fail-on-decrease      Exit 1 if coverage is lower than meta.certification.branch_coverage
  --report                Generate a full report (Markdown table to stdout)
```

### 3.2 Coverage Model

Coverage is computed by replaying all scenarios and recording which nodes in the tree are visited.

| Metric | Definition | Denominator |
|---|---|---|
| **Branch coverage** | Number of `condition:` branches entered by at least one scenario / total branches | All `branches[].condition` nodes in the tree |
| **Step coverage** | Number of steps visited (entered `running`) by at least one scenario / total steps | All `step:` nodes in the tree (including nested) |
| **Outcome coverage** | Number of distinct `outcome.state` values reached / total distinct outcome states defined | All `outcomes[].state` values in the tree |

### 3.3 Computation

```
1. Parse the runbook → enumerate all branches, steps, outcome states
2. For each scenario with test.yaml (or all scenarios if --all):
   a. Replay scenario
   b. Record visited steps, taken branches, reached outcome
3. Aggregate: union of visited nodes across all scenarios
4. Compute percentages
5. Report uncovered branches, steps, outcomes
```

### 3.4 Output: Human-Readable (default)

```
gert coverage login-success-rate-below-target.runbook.yaml

  Branch coverage:   17/42 (40%)
  Step coverage:     28/59 (47%)
  Outcome coverage:   3/4  (75%)

  Uncovered branches:
    - condition: login_failure_cause == "IsDW"                    (0 scenarios)
    - condition: login_failure_cause == "HasHighLatency..._HADR"  (0 scenarios)

  Uncovered steps:
    - transfer_to_oss
    - transfer_to_mi

  Uncovered outcomes:
    - state: needs_rca
```

### 3.5 Output: JSON (`--json`)

```json
{
  "runbook": "login-success-rate-below-target",
  "branch_coverage": { "covered": 17, "total": 42, "percent": 40.5 },
  "step_coverage":   { "covered": 28, "total": 59, "percent": 47.5 },
  "outcome_coverage": { "covered": 3,  "total": 4,  "percent": 75.0 },
  "uncovered_branches": [
    { "condition": "login_failure_cause == \"IsDW\"", "parent_step": "route_mitigation" }
  ],
  "uncovered_steps": ["transfer_to_oss", "transfer_to_mi"],
  "uncovered_outcomes": ["needs_rca"],
  "scenarios_used": 37
}
```

### 3.6 `--fail-on-decrease`

Reads `meta.certification.branch_coverage` from the runbook. If the computed branch coverage is lower, exit 1 with:

```
Coverage decreased: 40.5% → 38.1% (was 40.5% at certification)
```

If no certification data exists in the runbook, this flag is a no-op (allows bootstrapping).

### 3.7 Implementation Location

```
pkg/coverage/
  coverage.go       — tree enumeration, aggregation
  coverage_test.go  — unit tests
  types.go          — CoverageResult, BranchInfo structs

cmd/gert/main.go    — add coverageCmd to cobra
```

---

## 4. Extension Features

### Feature 1: Generate `test.yaml` from a Completed Run

**Trigger:** After user clicks "Save for Replay" and the scenario is saved, show a follow-up notification with "Save as Test Case" action.

**Data source:** The extension already caches during a run:
- `this.outcomeResult` — the final outcome (state, recommendation, nextRunbook)
- `this.captures` — accumulated captures map
- `this.stepStates` — Map<stepId, state> for all visited steps
- `this.chainHistory` — ChainEntry[] for chained runbooks

No new RPC is needed. The extension has everything to write `test.yaml`.

**Implementation:**

Add to the `saveForReplay` handler in `runbookPanel.ts`, after the scenario directory is written:

```typescript
// After successful save, offer to generate test.yaml
const saveTest = await vscode.window.showInformationMessage(
  `Scenario saved to ${outputDir} (${copied} steps)`,
  'Save as Test Case',
  'Skip'
);

if (saveTest === 'Save as Test Case') {
  const testSpec: any = {};

  // Outcome
  if (this.outcomeResult?.state) {
    testSpec.expected_outcome = this.outcomeResult.state;
  }

  // Chain
  if (this.chainHistory.length > 0) {
    testSpec.expected_chain = this.chainHistory.map(e => e.name);
    // Add current runbook
    const currentName = path.basename(this.creationArgs?.runbookPath || '')
      .replace(/\.runbook\.(yaml|yml)$/i, '');
    testSpec.expected_chain.push(currentName);
  }

  // Captures (include all non-empty captures)
  const capturesForTest: Record<string, string> = {};
  for (const [k, v] of Object.entries(this.captures)) {
    if (v && v !== '<dry-run>') {
      capturesForTest[k] = v;
    }
  }
  if (Object.keys(capturesForTest).length > 0) {
    testSpec.expected_captures = capturesForTest;
  }

  // must_reach: all steps that were visited (passed or failed)
  const reached: string[] = [];
  for (const [id, state] of this.stepStates.entries()) {
    if (state === 'passed' || state === 'failed') {
      reached.push(id);
    }
  }
  if (reached.length > 0) {
    testSpec.must_reach = reached;
  }

  // Write test.yaml
  const testYaml = YAML.stringify(testSpec);
  fs.writeFileSync(path.join(outputDir, 'test.yaml'), testYaml, 'utf-8');
  vscode.window.showInformationMessage(`Test case saved: ${path.join(outputDir, 'test.yaml')}`);
}
```

**Acceptance criteria:**
- [ ] After "Save for Replay", a "Save as Test Case" action is offered.
- [ ] Clicking it writes `test.yaml` with `expected_outcome`, `expected_captures`, `must_reach`.
- [ ] The generated `test.yaml` validates against the test schema.
- [ ] Replaying the same scenario with `gert test` passes (generated assertions match the run).

---

### Feature 2: Batch Replay (`Gert: Run Tests`)

**Trigger:** Command palette → `Gert: Run Tests`. Also available as a code lens on `.runbook.yaml` files.

**Implementation:**

Register a new command `gert.runTests` in the extension:

```typescript
const runTestsCommand = vscode.commands.registerCommand(
  'gert.runTests',
  async (uri?: vscode.Uri) => {
    // 1. Resolve runbook path (active editor or provided uri)
    // 2. Spawn: gert test <runbook> --json
    // 3. Parse JSON output
    // 4. Display results in a webview panel or Test Explorer
  }
);
```

**Output display options (choose one):**

| Option | Effort | UX Quality |
|---|---|---|
| VS Code Test Explorer API | Medium | Native, familiar, integrates with test sidebar |
| Webview panel | Low | Custom layout, matches gert visual style |
| Output channel + diagnostics | Low | Minimal UI, results in Problems panel |

**Recommended:** VS Code Test Explorer API. It provides:
- Tree view of scenarios (pass/fail icons)
- Click-to-run individual scenarios
- Re-run failed tests
- Integration with the test sidebar and status bar

**Test controller registration:**

```typescript
const controller = vscode.tests.createTestController('gert', 'Gert Runbook Tests');

// Discover test items from scenario directories
controller.resolveHandler = async (item) => {
  if (!item) {
    // Root: find all .runbook.yaml files with scenarios
    // For each, create a test item per scenario with test.yaml
  }
};

// Run handler: spawn gert test --json, map results to test items
controller.createRunProfile('Run', vscode.TestRunProfileKind.Run, async (request, token) => {
  // ...
});
```

**Acceptance criteria:**
- [ ] `Gert: Run Tests` command available in the palette when a `.runbook.yaml` is active.
- [ ] All scenarios with `test.yaml` are discovered and run.
- [ ] Results show pass/fail per scenario with failure details.
- [ ] Clicking a failed test opens the scenario in replay mode.

---

### Feature 3: Test-on-Save (Background Replay)

**Trigger:** Saving a `.runbook.yaml` file. Debounced (500ms).

**Implementation:**

In the extension's `onDidSaveTextDocument` handler (where validation already runs):

```typescript
// After validation succeeds, check for scenarios
const runbookName = path.basename(doc.fileName).replace(/\.runbook\.(yaml|yml)$/i, '');
const scenarioBase = path.join(path.dirname(doc.fileName), 'scenarios', runbookName);

if (fs.existsSync(scenarioBase)) {
  // Spawn gert test <runbook> --json in the background
  const proc = exec(`"${gertPath}" test "${doc.fileName}" --json --timeout 10s`);
  // Parse output, convert to diagnostics
}
```

**Diagnostics mapping:**

```typescript
for (const scenario of result.scenarios) {
  if (scenario.status === 'failed') {
    for (const assertion of scenario.assertions.filter(a => !a.passed)) {
      const diagnostic = new vscode.Diagnostic(
        // Range: line 0 (file-level) or step definition line if source mapping exists
        range,
        `Scenario ${scenario.name}: ${assertion.message}`,
        vscode.DiagnosticSeverity.Warning
      );
      diagnostic.source = 'gert test';
      diagnostics.push(diagnostic);
    }
  }
}
```

**Performance budget:** 5 seconds for 40 scenarios. If exceeded, show a progress indicator and allow cancellation. Background test runs are cancelled if the file is saved again (debounce).

**Acceptance criteria:**
- [ ] Saving a `.runbook.yaml` triggers background `gert test --json`.
- [ ] Failed assertions appear as warnings in the Problems panel.
- [ ] Tests are cancelled and re-triggered on subsequent saves.
- [ ] No tests run if no `test.yaml` files exist (avoid wasted spawns).

---

### Feature 4: Coverage Overlay on Workflow Map

**Trigger:** After a batch test run (`Gert: Run Tests`), or via a toggle button on the workflow map.

**Implementation:**

After `gert test --json` completes, the extension also runs `gert coverage <runbook> --json` and parses the output.

The tree renderer in `getHtml()` already assigns CSS classes to step nodes based on `stepStates`. Add a `coverageMode` boolean that switches the rendering:

| Coverage state | Visual treatment |
|---|---|
| Covered (≥1 scenario) | Green left border + scenario count badge |
| Uncovered (0 scenarios) | Red/gray left border + "0" badge |
| Condition branch covered | Green connector line |
| Condition branch uncovered | Dashed gray connector line |

**Data flow:**

```
gert coverage --json → CoverageResult
  → Map<stepId, { covered: boolean, count: number }>
  → Map<branchCondition, { covered: boolean, count: number }>
  → Pass to webview via postMessage
  → Tree renderer reads coverage data if coverageMode is true
```

**Toggle:** A button in the workflow map toolbar: `📊 Coverage` / `▶ Execution` to switch between coverage view and execution view.

**Acceptance criteria:**
- [ ] After `Gert: Run Tests`, a coverage overlay can be toggled.
- [ ] Uncovered branches are visually distinct from covered branches.
- [ ] Each step shows the number of scenarios that exercise it.
- [ ] The overlay updates when new test results arrive.

---

### Feature 5: Scenario Diff View

**Trigger:** Clicking "View Failed" on a failed test result (Feature 2).

**Implementation:**

When a test fails, the extension has:
- `test.yaml` (expected) — loaded from the scenario directory
- Test result JSON (actual) — from the `gert test --json` output

Display a structured diff in a webview panel:

```
┌─ Scenario Diff: icm-751000123 ────────────────────┐
│                                                     │
│  Outcome                                            │
│    expected:  mitigate                              │
│    actual:    escalated                      ✗      │
│                                                     │
│  Path diverged at: route_mitigation                 │
│    Condition: login_failure_cause == "Login..."     │
│    Expected:  → LoginErrorsFound_* branch           │
│    Actual:    → fallback → escalate                 │
│                                                     │
│  Captures                                           │
│    failure_row_count:  "143" → "143"         ✓      │
│    login_failure_cause:                             │
│      expected: "LoginErrorsFound_40613_127"         │
│      actual:   "Timeout_40613"               ✗      │
│                                                     │
│  Steps                                              │
│    ✓ query_login_failures      (must_reach)         │
│    ✓ query_failure_breakdown   (must_reach)         │
│    ✗ kill_gateway_process      (must_reach, missed) │
│    ✓ escalate_unknown_cause    (must_not_reach: OK) │
│                                                     │
│  [Open in Replay Mode]   [Update test.yaml]         │
└─────────────────────────────────────────────────────┘
```

**"Update test.yaml":** If the new behavior is intentional (the author changed the logic on purpose), clicking this button overwrites `test.yaml` with the actual results. This is the equivalent of "accepting" a snapshot update.

**Acceptance criteria:**
- [ ] Failed tests offer a "View Diff" action.
- [ ] The diff view shows expected vs. actual for outcome, captures, and step reachability.
- [ ] "Open in Replay Mode" launches the scenario in step-by-step replay.
- [ ] "Update test.yaml" writes the actual results as the new expected values.

---

## 5. Serve-Side Changes

The `gert serve` JSON-RPC server (used by the extension) needs minimal additions:

### 5.1 `exec/getRunSummary` (new RPC)

Returns the final state of a completed run. Used by the extension after "Save for Replay" if cached state is insufficient for edge cases (e.g., chain collapsed into snapshot).

**Request:**
```json
{ "method": "exec/getRunSummary", "params": {} }
```

**Response:**
```json
{
  "outcome": { "state": "escalated", "step_id": "escalate_unknown_cause" },
  "captures": { "failure_row_count": "143", "login_failure_cause": "..." },
  "visited_steps": ["query_login_failures", "query_failure_breakdown", "route_mitigation", "escalate_unknown_cause"],
  "step_statuses": { "query_login_failures": "passed", "route_mitigation": "passed" },
  "chain": ["login-success-rate-below-target"]
}
```

### 5.2 No Change to `exec/saveScenario`

The existing `exec/saveScenario` RPC writes `inputs.yaml` and `steps/*.json`. The `test.yaml` generation is handled entirely in the extension (Feature 1) using cached state. No engine-side change needed.

---

## 6. Implementation Plan

### Phase 1: Schema + CLI (engine-side)

| Task | Package | Depends On |
|---|---|---|
| Define `TestSpec` struct + YAML parsing | `pkg/testing/spec.go` | — |
| Export `test-v0.json` schema | `pkg/testing/spec.go` | TestSpec struct |
| Assertion evaluator (outcome, captures, steps) | `pkg/testing/assert.go` | TestSpec |
| Scenario discovery + runner | `pkg/testing/runner.go` | assert, engine |
| `gert test` cobra command | `cmd/gert/main.go` | runner |
| Unit tests for all of the above | `pkg/testing/*_test.go` | all |
| Tree enumerator (all branches, steps, outcomes) | `pkg/coverage/coverage.go` | schema |
| Coverage aggregator | `pkg/coverage/coverage.go` | runner, enumerator |
| `gert coverage` cobra command | `cmd/gert/main.go` | coverage |

### Phase 2: Extension Features

| Task | File | Depends On |
|---|---|---|
| Feature 1: Generate test.yaml | `runbookPanel.ts` | Phase 1 (test.yaml schema) |
| Feature 2: Batch replay via Test Explorer | `extension.ts` + new `testController.ts` | Phase 1 (`gert test --json`) |
| Feature 3: Test-on-save | `extension.ts` (onDidSave handler) | Phase 1 (`gert test --json`) |
| Feature 4: Coverage overlay | `runbookPanel.ts` (getHtml) | Phase 1 (`gert coverage --json`) |
| Feature 5: Scenario diff view | new `testDiffPanel.ts` | Feature 2 |

### Phase 3: CI Integration

| Task | Depends On |
|---|---|
| Sample Azure Pipelines YAML for runbook repos | Phase 1 (gert binary) |
| Document binary distribution options | Phase 1 |
| Pre-commit hook configuration (gert validate) | Phase 1 |

---

## 7. Acceptance Criteria (Full Feature)

### Engine

- [ ] `gert test` discovers all scenarios with `test.yaml` for a given runbook.
- [ ] `gert test` replays each scenario and compares against all assertion fields.
- [ ] `gert test` exits 0 on all-pass, 1 on any failure, 2 on validation error.
- [ ] `gert test --json` produces parseable structured output matching the schema in §2.5.
- [ ] `gert coverage` enumerates all branches and steps in a tree-based runbook.
- [ ] `gert coverage --json` produces parseable output matching the schema in §3.5.
- [ ] `gert coverage --fail-on-decrease` compares against `meta.certification.branch_coverage`.
- [ ] All assertion types work: outcome, captures (exact, numeric, regex), must_reach, must_not_reach, step_status, chain.
- [ ] Edge case: scenario with no `test.yaml` is skipped (reported but not counted as failure).
- [ ] Edge case: runbook with no scenarios directory reports 0 tests (exit 0).

### Extension

- [ ] Feature 1: "Save as Test Case" offered after "Save for Replay".
- [ ] Feature 1: Generated `test.yaml` passes `gert test` for the same scenario.
- [ ] Feature 2: `Gert: Run Tests` runs all scenarios and shows results.
- [ ] Feature 2: Failed tests can be opened in replay mode with one click.
- [ ] Feature 3: Saving a `.runbook.yaml` triggers background test run.
- [ ] Feature 3: Failed assertions appear as warnings in the Problems panel.
- [ ] Feature 4: Coverage overlay shows covered/uncovered branches on the workflow map.
- [ ] Feature 5: Diff view shows expected vs. actual for a failed test.
- [ ] Feature 5: "Update test.yaml" rewrites assertions from actual results.

### CI

- [ ] Sample pipeline runs `gert validate` + `gert test` + `gert coverage --fail-on-decrease`.
- [ ] Pipeline works with a checked-in `gert` binary (no external dependencies).
- [ ] Replay tests complete in under 30 seconds for a runbook with 40 scenarios.

---

## 8. Test Data for the Testing Framework Itself

The gert repo needs tests for the test framework. These live alongside the existing test fixtures:

```
testdata/
  testing/
    pass-all/
      minimal.runbook.yaml
      scenarios/minimal/
        smoke-test/
          inputs.yaml
          steps/000-step-one.json
          test.yaml                  ← expects outcome "completed"
    fail-outcome/
      ...runbook + scenario where expected_outcome doesn't match
    fail-capture/
      ...runbook + scenario where expected_captures has a mismatch
    fail-must-reach/
      ...runbook + scenario where a must_reach step is not visited
    no-test-yaml/
      ...runbook + scenario without test.yaml (should be skipped)
    coverage/
      branching.runbook.yaml       ← runbook with known branch count
      scenarios/branching/
        branch-a/ + branch-b/      ← two scenarios covering different branches
```

Each directory is a self-contained test case for the `gert test` and `gert coverage` commands. The Go tests in `pkg/testing/*_test.go` load these fixtures and assert the expected behavior of the test runner itself.

---

## Open Questions

1. **Should `gert test` support `--live` mode?** Live mode executes real Kusto queries. This catches schema drift but is non-deterministic and requires auth. Proposed: support it as a flag, but the default (and CI default) is always replay.

2. **Should `test.yaml` support negated capture assertions?** e.g., `failure_count: "!0"` (capture must NOT be zero). Proposed: yes, add `!=` as a valid prefix.

3. **Should coverage track condition *expressions* or condition *branches*?** A condition like `login_failure_cause == "HasDumps"` is one branch. The catch-all `when:` clause at the end is another. Proposed: each `branches[].condition` is one branch, and each `outcomes[].when` is one branch.

4. **Should `gert test` support parallel scenario execution?** Replay is CPU-bound and fast. For runbooks with 100+ scenarios, parallelism could help. Proposed: support `--parallel N` flag, default 1 (sequential) for deterministic output ordering.

5. **Extension: Test Explorer vs. custom webview for results?** Test Explorer integrates with VS Code natively but has limited customization. Custom webview allows richer diff views but is non-standard. Proposed: Test Explorer for discovery and pass/fail status; custom webview for the diff detail (Feature 5).
