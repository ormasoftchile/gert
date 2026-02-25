package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ormasoftchile/gert/pkg/inputs"
	"github.com/ormasoftchile/gert/pkg/serve"
)

// --- Tea messages ---

// serverEventMsg wraps a JSON-RPC notification from the server.
type serverEventMsg struct {
	Method string
	Params json.RawMessage
}

// startedMsg is sent after exec/start completes.
type startedMsg struct {
	result *startResult
	err    error
}

// nextDoneMsg is sent after exec/next completes.
type nextDoneMsg struct {
	result *nextResult
	err    error
}

// serverClosedMsg signals server pipe closed.
type serverClosedMsg struct{}

// errMsg wraps a fatal error.
type errMsg struct{ err error }

// varsResultMsg returns fetched variables to the model.
type varsResultMsg struct {
	vars *variablesResult
	err  error
}

// --- Overlay state ---

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayEvidence
	overlayChoice
	overlayVars
	overlaySummary
)

// --- Model ---

// Model is the top-level Bubble Tea model for the TUI.
type Model struct {
	// Components
	steps   stepsPanel
	output  outputPanel
	detail  detailBar
	spinner spinner.Model

	// Overlays
	evidence evidenceOverlay
	choice   choiceOverlay
	summary  summaryOverlay
	overlay  overlayKind

	// Search
	search searchBar

	// RPC client
	client *Client

	// State
	started      bool
	running      bool
	completed    bool
	awaitingUser bool   // server returned awaiting_user — waiting for choice/outcome/evidence
	pendingStep  string // stepID that is awaiting user action
	fatalErr     string

	// Vars display
	varsText string

	// Start parameters
	runbook     string
	mode        string
	vars        map[string]string
	scenarioDir string
	actor       string
	cwd         string

	// Layout
	compact bool // single-column mode for narrow terminals

	// Results
	runID string

	// Timing
	startTime time.Time

	// Layout
	width  int
	height int
}

// Config holds the parameters needed to launch the TUI.
type Config struct {
	Runbook     string
	Mode        string
	Vars        map[string]string
	ScenarioDir string
	Actor       string
	Cwd         string
	Compact     bool
	InputMgr    *inputs.Manager
}

// Run starts the TUI. It creates in-memory pipes, launches the serve engine
// in a goroutine, and runs the Bubble Tea program.
func Run(cfg Config) error {
	// In-memory pipes: TUI writes to clientW → server reads from serverR
	//                  Server writes to serverW → TUI reads from clientR
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	// Create server with pipe IO
	srv := serve.NewWithIO(serverR, serverW)
	if cfg.InputMgr != nil {
		srv.InputManager = cfg.InputMgr
	} else {
		srv.InputManager = inputs.NewManager()
	}

	// Run server in background
	go func() {
		_ = srv.Run()
		serverW.Close()
	}()

	// Create TUI client
	client := NewClient(clientR, clientW)
	go client.Listen()

	// Create spinner
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	m := Model{
		steps:    newStepsPanel(),
		output:   newOutputPanel(),
		detail:   newDetailBar(),
		spinner:  sp,
		evidence: newEvidenceOverlay(),
		choice:   newChoiceOverlay(),
		summary:  newSummaryOverlay(),
		search:   newSearchBar(),
		client:   client,

		runbook:     cfg.Runbook,
		mode:        cfg.Mode,
		vars:        cfg.Vars,
		scenarioDir: cfg.ScenarioDir,
		actor:       cfg.Actor,
		cwd:         cfg.Cwd,
		compact:     cfg.Compact,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// Init returns the initial commands: start spinner, listen for events, start execution.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.listenForEvents(),
		m.startExecution(),
	)
}

// listenForEvents returns a command that waits for the next server event.
func (m Model) listenForEvents() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.client.Events
		if !ok {
			return serverClosedMsg{}
		}
		return serverEventMsg{
			Method: msg.Method,
			Params: msg.Params,
		}
	}
}

