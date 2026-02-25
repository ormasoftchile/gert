package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// --- Summary messages ---

// scenarioSavedMsg is sent after exec/saveScenario completes.
type scenarioSavedMsg struct {
	outputDir string
	err       error
}

// --- Summary overlay ---

// summaryOverlay renders the end-of-run summary view.
type summaryOverlay struct {
	visible bool

	// Run results
	outcome        string
	recommendation string
	total          int
	passed         int
	failed         int
	skipped        int
	outcomes       int

	// Timing
	startTime time.Time
	endTime   time.Time

	// Run metadata
	runID    string
	traceDir string

	// Save scenario state
	saving     bool
	savedDir   string
	saveErr    string

	width  int
	height int
}

func newSummaryOverlay() summaryOverlay {
	return summaryOverlay{}
}

// Show populates and displays the summary.
func (s *summaryOverlay) Show(runID string, total, passed, failed, skipped, outcomes int, startTime time.Time) {
	s.visible = true
	s.runID = runID
	s.total = total
	s.passed = passed
	s.failed = failed
	s.skipped = skipped
	s.outcomes = outcomes
	s.startTime = startTime
	s.endTime = time.Now()
}

// SetOutcome sets the outcome and recommendation.
func (s *summaryOverlay) SetOutcome(state, recommendation string) {
	s.outcome = state
	s.recommendation = recommendation
}

// SetTraceDir sets the trace directory path.
func (s *summaryOverlay) SetTraceDir(dir string) {
	s.traceDir = dir
}

// SetSaved records a successful scenario save.
func (s *summaryOverlay) SetSaved(dir string) {
	s.saving = false
	s.savedDir = dir
	s.saveErr = ""
}

// SetSaveError records a scenario save failure.
func (s *summaryOverlay) SetSaveError(err string) {
	s.saving = false
	s.saveErr = err
}

// SetSaving marks that a save is in progress.
func (s *summaryOverlay) SetSaving() {
	s.saving = true
}

// Hide closes the summary overlay.
func (s *summaryOverlay) Hide() {
	s.visible = false
}

// View renders the summary overlay.
func (s *summaryOverlay) View() string {
	if !s.visible {
		return ""
	}

	contentW := s.width - 8
	if contentW < 50 {
		contentW = 50
	}

	var b strings.Builder

	b.WriteString(summaryTitleStyle.Render("Run Complete"))
	b.WriteString("\n\n")

	// Outcome
	if s.outcome != "" {
		outcomeStyled := lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(s.outcome)
		b.WriteString(detailLabelStyle.Render("Outcome: ") + outcomeStyled)
		b.WriteString("\n")
		if s.recommendation != "" {
			b.WriteString(detailValueStyle.Render("  → " + s.recommendation))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Step stats
	statsLine := fmt.Sprintf("Steps: %d total", s.total)
	if s.passed > 0 {
		statsLine += ", " + summaryPassedStyle.Render(fmt.Sprintf("✓%d passed", s.passed))
	}
	if s.failed > 0 {
		statsLine += ", " + summaryFailedStyle.Render(fmt.Sprintf("✗%d failed", s.failed))
	}
	if s.skipped > 0 {
		statsLine += ", " + stepSkipped.Render(fmt.Sprintf("⏭%d skipped", s.skipped))
	}
	if s.outcomes > 0 {
		statsLine += ", " + lipgloss.NewStyle().Foreground(colorCyan).Render(fmt.Sprintf("◆%d outcome", s.outcomes))
	}
	b.WriteString(detailLabelStyle.Render("Stats:    ") + statsLine)
	b.WriteString("\n")

	// Duration
	duration := s.endTime.Sub(s.startTime)
	b.WriteString(detailLabelStyle.Render("Duration: ") + detailValueStyle.Render(formatDuration(duration)))
	b.WriteString("\n")

	// Trace path
	if s.traceDir != "" {
		b.WriteString(detailLabelStyle.Render("Trace:    ") + keyDescStyle.Render(s.traceDir))
		b.WriteString("\n")
	}

	// Save status
	if s.saving {
		b.WriteString("\n" + statusRunningStyle.Render("Saving scenario..."))
	}
	if s.savedDir != "" {
		b.WriteString("\n" + summaryPassedStyle.Render("✓ Scenario saved: ") + keyDescStyle.Render(s.savedDir))
	}
	if s.saveErr != "" {
		b.WriteString("\n" + errorStyle.Render("Save error: "+s.saveErr))
	}

	// Key hints
	b.WriteString("\n\n")
	b.WriteString(keyStyle.Render("q") + keyDescStyle.Render(":quit") + "  " +
		keyStyle.Render("s") + keyDescStyle.Render(":save scenario") + "  " +
		keyStyle.Render("v") + keyDescStyle.Render(":vars"))

	box := overlayBorder.Width(contentW).Render(b.String())
	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, box)
}

// formatDuration returns a human-friendly duration string.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m >= 60 {
		h := m / 60
		m = m % 60
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}
