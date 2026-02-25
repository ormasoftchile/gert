package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// outputPanel renders scrollable command output for each step.
type outputPanel struct {
	viewport viewport.Model

	// outputs stores the output text per step ID.
	outputs map[string]string

	// activeStep is the step ID whose output is currently displayed.
	activeStep string

	// search highlight
	highlightQuery string

	width  int
	height int
	ready  bool
}

func newOutputPanel() outputPanel {
	return outputPanel{
		outputs: make(map[string]string),
	}
}

// SetSize updates the viewport dimensions.
func (p *outputPanel) SetSize(width, height int) {
	p.width = width
	p.height = height

	contentW := width - 4  // border padding
	contentH := height - 3 // title + border

	if contentW < 1 {
		contentW = 1
	}
	if contentH < 1 {
		contentH = 1
	}

	if !p.ready {
		p.viewport = viewport.New(contentW, contentH)
		p.viewport.HighPerformanceRendering = false
		p.ready = true
	} else {
		p.viewport.Width = contentW
		p.viewport.Height = contentH
	}

	// Re-render current content
	if content, ok := p.outputs[p.activeStep]; ok {
		p.viewport.SetContent(content)
	}
}

// AppendOutput adds text to a step's output buffer.
func (p *outputPanel) AppendOutput(stepID, text string) {
	existing := p.outputs[stepID]
	p.outputs[stepID] = existing + text

	if stepID == p.activeStep {
		p.refreshContent()
		p.viewport.GotoBottom()
	}
}

// SetStepOutput sets the complete output for a step.
func (p *outputPanel) SetStepOutput(stepID, text string) {
	p.outputs[stepID] = text
	if stepID == p.activeStep {
		p.refreshContent()
		p.viewport.GotoBottom()
	}
}

// ShowStep switches the displayed output to the given step.
func (p *outputPanel) ShowStep(stepID string) {
	p.activeStep = stepID
	if p.ready {
		p.refreshContent()
		p.viewport.GotoBottom()
	}
}

// Update handles viewport-specific messages (mouse scroll, etc.).
func (p *outputPanel) Update(msg tea.Msg) {
	if p.ready {
		p.viewport, _ = p.viewport.Update(msg)
	}
}

// PageUp scrolls the viewport up.
func (p *outputPanel) PageUp() {
	if p.ready {
		p.viewport.HalfViewUp()
	}
}

// PageDown scrolls the viewport down.
func (p *outputPanel) PageDown() {
	if p.ready {
		p.viewport.HalfViewDown()
	}
}

// SetHighlight sets the search highlight query and re-renders output.
func (p *outputPanel) SetHighlight(query string) {
	p.highlightQuery = query
	p.refreshContent()
}

// ClearHighlight removes search highlighting.
func (p *outputPanel) ClearHighlight() {
	p.highlightQuery = ""
	p.refreshContent()
}

// refreshContent re-renders the current step's content with highlighting.
func (p *outputPanel) refreshContent() {
	if !p.ready {
		return
	}
	content := p.outputs[p.activeStep]
	if p.highlightQuery != "" {
		highlighted, _ := HighlightContent(content, p.highlightQuery)
		p.viewport.SetContent(highlighted)
	} else {
		p.viewport.SetContent(content)
	}
}

// View renders the output panel.
func (p *outputPanel) View() string {
	title := panelTitle.Render("Output")

	var content string
	if p.ready {
		content = p.viewport.View()
	} else {
		content = "  Waiting for execution..."
	}

	// Scroll indicator
	scrollInfo := ""
	if p.ready && p.viewport.TotalLineCount() > p.viewport.VisibleLineCount() {
		pct := p.viewport.ScrollPercent() * 100
		scrollInfo = fmt.Sprintf(" %3.0f%%", pct)
	}

	header := title
	if scrollInfo != "" {
		padding := p.width - 4 - len("Output") - len(scrollInfo)
		if padding < 0 {
			padding = 0
		}
		header = title + strings.Repeat(" ", padding) + keyDescStyle.Render(scrollInfo)
	}

	return panelBorder.Width(p.width).Height(p.height).Render(
		header + "\n" + content,
	)
}
