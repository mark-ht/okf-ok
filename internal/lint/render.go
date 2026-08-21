package lint

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func render(out io.Writer, ds []Diagnostic, format string) error {
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
		return nil
	default:
		return fmt.Errorf("unknown output format %q", format)
	}
}
