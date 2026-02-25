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

// TestJSONRPCIntegration builds a mock JSON-RPC server and tests the full flow:
// spawn, ready signal, call, capture, shutdown.
func TestJSONRPCIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build the mock server
	mockSrc := filepath.Join("..", "..", "testdata", "tools", "mock-jsonrpc-server.go")
	if _, err := os.Stat(mockSrc); err != nil {
		t.Fatalf("mock server source not found: %v", err)
	}

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	mockBin := filepath.Join(t.TempDir(), "mock-server"+ext)

	buildCmd := exec.Command("go", "build", "-o", mockBin, mockSrc)
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build mock server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("spawn and call", func(t *testing.T) {
		proc, err := spawnJSONRPC(ctx, mockBin, nil, &schema.ToolStartup{
			ReadySignal: "listening",
			Timeout:     "5s",
		})
		if err != nil {
			t.Fatalf("spawnJSONRPC: %v", err)
		}
		defer proc.Shutdown("shutdown", 2*time.Second)

		if !proc.alive() {
			t.Fatal("process should be alive after spawn")
		}

		// Test echo method
		result, err := proc.Call("test/echo", map[string]interface{}{
			"message": "hello",
		})
		if err != nil {
			t.Fatalf("Call test/echo: %v", err)
		}

		extracted, err := ExtractJSONPath(result, "message")
		if err != nil {
			t.Fatalf("extract message: %v", err)
		}
		if extracted != "hello" {
			t.Errorf("got %q, want %q", extracted, "hello")
		}

		// Test query method
		result, err = proc.Call("test/query", nil)
		if err != nil {
			t.Fatalf("Call test/query: %v", err)
		}

		data, err := ExtractJSONPath(result, "data")
		if err != nil {
			t.Fatalf("extract data: %v", err)
		}
		if data != "query-result-data" {
			t.Errorf("data = %q, want %q", data, "query-result-data")
		}

		count, err := ExtractJSONPath(result, "count")
		if err != nil {
			t.Fatalf("extract count: %v", err)
		}
		if count != "42" {
			t.Errorf("count = %q, want %q", count, "42")
		}
	})

	t.Run("error response", func(t *testing.T) {
		proc, err := spawnJSONRPC(ctx, mockBin, nil, &schema.ToolStartup{
			ReadySignal: "listening",
			Timeout:     "5s",
		})
		if err != nil {
			t.Fatalf("spawnJSONRPC: %v", err)
		}
		defer proc.Shutdown("shutdown", 2*time.Second)

		_, err = proc.Call("test/error", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !contains(err.Error(), "test error from mock server") {
			t.Errorf("error = %q, expected to contain 'test error from mock server'", err.Error())
		}
	})

	t.Run("shutdown", func(t *testing.T) {
		proc, err := spawnJSONRPC(ctx, mockBin, nil, &schema.ToolStartup{
			ReadySignal:    "listening",
			Timeout:        "5s",
			ShutdownMethod: "shutdown",
		})
		if err != nil {
			t.Fatalf("spawnJSONRPC: %v", err)
		}

		if !proc.alive() {
			t.Fatal("process should be alive")
		}

		err = proc.Shutdown("shutdown", 2*time.Second)
		if err != nil {
			t.Errorf("Shutdown: %v", err)
		}

		// Process should be dead now
		time.Sleep(100 * time.Millisecond)
		if proc.alive() {
			t.Error("process should be dead after shutdown")
		}
	})

	t.Run("multiple calls reuse process", func(t *testing.T) {
		proc, err := spawnJSONRPC(ctx, mockBin, nil, &schema.ToolStartup{
			ReadySignal: "listening",
			Timeout:     "5s",
		})
		if err != nil {
			t.Fatalf("spawnJSONRPC: %v", err)
		}
		defer proc.Shutdown("shutdown", 2*time.Second)

		for i := 0; i < 5; i++ {
			result, err := proc.Call("test/echo", map[string]interface{}{
				"iteration": i,
			})
			if err != nil {
				t.Fatalf("Call iteration %d: %v", i, err)
			}
			if result == nil {
				t.Fatalf("nil result at iteration %d", i)
			}
		}
	})
}

