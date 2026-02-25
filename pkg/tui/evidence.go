package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Evidence overlay styles ---

var (
	overlayBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorCyan).
			Padding(1, 2)

	overlayTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan)

	overlayInstructions = lipgloss.NewStyle().
				Foreground(colorDim).
				Italic(true)

	checkboxChecked = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	checkboxUnchecked = lipgloss.NewStyle().
				Foreground(colorDim)
)

// --- Evidence kinds ---

const (
	evidenceText       = "text"
	evidenceChecklist  = "checklist"
	evidenceAttachment = "attachment"
	evidenceApproval   = "approval"
)

// --- Messages ---

// evidenceRequestMsg is sent when the server needs evidence.
type evidenceRequestMsg struct {
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Instructions string   `json:"instructions,omitempty"`
	Items        []string `json:"items,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	Min          int      `json:"min,omitempty"`
}

// evidenceSubmittedMsg is sent after evidence is submitted to the server.
type evidenceSubmittedMsg struct {
	stepID string
	err    error
}

// --- Evidence overlay model ---

// evidenceOverlay renders a modal form for collecting evidence.
type evidenceOverlay struct {
	visible bool
	kind    string
	name    string
	stepID  string

	// text / attachment
	textInput    textinput.Model
	instructions string

	// checklist
	items      []string
	checked    []bool
	checkCursor int

	// approval
	roles []string
	min   int

	width  int
	height int
}

func newEvidenceOverlay() evidenceOverlay {
	ti := textinput.New()
	ti.Placeholder = "Enter value..."
	ti.CharLimit = 4096
	ti.Width = 60

	return evidenceOverlay{
		textInput: ti,
	}
}

// Show displays the evidence form for a given request.
func (e *evidenceOverlay) Show(stepID string, req evidenceRequestMsg) {
	e.visible = true
	e.kind = req.Kind
	e.name = req.Name
	e.stepID = stepID
	e.instructions = req.Instructions

	switch req.Kind {
	case evidenceText, evidenceAttachment:
		e.textInput.Reset()
		if req.Kind == evidenceAttachment {
			e.textInput.Placeholder = "Enter file path..."
		} else {
			e.textInput.Placeholder = "Enter value..."
		}
		e.textInput.Focus()

	case evidenceChecklist:
		e.items = req.Items
		e.checked = make([]bool, len(req.Items))
		e.checkCursor = 0

	case evidenceApproval:
		e.roles = req.Roles
		e.min = req.Min
	}
}

// Hide closes the overlay.
func (e *evidenceOverlay) Hide() {
	e.visible = false
	e.textInput.Blur()
}

// Update handles key events within the overlay.
func (e *evidenceOverlay) Update(msg tea.Msg) (submitted bool, cmd tea.Cmd) {
	if !e.visible {
		return false, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch e.kind {
		case evidenceText, evidenceAttachment:
			return e.updateTextInput(msg)
		case evidenceChecklist:
			return e.updateChecklist(msg)
		case evidenceApproval:
			// Auto-approve on Enter
			if msg.String() == "enter" {
				return true, nil
			}
			if msg.String() == "esc" {
				e.Hide()
				return false, nil
			}
		}
	}

	// Forward to text input for typing
	if e.kind == evidenceText || e.kind == evidenceAttachment {
		var cmd tea.Cmd
		e.textInput, cmd = e.textInput.Update(msg)
		return false, cmd
	}

	return false, nil
}

func (e *evidenceOverlay) updateTextInput(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if e.textInput.Value() != "" {
			return true, nil
		}
	case "esc":
		// Can't cancel evidence — it's required. Just ignore.
		return false, nil
	}

	var cmd tea.Cmd
	e.textInput, cmd = e.textInput.Update(msg)
	return false, cmd
}

func (e *evidenceOverlay) updateChecklist(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if e.checkCursor > 0 {
			e.checkCursor--
		}
	case "down", "j":
		if e.checkCursor < len(e.items)-1 {
			e.checkCursor++
		}
	case " ":
		if e.checkCursor >= 0 && e.checkCursor < len(e.checked) {
			e.checked[e.checkCursor] = !e.checked[e.checkCursor]
		}
	case "enter":
		return true, nil
	case "esc":
		return false, nil
	}
	return false, nil
}

// Value returns the collected evidence as an EvidenceValue-compatible map.
func (e *evidenceOverlay) Value() map[string]interface{} {
	switch e.kind {
	case evidenceText:
		return map[string]interface{}{
			"kind":  "text",
			"value": e.textInput.Value(),
		}
	case evidenceAttachment:
		return map[string]interface{}{
			"kind": "attachment",
			"path": e.textInput.Value(),
		}
	case evidenceChecklist:
		items := make(map[string]bool)
		for i, item := range e.items {
			if i < len(e.checked) {
				items[item] = e.checked[i]
			}
		}
		return map[string]interface{}{
			"kind":  "checklist",
			"items": items,
		}
	case evidenceApproval:
		return map[string]interface{}{
			"kind": "approval",
		}
	}
	return nil
}

// View renders the evidence overlay.
func (e *evidenceOverlay) View() string {
	if !e.visible {
		return ""
	}

	contentW := e.width - 8
	if contentW < 40 {
		contentW = 40
	}

	var content string

	switch e.kind {
	case evidenceText:
		content = e.renderTextForm(contentW)
	case evidenceAttachment:
		content = e.renderAttachmentForm(contentW)
	case evidenceChecklist:
		content = e.renderChecklistForm(contentW)
	case evidenceApproval:
		content = e.renderApprovalForm(contentW)
	}

	box := overlayBorder.Width(contentW).Render(content)

	// Center the overlay
	return lipgloss.Place(e.width, e.height, lipgloss.Center, lipgloss.Center, box)
}

func (e *evidenceOverlay) renderTextForm(w int) string {
	var b strings.Builder

	title := fmt.Sprintf("Evidence: %s", e.name)
	b.WriteString(overlayTitle.Render(title))
	b.WriteString("\n\n")

	if e.instructions != "" {
		b.WriteString(renderMarkdown(e.instructions))
		b.WriteString("\n\n")
	}

	e.textInput.Width = w - 4
	b.WriteString(e.textInput.View())
	b.WriteString("\n\n")

	b.WriteString(keyStyle.Render("Enter") + keyDescStyle.Render(":submit") + "  " +
		keyStyle.Render("Esc") + keyDescStyle.Render(":cancel"))

	return b.String()
}

func (e *evidenceOverlay) renderAttachmentForm(w int) string {
	var b strings.Builder

	title := fmt.Sprintf("Attachment: %s", e.name)
	b.WriteString(overlayTitle.Render(title))
	b.WriteString("\n\n")

	if e.instructions != "" {
		b.WriteString(renderMarkdown(e.instructions))
		b.WriteString("\n\n")
	}

	b.WriteString(detailLabelStyle.Render("File path:"))
	b.WriteString("\n")
	e.textInput.Width = w - 4
	b.WriteString(e.textInput.View())
	b.WriteString("\n\n")

	b.WriteString(keyStyle.Render("Enter") + keyDescStyle.Render(":submit") + "  " +
		keyStyle.Render("Esc") + keyDescStyle.Render(":cancel"))

	return b.String()
}

func (e *evidenceOverlay) renderChecklistForm(w int) string {
	var b strings.Builder

	title := fmt.Sprintf("Checklist: %s", e.name)
	b.WriteString(overlayTitle.Render(title))
	b.WriteString("\n\n")

	for i, item := range e.items {
		var checkbox string
		if i < len(e.checked) && e.checked[i] {
			checkbox = checkboxChecked.Render("[x]")
		} else {
			checkbox = checkboxUnchecked.Render("[ ]")
		}

		line := fmt.Sprintf("%s %s", checkbox, item)
		if i == e.checkCursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(keyStyle.Render("Space") + keyDescStyle.Render(":toggle") + "  " +
		keyStyle.Render("↑↓") + keyDescStyle.Render(":navigate") + "  " +
		keyStyle.Render("Enter") + keyDescStyle.Render(":submit"))

	return b.String()
}

func (e *evidenceOverlay) renderApprovalForm(w int) string {
	var b strings.Builder

	b.WriteString(overlayTitle.Render("Approval Required"))
	b.WriteString("\n\n")

	rolesStr := strings.Join(e.roles, ", ")
	b.WriteString(fmt.Sprintf("Requires %d approval(s) from: %s\n\n", e.min, rolesStr))
	b.WriteString(detailValueStyle.Render("Press Enter to approve as current actor."))
	b.WriteString("\n\n")

	b.WriteString(keyStyle.Render("Enter") + keyDescStyle.Render(":approve") + "  " +
		keyStyle.Render("Esc") + keyDescStyle.Render(":cancel"))

	return b.String()
}
