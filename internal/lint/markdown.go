package lint

import (
	"bytes"
	"regexp"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Definitions which are not used by a reference link do not appear in
// Goldmark's AST. This narrow scanner covers their destinations so an index or
// document does not hide a stale definition merely because it is currently unused.
var definitionRE = regexp.MustCompile(`(?m)^[ \t]{0,3}\[[^\]]+\]:[ \t]*(?:<([^>]+)>|([^ \t\n]+))`)

func markdownReferences(path string, source []byte, lineBase int) []Reference {
	parser := goldmark.New().Parser()
	document := parser.Parse(text.NewReader(source))
	var refs []Reference
	seen := map[string]struct{}{}
	autoCursor := 0
	add := func(kind, target string, offset int) {
		if target == "" {
			return
		}
		loc := offsetLocation(path, source, offset, lineBase)
		key := kind + "\x00" + target + "\x00" + string(rune(offset))
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		refs = append(refs, Reference{Origin: path, Location: loc, Kind: kind, Target: target})
	}
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Link:
			add("markdown.link", string(n.Destination), firstTextOffset(n, source))
		case *ast.Image:
			add("markdown.image", string(n.Destination), firstTextOffset(n, source))
		case *ast.AutoLink:
			value := n.URL(source)
			offset := bytes.Index(source[autoCursor:], value)
			if offset >= 0 {
				offset += autoCursor
				autoCursor = offset + len(value)
			}
			add("markdown.autolink", string(value), offset)
		}
		return ast.WalkContinue, nil
	})
	// A used definition is represented by its resolved Link AST node. Avoid a
	// duplicate finding for it while retaining unused definitions as references.
	astDestinations := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		astDestinations[ref.Target] = struct{}{}
	}
	for _, match := range definitionRE.FindAllSubmatchIndex(source, -1) {
		if isFencedOffset(source, match[0]) || bytes.HasPrefix(bytes.TrimLeft(source[match[0]:match[1]], " \t"), []byte("[^")) {
			continue // Footnotes are attribution labels, not Markdown link definitions.
		}
		start, end := match[2], match[3]
		if start < 0 {
			start, end = match[4], match[5]
		}
		target := string(source[start:end])
		if _, used := astDestinations[target]; !used {
			add("markdown.reference_definition", target, start)
		}
	}
	return refs
}

func isFencedOffset(source []byte, offset int) bool {
	inFence := false
	for lineStart := 0; lineStart < len(source) && lineStart <= offset; {
		lineEnd := bytes.IndexByte(source[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(source) - lineStart
		}
		line := bytes.TrimLeft(source[lineStart:lineStart+lineEnd], " \t")
		if bytes.HasPrefix(line, []byte("```")) || bytes.HasPrefix(line, []byte("~~~")) {
			inFence = !inFence
		}
		lineStart += lineEnd + 1
	}
	return inFence
}

func firstTextOffset(node ast.Node, source []byte) int {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if textNode, ok := child.(*ast.Text); ok {
			return textNode.Segment.Start
		}
		if offset := firstTextOffset(child, source); offset >= 0 {
			return offset
		}
	}
	// Empty-alt images and empty-label links lack a child segment. The target is
	// still a better location than an arbitrary file-level fallback.
	return bytes.Index(source, []byte(""))
}

func offsetLocation(path string, source []byte, offset, lineBase int) Location {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line := lineBase
	lastNewline := -1
	for i, b := range source[:offset] {
		if b == '\n' {
			line++
			lastNewline = i
		}
	}
	return Location{File: path, Line: line, Column: offset - lastNewline}
}
