---
name: collect-icm
description: Collect ICM incident IDs for a service/team using Playwright, then enrich via MCP and save a categorized collection
---

# Collect ICM Incidents

Searches ICM via Playwright browser automation to discover incident IDs, enriches each via MCP tools, and saves a categorized YAML collection for use by the TSG quality harness.

## When to Use

Use this skill when:
- Building a corpus of real incidents to validate TSGs against
- Populating `meta.icm.seeds` for runbooks
- Analyzing which TSGs are used for which incident patterns
- Refreshing the incident collection with recent data

## Prerequisites

- User must be logged into ICM in their browser (https://portal.microsofticm.com)
- MCP ICM tools must be available (`get_incident_details_by_id`)
- Playwright MCP tools must be available (`browser_navigate`, `browser_snapshot`, `browser_run_code`, etc.)

## Variables

- **{ServiceName}**: ICM service to search (default: `Azure SQL DB`)
- **{TeamFilter}**: Optional team name filter (e.g., `Gateway`, `SQL DB Availability`)
- **{DateRange}**: How far back to search (default: `Last 30 Days`)
- **{Severities}**: Which severities to include (default: `0, 1, 2`)
- **{States}**: Which states to include (default: `Resolved, Mitigated`)
- **{OutputPath}**: Where to save the collection YAML (default: `docs/design/icm-collection-{date}.yaml`)
- **{TsgRepoPath}**: Path to TSG repo for tsgLink matching (e.g., `c:\One\TSG-SQL-DB-Connectivity`)

## How It Works

### Step 1: Navigate to ICM Search

1. Use Playwright to navigate to `https://portal.microsofticm.com/imp/v3/incidents/search/advanced`
2. Take a snapshot to confirm the page loaded and user is authenticated
3. If not authenticated, stop and tell the user: "Please log into ICM in your browser first."

### Step 2: Configure Search Filters

Using the ICM Advanced Search form:

1. **Service/Team**: Type `{ServiceName}` into the "Search services or teams" textbox, click Search
2. **Date Range**: If {DateRange} differs from default, update the date range field
3. **State**: Ensure checkboxes match {States} (check/uncheck Active, Mitigated, Resolved)
4. **Severity**: Ensure checkboxes match {Severities}
5. **Environment**: Keep Prod checked
6. Click the **Run** button to execute the search

### Step 3: Extract Incident IDs

Run this Playwright code to scrape all incident IDs from the results page:

```javascript
async (page) => {
  const links = await page.locator('a[href*="/incidents/details/"]').all();
  const ids = new Set();
  for (const link of links) {
    const href = await link.getAttribute('href');
    const m = href.match(/details\/(\d+)/);
    if (m) ids.add(m[1]);
  }
  return JSON.stringify([...ids]);
}
```

If the result set is large (45+ IDs), check if there are pagination controls and collect from additional pages.

Save the raw IDs list.

### Step 4: Enrich via MCP

For each incident ID, call `get_incident_details_by_id` and extract:

- `id`: Incident ID
- `title`: Incident title
- `severity`: Severity level
- `state`: Current state (ACTIVE, MITIGATED, RESOLVED)
- `owningTeamName`: The ICM team queue
- `tsgLink`: The TSG URL (eng.ms link) — **this is the key field for TSG matching**
- `customFields`: Extract `CustomerServerName`, `CustomerDatabaseName`, `ServerName`, `DatabaseName`, `PrimaryTenantRing` and any other StringValue fields
- `mitigateData.mitigationSteps`: How it was actually fixed (ground truth)
- `howFixed`: Resolution category
- `createDate`, `mitigateDate`, `resolveDate`: Timeline

**Rate limiting**: Process 5 incidents at a time, not all at once.

If {TeamFilter} is set, skip incidents where `owningTeamName` does not contain {TeamFilter}.

### Step 5: Categorize by TSG

Group the enriched incidents by `tsgLink`:

1. Parse the eng.ms URL from `tsgLink` to extract the TSG path
2. If {TsgRepoPath} is provided, try to match the URL path to actual files in the repo
3. Group incidents under their TSG match
4. Mark incidents with no `tsgLink` as "uncategorized"

Build a categorization map:
```yaml
tsg_categories:
  login-success-rate-below-target:
    tsg_url: "https://eng.ms/docs/.../login-success-rate-below-target"
    local_file: "TSG/connection/login-success-rate-below-target.md"  # if matched
    incidents:
      - id: 747870160
        severity: 2
        state: MITIGATED
        server: sobeys-sql01
        database: AlarmDB
        how_fixed: Transient
  missing-server-dns-alias-records:
    tsg_url: "https://eng.ms/docs/.../missing-server-dns-alias-records"
    local_file: "TSG/alias/missing-server-dns-alias-records.md"
    incidents:
      - id: 123456789
        ...
  uncategorized:
    incidents:
      - id: 999999999
        title: "..."
        team: "..."
```

### Step 6: Save Collection

Write the full collection to {OutputPath} as YAML:

```yaml
# ICM Incident Collection
# Collected: {date} via Playwright + MCP
# Service: {ServiceName}
# Team filter: {TeamFilter}
# Date range: {DateRange}
# Severities: {Severities}
# States: {States}

collection_metadata:
  date: "2026-02-15"
  service: "Azure SQL DB"
  total_incidents: 45
  categorized: 38
  uncategorized: 7

tsg_categories:
  # ... as above ...

raw_ids:
  - 748247603
  - ...
```

### Step 7: Summary Report

Present to the user:

```
ICM Collection Complete
=======================
Total incidents scraped: {N}
Enriched via MCP: {N}
Categorized by TSG: {N} across {M} unique TSGs
Uncategorized: {N}
Saved to: {OutputPath}

Top TSGs by incident count:
  1. login-success-rate-below-target (12 incidents)
  2. trdb0002-common-unavailability (8 incidents)
  3. missing-server-dns-alias-records (3 incidents)
  ...

Custom fields found across incidents:
  CustomerServerName: 45/45
  CustomerDatabaseName: 42/45
  PrimaryTenantRing: 40/45
```

## Notes

- The Playwright step requires the user to be logged into ICM — there is no programmatic auth for the web UI
- MCP `get_incident_details_by_id` handles auth automatically
- The `tsgLink` field is the best way to match incidents to TSGs (better than team name)
- Custom field names vary by team/service — the enrichment step captures all of them
- Collections should be refreshed periodically (monthly) to catch new incident patterns
- ICM browser tokens expire after ~3 hours — if Playwright fails mid-collection, re-login
