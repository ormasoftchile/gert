// Package evidence implements evidence types and SHA256 hashing.
package evidence

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/ormasoftchile/gert/pkg/providers"
)

// NewTextEvidence creates a text evidence value.
func NewTextEvidence(value string) *providers.EvidenceValue {
	return &providers.EvidenceValue{
		Kind:  "text",
		Value: value,
	}
}

// NewChecklistEvidence creates a checklist evidence value.
func NewChecklistEvidence(items map[string]bool) *providers.EvidenceValue {
	return &providers.EvidenceValue{
		Kind:  "checklist",
		Items: items,
	}
}

// NewAttachmentEvidence creates an attachment evidence value with SHA256 hash.
func NewAttachmentEvidence(path string) (*providers.EvidenceValue, error) {
	hash, size, err := HashFile(path)
	if err != nil {
		return nil, fmt.Errorf("hash attachment: %w", err)
	}
	return &providers.EvidenceValue{
		Kind:   "attachment",
		Path:   path,
		SHA256: hash,
		Size:   size,
	}, nil
}

// HashFile computes SHA256 hash and file size.
func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), size, nil
}
