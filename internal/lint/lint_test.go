package lint

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func codes(ds []Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Code
	}
	return out
}

func TestCheckValidBundleAndLinks(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "tables/orders.md", "---\ntype: Table\n---\n# Orders\n")
	writeTestFile(t, root, "references/check.py", "# test artifact\n")
	writeTestFile(t, root, "metrics/revenue.md", `---
 type: Metric
 resource: ../tables/orders.md
 sources:
   - resource: /tables/orders.md
 executor: {resource: ../references/check.py}
---
See [orders](../tables/orders.md), [absolute](/tables/orders.md), and [ref][orders].
[orders]: ../tables/orders.md
`)
	ds, err := Check(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 0 {
		t.Fatalf("diagnostics = %#v", ds)
	}
}

func TestStructuralAndLocalReferenceDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "bad/no-frontmatter.md", "# no frontmatter\n")
	writeTestFile(t, root, "bad/no-type.md", "---\ntitle: Missing type\n---\n")
	writeTestFile(t, root, "bad/broken.md", "---\ntype: Metric\nresource: missing-resource.md\n---\nSee [missing](missing-body.md).\n")
	writeTestFile(t, root, "index.md", "# This reserved document needs no frontmatter\n")
	ds, err := Check(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(codes(ds), ",")
	want := "OKF101,OKF101,OKF010,OKF012"
	if got != want {
		t.Fatalf("codes = %s, want %s; diagnostics=%#v", got, want, ds)
	}
	for _, d := range ds[:2] {
		if d.Severity != SeverityWarning {
			t.Fatalf("missing target severity = %s", d.Severity)
		}
	}
}

func TestScopeDescriptorsAndStrictSources(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "metrics/usage.md", "---\ntype: Metric\nsources:\n  - resource: all queries in BigQuery project X\n---\n")
	ds, err := Check(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Code != "OKF103" || ds[0].Severity != SeverityInfo {
		t.Fatalf("default diagnostics = %#v", ds)
	}
	ds, err = Check(context.Background(), root, Options{StrictSourcePaths: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Code != "OKF101" || ds[0].Severity != SeverityWarning {
		t.Fatalf("strict diagnostics = %#v", ds)
	}
}

func TestLinkExtractionIgnoresCodeAndBlocksEscapes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "docs/links.md", "---\ntype: Note\n---\n`[inline](ignored.md)`\n\n```md\n[code](also-ignored.md)\n[definition]: ignored-too.md\n```\n\n[escape](../../outside.md)\n")
	ds, err := Check(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Code != "OKF100" {
		t.Fatalf("diagnostics = %#v", ds)
	}
}

func TestSymlinkBundleRootIsRejected(t *testing.T) {
	realRoot := t.TempDir()
	writeTestFile(t, realRoot, "broken.md", "---\ntype: Note\n---\n[missing](missing.md)\n")
	link := filepath.Join(t.TempDir(), "bundle")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(context.Background(), link, Options{}); err == nil {
		t.Fatal("symlink root was accepted")
	}
}

func TestSymlinkPathIsAnEscape(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "source.md", "---\ntype: Note\n---\n[escape](linked/secret.md)\n")
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	ds, err := Check(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Code != "OKF100" {
		t.Fatalf("diagnostics = %#v", ds)
	}
}

func TestRepeatedAutolinksHaveSeparateLocations(t *testing.T) {
	refs := markdownReferences("doc.md", []byte("<https://example.com/x>\n<https://example.com/x>\n"), 1)
	if len(refs) != 2 || refs[0].Location.Line != 1 || refs[1].Location.Line != 2 {
		t.Fatalf("references = %#v", refs)
	}
}

func TestMainCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if got := Main(ctx, []string{t.TempDir()}, &stdout, &stderr); got != 130 {
		t.Fatalf("cancelled exit = %d", got)
	}
}

func TestMainThresholdAndJSONL(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "doc.md", "---\ntype: Note\n---\n[missing](missing.md)\n")
	var stdout, stderr bytes.Buffer
	if got := Main(context.Background(), []string{"--format", "jsonl", root}, &stdout, &stderr); got != 0 {
		t.Fatalf("default exit = %d", got)
	}
	if !strings.Contains(stdout.String(), `"code":"OKF101"`) {
		t.Fatalf("jsonl output = %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if got := Main(context.Background(), []string{"--fail-on", "warning", root}, &stdout, &stderr); got != 1 {
		t.Fatalf("warning exit = %d", got)
	}
}
