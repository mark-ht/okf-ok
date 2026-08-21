// Package lint validates Open Knowledge Format (OKF) bundles and reports
// structural and reference-health diagnostics. It never modifies a bundle.
package lint

import "sort"

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Diagnostic struct {
	Schema        string   `json:"schema"`
	Code          string   `json:"code"`
	Severity      Severity `json:"severity"`
	File          string   `json:"file"`
	Line          int      `json:"line"`
	Column        int      `json:"column"`
	ReferenceKind string   `json:"reference_kind,omitempty"`
	Field         string   `json:"field,omitempty"`
	Target        string   `json:"target,omitempty"`
	Resolved      string   `json:"resolved,omitempty"`
	Outcome       string   `json:"outcome,omitempty"`
	Message       string   `json:"message"`
}

type Reference struct {
	Origin    string
	Location  Location
	Kind      string
	Field     string
	Target    string
	Ambiguous bool // A bare sources[].resource may be a scope descriptor.
}

type Bundle struct {
	Root  string
	Files map[string]struct{} // slash-separated paths of regular files
	Dirs  map[string]struct{}
}

type Options struct {
	StrictSourcePaths bool
	CheckFragments    bool
	Remote            RemoteOptions
}

// Summary describes the work completed during one bundle check. Counts are
// independent of the selected failure threshold and remote failure policy.
type Summary struct {
	BundleFiles       int
	MarkdownFiles     int
	MarkdownFilesRead int
	ConceptDocuments  int
	ReservedDocuments int
	ReferencesChecked int
	TypeCounts        map[string]int
	SeverityCounts    map[Severity]int
	CodeCounts        map[string]int
}

type Result struct {
	Diagnostics []Diagnostic
	Summary     Summary
}

func sortDiagnostics(ds []Diagnostic) {
	sort.Slice(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Target < b.Target
	})
}

func diagnostic(code string, severity Severity, loc Location, ref Reference, message string) Diagnostic {
	return Diagnostic{
		Schema: "okf.lint/v1", Code: code, Severity: severity,
		File: loc.File, Line: loc.Line, Column: loc.Column,
		ReferenceKind: ref.Kind, Field: ref.Field, Target: ref.Target,
		Message: message,
	}
}
