// Package diagram generates visual diagrams from parsed runbooks.
// Supports Mermaid flowchart and ASCII formats.
package diagram

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// Format represents the output diagram format.
type Format string

const (
	FormatMermaid Format = "mermaid"
	FormatASCII   Format = "ascii"
)

// Generate produces a diagram string from a parsed runbook.
func Generate(rb *schema.Runbook, format Format) (string, error) {
	if rb == nil {
		return "", fmt.Errorf("nil runbook")
	}
	switch format {
	case FormatMermaid:
		return generateMermaid(rb), nil
	case FormatASCII:
		return generateASCII(rb), nil
	default:
		return "", fmt.Errorf("unsupported diagram format: %s", format)
	}
}

// --- Mermaid flowchart ---

func generateMermaid(rb *schema.Runbook) string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")

	nodes := rb.Tree
	if len(nodes) == 0 {
		// Fall back to flat steps
		for _, s := range rb.Steps {
			nodes = append(nodes, schema.TreeNode{Step: s})
		}
	}

	steps := flattenTree(nodes)
	if len(steps) == 0 {
		return b.String()
	}

	// Start node
	b.WriteString("    START([Start]) --> " + safeID(steps[0].id) + "\n")

	for i, s := range steps {
		// Node definition
		b.WriteString("    " + nodeDefinition(s) + "\n")

		// Branches
		if len(s.branches) > 0 {
			for _, br := range s.branches {
				branchSteps := flattenTree(br.steps)
				if len(branchSteps) == 0 {
					continue
				}
				// Edge from parent to first branch step
				label := br.label
				if label == "" {
					label = truncate(br.condition, 30)
				}
				b.WriteString(fmt.Sprintf("    %s -->|%q| %s\n",
					safeID(s.id), label, safeID(branchSteps[0].id)))

				// Branch internal edges
				for j, bs := range branchSteps {
					b.WriteString("    " + nodeDefinition(bs) + "\n")
					if j < len(branchSteps)-1 {
						b.WriteString(fmt.Sprintf("    %s --> %s\n",
							safeID(bs.id), safeID(branchSteps[j+1].id)))
					}
				}

				// Last branch step rejoins the next main step
				lastBranch := branchSteps[len(branchSteps)-1]
				if i < len(steps)-1 {
					b.WriteString(fmt.Sprintf("    %s --> %s\n",
						safeID(lastBranch.id), safeID(steps[i+1].id)))
				}
			}

			// Main flow "continue" edge
			if i < len(steps)-1 {
				b.WriteString(fmt.Sprintf("    %s -->|\"continue\"| %s\n",
					safeID(s.id), safeID(steps[i+1].id)))
			}
		} else if i < len(steps)-1 {
			// Simple sequential edge
			b.WriteString(fmt.Sprintf("    %s --> %s\n",
				safeID(s.id), safeID(steps[i+1].id)))
		}
	}

	// Outcomes from the last step (or any step with outcomes)
	for _, s := range steps {
		if len(s.outcomes) == 0 {
			continue
		}
		for _, o := range s.outcomes {
			outcomeID := safeID(s.id + "_" + o.state)
			shape := outcomeShape(o.state)
			b.WriteString(fmt.Sprintf("    %s%s\n", outcomeID, shape))

			label := truncate(o.when, 30)
			if label == "" {
				label = o.state
			}
			b.WriteString(fmt.Sprintf("    %s -->|%q| %s\n",
				safeID(s.id), label, outcomeID))

			// Apply style
			if style := outcomeStyle(o.state); style != "" {
				b.WriteString(fmt.Sprintf("    style %s %s\n", outcomeID, style))
			}
		}
	}

	// Style CLI steps
	for _, s := range steps {
		if s.stepType == "cli" {
			b.WriteString(fmt.Sprintf("    style %s fill:#1a3a4a,stroke:#0af\n", safeID(s.id)))
		}
	}

	return b.String()
}

func outcomeShape(state string) string {
	switch state {
	case "resolved":
		return "([✅ Resolved])"
	case "escalated":
		return "([⚠️ Request Assistance])"
	case "no_action":
		return "([ℹ️ No Action Needed])"
	case "needs_rca":
		return "([🔍 Needs RCA])"
	default:
		return fmt.Sprintf("([%s])", state)
	}
}

func outcomeStyle(state string) string {
	switch state {
	case "resolved":
		return "fill:#0d6,stroke:#0a5,color:#fff"
	case "escalated":
		return "fill:#e60,stroke:#c40,color:#fff"
	case "no_action":
		return "fill:#07a,stroke:#058,color:#fff"
	case "needs_rca":
		return "fill:#a0a,stroke:#808,color:#fff"
	default:
		return ""
	}
}

// --- ASCII ---

