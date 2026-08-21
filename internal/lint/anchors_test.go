package lint

import (
	"context"
	"testing"
)

func TestLocalFragmentsAreOptIn(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "target.md", "---\ntype: Note\n---\n# Known heading\n# Known heading\n")
	writeTestFile(t, root, "source.md", "---\ntype: Note\n---\n[good](target.md#known-heading) [duplicate](target.md#known-heading-1) [bad](target.md#gone)\n")
	ds, err := Check(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 0 {
		t.Fatalf("default diagnostics = %#v", ds)
	}
	ds, err = Check(context.Background(), root, Options{CheckFragments: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Code != "OKF104" || ds[0].Target != "target.md#gone" {
		t.Fatalf("fragment diagnostics = %#v", ds)
	}
}

func TestHeadingSlug(t *testing.T) {
	if got := headingSlug(" Cost & Revenue: FY 2026 "); got != "cost-revenue-fy-2026" {
		t.Fatalf("slug = %q", got)
	}
}