// TestJSONRPCManagerIntegration tests the full Manager.Execute flow with jsonrpc transport.
func TestJSONRPCManagerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build the mock server
	mockSrc := filepath.Join("..", "..", "testdata", "tools", "mock-jsonrpc-server.go")
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	mockBin := filepath.Join(t.TempDir(), "mock-server"+ext)

	buildCmd := exec.Command("go", "build", "-o", mockBin, mockSrc)
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build mock server: %v", err)
	}

	// Create a tool definition pointing to the mock server
	td := &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "mock", Binary: mockBin},
		Transport: schema.ToolTransport{
			Mode: "jsonrpc",
			Startup: &schema.ToolStartup{
				ReadySignal:    "listening",
				Timeout:        "5s",
				ShutdownMethod: "shutdown",
			},
		},
		Actions: map[string]schema.ToolAction{
			"echo": {
				Method: "test/echo",
				Args: map[string]schema.ToolArg{
					"message": {Type: "string", Required: true},
				},
				Capture: map[string]schema.ToolCapture{
					"result": {From: "message", Format: "text"},
				},
			},
			"query": {
				Method: "test/query",
				Capture: map[string]schema.ToolCapture{
					"data": {From: "data", Format: "text"},
				},
			},
		},
	}

	mgr := NewManager(nil, nil) // executor not needed for jsonrpc
	mgr.defs["mock"] = td

	ctx := context.Background()

	t.Run("execute echo", func(t *testing.T) {
		result, err := mgr.Execute(ctx, "mock", "echo",
			map[string]string{"message": "hello-from-manager"},
			nil,
		)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Captures["result"] != "hello-from-manager" {
			t.Errorf("capture result = %q, want %q", result.Captures["result"], "hello-from-manager")
		}
	})

	t.Run("execute query with dot-path capture", func(t *testing.T) {
		result, err := mgr.Execute(ctx, "mock", "query", nil, nil)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Captures["data"] != "query-result-data" {
			t.Errorf("capture data = %q, want %q", result.Captures["data"], "query-result-data")
		}
	})

	t.Run("process reused across calls", func(t *testing.T) {
		// First call spawns the process
		_, err := mgr.Execute(ctx, "mock", "echo",
			map[string]string{"message": "first"},
			nil,
		)
		if err != nil {
			t.Fatalf("first call: %v", err)
		}

		// Second call should reuse the same process
		_, err = mgr.Execute(ctx, "mock", "echo",
			map[string]string{"message": "second"},
			nil,
		)
		if err != nil {
			t.Fatalf("second call: %v", err)
		}

		// Only one process should exist
		mgr.mu.Lock()
		procCount := len(mgr.processes)
		mgr.mu.Unlock()
		if procCount != 1 {
			t.Errorf("expected 1 process, got %d", procCount)
		}
	})

	// Cleanup
	mgr.Shutdown(ctx)
}

// TestJSONRPCCrashRecovery tests that a crashed process is automatically respawned.
func TestJSONRPCCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockSrc := filepath.Join("..", "..", "testdata", "tools", "mock-jsonrpc-server.go")
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	mockBin := filepath.Join(t.TempDir(), "mock-server"+ext)
	buildCmd := exec.Command("go", "build", "-o", mockBin, mockSrc)
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}

	td := &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "crash-test", Binary: mockBin},
		Transport: schema.ToolTransport{
			Mode: "jsonrpc",
			Startup: &schema.ToolStartup{
				ReadySignal:    "listening",
				Timeout:        "5s",
				ShutdownMethod: "shutdown",
			},
		},
		Actions: map[string]schema.ToolAction{
			"echo": {
				Method: "test/echo",
				Args: map[string]schema.ToolArg{
					"message": {Type: "string", Required: true},
				},
				Capture: map[string]schema.ToolCapture{
					"result": {From: "message", Format: "text"},
				},
			},
		},
	}

	mgr := NewManager(nil, nil)
	mgr.defs["crash-test"] = td

	ctx := context.Background()

	// First call spawns process
	result, err := mgr.Execute(ctx, "crash-test", "echo",
		map[string]string{"message": "before-crash"}, nil)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if result.Captures["result"] != "before-crash" {
		t.Errorf("first result = %q, want %q", result.Captures["result"], "before-crash")
	}

	// Kill the process to simulate crash
	mgr.mu.Lock()
	proc := mgr.processes["crash-test"]
	mgr.mu.Unlock()
	if proc == nil {
		t.Fatal("expected process to exist")
	}
	proc.kill()
	time.Sleep(100 * time.Millisecond)

	// Next call should detect dead process, respawn, and succeed
	result, err = mgr.Execute(ctx, "crash-test", "echo",
		map[string]string{"message": "after-crash"}, nil)
	if err != nil {
		t.Fatalf("post-crash call: %v", err)
	}
	if result.Captures["result"] != "after-crash" {
		t.Errorf("post-crash result = %q, want %q", result.Captures["result"], "after-crash")
	}

	mgr.Shutdown(ctx)
}

// TestJSONRPCConcurrent tests concurrent calls to the same jsonrpc process.
func TestJSONRPCConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockSrc := filepath.Join("..", "..", "testdata", "tools", "mock-jsonrpc-server.go")
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	mockBin := filepath.Join(t.TempDir(), "mock-server"+ext)
	buildCmd := exec.Command("go", "build", "-o", mockBin, mockSrc)
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	proc, err := spawnJSONRPC(ctx, mockBin, nil, &schema.ToolStartup{
		ReadySignal: "listening",
		Timeout:     "5s",
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer proc.Shutdown("shutdown", 2*time.Second)

	// Launch 10 concurrent calls — the mutex should serialize them safely
	const n = 10
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			_, err := proc.Call("test/echo", map[string]interface{}{
				"idx": idx,
			})
			errs <- err
		}(i)
	}

	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent call %d: %v", i, err)
		}
	}
}
