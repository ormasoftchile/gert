package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// searchBar renders an inline search field within the output panel.
type searchBar struct {
	active  bool
	input   textinput.Model
	query   string // committed search term
	matches int    // number of matches in current content
	current int    // current match index (0-based)
}

func newSearchBar() searchBar {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.CharLimit = 256
	ti.Width = 40
	ti.Prompt = "/ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	return searchBar{input: ti}
}

// Open activates the search bar and focuses the text input.
func (s *searchBar) Open() {
	s.active = true
	s.input.Reset()
	s.input.Focus()
	s.query = ""
	s.matches = 0
	s.current = 0
}

// Close deactivates the search bar.
func (s *searchBar) Close() {
	s.active = false
	s.input.Blur()
	s.query = ""
	s.matches = 0
	s.current = 0
}

// Update handles key events when the search bar is active.
// Returns (closed, committed, cmd).
// closed: user pressed Esc to close search.
// committed: user pressed Enter to commit and close.
func (s *searchBar) Update(msg tea.KeyMsg) (closed bool, committed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.Close()
		return true, false, nil
	case "enter":
		s.query = s.input.Value()
		s.active = false
		s.input.Blur()
		return false, true, nil
	}

	var c tea.Cmd
	s.input, c = s.input.Update(msg)
	// Live update the query for incremental search
	s.query = s.input.Value()
	return false, false, c
}

// Query returns the current search query.
func (s *searchBar) Query() string {
	return s.query
}

// IsActive returns whether the search bar is accepting input.
func (s *searchBar) IsActive() bool {
	return s.active
}

// HasQuery returns whether there's an active search (even after closing).
func (s *searchBar) HasQuery() bool {
	return s.query != ""
}

// SetMatchInfo updates the match count and current index for display.
func (s *searchBar) SetMatchInfo(matches, current int) {
	s.matches = matches
	s.current = current
}

// View renders the search bar.
func (s *searchBar) View() string {
	if !s.active && !s.HasQuery() {
		return ""
	}

	var result string
	if s.active {
		result = s.input.View()
	} else {
		result = keyDescStyle.Render("/" + s.query)
	}

	if s.matches > 0 {
		result += "  " + keyDescStyle.Render(strings.Join([]string{
			lipgloss.NewStyle().Foreground(colorCyan).Render(
				strings.Replace("N/M", "N", string(rune('0'+s.current+1)), 1)),
		}, ""))
	}

	// Simpler match info
	if s.HasQuery() {
		if s.matches > 0 {
			matchInfo := lipgloss.NewStyle().Foreground(colorGreen).Render(
				strings.TrimSpace(strings.Replace(strings.Replace("X matches", "X",
					intToStr(s.matches), 1), "matches", pluralize(s.matches, "match", "matches"), 1)))
			result += "  " + matchInfo
		} else if !s.active {
			result += "  " + lipgloss.NewStyle().Foreground(colorRed).Render("no matches")
		}
	}

	return result
}

func intToStr(n int) string {
	return strings.TrimSpace(strings.Replace("     ", " ", "", n))
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// HighlightContent returns the content with search matches highlighted.
// Returns the modified content and the number of matches found.
func HighlightContent(content, query string) (string, int) {
	if query == "" {
		return content, 0
	}

	lower := strings.ToLower(content)
	lowerQuery := strings.ToLower(query)

	count := strings.Count(lower, lowerQuery)
	if count == 0 {
		return content, 0
	}

	// Highlight matches with a distinctive style
	highlightStyle := lipgloss.NewStyle().
		Background(colorYellow).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	var result strings.Builder
	remaining := content
	remainingLower := lower

	for {
		idx := strings.Index(remainingLower, lowerQuery)
		if idx < 0 {
			result.WriteString(remaining)
			break
		}

		// Write text before match
		result.WriteString(remaining[:idx])

		// Write highlighted match (preserve original case)
		matchText := remaining[idx : idx+len(query)]
		result.WriteString(highlightStyle.Render(matchText))

		remaining = remaining[idx+len(query):]
		remainingLower = remainingLower[idx+len(lowerQuery):]
	}

	return result.String(), count
}
