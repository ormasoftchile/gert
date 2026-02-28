package validate

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"github.com/ormasoftchile/gert/pkg/kernel/contract"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// validateDomain runs kernel/v0 domain-level validation rules.
func validateDomain(rb *schema.Runbook, baseDir string) []*ValidationError {
	var errs []*ValidationError

	// D1: apiVersion must be kernel/v0
	if rb.APIVersion != schema.APIVersionKernel {
		errs = append(errs, errorf("domain", "apiVersion", "expected %q, got %q", schema.APIVersionKernel, rb.APIVersion))
	}

	// D2: step type must be one of the seven kernel types
	for i, step := range rb.Steps {
		errs = append(errs, validateStepType(step, fmt.Sprintf("steps[%d]", i))...)
	}

	// D3: step ID uniqueness (global across the entire step graph)
	ids := map[string]string{} // id → path
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.ID != "" {
			if prev, ok := ids[s.ID]; ok {
				errs = append(errs, errorf("domain", path+".id", "duplicate step ID %q (first at %s)", s.ID, prev))
			} else {
				ids[s.ID] = path
			}
		}
	})

	// D4: type-specific required fields
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		errs = append(errs, validateStepFields(s, path)...)
	})

	// D5: end-step reachability — every reachable path must lead to an end step
	errs = append(errs, validateEndReachability(rb.Steps, "steps")...)

	// D6: next target scoping — targets must be scope-local
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		errs = append(errs, validateNextTarget(s, rb.Steps, path)...)
	})

	// D7: next backward jumps must have max
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		errs = append(errs, validateNextBounded(s, rb.Steps, path)...)
	})

	// D8: variable resolution — all template refs must resolve
	errs = append(errs, validateVariableResolution(rb, baseDir)...)

	// D9: constant immutability — step outputs must not shadow constants
	errs = append(errs, validateConstantImmutability(rb)...)

	// D10: parallel branch output uniqueness
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.Type == schema.StepParallel {
			errs = append(errs, validateParallelOutputs(s, path)...)
		}
	})

	// D11: parallel branch contract conflict detection
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.Type == schema.StepParallel {
			errs = append(errs, validateParallelConflicts(s, path)...)
		}
	})

	// D12: outcome category must be valid enum
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.Type == schema.StepEnd && s.Outcome != nil {
			errs = append(errs, validateOutcomeCategory(s, path)...)
		}
	})

	// D13: branch must have conditions
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.Type == schema.StepBranch {
			errs = append(errs, validateBranchConditions(s, path)...)
		}
	})

	// D14: assert step must have assertions
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.Type == schema.StepAssert && len(s.Assert) == 0 {
			errs = append(errs, errorf("domain", path, "assert step must have at least one assertion"))
		}
	})

	// D15: for_each validation
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.ForEach != nil {
			errs = append(errs, validateForEach(s, path)...)
		}
	})

	// D16: tool step — validate tool is in the allow-list
	if len(rb.Tools) > 0 {
		toolSet := make(map[string]struct{}, len(rb.Tools))
		for _, t := range rb.Tools {
			toolSet[t] = struct{}{}
		}
		walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
			if s.Type == schema.StepTool && s.Tool != "" {
				if _, ok := toolSet[s.Tool]; !ok {
					errs = append(errs, errorf("domain", path+".tool", "tool %q is not declared in the runbook tools list", s.Tool))
				}
			}
		})
	}

	// D17: extension step must have inline contract
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.Type == schema.StepExtension && s.Contract == nil {
			errs = append(errs, errorf("domain", path, "extension step must declare an inline contract"))
		}
	})

	// D18: contract tightening — validate tool-step contracts against tool definitions
	errs = append(errs, validateContractTightening(rb, baseDir)...)

	// D19: inputs_from validation
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.InputsFrom != nil {
			errs = append(errs, validateInputsFrom(s, rb, path)...)
		}
	})

	// D20: evidence requirements
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.Type == schema.StepManual {
			errs = append(errs, validateEvidence(s, path)...)
		}
	})

	// D21: platform constraints — warn if tool steps reference platform-restricted tools
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.Type != schema.StepTool || s.Tool == "" {
			return
		}
		toolPath := ResolveToolPath(s.Tool, baseDir, "")
		if toolPath == "" {
			return
		}
		td, err := schema.LoadToolFile(toolPath)
		if err != nil {
			return
		}
		if len(td.Meta.Platform) > 0 && !platformMatches(td.Meta.Platform) {
			errs = append(errs, warningf("domain", path,
				"tool %q requires platform %v but current OS is %q", s.Tool, td.Meta.Platform, runtime.GOOS))
		}
	})

	return errs
}

