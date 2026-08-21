package lint

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderSARIFGolden(t *testing.T) {
	diagnostics := []Diagnostic{
		{Schema: "okf.lint/v1", Code: "OKF101", Severity: SeverityWarning, File: "metrics/revenue.md", Line: 13, Column: 15, ReferenceKind: "frontmatter.sources.resource", Field: "sources[0].resource", Target: "policies/revenue.md", Resolved: "metrics/policies/revenue.md", Message: "local target does not exist"},
		{Schema: "okf.lint/v1", Code: "OKF201", Severity: SeverityWarning, File: "references/docs.md", Line: 8, Column: 3, ReferenceKind: "markdown.link", Target: "https://example.com/gone", Outcome: "gone", Message: "remote URL returned a permanent not-found response (HTTP 410)"},
	}
	var output bytes.Buffer
	if err := renderSARIF(&output, diagnostics); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "sarif.golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, output.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("SARIF output changed; run UPDATE_GOLDEN=1 go test ./internal/lint")
	}
	var report sarifReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Version != "2.1.0" || len(report.Runs) != 1 || len(report.Runs[0].Results) != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Runs[0].Results[1].Properties["outcome"] != "gone" {
		t.Fatalf("remote outcome missing: %#v", report.Runs[0].Results[1])
	}
}
