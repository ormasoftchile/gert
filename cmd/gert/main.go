package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/compiler"
	"github.com/ormasoftchile/gert/pkg/debugger"
	"github.com/ormasoftchile/gert/pkg/icm"
	"github.com/ormasoftchile/gert/pkg/inputs"
	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/replay"
	"github.com/ormasoftchile/gert/pkg/runtime"
	"github.com/ormasoftchile/gert/pkg/schema"
	"github.com/ormasoftchile/gert/pkg/serve"
	"github.com/ormasoftchile/gert/pkg/tools"
	runtest "github.com/ormasoftchile/gert/pkg/testing"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Version is set at build time via ldflags.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	loadDotEnv() // load .env file if present (gitignored)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// loadDotEnv reads a .env file from the working directory and sets
// any variables that aren't already set in the environment.
// Lines are KEY=VALUE (or KEY="VALUE"). Comments (#) and blanks are skipped.
// The .env file is gitignored so secrets never end up in source control.
func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return // no .env file — that's fine
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Remove surrounding quotes
		val = strings.Trim(val, `"'`)
		// Don't overwrite existing env vars
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

var rootCmd = &cobra.Command{
	Use:   "gert",
	Short: "Governed Executable Runbook Engine",
	Long:  "gert — a platform for governed, executable, debuggable runbooks with traceability and evidence capture.",
}

// --- validate ---

var validateCmd = &cobra.Command{
	Use:   "validate [runbook.yaml]",
	Short: "Validate a runbook YAML file against the schema",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

func runValidate(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".md" || ext == ".markdown" {
		return fmt.Errorf("%s is a Markdown file, not a runbook YAML.\nDid you mean: gert compile %s --out runbook.yaml", filePath, filePath)
	}

	rb, errs := schema.ValidateFile(filePath)
	if len(errs) > 0 {
		// Separate warnings from errors
		var errors []*schema.ValidationError
		var warnings []*schema.ValidationError
		for _, e := range errs {
			if e.Severity == "warning" {
				warnings = append(warnings, e)
			} else {
				errors = append(errors, e)
			}
		}
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "  ⚠ [%s] %s\n", w.Phase, w.Message)
			if w.Path != "" {
				fmt.Fprintf(os.Stderr, "    at: %s\n", w.Path)
			}
		}
		if len(errors) > 0 {
			fmt.Fprintf(os.Stderr, "Validation failed: %d error(s)\n\n", len(errors))
			for i, e := range errors {
				fmt.Fprintf(os.Stderr, "  %d. [%s] %s\n", i+1, e.Phase, e.Message)
				if e.Path != "" {
					fmt.Fprintf(os.Stderr, "     at: %s\n", e.Path)
				}
			}
			return fmt.Errorf("validation failed with %d error(s)", len(errors))
		}
	}
	fmt.Printf("✓ %s is valid (%d steps)\n", rb.Meta.Name, len(rb.Steps))
	return nil
}

// --- exec ---

var (
	execMode       string
	execScenario   string
	execAs         string
	execResume     string
	execVars       []string
	execRebaseTime string
	execICM        string
	execRecord     string
)

var execCmd = &cobra.Command{
	Use:   "exec [runbook.yaml]",
	Short: "Execute a runbook",
	Args:  cobra.ExactArgs(1),
	RunE:  runExec,
}

