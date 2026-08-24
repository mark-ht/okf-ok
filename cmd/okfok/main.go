package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark-ht/okf-ok/internal/generate"
	"github.com/mark-ht/okf-ok/internal/lint"
)

func main() { os.Exit(run(context.Background(), os.Args[1:])) }

func run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: okfok <generate|lint|serve> ...")
		return 2
	}
	switch args[0] {
	case "generate":
		return generate.Main(ctx, args[1:], os.Stdout, os.Stderr)
	case "lint":
		return lint.Main(ctx, workspaceLintArgs(args[1:]), os.Stdout, os.Stderr)
	case "serve":
		return lint.Main(ctx, workspaceServeArgs(args[1:]), os.Stdout, os.Stderr)
	default:
		// A bare path or legacy lint flag preserves the established container and
		// CLI contract while making okfok the umbrella executable.
		return lint.Main(ctx, args, os.Stdout, os.Stderr)
	}
}

func workspaceLintArgs(args []string) []string {
	repo, bundle, rest := workspaceFlags(args)
	return append(append([]string{"--workspace-root", repo}, rest...), filepath.Join(repo, bundle))
}
func workspaceServeArgs(args []string) []string {
	repo, bundle, rest := workspaceFlags(args)
	if len(rest) == 0 {
		return []string{}
	}
	address := rest[0]
	return []string{"--workspace-root", repo, "--serve", address, filepath.Join(repo, bundle)}
}
func workspaceFlags(args []string) (string, string, []string) {
	repo, bundle := ".", "knowledge"
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 < len(args) {
				repo = args[i+1]
				i++
				continue
			}
		case "--bundle":
			if i+1 < len(args) {
				bundle = args[i+1]
				i++
				continue
			}
		}
		rest = append(rest, args[i])
	}
	absolute, err := filepath.Abs(repo)
	if err == nil {
		repo = absolute
	}
	return repo, bundle, rest
}
