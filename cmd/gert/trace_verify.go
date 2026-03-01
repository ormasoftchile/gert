package main

import (
	"fmt"

	"github.com/ormasoftchile/gert/pkg/kernel/trace"
	"github.com/spf13/cobra"
)

var traceVerifyCmd = &cobra.Command{
	Use:   "verify [trace.jsonl]",
	Short: "Verify trace file integrity (hash chain + signature)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTraceVerify,
}

func runTraceVerify(cmd *cobra.Command, args []string) error {
	path := args[0]

	result, err := trace.VerifyFile(path)
	if err != nil {
		return err
	}

	if !result.Valid {
		fmt.Printf("✗ Chain broken at event %d\n", result.BrokenAt)
		if result.Error != "" {
			fmt.Printf("  %s\n", result.Error)
		}
		return fmt.Errorf("chain verification failed")
	}

	fmt.Printf("✓ Chain integrity: %d events, no breaks\n", result.EventCount)

	if result.ChainHash != "" {
		if result.SignatureOK {
			keyLabel := result.SigningKeyID
			if keyLabel == "" {
				keyLabel = "(default)"
			}
			fmt.Printf("✓ Signature valid: signed by key %q\n", keyLabel)
		} else if result.SignatureNoKey {
			keyLabel := result.SigningKeyID
			if keyLabel == "" {
				keyLabel = "unknown"
			}
			fmt.Printf("\u26a0 Signature present (key %q) but no GERT_TRACE_SIGNING_KEY set to verify\n", keyLabel)
		} else if result.SigningKeyID != "" {
			fmt.Printf("✗ Signature invalid\n")
			return fmt.Errorf("signature verification failed")
		}
	}

	return nil
}

func init() {
	// Add verify as subcommand of trace (which is a subcommand of schema's parent)
	traceCmd := &cobra.Command{
		Use:   "trace",
		Short: "Trace file operations",
	}
	traceCmd.AddCommand(traceVerifyCmd)
	rootCmd.AddCommand(traceCmd)
}