func runExec(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Validate first
	rb, errs := schema.ValidateFile(filePath)
	if hasValidationErrors(errs) {
		fmt.Fprintf(os.Stderr, "Validation failed: %d error(s)\n", countValidationErrors(errs))
		for _, e := range errs {
			if e.Severity != "warning" {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", e.Phase, e.Message)
			}
		}
		return fmt.Errorf("runbook validation failed")
	}
	printValidationWarnings(errs)

	// Ensure Vars map exists
	if rb.Meta.Vars == nil {
		rb.Meta.Vars = make(map[string]string)
	}

	// Parse --var flags into map
	cliVars := make(map[string]string)
	for _, v := range execVars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --var %q: expected key=value", v)
		}
		cliVars[parts[0]] = parts[1]
	}

	// Resolve inputs: CLI flags → defaults → prompt
	if rb.Meta.Inputs != nil {
		for name, input := range rb.Meta.Inputs {
			if val, ok := cliVars[name]; ok {
				rb.Meta.Vars[name] = val // CLI override
			} else if input.Default != "" {
				rb.Meta.Vars[name] = input.Default
			} else {
				// Prompt for unresolved inputs
				desc := name
				if input.Description != "" {
					desc = input.Description
				}
				fmt.Printf("  [input] %s (%s)\n", name, input.From)
				fmt.Printf("          %s\n", desc)
				fmt.Printf("          value: ")
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					rb.Meta.Vars[name] = strings.TrimSpace(scanner.Text())
				} else {
					return fmt.Errorf("failed to read input %q", name)
				}
			}
		}
	}

	// Also apply any --var that aren't inputs (direct overrides)
	for k, v := range cliVars {
		if rb.Meta.Inputs == nil || rb.Meta.Inputs[k] == nil {
			rb.Meta.Vars[k] = v
		}
	}

	// Resolve environment via xts-cli env resolve if needed
	if envVal, ok := rb.Meta.Vars["environment"]; ok && envVal != "" && rb.Meta.XTS != nil {
		// Check if the value looks like it needs resolution (tenant ring FQDN or region name)
		if strings.Contains(envVal, ".database.") || strings.Contains(envVal, " ") {
			// Try to create a temporary XTS provider for env resolution
			if tmpProv, err := providers.NewXTSProvider(rb.Meta.XTS); err == nil {
				if resolved, err := tmpProv.ResolveEnvironment(envVal); err == nil {
					fmt.Printf("  [env] Resolved %q → %s\n", envVal, resolved)
					rb.Meta.Vars["environment"] = resolved
				} else {
					fmt.Fprintf(os.Stderr, "  [env] Warning: could not resolve %q: %v\n", envVal, err)
				}
			}
		}
	}

	// Set up executor based on mode
	var executor providers.CommandExecutor
	var collector providers.EvidenceCollector
	var xtsScenario *replay.XTSScenario

	switch execMode {
	case "real":
		executor = &providers.RealExecutor{}
		collector = providers.NewInteractiveCollector()
	case "dry-run":
		executor = &DryRunExecutor{}
		collector = &providers.DryRunCollector{}
	case "replay":
		if execScenario == "" {
			return fmt.Errorf("--scenario is required for replay mode")
		}
		// Check if scenario is a directory (XTS scenario) or a file (CLI scenario)
		info, err := os.Stat(execScenario)
		if err != nil {
			return fmt.Errorf("stat scenario: %w", err)
		}
		if info.IsDir() {
			// XTS scenario directory — load with optional time rebasing
			var refTime time.Time
			if execRebaseTime != "" {
				// Only rebase when explicitly requested
				if execRebaseTime == "now" {
					// Use start_time from vars as original reference
					if st, ok := rb.Meta.Vars["start_time"]; ok && st != "" {
						refTime, _ = time.Parse(time.RFC3339, st)
					}
				} else {
					// Use provided time as original reference, rebase to now
					refTime, _ = time.Parse(time.RFC3339, execRebaseTime)
				}
			}
			// refTime is zero if no rebasing requested → LoadXTSScenario skips rebasing
			var err error
			xtsScenario, err = replay.LoadXTSScenario(execScenario, refTime)
			if err != nil {
				return fmt.Errorf("load XTS scenario: %w", err)
			}
			fmt.Printf("  [replay] Loaded scenario from %s (%d step responses)\n", execScenario, len(xtsScenario.StepResponses))
			if xtsScenario.Rebaser != nil {
				fmt.Printf("  [replay] Time rebasing: %s → %s\n", xtsScenario.Rebaser.OriginalRef.Format(time.RFC3339), xtsScenario.Rebaser.ReplayRef.Format(time.RFC3339))
			}
			executor = replay.NewReplayExecutor(xtsScenario.Scenario)
			collector = &providers.DryRunCollector{}
		} else {
			// CLI scenario file
			scenario, err := replay.LoadScenario(execScenario)
			if err != nil {
				return fmt.Errorf("load scenario: %w", err)
			}
			executor = replay.NewReplayExecutor(scenario)
			collector = providers.NewScenarioCollector(scenario.Evidence)
		}
	default:
		return fmt.Errorf("unknown mode: %q", execMode)
	}

	ctx := context.Background()

	// Resume or fresh run
	var engine *runtime.Engine
	if execResume != "" {
		var err error
		engine, err = runtime.ResumeEngine(rb, executor, collector, execResume)
		if err != nil {
			return fmt.Errorf("resume: %w", err)
		}
	} else {
		var err error
		engine, err = runtime.NewEngine(rb, executor, collector, execMode, execAs)
		if err != nil {
			return fmt.Errorf("create engine: %w", err)
		}
	}

	// Inject XTS scenario for replay mode
	if xtsScenario != nil {
		engine.XTSScenario = xtsScenario
	}

	// Set metadata for run manifest
	engine.RunbookPath = filePath
	engine.ICMID = execICM

	// Load tool definitions if the runbook declares tools:
	if len(rb.Tools) > 0 {
		tm := tools.NewManager(executor, engine.Redact)
		baseDir := filepath.Dir(filePath)
		for _, name := range rb.Tools {
			if err := tm.Load(name, filepath.Join("tools", name+".tool.yaml"), baseDir); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to load tool %q: %v\n", name, err)
			}
		}
		engine.ToolManager = tm
	}

	// Register built-in XTS tool when meta.xts is present
	if rb.Meta.XTS != nil {
		if engine.ToolManager == nil {
			engine.ToolManager = tools.NewManager(executor, engine.Redact)
		}
		engine.ToolManager.RegisterBuiltin("__xts", tools.BuildXTSToolDef(engine.GetXTSCLIPath()))
	}

	fmt.Printf("Run ID: %s\n", engine.GetRunID())
	fmt.Printf("Mode: %s\n", execMode)
	if execAs != "" {
		fmt.Printf("Actor: %s\n", execAs)
	}
	if execICM != "" {
		fmt.Printf("ICM: %s\n", execICM)
	}

	runErr := engine.Run(ctx)

	// Write run manifest (always, even on failure)
	if err := engine.WriteManifest(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write run manifest: %v\n", err)
	} else {
		fmt.Printf("  Manifest: %s/run.yaml\n", engine.GetBaseDir())
	}

	// Export as replayable scenario if --record is set
	if execRecord != "" && runErr == nil {
		if err := exportScenario(engine, filePath, execRecord); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to record scenario: %v\n", err)
		}
	}

	if runErr != nil {
		os.Exit(1)
	}
	return nil
}

