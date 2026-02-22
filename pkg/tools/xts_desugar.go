package tools

import (
	"fmt"
	"strings"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// DesugarXTSToToolStep converts a type:xts step into a synthetic type:tool step
// that can be executed via the tool manager. This handles the full argv construction
// including --param, --auto-select, --sql-timeout, and view path resolution.
//
// The returned step is a copy — the original is not modified.
func DesugarXTSToToolStep(step schema.Step, defaultEnv string, viewsRoot string, cliPath string) schema.Step {
	xts := step.XTS
	if xts == nil {
		return step
	}

	action := xts.Mode // "query", "view", or "activity"

	// Resolve environment: step → default → empty
	env := xts.Environment
	if env == "" {
		env = defaultEnv
	}

	// Build args map
	args := map[string]string{
		"environment": env,
	}

	switch xts.Mode {
	case "query":
		args["query_type"] = xts.QueryType
		args["query"] = xts.Query
	case "view":
		args["file"] = resolveXTSViewPath(xts.File, viewsRoot)
		if xts.AutoSelect {
			args["auto_select"] = "true"
		}
		if xts.SQLTimeout > 0 {
			args["sql_timeout"] = fmt.Sprintf("%d", xts.SQLTimeout)
		}
		// Flatten params as param_<key>
		for k, v := range xts.Params {
			args["param_"+k] = v
		}
	case "activity":
		args["file"] = resolveXTSViewPath(xts.File, viewsRoot)
		args["activity"] = xts.Activity
		if xts.SQLTimeout > 0 {
			args["sql_timeout"] = fmt.Sprintf("%d", xts.SQLTimeout)
		}
		for k, v := range xts.Params {
			args["param_"+k] = v
		}
	}

	// Build synthesized step
	synth := step // copy
	synth.Type = "tool"
	synth.XTS = nil
	synth.Tool = &schema.ToolStepConfig{
		Name:   "__xts",
		Action: action,
		Args:   args,
	}

	return synth
}

// BuildXTSToolDef returns the enhanced built-in XTS tool definition with
// full argv construction including --param, --auto-select, --sql-timeout.
// The binary can be overridden via cliPath.
func BuildXTSToolDef(cliPath string) *schema.ToolDefinition {
	binary := "xts-cli"
	if cliPath != "" {
		binary = cliPath
	}

	return &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta: schema.ToolMeta{
			Name:        "xts",
			Version:     "builtin",
			Description: "XTS query execution for Azure Service Fabric (built-in)",
			Binary:      binary,
		},
		Transport: schema.ToolTransport{Mode: "stdio"},
		Governance: &schema.ToolGovernance{
			ReadOnly: true,
		},
		Actions: map[string]schema.ToolAction{
			"query": {
				Description: "Run an ad-hoc query (SQL, Kusto, CMS, MDS)",
				// Argv is built dynamically — the template is for the simple case
				Argv: []string{"query", "--type", "{{ .query_type }}", "-e", "{{ .environment }}", "-q", "{{ .query }}", "--format", "json"},
				Args: map[string]schema.ToolArg{
					"query_type":  {Type: "string", Required: true, Enum: []string{"sql", "kusto", "cms", "mds"}},
					"environment": {Type: "string", Required: true},
					"query":       {Type: "string", Required: true},
				},
				Capture: map[string]schema.ToolCapture{"stdout": {Format: "json"}},
				Governance: &schema.ActionGovernance{ReadOnly: true},
			},
			"view": {
				Description: "Execute an XTS view file",
				// Dynamic argv built by custom handler (params, auto_select, sql_timeout vary)
				Argv: []string{"execute", "--file", "{{ .file }}", "--environment", "{{ .environment }}", "--format", "json"},
				Args: map[string]schema.ToolArg{
					"file":        {Type: "string", Required: true},
					"environment": {Type: "string", Required: true},
					"auto_select": {Type: "string", Required: false},
					"sql_timeout": {Type: "string", Required: false},
				},
				Capture: map[string]schema.ToolCapture{"stdout": {Format: "json"}},
				Governance: &schema.ActionGovernance{ReadOnly: true},
			},
			"activity": {
				Description: "Execute an activity within an XTS view",
				Argv:        []string{"execute-activity", "--file", "{{ .file }}", "--activity", "{{ .activity }}", "--environment", "{{ .environment }}", "--format", "json"},
				Args: map[string]schema.ToolArg{
					"file":        {Type: "string", Required: true},
					"activity":    {Type: "string", Required: true},
					"environment": {Type: "string", Required: true},
					"sql_timeout": {Type: "string", Required: false},
				},
				Capture: map[string]schema.ToolCapture{"stdout": {Format: "json"}},
				Governance: &schema.ActionGovernance{ReadOnly: true},
			},
		},
	}
}

