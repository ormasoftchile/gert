!!!AI Generated.  To be verified!!!

# Testing Runbooks: A First-Class Development Practice

## Overview

Testing is not a phase that happens after a runbook is written — it is an integral part of the authoring cycle. Every runbook change is validated structurally, exercised against recorded scenarios, and gated on coverage before it can merge. This document defines where tests live, how they run, when they run, and how the test corpus grows organically from real incidents.

---

## 1. Why Tests Are Part of the Runbook, Not an Afterthought

A prose TSG cannot be tested. A structured runbook can. The shift from markdown documents to `runbook.yaml` unlocks the same workflow software engineers rely on: write logic → write tests → validate in CI → merge with confidence.

**Without tests, runbooks drift.** A Kusto column gets renamed. A branch condition references a capture that no longer exists. A new `LoginFailureCause` variant appears but no path handles it. These failures surface during a live incident — the worst possible time.

**With tests, runbooks are contracts.** Each scenario encodes: "given these inputs and these Kusto responses, the runbook should follow this path and reach this outcome." When anything changes, the contract breaks loudly and before production.

---

## 2. Test Taxonomy

Runbook testing has four layers, each catching a different class of defect:

| Layer | Command | What It Catches | When It Runs |
|---|---|---|---|
| **Structural validation** | `gert validate` | Schema errors, undefined variables, dangling references, duplicate step IDs | On save (editor), on commit (pre-commit hook), on PR (CI) |
| **Scenario replay** | `gert test --replay` | Wrong branching logic, broken conditions, incorrect captures, missing outcomes | On save (editor), on PR (CI), nightly |
| **Live smoke test** | `gert test --live` | Kusto schema changes, template resolution failures with real data, XTS provider changes | Nightly or on-demand |
| **Coverage analysis** | `gert coverage` | Untested branches, missing failure-cause variants, steps never exercised | On PR (CI), weekly report |

---

## 3. Repository Layout: Where Tests Live

Tests live alongside the runbooks they validate. No separate `tests/` directory, no external test project. The convention is:

```
connection/
  login-success-rate-below-target.runbook.yaml     ← the runbook
  scenarios/
    login-success-rate-below-target/                ← matches runbook name (by convention)
      icm-748724360/                                ← one scenario per incident
        inputs.yaml                                 ← input values for this scenario
        steps/                                      ← recorded step responses
          000-query-login-failures.json             ← Kusto response snapshot
          001-query-failure-cause.json
        test.yaml                                   ← expected outcome assertions
      icm-749083743/
        inputs.yaml
        steps/
          000-query-login-failures.json
        test.yaml
```

### 3.1 Why Co-Located?

- **Discoverability.** An engineer reading the runbook can see all its test cases immediately.
- **Ownership.** The person editing the runbook owns the scenarios. No cross-repo coordination.
- **Automatic discovery.** The engine finds scenarios at `scenarios/{runbook-name}/*/inputs.yaml` — no registration or manifest file.
- **PR diffs are self-contained.** A PR that changes branching logic also shows which scenarios were updated.

### 3.2 File Breakdown

#### `inputs.yaml` — What Triggered the Incident

```yaml
# ICM 748724360 — LoginErrorsFound_40613_127
# State: ACTIVE | Region: Germany West Central (ProdGeWc1a)
server_name: "spartan-srv-ger-bcprodweu-919dae62a58b"
database_name: "db_bcprodweu_t47243237_20251118_04355555_318e"
environment: "ProdGeWc1a"
login_failure_cause: "LoginErrorsFound_40613_127"
```

This file supplies values for every `meta.inputs` field in the runbook. During replay, these values are injected instead of prompting the user or parsing ICM.

#### `steps/{NNN}-{step-id}.json` — Recorded Kusto Responses

```json
{
  "success": true,
  "rowCount": 12,
  "columns": ["FailureCount", "GatewayNodesWithFailure"],
  "data": [
    { "FailureCount": 143, "GatewayNodesWithFailure": "GW.12,GW.47" }
  ]
}
```

This is the frozen response for a specific XTS/Kusto step. During replay, the engine serves this file instead of querying Kusto. The file name prefix (`000-`, `001-`) indicates execution order.