// exportScenario copies the run artifacts into a replayable scenario folder.
func exportScenario(engine *runtime.Engine, runbookPath, targetDir string) error {
	manifest := engine.BuildManifest()

	// Create target directory structure
	stepsDir := filepath.Join(targetDir, "steps")
	if err := os.MkdirAll(stepsDir, 0755); err != nil {
		return fmt.Errorf("create scenario dir: %w", err)
	}

	// Copy step response files from run artifacts
	srcSteps := filepath.Join(engine.GetBaseDir(), "steps")
	if entries, err := os.ReadDir(srcSteps); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(srcSteps, entry.Name()))
			if err != nil {
				continue
			}
			os.WriteFile(filepath.Join(stepsDir, entry.Name()), data, 0644)
		}
	}

	// Copy run manifest
	runYAML, _ := yaml.Marshal(manifest)
	os.WriteFile(filepath.Join(targetDir, "run.yaml"), runYAML, 0644)

	// Write inputs.yaml from resolved vars
	inputsYAML, _ := yaml.Marshal(manifest.InputsResolved)
	os.WriteFile(filepath.Join(targetDir, "inputs.yaml"), inputsYAML, 0644)

	// Generate scenario.yaml
	scenarioManifest := map[string]interface{}{
		"icm_id":      manifest.ICMID,
		"title":       manifest.Runbook,
		"captured_at": time.Now().UTC().Format(time.RFC3339),
		"run_id":      manifest.RunID,
		"mode":        manifest.Mode,
		"outcome":     manifest.Outcome,
	}

	// List step files
	stepFiles := []string{}
	if entries, err := os.ReadDir(stepsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				stepFiles = append(stepFiles, entry.Name())
			}
		}
	}
	scenarioManifest["step_files"] = stepFiles

	scenarioYAML, _ := yaml.Marshal(scenarioManifest)
	os.WriteFile(filepath.Join(targetDir, "scenario.yaml"), scenarioYAML, 0644)

	fmt.Printf("  Scenario recorded: %s (%d step files)\n", targetDir, len(stepFiles))
	return nil
}

// DryRunExecutor reports commands without executing them.
type DryRunExecutor struct{}

func (d *DryRunExecutor) Execute(ctx context.Context, command string, args []string, env []string) (*providers.CommandResult, error) {
	fmt.Printf("  [dry-run] would execute: %s %v\n", command, args)
	return &providers.CommandResult{
		Stdout:   []byte("<dry-run>"),
		Stderr:   nil,
		ExitCode: 0,
	}, nil
}

// --- debug ---

var (
	debugMode       string
	debugScenario   string
	debugAs         string
	debugRebaseTime string
	debugVars       []string
)

var debugCmd = &cobra.Command{
	Use:   "debug [runbook.yaml]",
	Short: "Launch interactive debugger for a runbook",
	Args:  cobra.ExactArgs(1),
	RunE:  runDebug,
}

