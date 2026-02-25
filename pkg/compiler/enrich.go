package compiler

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ormasoftchile/gert/pkg/schema"
	"gopkg.in/yaml.v3"
)

// EnrichResult summarizes what gert enrich did.
type EnrichResult struct {
	DispatchVar    string   // e.g. "login_failure_cause"
	DispatchStepID string   // e.g. "decision_health_property_and_failures"
	ExistingValues []string // condition values already in branches
	ScenarioValues []string // unique values found across all scenarios
	AddedValues    []string // new branches added
	LinkedTSGs     []string // causes that found a matching TSG file
	BoundRunbooks  int      // branches converted from manual to invoke
	RunbookPath    string   // path written
}

// EnrichFromScenarios reads a runbook and its scenarios directory, finds
// the "dispatch variable" (the input whose values map 1:1 to branch conditions),
// identifies scenario values not covered by existing branches, and adds
// escalation branches for each missing value.
//
// The enrichment is deterministic and idempotent — running it twice produces
// the same result.
func EnrichFromScenarios(runbookPath string) (*EnrichResult, error) {
	// ── 1. Load runbook ──
	data, err := os.ReadFile(runbookPath)
	if err != nil {
		return nil, fmt.Errorf("read runbook: %w", err)
	}
	var rb schema.Runbook
	if err := yaml.Unmarshal(data, &rb); err != nil {
		return nil, fmt.Errorf("parse runbook: %w", err)
	}

	// ── 2. Discover scenarios directory ──
	// Use filename stem (not meta.name) to match testing.DiscoverScenarios convention:
	// {runbook-dir}/scenarios/{filename-without-.runbook.yaml}/
	rbDir := filepath.Dir(runbookPath)
	base := filepath.Base(runbookPath)
	stem := strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	stem = strings.TrimSuffix(stem, ".runbook")
	scenariosDir := filepath.Join(rbDir, "scenarios", stem)
	entries, err := os.ReadDir(scenariosDir)
	if err != nil {
		return nil, fmt.Errorf("read scenarios dir %s: %w", scenariosDir, err)
	}

	// ── 3. Read all scenario input values ──
	scenarioInputs := make([]map[string]string, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		inputPath := filepath.Join(scenariosDir, e.Name(), "inputs.yaml")
		inputData, err := os.ReadFile(inputPath)
		if err != nil {
			continue // skip dirs without inputs.yaml
		}
		var inputs map[string]string
		if err := yaml.Unmarshal(inputData, &inputs); err != nil {
			continue
		}
		scenarioInputs = append(scenarioInputs, inputs)
	}
	if len(scenarioInputs) == 0 {
		return nil, fmt.Errorf("no scenarios found in %s", scenariosDir)
	}

	// ── 4. Find the dispatch point: a step with branches whose conditions
	//       match the pattern `<var> == "<value>"` or `<var> startsWith "<value>"` ──
	dispatchVar, dispatchNode, _ := findDispatchPoint(rb.Tree)
	if dispatchVar == "" {
		return nil, fmt.Errorf("no dispatch point found (step with branching conditions on a single variable)")
	}

	// ── 5. Remove previously enriched branches (idempotent re-runs) ──
	cleaned := make([]schema.Branch, 0, len(dispatchNode.Branches))
	for _, b := range dispatchNode.Branches {
		if !isAutoEnrichedBranch(b) {
			cleaned = append(cleaned, b)
		}
	}
	dispatchNode.Branches = cleaned

	// Re-extract existing values from cleaned (non-enriched) branches only
	_, existingValues := extractDispatchInfo(cleaned)

	// ── 6. Collect unique scenario values for the dispatch variable ──
	scenarioValueSet := make(map[string]bool)
	for _, inputs := range scenarioInputs {
		if v, ok := inputs[dispatchVar]; ok && v != "" {
			scenarioValueSet[v] = true
		}
	}

	// ── 7. Find values not covered by existing branches ──
	//       A scenario value is "covered" if:
	//       - An exact match condition exists, OR
	//       - A startsWith condition matches the prefix
	missingValues := make([]string, 0)
	for sv := range scenarioValueSet {
		if isCovered(sv, existingValues, dispatchNode.Branches) {
			continue
		}
		missingValues = append(missingValues, sv)
	}
	sort.Strings(missingValues)

	if len(missingValues) == 0 {
		allScenarioVals := make([]string, 0, len(scenarioValueSet))
		for v := range scenarioValueSet {
			allScenarioVals = append(allScenarioVals, v)
		}
		sort.Strings(allScenarioVals)

		return &EnrichResult{
			DispatchVar:    dispatchVar,
			DispatchStepID: dispatchNode.Step.ID,
			ExistingValues: existingValues,
			ScenarioValues: allScenarioVals,
			AddedValues:    nil,
			RunbookPath:    runbookPath,
		}, nil
	}

	// ── 8. Resolve TSG links for missing values ──
	tsgLinks := extractTSGLinks(runbookPath)
	var linkedTSGs []string
	for _, val := range missingValues {
		tsgPath := findMatchingTSG(rbDir, val, tsgLinks)
		if tsgPath != "" {
			linkedTSGs = append(linkedTSGs, val)
		}
		branch := buildEnrichBranch(dispatchVar, val, tsgPath)
		dispatchNode.Branches = append(dispatchNode.Branches, branch)
	}

	// ── 9. Bind sub-runbooks: convert "Follow [X](path.md)" manual steps
	//       to type=invoke when the corresponding .runbook.yaml exists. ──
	boundCount, _ := bindSubRunbooks(dispatchNode, rbDir)

	// ── 10. Write enriched runbook (backup first) ──
	if err := WriteRunbook(&rb, runbookPath, true); err != nil {
		return nil, fmt.Errorf("write enriched runbook: %w", err)
	}

	allScenarioVals := make([]string, 0, len(scenarioValueSet))
	for v := range scenarioValueSet {
		allScenarioVals = append(allScenarioVals, v)
	}
	sort.Strings(allScenarioVals)

	return &EnrichResult{
		DispatchVar:    dispatchVar,
		DispatchStepID: dispatchNode.Step.ID,
		ExistingValues: existingValues,
		ScenarioValues: allScenarioVals,
		AddedValues:    missingValues,
		LinkedTSGs:     linkedTSGs,
		BoundRunbooks:  boundCount,
		RunbookPath:    runbookPath,
	}, nil
}