#### `test.yaml` — Expected Outcome Assertions

```yaml
scenario: icm-748724360
expected_outcome: escalate
expected_chain:
  - login-success-rate-below-target
  - error-40613-state-127
expected_captures:
  failure_row_count: ">0"
  login_failure_cause: "LoginErrorsFound_40613_127"
must_reach:
  - query_login_failures
  - check_error_cause
must_not_reach:
  - escalate_unknown_cause
```

This is the test assertion file. It declares what the runbook *should* do when given these inputs and responses. If the runbook's branching logic changes and the outcome no longer matches, the test fails.

---

## 4. The Extension as the Testing Surface

The VS Code extension is where runbook authors spend their time. Testing must be integrated directly into that experience — not as a separate CLI workflow the author has to remember. The extension already has the building blocks; this section describes the current capabilities, the testing workflows they enable, and the features that need to be added to close the gaps.

### 4.1 What the Extension Has Today

| Capability | Status | How It Works |
|---|---|---|
| **Validation on save** | Built | Extension runs `validateRunbook` on document change; schema errors, undefined vars, and duplicate IDs appear as editor diagnostics (red squiggles). |
| **Run in replay mode** | Built | Mode picker offers `replay` — loads `inputs.yaml` + `steps/*.json` from a scenario folder. Engine serves frozen responses instead of querying Kusto. |
| **Run All (replay auto-advance)** | Built | `runAll` webview message auto-advances through all steps in replay mode. Manual steps are auto-completed with recorded evidence. |
| **Save for Replay** | Built | `saveForReplay` webview message writes `inputs.yaml` from resolved vars and copies step `*.json` files from the engine's auto-save directory into `scenarios/{runbook-name}/{scenario-name}/`. |
| **Scenario picker** | Built | When launching in `replay` or `real (saved inputs)` mode, the extension discovers scenarios at `scenarios/{runbook-name}/*/inputs.yaml` and presents a quick-pick. |
| **Restart** | Built | `restart` webview message kills the gert-serve process, starts fresh, and re-runs from step 1 with the same inputs. |

### 4.2 Testing Workflow: Capture → Replay → Assert

Using only what exists today, here is how an author creates and runs a test:

#### Step 1: Run the Runbook Live

Open the runbook → Command Palette → `Gert: Run TSG` → pick `real` or `icm` mode.

The extension prompts for inputs (or extracts them from an ICM incident), then runs each step against live Kusto. The author watches the workflow map, inspects captures, and follows branches until the runbook reaches an outcome.

#### Step 2: Save the Execution as a Scenario

At any point after the run completes (or reaches an outcome), the extension shows a **"Save for Replay"** action. Clicking it:

1. Creates `scenarios/{runbook-name}/{tsg-name}-{outcome}/`
2. Writes `inputs.yaml` with the resolved input values
3. Copies every `steps/NNN-{step-id}.json` file from the engine's run directory

The author now has a frozen snapshot of the entire execution — inputs, Kusto responses, branch path taken.

#### Step 3: Replay the Scenario

Open the same runbook → `Gert: Run TSG` → pick `replay` → select the saved scenario.

The extension loads the frozen inputs and responses. Click **"Run All"** and the entire execution replays in seconds with no network access. The workflow map animates through the same path. The author can verify the runbook reaches the same outcome.

#### Step 4: Edit and Re-Replay

Change a condition, rename a capture, add a branch. Open the runbook → replay the same scenario. If the path or outcome changed, the author sees it immediately in the workflow map.

This is the test loop. No command line needed. No separate test runner.

### 4.3 What the Extension Needs for Complete Testing

The workflow above proves the logic works once. To make it a *regression test*, the extension needs to know what the *expected* result is and compare it automatically. These are the feature asks:

#### Feature 1: Generate `test.yaml` from a Completed Run

**What:** After "Save for Replay" succeeds, offer a follow-up action: **"Save as Test Case."** This writes a `test.yaml` alongside the `inputs.yaml` capturing the expected outcome, the steps visited, and key captures.

