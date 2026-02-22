# TSG Quality Harness — Self-Validating TSG Platform

> !!!AI Generated. To be verified!!!

## Vision

A **self-validating TSG harness** — an automated agent that:

1. **Discovers** mitigation TSGs in the repo
2. **Mines ICM** for real incidents that match each TSG
3. **Extracts inputs** from those incidents (server, database, subscription, etc.)
4. **Executes the TSG** through gert with those inputs
5. **Captures** the full run (replay scenario + summary)
6. **Validates** outcomes against the actual ICM resolution
7. **Detects defects** (broken queries, missing captures, wrong branches taken)
8. **Proposes enhancements** (better capture patterns, missing edge cases)
9. **Persists** everything, then loops

This is **TSG-as-code with ground truth validation**.

---

## Existing Layers

Every foundational layer is already in place:

| Layer | Mechanism |
|---|---|
| TSG source → compiled runbook | `gert compile` |
| Runbook → step-by-step execution | `gert serve` + engine |
| Execution → captured artifacts | `runBaseDir/steps/*.json` |
| Artifacts → replay scenarios | "Save for Replay" |
| Replay → reproducible validation | `mode: replay` + Run All |
| Summary → structured output | Copy Summary (markdown) |

What's missing is the **top of the funnel** (ICM → inputs) and the **bottom of the funnel** (outcome → ground truth comparison → learning).

---

## Architecture

```
┌─────────────────────────────────────────────────┐
│                   ORCHESTRATOR                   │
│  For each mitigation TSG:                        │
│    1. Parse meta.inputs to know what's needed    │
│    2. Query ICM for matching incidents           │
│    3. For each ICM:                              │
│       a. Extract input params (API)              │
│       b. Execute TSG via gert                    │
│       c. Capture replay + summary                │
│       d. Compare outcome vs ICM resolution       │
│       e. Score & classify result                 │
│    4. Aggregate findings per TSG                 │
│    5. Emit report + proposed changes             │
└─────────────────────────────────────────────────┘
```

---

## ICM API Integration

### API Details

- **Base URL**: `https://prod.microsofticm.com/api`
- **Protocol**: OData v3 (v2 Incidents API — the one with search/query support)
- **Auth**: MSI (Managed Service Identity) → Bearer token
- **Role needed**: **Readers** (search/query unrestricted incidents, scoped automatically)

### Key Endpoints

| Operation | Method | Endpoint |
|---|---|---|
| Search incidents | GET | `/api/cert/incidents?$filter=...&$orderby=...&$top=N` |
| Get incident | GET | `/api/cert/incidents({incidentId})` |
| Get incident + expand | GET | `/api/cert/incidents({id})?$expand=CustomFields` |

### Go ICM Client (`pkg/icm/`)

A lightweight Go client in gert that wraps the OData API:

```go
// pkg/icm/client.go
type Client struct {
    BaseURL string
    Token   string // Bearer token from MSI
}

// Search returns incidents matching an OData filter.
func (c *Client) Search(filter string, top int) ([]Incident, error)

// Get returns full incident details including custom fields.
func (c *Client) Get(incidentId int64) (*Incident, error)

// Incident holds the fields we care about for TSG validation.
type Incident struct {
    Id            int64
    Title         string
    Severity      int
    Status        string        // Active, Mitigated, Resolved
    OwningTeamId  string
    CreateDate    time.Time
    ResolveDate   *time.Time
    MitigateDate  *time.Time
    Resolution    string        // resolution notes
    CustomFields  map[string]string // server_name, subscription_id, etc.
}
```

Auth uses `azure-identity` Go SDK (`azidentity.NewDefaultAzureCredential`) to get a token for the ICM resource. Works from dev machines (az login), Azure VMs (system MSI), and CI pipelines.

---

## Phase Details

### Phase 1: Discovery & Matching

- Scan all `*.runbook.yaml` where `meta.kind: mitigation`
- Extract `meta.inputs` — these declare what the TSG needs (server name, subscription ID, etc.)
- Each TSG declares its ICM search filter in `meta.icm.filter`:
  ```yaml
  meta:
    icm:
      filter: "OwningTeamId eq '{teamId}' and substringof('DNS alias', Title)"
      seeds: [226532540, 364174443]  # optional known-good incident IDs
  ```
- The harness uses `icm.Search(filter, top=20)` to find matching resolved incidents
- Falls back to `seeds` + `icm.Get(id)` if filter is not defined
- Uses `get_similar_incidents` (MCP tool) to expand from seeds when needed

### Phase 2: ICM Parameter Extraction

For each incident returned by search:

1. Call `icm.Get(id)` with `$expand=CustomFields` to get full details
2. Map custom fields to TSG inputs using `meta.icm.input_mapping`:
   ```yaml
   input_mapping:
     server_name: "CustomField.LogicalServerName"
     subscription_id: "CustomField.SubscriptionId"
   ```
