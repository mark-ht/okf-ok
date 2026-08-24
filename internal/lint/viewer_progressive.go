package lint

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

type ViewerOverview struct {
	Schema string               `json:"schema"`
	Nodes  []ViewerOverviewNode `json:"nodes"`
	Edges  []ViewerOverviewEdge `json:"edges"`
}
type ViewerOverviewNode struct {
	Path      string `json:"path"`
	Documents int    `json:"documents"`
}
type ViewerOverviewEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Count  int    `json:"count"`
}
type ViewerTree struct {
	Path     string            `json:"path"`
	Children []ViewerTreeChild `json:"children"`
}
type ViewerTreeChild struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Type      string `json:"type,omitempty"`
	Documents int    `json:"documents,omitempty"`
}
type ViewerNeighborhood struct {
	Path      string       `json:"path"`
	Nodes     []ViewerNode `json:"nodes"`
	Edges     []ViewerEdge `json:"edges"`
	Truncated bool         `json:"truncated"`
}

func viewerGroup(file string) string {
	parts := strings.Split(file, "/")
	if len(parts) > 2 && parts[0] == "packages" {
		return strings.Join(parts[:2], "/")
	}
	if len(parts) > 1 {
		return parts[0]
	}
	return "."
}
func ViewerOverviewFor(graph ViewerGraph) ViewerOverview {
	counts := map[string]int{}
	for _, n := range graph.Nodes {
		counts[viewerGroup(n.Path)]++
	}
	edges := map[string]int{}
	for _, e := range graph.Edges {
		if e.Target == "" {
			continue
		}
		a, b := viewerGroup(e.Source), viewerGroup(e.Target)
		if a != b {
			edges[a+"\x00"+b]++
		}
	}
	out := ViewerOverview{Schema: "okf.viewer.overview/v1"}
	for p, n := range counts {
		out.Nodes = append(out.Nodes, ViewerOverviewNode{p, n})
	}
	for k, n := range edges {
		p := strings.SplitN(k, "\x00", 2)
		out.Edges = append(out.Edges, ViewerOverviewEdge{p[0], p[1], n})
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Path < out.Nodes[j].Path })
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].Source != out.Edges[j].Source {
			return out.Edges[i].Source < out.Edges[j].Source
		}
		return out.Edges[i].Target < out.Edges[j].Target
	})
	return out
}
func ViewerTreeFor(graph ViewerGraph, parent string) (ViewerTree, error) {
	if parent != "" && (strings.HasPrefix(parent, "/") || strings.Contains(parent, "..") || path.Clean(parent) != parent) {
		return ViewerTree{}, fmt.Errorf("invalid tree path")
	}
	prefix := parent
	if prefix != "" {
		prefix += "/"
	}
	dirs := map[string]int{}
	docs := map[string]ViewerTreeChild{}
	for _, n := range graph.Nodes {
		if !strings.HasPrefix(n.Path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(n.Path, prefix)
		parts := strings.Split(rest, "/")
		if len(parts) > 1 {
			dirs[prefix+parts[0]]++
			continue
		}
		docs[n.Path] = ViewerTreeChild{Path: n.Path, Kind: "document", Type: n.Type}
	}
	out := ViewerTree{Path: parent}
	for p, n := range dirs {
		out.Children = append(out.Children, ViewerTreeChild{Path: p, Kind: "directory", Documents: n})
	}
	for _, n := range docs {
		out.Children = append(out.Children, n)
	}
	sort.Slice(out.Children, func(i, j int) bool { return out.Children[i].Path < out.Children[j].Path })
	return out, nil
}
func ViewerSearch(graph ViewerGraph, query string, limit int) []ViewerNode {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	var out []ViewerNode
	for _, n := range graph.Nodes {
		if strings.Contains(strings.ToLower(n.Path), query) || strings.Contains(strings.ToLower(n.Type), query) {
			out = append(out, n)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}
func ViewerNeighborhoodFor(graph ViewerGraph, focus string, depth, limit int) (ViewerNeighborhood, error) {
	if depth < 1 || depth > 2 || limit < 1 || limit > 250 {
		return ViewerNeighborhood{}, fmt.Errorf("invalid neighborhood bounds")
	}
	nodes := map[string]ViewerNode{}
	for _, n := range graph.Nodes {
		nodes[n.Path] = n
	}
	if _, ok := nodes[focus]; !ok {
		return ViewerNeighborhood{}, fmt.Errorf("unknown document")
	}
	seen := map[string]bool{focus: true}
	frontier := []string{focus}
	truncated := false
	for step := 0; step < depth; step++ {
		var next []string
		for _, current := range frontier {
			for _, e := range graph.Edges {
				other := ""
				if e.Source == current {
					other = e.Target
				} else if e.Target == current {
					other = e.Source
				}
				if other == "" || seen[other] {
					continue
				}
				if len(seen) >= limit {
					truncated = true
					continue
				}
				seen[other] = true
				next = append(next, other)
			}
		}
		frontier = next
	}
	out := ViewerNeighborhood{Path: focus, Truncated: truncated}
	for _, n := range graph.Nodes {
		if seen[n.Path] {
			out.Nodes = append(out.Nodes, n)
		}
	}
	grouped := map[string]ViewerEdge{}
	for _, e := range graph.Edges {
		if !seen[e.Source] || e.Target == "" || !seen[e.Target] {
			continue
		}
		key := e.Source + "\x00" + e.Target + "\x00" + e.Status + "\x00" + e.Kind + "\x00" + e.Field
		prior := grouped[key]
		if prior.Count == 0 {
			prior = e
		}
		prior.Count++
		grouped[key] = prior
	}
	for _, e := range grouped {
		out.Edges = append(out.Edges, e)
	}
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].Source != out.Edges[j].Source {
			return out.Edges[i].Source < out.Edges[j].Source
		}
		return out.Edges[i].Target < out.Edges[j].Target
	})
	return out, nil
}
