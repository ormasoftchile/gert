// Package main provides the gert-mcp binary — MCP server for AI agents.
package main

import (
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
	gmcp "github.com/ormasoftchile/gert/pkg/ecosystem/mcp"
)

var version = "dev"

func main() {
	s := gmcp.NewServer(version)
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
