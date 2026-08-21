package lint

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

type stringList []string

func (values *stringList) String() string { return fmt.Sprint([]string(*values)) }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

// Main is the command entry point. It is exported to keep main.go free of CLI
// policy and to let integration tests exercise the exact command behavior.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("okflint", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "text", "output format: text, json, jsonl, or sarif")
	failOn := flags.String("fail-on", "error", "fail on: error or warning")
	strictSources := flags.Bool("strict-source-paths", false, "treat ambiguous source resources as local paths")
	checkFragments := flags.Bool("check-fragments", false, "validate local Markdown heading fragments")
	checkRemote := flags.Bool("check-remote", false, "probe HTTPS references under an explicit remote policy")
	remotePolicy := flags.String("remote-policy", "off", "remote policy: off, allowlist, or public")
	remoteWorkers := flags.Int("remote-workers", 4, "maximum concurrent remote probes")
	maxRemoteURLs := flags.Int("max-remote-urls", 100, "maximum unique remote URLs to probe")
	remoteTimeout := flags.Duration("remote-timeout", 10*time.Second, "timeout for each remote probe")
	remoteDeadline := flags.Duration("remote-deadline", time.Minute, "overall remote-check deadline")
	failOnRemote := flags.String("fail-on-remote", "none", "remote outcomes that fail: none, gone, redirected, or all")
	var allowedHosts stringList
	flags.Var(&allowedHosts, "allow-host", "allowed remote hostname (repeatable)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: okflint [flags] <bundle-root>")
		return 2
	}
	if *format != "text" && *format != "json" && *format != "jsonl" && *format != "sarif" {
		fmt.Fprintf(stderr, "invalid --format %q\n", *format)
		return 2
	}
	if *failOn != "error" && *failOn != "warning" {
		fmt.Fprintf(stderr, "invalid --fail-on %q\n", *failOn)
		return 2
	}
	if *failOnRemote != "none" && *failOnRemote != "gone" && *failOnRemote != "redirected" && *failOnRemote != "all" {
		fmt.Fprintf(stderr, "invalid --fail-on-remote %q\n", *failOnRemote)
		return 2
	}
	policy := RemotePolicy(*remotePolicy)
	if policy != RemoteOff && policy != RemoteAllowlist && policy != RemotePublic {
		fmt.Fprintf(stderr, "invalid --remote-policy %q\n", *remotePolicy)
		return 2
	}
	if *checkRemote && policy == RemoteOff {
		fmt.Fprintln(stderr, "--check-remote requires --remote-policy=allowlist or public")
		return 2
	}
	if *checkRemote && policy == RemoteAllowlist && len(allowedHosts) == 0 {
		fmt.Fprintln(stderr, "--remote-policy=allowlist requires --allow-host")
		return 2
	}
	if *remoteWorkers < 1 || *maxRemoteURLs < 1 || *remoteTimeout <= 0 || *remoteDeadline <= 0 {
		fmt.Fprintln(stderr, "remote workers, URL budget, timeouts, and deadline must be positive")
		return 2
	}
	select {
	case <-ctx.Done():
		return 130
	default:
	}
	result, err := CheckWithSummary(ctx, flags.Arg(0), Options{
		StrictSourcePaths: *strictSources,
		CheckFragments:    *checkFragments,
		Remote:            RemoteOptions{Enabled: *checkRemote, Policy: policy, Hosts: allowedHosts, Workers: *remoteWorkers, MaxURLs: *maxRemoteURLs, Timeout: *remoteTimeout, Deadline: *remoteDeadline},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 130
		}
		fmt.Fprintf(stderr, "okflint: %v\n", err)
		return 2
	}
	if err := render(stdout, result.Diagnostics, result.Summary, *format); err != nil {
		fmt.Fprintf(stderr, "okflint: writing output: %v\n", err)
		return 3
	}
	errors, warnings, remoteFailures := 0, 0, 0
	for _, d := range result.Diagnostics {
		if d.Severity == SeverityError {
			errors++
		}
		if d.Severity == SeverityWarning {
			warnings++
		}
		if remoteFailure(d.Outcome, *failOnRemote) {
			remoteFailures++
		}
	}
	fmt.Fprintf(stderr, "okflint: %d error(s), %d warning(s), %d selected remote failure(s)\n", errors, warnings, remoteFailures)
	if errors > 0 || (*failOn == "warning" && warnings > 0) || remoteFailures > 0 {
		return 1
	}
	return 0
}

func remoteFailure(outcome, policy string) bool {
	switch policy {
	case "gone":
		return outcome == "gone"
	case "redirected":
		return outcome == "redirected"
	case "all":
		return outcome == "gone" || outcome == "redirected" || outcome == "inconclusive" || outcome == "policy_skipped"
	default:
		return false
	}
}