func runDebug(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Validate first
	rb, errs := schema.ValidateFile(filePath)
	if hasValidationErrors(errs) {
		fmt.Fprintf(os.Stderr, "Validation failed: %d error(s)\n", countValidationErrors(errs))
		for _, e := range errs {
			if e.Severity != "warning" {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", e.Phase, e.Message)
			}
		}
		return fmt.Errorf("runbook validation failed")
	}
	printValidationWarnings(errs)

	// Ensure Vars map exists and apply --var flags
	if rb.Meta.Vars == nil {
		rb.Meta.Vars = make(map[string]string)
	}
	for _, v := range debugVars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) == 2 {
			rb.Meta.Vars[parts[0]] = parts[1]
		}
	}
	// Resolve inputs into vars
	if rb.Meta.Inputs != nil {
		for name, input := range rb.Meta.Inputs {
			if _, ok := rb.Meta.Vars[name]; !ok {
				if input.Default != "" {
					rb.Meta.Vars[name] = input.Default
				}
			}
		}
	}

	// Set up executor/collector based on mode
	var executor providers.CommandExecutor
	var collector providers.EvidenceCollector
	var xtsScenario *replay.XTSScenario

	switch debugMode {
	case "real":
		executor = &providers.RealExecutor{}
		collector = providers.NewInteractiveCollector()
	case "dry-run":
		executor = &DryRunExecutor{}
		collector = &providers.DryRunCollector{}
	case "replay":
		if debugScenario == "" {
			return fmt.Errorf("--scenario is required for replay mode")
		}
		info, err := os.Stat(debugScenario)
		if err != nil {
			return fmt.Errorf("stat scenario: %w", err)
		}
		if info.IsDir() {
			var refTime time.Time
			if debugRebaseTime == "now" {
				if st, ok := rb.Meta.Vars["start_time"]; ok && st != "" {
					refTime, _ = time.Parse(time.RFC3339, st)
				}
			} else if debugRebaseTime != "" {
				refTime, _ = time.Parse(time.RFC3339, debugRebaseTime)
			}
			var err error
			xtsScenario, err = replay.LoadXTSScenario(debugScenario, refTime)
			if err != nil {
				return fmt.Errorf("load XTS scenario: %w", err)
			}
			fmt.Printf("  [replay] Loaded scenario (%d step responses)\n", len(xtsScenario.StepResponses))
			executor = replay.NewReplayExecutor(xtsScenario.Scenario)
			collector = &providers.DryRunCollector{}
		} else {
			scenario, err := replay.LoadScenario(debugScenario)
			if err != nil {
				return fmt.Errorf("load scenario: %w", err)
			}
			executor = replay.NewReplayExecutor(scenario)
			collector = providers.NewScenarioCollector(scenario.Evidence)
		}
	default:
		return fmt.Errorf("unknown mode: %q", debugMode)
	}

	d, err := debugger.New(rb, executor, collector, debugMode, debugAs)
	if err != nil {
		return fmt.Errorf("create debugger: %w", err)
	}

	// Inject XTS scenario into the engine for replay
	if xtsScenario != nil {
		d.Engine().XTSScenario = xtsScenario
	}

	ctx := context.Background()
	return d.Run(ctx)
}

// --- compile ---

var (
	compileOut        string
	compileMapping    string
	compileEndpoint   string
	compileAPIKey     string
	compileDeployment string
	compileAPIVersion string
)

var compileCmd = &cobra.Command{
	Use:   "compile [tsg.md]",
	Short: "Compile a Markdown TSG into a runbook using Azure OpenAI",
	Long: `Compile a Markdown TSG into a schema-valid runbook.yaml and mapping.md.

Uses an Azure OpenAI agent to interpret prose and code blocks, then
validates the output against the runbook JSON Schema.

Credentials are read from (in priority order):
  1. CLI flags (--endpoint, --api-key, --deployment)
  2. Environment variables
  3. A .env file in the current directory (gitignored)

Create a .env file:
  AZURE_OPENAI_ENDPOINT=https://<resource>.openai.azure.com
  AZURE_OPENAI_API_KEY=<your-key>
  AZURE_OPENAI_DEPLOYMENT=<deployment-name>`,
	Args: cobra.ExactArgs(1),
	RunE: runCompile,
}

