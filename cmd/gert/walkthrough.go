package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
	"github.com/spf13/cobra"
)

// --- walkthrough ---

var (
	walkthroughMode string
	walkthroughVars []string
	walkthroughOut  string
)

var walkthroughCmd = &cobra.Command{
	Use:   "walkthrough [runbook.yaml]",
	Short: "Generate a step-by-step walkthrough document for a runbook",
	Long: `Analyze a runbook and produce a structured Markdown walkthrough with
step descriptions, command details, branch logic, and outcome analysis.

Output is a Markdown document suitable for review, onboarding, or as
input to an AI evaluation pipeline.

The walkthrough is generated from static analysis of the runbook YAML —
no execution occurs. Use 'gert exec --mode dry-run' if you need execution output.`,
	Args: cobra.ExactArgs(1),
	RunE: runWalkthrough,
}

func runWalkthrough(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Validate first
	rb, errs := schema.ValidateFile(filePath)
	if hasValidationErrors(errs) {
		fmt.Fprintf(os.Stderr, "Validation failed: %d error(s)\n", countValidationErrors(errs))
		for _, e := range errs {
			if e.Severity != "warning" {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", e.Phase, e.Message)
			}
		}
		return fmt.Errorf("runbook validation failed")
	}

	warnings := collectValidationWarnings(errs)

	// Count steps and types
	stats := analyzeRunbook(rb)

	// Determine output path
	outPath := walkthroughOut
	if outPath == "" {
		dir := filepath.Dir(filePath)
		base := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(filePath), ".yaml"), ".yml")
		base = strings.TrimSuffix(base, ".runbook")
		outPath = filepath.Join(dir, "walkthrough-"+base+".md")
	}

	// Generate the walkthrough document
	var sb strings.Builder

	writeHeader(&sb, rb, filePath, stats)
	writeValidation(&sb, warnings)
	writeInputs(&sb, rb)
	writeGovernance(&sb, rb)

	if len(rb.Tree) > 0 {
		writeTreeWalkthrough(&sb, rb.Tree, rb, 0, stats)
	} else {
		writeFlatWalkthrough(&sb, rb)
	}

	writeBranchAnalysis(&sb, rb, stats)
	writeOutcomeAnalysis(&sb, rb, stats)
	writeSummary(&sb, stats)

	// Write to file
	if err := os.WriteFile(outPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("write walkthrough: %w", err)
	}

	fmt.Printf("✓ Walkthrough generated: %s\n", outPath)
	fmt.Printf("  %d steps (%d CLI, %d manual, %d tool, %d invoke)\n",
		stats.totalSteps, stats.cliSteps, stats.manualSteps, stats.toolSteps, stats.invokeSteps)
	fmt.Printf("  %d branches, %d outcomes\n", stats.branches, stats.outcomes)

	return nil
}

// runbookStats holds counters from analysis.
type runbookStats struct {
	totalSteps  int
	cliSteps    int
	manualSteps int
	toolSteps   int
	invokeSteps int
	branches    int
	outcomes    int
	iterBlocks  int
	captures    map[string]string // variable → source step ID
	allSteps    []stepDetail      // flat list of all steps in order
}

// stepDetail captures info about a single step for the walkthrough.
type stepDetail struct {
	ID           string
	Title        string
	Type         string
	When         string
	Depth        int
	Command      []string
	Instructions string
	Captures     map[string]string
	Choices      *schema.ChoiceConfig
	Outcomes     []schema.Outcome
	Assertions   []schema.Assertion
	Evidence     []schema.EvidenceRequirement
	BranchLabel  string // non-empty if this is a branch header
}

func analyzeRunbook(rb *schema.Runbook) *runbookStats {
	stats := &runbookStats{
		captures: make(map[string]string),
	}
	if len(rb.Tree) > 0 {
		analyzeTree(rb.Tree, stats, 0)
	} else {
		for _, step := range rb.Steps {
			addStepStats(step, stats, 0)
		}
	}
	return stats
}

func analyzeTree(nodes []schema.TreeNode, stats *runbookStats, depth int) {
	for _, node := range nodes {
		if node.Iterate != nil {
			stats.iterBlocks++
			analyzeTree(node.Iterate.Steps, stats, depth+1)
			continue
		}

		step := node.Step
		addStepStats(step, stats, depth)

		for _, branch := range node.Branches {
			stats.branches++
			stats.allSteps = append(stats.allSteps, stepDetail{
				BranchLabel: branch.Label,
				Depth:       depth,
			})
			analyzeTree(branch.Steps, stats, depth+1)
		}
	}
}

