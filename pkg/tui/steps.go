package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// stepStatus tracks the display state of each step.
type stepStatus int

const (
	statusPending stepStatus = iota
	statusCurrent
	statusPassed
	statusFailed
	statusSkipped
	statusOutcome
)

// stepInfo holds the display state for a single step.
type stepInfo struct {
	ID     string
	Title  string
	Type   string
	Status stepStatus
	Error  string
	Depth  int // invoke nesting depth
}

// stepsPanel renders the scrollable step list.
type stepsPanel struct {
	steps    []stepInfo
	cursor   int // highlighted step (for browsing)
	current  int // currently executing step index
	width    int
	height   int
	offset   int // scroll offset
}

func newStepsPanel() stepsPanel {
	return stepsPanel{
		cursor: -1,
	}
}

// SetSteps initializes the step list from exec/start results.
func (p *stepsPanel) SetSteps(summaries []stepSummary) {
	p.steps = make([]stepInfo, len(summaries))
	for i, s := range summaries {
		p.steps[i] = stepInfo{
			ID:     s.ID,
			Title:  s.Title,
			Type:   s.Type,
			Status: statusPending,
		}
	}
}

// AddStep appends a dynamically discovered step (e.g. from an iterate block
// or an invoked child runbook) that wasn't in the initial step list.
func (p *stepsPanel) AddStep(id, title, typ string) {
	p.steps = append(p.steps, stepInfo{
		ID:     id,
		Title:  title,
		Type:   typ,
		Status: statusPending,
	})
}

// HasStep returns true if a step with the given ID is already tracked.
func (p *stepsPanel) HasStep(id string) bool {
	for _, s := range p.steps {
		if s.ID == id {
			return true
		}
	}
	return false
}

// SetStatus updates a step's status by ID.
func (p *stepsPanel) SetStatus(stepID string, status stepStatus) {
	for i := range p.steps {
		if p.steps[i].ID == stepID {
			p.steps[i].Status = status
			if status == statusCurrent {
				p.current = i
				p.cursor = i
			}
			return
		}
	}
}

// SetStepError records an error message on a step.
func (p *stepsPanel) SetStepError(stepID, errMsg string) {
	for i := range p.steps {
		if p.steps[i].ID == stepID {
			p.steps[i].Error = errMsg
			return
		}
	}
}

// CursorUp moves the browsing cursor up.
func (p *stepsPanel) CursorUp() {
	if p.cursor > 0 {
		p.cursor--
		p.ensureVisible()
	}
}

// CursorDown moves the browsing cursor down.
func (p *stepsPanel) CursorDown() {
	if p.cursor < len(p.steps)-1 {
		p.cursor++
		p.ensureVisible()
	}
}

// SelectedID returns the step ID at the cursor position.
func (p *stepsPanel) SelectedID() string {
	if p.cursor >= 0 && p.cursor < len(p.steps) {
		return p.steps[p.cursor].ID
	}
	return ""
}

func (p *stepsPanel) ensureVisible() {
	visible := p.height - 2 // account for border/title
	if visible < 1 {
		visible = 1
	}
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+visible {
		p.offset = p.cursor - visible + 1
	}
}

// View renders the step list panel.
func (p *stepsPanel) View() string {
	if len(p.steps) == 0 {
		return panelBorder.Width(p.width).Height(p.height).Render("  No steps loaded")
	}

	visible := p.height - 2
	if visible < 1 {
		visible = 1
	}

	var lines []string
	end := p.offset + visible
	if end > len(p.steps) {
		end = len(p.steps)
	}

	for i := p.offset; i < end; i++ {
		step := p.steps[i]

		// Glyph and style based on status
		var glyph string
		var style lipgloss.Style
		switch step.Status {
		case statusPending:
			glyph = GlyphPending
			style = stepNormal
		case statusCurrent:
			glyph = GlyphCurrent
			style = stepCurrent
		case statusPassed:
			glyph = GlyphPassed
			style = stepPassed
		case statusFailed:
			glyph = GlyphFailed
			style = stepFailed
		case statusSkipped:
			glyph = GlyphSkipped
			style = stepSkipped
		case statusOutcome:
			glyph = GlyphOutcome
			style = stepOutcome
		}

		// Indent for invoke depth
		indent := strings.Repeat("  ", step.Depth)

		// Title — truncate if too wide
		title := step.Title
		if title == "" {
			title = step.ID
		}
		maxTitle := p.width - 8 - len(indent) // glyph + padding + number
		if maxTitle < 4 {
			maxTitle = 4
		}
		if len(title) > maxTitle {
			title = title[:maxTitle-1] + "…"
		}

		num := fmt.Sprintf("%d.", i+1)
		line := fmt.Sprintf(" %s %s%s %s", glyph, indent, num, title)

		// Cursor indicator
		if i == p.cursor {
			line = style.Reverse(true).Render(line)
		} else {
			line = style.Render(line)
		}

		lines = append(lines, line)
	}

	// Pad remaining height
	for len(lines) < visible {
		lines = append(lines, "")
	}

	content := strings.Join(lines, "\n")

	title := panelTitle.Render("Steps")
	return panelBorder.Width(p.width).Height(p.height).Render(
		title + "\n" + content,
	)
}

// Stats returns counts of steps by status.
func (p *stepsPanel) Stats() (total, passed, failed, skipped int) {
	total = len(p.steps)
	for _, s := range p.steps {
		switch s.Status {
		case statusPassed:
			passed++
		case statusFailed:
			failed++
		case statusSkipped:
			skipped++
		}
	}
	return
}