func runCompile(cmd *cobra.Command, args []string) error {
	tsgPath := args[0]

	// Derive output paths from source TSG if not explicitly set
	if !cmd.Flags().Changed("out") {
		base := strings.TrimSuffix(filepath.Base(tsgPath), filepath.Ext(tsgPath))
		dir := filepath.Dir(tsgPath)
		compileOut = filepath.Join(dir, base+".runbook.yaml")
	}
	if !cmd.Flags().Changed("mapping") {
		base := strings.TrimSuffix(filepath.Base(tsgPath), filepath.Ext(tsgPath))
		dir := filepath.Dir(tsgPath)
		compileMapping = filepath.Join(dir, base+".mapping.md")
	}

	// Build Azure OpenAI client — flags override env vars
	cfg := compiler.AzureOpenAIConfig{
		Endpoint:   firstNonEmpty(compileEndpoint, os.Getenv("AZURE_OPENAI_ENDPOINT")),
		APIKey:     firstNonEmpty(compileAPIKey, os.Getenv("AZURE_OPENAI_API_KEY")),
		Deployment: firstNonEmpty(compileDeployment, os.Getenv("AZURE_OPENAI_DEPLOYMENT")),
		APIVersion: firstNonEmpty(compileAPIVersion, os.Getenv("AZURE_OPENAI_API_VERSION")),
	}

	client, err := compiler.NewAzureOpenAIClient(cfg)
	if err != nil {
		return fmt.Errorf("Azure OpenAI setup: %w\n\nCreate a .env file in the project root with:\n  AZURE_OPENAI_ENDPOINT=https://<resource>.openai.azure.com\n  AZURE_OPENAI_API_KEY=<your-key>\n  AZURE_OPENAI_DEPLOYMENT=<deployment-name>", err)
	}

	fmt.Printf("Compiling TSG via Azure OpenAI (%s)...\n", cfg.Deployment)
	result, err := compiler.CompileTSG(tsgPath, client)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	fmt.Printf("%d steps identified\n", result.StepCount)
	fmt.Printf("  %d CLI steps, %d manual steps, %d TODOs\n",
		result.CLICount, result.ManualCount, result.TODOCount)

	// Write runbook
	fmt.Printf("Generating %s... ", compileOut)
	if err := compiler.WriteRunbook(result.Runbook, compileOut); err != nil {
		return fmt.Errorf("write runbook: %w", err)
	}
	fmt.Println("done")

	// Write mapping report
	fmt.Printf("Generating %s... ", compileMapping)
	if err := compiler.WriteMapping(result.Mapping, compileMapping); err != nil {
		return fmt.Errorf("write mapping: %w", err)
	}
	fmt.Println("done")

	// Stage C: Post-compilation validation
	fmt.Printf("Validating output... ")
	_, errs := schema.ValidateFile(compileOut)
	if hasValidationErrors(errs) {
		fmt.Println("FAILED")
		for _, e := range errs {
			if e.Severity != "warning" {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", e.Phase, e.Message)
			}
		}
		return fmt.Errorf("generated runbook failed validation with %d error(s)", countValidationErrors(errs))
	}
	fmt.Println("passed")

	if len(result.Warnings) > 0 {
		fmt.Printf("\nWarnings (%d):\n", len(result.Warnings))
		for _, w := range result.Warnings {
			fmt.Printf("  ⚠ %s\n", w)
		}
		os.Exit(1) // Exit 1 = completed with warnings
	}

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// --- schema export ---

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Schema operations",
}

var schemaExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export JSON Schema to stdout",
	RunE:  runSchemaExport,
}

func runSchemaExport(cmd *cobra.Command, args []string) error {
	data, err := schema.GenerateJSONSchema()
	if err != nil {
		return fmt.Errorf("generate schema: %w", err)
	}
	// Pretty-print the JSON
	var out json.RawMessage = data
	formatted, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		// fallback to raw
		fmt.Println(string(data))
		return nil
	}
	fmt.Println(string(formatted))
	return nil
}

// --- version ---

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("gert %s (build: %s)\n", version, commit)
	},
}

