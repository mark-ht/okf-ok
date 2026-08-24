package generate

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
)

// Main implements `okfok generate plan|apply`. Planning is read-only and emits
// a reviewable plan to stdout; callers choose whether and where to save it.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: okfok generate <plan|apply> [flags]")
		return 2
	}
	switch args[0] {
	case "plan":
		return planMain(ctx, args[1:], stdout, stderr)
	case "apply":
		return applyMain(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown generate command %q\n", args[0])
		return 2
	}
}
func planMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("okfok generate plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "repository workspace root")
	bundle := flags.String("bundle", "knowledge", "OKF bundle path relative to repository")
	format := flags.String("format", "text", "plan format: text or json")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(stderr, "invalid --format")
		return 2
	}
	plan, err := Build(ctx, *repo, *bundle)
	if err != nil {
		fmt.Fprintf(stderr, "okfok: generate plan: %v\n", err)
		return 2
	}
	if *format == "json" {
		if err := json.NewEncoder(stdout).Encode(plan); err != nil {
			return 3
		}
		return 0
	}
	fmt.Fprintf(stdout, "OKF generation plan for %s\n", filepath.ToSlash(plan.Repository))
	fmt.Fprintf(stdout, "bundle: %s\nsymbols: %d\ndocuments: %d\nexcluded: %d\ninventory: %s\n", plan.Bundle, len(plan.Symbols), len(plan.Documents), len(plan.Exclusions), plan.InventoryHash)
	for _, document := range plan.Documents {
		fmt.Fprintf(stdout, "  create %s\n", document.Path)
	}
	return 0
}
func applyMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("okfok generate apply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", ".", "repository workspace root")
	input := flags.String("plan", "", "reviewed JSON plan path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *input == "" {
		fmt.Fprintln(stderr, "usage: okfok generate apply --repo . --plan plan.json")
		return 2
	}
	plan, err := ReadPlan(*input)
	if err != nil {
		fmt.Fprintf(stderr, "okfok: read plan: %v\n", err)
		return 2
	}
	absolute, err := filepath.Abs(*repo)
	if err != nil || absolute != plan.Repository {
		fmt.Fprintln(stderr, "okfok: plan repository does not match --repo")
		return 2
	}
	if err := Apply(ctx, plan); err != nil {
		fmt.Fprintf(stderr, "okfok: generate apply: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "generated %d OKF documents in %s\n", len(plan.Documents), plan.Bundle)
	return 0
}
