package replay

import (
	"context"
	"fmt"
	"strings"

	"github.com/ormasoftchile/gert/pkg/providers"
)

// ReplayExecutor implements CommandExecutor by matching commands against
// pre-recorded scenario entries. Fail-closed: returns an error if no match.
type ReplayExecutor struct {
	scenario *Scenario
	used     []bool // track which commands have been used
}

// NewReplayExecutor creates a ReplayExecutor from a loaded scenario.
func NewReplayExecutor(s *Scenario) *ReplayExecutor {
	return &ReplayExecutor{
		scenario: s,
		used:     make([]bool, len(s.Commands)),
	}
}

// Execute matches the command+args against scenario entries and returns
// the pre-recorded response. Returns an error if no matching entry is found.
func (r *ReplayExecutor) Execute(ctx context.Context, command string, args []string, env []string) (*providers.CommandResult, error) {
	// Build the full argv for matching
	fullArgv := append([]string{command}, args...)

	for i, sc := range r.scenario.Commands {
		if r.used[i] {
			continue
		}
		if argvMatch(fullArgv, sc.Argv) {
			r.used[i] = true
			return &providers.CommandResult{
				Stdout:   []byte(sc.Stdout),
				Stderr:   []byte(sc.Stderr),
				ExitCode: sc.ExitCode,
			}, nil
		}
	}

	return nil, fmt.Errorf("replay: no matching scenario entry for command: %s", strings.Join(fullArgv, " "))
}

// argvMatch returns true if the two argv slices are identical.
func argvMatch(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range actual {
		if actual[i] != expected[i] {
			return false
		}
	}
	return true
}
