// mock-mcp-server is a test helper binary that implements a minimal MCP server
// over stdio for integration testing. Supports initialize, tools/list, and tools/call.
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
	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer for large messages
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

		// Skip notifications (no ID)
		if req.ID == nil {
			continue
		}

		var resp response
		resp.JSONRPC = "2.0"
		resp.ID = *req.ID

		switch req.Method {
		case "initialize":
			resp.Result = map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "mock-mcp-server",
					"version": "1.0.0",
				},
			}

		case "tools/list":
			resp.Result = map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "echo",
						"description": "Echo back the input",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"message": map[string]interface{}{"type": "string"},
							},
						},
					},
					{
						"name":        "query",
						"description": "Return test data",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
					{
						"name":        "failing",
						"description": "Always returns an error",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			}

		case "tools/call":
			var params struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			json.Unmarshal(req.Params, &params)

			switch params.Name {
			case "echo":
				msg := ""
				if m, ok := params.Arguments["message"]; ok {
					msg = fmt.Sprintf("%v", m)
				}
				resp.Result = map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": msg},
					},
				}

			case "query":
				resp.Result = map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": `{"data":"mcp-query-result","count":99}`},
					},
				}

			case "failing":
				resp.Result = map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": "something went wrong"},
					},
					"isError": true,
				}

			default:
				resp.Error = map[string]interface{}{
					"code":    -32601,
					"message": fmt.Sprintf("unknown tool %q", params.Name),
				}
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