func init() {
	// exec flags
	execCmd.Flags().StringVar(&execMode, "mode", "real", "Execution mode: real, replay, or dry-run")
	execCmd.Flags().StringVar(&execScenario, "scenario", "", "Path to scenario YAML (required for replay)")
	execCmd.Flags().StringVar(&execAs, "as", "", "Actor identity for approval recording")
	execCmd.Flags().StringVar(&execResume, "resume", "", "Run ID to resume from last completed step")
	execCmd.Flags().StringArrayVar(&execVars, "var", nil, "Set a variable (key=value), repeatable")
	execCmd.Flags().StringVar(&execRebaseTime, "rebase-time", "", "Rebase scenario timestamps: 'now' or a reference timestamp. Omit to keep original times.")
	execCmd.Flags().StringVar(&execICM, "icm", "", "ICM incident ID to link this run to")
	execCmd.Flags().StringVar(&execRecord, "record", "", "Save run as a replayable scenario to this directory")

	// debug flags
	debugCmd.Flags().StringVar(&debugMode, "mode", "real", "Execution mode: real, dry-run, or replay")
	debugCmd.Flags().StringVar(&debugScenario, "scenario", "", "Path to scenario YAML or directory (required for replay)")
	debugCmd.Flags().StringVar(&debugAs, "as", "", "Actor identity for approvals")
	debugCmd.Flags().StringVar(&debugRebaseTime, "rebase-time", "", "Rebase scenario timestamps: 'now' or reference timestamp")
	debugCmd.Flags().StringArrayVar(&debugVars, "var", nil, "Set a variable (key=value), repeatable")

	// compile flags
	compileCmd.Flags().StringVar(&compileOut, "out", "runbook.yaml", "Output path for the generated runbook")
	compileCmd.Flags().StringVar(&compileMapping, "mapping", "mapping.md", "Output path for the mapping report")
	compileCmd.Flags().StringVar(&compileEndpoint, "endpoint", "", "Azure OpenAI endpoint (overrides AZURE_OPENAI_ENDPOINT)")
	compileCmd.Flags().StringVar(&compileAPIKey, "api-key", "", "Azure OpenAI API key (overrides AZURE_OPENAI_API_KEY)")
	compileCmd.Flags().StringVar(&compileDeployment, "deployment", "", "Azure OpenAI deployment name (overrides AZURE_OPENAI_DEPLOYMENT)")
	compileCmd.Flags().StringVar(&compileAPIVersion, "api-version", "", "Azure OpenAI API version (overrides AZURE_OPENAI_API_VERSION)")

	// schema subcommands
	schemaCmd.AddCommand(schemaExportCmd)

	// root subcommands
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(compileCmd)
	rootCmd.AddCommand(schemaCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(icmCmd)
	rootCmd.AddCommand(testCmd)
	rootCmd.AddCommand(migrateCmd)

	// test flags
	testCmd.Flags().StringVar(&testScenario, "scenario", "", "Run only the named scenario (default: all)")
	testCmd.Flags().BoolVar(&testJSON, "json", false, "Output results as structured JSON")
	testCmd.Flags().BoolVar(&testFailFast, "fail-fast", false, "Stop after first failure")
	testCmd.Flags().StringVar(&testTimeout, "timeout", "30s", "Per-scenario timeout (e.g. 30s, 1m)")

	// icm subcommands & flags
	icmCmd.AddCommand(icmSearchCmd)
	icmCmd.AddCommand(icmGetCmd)
	icmSearchCmd.Flags().StringVar(&icmSearchFilter, "filter", "", "OData $filter expression (required)")
	icmSearchCmd.Flags().IntVar(&icmSearchTop, "top", 20, "Max results to return")
	icmSearchCmd.Flags().BoolVar(&icmOutputJSON, "json", false, "Output as JSON")
	icmGetCmd.Flags().BoolVar(&icmOutputJSON, "json", false, "Output as JSON")
}

// --- icm ---

var (
	icmSearchTop    int
	icmSearchFilter string
	icmGetExpand    bool
	icmOutputJSON   bool
)

var icmCmd = &cobra.Command{
	Use:   "icm",
	Short: "Query the Microsoft ICM API",
}

var icmSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search ICM incidents by OData filter",
	Long: `Search ICM for incidents matching an OData $filter expression.

Requires Azure CLI login (az login) with access to the ICM Readers role.

Examples:
  gert icm search --filter "OwningTeamId eq 'YOURTEAMID' and Status eq 'Resolved'"
  gert icm search --filter "substringof('DNS alias', Title)" --top 10
  gert icm search --filter "Severity le 2 and Status eq 'Active'" --json`,
	RunE: runIcmSearch,
}

var icmGetCmd = &cobra.Command{
	Use:   "get [incidentId]",
	Short: "Get full details for an ICM incident",
	Long: `Retrieve full incident details including custom fields.

Examples:
  gert icm get 226532540
  gert icm get 226532540 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runIcmGet,
}

func runIcmSearch(cmd *cobra.Command, args []string) error {
	if icmSearchFilter == "" {
		return fmt.Errorf("--filter is required\n\nExample: gert icm search --filter \"OwningTeamId eq 'TEAMID' and Status eq 'Resolved'\"")
	}

	client := icm.New()
	incidents, err := client.Search(icmSearchFilter, icmSearchTop)
	if err != nil {
		return err
	}

	if icmOutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(incidents)
	}

	if len(incidents) == 0 {
		fmt.Println("No incidents found.")
		return nil
	}

	fmt.Printf("Found %d incident(s):\n\n", len(incidents))
	for _, inc := range incidents {
		sev := fmt.Sprintf("Sev%d", inc.Severity)
		fmt.Printf("  %d  %-6s  %-12s  %s  %s\n",
			inc.Id, sev, inc.Status,
			inc.CreateDate.Format("2006-01-02"),
			truncateStr(inc.Title, 80))
	}
	return nil
}

func runIcmGet(cmd *cobra.Command, args []string) error {
	var id int64
	if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
		return fmt.Errorf("invalid incident ID %q: expected a number", args[0])
	}

	client := icm.New()
	inc, err := client.Get(id)
	if err != nil {
		return err
	}

	if icmOutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(inc)
	}

	// Human-readable output
	fmt.Printf("Incident %d\n", inc.Id)
	fmt.Printf("  Title:    %s\n", inc.Title)
	fmt.Printf("  Severity: %d\n", inc.Severity)
	fmt.Printf("  Status:   %s\n", inc.Status)
	fmt.Printf("  Created:  %s\n", inc.CreateDate.Format(time.RFC3339))
	if inc.OwningTeamId != "" {
		fmt.Printf("  Team:     %s\n", inc.OwningTeamId)
	}
	if inc.MitigateDate != nil {
		fmt.Printf("  Mitigated: %s\n", inc.MitigateDate.Format(time.RFC3339))
	}
	if inc.ResolveDate != nil {
		fmt.Printf("  Resolved:  %s\n", inc.ResolveDate.Format(time.RFC3339))
	}
	if inc.MitigationData != nil && inc.MitigationData.Entry != "" {
		fmt.Printf("  Resolution: %s\n", truncateStr(inc.MitigationData.Entry, 200))
	}

	if len(inc.Fields) > 0 {
		fmt.Printf("\n  Custom Fields (%d):\n", len(inc.Fields))
		for k, v := range inc.Fields {
			fmt.Printf("    %-30s  %s\n", k, truncateStr(v, 100))
		}
	}
	return nil
}

func truncateStr(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// --- serve ---

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start JSON-RPC server for VS Code extension (stdio)",
	Long: `Start a JSON-RPC server that communicates over stdin/stdout.
Used by the gert VS Code extension to drive runbook execution interactively.
Messages are newline-delimited JSON-RPC 2.0.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s := serve.New()

		// Load input providers from workspace config, or fall back to built-in ICM
		cwd, _ := os.Getwd()
		wsCfg, _ := inputs.LoadWorkspaceConfig(cwd)
		var inputMgr *inputs.Manager
		if wsCfg != nil && len(wsCfg.Providers) > 0 {
			inputMgr = inputs.LoadProvidersFromConfig(wsCfg, cwd)
		} else {
			// Fallback: register built-in ICM provider
			inputMgr = inputs.NewManager()
			inputMgr.Register(&icm.ICMInputProvider{})
		}
		s.InputManager = inputMgr
		defer inputMgr.Shutdown()

		return s.Run()
	},
}

