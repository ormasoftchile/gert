package tui

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ormasoftchile/gert/pkg/kernel/engine"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
	"github.com/ormasoftchile/gert/pkg/kernel/trace"
)

// StepState tracks the status of each step in the TUI.
type StepState struct {
	ID       string
	Type     string
	Status   string // "pending", "running", "success", "failed", "skipped"
	Duration time.Duration
	Output   string
}

// Model is the Bubble Tea model for gert-tui.
type Model struct {
	runbook     *schema.Runbook
	steps       []StepState
	selected    int
	traceEvents []trace.Event
	outcome     string
	outcomeCode string
	status      string // "idle", "running", "completed", "failed"
	width       int
	height      int
	err         error
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewModel creates a TUI model from a runbook.
func NewModel(rb *schema.Runbook) Model {
	steps := make([]StepState, 0, len(rb.Steps))
	for _, s := range rb.Steps {
		steps = append(steps, StepState{
			ID:     s.ID,
			Type:   string(s.Type),
			Status: "pending",
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		runbook: rb,
		steps:   steps,
		status:  "idle",
		ctx:     ctx,
		cancel:  cancel,
	}
}

// --- Messages ---

// traceEventMsg delivers a trace event to the TUI.
type traceEventMsg struct {
	Event trace.Event
}

// runCompleteMsg signals run completion.
type runCompleteMsg struct {
	Outcome     string
	OutcomeCode string
	Status      string
	Err         error
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.cancel()
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.steps)-1 {
				m.selected++
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case traceEventMsg:
		m.traceEvents = append(m.traceEvents, msg.Event)
		m.applyTraceEvent(msg.Event)

	case runCompleteMsg:
		m.status = msg.Status
		m.outcome = msg.Outcome
		m.outcomeCode = msg.OutcomeCode
		m.err = msg.Err
	}

	return m, nil
}

// applyTraceEvent updates step states based on trace events.
func (m *Model) applyTraceEvent(evt trace.Event) {
	stepID, _ := evt.Data["step_id"].(string)
	if stepID == "" {
		return
	}

	for i := range m.steps {
		if m.steps[i].ID != stepID {
			continue
		}

		switch evt.Type {
		case trace.EventStepStart:
			m.steps[i].Status = "running"
			m.status = "running"
		case trace.EventStepComplete:
			status, _ := evt.Data["status"].(string)
			switch status {
			case "success":
				m.steps[i].Status = "success"
			case "failed":
				m.steps[i].Status = "failed"
			case "skipped":
				m.steps[i].Status = "skipped"
			default:
				m.steps[i].Status = status
			}
			if d, ok := evt.Data["duration"].(string); ok {
				m.steps[i].Duration, _ = time.ParseDuration(d)
			}
		}
	}
}

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	b.WriteString(headerStyle.Render(fmt.Sprintf("  gert-tui: %s", m.runbook.Meta.Name)))
	b.WriteString("\n\n")

	// Step list
	for i, s := range m.steps {
		icon := stepIcon(s.Status)
		name := s.ID
		if name == "" {
			name = fmt.Sprintf("step-%d", i+1)
		}

		line := fmt.Sprintf("  %s %s [%s]", icon, name, s.Type)
		if s.Duration > 0 {
			line += fmt.Sprintf("  %s", s.Duration.Truncate(time.Millisecond))
		}

		if i == m.selected {
			selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
			b.WriteString(selectedStyle.Render("▸ " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}

	// Status bar
	b.WriteString("\n")
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	switch m.status {
	case "idle":
		b.WriteString(statusStyle.Render("  Ready"))
	case "running":
		b.WriteString(statusStyle.Render("  Running..."))
	case "completed":
		outcomeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("40"))
		b.WriteString(outcomeStyle.Render(fmt.Sprintf("  ✓ %s (%s)", m.outcome, m.outcomeCode)))
	case "failed":
		failStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
		errMsg := ""
		if m.err != nil {
			errMsg = m.err.Error()
		}
		b.WriteString(failStyle.Render(fmt.Sprintf("  ✗ Failed: %s", errMsg)))
	}

	// Output panel
	if m.selected < len(m.steps) {
		s := m.steps[m.selected]
		if s.Output != "" {
			b.WriteString("\n\n")
			b.WriteString(statusStyle.Render("  Output:"))
			b.WriteString("\n  " + s.Output)
		}
	}

	b.WriteString("\n\n")
	b.WriteString(statusStyle.Render("  q: quit  ↑/↓: navigate"))

	return b.String()
}

func stepIcon(status string) string {
	switch status {
	case "pending":
		return "○"
	case "running":
		return "◉"
	case "success":
		return "✓"
	case "failed":
		return "✗"
	case "skipped":
		return "⊘"
	default:
		return "?"
	}
}

// --- Engine Integration ---

// RunConfig holds parameters for the TUI engine execution.
type RunConfig struct {
	Mode        string
	Vars        map[string]string
	RunbookPath string
	Scenario    string            // for replay mode
	ToolExec    engine.ToolExecutor // optional (replay executor)
}

// StartEngine launches the kernel engine in a goroutine and feeds trace events to the TUI.
func (m Model) StartEngine(cfg RunConfig) tea.Cmd {
	return func() tea.Msg {
		// Create a trace writer that captures events
		var traceBuf bytes.Buffer
		tw := trace.NewWriter(&traceBuf, "tui-run")

		// Build engine config
		var stdout bytes.Buffer
		eCfg := engine.RunConfig{
			RunID:       "tui-run",
			Mode:        cfg.Mode,
			Vars:        cfg.Vars,
			BaseDir:     filepath.Dir(cfg.RunbookPath),
			Trace:       tw,
			Stdout:      &stdout,
			RunbookPath: cfg.RunbookPath,
		}
		if cfg.ToolExec != nil {
			eCfg.ToolExec = cfg.ToolExec
		}

		eng := engine.New(m.runbook, eCfg)
		result := eng.Run(m.ctx)

		outcomeStr := ""
		outcomeCode := ""
		if result.Outcome != nil {
			outcomeStr = string(result.Outcome.Category)
			outcomeCode = result.Outcome.Code
		}

		status := result.Status
		if status == "" {
			status = "completed"
		}

		return runCompleteMsg{
			Outcome:     outcomeStr,
			OutcomeCode: outcomeCode,
			Status:      status,
			Err:         result.Error,
		}
	}
}

// TUIApprovalProvider auto-approves in TUI mode (v0 — interactive approval is future work).
type TUIApprovalProvider struct{}

func (p *TUIApprovalProvider) Submit(ctx context.Context, req engine.ApprovalRequest) (*engine.ApprovalTicket, error) {
	return &engine.ApprovalTicket{
		TicketID: fmt.Sprintf("tui-%s-%s", req.RunID, req.StepID),
		Status:   "pending",
		Created:  time.Now(),
	}, nil
}

func (p *TUIApprovalProvider) Wait(ctx context.Context, ticket *engine.ApprovalTicket) (*engine.ApprovalResponse, error) {
	// v0: auto-approve in TUI (interactive modal is future work)
	return &engine.ApprovalResponse{
		TicketID:   ticket.TicketID,
		Approved:   true,
		ApproverID: "tui-auto",
		Method:     "tui-auto",
		Timestamp:  time.Now(),
	}, nil
}
