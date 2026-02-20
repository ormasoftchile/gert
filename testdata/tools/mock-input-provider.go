// mock-input-provider is a test helper binary that implements a minimal
// input provider JSON-RPC server for integration testing.
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
	ID      *int64          `json:"id,omitempty"`
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
	fmt.Fprintln(os.Stderr, "mock-input-provider: ready")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		if req.ID == nil {
			if req.Method == "shutdown" {
				os.Exit(0)
			}
			continue
		}

		var resp response
		resp.JSONRPC = "2.0"
		resp.ID = *req.ID

		switch req.Method {
		case "resolve":
			var params struct {
				Bindings map[string]struct {
					From    string `json:"From"`
					Pattern string `json:"Pattern"`
				} `json:"bindings"`
				Context map[string]string `json:"context"`
			}
			json.Unmarshal(req.Params, &params)

			resolved := make(map[string]string)
			for name, binding := range params.Bindings {
				switch binding.From {
				case "mock.hostname":
					resolved[name] = "test-server-01.example.com"
				case "mock.severity":
					resolved[name] = "2"
				case "mock.region":
					resolved[name] = "West US 2"
				}
			}

			resp.Result = map[string]interface{}{
				"resolved": resolved,
				"warnings": []string{},
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
