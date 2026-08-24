package lint

import "testing"

func TestProgressiveViewerScopesAndBoundsGraph(t *testing.T) {
	graph := ViewerGraph{Nodes: []ViewerNode{{Path: "packages/a/one.md"}, {Path: "packages/a/two.md"}, {Path: "packages/b/three.md"}}, Edges: []ViewerEdge{{Source: "packages/a/one.md", Target: "packages/a/two.md", Kind: "markdown.link"}, {Source: "packages/a/one.md", Target: "packages/b/three.md", Kind: "markdown.link"}}}
	overview := ViewerOverviewFor(graph)
	if len(overview.Nodes) != 2 || len(overview.Edges) != 1 || overview.Edges[0].Count != 1 {
		t.Fatalf("overview = %#v", overview)
	}
	tree, err := ViewerTreeFor(graph, "packages/a")
	if err != nil || len(tree.Children) != 2 {
		t.Fatalf("tree = %#v, %v", tree, err)
	}
	neighborhood, err := ViewerNeighborhoodFor(graph, "packages/a/one.md", 1, 2)
	if err != nil || len(neighborhood.Nodes) != 2 || !neighborhood.Truncated {
		t.Fatalf("neighborhood = %#v, %v", neighborhood, err)
	}
	if _, err := ViewerNeighborhoodFor(graph, "packages/a/one.md", 3, 10); err == nil {
		t.Fatal("invalid depth accepted")
	}
	if len(ViewerSearch(graph, "THREE", 50)) != 1 {
		t.Fatal("search is not case-insensitive")
	}
}
