package lint

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxPreviewBytes = 16 * 1024

//go:embed viewer.html
var viewerHTML []byte

type ViewerGraph struct {
	Schema      string       `json:"schema"`
	Summary     Summary      `json:"summary"`
	Nodes       []ViewerNode `json:"nodes"`
	Edges       []ViewerEdge `json:"edges"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type ViewerNode struct {
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	Type      string    `json:"type,omitempty"`
	Headings  []Heading `json:"headings"`
	Preview   string    `json:"preview"`
	Truncated bool      `json:"truncated"`
}

type ViewerEdge struct {
	Source    string `json:"source"`
	Target    string `json:"target,omitempty"`
	RawTarget string `json:"raw_target"`
	Kind      string `json:"kind"`
	Field     string `json:"field,omitempty"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Status    string `json:"status"`
}

// BuildViewerGraph takes a read-only snapshot of a bundle for the local UI.
// Remote probing is deliberately disabled by Serve regardless of CLI settings.
func BuildViewerGraph(ctx context.Context, root string, options Options) (ViewerGraph, error) {
	options.Remote = RemoteOptions{}
	result, err := CheckWithSummary(ctx, root, options)
	if err != nil {
		return ViewerGraph{}, err
	}
	bundle, err := discover(root)
	if err != nil {
		return ViewerGraph{}, err
	}
	paths := make([]string, 0, len(bundle.Files))
	for file := range bundle.Files {
		if isMarkdown(file) {
			paths = append(paths, file)
		}
	}
	sort.Strings(paths)
	graph := ViewerGraph{Schema: "okf.viewer/v1", Summary: result.Summary, Diagnostics: result.Diagnostics}
	for _, file := range paths {
		if err := ctx.Err(); err != nil {
			return ViewerGraph{}, err
		}
		source, err := os.ReadFile(filepath.Join(bundle.Root, filepath.FromSlash(file)))
		if err != nil {
			continue // CheckWithSummary already includes the read diagnostic.
		}
		fm, _ := parseFrontmatter(file, source)
		node := ViewerNode{Path: file, Kind: "concept", Headings: documentHeadings(markdownBodyOnly(source, fm))}
		if isReserved(file) {
			node.Kind = "reserved"
		} else if typ, ok := conceptType(fm); ok {
			node.Type = typ
		}
		node.Preview, node.Truncated = preview(source)
		graph.Nodes = append(graph.Nodes, node)

		var references []Reference
		if fm.root != nil {
			references = append(references, frontmatterReferences(file, fm)...)
		}
		body, lineBase := markdownBody(source, fm)
		references = append(references, markdownReferences(file, body, lineBase)...)
		for _, ref := range references {
			status, target := viewerTarget(bundle, ref, options.StrictSourcePaths)
			graph.Edges = append(graph.Edges, ViewerEdge{
				Source: file, Target: target, RawTarget: ref.Target, Kind: ref.Kind, Field: ref.Field,
				Line: ref.Location.Line, Column: ref.Location.Column, Status: status,
			})
		}
	}
	sort.Slice(graph.Edges, func(i, j int) bool {
		a, b := graph.Edges[i], graph.Edges[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.RawTarget < b.RawTarget
	})
	return graph, nil
}

func markdownBodyOnly(source []byte, fm frontmatter) []byte {
	body, _ := markdownBody(source, fm)
	return body
}

func preview(source []byte) (string, bool) {
	if len(source) <= maxPreviewBytes {
		return string(source), false
	}
	return string(source[:maxPreviewBytes]), true
}

func viewerTarget(bundle Bundle, ref Reference, strictSources bool) (string, string) {
	target := strings.TrimSpace(ref.Target)
	if target == "" {
		return "descriptor", ""
	}
	if strings.HasPrefix(target, "#") {
		return "fragment", ref.Origin
	}
	if uriSchemeRE.MatchString(target) {
		return "external", ""
	}
	if strings.HasPrefix(target, "//") {
		return "unsafe", ""
	}
	local := strings.SplitN(strings.SplitN(target, "#", 2)[0], "?", 2)[0]
	decoded, err := url.PathUnescape(local)
	if err != nil || strings.ContainsAny(local, "\\\x00") || strings.ContainsAny(decoded, "\\\x00") || strings.Contains(strings.ToLower(local), "%2f") || strings.Contains(strings.ToLower(local), "%5c") {
		return "unsafe", ""
	}
	var resolved string
	if strings.HasPrefix(decoded, "/") {
		resolved = path.Clean(strings.TrimPrefix(decoded, "/"))
	} else {
		resolved = path.Clean(path.Join(path.Dir(ref.Origin), decoded))
	}
	if resolved == "." {
		resolved = ""
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") || traversesSymlink(bundle, resolved) {
		return "unsafe", ""
	}
	if _, ok := bundle.Files[resolved]; ok {
		return "resolved", resolved
	}
	if _, ok := bundle.Dirs[resolved]; ok {
		return "directory", resolved
	}
	if ref.Ambiguous && !strictSources {
		return "descriptor", resolved
	}
	return "missing", resolved
}

// Serve starts the local, read-only relationship viewer and blocks until its
// context is cancelled or the listener fails. It never performs remote probes.
func Serve(ctx context.Context, root, address string, options Options, stderr io.Writer) error {
	graph, err := BuildViewerGraph(ctx, root, options)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: viewerHandler(graph, graphRoot(root)), ReadHeaderTimeout: 5 * time.Second}
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	fmt.Fprintf(stderr, "okflint: viewer serving %s for %s\n", listener.Addr(), graphRoot(root))
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return nil
	case err := <-errs:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func viewerHandler(graph ViewerGraph, root string) http.Handler {
	nodes := make(map[string]struct{}, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes[node.Path] = struct{}{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(viewerHTML)
	})
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(graph)
	})
	mux.HandleFunc("/document", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		file, ok := viewerDocumentPath(r, nodes)
		if !ok {
			http.Error(w, "invalid document path", http.StatusBadRequest)
			return
		}
		source, err := safeViewerRead(root, file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(source)
	})
	return mux
}

func safeViewerRead(root, file string) ([]byte, error) {
	current := root
	for _, segment := range strings.Split(file, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("document path traverses symlink")
		}
	}
	info, err := os.Stat(current)
	if err != nil || !info.Mode().IsRegular() {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("document is not a regular file")
	}
	return os.ReadFile(current)
}

func graphRoot(root string) string {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	return absolute
}

func viewerDocumentPath(r *http.Request, nodes map[string]struct{}) (string, bool) {
	raw := strings.ToLower(r.URL.RawQuery)
	if strings.Contains(raw, "%2f") || strings.Contains(raw, "%5c") {
		return "", false
	}
	file := r.URL.Query().Get("path")
	if file == "" || strings.HasPrefix(file, "/") || strings.ContainsAny(file, "\\\x00") || file == "." || file == ".." || strings.HasPrefix(file, "../") || path.Clean(file) != file || !isMarkdown(file) {
		return "", false
	}
	_, ok := nodes[file]
	return file, ok
}
