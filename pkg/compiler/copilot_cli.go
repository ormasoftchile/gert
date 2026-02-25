package compiler

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CopilotCLIClient implements LLMClient by shelling out to the GitHub
// Copilot CLI (`copilot` binary). It writes the combined system + user
// prompt to a temp file and passes it via `-p` to work around Windows
// command-line length limits.
type CopilotCLIClient struct {
	// Binary is the path to the copilot executable (default: "copilot").
	Binary string
	// Timeout for the copilot process (default: 5 minutes).
	Timeout time.Duration
}

// NewCopilotCLIClient creates a CopilotCLIClient with sensible defaults.
func NewCopilotCLIClient() *CopilotCLIClient {
	return &CopilotCLIClient{
		Binary:  "copilot",
		Timeout: 5 * time.Minute,
	}
}

// ModelName returns a label for provenance tracking.
func (c *CopilotCLIClient) ModelName() string {
	return "copilot-cli"
}

// Complete sends the system + user prompts to the copilot CLI and returns
// the captured stdout. The prompts are written to a temp file and passed
// via the -p flag to avoid Windows command-line length limits.
func (c *CopilotCLIClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	binary := c.Binary
	if binary == "" {
		binary = "copilot"
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	// Combine system + user prompts
	combinedPrompt := fmt.Sprintf(
		"SYSTEM INSTRUCTIONS (follow these exactly):\n\n%s\n\n---\n\nUSER REQUEST:\n\n%s",
		systemPrompt, userPrompt,
	)

	// Write the full prompt to a temp file under .temp/ in the working
	// directory so copilot CLI can access it via its workspace file-reading
	// tools. The combined prompt easily exceeds the Windows 32K command-line limit.
	tempDir := filepath.Join(".", ".temp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("create .temp dir: %w", err)
	}
	promptFile := filepath.Join(tempDir, "copilot-prompt.txt")
	if err := os.WriteFile(promptFile, []byte(combinedPrompt), 0600); err != nil {
		return "", fmt.Errorf("write prompt file: %w", err)
	}
	defer os.Remove(promptFile)

	// The -p argument just tells copilot to read and follow the prompt file.
	// This keeps the actual command line short.
	shortPrompt := fmt.Sprintf(
		"Read the file at %s and follow ALL instructions inside it exactly. "+
			"The file contains SYSTEM INSTRUCTIONS and a USER REQUEST separated by ---. "+
			"You MUST produce output in the exact format described in the SYSTEM INSTRUCTIONS. "+
			"Do not explain, just output the result as specified.",
		filepath.ToSlash(promptFile),
	)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary,
		"-p", shortPrompt,
		"--yolo",
		"--no-ask-user",
		"-s",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Include stderr for diagnostics
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = "(no stderr)"
		}
		return "", fmt.Errorf("copilot CLI failed: %w\nstderr: %s", err, detail)
	}

	output := stdout.String()
	if strings.TrimSpace(output) == "" {
		return "", fmt.Errorf("copilot CLI returned empty output\nstderr: %s", strings.TrimSpace(stderr.String()))
	}

	return output, nil
}
