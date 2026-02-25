package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// rpcMessage is the JSON-RPC 2.0 message exchanged with gert serve.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// stepSummary mirrors the step info returned by exec/start.
type stepSummary struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Index       int    `json:"index"`
	When        string `json:"when,omitempty"`
	HasOutcomes bool   `json:"hasOutcomes,omitempty"`
}

// startResult is the response from exec/start.
type startResult struct {
	RunID     string        `json:"runId"`
	BaseDir   string        `json:"baseDir"`
	StepCount int           `json:"stepCount"`
	Steps     []stepSummary `json:"steps"`
}

// stepEvent is the payload for event/stepStarted and event/stepCompleted.
type stepEvent struct {
	StepID       string            `json:"stepId"`
	Index        int               `json:"index,omitempty"`
	Type         string            `json:"type,omitempty"`
	Title        string            `json:"title,omitempty"`
	Status       string            `json:"status,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
	Command      string            `json:"command,omitempty"`
	Query        string            `json:"query,omitempty"`
	QueryType    string            `json:"queryType,omitempty"`
	Captures     map[string]string `json:"captures,omitempty"`
	Error        string            `json:"error,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	InvokeChild  bool              `json:"invokeChild,omitempty"`
}

// outcomeEvent is the payload for event/outcomeReached.
type outcomeEvent struct {
	StepID         string `json:"stepId"`
	State          string `json:"state"`
	Recommendation string `json:"recommendation"`
}

// variablesResult is the response from exec/getVariables.
type variablesResult struct {
	Vars     map[string]string `json:"vars"`
	Captures map[string]string `json:"captures"`
}

// nextResult is the response from exec/next.
type nextResult struct {
	StepID         string            `json:"stepId"`
	Status         string            `json:"status"`
	Title          string            `json:"title,omitempty"`
	Type           string            `json:"type,omitempty"`
	Instructions   string            `json:"instructions,omitempty"`
	Captures       map[string]string `json:"captures,omitempty"`
	Error          string            `json:"error,omitempty"`
	OutcomeState   string            `json:"outcomeState,omitempty"`
	Recommendation string            `json:"recommendation,omitempty"`
	HasOutcomes    bool              `json:"hasOutcomes,omitempty"`
	Outcomes       []outcomeOption   `json:"outcomes,omitempty"`
	Choices        *choicesPayload   `json:"choices,omitempty"`
}

// choicesPayload mirrors the choices block in exec/next awaiting_user responses.
type choicesPayload struct {
	Variable string         `json:"variable"`
	Prompt   string         `json:"prompt,omitempty"`
	Options  []choiceOption `json:"options"`
}

// outcomeOption mirrors the outcome summaries in exec/next awaiting_user responses.
type outcomeOption struct {
	State          string `json:"state"`
	Recommendation string `json:"recommendation,omitempty"`
	When           string `json:"when,omitempty"`
}

// Client communicates with gert serve over in-memory JSON-RPC pipes.
type Client struct {
	writer io.Writer
	reader *bufio.Scanner
	nextID int
	mu     sync.Mutex

	// pending maps request IDs to response channels.
	pending map[int]chan *rpcMessage

	// Events is written to when server sends notifications (events).
	Events chan *rpcMessage

	done chan struct{}
}

// NewClient creates a client that reads from r and writes to w.
// Call Listen() in a goroutine to start processing server messages.
func NewClient(r io.Reader, w io.Writer) *Client {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	return &Client{
		writer:  w,
		reader:  scanner,
		pending: make(map[int]chan *rpcMessage),
		Events:  make(chan *rpcMessage, 64),
		done:    make(chan struct{}),
	}
}

// Listen reads messages from the server and dispatches them.
// Call this in a goroutine. It returns when the pipe closes.
func (c *Client) Listen() {
	defer close(c.done)
	for c.reader.Scan() {
		line := c.reader.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.ID != nil && msg.Error == nil && msg.Result != nil {
			// Response to a request
			c.mu.Lock()
			ch, ok := c.pending[*msg.ID]
			if ok {
				delete(c.pending, *msg.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- &msg
			}
		} else if msg.ID != nil && msg.Error != nil {
			// Error response
			c.mu.Lock()
			ch, ok := c.pending[*msg.ID]
			if ok {
				delete(c.pending, *msg.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- &msg
			}
		} else if msg.Method != "" {
			// Event notification
			select {
			case c.Events <- &msg:
			default:
				// drop if event channel is full
			}
		}
	}
}

// request sends a JSON-RPC request and waits for the response.
func (c *Client) request(method string, params interface{}) (*rpcMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan *rpcMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	} else {
		rawParams = json.RawMessage("{}")
	}

	msg := rpcMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  rawParams,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.mu.Lock()
	_, err = fmt.Fprintf(c.writer, "%s\n", data)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Wait for response
	resp := <-ch
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp, nil
}

// ExecStart starts runbook execution.
func (c *Client) ExecStart(runbook, mode string, vars map[string]string, cwd, scenarioDir, actor string) (*startResult, error) {
	params := map[string]interface{}{
		"runbook": runbook,
		"mode":    mode,
	}
	if len(vars) > 0 {
		params["vars"] = vars
	}
	if cwd != "" {
		params["cwd"] = cwd
	}
	if scenarioDir != "" {
		params["scenarioDir"] = scenarioDir
	}
	if actor != "" {
		params["actor"] = actor
	}

	resp, err := c.request("exec/start", params)
	if err != nil {
		return nil, err
	}
	var result startResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode start result: %w", err)
	}
	return &result, nil
}

// ExecNext advances to the next step.
func (c *Client) ExecNext() (*nextResult, error) {
	resp, err := c.request("exec/next", nil)
	if err != nil {
		return nil, err
	}
	var result nextResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode next result: %w", err)
	}
	return &result, nil
}

// ChooseOutcome sends an outcome choice.
func (c *Client) ChooseOutcome(stepID, state string) error {
	_, err := c.request("exec/chooseOutcome", map[string]string{
		"stepId": stepID,
		"state":  state,
	})
	return err
}

// GetVariables retrieves current variables and captures.
func (c *Client) GetVariables() (*variablesResult, error) {
	resp, err := c.request("exec/getVariables", nil)
	if err != nil {
		return nil, err
	}
	var result variablesResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode variables result: %w", err)
	}
	return &result, nil
}

// SubmitChoice sends a choice selection for a step.
func (c *Client) SubmitChoice(stepID, variable, value string) error {
	_, err := c.request("exec/submitChoice", map[string]string{
		"stepId":   stepID,
		"variable": variable,
		"value":    value,
	})
	return err
}

// SubmitEvidence sends collected evidence for a step.
func (c *Client) SubmitEvidence(stepID string, evidence map[string]interface{}) error {
	params := map[string]interface{}{
		"stepId":   stepID,
		"evidence": evidence,
	}
	_, err := c.request("exec/submitEvidence", params)
	return err
}

// SaveScenario saves the current run as a replay scenario.
func (c *Client) SaveScenario(outputDir string) (string, error) {
	resp, err := c.request("exec/saveScenario", map[string]string{
		"outputDir": outputDir,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Status    string `json:"status"`
		OutputDir string `json:"outputDir"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("decode save result: %w", err)
	}
	return result.OutputDir, nil
}

// Shutdown sends a graceful shutdown request.
func (c *Client) Shutdown() error {
	_, err := c.request("shutdown", nil)
	return err
}