// startExecution sends exec/start to the server.
func (m Model) startExecution() tea.Cmd {
	return func() tea.Msg {
		result, err := m.client.ExecStart(m.runbook, m.mode, m.vars, m.cwd, m.scenarioDir, m.actor)
		if err != nil {
			return startedMsg{err: err}
		}
		return startedMsg{result: result}
	}
}

// advanceStep sends exec/next to the server.
func (m Model) advanceStep() tea.Cmd {
	return func() tea.Msg {
		result, err := m.client.ExecNext()
		if err != nil {
			return nextDoneMsg{err: err}
		}
		return nextDoneMsg{result: result}
	}
}

// Update processes messages and returns the updated model and any commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutPanels()
		// Update overlay dimensions
		m.evidence.width = msg.Width
		m.evidence.height = msg.Height
		m.choice.width = msg.Width
		m.choice.height = msg.Height
		m.summary.width = msg.Width
		m.summary.height = msg.Height
		// Auto-detect compact mode for narrow terminals
		if msg.Width < 80 {
			m.compact = true
		}

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case startedMsg:
		if msg.err != nil {
			m.fatalErr = msg.err.Error()
			return m, nil
		}
		m.started = true
		m.running = true
		m.runID = msg.result.RunID
		m.startTime = time.Now()
		m.steps.SetSteps(msg.result.Steps)
		m.layoutPanels()

		// Auto-advance first step
		cmds = append(cmds, m.advanceStep())

	case nextDoneMsg:
		m.running = false
		if msg.err != nil {
			// Check if it's a "run completed" signal
			if strings.Contains(msg.err.Error(), "no more steps") ||
				strings.Contains(msg.err.Error(), "run complete") {
				m.completed = true
				m.detail.Clear()
			} else {
				m.fatalErr = msg.err.Error()
			}
		} else if msg.result != nil && msg.result.Status == "awaiting_user" {
			m.awaitingUser = true
			m.pendingStep = msg.result.StepID

			// Determine which overlay to show
			if msg.result.Choices != nil {
				// Choice step — show choice picker with instructions
				instructions := ""
				if msg.result.Instructions != "" {
					// Use width-constrained rendering for the overlay
					instructions = renderMarkdownWidth(msg.result.Instructions, m.choice.width-8)
				}
				m.choice.ShowChoice(
					msg.result.StepID,
					msg.result.Choices.Variable,
					msg.result.Choices.Prompt,
					instructions,
					msg.result.Choices.Options,
				)
				m.overlay = overlayChoice
			} else if msg.result.HasOutcomes && len(msg.result.Outcomes) > 0 {
				// Outcome routing — show outcome picker
				m.choice.ShowOutcome(msg.result.StepID, msg.result.Outcomes)
				m.overlay = overlayChoice
			} else {
				// Manual step without choices/outcomes — just acknowledge
				// User presses Enter to advance
				m.output.AppendOutput(msg.result.StepID,
					"\n"+detailLabelStyle.Render("Awaiting user action — press Enter to continue")+"\n")
			}
		}

	case evidenceSubmittedMsg:
		if msg.err != nil {
			m.output.AppendOutput(m.pendingStep,
				"\n"+errorStyle.Render("Evidence submission error: "+msg.err.Error())+"\n")
		}

	case choiceSubmittedMsg:
		// Hide the choice overlay — next step will show a new one if needed
		m.choice.Hide()
		m.overlay = overlayNone
		if msg.err != nil {
			m.output.AppendOutput(m.pendingStep,
				"\n"+errorStyle.Render("Choice submission error: "+msg.err.Error())+"\n")
		}

	case varsResultMsg:
		if msg.err == nil && msg.vars != nil {
			m.varsText = m.formatVars(msg.vars)
			m.overlay = overlayVars
		}

	case scenarioSavedMsg:
		if msg.err != nil {
			m.summary.SetSaveError(msg.err.Error())
		} else {
			m.summary.SetSaved(msg.outputDir)
		}

	case serverEventMsg:
		m.handleServerEvent(msg)
		// Continue listening for more events
		cmds = append(cmds, m.listenForEvents())

	case serverClosedMsg:
		m.completed = true
		m.running = false
	}

	return m, tea.Batch(cmds...)
}

