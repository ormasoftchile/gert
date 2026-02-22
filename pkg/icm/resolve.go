package icm

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// ResolveInputs resolves input bindings against an ICM incident.
// It handles from: icm.* paths and also attempts best-effort title extraction
// for any input with a from: icm.* binding.
//
// Supported icm.* paths:
//   - icm.title                     → incident title (may apply pattern for extraction)
//   - icm.impactStartTime           → incident impact start date (UTC)
//   - icm.severity                  → incident severity
//   - icm.routingId                 → routing ID
//   - icm.correlationId             → correlation ID
//   - icm.occuringLocation.instance → environment/instance (from CorrelationId prefix or OccuringLocation)
//   - icm.location.Region           → classified region (from OccuringDatacenter)
//   - icm.customFields.<Name>       → custom field value by name
func ResolveInputs(inputs map[string]*schema.InputDef, inc *Incident) map[string]string {
	resolved := make(map[string]string)

	// Pre-extract well-known fields from the title so they're available
	// even for inputs that don't explicitly bind to icm.title.
	titleExtracted := ExtractTitleFields(inc.Title)

	for name, input := range inputs {
		if !strings.HasPrefix(input.From, "icm.") {
			// For non-icm inputs (e.g. from: prompt), try title extraction as best-effort.
			// Convert input name from snake_case to PascalCase and check title patterns.
			key := SnakeToPascal(name)
			if v, ok := titleExtracted[key]; ok {
				resolved[name] = v
			}
			continue
		}

		var value string
		from := input.From

		switch {
		case from == "icm.title":
			value = inc.Title
			// If a pattern is specified, extract via regex
			if input.Pattern != "" && value != "" {
				re, err := regexp.Compile(input.Pattern)
				if err == nil {
					m := re.FindStringSubmatch(value)
					if len(m) > 1 {
						value = m[1] // first capture group
					}
				}
			}

		case from == "icm.impactStartTime":
			if inc.ImpactStartDate != nil {
				value = inc.ImpactStartDate.UTC().Format(time.RFC3339)
			}

		case from == "icm.severity":
			value = fmt.Sprintf("%d", inc.Severity)

		case from == "icm.routingId":
			value = inc.RoutingId

		case from == "icm.correlationId":
			value = inc.CorrelationId

		case from == "icm.occuringLocation.instance":
			// Primary: OccuringLocation.Instance (ServiceInstanceId)
			if inc.OccuringLocation != nil && inc.OccuringLocation.Instance != "" {
				value = inc.OccuringLocation.Instance
			} else if inc.CorrelationId != "" {
				// Fallback: extract from CorrelationId (format: "ProdEus1a/AIMS://...")
				parts := strings.SplitN(inc.CorrelationId, "/", 2)
				if len(parts) > 0 && parts[0] != "" {
					value = parts[0]
				}
			}

		case from == "icm.location.Region":
			// OccuringDatacenter is the human-readable region (e.g. "East US")
			value = inc.OccuringDatacenter

		case strings.HasPrefix(from, "icm.customFields."):
			fieldName := strings.TrimPrefix(from, "icm.customFields.")
			if inc.Fields != nil {
				value = inc.Fields[fieldName]
			}

		default:
			// Unknown icm.* path — check if the field name matches a title-extracted key
			key := strings.TrimPrefix(from, "icm.")
			if v, ok := titleExtracted[key]; ok {
				value = v
			} else {
				fmt.Fprintf(os.Stderr, "icm: unknown input binding: %s\n", from)
			}
		}

		if value != "" {
			resolved[name] = value
		}
	}

	return resolved
}

// TitleFieldPatterns are well-known key:value patterns found in Azure SQL DB ICM titles.
// Format: [Key: Value] or [Key:Value] bracketed in the title string.
var TitleFieldPatterns = []struct {
	Key     string
	Pattern *regexp.Regexp
}{
	{"LoginFailureCause", regexp.MustCompile(`LoginFailureCause:\s*(\w+)`)},
	{"SLO", regexp.MustCompile(`SLO:\s*([\w_]+)`)},
	{"Sev", regexp.MustCompile(`Sev-(\d+)`)},
	{"ClusterName", regexp.MustCompile(`Sterling\s+(\w+):`)},
}

// ExtractTitleFields parses well-known key:value pairs from an ICM title.
func ExtractTitleFields(title string) map[string]string {
	result := make(map[string]string)
	for _, p := range TitleFieldPatterns {
		m := p.Pattern.FindStringSubmatch(title)
		if len(m) > 1 {
			result[p.Key] = m[1]
		}
	}
	return result
}

// SnakeToPascal converts a snake_case name to PascalCase.
// Example: "login_failure_cause" → "LoginFailureCause"
func SnakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}
