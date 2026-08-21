package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceRootPermitsOnlyFrontmatterArtifacts(t *testing.T) {
	repo := t.TempDir()
	writeTestFile(t, repo, "pkg/widget.go", "package pkg\n")
	bundle := filepath.Join(repo, "knowledge")
	writeTestFile(t, bundle, "docs/widget.md", "---\ntype: Go Type\nresource: ../../pkg/widget.go\n---\n[source](../../pkg/widget.go)\n")

	diagnostics, err := Check(context.Background(), bundle, Options{WorkspaceRoot: repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "OKF100" || diagnostics[0].ReferenceKind != "markdown.link" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestWorkspaceViewerServesValidatedSourceArtifact(t *testing.T) {
	repo := t.TempDir()
	writeTestFile(t, repo, "pkg/widget.go", "package pkg\n")
	bundle := filepath.Join(repo, "knowledge")
	writeTestFile(t, bundle, "docs/widget.md", "---\ntype: Go Type\nresource: ../../pkg/widget.go\n---\n")
	options := Options{WorkspaceRoot: repo}
	graph, err := BuildViewerGraph(context.Background(), bundle, options)
	if err != nil {
		t.Fatal(err)
	}
	if !graph.Nodes[0].SourceAvailable {
		t.Fatal("source was not marked available")
	}
	server := httptest.NewServer(viewerHandler(graph, bundle, options))
	defer server.Close()
	response, err := http.Get(server.URL + "/source?path=docs/widget.md")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("source status = %d", response.StatusCode)
	}
}

func TestWorkspaceRootRejectsSymlinkedArtifact(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, outside, "widget.go", "package outside\n")
	if err := os.Symlink(filepath.Join(outside, "widget.go"), filepath.Join(repo, "widget.go")); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(repo, "knowledge")
	writeTestFile(t, bundle, "doc.md", "---\ntype: Go Type\nresource: ../widget.go\n---\n")
	diagnostics, err := Check(context.Background(), bundle, Options{WorkspaceRoot: repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "OKF100" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}