// handleKey processes key presses.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Always allow quit (except when search is active — q is a character)
	if !m.search.IsActive() && matchKey(msg, keys.Quit) {
		go m.client.Shutdown()
		return m, tea.Quit
	}

	// Search bar active — route all input there
	if m.search.IsActive() {
		closed, committed, cmd := m.search.Update(msg)
		if closed {
			m.search.Close()
			m.output.ClearHighlight()
		}
		if committed || m.search.HasQuery() {
			m.output.SetHighlight(m.search.Query())
		}
		return m, cmd
	}

	// Escape closes overlays
	if msg.String() == "esc" {
		if m.overlay == overlayVars || m.overlay == overlaySummary {
			m.overlay = overlayNone
			return m, nil
		}
		// Clear search highlight
		if m.search.HasQuery() {
			m.search.Close()
			m.output.ClearHighlight()
			return m, nil
		}
	}

	// Route to active overlay first
	if m.overlay == overlayEvidence {
		submitted, ecmd := m.evidence.Update(msg)
		if submitted {
			cmd := m.submitEvidence()
			return m, cmd
		}
		return m, ecmd
	}

	if m.overlay == overlayChoice {
		selected := m.choice.Update(msg)
		if selected {
			cmd := m.submitChoice()
			return m, cmd
		}
		return m, nil
	}

	if m.overlay == overlaySummary {
		switch {
		case matchKey(msg, keys.Save):
			if !m.summary.saving && m.summary.savedDir == "" {
				m.summary.SetSaving()
				return m, m.saveScenarioCmd()
			}
		case matchKey(msg, keys.Vars):
			return m, m.fetchVarsCmd()
		}
		return m, nil
	}

	// Normal key handling (no overlay)
	switch {
	case matchKey(msg, keys.Advance):
		if m.awaitingUser && m.pendingStep != "" {
			// Acknowledge manual step — advance
			m.awaitingUser = false
			m.running = true
			return m, m.advanceStep()
		}
		if !m.running && m.started && !m.completed {
			m.running = true
			return m, m.advanceStep()
		}

	case matchKey(msg, keys.Up):
		m.steps.CursorUp()
		id := m.steps.SelectedID()
		m.output.ShowStep(id)

	case matchKey(msg, keys.Down):
		m.steps.CursorDown()
		id := m.steps.SelectedID()
		m.output.ShowStep(id)

	case matchKey(msg, keys.PgUp):
		m.output.PageUp()

	case matchKey(msg, keys.PgDown):
		m.output.PageDown()

	case matchKey(msg, keys.Vars):
		if m.started {
			return m, m.fetchVarsCmd()
		}

	case matchKey(msg, keys.Search):
		if m.started {
			m.search.Open()
			return m, nil
		}

	case matchKey(msg, keys.Save):
		if m.completed {
			m.showSummary()
			return m, nil
		}

	case matchKey(msg, keys.Help):
		// Future: dedicated help overlay
	}

	return m, nil
}

// matchKey checks if a key message matches a key.Binding.
func matchKey(msg tea.KeyMsg, binding key.Binding) bool {
	return key.Matches(msg, binding)
}