**Why:** Today the author must hand-write `test.yaml`. Most authors will not. If the extension generates it from the actual run, every saved scenario becomes a test case automatically.

**Proposed UX:**
```
  ┌──────────────────────────────────────────────┐
  │  Scenario saved to scenarios/.../icm-749083  │
  │                                              │
  │  [Save as Test Case]    [Skip]               │
  └──────────────────────────────────────────────┘
```

Clicking "Save as Test Case" writes:
```yaml
# test.yaml — auto-generated from run 20260219T141530-a3f2b1c0
scenario: icm-749083743
expected_outcome: escalate
expected_chain:
  - login-success-rate-below-target
  - error-40613-state-127
expected_captures:
  failure_row_count: "143"
  login_failure_cause: "LoginErrorsFound_40613_127"
must_reach:
  - query_login_failures
  - assess_failure_cause
  - check_db_availability
must_not_reach: []
```

The author can edit the file afterward (loosen assertions, add `must_not_reach` entries), but the default is a complete snapshot of the observed behavior.

**Serve-side dependency:** The `exec/saveScenario` RPC already exists. The extension needs the engine to also return the visited step IDs, final outcome state, and final captures map — either from the existing event stream (`event/outcomeReached`, `event/stepCompleted`) which it already collects, or via a new `exec/getSummary` RPC.

#### Feature 2: Run All Scenarios as Tests (Batch Replay)

**What:** A command `Gert: Run Tests` that discovers all scenarios for the active runbook, replays each one, compares against `test.yaml`, and shows pass/fail results.

**Why:** Today the author can replay one scenario at a time. To verify all tests pass after an edit, they would need to pick each scenario manually. That does not scale beyond 2-3 scenarios.

**Proposed UX:**
```
  ┌─────────────────────────────────────────────┐
  │  Test Results: login-success-rate-below...   │
  │                                              │
  │  ✓ icm-748724360  (no_action)       0.3s    │
  │  ✓ icm-749083743  (escalate)        0.4s    │
  │  ✗ icm-751000123  (expected: mitigate,      │
  │                    got: escalate)    0.2s    │
  │                                              │
  │  2 passed, 1 failed                          │
  │                                              │
  │  [View Failed]   [Re-run All]                │
  └─────────────────────────────────────────────┘
```

Clicking "View Failed" opens the scenario in replay mode so the author can step through and see where the path diverged.

**Implementation path:**
- The extension already knows how to start a replay (`execStart` with `scenarioDir`).
- The batch command iterates over discovered scenarios, starts a headless (non-UI) replay for each, and collects outcomes.
- Each outcome is compared against `test.yaml` assertions.
- Results are displayed in a webview panel or the VS Code Test Explorer API.

#### Feature 3: Test Results on Save (Background Replay)

**What:** When the author saves a `.runbook.yaml` file, the extension automatically replays all scenarios with `test.yaml` files in the background and reports results as diagnostics.

**Why:** This is the equivalent of "compile on save" for runbooks. The author does not need to remember to run tests — they just appear. A broken assertion shows up as a warning in the Problems panel within seconds of the edit.

**Proposed UX (Problems panel):**
```
  ⚠ login-success-rate-below-target.runbook.yaml
    Scenario icm-751000123: expected outcome 'mitigate', got 'escalate'
    Scenario icm-751000123: step 'kill_gateway_process' in must_reach but was not visited
```

**Performance:** Replay is fast (no I/O beyond reading JSON files). Running 40 scenarios takes under 5 seconds. The extension spawns a `gert test --replay` process on save with a debounce and parses structured output.

#### Feature 4: Coverage Overlay on the Workflow Map

**What:** When viewing a runbook (either in editing or after a batch test run), the workflow map highlights which branches and steps are covered by scenarios and which are not.

**Why:** The workflow map already renders the tree with step states (pending, running, passed, failed, skipped). Adding a "coverage" layer — green for covered, red/gray for uncovered — gives the author instant visual feedback on where scenarios are missing.

