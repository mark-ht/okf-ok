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
	result, err := CheckWithSummary(ctx, root, options)
	return result.Diagnostics, err
}

// CheckWithSummary validates one bundle and returns diagnostics plus the work
// performed, suitable for a human-readable end-of-run summary.
func CheckWithSummary(ctx context.Context, root string, options Options) (Result, error) {
	bundle, err := discover(root)
	if err != nil {
		return Result{}, err
	}
	paths := make([]string, 0, len(bundle.Files))
	for file := range bundle.Files {
		if isMarkdown(file) {
			paths = append(paths, file)
		}
	}
	sort.Strings(paths)
	summary := Summary{
		BundleFiles:    len(bundle.Files),
		MarkdownFiles:  len(paths),
		TypeCounts:     make(map[string]int),
		SeverityCounts: make(map[Severity]int),
		CodeCounts:     make(map[string]int),
	}
	var diagnostics []Diagnostic
	var references []Reference
	for _, file := range paths {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		source, err := os.ReadFile(filepath.Join(bundle.Root, filepath.FromSlash(file)))
		if err != nil {
			diagnostics = append(diagnostics, diagnostic("OKF010", SeverityError, Location{File: file, Line: 1, Column: 1}, Reference{}, "could not read Markdown file"))
			continue
		}
		summary.MarkdownFilesRead++
		reserved := isReserved(file)
		if reserved {
			summary.ReservedDocuments++
		}
		fm, parseDiagnostics := parseFrontmatter(file, source)
		diagnostics = append(diagnostics, parseDiagnostics...)
		if !reserved && len(parseDiagnostics) == 0 {
			diagnostics = append(diagnostics, validateConcept(file, fm)...)
			if typ, ok := conceptType(fm); ok {
				summary.ConceptDocuments++
				summary.TypeCounts[typ]++
			}
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
	summary.ReferencesChecked = len(references)
	diagnostics = append(diagnostics, remoteDiagnostics(ctx, references, options.Remote)...)
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sortDiagnostics(diagnostics)
	for _, d := range diagnostics {
		summary.SeverityCounts[d.Severity]++
		summary.CodeCounts[d.Code]++
	}
	return Result{Diagnostics: diagnostics, Summary: summary}, nil
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