// findDispatchPoint walks the tree to find the first node whose branches
// use conditions in the pattern `<var> == "<value>"` or `<var> startsWith "<value>"`.
// Returns the variable name, a pointer to the node, and the existing condition values.
func findDispatchPoint(tree []schema.TreeNode) (string, *schema.TreeNode, []string) {
	for i := range tree {
		node := &tree[i]
		if len(node.Branches) >= 3 { // dispatch points have many branches
			varName, values := extractDispatchInfo(node.Branches)
			if varName != "" {
				return varName, node, values
			}
		}
		// Recurse into branches
		for j := range node.Branches {
			v, n, vals := findDispatchPoint(node.Branches[j].Steps)
			if v != "" {
				return v, n, vals
			}
		}
		// Recurse into iterate blocks
		if node.Iterate != nil {
			v, n, vals := findDispatchPoint(node.Iterate.Steps)
			if v != "" {
				return v, n, vals
			}
		}
	}
	return "", nil, nil
}

// conditionPattern matches `var == "value"` or `var startsWith "value"`.
var conditionPattern = regexp.MustCompile(`^(\w+)\s+(==|startsWith|contains)\s+"([^"]*)"$`)

// extractDispatchInfo analyzes branches to find a common dispatch variable.
// Returns the variable name and all condition values if a majority of branches
// use the same variable.
func extractDispatchInfo(branches []schema.Branch) (string, []string) {
	varCounts := make(map[string]int)
	varValues := make(map[string][]string)

	for _, b := range branches {
		cond := strings.TrimSpace(b.Condition)
		m := conditionPattern.FindStringSubmatch(cond)
		if m == nil {
			continue
		}
		v := m[1]
		val := m[3]
		varCounts[v]++
		varValues[v] = append(varValues[v], val)
	}

	// Find the variable used in most branches
	var bestVar string
	var bestCount int
	for v, count := range varCounts {
		if count > bestCount {
			bestVar = v
			bestCount = count
		}
	}

	// Require that the dominant variable covers a majority of branches
	if bestVar != "" && bestCount >= len(branches)/2 {
		return bestVar, varValues[bestVar]
	}
	return "", nil
}

// isCovered checks if a scenario value is covered by existing branches.
func isCovered(value string, existingValues []string, branches []schema.Branch) bool {
	for _, b := range branches {
		cond := strings.TrimSpace(b.Condition)
		m := conditionPattern.FindStringSubmatch(cond)
		if m == nil {
			continue
		}
		op := m[2]
		condVal := m[3]

		switch op {
		case "==":
			if value == condVal {
				return true
			}
		case "startsWith":
			if strings.HasPrefix(value, condVal) {
				return true
			}
		case "contains":
			if strings.Contains(value, condVal) {
				return true
			}
		}
	}
	return false
}

