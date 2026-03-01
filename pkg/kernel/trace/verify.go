package trace

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// VerifyResult is the outcome of verifying a trace file.
type VerifyResult struct {
	EventCount     int
	Valid          bool
	BrokenAt       int // -1 if no break
	SignatureOK    bool
	SignatureNoKey bool // signature present but no key to verify
	SigningKeyID   string
	ChainHash      string
	Error          string
}

// VerifyFile verifies the hash chain and optional signature of a trace file.
func VerifyFile(path string) (*VerifyResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	defer f.Close()
	return Verify(f)
}

// Verify checks hash chain integrity and optional HMAC signature.
func Verify(r io.Reader) (*VerifyResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max line

	genesis := strings.Repeat("0", 64)
	expectedPrevHash := genesis
	count := 0
	var lastEvent Event
	var lastJSON []byte

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		count++

		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			return &VerifyResult{
				EventCount: count,
				Valid:      false,
				BrokenAt:   count,
				Error:      fmt.Sprintf("event %d: invalid JSON: %v", count, err),
			}, nil
		}

		// Check prev_hash
		if evt.PrevHash != expectedPrevHash {
			return &VerifyResult{
				EventCount: count,
				Valid:      false,
				BrokenAt:   count,
				Error:      fmt.Sprintf("event %d: prev_hash mismatch (expected %s, got %s)", count, expectedPrevHash[:16]+"...", evt.PrevHash[:16]+"..."),
			}, nil
		}

		// Compute hash of this event's JSON for next comparison
		h := sha256.Sum256(line)
		expectedPrevHash = hex.EncodeToString(h[:])

		lastEvent = evt
		lastJSON = make([]byte, len(line))
		copy(lastJSON, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read trace: %w", err)
	}

	result := &VerifyResult{
		EventCount: count,
		Valid:      true,
		BrokenAt:   -1,
	}

	// Check signature on run_complete event
	if lastEvent.Type == EventRunComplete && lastEvent.Data != nil {
		if chainHash, ok := lastEvent.Data["chain_hash"].(string); ok {
			result.ChainHash = chainHash
		}
		if sig, ok := lastEvent.Data["signature"].(string); ok {
			keyID, _ := lastEvent.Data["signing_key_id"].(string)
			result.SigningKeyID = keyID

			// Look up signing key
			envName := "GERT_TRACE_SIGNING_KEY"
			sigKey := os.Getenv(envName)
			if sigKey == "" {
				result.SignatureNoKey = true
			} else if result.ChainHash != "" {
				mac := hmac.New(sha256.New, []byte(sigKey))
				mac.Write([]byte(result.ChainHash))
				expectedSig := hex.EncodeToString(mac.Sum(nil))
				result.SignatureOK = hmac.Equal([]byte(sig), []byte(expectedSig))
			}
		}
	}

	return result, nil
}