**Proposed UX:**
```
  Workflow Map (coverage mode)
  ┌─────────────────────────────────────┐
  │  ● query_login_failures      ██ 40  │  ← 40 scenarios cover this step
  │  ├── LoginErrorsFound_*      ██ 17  │
  │  │   └── error-40613-127     ██  3  │
  │  ├── HasDumps                ░░  0  │  ← no scenarios
  │  ├── IsDW                    ░░  0  │  ← no scenarios
  │  └── escalate_unknown        ██ 20  │
  └─────────────────────────────────────┘
```

**Implementation path:** `gert coverage --json` outputs per-branch and per-step coverage data. The extension calls this after a batch test run and merges the data into the existing tree renderer.

#### Feature 5: Scenario Diff View (Expected vs. Actual)

**What:** When a test fails, offer a diff view showing the expected outcome/captures from `test.yaml` alongside the actual outcome/captures from the replay.

**Why:** "Expected mitigate, got escalate" is helpful, but the author also needs to see *which capture changed* or *which branch diverged*. A structured diff (not a text diff) shows exactly where the logic went wrong.

**Proposed UX (webview panel):**
```
  ┌─ Scenario Diff: icm-751000123 ─────────────────┐
  │                                                  │
  │  Outcome:   expected mitigate  │  actual escalate │
  │                                                  │
  │  Path diverged at step: assess_failure_cause     │
  │    Condition: login_failure_cause startsWith...  │
  │    Expected branch: "LoginErrorsFound_*"         │
  │    Actual branch:   fallback → escalate          │
  │                                                  │
  │  Captures:                                       │
  │    failure_row_count:  "143" → "143" (match)     │
  │    login_failure_cause: expected "LoginErrors..."│
  │                         actual  "Timeout_40613"  │
  └──────────────────────────────────────────────────┘
```

### 4.4 Testing Without the Command Line

With the features above, the complete testing workflow lives inside the extension:

| Action | How (Extension) | CLI Equivalent |
|---|---|---|
| Validate structure | Automatic on save (diagnostics) | `gert validate` |
| Create a scenario | Run live → "Save for Replay" | `gert scenario capture <ICM>` |
| Create a test case | "Save as Test Case" after save | Hand-write `test.yaml` |
| Run one scenario | `Gert: Run TSG` → replay mode | `gert run --replay --scenario ...` |
| Run all tests | `Gert: Run Tests` | `gert test --replay` |
| See test results on save | Automatic (background replay) | — |
| View coverage | Coverage overlay on workflow map | `gert coverage` |
| Debug a failure | "View Failed" → step-through replay | `gert debug --replay ...` |

The command line remains available for CI, automation, and power users. But the typical author never needs to leave VS Code.

### 4.5 When to Add More Scenarios

- **New branch added** → Run a live execution that hits the new branch → "Save for Replay" → "Save as Test Case."
- **Bug found in logic** → Replay an existing scenario that should fail → observe the wrong path → fix the runbook → replay again → see it pass.
- **ICM resolved with runbook** → The DRI clicks "Save for Replay" at the end of incident response. The scenario is committed with the runbook. It is now a test case.
- **Coverage map shows gaps** → The gray/uncovered branches in the workflow map tell the author exactly which inputs to craft.

---

## 5. CI/PR Gate: Tests During Review

Every pull request that modifies a `.runbook.yaml` or its scenarios must pass the following checks:

### 5.1 PR Pipeline

```yaml
# .azure-pipelines/runbook-ci.yaml  (or GitHub Actions equivalent)
steps:
  - name: Validate runbooks
    run: gert validate **/*.runbook.yaml

  - name: Replay all scenarios
    run: gert test --replay **/*.runbook.yaml

  - name: Check coverage
    run: gert coverage --fail-on-decrease **/*.runbook.yaml

  - name: Check rendered docs
    run: gert render --check **/*.runbook.yaml
```

| Gate | Blocks PR? | What It Prevents |
|---|---|---|
| `gert validate` | Yes | Broken schema, undefined variables, duplicate IDs |
| `gert test --replay` | Yes | Changed logic that breaks existing scenarios |
| `gert coverage --fail-on-decrease` | Yes (warning mode initially) | Runbook changes that reduce branch coverage |
| `gert render --check` | Yes (if docs are persisted) | TSG documentation out of sync with runbook logic |