// isAutoEnrichedBranch detects branches added by previous enrich runs.
// Old format used "(from scenario)" suffix; current format uses "(enriched)" suffix.
func isAutoEnrichedBranch(b schema.Branch) bool {
	return strings.HasSuffix(b.Label, "(from scenario)") ||
		strings.HasSuffix(b.Label, "(enriched)")
}

// buildEnrichBranch creates a branch for an uncovered cause value.
// When tsgPath is non-empty, generates a proper TSG link branch matching
// the pattern of existing manually-authored branches.
// When tsgPath is empty, generates a placeholder marked "(enriched)".
func buildEnrichBranch(dispatchVar, cause, tsgPath string) schema.Branch {
	stepID := toTSGStepID(cause)
	if tsgPath != "" {
		return schema.Branch{
			Condition: fmt.Sprintf("%s == %q", dispatchVar, cause),
			Label:     cause,
			Steps: []schema.TreeNode{{
				Step: schema.Step{
					ID:           stepID,
					Type:         "manual",
					Title:        fmt.Sprintf("Follow %s TSG", cause),
					Instructions: fmt.Sprintf("Follow [%s](%s).", cause, tsgPath),
				},
			}},
		}
	}
	return schema.Branch{
		Condition: fmt.Sprintf("%s == %q", dispatchVar, cause),
		Label:     cause + " (enriched)",
		Steps: []schema.TreeNode{{
			Step: schema.Step{
				ID:    stepID,
				Type:  "manual",
				Title: cause,
				Instructions: fmt.Sprintf(
					"No dedicated TSG found for **%s**.\n"+
						"Check the [availability-manager](./availability-manager/availability-manager.md) index for related guides.\n"+
						"See also [Login Errors](./login-errors/login-errors.md) for error-code-specific TSGs.\n"+
						"Escalate to domain experts if the cause is unclear.",
					cause,
				),
			},
		}},
	}
}