func generateASCII(rb *schema.Runbook) string {
	var b strings.Builder

	name := rb.Meta.Name
	if name == "" {
		name = "Runbook"
	}

	nodes := rb.Tree
	if len(nodes) == 0 {
		for _, s := range rb.Steps {
			nodes = append(nodes, schema.TreeNode{Step: s})
		}
	}

	steps := flattenTree(nodes)
	if len(steps) == 0 {
		b.WriteString(name + " (empty)\n")
		return b.String()
	}

	// Header
	header := "  " + name + "  "
	border := strings.Repeat("═", len(header))
	b.WriteString("╔" + border + "╗\n")
	b.WriteString("║" + header + "║\n")
	mid := len(header) / 2
	b.WriteString("╚" + strings.Repeat("═", mid) + "╤" + strings.Repeat("═", len(header)-mid-1) + "╝\n")
	indent := strings.Repeat(" ", mid+1)
	b.WriteString(indent + "│\n")

	for i, s := range steps {
		writeASCIIStep(&b, s, 8)

		// Branches
		if len(s.branches) > 0 {
			b.WriteString("                │\n")
			b.WriteString("          ┌─────◇─────┐\n")
			for _, br := range s.branches {
				label := br.label
				if label == "" {
					label = truncate(br.condition, 28)
				}
				b.WriteString(fmt.Sprintf("          │ %s\n", label))
				for _, bs := range flattenTree(br.steps) {
					icon := stepIcon(bs.stepType)
					b.WriteString(fmt.Sprintf("          │  %s %s\n", icon, bs.title))
				}
			}
			b.WriteString("          └─────┬─────┘\n")
		}

		// Connector
		if i < len(steps)-1 || len(s.outcomes) > 0 {
			b.WriteString("                │\n")
		}
	}

	// Outcomes
	for _, s := range steps {
		for _, o := range s.outcomes {
			icon := "✅"
			label := "Resolved"
			switch o.state {
			case "escalated":
				icon = "⚠️"
				label = "Request Assistance"
			case "no_action":
				icon = "ℹ️"
				label = "No Action Needed"
			case "needs_rca":
				icon = "🔍"
				label = "Needs RCA"
			}
			rec := o.recommendation
			if rec != "" {
				rec = " — " + truncate(rec, 40)
			}
			b.WriteString(fmt.Sprintf("          %s %s%s\n", icon, label, rec))
		}
	}

	return b.String()
}

func writeASCIIStep(b *strings.Builder, s diagramStep, indent int) {
	icon := stepIcon(s.stepType)
	label := s.title
	if label == "" {
		label = s.id
	}

	content := fmt.Sprintf(" %s %s ", icon, label)
	contentWidth := runewidth.StringWidth(content)
	boxWidth := contentWidth
	if s.capture != "" {
		capLine := " → " + s.capture
		if w := runewidth.StringWidth(capLine); w > boxWidth {
			boxWidth = w
		}
	}
	if boxWidth < 22 {
		boxWidth = 22
	}

	pad := strings.Repeat(" ", indent)
	topBot := strings.Repeat("─", boxWidth)
	mid := boxWidth / 2

	b.WriteString(pad + "┌" + topBot + "┐\n")
	b.WriteString(pad + "│" + content + strings.Repeat(" ", boxWidth-contentWidth) + "│\n")
	if s.capture != "" {
		capLine := " → " + s.capture
		b.WriteString(pad + "│" + capLine + strings.Repeat(" ", boxWidth-runewidth.StringWidth(capLine)) + "│\n")
	}
	b.WriteString(pad + "└" + strings.Repeat("─", mid) + "┬" + strings.Repeat("─", boxWidth-mid-1) + "┘\n")
}

func stepIcon(stepType string) string {
	switch stepType {
	case "cli":
		return "⚡"
	case "manual":
		return "🧑"
	case "tool":
		return "🔧"
	case "invoke":
		return "📎"
	default:
		return "○"
	}
}

// --- tree walking helpers ---

type diagramStep struct {
	id       string
	title    string
	stepType string
	capture  string
	branches []diagramBranch
	outcomes []diagramOutcome
}

type diagramBranch struct {
	condition string
	label     string
	steps     []schema.TreeNode
}

type diagramOutcome struct {
	state          string
	when           string
	recommendation string
}

func flattenTree(entries []schema.TreeNode) []diagramStep {
	var result []diagramStep
	for _, e := range entries {
		if e.Step.ID == "" {
			// iterate block — recurse into its steps
			if e.Iterate != nil {
				result = append(result, flattenTree(e.Iterate.Steps)...)
			}
			continue
		}
		s := e.Step
		ds := diagramStep{
			id:       s.ID,
			title:    s.Title,
			stepType: s.Type,
		}

		// Capture summary
		if s.Capture != nil {
			caps := make([]string, 0, len(s.Capture))
			for k := range s.Capture {
				caps = append(caps, k)
			}
			ds.capture = strings.Join(caps, ", ")
		}

		// Branches
		for _, br := range e.Branches {
			ds.branches = append(ds.branches, diagramBranch{
				condition: br.Condition,
				label:     br.Label,
				steps:     br.Steps,
			})
		}

		// Outcomes
		for _, o := range s.Outcomes {
			ds.outcomes = append(ds.outcomes, diagramOutcome{
				state:          o.State,
				when:           o.When,
				recommendation: o.Recommendation,
			})
		}

		result = append(result, ds)
	}
	return result
}

// --- string helpers ---

func nodeDefinition(s diagramStep) string {
	id := safeID(s.id)
	title := s.title
	if title == "" {
		title = s.id
	}

	icon := stepIcon(s.stepType)
	captureSuffix := ""
	if s.capture != "" {
		captureSuffix = "<br/>→ " + s.capture
	}

	switch s.stepType {
	case "manual":
		return fmt.Sprintf(`%s{{"` + icon + ` %s%s"}}`, id, escMermaid(title), captureSuffix)
	case "cli":
		return fmt.Sprintf(`%s["` + icon + ` %s%s"]`, id, escMermaid(title), captureSuffix)
	case "tool":
		return fmt.Sprintf(`%s[/"` + icon + ` %s%s"/]`, id, escMermaid(title), captureSuffix)
	case "invoke":
		return fmt.Sprintf(`%s[["` + icon + ` %s"]]`, id, escMermaid(title))
	default:
		return fmt.Sprintf(`%s["%s%s"]`, id, escMermaid(title), captureSuffix)
	}
}

func safeID(id string) string {
	r := strings.NewReplacer("-", "_", " ", "_", ".", "_")
	return r.Replace(id)
}

func escMermaid(s string) string {
	s = strings.ReplaceAll(s, `"`, "#quot;")
	s = strings.ReplaceAll(s, `'`, "#apos;")
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