// --- test ---

var (
	testScenario string
	testJSON     bool
	testFailFast bool
	testTimeout  string
)

var testCmd = &cobra.Command{
	Use:   "test [runbook.yaml...]",
	Short: "Run scenario replay tests for runbooks",
	Long: `Discover scenarios for each runbook, replay them, and compare against test.yaml assertions.

Scenarios are discovered by convention at:
  {runbook-dir}/scenarios/{runbook-name}/*/inputs.yaml

Only scenarios with a test.yaml file are asserted. Scenarios without
test.yaml are reported as skipped.

Exit codes:
  0 — all asserted tests passed
  1 — at least one asserted test failed
  2 — runbook validation failed (no tests ran)`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTest,
}

func runTest(cmd *cobra.Command, args []string) error {
	timeout := 30 * time.Second
	if testTimeout != "" {
		d, err := time.ParseDuration(testTimeout)
		if err != nil {
			return fmt.Errorf("invalid --timeout %q: %w", testTimeout, err)
		}
		timeout = d
	}

	runner := &runtest.Runner{Timeout: timeout}
	allPassed := true
	hasValidationError := false

	for _, runbookPath := range args {
		output, err := runner.RunAll(runbookPath, testFailFast)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", runbookPath, err)
			hasValidationError = true
			continue
		}

		if testJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(output)
		} else {
			printTestOutput(output)
		}

		if output.Summary.Failed > 0 || output.Summary.Errors > 0 {
			allPassed = false
		}
		if testFailFast && !allPassed {
			break
		}
	}

	if hasValidationError {
		os.Exit(2)
	}
	if !allPassed {
		os.Exit(1)
	}
	return nil
}

func printTestOutput(output *runtest.TestOutput) {
	fmt.Printf("\n  %s\n", output.Runbook)
	for _, s := range output.Scenarios {
		switch s.Status {
		case "passed":
			outcome := ""
			if s.Outcome != nil {
				outcome = s.Outcome.Actual
			}
			fmt.Printf("    ✓ %-30s (%s)  %dms\n", s.ScenarioName, outcome, s.DurationMs)
		case "failed":
			outcome := ""
			if s.Outcome != nil {
				outcome = fmt.Sprintf("expected: %s, got: %s", s.Outcome.Expected, s.Outcome.Actual)
			}
			fmt.Printf("    ✗ %-30s (%s)  %dms\n", s.ScenarioName, outcome, s.DurationMs)
			for _, a := range s.Assertions {
				if !a.Passed {
					fmt.Printf("        %s: %s\n", a.Type, a.Message)
				}
			}
		case "skipped":
			fmt.Printf("    ○ %-30s (no test.yaml)  %dms\n", s.ScenarioName, s.DurationMs)
		case "error":
			fmt.Printf("    ✗ %-30s ERROR: %s\n", s.ScenarioName, s.Error)
		}
	}
	fmt.Printf("\n  %d scenarios, %d passed, %d failed, %d skipped\n",
		output.Summary.Total, output.Summary.Passed, output.Summary.Failed, output.Summary.Skipped)
	if output.Summary.Errors > 0 {
		fmt.Printf("  %d errors\n", output.Summary.Errors)
	}
}

