package lint

import (
	"context"
	"os"
	"path/filepath"
	"sort"
)

// Check reads and validates one bundle. Read failures become diagnostics so a
// caller can still receive findings for the rest of the bundle.
func Check(ctx context.Context, root string, options Options) ([]Diagnostic, error) {
	bundle, err := discover(root)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(bundle.Files))
	for file := range bundle.Files {
		if isMarkdown(file) {
			paths = append(paths, file)
		}
	}
	sort.Strings(paths)
	var diagnostics []Diagnostic
	var references []Reference
	for _, file := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source, err := os.ReadFile(filepath.Join(bundle.Root, filepath.FromSlash(file)))
		if err != nil {
			diagnostics = append(diagnostics, diagnostic("OKF010", SeverityError, Location{File: file, Line: 1, Column: 1}, Reference{}, "could not read Markdown file"))
			continue
		}
		fm, parseDiagnostics := parseFrontmatter(file, source)
		diagnostics = append(diagnostics, parseDiagnostics...)
		if !isReserved(file) && len(parseDiagnostics) == 0 {
			diagnostics = append(diagnostics, validateConcept(file, fm)...)
		}
		if fm.root != nil {
			for _, ref := range frontmatterReferences(file, fm) {
				references = append(references, ref)
				diagnostics = append(diagnostics, localDiagnostics(bundle, ref, options)...)
				diagnostics = append(diagnostics, anchorDiagnostics(bundle, ref, options)...)
			}
		}
		body, lineBase := markdownBody(source, fm)
		for _, ref := range markdownReferences(file, body, lineBase) {
			references = append(references, ref)
			diagnostics = append(diagnostics, localDiagnostics(bundle, ref, options)...)
			diagnostics = append(diagnostics, anchorDiagnostics(bundle, ref, options)...)
		}
	}
	diagnostics = append(diagnostics, remoteDiagnostics(ctx, references, options.Remote)...)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sortDiagnostics(diagnostics)
	return diagnostics, nil
}

func markdownBody(source []byte, fm frontmatter) ([]byte, int) {
	if !fm.present {
		return source, 1
	}
	lines := 1
	for offset := 0; offset < len(source); {
		next := offset
		for next < len(source) && source[next] != '\n' {
			next++
		}
		if lines > 1 && stringTrim(source[offset:next]) == "---" {
			if next < len(source) {
				next++
			}
			return source[next:], lines + 1
		}
		if next == len(source) {
			break
		}
		offset = next + 1
		lines++
	}
	return nil, 1
}

func stringTrim(b []byte) string {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r') {
		end--
	}
	return string(b[start:end])
}