// validateToolEffects checks effects/side_effects consistency on a tool definition.
func validateToolEffects(td *schema.ToolDefinition) []*ValidationError {
	var errs []*ValidationError
	c := &td.Contract

	// Error if both side_effects and effects declared
	if c.SideEffects != nil && len(c.Effects) > 0 {
		errs = append(errs, errorf("domain", "contract", "cannot declare both 'side_effects' and 'effects' — use 'effects' only"))
	}

	// Warning if side_effects used without effects (deprecated)
	if c.SideEffects != nil && len(c.Effects) == 0 {
		errs = append(errs, warningf("domain", "contract.side_effects", "'side_effects' is deprecated — use 'effects: [...]' instead"))
	}

	// Validate secrets
	for i, secret := range td.Meta.Secrets {
		if secret.Env == "" {
			errs = append(errs, errorf("domain", fmt.Sprintf("meta.secrets[%d].env", i), "secret env var name is required"))
		}
	}

	return errs
}

// ---------------------------------------------------------------------------
// Type validation
// ---------------------------------------------------------------------------

var validStepTypes = map[schema.StepType]bool{
	schema.StepTool:      true,
	schema.StepManual:    true,
	schema.StepAssert:    true,
	schema.StepBranch:    true,
	schema.StepParallel:  true,
	schema.StepEnd:       true,
	schema.StepExtension: true,
}

