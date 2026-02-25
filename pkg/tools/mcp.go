package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// mcpProcess manages a persistent MCP server process.
// MCP uses JSON-RPC 2.0 over stdio with an initialization handshake.
type mcpProcess struct {
	cmd    *exec.Cmd
	stdin  *json.Encoder
	reader *bufio.Reader
	nextID int64
	tools  map[string]bool // discovered tool names from tools/list
	mu     sync.Mutex
	done   chan struct{}
}

// mcpContent is an item in an MCP tools/call response content array.
type mcpContent struct {
	Type string `json:"type"` // "text" or "image"
	Text string `json:"text,omitempty"`
}

// mcpCallResult is the result of an MCP tools/call response.
type mcpCallResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// spawnMCP starts an MCP server process and performs the initialization handshake.
func spawnMCP(ctx context.Context, binary string, argv []string, startup *schema.ToolStartup) (*mcpProcess, error) {
	cmd := exec.CommandContext(ctx, binary, argv...)
	cmd.Env = os.Environ()

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start MCP process %q: %w", binary, err)
	}

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	// Drain stderr in background
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			fmt.Fprintf(os.Stderr, "  [mcp:%s] %s\n", binary, scanner.Text())
		}
	}()

	p := &mcpProcess{
		cmd:    cmd,
		stdin:  json.NewEncoder(stdinPipe),
		reader: bufio.NewReader(stdout),
		tools:  make(map[string]bool),
		done:   done,
	}

	// Initialization timeout
	timeout := 15 * time.Second
	if startup != nil && startup.Timeout != "" {
		if d, err := parseDuration(startup.Timeout); err == nil {
			timeout = d
		}
	}

	initCtx, initCancel := context.WithTimeout(ctx, timeout)
	defer initCancel()

	// Step 1: Send initialize request
	if err := p.sendInitialize(initCtx); err != nil {
		p.kill()
		return nil, fmt.Errorf("MCP initialize: %w", err)
	}

	// Step 2: Send initialized notification
	p.sendNotification("notifications/initialized", nil)

	// Step 3: Discover available tools
	if err := p.discoverTools(initCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  [mcp:%s] warning: tools/list failed: %v\n", binary, err)
		// Non-fatal — tools may be called by name even without discovery
	}

	fmt.Fprintf(os.Stderr, "  [mcp:%s] initialized, %d tools discovered\n", binary, len(p.tools))
	return p, nil
}

// sendInitialize sends the MCP initialize request and reads the response.
func (p *mcpProcess) sendInitialize(ctx context.Context) error {
	id := atomic.AddInt64(&p.nextID, 1)
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":   map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "gert",
				"version": "0.1.0",
			},
		},
	}

	if err := p.writeMessage(req); err != nil {
		return fmt.Errorf("send initialize: %w", err)
	}

	// Read response
	resp, err := p.readResponse(ctx)
	if err != nil {
		return fmt.Errorf("read initialize response: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error [%d]: %s", resp.Error.Code, resp.Error.Message)
	}

	return nil
}

// sendNotification sends a JSON-RPC notification (no id, no response expected).
func (p *mcpProcess) sendNotification(method string, params interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	p.writeMessage(msg)
}

// discoverTools calls tools/list to discover available tool names.
func (p *mcpProcess) discoverTools(ctx context.Context) error {
	id := atomic.AddInt64(&p.nextID, 1)
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/list",
	}

	if err := p.writeMessage(req); err != nil {
		return err
	}

	resp, err := p.readResponse(ctx)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("tools/list error [%d]: %s", resp.Error.Code, resp.Error.Message)
	}

	// Parse tool list from result
	var listResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &listResult); err != nil {
		return fmt.Errorf("parse tools/list result: %w", err)
	}

	for _, t := range listResult.Tools {
		p.tools[t.Name] = true
	}
	return nil
}

// CallTool invokes an MCP tool by name with the given arguments.
func (p *mcpProcess) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case <-p.done:
		return "", fmt.Errorf("MCP process has exited")
	default:
	}

	id := atomic.AddInt64(&p.nextID, 1)
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	if err := p.writeMessage(req); err != nil {
		return "", fmt.Errorf("send tools/call: %w", err)
	}

	resp, err := p.readResponse(ctx)
	if err != nil {
		return "", fmt.Errorf("read tools/call response: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("tools/call error [%d]: %s", resp.Error.Code, resp.Error.Message)
	}

	// Parse MCP call result
	var callResult mcpCallResult
	if err := json.Unmarshal(resp.Result, &callResult); err != nil {
		// Fallback: return raw result as string
		return string(resp.Result), nil
	}

	if callResult.IsError {
		// Collect error text from content
		var errTexts []string
		for _, c := range callResult.Content {
			if c.Type == "text" {
				errTexts = append(errTexts, c.Text)
			}
		}
		return "", fmt.Errorf("MCP tool error: %s", strings.Join(errTexts, "; "))
	}

	// Collect text content
	var texts []string
	for _, c := range callResult.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// Shutdown sends a graceful shutdown and terminates the process.
func (p *mcpProcess) Shutdown(grace time.Duration) error {
	// MCP doesn't have a standard shutdown method — just close stdin
	p.cmd.Process.Signal(os.Interrupt)
	select {
	case <-p.done:
		return nil
	case <-time.After(grace):
		return p.kill()
	}
}

// kill terminates the process.
func (p *mcpProcess) kill() error {
	select {
	case <-p.done:
		return nil
	default:
	}
	if p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

// alive returns true if the process is still running.
func (p *mcpProcess) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// writeMessage encodes and writes a JSON message followed by a newline.
func (p *mcpProcess) writeMessage(msg interface{}) error {
	return p.stdin.Encode(msg)
}

// readResponse reads a single JSON-RPC response, skipping notifications.
func (p *mcpProcess) readResponse(ctx context.Context) (*jsonrpcResponse, error) {
	type readResult struct {
		resp *jsonrpcResponse
		err  error
	}

	ch := make(chan readResult, 1)
	go func() {
		for {
			line, err := p.reader.ReadString('\n')
			if err != nil {
				ch <- readResult{err: fmt.Errorf("read: %w", err)}
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var msg json.RawMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}

			// Check if this is a response (has "id") or a notification (has "method" only)
			var peek struct {
				ID     *int64 `json:"id"`
				Method string `json:"method"`
			}
			json.Unmarshal([]byte(line), &peek)

			// Skip notifications (server-initiated messages with method but no id)
			if peek.ID == nil && peek.Method != "" {
				continue
			}

			var resp jsonrpcResponse
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				ch <- readResult{err: fmt.Errorf("unmarshal response: %w", err)}
				return
			}
			ch <- readResult{resp: &resp}
			return
		}
	}()

	select {
	case result := <-ch:
		return result.resp, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.done:
		return nil, fmt.Errorf("MCP process exited while waiting for response")
	}
}
