package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestExtensionRunner_Protocol(t *testing.T) {
	// Simulate a JSON-RPC runner using pipes
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	runner := &ExtensionRunner{
		stdin:   clientWrite,
		scanner: bufio.NewScanner(clientRead),
		started: true,
	}
	runner.scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Mock server in goroutine
	go func() {
		scan := bufio.NewScanner(serverRead)
		for scan.Scan() {
			var req jsonRPCRequest
			if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
				continue
			}

			var resp jsonRPCResponse
			resp.JSONRPC = "2.0"
			resp.ID = req.ID

			switch req.Method {
			case "initialize":
				resp.Result = json.RawMessage(`{"capabilities":{}}`)
			case "execute":
				resp.Result = json.RawMessage(`{"outputs":{"status":"ok"},"exit_code":0,"stderr":""}`)
			case "shutdown":
				resp.Result = json.RawMessage(`{}`)
			default:
				resp.Error = &jsonRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)}
			}

			data, _ := json.Marshal(resp)
			data = append(data, '\n')
			serverWrite.Write(data)

			if req.Method == "shutdown" {
				serverWrite.Close()
				return
			}
		}
	}()

	ctx := context.Background()

	// Test initialize
	t.Run("initialize", func(t *testing.T) {
		runner.mu.Lock()
		_, err := runner.callLocked(ctx, "initialize", map[string]any{"protocol_version": "1"})
		runner.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	})

	// Test execute
	t.Run("execute", func(t *testing.T) {
		result, err := runner.Execute(ctx, map[string]any{"input": "test"}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.ExitCode != 0 {
			t.Errorf("ExitCode = %d, want 0", result.ExitCode)
		}
		if result.Outputs["status"] != "ok" {
			t.Errorf("Outputs[status] = %v, want ok", result.Outputs["status"])
		}
	})

	// Test shutdown
	t.Run("shutdown", func(t *testing.T) {
		runner.mu.Lock()
		_, err := runner.callLocked(ctx, "shutdown", map[string]any{})
		runner.mu.Unlock()
		if err != nil && !strings.Contains(err.Error(), "closed") {
			t.Fatal(err)
		}
	})
}
