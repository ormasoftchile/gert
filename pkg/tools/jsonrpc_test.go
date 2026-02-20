package tools

import (
	"encoding/json"
	"testing"
)

func TestExtractJSONPath(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		path     string
		expected string
		wantErr  bool
	}{
		{
			name:     "empty path returns full value",
			raw:      `{"data": "hello"}`,
			path:     "",
			expected: `{"data": "hello"}`,
		},
		{
			name:     "simple key",
			raw:      `{"data": "hello"}`,
			path:     "data",
			expected: "hello",
		},
		{
			name:     "nested key",
			raw:      `{"result": {"data": "world"}}`,
			path:     "result.data",
			expected: "world",
		},
		{
			name:     "nested object returns JSON",
			raw:      `{"result": {"items": [1,2,3]}}`,
			path:     "result.items",
			expected: "[1,2,3]",
		},
		{
			name:     "numeric value",
			raw:      `{"count": 42}`,
			path:     "count",
			expected: "42",
		},
		{
			name:     "boolean value",
			raw:      `{"ok": true}`,
			path:     "ok",
			expected: "true",
		},
		{
			name:    "missing key",
			raw:     `{"data": "hello"}`,
			path:    "missing",
			wantErr: true,
		},
		{
			name:    "index into non-object",
			raw:     `{"data": "hello"}`,
			path:    "data.sub",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractJSONPath(json.RawMessage(tt.raw), tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got result %q", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"10s", "10s"},
		{"2m", "2m0s"},
		{"1h", "1h0m0s"},
		{"500ms", "500ms"},
	}
	for _, tt := range tests {
		d, err := parseDuration(tt.input)
		if err != nil {
			t.Errorf("parseDuration(%q): %v", tt.input, err)
			continue
		}
		if d.String() != tt.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tt.input, d, tt.want)
		}
	}
}

func TestJsonrpcRequestMarshal(t *testing.T) {
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test/query",
		Params:  map[string]string{"key": "value"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	expected := `{"jsonrpc":"2.0","id":1,"method":"test/query","params":{"key":"value"}}`
	if string(data) != expected {
		t.Errorf("got %s, want %s", data, expected)
	}
}

func TestJsonrpcResponseUnmarshal(t *testing.T) {
	t.Run("success response", func(t *testing.T) {
		raw := `{"jsonrpc":"2.0","id":1,"result":{"data":"hello"}}`
		var resp jsonrpcResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.ID != 1 {
			t.Errorf("id = %d, want 1", resp.ID)
		}
		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}
		if string(resp.Result) != `{"data":"hello"}` {
			t.Errorf("result = %s, want {\"data\":\"hello\"}", resp.Result)
		}
	})

	t.Run("error response", func(t *testing.T) {
		raw := `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"invalid request"}}`
		var resp jsonrpcResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Error == nil {
			t.Fatal("expected error, got nil")
		}
		if resp.Error.Code != -32600 {
			t.Errorf("error code = %d, want -32600", resp.Error.Code)
		}
		if resp.Error.Message != "invalid request" {
			t.Errorf("error message = %q, want %q", resp.Error.Message, "invalid request")
		}
	})
}