3. Extract resolution metadata for ground truth:
   - `Status` → was it resolved or escalated?
   - `Resolution` → free-text resolution notes
   - `ResolveDate - CreateDate` → TTR (time to resolve)
4. Skip incidents with missing required inputs (log as gap)

### Phase 3: Execution & Capture

Already built. The orchestrator calls:

```bash
gert run --runbook path.runbook.yaml --var server=xyz --var db=abc
```

Or via `gert serve` API:
```
exec/start(runbook, mode: 'real', vars: extractedParams) → exec/next (loop)
```

Each run produces:
- Step-by-step JSON responses in `steps/`
- Final outcome (resolved / escalated / needs_rca)
- Captured data (query results, counts, etc.)
- Auto-saved as replay scenario for future regression tests

### Phase 4: Validation

Compare TSG outcome against ICM resolution:

| TSG says | ICM says | Verdict |
|---|---|---|
| resolved | Resolved by OCE | ✓ TSG correct |
| escalated | Resolved by OCE | ✗ False escalation |
| resolved | Escalated to PG | ✗ TSG missed something |
| error (step failed) | any | 🚩 TSG defect |

Metrics per TSG:
- **Accuracy rate** — % of ICMs where TSG outcome matches reality
- **False escalation rate** — TSG says escalate, ICM was actually resolved
- **Miss rate** — TSG says resolved, but incident needed escalation
- **Failure rate** — TSG crashes or steps error out

### Phase 5: Defect Detection

From captured runs, automatically detect:

1. **Broken XTS queries** — step returns error, stderr has SQL/Kusto failures
2. **Empty captures** — query ran but returned `[]` when it shouldn't have (stale timeframes, wrong columns)
3. **Unreachable branches** — across N ICMs, certain outcome branches are never taken (dead code)
4. **Missing edge cases** — ICMs that don't match any TSG at all (gap in coverage)
5. **Flaky steps** — same inputs give different outcomes on different runs (timing-dependent queries)

### Phase 6: Enhancement Proposals

With enough data, generate actionable suggestions:
- "Step `check-dns-alias-records` returns empty for 40% of incidents — the time window may be too narrow"
- "Outcome `resolved` is reached for incidents that were actually escalated — add a check for X"
- "This TSG has no scenario coverage for the `needs_rca` path"
- "Input `bad_cache_instance_name` is never populated from ICM — consider making it optional or deriving it"

---

## Why This Matters

1. **Not synthetic testing** — uses real incidents with real data. Replay scenarios generated are production-representative.

2. **Virtuous cycle**:
   - Run TSGs against real ICMs → find defects → fix TSGs → re-run → verify fix → save as regression test (replay)
   - Every fixed ICM becomes a permanent test case

3. **Institutional knowledge** — the replay library becomes a corpus of "here's what this incident looked like and how the TSG handled it." New OCEs learn by replaying real incidents.

4. **Quantitative TSG quality** — today nobody knows if a TSG works until someone uses it at 3 AM under pressure. This gives accuracy scores, failure rates, coverage metrics.

5. **Scalability** — once the harness works for one TSG, it works for all of them. Input extraction and outcome comparison are parameterized.

---

## Practical Concerns

### Data Freshness
XTS queries return real-time data. Running the harness against old ICMs may produce different results (server moved, issue fixed). Strategy:
- Run Phase 3 once in real mode while incident data is still fresh, save replays
- All subsequent validation runs use replay mode (zero prod impact)
- For active incidents, capture immediately

### Rate Limiting / Cost
Running 50 TSGs x 10 ICMs = 500 real XTS executions. That's production load. Mitigation:
- Run real mode **once per incident**, auto-save replay
- All re-validation uses replay mode
- Batch runs during off-peak hours

### Ground Truth Quality
ICM resolution notes are messy. "Resolved" might mean auto-resolved by timeout. Need classification heuristics:
- Parse `Status` field (Resolved / Mitigated / Transferred)
- Parse `Resolution` text for keywords ("escalated to PG", "customer confirmed", "auto-resolved")
- Flag ambiguous cases for manual review

### Privacy / Compliance
ICM data contains customer identifiers. Replay scenarios:
- Stored in **private** repo or scrubbed of PII before committing
- Input vars can be hashed/anonymized for the replay library
- XTS response JSONs already go through the collector which can redact

---

## Implementation Sequence

| Phase | What | Deliverable | Effort |
|---|---|---|---|
| **0** | Add `meta.icm` schema extension, populate for 2-3 TSGs | Schema + 2-3 TSGs annotated | Small |
| **1** | Build `pkg/icm/` Go client (search, get, auth via azidentity) | `gert icm search`, `gert icm get` CLI commands | Medium |
| **2** | Build `gert harness` orchestrator (ICM → gert run → capture) | `gert harness run --tsg path.runbook.yaml` | Medium |
| **3** | Build validator (compare TSG outcome vs ICM resolution) | Scorecard output per run | Small |
| **4** | Run against real ICMs for 2-3 TSGs, review results | Validated proof of concept | Manual |
| **5** | Build aggregate report generator (per-TSG scorecard) | `gert harness report` | Small |
| **6** | Scale to all mitigation TSGs | Full coverage | Iterate |

