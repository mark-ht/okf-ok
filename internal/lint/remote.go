package lint

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RemotePolicy string

const (
	RemoteOff       RemotePolicy = "off"
	RemoteAllowlist RemotePolicy = "allowlist"
	RemotePublic    RemotePolicy = "public"
)

type RemoteOptions struct {
	Enabled  bool
	Policy   RemotePolicy
	Hosts    []string
	Workers  int
	Timeout  time.Duration // Per unique URL, including its retry.
	Deadline time.Duration // Whole remote-check phase.
	MaxURLs  int
}

type remoteCandidate struct {
	url     *url.URL
	origins []Reference
}

func remoteDiagnostics(ctx context.Context, refs []Reference, options RemoteOptions) []Diagnostic {
	if !options.Enabled {
		return nil
	}
	deadline := options.Deadline
	if deadline <= 0 {
		deadline = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	candidates, diagnostics := remoteCandidates(refs, options)
	if len(candidates) == 0 {
		return diagnostics
	}
	workers := options.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(candidates) {
		workers = len(candidates)
	}
	client := &http.Client{Timeout: options.Timeout, Transport: safeTransport(), CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	jobs := make(chan remoteCandidate)
	results := make(chan []Diagnostic, len(candidates))
	gate := &remoteGate{interval: 250 * time.Millisecond} // four requests/second across the invocation
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				candidateCtx, cancel := context.WithTimeout(ctx, options.Timeout)
				results <- probeRemote(candidateCtx, client, candidate, options, gate)
				cancel()
			}
		}()
	}
	for _, candidate := range candidates {
		jobs <- candidate
	}
	close(jobs)
	wg.Wait()
	close(results)
	for result := range results {
		diagnostics = append(diagnostics, result...)
	}
	return diagnostics
}

func remoteCandidates(refs []Reference, options RemoteOptions) ([]remoteCandidate, []Diagnostic) {
	byURL := map[string]*remoteCandidate{}
	var diagnostics []Diagnostic
	for _, ref := range refs {
		parsed, err := url.Parse(strings.TrimSpace(ref.Target))
		if err != nil {
			continue
		}
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			continue
		}
		if !permittedRemoteURL(parsed, options) {
			diagnostics = append(diagnostics, remoteDiagnostic("OKF203", SeverityInfo, ref, "policy_skipped", "remote URL is not allowed by the configured policy"))
			continue
		}
		parsed.Host = strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
		parsed.Fragment = ""
		key := parsed.String()
		candidate := byURL[key]
		if candidate == nil {
			candidate = &remoteCandidate{url: parsed}
			byURL[key] = candidate
		}
		candidate.origins = append(candidate.origins, ref)
	}
	keys := make([]string, 0, len(byURL))
	for key := range byURL {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	limit := options.MaxURLs
	if limit <= 0 {
		limit = 100
	}
	out := make([]remoteCandidate, 0, min(limit, len(keys)))
	for index, key := range keys {
		candidate := byURL[key]
		if index >= limit {
			for _, ref := range candidate.origins {
				diagnostics = append(diagnostics, remoteDiagnostic("OKF203", SeverityInfo, ref, "policy_skipped", "remote request budget exceeded"))
			}
			continue
		}
		out = append(out, *candidate)
	}
	return out, diagnostics
}

func allowedHost(host string, options RemoteOptions) bool {
	if options.Policy == RemotePublic {
		return true
	}
	if options.Policy != RemoteAllowlist {
		return false
	}
	for _, allowed := range options.Hosts {
		if host == strings.TrimSuffix(strings.ToLower(allowed), ".") {
			return true
		}
	}
	return false
}

func probeRemote(ctx context.Context, client *http.Client, candidate remoteCandidate, options RemoteOptions, gate *remoteGate) []Diagnostic {
	for attempt := 0; attempt < 2; attempt++ {
		if !gate.wait(ctx) {
			return probeOutcome(candidate, "inconclusive", 0, "remote request was cancelled or exceeded its deadline")
		}
		response, err := remoteRequest(ctx, client, candidate.url)
		if err != nil {
			if attempt == 0 && waitRetry(ctx, 50*time.Millisecond) {
				continue
			}
			return probeOutcome(candidate, "inconclusive", 0, "remote request was inconclusive")
		}
		status := response.StatusCode
		if (status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout) && attempt == 0 {
			response.Body.Close()
			if waitRetry(ctx, retryAfter(response.Header.Get("Retry-After"))) {
				continue
			}
			return probeOutcome(candidate, "inconclusive", status, "remote URL could not be confirmed from this environment")
		}
		response.Body.Close()
		switch {
		case status >= 200 && status < 300:
			return nil
		case status >= 300 && status < 400:
			location, err := url.Parse(response.Header.Get("Location"))
			if err != nil || response.Header.Get("Location") == "" || !permittedRemoteURL(candidate.url.ResolveReference(location), options) {
				return probeOutcome(candidate, "policy_skipped", status, "redirect destination is not allowed by the configured policy")
			}
			return probeOutcome(candidate, "redirected", status, "remote URL redirects; its stored location may be stale")
		case status == http.StatusNotFound || status == http.StatusGone:
			return probeOutcome(candidate, "gone", status, "remote URL returned a permanent not-found response")
		default:
			return probeOutcome(candidate, "inconclusive", status, "remote URL could not be confirmed from this environment")
		}
	}
	return probeOutcome(candidate, "inconclusive", 0, "remote URL could not be confirmed from this environment")
}

