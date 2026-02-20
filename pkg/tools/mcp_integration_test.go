package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// TestMCPIntegration builds a mock MCP server and tests the full flow:
// spawn, initialize handshake, tools/list discovery, tools/call, shutdown.
func TestMCPIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockBin := buildMockMCPServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	t.Run("spawn and discover tools", func(t *testing.T) {
		proc, err := spawnMCP(ctx, mockBin, nil, &schema.ToolStartup{
			Timeout: "5s",
		})
		if err != nil {
			t.Fatalf("spawnMCP: %v", err)
		}
		defer proc.Shutdown(2 * time.Second)

		if !proc.alive() {
			t.Fatal("process should be alive after spawn")
		}

		// Should have discovered tools
		if len(proc.tools) == 0 {
			t.Fatal("expected discovered tools, got none")
		}
		if !proc.tools["echo"] {
			t.Error("expected 'echo' tool to be discovered")
		}
		if !proc.tools["query"] {
			t.Error("expected 'query' tool to be discovered")
		}
	})

	t.Run("call echo tool", func(t *testing.T) {
		proc, err := spawnMCP(ctx, mockBin, nil, &schema.ToolStartup{Timeout: "5s"})
		if err != nil {
			t.Fatalf("spawnMCP: %v", err)
		}
		defer proc.Shutdown(2 * time.Second)

		result, err := proc.CallTool(ctx, "echo", map[string]interface{}{
			"message": "hello-from-mcp",
		})
		if err != nil {
			t.Fatalf("CallTool echo: %v", err)
		}
		if result != "hello-from-mcp" {
			t.Errorf("result = %q, want %q", result, "hello-from-mcp")
		}
	})

	t.Run("call query tool", func(t *testing.T) {
		proc, err := spawnMCP(ctx, mockBin, nil, &schema.ToolStartup{Timeout: "5s"})
		if err != nil {
			t.Fatalf("spawnMCP: %v", err)
		}
		defer proc.Shutdown(2 * time.Second)

		result, err := proc.CallTool(ctx, "query", nil)
		if err != nil {
			t.Fatalf("CallTool query: %v", err)
		}
		if !contains(result, "mcp-query-result") {
			t.Errorf("result = %q, expected to contain 'mcp-query-result'", result)
		}
	})

	t.Run("tool error", func(t *testing.T) {
		proc, err := spawnMCP(ctx, mockBin, nil, &schema.ToolStartup{Timeout: "5s"})
		if err != nil {
			t.Fatalf("spawnMCP: %v", err)
		}
		defer proc.Shutdown(2 * time.Second)

		_, err = proc.CallTool(ctx, "failing", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !contains(err.Error(), "something went wrong") {
			t.Errorf("error = %q, expected 'something went wrong'", err.Error())
		}
	})

	t.Run("multiple calls reuse process", func(t *testing.T) {
		proc, err := spawnMCP(ctx, mockBin, nil, &schema.ToolStartup{Timeout: "5s"})
		if err != nil {
			t.Fatalf("spawnMCP: %v", err)
		}
		defer proc.Shutdown(2 * time.Second)

		for i := 0; i < 3; i++ {
			result, err := proc.CallTool(ctx, "echo", map[string]interface{}{
				"message": "iteration",
			})
			if err != nil {
				t.Fatalf("Call iteration %d: %v", i, err)
			}
			if result != "iteration" {
				t.Errorf("iteration %d: got %q, want %q", i, result, "iteration")
			}
		}
	})
}

// TestMCPManagerIntegration tests the full Manager.Execute flow with MCP transport.
func TestMCPManagerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockBin := buildMockMCPServer(t)

	td := &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "mock-mcp", Binary: mockBin},
		Transport: schema.ToolTransport{
			Mode: "mcp",
			Startup: &schema.ToolStartup{
				Timeout: "5s",
			},
		},
		Actions: map[string]schema.ToolAction{
			"echo": {
				MCPTool: "echo",
				Args: map[string]schema.ToolArg{
					"message": {Type: "string", Required: true},
				},
				Capture: map[string]schema.ToolCapture{
					"output": {Format: "text"},
				},
			},
			"query": {
				MCPTool: "query",
				Capture: map[string]schema.ToolCapture{
					"result": {From: "result", Format: "text"},
				},
			},
		},
	}

	mgr := NewManager(nil, nil)
	mgr.defs["mock-mcp"] = td

	ctx := context.Background()

	t.Run("execute echo via manager", func(t *testing.T) {
		result, err := mgr.Execute(ctx, "mock-mcp", "echo",
			map[string]string{"message": "manager-mcp-test"},
			nil,
		)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Captures["output"] != "manager-mcp-test" {
			t.Errorf("capture output = %q, want %q", result.Captures["output"], "manager-mcp-test")
		}
	})

	t.Run("execute query via manager", func(t *testing.T) {
		result, err := mgr.Execute(ctx, "mock-mcp", "query", nil, nil)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !contains(result.Captures["result"], "mcp-query-result") {
			t.Errorf("capture result = %q, expected 'mcp-query-result'", result.Captures["result"])
		}
	})

	t.Run("process reused across calls", func(t *testing.T) {
		_, _ = mgr.Execute(ctx, "mock-mcp", "echo", map[string]string{"message": "a"}, nil)
		_, _ = mgr.Execute(ctx, "mock-mcp", "echo", map[string]string{"message": "b"}, nil)

		mgr.mu.Lock()
		procCount := len(mgr.mcpProcesses)
		mgr.mu.Unlock()
		if procCount != 1 {
			t.Errorf("expected 1 MCP process, got %d", procCount)
		}
	})

	mgr.Shutdown(ctx)
}

func buildMockMCPServer(t *testing.T) string {
	t.Helper()
	mockSrc := filepath.Join("..", "..", "testdata", "tools", "mock-mcp-server.go")
	if _, err := os.Stat(mockSrc); err != nil {
		t.Fatalf("mock MCP server source not found: %v", err)
	}

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	mockBin := filepath.Join(t.TempDir(), "mock-mcp-server"+ext)

	buildCmd := exec.Command("go", "build", "-o", mockBin, mockSrc)
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build mock MCP server: %v", err)
	}
	return mockBin
}
