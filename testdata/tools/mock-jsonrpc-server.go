// mock-jsonrpc-server is a test helper binary that implements a simple
// JSON-RPC 2.0 server over stdio for integration testing.
//
//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

func main() {
	// Emit ready signal on stderr
	fmt.Fprintln(os.Stderr, "mock-jsonrpc-server: listening")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		// Handle shutdown notification (no ID)
		if req.Method == "shutdown" {
			os.Exit(0)
		}

		var resp response
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "test/echo":
			// Echo back the params
			var params map[string]interface{}
			json.Unmarshal(req.Params, &params)
			resp.Result = params

		case "test/query":
			// Return a structured result
			resp.Result = map[string]interface{}{
				"data":  "query-result-data",
				"count": 42,
			}

		case "test/error":
			resp.Error = map[string]interface{}{
				"code":    -32000,
				"message": "test error from mock server",
			}

		default:
			resp.Error = map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("method %q not found", req.Method),
			}
		}

		data, _ := json.Marshal(resp)
		fmt.Fprintln(os.Stdout, string(data))
	}
}