func remoteRequest(ctx context.Context, client *http.Client, target *url.URL) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, target.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "okflint/0")
	response, err := client.Do(request)
	if err != nil || (response.StatusCode != http.StatusMethodNotAllowed && response.StatusCode != http.StatusNotImplemented) {
		return response, err
	}
	response.Body.Close()
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Range", "bytes=0-0")
	request.Header.Set("User-Agent", "okflint/0")
	return client.Do(request)
}

type remoteGate struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func (gate *remoteGate) wait(ctx context.Context) bool {
	if gate == nil {
		return true
	}
	gate.mu.Lock()
	now := time.Now()
	if gate.next.Before(now) {
		gate.next = now
	}
	at := gate.next
	gate.next = gate.next.Add(gate.interval)
	gate.mu.Unlock()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func retryAfter(value string) time.Duration {
	const maxRetryAfter = 30 * time.Second
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		delay := time.Duration(seconds) * time.Second
		if delay > maxRetryAfter {
			return maxRetryAfter
		}
		return delay
	}
	if at, err := http.ParseTime(value); err == nil {
		delay := time.Until(at)
		if delay < 0 {
			return 0
		}
		if delay > maxRetryAfter {
			return maxRetryAfter
		}
		return delay
	}
	return 50 * time.Millisecond
}

func waitRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func permittedRemoteURL(parsed *url.URL, options RemoteOptions) bool {
	if parsed == nil || strings.ToLower(parsed.Scheme) != "https" || parsed.Host == "" || parsed.User != nil || parsed.Port() != "" && parsed.Port() != "443" || net.ParseIP(parsed.Hostname()) != nil {
		return false
	}
	return allowedHost(strings.TrimSuffix(strings.ToLower(parsed.Hostname()), "."), options)
}

func probeOutcome(candidate remoteCandidate, outcome string, status int, message string) []Diagnostic {
	code, severity := "OKF200", SeverityInfo
	if outcome == "gone" {
		code, severity = "OKF201", SeverityWarning
	}
	if outcome == "redirected" {
		code, severity = "OKF202", SeverityWarning
	}
	if outcome == "policy_skipped" {
		code, severity = "OKF203", SeverityInfo
	}
	out := make([]Diagnostic, 0, len(candidate.origins))
	for _, ref := range candidate.origins {
		d := remoteDiagnostic(code, severity, ref, outcome, message)
		d.Resolved = candidate.url.String()
		if status != 0 {
			d.Message = fmt.Sprintf("%s (HTTP %d)", message, status)
		}
		out = append(out, d)
	}
	return out
}

func remoteDiagnostic(code string, severity Severity, ref Reference, outcome, message string) Diagnostic {
	d := diagnostic(code, severity, ref.Location, ref, message)
	d.Outcome = outcome
	return d
}

func safeTransport() *http.Transport {
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            safeDialContext,
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 32 << 10,
	}
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return nil, fmt.Errorf("unsafe remote address")
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("remote hostname could not be resolved")
	}
	for _, ip := range ips {
		if !publicAddress(ip) {
			return nil, fmt.Errorf("remote hostname resolves to a non-public address")
		}
	}
	// Dial a validated address directly so the transport cannot resolve it again.
	return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	if address.Is4() {
		ip := address.As4()
		return !(ip[0] == 100 && ip[1]&0xc0 == 0x40) && ip[0] != 0 && ip[0] != 127 && ip[0] < 224 && !(ip[0] == 192 && ip[1] == 0 && ip[2] == 2) && !(ip[0] == 198 && (ip[1] == 51 || ip[1] == 18)) && !(ip[0] == 203 && ip[1] == 0 && ip[2] == 113)
	}
	return true // IPv6 ULA addresses were rejected by Addr.IsPrivate above.
}