// extractTSGLinks reads the source TSG markdown file (same stem as runbook,
// with .md extension) and extracts cause→path mappings from markdown links.
// Returns a map from link text (cause name) to relative path.
func extractTSGLinks(runbookPath string) map[string]string {
	dir := filepath.Dir(runbookPath)
	base := filepath.Base(runbookPath)
	stem := strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	stem = strings.TrimSuffix(stem, ".runbook")
	mdPath := filepath.Join(dir, stem+".md")

	data, err := os.ReadFile(mdPath)
	if err != nil {
		return nil
	}

	links := make(map[string]string)
	re := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+\.md)\)`)
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		links[m[1]] = m[2]
	}
	return links
}

// findMatchingTSG attempts to resolve a TSG file path for a cause.
// Strategy order:
//  1. Exact match in the source TSG link map
//  2. File search by kebab-case name in the TSG directory tree
//  3. Pattern match for LoginErrorsFound_<code>_<state> → error-<code>-state-<state>.md
//
// Returns a relative path from runbook dir (forward slashes), or "".
func findMatchingTSG(runbookDir string, cause string, tsgLinks map[string]string) string {
	// Strategy 1: exact match from source TSG markdown
	if path, ok := tsgLinks[cause]; ok {
		return path
	}

	// Strategy 2: file search by kebab-case name
	kebab := causeToKebab(cause)
	searchRoot := filepath.Dir(runbookDir) // parent dir (e.g. TSG/) to find siblings
	if found := searchForTSGFile(searchRoot, runbookDir, kebab); found != "" {
		return found
	}

	// Strategy 3: LoginErrorsFound_<code>_<state> pattern
	loginErrRe := regexp.MustCompile(`^LoginErrorsFound_(\d+)_(\d+)$`)
	if m := loginErrRe.FindStringSubmatch(cause); m != nil {
		candidate := fmt.Sprintf("error-%s-state-%s", m[1], m[2])
		if found := searchForTSGFile(searchRoot, runbookDir, candidate); found != "" {
			return found
		}
	}

	return ""
}

// searchForTSGFile walks searchRoot looking for a .md file with the exact
// base name (excluding .mapping.md). Returns relative path from runbookDir.
func searchForTSGFile(searchRoot, runbookDir, baseName string) string {
	var best string
	bestLen := int(^uint(0) >> 1) // max int
	filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".mapping.md") {
			return nil
		}
		if strings.TrimSuffix(name, ".md") == baseName {
			rel, e := filepath.Rel(runbookDir, path)
			if e == nil && len(rel) < bestLen {
				best = filepath.ToSlash(rel)
				bestLen = len(rel)
			}
		}
		return nil
	})
	return best
}

// causeToKebab converts a CamelCase/PascalCase cause name to kebab-case.
//
//	HasSqlDump → has-sql-dump
//	IsActivateDatabaseFailure → is-activate-database-failure
//	LoginErrorsFound_40613_127 → login-errors-found-40613-127
//	IsDbStuckInDenyConnections → is-db-stuck-in-deny-connections
func causeToKebab(cause string) string {
	s := strings.ReplaceAll(cause, "_", "-")
	// Insert hyphen between lowercase/digit and uppercase
	re1 := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	s = re1.ReplaceAllString(s, "${1}-${2}")
	// Insert hyphen between consecutive uppercase and uppercase+lowercase (acronyms)
	re2 := regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	s = re2.ReplaceAllString(s, "${1}-${2}")
	return strings.ToLower(s)
}

// toTSGStepID converts a cause name to a step ID with tsg_ prefix.
// e.g. "HasSqlDump" → "tsg_has_sql_dump"
func toTSGStepID(cause string) string {
	re := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	s := re.ReplaceAllString(cause, "${1}_${2}")
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return "tsg_" + s
}

// followLinkRe matches "Follow [text](path.md)." instructions.
var followLinkRe = regexp.MustCompile(`(?s)^Follow \[[^\]]+\]\(([^)]+\.md)\)\.?\s*$`)

// extractFollowLink extracts the .md path from "Follow [X](path.md)." instructions.
// Returns "" if the instructions don't match the expected single-line pattern.
func extractFollowLink(instructions string) string {
	instructions = strings.TrimSpace(instructions)
	m := followLinkRe.FindStringSubmatch(instructions)
	if m != nil {
		return m[1]
	}
	return ""
}

// bindSubRunbooks converts manual "Follow [X](path.md)" branch steps
// to type=invoke steps when the corresponding .runbook.yaml exists.
// This upgrades both compiled and enriched branches to actually execute
// the sub-TSG instead of just showing a markdown link.
func bindSubRunbooks(node *schema.TreeNode, runbookDir string) (int, error) {
	bound := 0
	for i := range node.Branches {
		branch := &node.Branches[i]
		if len(branch.Steps) != 1 {
			continue
		}
		step := &branch.Steps[0].Step
		if step.Type != "manual" {
			continue
		}
		mdPath := extractFollowLink(step.Instructions)
		if mdPath == "" {
			continue
		}
		// Check if .runbook.yaml exists for this .md file
		rbPath := strings.TrimSuffix(mdPath, ".md") + ".runbook.yaml"
		fullRBPath := filepath.Join(runbookDir, filepath.FromSlash(rbPath))
		if _, err := os.Stat(fullRBPath); err != nil {
			continue // no compiled sub-runbook
		}
		// Read child runbook to determine required inputs
		inputMapping := buildInputMapping(readChildInputs(fullRBPath))
		// Convert manual → invoke
		step.Type = "invoke"
		step.Title = fmt.Sprintf("Run %s", branch.Label)
		step.Instructions = ""
		step.Invoke = &schema.InvokeConfig{
			Runbook: filepath.ToSlash(rbPath),
			Inputs:  inputMapping,
		}
		step.Gate = &schema.Gate{
			OnError: "skip",
		}
		bound++
	}
	return bound, nil
}

// readChildInputs loads a child runbook and returns its declared inputs.
func readChildInputs(runbookPath string) map[string]*schema.InputDef {
	data, err := os.ReadFile(runbookPath)
	if err != nil {
		return nil
	}
	var rb schema.Runbook
	if err := yaml.Unmarshal(data, &rb); err != nil {
		return nil
	}
	return rb.Meta.Inputs
}

// buildInputMapping creates an invoke input mapping from child input definitions.
// Each child input is mapped to a template variable of the same name in the parent.
func buildInputMapping(childInputs map[string]*schema.InputDef) map[string]string {
	if len(childInputs) == 0 {
		return nil
	}
	mapping := make(map[string]string)
	for k := range childInputs {
		mapping[k] = fmt.Sprintf("{{ .%s }}", k)
	}
	return mapping
}
