//go:build ignore

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ormasoftchile/gert/pkg/schema"
)

func main() {
	data, err := schema.GenerateJSONSchema()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile("schemas/runbook-v0.json", data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("wrote schemas/runbook-v0.json")

	toolData, err := schema.GenerateToolJSONSchema()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating tool schema: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile("schemas/tool-v0.json", toolData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("wrote schemas/tool-v0.json")

	// Generate runbook-v1 schema (same as v0 — XTS rejection is at validation, not schema level)
	// v1 shares the same Go structs; the version difference is enforced in ValidateDomain
	v1Data := make([]byte, len(data))
	copy(v1Data, data)
	// Simple text replacement to update the schema ID and title
	v1Str := strings.Replace(string(v1Data), "runbook-v0.json", "runbook-v1.json", 1)
	v1Str = strings.Replace(v1Str, "Governed Executable Runbook v0", "Governed Executable Runbook v1", 1)
	if err := os.WriteFile("schemas/runbook-v1.json", []byte(v1Str), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("wrote schemas/runbook-v1.json")
}