// handleServerEvent processes a JSON-RPC notification from the server.
func (m *Model) handleServerEvent(ev serverEventMsg) {
	switch ev.Method {
	case "event/stepStarted":
		var step stepEvent
		if err := json.Unmarshal(ev.Params, &step); err != nil {
			return
		}
		// Dynamically add steps not in the initial list (iterate expansions, invoked children)
		if !m.steps.HasStep(step.StepID) {
			m.steps.AddStep(step.StepID, step.Title, step.Type)
		}
		m.steps.SetStatus(step.StepID, statusCurrent)
		m.detail.SetStep(step)
		m.output.ShowStep(step.StepID)

		// Build initial output header
		header := fmt.Sprintf("━━━ Step: %s ━━━\n", step.StepID)
		if step.Title != "" {
			header += fmt.Sprintf("  %s\n", step.Title)
		}
		if step.Command != "" {
			header += fmt.Sprintf("  $ %s\n", commandStyle.Render(step.Command))
		}
		if step.Instructions != "" {
			header += renderMarkdown(step.Instructions) + "\n"
		}
		header += "\n"
		m.output.AppendOutput(step.StepID, header)

	case "event/stepCompleted":
		var step stepEvent
		if err := json.Unmarshal(ev.Params, &step); err != nil {
			return
		}

		status := statusPassed
		switch step.Status {
		case "passed":
			status = statusPassed
		case "failed":
			status = statusFailed
		case "skipped":
			status = statusSkipped
		}
		m.steps.SetStatus(step.StepID, status)
		m.detail.SetCompleted(step.Status, step.Error)

		if step.Error != "" {
			m.steps.SetStepError(step.StepID, step.Error)
			m.output.AppendOutput(step.StepID, "\n"+errorStyle.Render("Error: "+step.Error)+"\n")
		}

		// Show captures if any
		if len(step.Captures) > 0 {
			capText := "\nCaptures:\n"
			for k, v := range step.Captures {
				capText += fmt.Sprintf("  %s = %s\n", detailLabelStyle.Render(k), v)
			}
			m.output.AppendOutput(step.StepID, capText)
		}

		// Auto-advance: send next after a completed step
		m.running = false

	case "event/stepSkipped":
		var step stepEvent
		if err := json.Unmarshal(ev.Params, &step); err != nil {
			return
		}
		m.steps.SetStatus(step.StepID, statusSkipped)
		reason := "condition not met"
		if step.Reason != "" {
			reason = step.Reason
		}
		m.output.SetStepOutput(step.StepID,
			fmt.Sprintf("━━━ Step: %s ━━━\n  ⏭ Skipped: %s\n", step.StepID, reason))

	case "event/outcomeReached":
		var oc outcomeEvent
		if err := json.Unmarshal(ev.Params, &oc); err != nil {
			return
		}
		m.steps.SetStatus(oc.StepID, statusOutcome)
		m.detail.SetOutcome(oc.State, oc.Recommendation)
		m.summary.SetOutcome(oc.State, oc.Recommendation)
		m.output.AppendOutput(oc.StepID,
			fmt.Sprintf("\n%s Outcome: %s\n%s\n",
				GlyphOutcome, oc.State, oc.Recommendation))

	case "event/invokeStarted":
		var step stepEvent
		if err := json.Unmarshal(ev.Params, &step); err != nil {
			return
		}
		m.output.AppendOutput(step.StepID,
			fmt.Sprintf("\n  ▶ Invoking child runbook...\n"))

	case "event/invokeCompleted":
		var step stepEvent
		if err := json.Unmarshal(ev.Params, &step); err != nil {
			return
		}
		m.output.AppendOutput(step.StepID,
			fmt.Sprintf("  ◀ Child runbook completed\n"))

	case "event/runCompleted":
		m.completed = true
		m.running = false
		m.detail.Clear()
		m.showSummary()

	case "event/inputRequired":
		// Evidence / approval prompt from ServeCollector
		var req evidenceRequestMsg
		if err := json.Unmarshal(ev.Params, &req); err != nil {
			return
		}
		stepID := m.pendingStep
		if stepID == "" {
			stepID = m.steps.SelectedID()
		}
		m.evidence.Show(stepID, req)
		m.overlay = overlayEvidence

		label := fmt.Sprintf("\n%s requires %s evidence: %s\n",
			detailLabelStyle.Render(stepID), req.Kind, req.Name)
		m.output.AppendOutput(stepID, label)

	}
}

// submitEvidence collects the evidence overlay value and sends it to the server.
func (m *Model) submitEvidence() tea.Cmd {
	stepID := m.evidence.stepID
	name := m.evidence.name
	val := m.evidence.Value()

	m.evidence.Hide()
	m.overlay = overlayNone

	submitCmd := func() tea.Msg {
		evidence := map[string]interface{}{
			name: val,
		}
		err := m.client.SubmitEvidence(stepID, evidence)
		return evidenceSubmittedMsg{stepID: stepID, err: err}
	}
	return tea.Batch(tea.ClearScreen, submitCmd)
}

