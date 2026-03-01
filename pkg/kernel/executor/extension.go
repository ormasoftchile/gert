package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// ExtensionRunner communicates with an external tool runner via JSON-RPC 2.0 over stdio.
type ExtensionRunner struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
	nextID  atomic.Int64
	started bool
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      int64  `json:"id"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      int64           `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewExtensionRunner creates an extension runner for the given command.
func NewExtensionRunner(command string, args ...string) *ExtensionRunner {
	return &ExtensionRunner{
		cmd: exec.Command(command, args...),
	}
}

// Start spawns the runner process and sends the initialize handshake.
func (r *ExtensionRunner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stdin, err := r.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	r.stdin = stdin
	r.scanner = bufio.NewScanner(stdout)
	r.scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("start runner: %w", err)
	}
	r.started = true

	// Send initialize
	resp, err := r.callLocked(ctx, "initialize", map[string]any{"protocol_version": "1"})
	if err != nil {
		r.cmd.Process.Kill()
		return fmt.Errorf("initialize: %w", err)
	}
	_ = resp // capabilities — not used in v0
	return nil
}

// Execute sends an execute request to the runner and returns the result.
func (r *ExtensionRunner) Execute(ctx context.Context, inputs, vars map[string]any, contractInfo map[string]any) (*Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	params := map[string]any{
		"inputs": inputs,
		"vars":   vars,
	}
	if contractInfo != nil {
		params["contract"] = contractInfo
	}

	resp, err := r.callLocked(ctx, "execute", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		Outputs  map[string]any `json:"outputs"`
		ExitCode int            `json:"exit_code"`
		Stderr   string         `json:"stderr"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("unmarshal execute result: %w", err)
	}

	return &Result{
		ExitCode: result.ExitCode,
		Outputs:  result.Outputs,
		Stderr:   result.Stderr,
	}, nil
}

// Shutdown sends a shutdown request and waits for the process to exit.
func (r *ExtensionRunner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return nil
	}

	_, _ = r.callLocked(ctx, "shutdown", map[string]any{})
	r.stdin.Close()
	return r.cmd.Wait()
}

// callLocked sends a JSON-RPC request and reads the response. Must be called with mu held.
func (r *ExtensionRunner) callLocked(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := r.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := r.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read response
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		return nil, fmt.Errorf("runner closed stdout")
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(r.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("runner error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}
