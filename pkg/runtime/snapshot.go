package runtime

import (
	"encoding/json"
	"fmt"
	"os"
)

// SaveSnapshot persists RunState to a JSON file.
func SaveSnapshot(state *RunState, path string) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

// LoadSnapshot reads a RunState from a JSON file.
func LoadSnapshot(path string) (*RunState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return &state, nil
}
