package lint

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// anchorDiagnostics applies a deliberately small, documented GitHub-style
// heading-slug policy. OKF does not standardize anchors, so callers must opt in.
func anchorDiagnostics(bundle Bundle, ref Reference, options Options) []Diagnostic {
	if !options.CheckFragments || uriSchemeRE.MatchString(ref.Target) {
		return nil
	}
	hash := strings.IndexByte(ref.Target, '#')
	if hash < 0 || hash == len(ref.Target)-1 {
		return nil
	}
	fragment, err := url.PathUnescape(ref.Target[hash+1:])
	if err != nil {
		return []Diagnostic{diagnostic("OKF104", SeverityWarning, ref.Location, ref, "fragment contains invalid percent escaping")}
	}
	filePart := strings.SplitN(ref.Target[:hash], "?", 2)[0]
	resolved, ok := anchorTarget(ref.Origin, filePart)
	if !ok {
		return nil
	} // localDiagnostics owns malformed/escaping targets.
	if resolved == "" {
		resolved = ref.Origin
	}
	if _, exists := bundle.Files[resolved]; !exists {
		return nil
	}
	if !isMarkdown(resolved) {
		return []Diagnostic{diagnostic("OKF104", SeverityInfo, ref.Location, ref, "fragment target is not a Markdown document")}
	}
	source, err := os.ReadFile(filepath.Join(bundle.Root, filepath.FromSlash(resolved)))
	if err != nil {
		return nil
	}
	fm, _ := parseFrontmatter(resolved, source)
	body, _ := markdownBody(source, fm)
	if _, found := headings(body)[fragment]; found {
		return nil
	}
	d := diagnostic("OKF104", SeverityWarning, ref.Location, ref, "fragment does not match a heading under the GitHub anchor policy")
	d.Resolved = resolved
	return []Diagnostic{d}
}

func anchorTarget(origin, filePart string) (string, bool) {
	if filePart == "" {
		return origin, true
	}
	decoded, err := url.PathUnescape(filePart)
	if err != nil || strings.ContainsAny(decoded, "\\\x00") {
		return "", false
	}
	var target string
	if strings.HasPrefix(decoded, "/") {
		target = path.Clean(strings.TrimPrefix(decoded, "/"))
	} else {
		target = path.Clean(path.Join(path.Dir(origin), decoded))
	}
	if target == ".." || strings.HasPrefix(target, "../") {
		return "", false
	}
	if target == "." {
		target = ""
	}
	return target, true
}

type Heading struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

func headings(source []byte) map[string]struct{} {
	out := make(map[string]struct{})
	for _, heading := range documentHeadings(source) {
		out[heading.ID] = struct{}{}
	}
	return out
}

type headingSpan struct {
	Heading
	start int
}

func documentHeadings(source []byte) []Heading {
	spans := documentHeadingSpans(source)
	out := make([]Heading, len(spans))
	for i, span := range spans {
		out[i] = span.Heading
	}
	return out
}

func documentSection(source []byte, id string) (string, bool) {
	spans := documentHeadingSpans(source)
	for i, span := range spans {
		if span.ID != id {
			continue
		}
		end := len(source)
		for _, next := range spans[i+1:] {
			if next.Level <= span.Level {
				end = next.start
				break
			}
		}
		return string(source[span.start:end]), true
	}
	return "", false
}

func documentHeadingSpans(source []byte) []headingSpan {
	var out []headingSpan
	counts := map[string]int{}
	document := goldmark.New().Parser().Parse(text.NewReader(source))
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if !ok || heading.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		value := string(heading.Text(source))
		slug := headingSlug(value)
		if slug == "" {
			return ast.WalkContinue, nil
		}
		count := counts[slug]
		counts[slug]++
		if count > 0 {
			slug += "-" + strconv.Itoa(count)
		}
		out = append(out, headingSpan{Heading: Heading{ID: slug, Text: value, Level: heading.Level}, start: heading.Lines().At(0).Start})
		return ast.WalkContinue, nil
	})
	return out
}

func headingSlug(value string) string {
	var out []rune
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-':
			out = append(out, r)
			space = false
		case unicode.IsSpace(r):
			if len(out) > 0 {
				space = true
			}
		}
		if space && len(out) > 0 && out[len(out)-1] != '-' {
			out = append(out, '-')
		}
	}
	return strings.Trim(string(out), "-")
}