func addStepStats(step schema.Step, stats *runbookStats, depth int) {
	stats.totalSteps++
	switch step.Type {
	case "cli":
		stats.cliSteps++
	case "manual":
		stats.manualSteps++
	case "tool":
		stats.toolSteps++
	case "invoke":
		stats.invokeSteps++
	}
	stats.outcomes += len(step.Outcomes)
	for k := range step.Capture {
		stats.captures[k] = step.ID
	}

	detail := stepDetail{
		ID:           step.ID,
		Title:        step.Title,
		Type:         step.Type,
		When:         step.When,
		Depth:        depth,
		Instructions: step.Instructions,
		Captures:     step.Capture,
		Choices:      step.Choices,
		Outcomes:     step.Outcomes,
		Assertions:   step.Assertions,
		Evidence:     step.RequiredEvidence,
	}
	if step.With != nil {
		detail.Command = step.With.Argv
	}
	stats.allSteps = append(stats.allSteps, detail)
}

// --- Markdown generation ---

func writeHeader(sb *strings.Builder, rb *schema.Runbook, filePath string, stats *runbookStats) {
	sb.WriteString(fmt.Sprintf("# Walkthrough: %s\n\n", rb.Meta.Name))
	sb.WriteString(fmt.Sprintf("**Generated**: %s  \n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("**Runbook**: `%s`  \n", filePath))
	sb.WriteString(fmt.Sprintf("**API Version**: %s  \n", rb.APIVersion))
	if rb.Meta.Kind != "" {
		sb.WriteString(fmt.Sprintf("**Kind**: %s  \n", rb.Meta.Kind))
	}
	sb.WriteString("\n")

	sb.WriteString("## Overview\n\n")
	if rb.Meta.Description != "" {
		sb.WriteString(rb.Meta.Description)
		sb.WriteString("\n")
	}

	sb.WriteString("| Metric | Count |\n")
	sb.WriteString("|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Total steps | %d |\n", stats.totalSteps))
	sb.WriteString(fmt.Sprintf("| CLI steps | %d |\n", stats.cliSteps))
	sb.WriteString(fmt.Sprintf("| Manual steps | %d |\n", stats.manualSteps))
	if stats.toolSteps > 0 {
		sb.WriteString(fmt.Sprintf("| Tool steps | %d |\n", stats.toolSteps))
	}
	if stats.invokeSteps > 0 {
		sb.WriteString(fmt.Sprintf("| Invoke steps | %d |\n", stats.invokeSteps))
	}
	sb.WriteString(fmt.Sprintf("| Branches | %d |\n", stats.branches))
	sb.WriteString(fmt.Sprintf("| Outcomes | %d |\n", stats.outcomes))
	if stats.iterBlocks > 0 {
		sb.WriteString(fmt.Sprintf("| Iterate blocks | %d |\n", stats.iterBlocks))
	}
	sb.WriteString("\n")
}

func writeValidation(sb *strings.Builder, warnings []string) {
	sb.WriteString("## Validation\n\n")
	if len(warnings) == 0 {
		sb.WriteString("✓ Runbook is valid with no warnings.\n\n")
	} else {
		sb.WriteString(fmt.Sprintf("⚠ Runbook is valid with %d warning(s):\n\n", len(warnings)))
		for _, w := range warnings {
			sb.WriteString(fmt.Sprintf("- %s\n", w))
		}
		sb.WriteString("\n")
	}
}

func writeInputs(sb *strings.Builder, rb *schema.Runbook) {
	if rb.Meta.Inputs == nil || len(rb.Meta.Inputs) == 0 {
		return
	}

	sb.WriteString("## Inputs\n\n")
	sb.WriteString("| Name | Source | Description | Default |\n")
	sb.WriteString("|------|--------|-------------|---------|\n")
	for name, input := range rb.Meta.Inputs {
		def := input.Default
		if def == "" {
			def = "—"
		}
		desc := input.Description
		if desc == "" {
			desc = "—"
		}
		sb.WriteString(fmt.Sprintf("| `%s` | `%s` | %s | %s |\n", name, input.From, desc, def))
	}
	sb.WriteString("\n")
}

func writeGovernance(sb *strings.Builder, rb *schema.Runbook) {
	if rb.Meta.Governance == nil {
		return
	}

	sb.WriteString("## Governance\n\n")

	gov := rb.Meta.Governance
	if len(gov.AllowedCommands) > 0 {
		sb.WriteString("**Allowed commands**: ")
		cmds := make([]string, len(gov.AllowedCommands))
		for i, c := range gov.AllowedCommands {
			cmds[i] = "`" + c + "`"
		}
		sb.WriteString(strings.Join(cmds, ", "))
		sb.WriteString("\n\n")
	}

	if len(gov.DeniedCommands) > 0 {
		sb.WriteString("**Denied commands**: ")
		cmds := make([]string, len(gov.DeniedCommands))
		for i, c := range gov.DeniedCommands {
			cmds[i] = "`" + c + "`"
		}
		sb.WriteString(strings.Join(cmds, ", "))
		sb.WriteString("\n\n")
	}

	if len(gov.Redact) > 0 {
		sb.WriteString(fmt.Sprintf("**Redaction rules**: %d pattern(s)\n\n", len(gov.Redact)))
	}

	if gov.Evidence != nil {
		if gov.Evidence.RequireForManual {
			sb.WriteString("**Evidence**: Required for manual steps\n\n")
		}
	}
}

func writeTreeWalkthrough(sb *strings.Builder, nodes []schema.TreeNode, rb *schema.Runbook, depth int, stats *runbookStats) {
	sb.WriteString("## Step-by-Step Walkthrough\n\n")
	stepNum := 1
	writeTreeNodes(sb, nodes, rb, depth, stats, &stepNum)
}

func writeTreeNodes(sb *strings.Builder, nodes []schema.TreeNode, rb *schema.Runbook, depth int, stats *runbookStats, stepNum *int) {
	for _, node := range nodes {
		if node.Iterate != nil {
			writeIterateBlock(sb, node.Iterate, rb, depth, stats, stepNum)
			continue
		}

		step := node.Step
		indent := strings.Repeat("  ", depth)
		_ = indent

		// Heading level based on depth
		level := "###"
		if depth > 0 {
			level = "####"
		}
		if depth > 1 {
			level = "#####"
		}

		sb.WriteString(fmt.Sprintf("%s Step %d: %s", level, *stepNum, step.Title))
		if step.ID != "" {
			sb.WriteString(fmt.Sprintf(" [`%s`]", step.ID))
		}
		sb.WriteString("\n\n")

		writeStepDetail(sb, step, stats)
		*stepNum++

		// Branches
		for _, branch := range node.Branches {
			label := branch.Label
			if label == "" {
				label = branch.Condition
			}
			sb.WriteString(fmt.Sprintf("\n> **Branch**: %s  \n", label))
			sb.WriteString(fmt.Sprintf("> Condition: `%s`\n\n", branch.Condition))
			writeTreeNodes(sb, branch.Steps, rb, depth+1, stats, stepNum)
		}
	}
}

func writeIterateBlock(sb *strings.Builder, iter *schema.IterateBlock, rb *schema.Runbook, depth int, stats *runbookStats, stepNum *int) {
	if iter.Over != "" {
		asVar := iter.As
		if asVar == "" {
			asVar = "item"
		}
		sb.WriteString(fmt.Sprintf("### ↻ Iterate over `%s` (as `%s`)\n\n", iter.Over, asVar))
	} else {
		sb.WriteString(fmt.Sprintf("### ↻ Iterate (max %d, until: `%s`)\n\n", iter.Max, iter.Until))
	}
	writeTreeNodes(sb, iter.Steps, rb, depth+1, stats, stepNum)
}

func writeStepDetail(sb *strings.Builder, step schema.Step, stats *runbookStats) {
	sb.WriteString(fmt.Sprintf("**Type**: `%s`  \n", step.Type))

	if step.When != "" {
		sb.WriteString(fmt.Sprintf("**When guard**: `%s`  \n", step.When))
	}
	if step.Timeout != "" {
		sb.WriteString(fmt.Sprintf("**Timeout**: %s  \n", step.Timeout))
	}
	sb.WriteString("\n")

	// CLI command
	if step.With != nil && len(step.With.Argv) > 0 {
		sb.WriteString("**Command**:\n```\n")
		sb.WriteString(strings.Join(step.With.Argv, " "))
		sb.WriteString("\n```\n\n")
	}

	// Tool invocation
	if step.Tool != nil {
		sb.WriteString(fmt.Sprintf("**Tool**: `%s`\n\n", step.Tool.Name))
		if len(step.Tool.Args) > 0 {
			sb.WriteString("| Argument | Value |\n")
			sb.WriteString("|----------|-------|\n")
			for k, v := range step.Tool.Args {
				// Truncate long values
				display := v
				if len(display) > 120 {
					display = display[:117] + "..."
				}
				sb.WriteString(fmt.Sprintf("| `%s` | `%s` |\n", k, display))
			}
			sb.WriteString("\n")
		}
	}

	// Instructions
	if step.Instructions != "" {
		sb.WriteString("<details>\n<summary>Instructions</summary>\n\n")
		sb.WriteString(step.Instructions)
		sb.WriteString("\n</details>\n\n")
	}

	// Choices
	if step.Choices != nil {
		sb.WriteString(fmt.Sprintf("**Choices** → `%s`\n\n", step.Choices.Variable))
		sb.WriteString("| Value | Label | Description |\n")
		sb.WriteString("|-------|-------|-------------|\n")
		for _, opt := range step.Choices.Options {
			desc := opt.Description
			if desc == "" {
				desc = "—"
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", opt.Value, opt.Label, desc))
		}
		sb.WriteString("\n")
	}

	// Captures
	if len(step.Capture) > 0 {
		sb.WriteString("**Captures**:\n\n")
		for varName, source := range step.Capture {
			sb.WriteString(fmt.Sprintf("- `%s` ← `%s`\n", varName, source))
		}
		sb.WriteString("\n")
	}

	// Assertions
	if len(step.Assertions) > 0 {
		sb.WriteString("**Assertions**:\n\n")
		for _, a := range step.Assertions {
			switch {
			case a.Contains != "":
				sb.WriteString(fmt.Sprintf("- contains `%s`\n", a.Contains))
			case a.NotContains != "":
				sb.WriteString(fmt.Sprintf("- not_contains `%s`\n", a.NotContains))
			case a.Matches != "":
				sb.WriteString(fmt.Sprintf("- matches `%s`\n", a.Matches))
			case a.ExitCode != nil:
				sb.WriteString(fmt.Sprintf("- exit_code = `%d`\n", *a.ExitCode))
			case a.Equals != "":
				sb.WriteString(fmt.Sprintf("- equals `%s`\n", a.Equals))
			case a.NotEquals != "":
				sb.WriteString(fmt.Sprintf("- not_equals `%s`\n", a.NotEquals))
			case a.JSONPath != nil:
				sb.WriteString(fmt.Sprintf("- json_path `%s` equals `%s`\n", a.JSONPath.Path, a.JSONPath.Equals))
			}
		}
		sb.WriteString("\n")
	}

	// Evidence requirements
	if len(step.RequiredEvidence) > 0 {
		sb.WriteString("**Required evidence**:\n\n")
		for _, ev := range step.RequiredEvidence {
			sb.WriteString(fmt.Sprintf("- `%s` (kind: %s)\n", ev.Name, ev.Kind))
		}
		sb.WriteString("\n")
	}

	// Outcomes (brief, detailed in separate section)
	if len(step.Outcomes) > 0 {
		sb.WriteString("**Outcomes at this step**:\n\n")
		for _, outcome := range step.Outcomes {
			sb.WriteString(fmt.Sprintf("- **%s** when `%s`\n", outcome.State, outcome.When))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n")
}

func writeFlatWalkthrough(sb *strings.Builder, rb *schema.Runbook) {
	sb.WriteString("## Step-by-Step Walkthrough\n\n")
	for i, step := range rb.Steps {
		sb.WriteString(fmt.Sprintf("### Step %d: %s [`%s`]\n\n", i+1, step.Title, step.ID))
		writeStepDetail(sb, step, nil)
	}
}

func writeBranchAnalysis(sb *strings.Builder, rb *schema.Runbook, stats *runbookStats) {
	if stats.branches == 0 {
		return
	}

	sb.WriteString("## Branch Analysis\n\n")
	branchNum := 1
	if len(rb.Tree) > 0 {
		writeBranchesFromTree(sb, rb.Tree, &branchNum)
	}
}

func writeBranchesFromTree(sb *strings.Builder, nodes []schema.TreeNode, num *int) {
	for _, node := range nodes {
		if node.Iterate != nil {
			writeBranchesFromTree(sb, node.Iterate.Steps, num)
			continue
		}
		for _, branch := range node.Branches {
			label := branch.Label
			if label == "" {
				label = "(unlabeled)"
			}
			sb.WriteString(fmt.Sprintf("**Branch %d**: %s  \n", *num, label))
			sb.WriteString(fmt.Sprintf("- After step: `%s`  \n", node.Step.ID))
			sb.WriteString(fmt.Sprintf("- Condition: `%s`  \n", branch.Condition))
			sb.WriteString(fmt.Sprintf("- Contains: %d step(s)  \n\n", countTreeSteps(branch.Steps)))
			*num++
			writeBranchesFromTree(sb, branch.Steps, num)
		}
	}
}

func countTreeSteps(nodes []schema.TreeNode) int {
	count := 0
	for _, node := range nodes {
		if node.Iterate != nil {
			count += countTreeSteps(node.Iterate.Steps)
		} else {
			count++
			for _, b := range node.Branches {
				count += countTreeSteps(b.Steps)
			}
		}
	}
	return count
}

func writeOutcomeAnalysis(sb *strings.Builder, rb *schema.Runbook, stats *runbookStats) {
	if stats.outcomes == 0 {
		return
	}

	sb.WriteString("## Outcome Analysis\n\n")
	sb.WriteString("| Step | State | Condition | Chains to |\n")
	sb.WriteString("|------|-------|-----------|----------|\n")

	if len(rb.Tree) > 0 {
		writeOutcomesFromTree(sb, rb.Tree)
	} else {
		for _, step := range rb.Steps {
			for _, outcome := range step.Outcomes {
				chain := "—"
				if outcome.NextRunbook != nil {
					chain = fmt.Sprintf("`%s`", outcome.NextRunbook.File)
				}
				sb.WriteString(fmt.Sprintf("| `%s` | **%s** | `%s` | %s |\n",
					step.ID, outcome.State, outcome.When, chain))
			}
		}
	}
	sb.WriteString("\n")
}

func writeOutcomesFromTree(sb *strings.Builder, nodes []schema.TreeNode) {
	for _, node := range nodes {
		if node.Iterate != nil {
			writeOutcomesFromTree(sb, node.Iterate.Steps)
			continue
		}
		for _, outcome := range node.Step.Outcomes {
			chain := "—"
			if outcome.NextRunbook != nil {
				chain = fmt.Sprintf("`%s`", outcome.NextRunbook.File)
			}
			sb.WriteString(fmt.Sprintf("| `%s` | **%s** | `%s` | %s |\n",
				node.Step.ID, outcome.State, outcome.When, chain))
		}
		for _, branch := range node.Branches {
			writeOutcomesFromTree(sb, branch.Steps)
		}
	}
}

func writeSummary(sb *strings.Builder, stats *runbookStats) {
	sb.WriteString("## Summary\n\n")

	sb.WriteString("### Variable Flow\n\n")
	if len(stats.captures) > 0 {
		sb.WriteString("| Variable | Captured by |\n")
		sb.WriteString("|----------|-------------|\n")
		for varName, stepID := range stats.captures {
			sb.WriteString(fmt.Sprintf("| `%s` | `%s` |\n", varName, stepID))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("No captures defined.\n\n")
	}

	sb.WriteString("### Review Checklist\n\n")
	sb.WriteString("- [ ] All step titles are clear and descriptive\n")
	sb.WriteString("- [ ] Instructions are actionable for the target audience\n")
	sb.WriteString("- [ ] Branch conditions are mutually exclusive where intended\n")
	sb.WriteString("- [ ] Outcomes cover all expected terminal states\n")
	sb.WriteString("- [ ] Capture variables are used downstream\n")
	sb.WriteString("- [ ] Governance rules match the commands used\n")
	if stats.manualSteps > 0 {
		sb.WriteString("- [ ] Manual steps could be automated further\n")
	}
	sb.WriteString("\n")
}

// --- helpers ---

func collectValidationWarnings(errs []*schema.ValidationError) []string {
	var warnings []string
	for _, e := range errs {
		if e.Severity == "warning" {
			warnings = append(warnings, fmt.Sprintf("[%s] %s", e.Phase, e.Message))
		}
	}
	return warnings
}
