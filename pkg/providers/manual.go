package providers

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// InteractiveCollector prompts the user via CLI for evidence collection.
type InteractiveCollector struct {
	reader *bufio.Reader
}

// NewInteractiveCollector creates an evidence collector that reads from stdin.
func NewInteractiveCollector() *InteractiveCollector {
	return &InteractiveCollector{
		reader: bufio.NewReader(os.Stdin),
	}
}

func (ic *InteractiveCollector) PromptText(name string, instructions string) (string, error) {
	fmt.Printf("\n📝 Evidence required: %s\n", name)
	if instructions != "" {
		fmt.Printf("   %s\n", instructions)
	}
	fmt.Print("   Enter text: ")
	text, err := ic.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read text evidence: %w", err)
	}
	return strings.TrimSpace(text), nil
}

func (ic *InteractiveCollector) PromptChecklist(name string, items []string) (map[string]bool, error) {
	fmt.Printf("\n☑️  Checklist: %s\n", name)
	result := make(map[string]bool)
	for _, item := range items {
		fmt.Printf("   [ ] %s — complete? (y/n): ", item)
		answer, err := ic.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read checklist: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		result[item] = answer == "y" || answer == "yes"
	}
	return result, nil
}

func (ic *InteractiveCollector) PromptAttachment(name string, instructions string) (*AttachmentInfo, error) {
	fmt.Printf("\n📎 Attachment required: %s\n", name)
	if instructions != "" {
		fmt.Printf("   %s\n", instructions)
	}
	fmt.Print("   Enter file path: ")
	path, err := ic.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read attachment path: %w", err)
	}
	path = strings.TrimSpace(path)

	// Verify file exists
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("attachment file: %w", err)
	}

	return &AttachmentInfo{
		Path: path,
		Size: info.Size(),
		// SHA256 will be computed by the evidence package
	}, nil
}

func (ic *InteractiveCollector) PromptApproval(roles []string, min int) ([]Approval, error) {
	fmt.Printf("\n✅ Approval required (min %d from roles: %s)\n", min, strings.Join(roles, ", "))
	var approvals []Approval
	for i := 0; i < min; i++ {
		fmt.Printf("   Approver %d identity (--as value): ", i+1)
		actor, err := ic.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read approval: %w", err)
		}
		actor = strings.TrimSpace(actor)
		fmt.Printf("   Approver %d role: ", i+1)
		role, err := ic.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read approval role: %w", err)
		}
		role = strings.TrimSpace(role)
		approvals = append(approvals, Approval{
			Actor: actor,
			Role:  role,
		})
	}
	return approvals, nil
}

// DryRunCollector returns placeholder values without prompting.
type DryRunCollector struct{}

func (d *DryRunCollector) PromptText(name string, instructions string) (string, error) {
	return fmt.Sprintf("<dry-run: %s>", name), nil
}

func (d *DryRunCollector) PromptChecklist(name string, items []string) (map[string]bool, error) {
	result := make(map[string]bool)
	for _, item := range items {
		result[item] = true // assume all checked in dry-run
	}
	return result, nil
}

func (d *DryRunCollector) PromptAttachment(name string, instructions string) (*AttachmentInfo, error) {
	return &AttachmentInfo{
		Path: fmt.Sprintf("<dry-run: %s>", name),
	}, nil
}

func (d *DryRunCollector) PromptApproval(roles []string, min int) ([]Approval, error) {
	var approvals []Approval
	for i := 0; i < min; i++ {
		approvals = append(approvals, Approval{
			Actor: "dry-run",
			Role:  roles[i%len(roles)],
		})
	}
	return approvals, nil
}

// ScenarioCollector returns pre-recorded evidence from a scenario file.
// Used in replay mode to provide deterministic evidence for manual steps.
type ScenarioCollector struct {
	// StepEvidence maps step_id → evidence_name → EvidenceValue
	StepEvidence map[string]map[string]*EvidenceValue
	// CurrentStepID is set by the engine before collecting evidence.
	CurrentStepID string
}

// NewScenarioCollector creates a collector from pre-recorded evidence.
func NewScenarioCollector(evidence map[string]map[string]*EvidenceValue) *ScenarioCollector {
	return &ScenarioCollector{
		StepEvidence: evidence,
	}
}

func (sc *ScenarioCollector) PromptText(name string, instructions string) (string, error) {
	ev, err := sc.getEvidence(name)
	if err != nil {
		return "", err
	}
	if ev.Kind != "text" {
		return "", fmt.Errorf("scenario evidence %q for step %q has kind %q, expected text", name, sc.CurrentStepID, ev.Kind)
	}
	return ev.Value, nil
}

func (sc *ScenarioCollector) PromptChecklist(name string, items []string) (map[string]bool, error) {
	ev, err := sc.getEvidence(name)
	if err != nil {
		return nil, err
	}
	if ev.Kind != "checklist" {
		return nil, fmt.Errorf("scenario evidence %q for step %q has kind %q, expected checklist", name, sc.CurrentStepID, ev.Kind)
	}
	return ev.Items, nil
}

func (sc *ScenarioCollector) PromptAttachment(name string, instructions string) (*AttachmentInfo, error) {
	ev, err := sc.getEvidence(name)
	if err != nil {
		return nil, err
	}
	if ev.Kind != "attachment" {
		return nil, fmt.Errorf("scenario evidence %q for step %q has kind %q, expected attachment", name, sc.CurrentStepID, ev.Kind)
	}
	return &AttachmentInfo{
		Path:   ev.Path,
		SHA256: ev.SHA256,
		Size:   ev.Size,
	}, nil
}

func (sc *ScenarioCollector) PromptApproval(roles []string, min int) ([]Approval, error) {
	var approvals []Approval
	for i := 0; i < min; i++ {
		approvals = append(approvals, Approval{
			Actor: "scenario",
			Role:  roles[i%len(roles)],
		})
	}
	return approvals, nil
}

// getEvidence retrieves a named evidence value for the current step.
func (sc *ScenarioCollector) getEvidence(name string) (*EvidenceValue, error) {
	stepEv, ok := sc.StepEvidence[sc.CurrentStepID]
	if !ok {
		return nil, fmt.Errorf("scenario has no evidence for step %q", sc.CurrentStepID)
	}
	ev, ok := stepEv[name]
	if !ok {
		return nil, fmt.Errorf("scenario has no evidence %q for step %q", name, sc.CurrentStepID)
	}
	return ev, nil
}
