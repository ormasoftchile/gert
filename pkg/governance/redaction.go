package governance

import (
	"regexp"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// CompiledRedaction is a pre-compiled redaction rule.
type CompiledRedaction struct {
	Pattern *regexp.Regexp
	Replace string
}

// CompileRedactionRules compiles redaction rules from the governance policy.
func CompileRedactionRules(rules []schema.RedactionRule) ([]*CompiledRedaction, error) {
	var compiled []*CompiledRedaction
	for _, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, &CompiledRedaction{
			Pattern: re,
			Replace: r.Replace,
		})
	}
	return compiled, nil
}

// RedactOutput applies all compiled redaction rules to the given output.
func RedactOutput(output string, rules []*CompiledRedaction) string {
	result := output
	for _, r := range rules {
		result = r.Pattern.ReplaceAllString(result, r.Replace)
	}
	return result
}