// hasValidationErrors returns true if any error (non-warning) is present.
func hasValidationErrors(errs []*schema.ValidationError) bool {
	for _, e := range errs {
		if e.Severity != "warning" {
			return true
		}
	}
	return false
}

// countValidationErrors counts non-warning errors.
func countValidationErrors(errs []*schema.ValidationError) int {
	n := 0
	for _, e := range errs {
		if e.Severity != "warning" {
			n++
		}
	}
	return n
}

// printValidationWarnings prints any warnings to stderr.
func printValidationWarnings(errs []*schema.ValidationError) {
	for _, e := range errs {
		if e.Severity == "warning" {
			fmt.Fprintf(os.Stderr, "  ⚠ [%s] %s\n", e.Phase, e.Message)
		}
	}
}

// --- migrate ---

var migrateCmd = &cobra.Command{
	Use:   "migrate [runbook.yaml]",
	Short: "Migrate a runbook from v0 to v1 (rewrites type:xts to type:tool)",
	Long: `Migrate a runbook/v0 YAML file to runbook/v1:
  - Converts type:xts steps to type:tool with xts tool references
  - Moves meta.xts.environment to step-level args
  - Adds a tools: block with the xts tool reference
  - Bumps apiVersion to runbook/v1

The file is rewritten in place. Use --dry-run to preview changes.`,
	Args: cobra.ExactArgs(1),
	RunE: runMigrate,
}

var migrateDryRun bool

func init() {
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Preview changes without writing")
}

func runMigrate(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", filePath, err)
	}

	rb, loadErr := schema.LoadFile(filePath)
	if loadErr != nil {
		return fmt.Errorf("parse %s: %w", filePath, loadErr)
	}

	if rb.APIVersion == "runbook/v1" {
		fmt.Println("Already runbook/v1 — no migration needed.")
		return nil
	}
	if rb.APIVersion != "runbook/v0" {
		return fmt.Errorf("unexpected apiVersion %q — can only migrate from runbook/v0", rb.APIVersion)
	}

	// Check if there are any XTS steps to migrate
	hasXTS := false
	content := string(data)
	if strings.Contains(content, "type: xts") || strings.Contains(content, "type:xts") {
		hasXTS = true
	}

	if !hasXTS && rb.Meta.XTS == nil {
		// Simple version bump
		content = strings.Replace(content, "apiVersion: runbook/v0", "apiVersion: runbook/v1", 1)
		if migrateDryRun {
			fmt.Println("Would bump apiVersion to runbook/v1 (no XTS steps found)")
			return nil
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return err
		}
		fmt.Println("✓ Bumped apiVersion to runbook/v1 (no XTS steps)")
		return nil
	}

	// Count changes
	changes := 0

	// 1. Bump apiVersion
	content = strings.Replace(content, "apiVersion: runbook/v0", "apiVersion: runbook/v1", 1)
	changes++

	// 2. Add tools: block if not present and XTS is used
	if hasXTS && !strings.Contains(content, "tools:") {
		// Insert tools: block after apiVersion line
		content = strings.Replace(content,
			"apiVersion: runbook/v1\n",
			"apiVersion: runbook/v1\n\ntools:\n  xts: xts.tool.yaml\n",
			1)
		changes++
	}

	// 3. Remove meta.xts block (simple line-based removal)
	if rb.Meta.XTS != nil {
		lines := strings.Split(content, "\n")
		var newLines []string
		inXTSBlock := false
		xtsIndent := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			indent := len(line) - len(strings.TrimLeft(line, " "))

			if trimmed == "xts:" && indent > 0 && !inXTSBlock {
				// Check if this is meta.xts (not step.xts)
				// meta.xts is at indent 2 (under meta:)
				if indent <= 4 {
					inXTSBlock = true
					xtsIndent = indent
					continue
				}
			}

			if inXTSBlock {
				if indent > xtsIndent || trimmed == "" {
					continue // skip xts child lines
				}
				inXTSBlock = false
			}

			newLines = append(newLines, line)
		}
		content = strings.Join(newLines, "\n")
		changes++
	}

	if migrateDryRun {
		fmt.Printf("Would make %d change(s) to %s:\n", changes, filePath)
		fmt.Println("  - Bump apiVersion to runbook/v1")
		if hasXTS {
			fmt.Println("  - Add tools: block with xts reference")
		}
		if rb.Meta.XTS != nil {
			fmt.Println("  - Remove meta.xts block")
		}
		fmt.Println("\nNote: type:xts steps should be manually converted to type:tool.")
		fmt.Println("Use 'gert validate' after migration to identify remaining issues.")
		return nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("✓ Migrated %s to runbook/v1 (%d changes)\n", filePath, changes)
	if hasXTS {
		fmt.Println("  Note: type:xts steps need manual conversion to type:tool.")
		fmt.Println("  Run 'gert validate' to see remaining issues.")
	}
	return nil
}
