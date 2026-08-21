package lint

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func remoteRef(target string) Reference {
	return Reference{Origin: "concept.md", Location: Location{File: "concept.md", Line: 4, Column: 1}, Kind: "markdown.link", Target: target}
}

func TestRemoteCandidatesAreExplicitAndDeduplicated(t *testing.T) {
	candidates, diagnostics := remoteCandidates([]Reference{
		remoteRef("https://EXAMPLE.com/docs?q=one#first"),
		remoteRef("https://example.com/docs?q=one#second"),
		remoteRef("http://example.com/insecure"),
		remoteRef("https://denied.example/path"),
	}, RemoteOptions{Enabled: true, Policy: RemoteAllowlist, Hosts: []string{"example.com"}})
	if len(candidates) != 1 || len(candidates[0].origins) != 2 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].url.String() != "https://example.com/docs?q=one" {
		t.Fatalf("canonical URL = %s", candidates[0].url)
	}
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, d := range diagnostics {
		if d.Code != "OKF203" || d.Outcome != "policy_skipped" {
			t.Fatalf("diagnostic = %#v", d)
		}
	}
}

func TestRemoteCandidateBudget(t *testing.T) {
	candidates, diagnostics := remoteCandidates([]Reference{
		remoteRef("https://example.com/a"), remoteRef("https://example.com/b"),
	}, RemoteOptions{Enabled: true, Policy: RemotePublic, MaxURLs: 1})
	if len(candidates) != 1 || len(diagnostics) != 1 || diagnostics[0].Outcome != "policy_skipped" {
		t.Fatalf("candidates=%#v diagnostics=%#v", candidates, diagnostics)
	}
}

func TestProbeRemoteOutcomes(t *testing.T) {
	for _, test := range []struct {
		name          string
		status        int
		code, outcome string
	}{
		{"reachable", http.StatusNoContent, "", ""},
		{"redirect", http.StatusMovedPermanently, "OKF202", "redirected"},
		{"gone", http.StatusGone, "OKF201", "gone"},
		{"forbidden is inconclusive", http.StatusForbidden, "OKF200", "inconclusive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				header := make(http.Header)
				if test.status >= 300 && test.status < 400 {
					header.Set("Location", "https://example.com/new")
				}
				return &http.Response{StatusCode: test.status, Header: header, Body: io.NopCloser(strings.NewReader(""))}, nil
			}), CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
			result := probeRemote(context.Background(), client, remoteCandidate{url: mustURL(t, "https://example.com/doc"), origins: []Reference{remoteRef("https://example.com/doc")}}, RemoteOptions{Policy: RemotePublic}, nil)
			if test.code == "" {
				if len(result) != 0 {
					t.Fatalf("result = %#v", result)
				}
				return
			}
			if len(result) != 1 || result[0].Code != test.code || result[0].Outcome != test.outcome {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestProbeRetriesTransientStatus(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		status := http.StatusServiceUnavailable
		if attempts == 2 {
			status = http.StatusNoContent
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	result := probeRemote(context.Background(), client, remoteCandidate{url: mustURL(t, "https://example.com/doc"), origins: []Reference{remoteRef("https://example.com/doc")}}, RemoteOptions{Policy: RemotePublic}, nil)
	if attempts != 2 || len(result) != 0 {
		t.Fatalf("attempts=%d result=%#v", attempts, result)
	}
}

func TestPublicAddressPolicy(t *testing.T) {
	for _, test := range []struct {
		address string
		want    bool
	}{
		{"8.8.8.8", true}, {"127.0.0.1", false}, {"10.0.0.1", false}, {"100.64.0.1", false}, {"192.0.2.1", false}, {"::1", false}, {"fc00::1", false},
	} {
		address := netip.MustParseAddr(test.address)
		if got := publicAddress(address); got != test.want {
			t.Errorf("publicAddress(%s) = %v, want %v", address, got, test.want)
		}
	}
}

func TestRemoteFailurePolicy(t *testing.T) {
	if !remoteFailure("gone", "gone") || remoteFailure("inconclusive", "gone") || !remoteFailure("inconclusive", "all") || remoteFailure("gone", "none") {
		t.Fatal("unexpected remote failure policy")
	}
}

func TestRemoteInvocationRequiresPolicy(t *testing.T) {
	var out, err strings.Builder
	if exit := Main(context.Background(), []string{"--check-remote", "bundle"}, &out, &err); exit != 2 {
		t.Fatalf("exit = %d", exit)
	}
}

func TestRemoteTimeoutDefaultsAreUsable(t *testing.T) {
	if (RemoteOptions{Timeout: 10 * time.Second, Workers: 4}).Timeout <= 0 {
		t.Fatal("invalid timeout")
	}
}
