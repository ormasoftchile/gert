package providers

import (
	"testing"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// TestEvaluateXTSCapture tests the capture expression evaluator.
func TestEvaluateXTSCapture(t *testing.T) {
	xtsOut := &XTSOutput{
		Success:  true,
		RowCount: 3,
		Columns:  []string{"name", "region", "status"},
		Data: []map[string]interface{}{
			{"name": "server-01", "region": "eastus", "status": "healthy"},
			{"name": "server-02", "region": "westus", "status": "degraded"},
			{"name": "server-03", "region": "eastus", "status": "healthy"},
		},
	}
	rawStdout := `{"success":true,"rowCount":3,"columns":["name","region","status"],"data":[...]}`

	tests := []struct {
		name    string
		expr    string
		want    string
		wantErr bool
	}{
		{
			name: "stdout passthrough",
			expr: "stdout",
			want: rawStdout,
		},
		{
			name: "row_count",
			expr: "row_count",
			want: "3",
		},
		{
			name: "columns",
			expr: "$.columns",
			want: "name,region,status",
		},
		{
			name: "data specific cell",
			expr: "$.data[0].name",
			want: "server-01",
		},
		{
			name: "data second row",
			expr: "$.data[1].status",
			want: "degraded",
		},
		{
			name: "data wildcard all rows",
			expr: "$.data[*].region",
			want: "eastus\nwestus\neastus",
		},
		{
			name:    "data index out of range",
			expr:    "$.data[5].name",
			wantErr: true,
		},
		{
			name:    "data missing field",
			expr:    "$.data[0].nonexistent",
			wantErr: true,
		},
		{
			name:    "unsupported expression",
			expr:    "$.metadata.foo",
			wantErr: true,
		},
		{
			name:    "malformed data path",
			expr:    "$.data",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluateXTSCapture(tt.expr, xtsOut, rawStdout)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for expr %q, got %q", tt.expr, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for expr %q: %v", tt.expr, err)
			}
			if got != tt.want {
				t.Errorf("expr %q = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

// TestEvaluateDataPathEmptyData tests edge cases with empty data.
func TestEvaluateDataPathEmptyData(t *testing.T) {
	xtsOut := &XTSOutput{
		Success:  true,
		RowCount: 0,
		Columns:  []string{"a"},
		Data:     []map[string]interface{}{},
	}

	// Index into empty data
	_, err := evaluateDataPath("$.data[0].a", xtsOut)
	if err == nil {
		t.Error("expected error for index into empty data")
	}

	// Wildcard on empty data → empty string
	val, err := evaluateDataPath("$.data[*].a", xtsOut)
	if err != nil {
		t.Fatalf("unexpected error for wildcard on empty data: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for wildcard on empty data, got %q", val)
	}
}

// TestXTSProviderValidate tests the provider-level Validate method.
func TestXTSProviderValidate(t *testing.T) {
	// Test with no xts config
	p := &XTSProvider{CLIPath: "xts-cli", ViewsRoot: ""}
	result := p.Validate(schema.Step{Type: "xts"})
	if result.Valid {
		t.Error("expected invalid for step with nil xts config")
	}
}

// TestResolveViewPath tests view path resolution.
func TestResolveViewPath(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		viewsRoot string
		want      string
	}{
		{
			name:      "absolute path unchanged",
			file:      "C:\\views\\test.xts",
			viewsRoot: "C:\\root",
			want:      "C:\\views\\test.xts",
		},
		{
			name:      "relative with views_root",
			file:      "sterling/test.xts",
			viewsRoot: "C:\\root",
			want:      "C:\\root\\sterling\\test.xts",
		},
		{
			name:      "relative without views_root",
			file:      "sterling/test.xts",
			viewsRoot: "",
			want:      "sterling/test.xts",
		},
		{
			name:      "empty file",
			file:      "",
			viewsRoot: "C:\\root",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &XTSProvider{ViewsRoot: tt.viewsRoot}
			got := p.resolveViewPath(tt.file)
			if got != tt.want {
				t.Errorf("resolveViewPath(%q) = %q, want %q", tt.file, got, tt.want)
			}
		})
	}
}