### 5.2 What Reviewers See

The PR diff is self-contained:

```
connection/login-success-rate-below-target.runbook.yaml   ← logic change
connection/scenarios/login-success-rate-below-target/
  icm-749083743/
    test.yaml                                              ← updated expectations
  icm-751000123/                                           ← new scenario for new branch
    inputs.yaml
    steps/000-query-login-failures.json
    test.yaml
```

A reviewer can verify:
- The runbook logic change is correct.
- Existing scenarios were updated if behavior intentionally changed.
- New scenarios cover the new branches.
- CI passed all replays.

### 5.3 gert in the CI Pipeline

The `gert` binary **must be available on the CI agent** to run any of these gates. The runbook logic — condition evaluation, branch traversal, capture resolution, assertion checking — lives in the gert engine. There is no way to validate or replay a runbook with a generic YAML linter or a shell script. The gert binary is what turns a `.runbook.yaml` into a state machine and walks it.

**Distribution options:**

| Option | How | Trade-offs |
|---|---|---|
| **Checked-in binary** | Commit `bin/gert` (or `bin/gert.exe`) to the TSG repository | Simple, version-locked to the repo, works offline. Binary bloat in git (mitigated with Git LFS). |
| **Pipeline artifact** | Download from a release pipeline or Azure Artifacts feed as a pipeline step | No binary in git. Requires network access at CI time. Version pinned in pipeline YAML. |
| **Go install** | `go install github.com/ormasoftchile/gert/cmd/gert@v0.x` in the pipeline | Requires Go toolchain on the agent. Slowest option. |
| **Container image** | Run pipeline steps inside a container that includes gert | Clean isolation. Requires container registry. |

**Recommended approach for TSG repos:** Check in a platform-specific binary under `bin/` (or use Git LFS). The TSG repo is not a Go project — its authors are SREs, not Go developers. Requiring a Go toolchain or an artifact feed introduces friction that will block adoption. A checked-in binary makes CI self-contained: clone the repo, run the tests, done.

Example pipeline step:
```yaml
steps:
  - name: Validate and test runbooks
    run: |
      chmod +x ./bin/gert
      ./bin/gert validate **/*.runbook.yaml
      ./bin/gert test --replay **/*.runbook.yaml
      ./bin/gert coverage --fail-on-decrease **/*.runbook.yaml
```

**Version management:** The gert binary version should match the `apiVersion` in the runbooks. When gert ships a new schema version, update the binary in the repo and re-validate all runbooks. A `gert version` command prints the build version and supported schema versions.

### 5.4 No Auth Required for Replay

Replay tests use recorded Kusto responses from `steps/*.json` — they never touch a live cluster. This means:
- No service principal required.
- No network access required (beyond cloning the repo).
- Deterministic results on every run.
- Fast execution (seconds, not minutes).

---

## 6. Test Lifecycle

```
                 ┌─────────────────────────┐
                 │   ICM Incident Occurs    │
                 └────────────┬────────────┘
                              ▼
                 ┌─────────────────────────┐
Execution ──────▶│ DRI runs TSG in ext.    │  ← extension, live mode
                 └────────────┬────────────┘
                              ▼
                 ┌─────────────────────────┐
Capture ────────▶│ "Save for Replay"       │  ← extension button (exists today)
                 │  inputs.yaml + steps/   │
                 └────────────┬────────────┘
                              ▼
                 ┌─────────────────────────┐
Assertion ──────▶│ "Save as Test Case"     │  ← extension button (Feature 1)
                 │  writes test.yaml       │
                 └────────────┬────────────┘
                              ▼
                 ┌─────────────────────────┐
Author edit ────▶│ Edit runbook.yaml       │
                 │ Background replay runs  │  ← extension on-save (Feature 3)
                 │ Diagnostics appear      │
                 └────────────┬────────────┘
                              ▼
                 ┌─────────────────────────┐
PR gate ────────▶│ CI runs gert test       │  ← pipeline, replay mode
                 │ --replay + --coverage   │
                 └─────────────────────────┘
```

