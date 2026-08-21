package lint

import (
	"encoding/json"
	"io"
)

type sarifReport struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}
type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}
type sarifRule struct {
	ID               string `json:"id"`
	ShortDescription struct {
		Text string `json:"text"`
	} `json:"shortDescription"`
	DefaultConfiguration struct {
		Level string `json:"level"`
	} `json:"defaultConfiguration"`
}
type sarifResult struct {
	RuleID     string            `json:"ruleId"`
	Level      string            `json:"level"`
	Message    sarifMessage      `json:"message"`
	Locations  []sarifLocation   `json:"locations,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}
type sarifMessage struct {
	Text string `json:"text"`
}
type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}
type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}
type sarifArtifactLocation struct {
	URI string `json:"uri"`
}
type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
}

var sarifRules = []struct {
	code, description string
	severity          Severity
}{
	{"OKF010", "Malformed, missing, or unreadable concept frontmatter", SeverityError},
	{"OKF011", "Concept frontmatter is not a YAML mapping", SeverityError},
	{"OKF012", "Concept frontmatter lacks a non-empty type", SeverityError},
	{"OKF100", "Reference escapes the bundle or traverses a symlink", SeverityError},
	{"OKF101", "Local reference target does not exist", SeverityWarning},
	{"OKF102", "Reference target is invalid or unsupported", SeverityError},
	{"OKF103", "Source resource could be a non-followable scope descriptor", SeverityInfo},
	{"OKF104", "Local Markdown fragment is unsupported or missing", SeverityWarning},
	{"OKF200", "Remote URL could not be confirmed", SeverityInfo},
	{"OKF201", "Remote URL is permanently unavailable", SeverityWarning},
	{"OKF202", "Remote URL redirects", SeverityWarning},
	{"OKF203", "Remote URL was skipped by policy or budget", SeverityInfo},
}

func renderSARIF(out io.Writer, diagnostics []Diagnostic) error {
	rules := make([]sarifRule, 0, len(sarifRules))
	for _, source := range sarifRules {
		rule := sarifRule{ID: source.code}
		rule.ShortDescription.Text = source.description
		rule.DefaultConfiguration.Level = sarifLevel(source.severity)
		rules = append(rules, rule)
	}
	results := make([]sarifResult, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		properties := map[string]string{}
		for key, value := range map[string]string{
			"reference_kind": diagnostic.ReferenceKind,
			"field":          diagnostic.Field,
			"target":         diagnostic.Target,
			"resolved":       diagnostic.Resolved,
			"outcome":        diagnostic.Outcome,
		} {
			if value != "" {
				properties[key] = value
			}
		}
		result := sarifResult{RuleID: diagnostic.Code, Level: sarifLevel(diagnostic.Severity), Message: sarifMessage{Text: diagnostic.Message}, Properties: properties}
		if diagnostic.File != "" {
			result.Locations = []sarifLocation{{PhysicalLocation: sarifPhysicalLocation{ArtifactLocation: sarifArtifactLocation{URI: diagnostic.File}, Region: sarifRegion{StartLine: diagnostic.Line, StartColumn: diagnostic.Column}}}}
		}
		results = append(results, result)
	}
	report := sarifReport{
		Version: "2.1.0", Schema: "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{Tool: sarifTool{Driver: sarifDriver{Name: "okflint", InformationURI: "https://github.com/mark-ht/okf-ok", Rules: rules}}, Results: results}},
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func sarifLevel(severity Severity) string {
	switch severity {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return "note"
	}
}