// submitChoice processes the selected choice or outcome and sends it to the server.
func (m *Model) submitChoice() tea.Cmd {
	if m.choice.isChoice {
		variable, value := m.choice.SelectedChoice()
		stepID := m.choice.stepID

		m.choice.Hide()
		m.overlay = overlayNone
		m.awaitingUser = false

		m.output.AppendOutput(stepID,
			fmt.Sprintf("\n  Choice: %s = %s\n", detailLabelStyle.Render(variable), value))

		submitCmd := func() tea.Msg {
			err := m.client.SubmitChoice(stepID, variable, value)
			if err != nil {
				return choiceSubmittedMsg{err: err}
			}
			// After submitting choice, advance to execute the step
			result, err := m.client.ExecNext()
			if err != nil {
				return nextDoneMsg{err: err}
			}
			return nextDoneMsg{result: result}
		}
		// Clear screen to prevent ghost content from previous overlay
		return tea.Batch(tea.ClearScreen, submitCmd)
	}

	if m.choice.isOutcome {
		state := m.choice.SelectedOutcome()
		stepID := m.choice.stepID

		m.choice.Hide()
		m.overlay = overlayNone
		m.awaitingUser = false

		m.output.AppendOutput(stepID,
			fmt.Sprintf("\n  Selected outcome: %s\n", lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(state)))

		submitCmd := func() tea.Msg {
			err := m.client.ChooseOutcome(stepID, state)
			return choiceSubmittedMsg{err: err}
		}
		// Clear screen to prevent ghost content from previous overlay
		return tea.Batch(tea.ClearScreen, submitCmd)
	}

	return nil
}

// fetchVarsCmd returns a tea.Cmd that fetches variables from the server.
func (m Model) fetchVarsCmd() tea.Cmd {
	return func() tea.Msg {
		vars, err := m.client.GetVariables()
		return varsResultMsg{vars: vars, err: err}
	}
}

// showSummary populates and displays the run summary overlay.
func (m *Model) showSummary() {
	total, passed, failed, skipped := m.steps.Stats()
	outcomes := 0
	for _, s := range m.steps.steps {
		if s.Status == statusOutcome {
			outcomes++
		}
	}
	m.summary.Show(m.runID, total, passed, failed, skipped, outcomes, m.startTime)
	if m.runID != "" {
		m.summary.SetTraceDir(filepath.Join(".runbook", "runs", m.runID, "trace.jsonl"))
	}
	m.overlay = overlaySummary
}

// saveScenarioCmd returns a tea.Cmd that saves the current run as a replay scenario.
func (m Model) saveScenarioCmd() tea.Cmd {
	runID := m.runID
	rbName := filepath.Base(m.runbook)
	rbName = strings.TrimSuffix(rbName, filepath.Ext(rbName))
	rbName = strings.TrimSuffix(rbName, ".runbook")
	outputDir := filepath.Join("scenarios", rbName, runID)

	return func() tea.Msg {
		dir, err := m.client.SaveScenario(outputDir)
		return scenarioSavedMsg{outputDir: dir, err: err}
	}
}

// formatVars formats variables for display.
func (m *Model) formatVars(vars *variablesResult) string {
	var b strings.Builder
	b.WriteString("━━━ Variables & Captures ━━━\n\n")

	if len(vars.Vars) > 0 {
		b.WriteString(detailLabelStyle.Render("Variables:") + "\n")
		for k, v := range vars.Vars {
			b.WriteString(fmt.Sprintf("  %s = %s\n", detailLabelStyle.Render(k), v))
		}
	}
	if len(vars.Captures) > 0 {
		b.WriteString("\n" + detailLabelStyle.Render("Captures:") + "\n")
		for k, v := range vars.Captures {
			b.WriteString(fmt.Sprintf("  %s = %s\n", detailLabelStyle.Render(k), v))
		}
	}

	if len(vars.Vars) == 0 && len(vars.Captures) == 0 {
		b.WriteString(keyDescStyle.Render("  (no variables set)"))
	}

	b.WriteString("\n\n" + keyStyle.Render("Esc") + keyDescStyle.Render(":close"))
	return b.String()
}

