package lint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestViewerGraphAndDocumentRoutes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "notes/one.md", "---\ntype: Note\n---\n# One\nSee [two](two.md).\n")
	writeTestFile(t, root, "notes/two.md", "---\ntype: Note\n---\n# Two\n")
	writeTestFile(t, root, "secret.txt", "must not be served\n")

	graph, err := BuildViewerGraph(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if graph.Schema != "okf.viewer/v1" || len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("graph = %#v", graph)
	}
	if got := graph.Nodes[0].Headings[0]; got.ID != "one" || got.Text != "One" {
		t.Fatalf("heading = %#v", got)
	}
	edge := graph.Edges[0]
	if edge.Source != "notes/one.md" || edge.Target != "notes/two.md" || edge.Status != "resolved" {
		t.Fatalf("edge = %#v", edge)
	}

	server := httptest.NewServer(viewerHandler(graph, root))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/graph")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("graph response = %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	response.Body.Close()

	response, err = http.Get(server.URL + "/document?path=notes%2Fone.md")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("encoded separator response = %d", response.StatusCode)
	}
	response.Body.Close()

	response, err = http.Get(server.URL + "/document?path=notes/one.md")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/plain") || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("document response = %d %q %#v", response.StatusCode, response.Header.Get("Content-Type"), response.Header)
	}
	response.Body.Close()

	if err := os.Remove(root + "/notes/one.md"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root+"/secret.txt", root+"/notes/one.md"); err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(server.URL + "/document?path=notes/one.md")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("symlink replacement response = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestViewerGraphMarksUnsafeAndMissingEdges(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "docs/one.md", "---\ntype: Note\n---\n[missing](missing.md) [escape](../../secret.md)\n")
	graph, err := BuildViewerGraph(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Edges) != 2 || graph.Edges[0].Status != "missing" || graph.Edges[1].Status != "unsafe" {
		t.Fatalf("edges = %#v", graph.Edges)
	}
}
