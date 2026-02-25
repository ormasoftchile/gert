// Package tui implements a terminal user interface for governed runbook execution.
// It communicates with the gert engine via the same JSON-RPC protocol used by the
// VS Code extension, rendering an interactive Bubble Tea app in the terminal.
package tui

import "github.com/charmbracelet/lipgloss"

// Step status glyphs — convey meaning without relying on color alone.
const (
	GlyphPending    = "○"
	GlyphCurrent    = "▸"
	GlyphPassed     = "✓"
	GlyphFailed     = "✗"
	GlyphSkipped    = "⏭"
	GlyphOutcome    = "◆"
	GlyphIterating  = "⟳"
	GlyphEvidence   = "?"
)

// Palette adapts to terminal capabilities via lipgloss.
var (
	colorGreen   = lipgloss.Color("42")
	colorRed     = lipgloss.Color("196")
	colorYellow  = lipgloss.Color("214")
	colorBlue    = lipgloss.Color("39")
	colorCyan    = lipgloss.Color("51")
	colorDim     = lipgloss.Color("240")
	colorWhite   = lipgloss.Color("255")
	colorMagenta = lipgloss.Color("201")
)

// --- Header styles ---

var headerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(colorCyan).
	Padding(0, 1)

var modeBadgeStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("0")).
	Background(colorYellow).
	Padding(0, 1)

// --- Step list styles ---

var (
	stepNormal = lipgloss.NewStyle().
			Foreground(colorWhite)

	stepCurrent = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorYellow)

	stepPassed = lipgloss.NewStyle().
			Foreground(colorGreen)

	stepFailed = lipgloss.NewStyle().
			Foreground(colorRed)

	stepSkipped = lipgloss.NewStyle().
			Faint(true)

	stepOutcome = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)
)

// --- Panel styles ---

var (
	panelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim)

	panelTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan).
			Padding(0, 1)

	outputStyle = lipgloss.NewStyle().
			Foreground(colorWhite)

	commandStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true)
)

// --- Detail bar styles ---

var (
	detailBarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			BorderTop(true).
			BorderBottom(true).
			BorderLeft(true).
			BorderRight(true).
			Padding(0, 1)

	detailLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBlue)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	statusPassedStyle = lipgloss.NewStyle().
				Foreground(colorGreen).
				Bold(true)

	statusFailedStyle = lipgloss.NewStyle().
				Foreground(colorRed).
				Bold(true)

	statusRunningStyle = lipgloss.NewStyle().
				Foreground(colorYellow)
)

// --- Key bar styles ---

var (
	keyStyle = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	keyDescStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	keyBarStyle = lipgloss.NewStyle().
			Padding(0, 1)
)

// --- Outcome banner ---

var outcomeBannerStyle = lipgloss.NewStyle().
	Border(lipgloss.DoubleBorder()).
	BorderForeground(colorCyan).
	Foreground(colorCyan).
	Bold(true).
	Padding(0, 2).
	Align(lipgloss.Center)

// --- Error style ---

var errorStyle = lipgloss.NewStyle().
	Foreground(colorRed).
	Bold(true)

// --- Spinner style ---

var spinnerStyle = lipgloss.NewStyle().
	Foreground(colorYellow)

// --- Summary styles ---

var (
	summaryTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorCyan).
				Padding(0, 1)

	summaryStatStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	summaryPassedStyle = lipgloss.NewStyle().
				Foreground(colorGreen).
				Bold(true)

	summaryFailedStyle = lipgloss.NewStyle().
				Foreground(colorRed).
				Bold(true)
)