// layoutPanels recalculates panel dimensions based on terminal size.
func (m *Model) layoutPanels() {
	if m.width == 0 || m.height == 0 {
		return
	}

	// Layout: header(1) + main panels + detail bar(6) + key bar(1)
	headerH := 1
	detailH := 7
	mainH := m.height - headerH - detailH
	if mainH < 4 {
		mainH = 4
	}

	if m.compact {
		// Compact mode: full-width output, no steps panel visible
		m.steps.width = 0
		m.steps.height = 0
		m.output.SetSize(m.width, mainH)
	} else {
		// Steps panel: 30% width, minimum 25, maximum 45
		stepsW := m.width * 30 / 100
		if stepsW < 25 {
			stepsW = 25
		}
		if stepsW > 45 {
			stepsW = 45
		}

		outputW := m.width - stepsW

		m.steps.width = stepsW
		m.steps.height = mainH

		m.output.SetSize(outputW, mainH)
	}

	m.detail.width = m.width
}

// View renders the complete TUI.
func (m Model) View() string {
	if m.fatalErr != "" {
		return errorStyle.Render("Fatal: "+m.fatalErr) + "\n\nPress q to quit."
	}

	// Overlay views take over the full screen
	switch m.overlay {
	case overlayEvidence:
		return m.evidence.View()
	case overlayChoice:
		return m.choice.View()
	case overlayVars:
		return m.renderVarsOverlay()
	case overlaySummary:
		return m.summary.View()
	}

	// Header
	header := m.renderHeader()

	// Main panels
	var main string
	if m.width > 0 {
		if m.compact {
			// Compact mode: output only, step info in header
			outputView := m.output.View()
			main = outputView
		} else {
			stepsView := m.steps.View()
			outputView := m.output.View()
			main = lipgloss.JoinHorizontal(lipgloss.Top, stepsView, outputView)
		}
	}

	// Search bar (shown above detail bar when active or has query)
	searchView := m.search.View()

	// Detail bar
	detail := m.detail.View(m.running, m.completed)

	result := header + "\n" + main
	if searchView != "" {
		result += "\n" + searchView
	}
	result += "\n" + detail

	return result
}

// renderVarsOverlay renders the variables display as a centered overlay.
func (m Model) renderVarsOverlay() string {
	contentW := m.width - 8
	if contentW < 50 {
		contentW = 50
	}
	box := overlayBorder.Width(contentW).Render(m.varsText)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// renderHeader builds the top header line.
func (m Model) renderHeader() string {
	title := headerStyle.Render("gert")
	mode := modeBadgeStyle.Render(m.mode)

	var status string
	if m.completed {
		total, passed, failed, skipped := m.steps.Stats()
		status = fmt.Sprintf("%s/%s/%s/%d",
			summaryPassedStyle.Render(fmt.Sprintf("✓%d", passed)),
			summaryFailedStyle.Render(fmt.Sprintf("✗%d", failed)),
			stepSkipped.Render(fmt.Sprintf("⏭%d", skipped)),
			total)
	} else if m.running {
		status = m.spinner.View() + " executing"
	} else if m.started {
		status = "ready"
	} else {
		status = "loading..."
	}

	// Runbook name — extract filename
	rbName := m.runbook
	if idx := strings.LastIndex(rbName, "/"); idx >= 0 {
		rbName = rbName[idx+1:]
	}
	if idx := strings.LastIndex(rbName, "\\"); idx >= 0 {
		rbName = rbName[idx+1:]
	}

	left := title + " " + mode + "  " + detailValueStyle.Render(rbName)
	right := status

	padding := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if padding < 1 {
		padding = 1
	}

	return left + strings.Repeat(" ", padding) + right
}
