package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ormasoftchile/gert/pkg/providers"
)

// TraceWriter writes StepResult events to a JSONL trace file.
type TraceWriter struct {
	file   *os.File
	writer *bufio.Writer
	enc    *json.Encoder
}

// NewTraceWriter creates a trace writer that appends to the given file.
func NewTraceWriter(path string) (*TraceWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	w := bufio.NewWriter(f)
	return &TraceWriter{
		file:   f,
		writer: w,
		enc:    json.NewEncoder(w),
	}, nil
}

// Write appends a StepResult as a JSONL event and flushes to disk.
func (tw *TraceWriter) Write(result *providers.StepResult) error {
	event := TraceEvent{
		Type:      "step_result",
		Timestamp: time.Now(),
		RunID:     result.RunID,
		Result:    result,
	}
	if err := tw.enc.Encode(event); err != nil {
		return fmt.Errorf("encode trace event: %w", err)
	}
	// Flush and sync at step boundaries
	if err := tw.writer.Flush(); err != nil {
		return fmt.Errorf("flush trace: %w", err)
	}
	if err := tw.file.Sync(); err != nil {
		return fmt.Errorf("sync trace: %w", err)
	}
	return nil
}

// Close flushes and closes the trace file.
func (tw *TraceWriter) Close() error {
	if err := tw.writer.Flush(); err != nil {
		return err
	}
	return tw.file.Close()
}