func validateStepType(s schema.Step, path string) []*ValidationError {
	if !validStepTypes[s.Type] {
		return []*ValidationError{errorf("domain", path+".type", "unknown step type %q", s.Type)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Type-specific required fields
// ---------------------------------------------------------------------------

func validateStepFields(s schema.Step, path string) []*ValidationError {
	var errs []*ValidationError
	switch s.Type {
	case schema.StepTool:
		if s.Tool == "" {
			errs = append(errs, errorf("domain", path, "tool step requires 'tool' field"))
		}
	case schema.StepManual:
		if s.Instructions == "" {
			errs = append(errs, errorf("domain", path, "manual step requires 'instructions' field"))
		}
	case schema.StepAssert:
		// validated separately in D14
	case schema.StepBranch:
		if len(s.Branches) == 0 {
			errs = append(errs, errorf("domain", path, "branch step requires at least one branch"))
		}
	case schema.StepParallel:
		if len(s.Branches) < 2 {
			errs = append(errs, errorf("domain", path, "parallel step requires at least two branches"))
		}
	case schema.StepEnd:
		if s.Outcome == nil {
			errs = append(errs, errorf("domain", path, "end step requires 'outcome' field"))
		}
	case schema.StepExtension:
		// contract validated in D17
	}
	return errs
}

// ---------------------------------------------------------------------------
// End-step reachability
// ---------------------------------------------------------------------------

func validateEndReachability(steps []schema.Step, basePath string) []*ValidationError {
	var errs []*ValidationError
	if len(steps) == 0 {
		return errs
	}

	// Check that every linear path through steps reaches an end step.
	// A path can end via: end step, next jump, or branch/parallel with end in all arms.
	hasEnd := stepsReachEnd(steps)
	if !hasEnd {
		errs = append(errs, errorf("domain", basePath, "not every path through the runbook reaches an end step"))
	}

	// Check inside branches
	for i, s := range steps {
		path := fmt.Sprintf("%s[%d]", basePath, i)
		if s.Type == schema.StepBranch || s.Type == schema.StepParallel {
			for j, br := range s.Branches {
				brPath := fmt.Sprintf("%s.branches[%d]", path, j)
				if s.Type == schema.StepBranch {
					// For branch steps, each arm must eventually reach an end
					// (or the steps after the branch must)
					// Only validate if this is the last step (no steps after branch)
					if i == len(steps)-1 && !stepsReachEnd(br.Steps) {
						errs = append(errs, errorf("domain", brPath, "branch arm %q does not reach an end step", br.Label))
					}
				}
				// Recurse into branch steps for nested validation
				errs = append(errs, validateEndReachability(br.Steps, brPath+".steps")...)
			}
		}
	}
	return errs
}

func stepsReachEnd(steps []schema.Step) bool {
	for _, s := range steps {
		if s.Type == schema.StepEnd {
			return true
		}
		if s.Type == schema.StepBranch {
			allArmsEnd := len(s.Branches) > 0
			for _, br := range s.Branches {
				if !stepsReachEnd(br.Steps) {
					allArmsEnd = false
					break
				}
			}
			if allArmsEnd {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// next target scoping
// ---------------------------------------------------------------------------

func validateNextTarget(s schema.Step, scopeSteps []schema.Step, path string) []*ValidationError {
	target, _, _, err := schema.ParseNext(s.Next)
	if err != nil {
		return []*ValidationError{errorf("domain", path+".next", "%s", err.Error())}
	}
	if target == "" {
		return nil
	}

	// Target must exist in the same scope
	found := false
	for _, ss := range scopeSteps {
		if ss.ID == target {
			found = true
			break
		}
	}
	if !found {
		return []*ValidationError{errorf("domain", path+".next", "target %q not found in current scope (next targets must be scope-local)", target)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// next backward bounded
// ---------------------------------------------------------------------------

func validateNextBounded(s schema.Step, scopeSteps []schema.Step, path string) []*ValidationError {
	target, max, _, err := schema.ParseNext(s.Next)
	if err != nil {
		return nil // already reported
	}
	if target == "" {
		return nil
	}

	// Determine if backward by checking if target appears before this step
	isBackward := false
	for _, ss := range scopeSteps {
		if ss.ID == target {
			isBackward = true
			break
		}
		if ss.ID == s.ID {
			break // reached self first — target is forward
		}
	}

	if isBackward && max <= 0 {
		return []*ValidationError{errorf("domain", path+".next", "backward jump to %q requires a 'max' bound to guarantee termination", target)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Variable resolution
// ---------------------------------------------------------------------------

// templateVarRe matches {{ .name }} and {{ .name.field }} patterns.
var templateVarRe = regexp.MustCompile(`\{\{\s*\.(\w+)(?:\.\w+)*\s*\}\}`)

func validateVariableResolution(rb *schema.Runbook, baseDir string) []*ValidationError {
	var errs []*ValidationError
	available := make(map[string]bool)

	// Inputs and constants are available from the start
	for name := range rb.Meta.Inputs {
		available[name] = true
	}
	for name := range rb.Meta.Constants {
		available[name] = true
	}

	// Pre-load tool definitions to know their contract outputs
	toolOutputs := loadToolOutputs(rb, baseDir)

	// Walk steps in order, adding outputs
	errs = append(errs, walkVariableResolution(rb.Steps, "steps", available, toolOutputs)...)

	return errs
}

// loadToolOutputs loads tool definitions and returns a map of tool:action → output names.
func loadToolOutputs(rb *schema.Runbook, baseDir string) map[string][]string {
	outputs := make(map[string][]string)
	for _, toolName := range rb.Tools {
		path := ResolveToolPath(toolName, baseDir, "")
		if path == "" {
			continue
		}
		td, err := schema.LoadToolFile(path)
		if err != nil {
			continue
		}
		// Collect tool-level outputs
		var names []string
		for name := range td.Contract.Outputs {
			names = append(names, name)
		}
		outputs[toolName] = names
		// Also collect per-action outputs
		for actionName, action := range td.Actions {
			if action.Contract != nil {
				for name := range action.Contract.Outputs {
					outputs[toolName+":"+actionName] = append(outputs[toolName+":"+actionName], name)
				}
			}
		}
	}
	return outputs
}

func walkVariableResolution(steps []schema.Step, basePath string, available map[string]bool, toolOutputs map[string][]string) []*ValidationError {
	var errs []*ValidationError

	for i, s := range steps {
		path := fmt.Sprintf("%s[%d]", basePath, i)

		// Check all template references in this step
		refs := collectTemplateRefs(s)
		for _, ref := range refs {
			rootVar := strings.Split(ref, ".")[0]
			if !available[rootVar] {
				errs = append(errs, errorf("domain", path, "variable reference %q does not resolve to a declared input, constant, or prior step output", ref))
			}
		}

		// After this step, its outputs become available
		if s.ID != "" {
			available[s.ID] = true
		}
		// Step contract outputs (inline)
		if s.Contract != nil {
			for name := range s.Contract.Outputs {
				available[name] = true
			}
		}
		// Tool contract outputs (from loaded tool definitions)
		if s.Type == schema.StepTool && s.Tool != "" {
			// Tool-level outputs
			if names, ok := toolOutputs[s.Tool]; ok {
				for _, name := range names {
					available[name] = true
				}
			}
			// Action-specific outputs
			if s.Action != "" {
				if names, ok := toolOutputs[s.Tool+":"+s.Action]; ok {
					for _, name := range names {
						available[name] = true
					}
				}
			}
		}

		// ForEach scoping — the `as` variable is available in template refs
		if s.ForEach != nil && s.ForEach.As != "" {
			available[s.ForEach.As] = true
		}

		// Recurse into branches
		if s.Type == schema.StepBranch || s.Type == schema.StepParallel {
			for j, br := range s.Branches {
				brPath := fmt.Sprintf("%s.branches[%d].steps", path, j)
				// Fork the available set for each branch
				brAvail := copySet(available)
				errs = append(errs, walkVariableResolution(br.Steps, brPath, brAvail, toolOutputs)...)
			}
		}
	}
	return errs
}

func collectTemplateRefs(s schema.Step) []string {
	var refs []string
	// Collect from all string fields that may contain templates
	refs = append(refs, extractRefs(s.When)...)
	refs = append(refs, extractRefs(s.Instructions)...)
	for _, v := range s.Inputs {
		if str, ok := v.(string); ok {
			refs = append(refs, extractRefs(str)...)
		}
	}
	if s.Outcome != nil {
		refs = append(refs, extractRefs(s.Outcome.Code)...)
		for _, v := range s.Outcome.Meta {
			if str, ok := v.(string); ok {
				refs = append(refs, extractRefs(str)...)
			}
		}
	}
	for _, a := range s.Assert {
		refs = append(refs, extractRefs(a.Value)...)
		refs = append(refs, extractRefs(a.Expected)...)
		refs = append(refs, extractRefs(a.Pattern)...)
	}
	for _, br := range s.Branches {
		refs = append(refs, extractRefs(br.Condition)...)
	}
	if s.ForEach != nil {
		refs = append(refs, extractRefs(s.ForEach.Over)...)
	}
	// next target — can reference templates in max
	if m, ok := s.Next.(map[string]any); ok {
		if ms, ok := m["max"].(string); ok {
			refs = append(refs, extractRefs(ms)...)
		}
	}
	return refs
}

func extractRefs(s string) []string {
	matches := templateVarRe.FindAllStringSubmatch(s, -1)
	var refs []string
	for _, m := range matches {
		refs = append(refs, m[1])
	}
	return refs
}

func copySet(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Constant immutability
// ---------------------------------------------------------------------------

func validateConstantImmutability(rb *schema.Runbook) []*ValidationError {
	var errs []*ValidationError
	if len(rb.Meta.Constants) == 0 {
		return errs
	}

	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		// Check if step ID shadows a constant
		if s.ID != "" {
			if _, ok := rb.Meta.Constants[s.ID]; ok {
				errs = append(errs, errorf("domain", path+".id", "step ID %q shadows a constant — constants are immutable", s.ID))
			}
		}
		// Check if contract outputs shadow constants
		if s.Contract != nil {
			for name := range s.Contract.Outputs {
				if _, ok := rb.Meta.Constants[name]; ok {
					errs = append(errs, errorf("domain", path+".contract.outputs."+name, "output %q shadows a constant — constants are immutable", name))
				}
			}
		}
	})
	return errs
}

// ---------------------------------------------------------------------------
// Parallel output uniqueness
// ---------------------------------------------------------------------------

func validateParallelOutputs(s schema.Step, path string) []*ValidationError {
	var errs []*ValidationError
	seen := map[string]int{} // output name → first branch index

	for i, br := range s.Branches {
		for _, step := range br.Steps {
			if step.Contract != nil {
				for name := range step.Contract.Outputs {
					if prevBranch, ok := seen[name]; ok && prevBranch != i {
						errs = append(errs, errorf("domain", fmt.Sprintf("%s.branches[%d]", path, i),
							"output %q is also declared in branch %d — parallel branches must have unique output names", name, prevBranch))
					} else {
						seen[name] = i
					}
				}
			}
			// Also check step IDs as implicit outputs
			if step.ID != "" {
				if prevBranch, ok := seen[step.ID]; ok && prevBranch != i {
					errs = append(errs, errorf("domain", fmt.Sprintf("%s.branches[%d]", path, i),
						"step ID %q is also used in branch %d — parallel branches must have unique outputs", step.ID, prevBranch))
				} else {
					seen[step.ID] = i
				}
			}
		}
	}
	return errs
}

// ---------------------------------------------------------------------------
// Parallel contract conflict detection
// ---------------------------------------------------------------------------

func validateParallelConflicts(s schema.Step, path string) []*ValidationError {
	var errs []*ValidationError

	type branchContract struct {
		index    int
		label    string
		contract contract.Contract
	}

	var brContracts []branchContract
	for i, br := range s.Branches {
		// Aggregate reads/writes from all steps in this branch
		var allReads, allWrites []string
		walkSteps(br.Steps, "", func(step schema.Step, _ string) {
			if step.Contract != nil {
				allReads = append(allReads, step.Contract.Reads...)
				allWrites = append(allWrites, step.Contract.Writes...)
			}
		})
		brContracts = append(brContracts, branchContract{
			index: i,
			label: br.Label,
			contract: contract.Contract{
				Reads:  allReads,
				Writes: allWrites,
			},
		})
	}

	// Check all pairs
	for i := 0; i < len(brContracts); i++ {
		for j := i + 1; j < len(brContracts); j++ {
			if contract.HasConflict(&brContracts[i].contract, &brContracts[j].contract) {
				errs = append(errs, warningf("domain",
					fmt.Sprintf("%s.branches[%d,%d]", path, i, j),
					"branches %q and %q have reads/writes conflicts — they will be serialized at runtime",
					brContracts[i].label, brContracts[j].label))
			}
		}
	}
	return errs
}

// ---------------------------------------------------------------------------
// Outcome category
// ---------------------------------------------------------------------------

var validOutcomeCategories = map[schema.OutcomeCategory]bool{
	schema.OutcomeResolved:  true,
	schema.OutcomeEscalated: true,
	schema.OutcomeNoAction:  true,
	schema.OutcomeNeedsRCA:  true,
}

func validateOutcomeCategory(s schema.Step, path string) []*ValidationError {
	if !validOutcomeCategories[s.Outcome.Category] {
		return []*ValidationError{errorf("domain", path+".outcome.category",
			"invalid outcome category %q: must be resolved, escalated, no_action, or needs_rca", s.Outcome.Category)}
	}
	if s.Outcome.Code == "" {
		return []*ValidationError{errorf("domain", path+".outcome.code", "outcome code is required")}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Branch conditions
// ---------------------------------------------------------------------------

func validateBranchConditions(s schema.Step, path string) []*ValidationError {
	var errs []*ValidationError
	hasDefault := false
	for i, br := range s.Branches {
		brPath := fmt.Sprintf("%s.branches[%d]", path, i)
		if br.Condition == "" {
			errs = append(errs, errorf("domain", brPath, "branch must have a condition"))
		}
		if br.Condition == "default" {
			hasDefault = true
		}
		if len(br.Steps) == 0 {
			errs = append(errs, errorf("domain", brPath, "branch must have at least one step"))
		}
	}
	if !hasDefault {
		errs = append(errs, warningf("domain", path, "branch has no 'default' condition — not all inputs may be handled"))
	}
	return errs
}

// ---------------------------------------------------------------------------
// ForEach
// ---------------------------------------------------------------------------

func validateForEach(s schema.Step, path string) []*ValidationError {
	var errs []*ValidationError
	if s.ForEach.As == "" {
		errs = append(errs, errorf("domain", path+".for_each.as", "for_each requires 'as' field"))
	}
	if s.ForEach.Over == "" {
		errs = append(errs, errorf("domain", path+".for_each.over", "for_each requires 'over' field"))
	}
	return errs
}

// ---------------------------------------------------------------------------
// Contract tightening
// ---------------------------------------------------------------------------

func validateContractTightening(rb *schema.Runbook, baseDir string) []*ValidationError {
	var errs []*ValidationError

	// Load tool definitions and validate tightening for tool steps with inline contracts
	walkSteps(rb.Steps, "steps", func(s schema.Step, path string) {
		if s.Type != schema.StepTool || s.Contract == nil || s.Tool == "" {
			return
		}

		// Try to resolve the tool definition
		toolPath := ResolveToolPath(s.Tool, baseDir, "")
		if toolPath == "" {
			return // tool not found — separate validation concern
		}

		td, err := schema.LoadToolFile(toolPath)
		if err != nil {
			return // tool load error — separate validation concern
		}

		// Get the parent contract (tool-level, or action-level if action specified)
		parent := &td.Contract
		if s.Action != "" {
			if action, ok := td.Actions[s.Action]; ok && action.Contract != nil {
				merged := contract.Merge(&td.Contract, action.Contract)
				parent = &merged
			}
		}

		// Validate tightening
		violations := contract.CanTighten(parent, s.Contract)
		for _, v := range violations {
			errs = append(errs, errorf("domain", path+".contract", "contract tightening violation: %s", v))
		}
	})

	return errs
}

// ---------------------------------------------------------------------------
// inputs_from
// ---------------------------------------------------------------------------

func validateInputsFrom(s schema.Step, rb *schema.Runbook, path string) []*ValidationError {
	var errs []*ValidationError

	sources := normalizeInputsFrom(s.InputsFrom)
	for _, src := range sources {
		// The source must be a constant (object-valued) or a step ID
		if _, ok := rb.Meta.Constants[src]; ok {
			// Check it's an object
			if _, isMap := rb.Meta.Constants[src].(map[string]any); !isMap {
				errs = append(errs, errorf("domain", path+".inputs_from", "inputs_from source %q must be an object (map), not a scalar", src))
			}
		}
		// If it's a step ID, we can't validate at this level — it might be a prior step output
		// Variable resolution (D8) will catch undefined refs
	}
	return errs
}

func normalizeInputsFrom(raw any) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	}
	return nil
}

// ---------------------------------------------------------------------------
// Evidence requirements
// ---------------------------------------------------------------------------

func validateEvidence(s schema.Step, path string) []*ValidationError {
	var errs []*ValidationError
	names := make(map[string]bool)
	for i, ev := range s.RequiredEvidence {
		evPath := fmt.Sprintf("%s.required_evidence[%d]", path, i)
		if ev.Kind == "" {
			errs = append(errs, errorf("domain", evPath+".kind", "evidence kind is required"))
		} else {
			switch ev.Kind {
			case "text", "checklist", "attachment":
			default:
				errs = append(errs, errorf("domain", evPath+".kind", "unknown evidence kind %q: must be text, checklist, or attachment", ev.Kind))
			}
		}
		if ev.Name == "" {
			errs = append(errs, errorf("domain", evPath+".name", "evidence name is required"))
		} else if names[ev.Name] {
			errs = append(errs, errorf("domain", evPath+".name", "duplicate evidence name %q within step", ev.Name))
		} else {
			names[ev.Name] = true
		}
		if ev.Kind == "checklist" && len(ev.Items) == 0 {
			errs = append(errs, errorf("domain", evPath, "checklist evidence must have items"))
		}
	}
	return errs
}

// ---------------------------------------------------------------------------
// Tool definition validation
// ---------------------------------------------------------------------------

func validateToolDomain(td *schema.ToolDefinition) []*ValidationError {
	var errs []*ValidationError

	if td.APIVersion != schema.APIVersionTool {
		errs = append(errs, errorf("domain", "apiVersion", "expected %q, got %q", schema.APIVersionTool, td.APIVersion))
	}
	if td.Meta.Name == "" {
		errs = append(errs, errorf("domain", "meta.name", "tool name is required"))
	}
	if len(td.Actions) == 0 {
		errs = append(errs, errorf("domain", "actions", "at least one action is required"))
	}

	// Effects / side_effects consistency
	errs = append(errs, validateToolEffects(td)...)

	// Validate platform constraint
	if len(td.Meta.Platform) > 0 {
		validPlatforms := map[string]bool{"linux": true, "darwin": true, "windows": true, "freebsd": true, "openbsd": true}
		for _, p := range td.Meta.Platform {
			if !validPlatforms[p] {
				errs = append(errs, warningf("domain", "meta.platform", "unknown platform %q", p))
			}
		}
		if !platformMatches(td.Meta.Platform) {
			errs = append(errs, warningf("domain", "meta.platform",
				"tool %q declares platform %v but current OS is %q", td.Meta.Name, td.Meta.Platform, runtime.GOOS))
		}
	}

	// Validate transport
	switch td.Meta.Transport {
	case "", "stdio":
		// stdio is default
	case "jsonrpc", "mcp":
		// valid
	default:
		errs = append(errs, errorf("domain", "meta.transport", "unknown transport %q: must be stdio, jsonrpc, or mcp", td.Meta.Transport))
	}

	// Validate per-action
	for name, action := range td.Actions {
		aPath := fmt.Sprintf("actions.%s", name)
		transport := td.Meta.Transport
		if transport == "" {
			transport = "stdio"
		}
		switch transport {
		case "stdio":
			if len(action.Argv) == 0 {
				errs = append(errs, errorf("domain", aPath, "stdio action requires 'argv'"))
			}
		case "jsonrpc":
			if action.Method == "" {
				errs = append(errs, errorf("domain", aPath, "jsonrpc action requires 'method'"))
			}
		case "mcp":
			if action.MCPTool == "" {
				errs = append(errs, errorf("domain", aPath, "mcp action requires 'mcp_tool'"))
			}
		}

		// Validate action contract tightening against tool-level
		if action.Contract != nil {
			violations := contract.CanTighten(&td.Contract, action.Contract)
			for _, v := range violations {
				errs = append(errs, errorf("domain", aPath+".contract", "action contract tightening violation: %s", v))
			}
		}

		// Validate extract references contract outputs
		for eName := range action.Extract {
			if td.Contract.Outputs != nil {
				if _, ok := td.Contract.Outputs[eName]; !ok {
					// Also check action-level outputs
					if action.Contract == nil || action.Contract.Outputs == nil {
						errs = append(errs, errorf("domain", aPath+".extract."+eName, "extract key %q is not declared in contract outputs", eName))
					} else if _, ok := action.Contract.Outputs[eName]; !ok {
						errs = append(errs, errorf("domain", aPath+".extract."+eName, "extract key %q is not declared in contract outputs", eName))
					}
				}
			}
		}
	}

	return errs
}

// ---------------------------------------------------------------------------
// Step graph walker
// ---------------------------------------------------------------------------

// walkSteps recursively visits all steps in the step graph.
func walkSteps(steps []schema.Step, basePath string, fn func(schema.Step, string)) {
	for i, s := range steps {
		path := fmt.Sprintf("%s[%d]", basePath, i)
		fn(s, path)
		// Recurse into branches (branch + parallel)
		for j, br := range s.Branches {
			brPath := fmt.Sprintf("%s.branches[%d].steps", path, j)
			walkSteps(br.Steps, brPath, fn)
		}
	}
}

// platformMatches returns true if the current GOOS is in the platform list.
func platformMatches(platforms []string) bool {
	for _, p := range platforms {
		if p == runtime.GOOS {
			return true
		}
	}
	return false
}
