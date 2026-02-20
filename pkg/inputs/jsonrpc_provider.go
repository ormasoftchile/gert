package inputs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// JSONRPCInputProvider resolves inputs by dispatching to an external binary
// that speaks JSON-RPC 2.0 over stdio.
type JSONRPCInputProvider struct {
	def     *schema.ProviderDefinition
	process *providerProcess
}

// providerProcess manages a persistent JSON-RPC provider subprocess.
type providerProcess struct {
	cmd    *exec.Cmd
	stdin  *json.Encoder
	reader *bufio.Reader
	nextID int64
	done   chan struct{}
}

// NewJSONRPCInputProvider creates a provider from a provider definition.
// The process is spawned lazily on first Resolve call.
func NewJSONRPCInputProvider(def *schema.ProviderDefinition) *JSONRPCInputProvider {
	return &JSONRPCInputProvider{def: def}
}

// Prefixes returns the from: prefixes this provider handles.
func (p *JSONRPCInputProvider) Prefixes() []string {
	if p.def.Capabilities.ResolveInputs != nil {
		return p.def.Capabilities.ResolveInputs.Prefixes
	}
	return nil
}

// Resolve spawns (or reuses) the provider process and sends a resolve request.
func (p *JSONRPCInputProvider) Resolve(ctx context.Context, req *ResolveRequest) (*ResolveResult, error) {
	proc, err := p.getOrSpawn(ctx)
	if err != nil {
		return nil, fmt.Errorf("spawn provider %q: %w", p.def.Meta.Name, err)
	}

	// Build JSON-RPC request
	params := map[string]interface{}{
		"bindings": req.Bindings,
		"context":  req.Context,
	}

	result, err := proc.call(ctx, "resolve", params)
	if err != nil {
		return nil, fmt.Errorf("provider %q resolve: %w", p.def.Meta.Name, err)
	}

	// Parse result
	var resolveResult ResolveResult
	if err := json.Unmarshal(result, &resolveResult); err != nil {
		return nil, fmt.Errorf("provider %q: unmarshal resolve result: %w", p.def.Meta.Name, err)
	}

	return &resolveResult, nil
}

// Shutdown stops the provider process.
func (p *JSONRPCInputProvider) Shutdown() error {
	if p.process == nil {
		return nil
	}
	shutdown := ""
	if p.def.Transport.Startup != nil {
		shutdown = p.def.Transport.Startup.ShutdownMethod
	}
	return p.process.shutdown(shutdown, 3*time.Second)
}

// getOrSpawn returns the existing process or spawns a new one.
func (p *JSONRPCInputProvider) getOrSpawn(ctx context.Context) (*providerProcess, error) {
	if p.process != nil {
		select {
		case <-p.process.done:
			p.process = nil // dead, respawn
		default:
			return p.process, nil
		}
	}

	binary := p.def.Meta.Binary
	if p.def.Transport.Binary != "" {
		binary = p.def.Transport.Binary
	}

	var argv []string
	if p.def.Transport.Startup != nil {
		argv = p.def.Transport.Startup.Argv
	}

	fmt.Fprintf(os.Stderr, "inputs: spawning provider %q (%s %v)\n",
		p.def.Meta.Name, binary, argv)

	proc, err := spawnProvider(ctx, binary, argv, p.def.Transport.Startup)
	if err != nil {
		return nil, err
	}

	p.process = proc
	return proc, nil
}

// spawnProvider starts a JSON-RPC provider process and waits for ready signal.
func spawnProvider(ctx context.Context, binary string, argv []string, startup *schema.ToolStartup) (*providerProcess, error) {
	cmd := exec.CommandContext(ctx, binary, argv...)
	cmd.Env = os.Environ()

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", binary, err)
	}

	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()

	proc := &providerProcess{
		cmd:    cmd,
		stdin:  json.NewEncoder(stdinPipe),
		reader: bufio.NewReader(stdoutPipe),
		done:   done,
	}

	// Wait for ready signal if configured
	if startup != nil && startup.ReadySignal != "" {
		timeout := 10 * time.Second
		if startup.Timeout != "" {
			if d, err := time.ParseDuration(startup.Timeout); err == nil {
				timeout = d
			}
		}

		readyCh := make(chan error, 1)
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Fprintf(os.Stderr, "  [provider:%s] %s\n", binary, line)
				if strings.Contains(line, startup.ReadySignal) {
					readyCh <- nil
					go func() {
						for scanner.Scan() {
							fmt.Fprintf(os.Stderr, "  [provider:%s] %s\n", binary, scanner.Text())
						}
					}()
					return
				}
			}
			readyCh <- fmt.Errorf("process exited before ready signal %q", startup.ReadySignal)
		}()

		select {
		case err := <-readyCh:
			if err != nil {
				proc.kill()
				return nil, err
			}
		case <-time.After(timeout):
			proc.kill()
			return nil, fmt.Errorf("provider %q: ready signal timeout after %v", binary, timeout)
		case <-done:
			return nil, fmt.Errorf("provider %q exited during startup", binary)
		}
	} else {
		// No ready signal — drain stderr
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				fmt.Fprintf(os.Stderr, "  [provider:%s] %s\n", binary, scanner.Text())
			}
		}()
		time.Sleep(50 * time.Millisecond)
	}

	return proc, nil
}

// call sends a JSON-RPC request and reads the response.
func (p *providerProcess) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&p.nextID, 1)

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}

	if err := p.stdin.Encode(req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read response with context timeout
	type readResult struct {
		data json.RawMessage
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := p.reader.ReadString('\n')
		if err != nil {
			ch <- readResult{err: fmt.Errorf("read response: %w", err)}
			return
		}

		var resp struct {
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			ch <- readResult{err: fmt.Errorf("unmarshal: %w (raw: %s)", err, strings.TrimSpace(line))}
			return
		}
		if resp.Error != nil {
			ch <- readResult{err: fmt.Errorf("[%d] %s", resp.Error.Code, resp.Error.Message)}
			return
		}
		ch <- readResult{data: resp.Result}
	}()

	select {
	case r := <-ch:
		return r.data, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.done:
		return nil, fmt.Errorf("provider process exited")
	}
}

// shutdown sends a shutdown notification and terminates.
func (p *providerProcess) shutdown(method string, grace time.Duration) error {
	if method != "" {
		notif := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  method,
		}
		p.stdin.Encode(notif)

		select {
		case <-p.done:
			return nil
		case <-time.After(grace):
		}
	}
	return p.kill()
}

// kill terminates the process.
func (p *providerProcess) kill() error {
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