// BuildXTSCompatAliasToolDef returns a compatibility tool definition for runbooks
// that reference an external "xts" tool file with args like cluster/query_type/query.
// It executes via stdio against xts-cli directly, avoiding JSON-RPC method mismatches.
func BuildXTSCompatAliasToolDef(cliPath string) *schema.ToolDefinition {
	binary := "xts-cli"
	if cliPath != "" {
		binary = cliPath
	}

	return &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta: schema.ToolMeta{
			Name:        "xts",
			Version:     "builtin-compat",
			Description: "XTS query execution compatibility tool (built-in)",
			Binary:      binary,
		},
		Transport: schema.ToolTransport{Mode: "stdio"},
		Governance: &schema.ToolGovernance{ReadOnly: true},
		Actions: map[string]schema.ToolAction{
			"query": {
				Description: "Run an XTS query",
				Argv:        []string{"query", "--type", "{{ .query_type }}", "-e", "{{ .cluster }}", "-q", "{{ .query }}", "--format", "json"},
				Args: map[string]schema.ToolArg{
					"cluster":    {Type: "string", Required: true},
					"query_type": {Type: "string", Required: true, Enum: []string{"wql", "sql", "kusto", "cms", "mds"}},
					"query":      {Type: "string", Required: true},
				},
				Capture: map[string]schema.ToolCapture{
					"stdout":            {Format: "json"},
					"result":            {From: "data", Format: "json"},
					"row_count":         {From: "rowCount", Format: "text"},
					"first_app_type":    {From: "data[0].AppTypeName", Format: "text"},
					"first_error":       {From: "data[0].error", Format: "text"},
					"first_state":       {From: "data[0].state", Format: "text"},
					"failure_count":     {From: "data[0].FailureCount", Format: "text"},
					"gateway_nodes":     {From: "data[0].GatewayNodesWithFailure", Format: "text"},
					"restore_request_id": {From: "data[0].restore_request_id", Format: "text"},
				},
				Governance: &schema.ActionGovernance{ReadOnly: true},
			},
		},
	}
}

// resolveXTSViewPath resolves a view file path relative to the views root.
func resolveXTSViewPath(file string, viewsRoot string) string {
	if file == "" {
		return ""
	}
	// If already absolute or viewsRoot empty, return as-is
	if viewsRoot == "" || isAbsPath(file) {
		return file
	}
	// Join with views root
	return viewsRoot + "/" + file
}

// isAbsPath checks if a path is absolute (cross-platform simplification).
func isAbsPath(p string) bool {
	if len(p) == 0 {
		return false
	}
	if p[0] == '/' || p[0] == '\\' {
		return true
	}
	// Windows drive letter
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	return false
}

// BuildXTSArgv builds the full xts-cli argv for a desugared XTS tool step.
// This handles --param, --auto-select, --sql-timeout which can't be expressed
// in a simple argv template.
func BuildXTSArgv(action string, args map[string]string) []string {
	var argv []string

	switch action {
	case "query":
		argv = append(argv, "query", "--type", args["query_type"])
		if env := args["environment"]; env != "" {
			argv = append(argv, "-e", env)
		}
		argv = append(argv, "-q", args["query"])
		argv = append(argv, "--format", "json")

	case "view":
		argv = append(argv, "execute", "--file", args["file"])
		if env := args["environment"]; env != "" {
			argv = append(argv, "--environment", env)
		}
		if args["auto_select"] == "true" {
			argv = append(argv, "--auto-select")
		}
		if st := args["sql_timeout"]; st != "" && st != "0" {
			argv = append(argv, "--sql-timeout", st)
		}
		// Add --param flags
		for k, v := range args {
			if strings.HasPrefix(k, "param_") {
				paramName := strings.TrimPrefix(k, "param_")
				argv = append(argv, "--param", paramName+"="+v)
			}
		}
		argv = append(argv, "--format", "json")

	case "activity":
		argv = append(argv, "execute-activity",
			"--file", args["file"],
			"--activity", args["activity"])
		if env := args["environment"]; env != "" {
			argv = append(argv, "--environment", env)
		}
		if st := args["sql_timeout"]; st != "" && st != "0" {
			argv = append(argv, "--sql-timeout", st)
		}
		for k, v := range args {
			if strings.HasPrefix(k, "param_") {
				paramName := strings.TrimPrefix(k, "param_")
				argv = append(argv, "--param", paramName+"="+v)
			}
		}
		argv = append(argv, "--format", "json")
	}

	return argv
}