Every step after the initial execution happens either in the extension or CI. The author never shells out. The test corpus grows organically — every resolved incident becomes a test case with two clicks.

---

## 7. Structural Validation Details

`gert validate` performs deterministic checks with no external dependencies:

| Check | Example Failure |
|---|---|
| Schema compliance | `steps[2]: unknown field 'commnd'` |
| Unique step IDs | `step 'check_pods' used at index 0 and 3` |
| Variable flow | `step 'query': references '{{ .server_name }}' but no input or capture provides it` |
| Dangling file references | `next_runbook.file: 'error-40613.runbook.yaml' not found` |
| Outcome completeness | `branch at step 'triage' has no terminal outcome` |
| Governance consistency | `'kubectl' appears in both allowed_commands and denied_commands` |
| Regex validity | `redact pattern '[invalid(regex' does not compile` |

These checks map directly to the test fixtures in the project:

- `testdata/valid/` — runbooks that must pass validation (positive tests)
- `testdata/invalid/` — runbooks that must fail validation with specific errors (negative tests)

When adding a new validation rule, the author adds both a test fixture and a corresponding unit test. The fixture is the "is this valid?" test; the Go unit test asserts the exact error message.

---

## 8. Scenario Replay Details

### 8.1 Replay Execution Model

```
  inputs.yaml  ──▶  Engine resolves meta.inputs
                          │
  runbook.yaml ──▶  Engine evaluates step
                          │
                    ┌─────┴──────┐
                    │ Step type?  │
                    └─────┬──────┘
                     xts/cli │        manual
                          ▼              ▼
                 steps/NNN-id.json   evidence from
                 (frozen response)   scenario or
                                     dry-run placeholder
                          │              │
                          ▼              ▼
                    Engine captures, evaluates conditions,
                    follows branches, reaches outcome
                          │
                          ▼
                    Compare against test.yaml assertions
```

### 8.2 What Replay Catches

| Defect | How It Manifests |
|---|---|
| Broken condition expression | Replay follows wrong branch → unexpected outcome |
| Capture key renamed | Downstream step references missing capture → error |
| New branch with no fallback | Engine reaches dead end → no outcome |
| Assertion on wrong field | `expected_captures` mismatch → test failure |
| Step removed or reordered | `must_reach` step not visited → test failure |

### 8.3 Determinism Guarantee

Replay tests are fully deterministic:
- No network calls (Kusto responses are frozen files).
- No time dependency (timestamps come from `inputs.yaml`).
- No user interaction (manual steps use recorded evidence or dry-run placeholders).
- No randomness (the engine is a state machine with deterministic transitions).

Two engineers running the same replay on different machines will always get the same result.

---

## 9. Coverage as a Quality Signal

`gert coverage` reports three metrics:

| Metric | Definition |
|---|---|
| **Branch coverage** | Percentage of condition branches exercised by at least one scenario |
| **Step coverage** | Percentage of steps visited by at least one scenario |
| **Incident coverage** | Percentage of historical ICMs (by failure cause) that have a matching scenario |

### Coverage in CI

```
gert coverage --fail-on-decrease login-success-rate-below-target.runbook.yaml
```

This compares the current coverage against the value stored in `meta.certification.branch_coverage`. If the PR reduces coverage, the check fails. This prevents authors from adding new branches without tests.

### Coverage Report Example

```
Branch coverage:   17/42 (40%)
Step coverage:     28/59 (47%)
Incident coverage: 37/40 (93%)

Missing branches:
  - IsDW (0 scenarios)
  - HasHighLatencyLoginsTopWaitStat_HADR_LOGPR (0 scenarios)

Untested steps:
  - transfer_to_oss
  - transfer_to_mi
```

---

## 10. Certification Levels

Tests directly determine the runbook's certification level:

| Level | Name | Test Requirement |
|---|---|---|
| 0 | **Draft** | `gert validate` passes |
| 1 | **Tested** | At least 1 scenario replays successfully |
| 2 | **Verified** | All branches have scenarios, replay tests pass, coverage > 80% |
| 3 | **Certified** | Live-tested against ≥ 3 real incidents, peer-reviewed, coverage > 90% |

