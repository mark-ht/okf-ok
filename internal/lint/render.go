package lint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

func render(out io.Writer, ds []Diagnostic, summary Summary, format string) error {
	switch format {
	case "sarif":
		return renderSARIF(out, ds)
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(ds)
	case "jsonl":
		encoder := json.NewEncoder(out)
		for _, d := range ds {
			if err := encoder.Encode(d); err != nil {
				return err
			}
		}
		return nil
	case "text":
		return renderText(out, ds, summary)
	default:
		return fmt.Errorf("unknown output format %q", format)
	}
}

func renderText(out io.Writer, ds []Diagnostic, summary Summary) error {
	for _, d := range ds {
		field := ""
		if d.Field != "" {
			field = " [" + d.Field + "]"
		}
		target := ""
		if d.Target != "" {
			target = fmt.Sprintf(" %q", d.Target)
		}
		if _, err := fmt.Fprintf(out, "%s %s %s:%d:%d%s%s: %s\n", d.Code, strings.ToUpper(string(d.Severity)), d.File, d.Line, d.Column, field, target, d.Message); err != nil {
			return err
		}
	}
	if len(ds) > 0 {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	return renderSummary(out, summary)
}

func renderSummary(out io.Writer, summary Summary) error {
	var b bytes.Buffer
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Summary")
	fmt.Fprintln(w, "Metric\tCount")
	fmt.Fprintln(w, "Bundle files discovered\t", summary.BundleFiles)
	fmt.Fprintln(w, "Markdown files discovered\t", summary.MarkdownFiles)
	fmt.Fprintln(w, "Markdown files read\t", summary.MarkdownFilesRead)
	fmt.Fprintln(w, "Concept documents\t", summary.ConceptDocuments)
	fmt.Fprintln(w, "Reserved index/log documents\t", summary.ReservedDocuments)
	fmt.Fprintln(w, "References checked\t", summary.ReferencesChecked)

	fmt.Fprintln(w, "\nDocument types")
	fmt.Fprintln(w, "Type\tDocuments")
	if len(summary.TypeCounts) == 0 {
		fmt.Fprintln(w, "(none)\t0")
	} else {
		for _, typ := range sortedKeys(summary.TypeCounts) {
			fmt.Fprintln(w, typ+"\t", summary.TypeCounts[typ])
		}
	}

	fmt.Fprintln(w, "\nFindings by severity")
	fmt.Fprintln(w, "Severity\tCount")
	for _, severity := range []Severity{SeverityError, SeverityWarning, SeverityInfo} {
		fmt.Fprintln(w, string(severity)+"\t", summary.SeverityCounts[severity])
	}

	fmt.Fprintln(w, "\nFindings by code")
	fmt.Fprintln(w, "Code\tCount")
	if len(summary.CodeCounts) == 0 {
		fmt.Fprintln(w, "(none)\t0")
	} else {
		for _, code := range sortedKeys(summary.CodeCounts) {
			fmt.Fprintln(w, code+"\t", summary.CodeCounts[code])
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	_, err := out.Write(b.Bytes())
	return err
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
