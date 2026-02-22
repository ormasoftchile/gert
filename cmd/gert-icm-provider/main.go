// gert-icm-provider is a standalone JSON-RPC input provider for Microsoft ICM.
// It resolves from: icm.* input bindings by fetching incidents from the ICM API.
//
// Protocol: newline-delimited JSON-RPC 2.0 over stdio.
// Emits "ready" on stderr once initialized.
//
// Usage:
//
//	gert-icm-provider
//
// The provider expects ICM authentication via:
//   - ICM_TOKEN environment variable, or
//   - Azure CLI: az account get-access-token --resource https://icm.ad.msft.net
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ormasoftchile/gert/pkg/icm"
	"github.com/ormasoftchile/gert/pkg/inputs"
	"github.com/ormasoftchile/gert/pkg/schema"
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
	// Emit ready signal
	fmt.Fprintln(os.Stderr, "gert-icm-provider: ready")

	provider := &icm.ICMInputProvider{}

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

		// Skip notifications (no ID)
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
				Bindings map[string]inputs.InputBinding `json:"bindings"`
				Context  map[string]string              `json:"context"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				resp.Error = map[string]interface{}{
					"code":    -32602,
					"message": fmt.Sprintf("invalid params: %v", err),
				}
				break
			}

			resolveReq := &inputs.ResolveRequest{
				Bindings: params.Bindings,
				Context:  params.Context,
			}

			result, err := provider.Resolve(nil, resolveReq)
			if err != nil {
				resp.Error = map[string]interface{}{
					"code":    -32000,
					"message": err.Error(),
				}
			} else {
				resp.Result = result
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

// Ensure ICMInputProvider satisfies the interface at compile time.
var _ inputs.InputProvider = (*icm.ICMInputProvider)(nil)

// Suppress unused import — schema is used via icm.ICMInputProvider
var _ = schema.InputDef{}