---

## Schema Extension (Phase 0)

```yaml
meta:
  kind: mitigation
  icm:
    filter: "OwningTeamId eq '{teamId}' and substringof('DNS alias', Title)"
    seeds: [226532540, 364174443]
    input_mapping:
      server_name: "CustomField.LogicalServerName"
      subscription_id: "CustomField.SubscriptionId"
      bad_cache_instance_name: "CustomField.CacheInstanceName"
    outcome_mapping:
      resolved: ["Resolved", "Mitigated"]
      escalated: ["Transferred", "Escalated"]
      needs_rca: ["RCA Required"]
```

The `filter` field is an OData `$filter` expression for `GET /api/cert/incidents`. The `seeds` field provides known incident IDs for bootstrapping when the filter is not yet tuned. The `input_mapping` maps TSG input names to ICM custom field paths. The `outcome_mapping` maps TSG outcome states to ICM status/resolution values.

---

## CLI Commands

```bash
# Search ICM for incidents matching a TSG's filter
gert icm search --tsg path.runbook.yaml --top 20

# Get full details for a specific incident
gert icm get 226532540

# Run the full harness: search → extract → execute → validate → report
gert harness run --tsg path.runbook.yaml --top 10

# Generate aggregate report across all TSGs
gert harness report --dir ./harness-results/
```

---

## Summary

The gert platform has accidentally built half of a **TSG quality assurance platform**. The execution engine, replay system, scenario capture, and summary format are the hard parts — and they exist. The remaining piece is connecting to real incident data and building the comparison loop. That's architecturally straightforward and where the actual learning happens.

---

## Validated: ICM Collection (2026-02-15)

First collection run completed. Results in `docs/design/icm-collection-2026-02-15.yaml`.

### Method
- **Playwright** scraped ICM Advanced Search (Azure SQL DB, Sev 0-2, Prod, 7 days) → 45 incident IDs
- **MCP** `get_incident_details_by_id` enriched 20 of 45 → extracted tsgLink, customFields, state, howFixed
- **Categorized** by `tsgLink` into 6 TSG categories + 5 uncategorized

### ICM → TSG Mapping (20 enriched incidents)

| TSG | Repo | ICMs | Key Finding |
|---|---|---|---|
| login-success-rate-below-target | CONN | 6 | Most common; team=Availability but TSG=Connectivity |
| trdb0002-unavailability | AVAIL | 3 | All auto-mitigated by automation |
| geodr0039-out-of-rpo | GEODR | 2 | SPO-specific, uses Properties JSON |
| soc001-unhealthy-socrates | AVAIL | 2 | Hyperscale, self-healed |
| geodr0003-log-full | GEODR | 1 | |
| geodr0002-workflow-stuck | GEODR | 1 | Still ACTIVE |
| (no tsgLink) | Mixed | 5 | Manual incidents or SPO alerts |

### Cross-Repo TSG Scan (2,200+ TSGs)

Scanned all 4 repos for TSGs with executable automation potential (XTS/Kusto queries + structured steps):

| Repo | Total TSGs | With queries | Top candidate |
|---|---|---|---|
| Connectivity | 402 | 73 | login-success-rate-below-target (already compiled) |
| Availability | 487 | 250 | CRGW0001 (23 queries, 20 steps) |
| Performance | 1,167 | 468 | CPU104-Scheduler-Imbalances (25 queries) |
| GeoDR | 150+ | 31 | GeoDR0083-sysdbreg-mismatch (7 queries) |

### Recommended Compile Order

| Priority | TSG | Repo | Queries | Steps | ICM matches | Why |
|---|---|---|---|---|---|---|
| 1 | login-success-rate-below-target | CONN | — | — | 6 | **Already compiled**. Top ICM hit. |
| 2 | CRGW0001-Login-success-rate-below-99% | AVAIL | 23 | 20 | 6 | Deeper version of #1 |
| 3 | low-customer-database-availability-rca | CONN | 14 | 5 | — | RCA workflow |
| 4 | Redo-Lag-Analysis | AVAIL | 13 | 13 | — | Structured diagnostic |
| 5 | PPBOT-040-Tempdb-Out-of-Data-Space | PERF | 7 | 10 | — | Common issue |
| 6 | login-rate-drop-of-95 | CONN | 6 | 5 | — | Alert-driven triage |
| 7 | Category-HasSqlDump | AVAIL | 8 | 8 | — | Dump analysis |
| 8 | GEODR0063-MI-FG-Connectivity | GEODR | 3 | 5 | — | MI connectivity |
| 9 | CPU104-Scheduler-Imbalances | PERF | 25 | 3 | — | Deep perf diagnostic |
| 10 | AutoDR-FG-Failover-v2 | GEODR | 4 | 4 | — | Failover group triage |
