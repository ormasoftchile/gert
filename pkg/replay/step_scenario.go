package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// iso8601Pattern matches ISO 8601 timestamps in JSON string values.
var iso8601Pattern = regexp.MustCompile(`"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z?"`)

// TimeRebaser adjusts timestamps in JSON data relative to a new reference time.
// All timestamps are stored as offsets from the original reference time (e.g. impact start).
// At replay time, offsets are applied to the new reference time to produce fresh-looking data.
type TimeRebaser struct {
	OriginalRef time.Time // the original reference time (e.g. impact_start_time)
	ReplayRef   time.Time // the replay reference time (e.g. now)
}

// NewTimeRebaser creates a rebaser from an original reference time.
// The replay reference defaults to time.Now().
func NewTimeRebaser(originalRef time.Time) *TimeRebaser {
	return &TimeRebaser{
		OriginalRef: originalRef,
		ReplayRef:   time.Now().UTC(),
	}
}

// RebaseJSON takes raw JSON bytes and replaces all ISO 8601 timestamps
// by shifting them from the original time frame to the replay time frame.
func (r *TimeRebaser) RebaseJSON(data []byte) ([]byte, error) {
	result := iso8601Pattern.ReplaceAllFunc(data, func(match []byte) []byte {
		tsStr := string(match[1 : len(match)-1])
		parsed, err := parseFlexibleTimestamp(tsStr)
		if err != nil {
			return match
		}
		offset := parsed.Sub(r.OriginalRef)
		rebased := r.ReplayRef.Add(offset)
		formatted := formatMatchingPrecision(rebased, tsStr)
		return []byte(`"` + formatted + `"`)
	})
	return result, nil
}

// parseFlexibleTimestamp parses timestamps with varying precision.
func parseFlexibleTimestamp(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02T15:04:05.9999999Z",
		"2006-01-02T15:04:05.999999Z",
		"2006-01-02T15:04:05.99999Z",
		"2006-01-02T15:04:05.9999Z",
		"2006-01-02T15:04:05.999Z",
		"2006-01-02T15:04:05.99Z",
		"2006-01-02T15:04:05.9Z",
		"2006-01-02T15:04:05Z",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp %q", s)
}

// formatMatchingPrecision formats a timestamp with similar sub-second precision as the original.
func formatMatchingPrecision(t time.Time, original string) string {
	dotIdx := strings.Index(original, ".")
	if dotIdx < 0 {
		return t.Format("2006-01-02T15:04:05Z")
	}
	zIdx := strings.Index(original[dotIdx:], "Z")
	if zIdx < 0 {
		zIdx = len(original[dotIdx:])
	}
	fracLen := zIdx - 1

	switch {
	case fracLen >= 7:
		return t.Format("2006-01-02T15:04:05.0000000Z")
	case fracLen >= 3:
		format := "2006-01-02T15:04:05." + strings.Repeat("0", fracLen) + "Z"
		return t.Format(format)
	default:
		return t.Format("2006-01-02T15:04:05.0Z")
	}
}

// StepScenario extends the base Scenario with per-step responses loaded from a folder.
// Used for replay of tool steps, where each step's JSON response is pre-recorded.
type StepScenario struct {
	*Scenario
	StepResponses map[string]json.RawMessage // step_id → raw JSON response
	Rebaser       *TimeRebaser               // nil if no time rebasing
}

// LoadStepScenario loads a scenario folder containing:
//   - scenario.yaml (manifest with inputs and step mappings)
//   - steps/*.json (step responses keyed by filename prefix = step order)
//
// If referenceTime is non-zero, timestamps in step responses are rebased.
func LoadStepScenario(scenarioDir string, referenceTime time.Time) (*StepScenario, error) {
	var base *Scenario
	scenarioFile := filepath.Join(scenarioDir, "scenario.yaml")
	if data, err := os.ReadFile(scenarioFile); err == nil {
		base, _ = ParseScenario(data)
	}
	if base == nil {
		base = &Scenario{}
	}

	stepResponses := make(map[string]json.RawMessage)
	stepsDir := filepath.Join(scenarioDir, "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &StepScenario{
				Scenario:      base,
				StepResponses: stepResponses,
			}, nil
		}
		return nil, fmt.Errorf("read steps directory %q: %w", stepsDir, err)
	}

	var rebaser *TimeRebaser
	if !referenceTime.IsZero() {
		rebaser = NewTimeRebaser(referenceTime)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(stepsDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read step response %q: %w", entry.Name(), err)
		}
		if rebaser != nil {
			rebased, err := rebaser.RebaseJSON(data)
			if err != nil {
				return nil, fmt.Errorf("rebase timestamps in %q: %w", entry.Name(), err)
			}
			data = rebased
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		stepResponses[key] = json.RawMessage(data)
	}

	return &StepScenario{
		Scenario:      base,
		StepResponses: stepResponses,
		Rebaser:       rebaser,
	}, nil
}

// FindStepResponse looks up a step response by step ID.
// It matches against filenames using a suffix match (the filename prefix is the order number).
// E.g., step_id "check_login_failures_kusto" matches "001-check-login-failures-kusto".
func (s *StepScenario) FindStepResponse(stepID string) (json.RawMessage, bool) {
	if resp, ok := s.StepResponses[stepID]; ok {
		return resp, true
	}
	normalizedID := strings.ReplaceAll(stepID, "_", "-")
	for key, resp := range s.StepResponses {
		normalizedKey := strings.ReplaceAll(key, "_", "-")
		if strings.HasSuffix(normalizedKey, normalizedID) {
			return resp, true
		}
	}
	return nil, false
}
