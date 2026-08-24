package generate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark-ht/okf-ok/internal/lint"
)

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.test/widgets\n\ngo 1.26.0\n")
	writeFile(t, repo, "widgets/widget.go", `// Package widgets is a fixture.
package widgets

// Widget is a generated test type.
type Widget struct { Name string }

// Named is an interface.
type Named interface { Label() string }

const DefaultName = "widget"
var Count = 1

// New creates a widget.
func New(name string) Widget { return Widget{Name: name} }
func (Widget) Label() string { return "widget" }
`)
	return repo
}

func TestBuildIncludesDeclaredSymbolsDeterministically(t *testing.T) {
	repo := fixtureRepository(t)
	first, err := Build(context.Background(), repo, "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(context.Background(), repo, "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if PlanHash(first) != PlanHash(second) {
		t.Fatalf("plans differ: %s != %s", PlanHash(first), PlanHash(second))
	}
	want := []string{"widgets (widgets).Widget", "widgets (widgets).Named", "widgets (widgets).Named.Label", "widgets (widgets).DefaultName", "widgets (widgets).Count", "widgets (widgets).New", "widgets (widgets).Widget.Label"}
	got := make([]string, len(first.Symbols))
	for i, symbol := range first.Symbols {
		got[i] = symbol.ID()
	}
	for _, id := range want {
		found := false
		for _, value := range got {
			if value == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %q from %#v", id, got)
		}
	}
	for _, document := range first.Documents {
		if document.Source != "" && !strings.Contains(document.Content, "resource:") {
			t.Fatalf("source document missing provenance: %#v", document)
		}
		if strings.Contains(document.Path, "struct-field") {
			t.Fatalf("struct field document generated: %s", document.Path)
		}
		if strings.Contains(document.Path, "go-type-widgets-widgets-widget.md") && !strings.Contains(document.Content, "# Methods") {
			t.Fatalf("type document does not link its methods: %s", document.Content)
		}
	}
}

func TestApplyProducesLintCleanOwnedBundle(t *testing.T) {
	repo := fixtureRepository(t)
	plan, err := Build(context.Background(), repo, "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "knowledge", ".okfok-manifest.json")); err != nil {
		t.Fatal(err)
	}
	result, err := lint.CheckWithSummary(context.Background(), filepath.Join(repo, "knowledge"), lint.Options{WorkspaceRoot: repo})
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity == lint.SeverityError {
			t.Fatalf("structural diagnostic: %#v", diagnostic)
		}
	}
	if err := Apply(context.Background(), plan); err != nil {
		t.Fatalf("owned output cannot be regenerated: %v", err)
	}
}

func TestApplyRejectsUnownedOutput(t *testing.T) {
	repo := fixtureRepository(t)
	writeFile(t, repo, "knowledge/hand.md", "---\ntype: Note\n---\n")
	plan, err := Build(context.Background(), repo, "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), plan); err == nil {
		t.Fatal("apply accepted hand-authored output")
	}
}
