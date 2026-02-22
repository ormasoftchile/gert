// xts_builtin.go provides a built-in tool definition for XTS that enables
// type:xts steps to be rendered with tool UX in the extension while still
// routing through the native XTS provider for execution.
//
// This is Phase 6 of the tool definitions plan: a bridge that desugars
// type:xts metadata for the extension while preserving the existing
// execution path (replay, captures, XTSOutput parsing).
package tools

import "github.com/ormasoftchile/gert/pkg/schema"

// BuiltinXTSToolDef returns a synthetic ToolDefinition for XTS.
// This is used by the serve layer to enrich stepStarted events with
// tool metadata (name, action, args, governance) so the extension
// renders XTS steps with the same structured UI as type:tool steps.
func BuiltinXTSToolDef() *schema.ToolDefinition {
	return &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta: schema.ToolMeta{
			Name:        "xts",
			Version:     "builtin",
			Description: "XTS query execution for Azure Service Fabric (built-in)",
			Binary:      "xts-cli",
		},
		Transport: schema.ToolTransport{
			Mode: "stdio",
		},
		Governance: &schema.ToolGovernance{
			ReadOnly: true,
		},
		Actions: map[string]schema.ToolAction{
			"query": {
				Description: "Run an ad-hoc query (SQL, Kusto, CMS, MDS)",
				Argv:        []string{"query", "--type", "{{ .query_type }}", "-e", "{{ .environment }}", "-q", "{{ .query }}", "--format", "json"},
				Args: map[string]schema.ToolArg{
					"query_type":  {Type: "string", Required: true, Enum: []string{"sql", "kusto", "cms", "mds"}},
					"environment": {Type: "string", Required: true},
					"query":       {Type: "string", Required: true},
				},
				Capture: map[string]schema.ToolCapture{
					"stdout": {Format: "json"},
				},
				Governance: &schema.ActionGovernance{ReadOnly: true},
			},
			"view": {
				Description: "Execute an XTS view file",
				Argv:        []string{"execute", "--file", "{{ .file }}", "--environment", "{{ .environment }}", "--format", "json"},
				Args: map[string]schema.ToolArg{
					"file":        {Type: "string", Required: true},
					"environment": {Type: "string", Required: true},
				},
				Capture: map[string]schema.ToolCapture{
					"stdout": {Format: "json"},
				},
				Governance: &schema.ActionGovernance{ReadOnly: true},
			},
			"activity": {
				Description: "Execute an activity within an XTS view",
				Argv:        []string{"execute-activity", "--file", "{{ .file }}", "--activity", "{{ .activity }}", "--environment", "{{ .environment }}", "--format", "json"},
				Args: map[string]schema.ToolArg{
					"file":        {Type: "string", Required: true},
					"activity":    {Type: "string", Required: true},
					"environment": {Type: "string", Required: true},
				},
				Capture: map[string]schema.ToolCapture{
					"stdout": {Format: "json"},
				},
				Governance: &schema.ActionGovernance{ReadOnly: true},
			},
		},
	}
}

// DesugarXTSStep converts a type:xts step's metadata into tool-style metadata
// for the serve layer's event enrichment. It does NOT modify the step — the
// engine still executes it via the native XTS provider. This is only used to
// build the "tool" field in event/stepStarted so the extension renders the
// structured tool info block.
func DesugarXTSStep(step *schema.Step, defaultEnv string) map[string]interface{} {
	if step.XTS == nil {
		return nil
	}

	xts := step.XTS
	action := xts.Mode // "query", "view", or "activity"

	env := xts.Environment
	if env == "" {
		env = defaultEnv
	}

	args := map[string]string{
		"environment": env,
	}

	switch xts.Mode {
	case "query":
		args["query_type"] = xts.QueryType
		args["query"] = xts.Query
	case "view":
		args["file"] = xts.File
	case "activity":
		args["file"] = xts.File
		args["activity"] = xts.Activity
	}

	// Add params
	for k, v := range xts.Params {
		args["param_"+k] = v
	}

	return map[string]interface{}{
		"name":   "xts",
		"action": action,
		"args":   args,
		"governance": map[string]interface{}{
			"read_only": true,
		},
	}
}
