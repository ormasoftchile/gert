package compiler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
	"gopkg.in/yaml.v3"
)

// CompileResult holds the outputs of a compilation.
type CompileResult struct {
	Runbook     *schema.Runbook
	Mapping     string // mapping.md content
	Warnings    []string
	StepCount   int
	CLICount    int
	ManualCount int
	TODOCount   int
}

// CompileTSG compiles a Markdown TSG file into a runbook and mapping report
// using the LLM-backed 3-stage pipeline:
//
//	Stage A: Deterministic IR extraction (ParseTSG)
//	Stage B: LLM interpretation → runbook YAML + mapping (via LLMClient)
//	Stage C: Deterministic validation (schema.ValidateFile)
func CompileTSG(tsgPath string, client LLMClient) (*CompileResult, error) {
	source, err := os.ReadFile(tsgPath)
	if err != nil {
		return nil, fmt.Errorf("read TSG: %w", err)
	}
	result, err := CompileTSGFromSource(source, filepath.Base(tsgPath), client)
	if err != nil {
		return nil, err
	}

	// Inject source provenance metadata
	absPath, _ := filepath.Abs(tsgPath)
	if absPath == "" {
		absPath = tsgPath
	}
	sourceHash := fmt.Sprintf("%x", sha256.Sum256(source))
	result.Runbook.Meta.Source = &schema.SourceMeta{
		File:       absPath,
		CompiledAt: time.Now().UTC().Format(time.RFC3339),
		Model:      client.ModelName(),
		SourceHash: sourceHash,
	}

	return result, nil
}

// CompileTSGFromSource compiles TSG markdown source into a runbook via the LLM.
func CompileTSGFromSource(source []byte, sourceName string, client LLMClient) (*CompileResult, error) {
	// ── Stage A: deterministic IR extraction ──
	ir, err := ParseTSG(source)
	if err != nil {
		return nil, fmt.Errorf("stage A (parse TSG): %w", err)
	}

	// Load the JSON Schema for the system prompt
	jsonSchema, err := loadEmbeddedSchema()
	if err != nil {
		return nil, fmt.Errorf("load schema: %w", err)
	}

	// Count code blocks, decision points, and sub-TSG links from IR to set minimum step target
	minSteps := 0
	linkRe := regexp.MustCompile(`\]\([^)]*\.md\)`)
	for _, sec := range ir.Sections {
		minSteps += len(sec.CodeBlocks)
		// Count sections with decision-like headings
		lower := strings.ToLower(sec.Heading)
		if strings.Contains(lower, "step") || strings.Contains(lower, "check") || strings.Contains(lower, "verify") || strings.Contains(lower, "triage") {
			if len(sec.CodeBlocks) == 0 {
				minSteps++ // manual step
			}
		}
		// Count list items that link to sub-TSGs (.md files) — each is a potential branch step
		for _, item := range sec.ListItems {
			links := linkRe.FindAllString(item, -1)
			if len(links) > 0 {
				minSteps += len(links)
			}
		}
		// Count paragraphs with links to sub-TSGs as potential manual/branch steps
		for _, para := range sec.Paragraphs {
			links := linkRe.FindAllString(para, -1)
			if len(links) > 0 {
				minSteps += len(links)
			}
		}
	}
	// Also scan the raw source for .md links in case the IR missed some
	// (goldmark may strip link URLs from extracted text)
	rawLinks := linkRe.FindAllString(string(source), -1)
	if len(rawLinks) > minSteps {
		minSteps = len(rawLinks)
	}
	if minSteps < 2 {
		minSteps = 2 // absolute minimum
	}

	// ── Stage B: LLM interpretation ──
	promptData := PromptData{
		JSONSchema:    jsonSchema,
		TSGContent:    string(source),
		SourceName:    sourceName,
		ExtractedVars: ir.Vars,
		MinStepCount:  minSteps,
	}

	systemPrompt, err := RenderSystemPrompt(promptData)
	if err != nil {
		return nil, fmt.Errorf("render system prompt: %w", err)
	}
	userPrompt, err := RenderUserPrompt(promptData)
	if err != nil {
		return nil, fmt.Errorf("render user prompt: %w", err)
	}

	ctx := context.Background()
	llmResponse, err := client.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("stage B (LLM): %w", err)
	}

	// Parse the structured LLM response
	runbookYAML, mappingMD, err := ParseLLMResponse(llmResponse)
	if err != nil {
		// Dump the tail of the raw response for debugging
		tail := llmResponse
		if len(tail) > 500 {
			tail = "..." + tail[len(tail)-500:]
		}
		return nil, fmt.Errorf("stage B (parse response): %w\n\nRaw response tail:\n%s", err, tail)
	}

	// Deserialize the runbook YAML
	var rb schema.Runbook
	if err := yaml.Unmarshal([]byte(runbookYAML), &rb); err != nil {
		return nil, fmt.Errorf("stage B (unmarshal runbook): %w\n\nRaw LLM YAML:\n%s", err, runbookYAML)
	}

	// Build result stats
	totalSteps := len(rb.Steps)
	// Also count tree steps if present
	totalSteps += countTreeSteps(rb.Tree)
	if totalSteps == 0 {
		return nil, fmt.Errorf("stage B: LLM produced 0 steps. The model may have failed to parse the TSG.\n\nRaw LLM YAML (first 2000 chars):\n%s", truncateString(runbookYAML, 2000))
	}
	if totalSteps < promptData.MinStepCount {
		fmt.Fprintf(os.Stderr, "  ⚠ Warning: LLM produced %d steps but TSG has %d code blocks/sections. Output may be incomplete.\n", totalSteps, promptData.MinStepCount)
	}

	result := &CompileResult{
		Runbook:   &rb,
		Mapping:   mappingMD,
		StepCount: totalSteps,
	}
	for _, step := range rb.Steps {
		switch step.Type {
		case "cli":
			result.CLICount++
		case "manual":
			result.ManualCount++
			if step.Instructions != "" && strings.Contains(step.Instructions, "TODO") {
				result.TODOCount++
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("step %q: manual with TODO annotation", step.ID))
			}
		}
	}

	return result, nil
}

