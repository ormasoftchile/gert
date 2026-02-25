package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Messages ---

// choiceSubmittedMsg is sent after a choice or outcome is submitted.
type choiceSubmittedMsg struct {
	err error
}

// --- Choice option ---

type choiceOption struct {
	Value       string `json:"value"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// --- Choice overlay model ---

// choiceOverlay renders a selection list for choice steps or outcome routing.
type choiceOverlay struct {
	visible      bool
	title        string
	prompt       string
	stepID       string
	instructions string // rendered markdown instructions shown above the prompt

	// For choice steps
	isChoice bool
	variable string
	options  []choiceOption

	// For outcome steps
	isOutcome bool
	outcomes  []outcomeOption

	cursor    int
	scrollOff int // vertical scroll offset for long content

	width  int
	height int
}

func newChoiceOverlay() choiceOverlay {
	return choiceOverlay{}
}

// ShowChoice displays the overlay for a choice step.
func (c *choiceOverlay) ShowChoice(stepID, variable, prompt, instructions string, options []choiceOption) {
	c.visible = true
	c.isChoice = true
	c.isOutcome = false
	c.stepID = stepID
	c.variable = variable
	c.prompt = prompt
	c.instructions = instructions
	c.options = options
	c.title = "Choice: " + stepID
	c.cursor = 0
	c.scrollOff = 0
}

// ShowOutcome displays the overlay for outcome selection.
func (c *choiceOverlay) ShowOutcome(stepID string, outcomes []outcomeOption) {
	c.visible = true
	c.isOutcome = true
	c.isChoice = false
	c.stepID = stepID
	c.outcomes = outcomes
	c.title = "Outcome: " + stepID
	c.prompt = "Which outcome matches?"
	c.cursor = 0
	c.scrollOff = 0
}

// Hide closes the overlay.
func (c *choiceOverlay) Hide() {
	c.visible = false
}

// Update handles key events within the overlay. Returns true if a selection was made.
func (c *choiceOverlay) Update(msg tea.Msg) (selected bool) {
	if !c.visible {
		return false
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	maxIdx := c.itemCount() - 1

	switch keyMsg.String() {
	case "up", "k":
		if c.cursor > 0 {
			c.cursor--
		}
	case "down", "j":
		if c.cursor < maxIdx {
			c.cursor++
		}
	case "pgup":
		if c.scrollOff > 0 {
			c.scrollOff -= 5
			if c.scrollOff < 0 {
				c.scrollOff = 0
			}
		}
	case "pgdown":
		c.scrollOff += 5
	case "enter":
		return true
	case "esc":
		// Can't cancel — must choose. Ignore.
		return false
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(keyMsg.String()[0] - '1')
		if idx >= 0 && idx <= maxIdx {
			c.cursor = idx
			return true
		}
	}

	return false
}

func (c *choiceOverlay) itemCount() int {
	if c.isChoice {
		return len(c.options)
	}
	return len(c.outcomes)
}

// SelectedChoice returns the selected choice variable and value.
func (c *choiceOverlay) SelectedChoice() (variable, value string) {
	if c.isChoice && c.cursor < len(c.options) {
		return c.variable, c.options[c.cursor].Value
	}
	return "", ""
}

// SelectedOutcome returns the selected outcome state.
func (c *choiceOverlay) SelectedOutcome() string {
	if c.isOutcome && c.cursor < len(c.outcomes) {
		return c.outcomes[c.cursor].State
	}
	return ""
}

// View renders the choice overlay.
func (c *choiceOverlay) View() string {
	if !c.visible {
		return ""
	}

	contentW := c.width - 4
	if contentW < 50 {
		contentW = 50
	}

	var b strings.Builder

	b.WriteString(overlayTitle.Render(c.title))
	b.WriteString("\n\n")

	if c.instructions != "" {
		b.WriteString(c.instructions)
		b.WriteString("\n")
	}

	if c.prompt != "" {
		b.WriteString(detailValueStyle.Render(c.prompt))
		b.WriteString("\n\n")
	}

	if c.isChoice {
		for i, opt := range c.options {
			b.WriteString(c.renderChoiceItem(i, opt))
			b.WriteString("\n")
		}
	} else {
		for i, oc := range c.outcomes {
			b.WriteString(c.renderOutcomeItem(i, oc))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(keyStyle.Render("↑↓") + keyDescStyle.Render(":select") + "  " +
		keyStyle.Render("Enter") + keyDescStyle.Render(":choose") + "  " +
		keyStyle.Render("1-9") + keyDescStyle.Render(":quick select"))

	content := b.String()

	// Scroll support for tall content
	maxH := c.height - 6 // leave room for border + chrome
	lines := strings.Split(content, "\n")
	if len(lines) > maxH {
		if c.scrollOff > len(lines)-maxH {
			c.scrollOff = len(lines) - maxH
		}
		lines = lines[c.scrollOff:]
		if len(lines) > maxH {
			lines = lines[:maxH]
		}
		content = strings.Join(lines, "\n")
	}

	box := overlayBorder.Width(contentW).Render(content)
	return lipgloss.Place(c.width, c.height, lipgloss.Center, lipgloss.Center, box)
}

func (c *choiceOverlay) renderChoiceItem(idx int, opt choiceOption) string {
	prefix := "  "
	if idx == c.cursor {
		prefix = stepCurrent.Render("> ")
	}

	num := fmt.Sprintf("%d.", idx+1)
	label := opt.Label
	if label == "" {
		label = opt.Value
	}

	line := fmt.Sprintf("%s%s %s", prefix, keyStyle.Render(num), label)

	if opt.Description != "" {
		line += " " + keyDescStyle.Render("— "+opt.Description)
	}

	if idx == c.cursor {
		return stepCurrent.Render(line)
	}
	return line
}

func (c *choiceOverlay) renderOutcomeItem(idx int, oc outcomeOption) string {
	prefix := "  "
	if idx == c.cursor {
		prefix = stepCurrent.Render("> ")
	}

	num := fmt.Sprintf("%d.", idx+1)
	stateStyled := lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(oc.State)
	line := fmt.Sprintf("%s%s %s", prefix, keyStyle.Render(num), stateStyled)

	if oc.Recommendation != "" {
		line += " " + keyDescStyle.Render("→ "+oc.Recommendation)
	}

	if idx == c.cursor {
		return stepCurrent.Render(line)
	}
	return line
}
