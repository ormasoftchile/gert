package tui

import (
	"fmt"
	"strings"
)

// detailBar renders the step detail and key hints at the bottom.
type detailBar struct {
	stepID       string
	stepType     string
	stepTitle    string
	command      string
	instructions string
	status       string
	errMsg       string
	outcome      string
	recommendation string

	width int
}

func newDetailBar() detailBar {
	return detailBar{}
}

// SetStep updates the detail bar for a new step.
func (d *detailBar) SetStep(ev stepEvent) {
	d.stepID = ev.StepID
	d.stepType = ev.Type
	d.stepTitle = ev.Title
	d.command = ev.Command
	d.instructions = ev.Instructions
	d.status = "running"
	d.errMsg = ""
	d.outcome = ""
	d.recommendation = ""

	// Use query as command display if no argv command
	if d.command == "" && ev.Query != "" {
		qtype := ev.QueryType
		if qtype == "" {
			qtype = "query"
		}
		// Show first line of query
		firstLine := ev.Query
		if idx := strings.Index(firstLine, "\n"); idx > 0 {
			firstLine = firstLine[:idx] + "..."
		}
		d.command = fmt.Sprintf("[%s] %s", qtype, firstLine)
	}
}

// SetCompleted marks the current step as completed.
func (d *detailBar) SetCompleted(status, errMsg string) {
	d.status = status
	d.errMsg = errMsg
}

// SetOutcome sets the outcome display.
func (d *detailBar) SetOutcome(state, recommendation string) {
	d.outcome = state
	d.recommendation = recommendation
	d.status = "outcome"
}

// Clear resets the detail bar.
func (d *detailBar) Clear() {
	d.stepID = ""
	d.stepType = ""
	d.stepTitle = ""
	d.command = ""
	d.instructions = ""
	d.status = ""
	d.errMsg = ""
	d.outcome = ""
	d.recommendation = ""
}

// SetAwaiting marks the current step as awaiting user input.
func (d *detailBar) SetAwaiting(stepID string) {
	d.status = "awaiting"
}

// View renders the detail bar.
func (d *detailBar) View(running, completed bool, overlay overlayKind) string {
	if d.outcome != "" {
		// Outcome banner
		banner := fmt.Sprintf("◆ Outcome: %s", d.outcome)
		if d.recommendation != "" {
			banner += "\n" + d.recommendation
		}
		return outcomeBannerStyle.Width(d.width - 4).Render(banner)
	}

	if completed && d.stepID == "" {
		return detailBarStyle.Width(d.width - 4).Render(
			summaryTitleStyle.Render("Run Complete"),
		)
	}

	if d.stepID == "" {
		return detailBarStyle.Width(d.width - 4).Render(
			"  Press " + keyStyle.Render("enter") + " to start execution",
		)
	}

	// Step info line
	var parts []string
	if d.stepID != "" {
		parts = append(parts, detailLabelStyle.Render("Step: ")+detailValueStyle.Render(d.stepID))
	}
	if d.stepType != "" {
		parts = append(parts, detailLabelStyle.Render("│ ")+detailValueStyle.Render(d.stepType))
	}

	// Status
	switch d.status {
	case "running":
		parts = append(parts, detailLabelStyle.Render("│ ")+statusRunningStyle.Render("⏳ executing..."))
	case "awaiting":
		parts = append(parts, detailLabelStyle.Render("│ ")+statusAwaitingStyle.Render("⏸ awaiting input"))
	case "passed":
		parts = append(parts, detailLabelStyle.Render("│ ")+statusPassedStyle.Render("✓ passed"))
	case "failed":
		parts = append(parts, detailLabelStyle.Render("│ ")+statusFailedStyle.Render("✗ failed"))
	case "skipped":
		parts = append(parts, detailLabelStyle.Render("│ ")+stepSkipped.Render("⏭ skipped"))
	}

	line1 := strings.Join(parts, " ")

	// Command line
	var line2 string
	if d.command != "" {
		cmd := d.command
		maxW := d.width - 14
		if maxW < 10 {
			maxW = 10
		}
		if len(cmd) > maxW {
			cmd = cmd[:maxW-1] + "…"
		}
		line2 = detailLabelStyle.Render("  Command: ") + commandStyle.Render(cmd)
	}

	// Error line
	var line3 string
	if d.errMsg != "" {
		line3 = "  " + errorStyle.Render("Error: "+d.errMsg)
	}

	content := line1
	if line2 != "" {
		content += "\n" + line2
	}
	if line3 != "" {
		content += "\n" + line3
	}

	// Key bar
	content += "\n\n" + keyBarStyle.Render(keyBarText(running, completed, overlay))

	return detailBarStyle.Width(d.width - 4).Render(content)
}
