package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

// renderer is a package-level glamour renderer (dark style, no word-wrap —
// the viewport handles wrapping).
var renderer *glamour.TermRenderer

func init() {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0), // let the viewport/panel handle wrapping
	)
	if err == nil {
		renderer = r
	}
}

// renderMarkdown converts a markdown string to styled terminal output.
// Falls back to the raw input if glamour is unavailable or rendering fails.
func renderMarkdown(md string) string {
	if renderer == nil || strings.TrimSpace(md) == "" {
		return md
	}
	out, err := renderer.Render(md)
	if err != nil {
		return md
	}
	// Glamour adds trailing newlines; trim for inline use
	return strings.TrimRight(out, "\n")
}

// renderMarkdownWidth renders markdown constrained to a specific column width.
// Used for overlays where the viewport doesn't control wrapping.
func renderMarkdownWidth(md string, width int) string {
	if strings.TrimSpace(md) == "" {
		return md
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return strings.TrimRight(out, "\n")
}
