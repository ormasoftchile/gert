package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// jsonrpcProcess manages a persistent JSON-RPC tool server process.
type jsonrpcProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID int64
	mu     sync.Mutex
	done   chan struct{} // closed when process exits
}

// jsonrpcRequest is a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonrpcResponse is a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcError is a JSON-RPC error object.
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// spawnJSONRPC starts a long-lived tool process and waits for the ready signal.
func spawnJSONRPC(ctx context.Context, binary string, argv []string, startup *schema.ToolStartup) (*jsonrpcProcess, error) {
	cmd := exec.CommandContext(ctx, binary, argv...)
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start tool process %q: %w", binary, err)
	}

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	p := &jsonrpcProcess{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
		done:   done,
	}

	// Wait for ready signal on stderr if configured
	if startup != nil && startup.ReadySignal != "" {
		timeout := 10 * time.Second
		if startup.Timeout != "" {
			if d, err := parseDuration(startup.Timeout); err == nil {
				timeout = d
			}
		}

		readyCh := make(chan error, 1)
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", binary, line)
				if strings.Contains(line, startup.ReadySignal) {
					readyCh <- nil
					// Keep draining stderr to avoid blocking
					go func() {
						for scanner.Scan() {
							fmt.Fprintf(os.Stderr, "  [%s] %s\n", binary, scanner.Text())
						}
					}()
					return
				}
			}
			if err := scanner.Err(); err != nil {
				readyCh <- fmt.Errorf("reading stderr: %w", err)
			} else {
				readyCh <- fmt.Errorf("process exited before ready signal %q", startup.ReadySignal)
			}
		}()

		select {
		case err := <-readyCh:
			if err != nil {
				p.kill()
				return nil, err
			}
		case <-time.After(timeout):
			p.kill()
			return nil, fmt.Errorf("tool %q did not emit ready signal %q within %v", binary, startup.ReadySignal, timeout)
		case <-done:
			return nil, fmt.Errorf("tool process %q exited during startup", binary)
		}
	} else {
		// No ready signal — drain stderr in background
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", binary, scanner.Text())
			}
		}()
		// Brief pause to let process initialize
		time.Sleep(50 * time.Millisecond)
	}

	return p, nil
}

// Call sends a JSON-RPC request and waits for the response.
func (p *jsonrpcProcess) Call(method string, params interface{}) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if process is still alive
	select {
	case <-p.done:
		return nil, fmt.Errorf("tool process has exited")
	default:
	}

	id := atomic.AddInt64(&p.nextID, 1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Send request (newline-delimited)
	if _, err := fmt.Fprintf(p.stdin, "%s\n", data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read response line
	line, err := p.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (raw: %s)", err, strings.TrimSpace(line))
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tool error [%d]: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

// Shutdown sends a shutdown method (if configured) and terminates the process.
func (p *jsonrpcProcess) Shutdown(shutdownMethod string, grace time.Duration) error {
	if shutdownMethod != "" {
		// Send shutdown notification (no ID = notification)
		notif := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  shutdownMethod,
		}
		data, _ := json.Marshal(notif)
		fmt.Fprintf(p.stdin, "%s\n", data)

		// Wait for graceful exit
		select {
		case <-p.done:
			return nil
		case <-time.After(grace):
			// Fall through to kill
		}
	}

	return p.kill()
}

// kill terminates the process.
func (p *jsonrpcProcess) kill() error {
	p.stdin.Close()
	select {
	case <-p.done:
		return nil // already dead
	default:
	}
	if p.cmd.Process != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

// alive returns true if the process is still running.
func (p *jsonrpcProcess) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// parseDuration parses a duration string like "10s", "2m", "1h".
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// extractJSONPath extracts a value from a JSON document using a simple dot-path.
// Supports paths like "result.data", "items[0].name", etc.
func extractJSONPath(raw json.RawMessage, path string) (string, error) {
	if path == "" {
		return string(raw), nil
	}

	var obj interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("unmarshal JSON: %w", err)
	}

	parts := strings.Split(path, ".")
	current := obj
	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			next, ok := v[part]
			if !ok {
				return "", fmt.Errorf("key %q not found in JSON object", part)
			}
			current = next
		default:
			return "", fmt.Errorf("cannot index into %T with key %q", current, part)
		}
	}

	// Convert back to string
	switch v := current.(type) {
	case string:
		return v, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("marshal extracted value: %w", err)
		}
		return string(data), nil
	}
}