The certification level is stored in the runbook and displayed in the VS Code extension. It tells the DRI how much confidence they can place in the automated guidance.

---

## 11. Nightly and Scheduled Tests

Beyond PR gates, scheduled tests catch environmental drift:

| Schedule | Test | Purpose |
|---|---|---|
| **Nightly** | `gert test --live` | Detect Kusto schema changes, table renames, column removals |
| **Weekly** | `gert coverage --report` | Track coverage trends across all runbooks |
| **On incident close** | `gert scenario capture` (automated) | Grow the test corpus from real operations |

Live smoke tests require authentication to Kusto clusters. They run against a designated non-production ring or during off-peak hours to avoid impacting active incident response.

---

## 12. Extension Feature Summary

| Feature | Status | Priority | Dependency |
|---|---|---|---|
| Validation on save (diagnostics) | **Built** | — | — |
| Replay mode execution | **Built** | — | — |
| Run All (replay auto-advance) | **Built** | — | — |
| Save for Replay | **Built** | — | `exec/saveScenario` RPC (exists) |
| Scenario picker | **Built** | — | Convention-based discovery (exists) |
| **Generate `test.yaml`** | Needed | P0 | Outcome + captures from event stream |
| **Batch replay (Run Tests)** | Needed | P0 | Headless replay loop, `test.yaml` comparison |
| **Test-on-save (background replay)** | Needed | P1 | `gert test --replay --json` structured output |
| **Coverage overlay** | Needed | P1 | `gert coverage --json` per-branch data |
| **Scenario diff view** | Needed | P2 | Structured comparison of expected vs. actual |

---

## 13. Quick Reference

### For Runbook Authors (Extension)

| Action | How |
|---|---|
| Validate | Automatic on save — diagnostics in Problems panel |
| Run live | Command Palette → `Gert: Run TSG` → `real` or `icm` |
| Save scenario | Click "Save for Replay" after run completes |
| Save test case | Click "Save as Test Case" after save (Feature 1) |
| Replay one scenario | `Gert: Run TSG` → `replay` → pick scenario |
| Run all tests | `Gert: Run Tests` (Feature 2) |
| View coverage | Coverage overlay in workflow map (Feature 4) |

### For Runbook Authors (CLI)

```bash
# Validate structure (run often, runs instantly)
gert validate my-tsg.runbook.yaml

# Capture a scenario from a real incident
gert scenario capture <ICM_ID> --runbook my-tsg.runbook.yaml

# Run all replay tests for a runbook
gert test --replay my-tsg.runbook.yaml

# Check branch coverage
gert coverage my-tsg.runbook.yaml
```

### For CI Configuration

```bash
# PR gate — all fast, no auth needed
gert validate **/*.runbook.yaml
gert test --replay **/*.runbook.yaml
gert coverage --fail-on-decrease **/*.runbook.yaml

# Nightly — requires Kusto auth
gert test --live **/*.runbook.yaml
```

### For Reviewers

When reviewing a runbook PR, check:
1. Does `gert validate` pass? (CI will tell you)
2. Are existing scenarios updated if logic changed?
3. Are new scenarios added for new branches?
4. Does coverage hold or improve?
5. Does the rendered TSG prose read correctly?

---

## 14. Summary

| Question | Answer |
|---|---|
| Where do tests live? | Next to the runbook: `scenarios/{runbook-name}/` |
| How do I create a test? | Run live in the extension → "Save for Replay" → "Save as Test Case" |
| How do I run tests locally? | Extension: `Gert: Run Tests`. CLI: `gert test --replay` |
| Do I need the command line? | No. The extension covers validate, capture, replay, and batch test. CLI is for CI and automation. |
| How do tests run in CI? | `gert validate` + `gert test --replay` + `gert coverage --fail-on-decrease` on every PR |
| Do tests run during authoring? | Yes — validation on save (built), replay on save (Feature 3) |
| Do tests need auth or network? | Replay tests: no. Live smoke tests: yes (Kusto auth). |
| How does the test corpus grow? | Organically — every resolved incident becomes a test with two clicks in the extension |
| What blocks a PR? | Validation failure, replay test failure, coverage decrease |