// countTreeSteps recursively counts steps in a tree structure.
func countTreeSteps(tree []schema.TreeNode) int {
	count := 0
	for _, node := range tree {
		if node.Iterate != nil {
			count += countTreeSteps(node.Iterate.Steps)
			continue
		}
		if node.Step.ID != "" {
			count++
		}
		for _, branch := range node.Branches {
			count += countTreeSteps(branch.Steps)
		}
	}
	return count
}

func truncateString(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// loadEmbeddedSchema reads the JSON Schema from the schemas/ directory.
// It searches relative to the working directory and common project paths.
func loadEmbeddedSchema() (string, error) {
	candidates := []string{
		"schemas/runbook-v0.json",
		"../../schemas/runbook-v0.json", // from pkg/compiler/
	}

	// Also try from the executable's directory
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "schemas", "runbook-v0.json"),
			filepath.Join(filepath.Dir(exe), "..", "schemas", "runbook-v0.json"),
		)
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), nil
		}
	}

	// Fallback: generate from Go structs at runtime
	data, err := schema.GenerateJSONSchema()
	if err != nil {
		return "", fmt.Errorf("could not load or generate JSON schema: %w", err)
	}
	return string(data), nil
}

// WriteRunbook writes the runbook to a YAML file.
// If an existing runbook exists, it creates a .bak backup and refuses to
// overwrite if the new runbook has fewer steps (regression guard).
func WriteRunbook(rb *schema.Runbook, path string, force bool) error {
	newSteps := len(rb.Steps) + countTreeSteps(rb.Tree)

	// Check existing file for regression
	if existing, err := os.ReadFile(path); err == nil {
		var old schema.Runbook
		if err := yaml.Unmarshal(existing, &old); err == nil {
			oldSteps := len(old.Steps) + countTreeSteps(old.Tree)
			if oldSteps > 0 && newSteps < oldSteps && !force {
				return fmt.Errorf("regression guard: new runbook has %d steps but existing has %d. Use --force to overwrite. Backup saved to %s.bak", newSteps, oldSteps, path)
			}
		}
		// Always backup before overwrite
		_ = os.WriteFile(path+".bak", existing, 0644)
	}

	data, err := yaml.Marshal(rb)
	if err != nil {
		return fmt.Errorf("marshal runbook: %w", err)
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}
	return os.WriteFile(path, data, 0644)
}

// WriteMapping writes the mapping report to a file.
func WriteMapping(content string, path string) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// toKebabCase converts a heading to kebab-case for meta.name.
func toKebabCase(s string) string {
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9\s]+`)
	s = re.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	fields := strings.Fields(s)
	return strings.Join(fields, "-")
}

// toSnakeCase converts a heading to snake_case for step IDs.
func toSnakeCase(s string) string {
	s = regexp.MustCompile(`^\d+\.\s*`).ReplaceAllString(s, "")
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9\s]+`)
	s = re.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	fields := strings.Fields(s)
	return strings.Join(fields, "_")
}
