package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The dial hook maps an already-validated PUBLIC pinned address to a local
// fixture. Production has no private-address bypass.
func researchFixture(t *testing.T, handler http.HandlerFunc) *researchWeb {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	w := &researchWeb{}
	w.lookupIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	w.dialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "93.184.216.34:80" {
			t.Errorf("not pinned: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, s.Listener.Addr().String())
	}
	return w
}

func TestResearchFetchRealHTTP(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	w := researchFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("unsafe request")
		}
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.Write([]byte(`<html><title>Example &amp; proof</title><body><h1>Hello</h1><script>evil()</script><style>hidden</style><p>Actual &lt;text&gt;</p></body></html>`))
	})
	out, err := w.fetch(context.Background(), "http://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ URL, Title, Content string }
	if err = json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.URL != "http://example.com/article" || got.Title != "Example & proof" || !strings.Contains(got.Content, "Actual <text>") || strings.Contains(got.Content, "evil") || strings.Contains(got.Content, "hidden") {
		t.Fatalf("wrong extraction: %s", out)
	}
}

func TestResearchFetchDNSPolicyAndPinning(t *testing.T) {
	for _, addr := range []string{"127.0.0.1", "10.0.0.2", "169.254.169.254", "::ffff:93.184.216.34", "2001:db8::1"} {
		t.Run(addr, func(t *testing.T) {
			w := &researchWeb{lookupIP: func(context.Context, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr(addr)}, nil
			}}
			w.dialContext = func(context.Context, string, string) (net.Conn, error) { t.Fatal("unsafe DNS dialed"); return nil, nil }
			if _, err := w.fetch(context.Background(), "http://example.com"); err == nil {
				t.Fatal("unsafe DNS allowed")
			}
		})
	}
	var lookups atomic.Int32
	w := researchFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/plain")
		rw.Write([]byte("pinned"))
	})
	w.lookupIP = func(context.Context, string) ([]netip.Addr, error) {
		if lookups.Add(1) > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	if _, err := w.fetch(context.Background(), "http://example.com"); err != nil {
		t.Fatal(err)
	}
	if lookups.Load() != 1 {
		t.Fatal("resolved again after validation")
	}
}

type researchRoundTrip func(*http.Request) (*http.Response, error)

func (f researchRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResearchBraveSearch(t *testing.T) {
	const fakeKey = "fixture-token-not-a-secret"
	s := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/res/v1/web/search" || r.URL.Query().Get("q") != "test query" || r.URL.Query().Get("count") != "5" || r.Header.Get("X-Subscription-Token") != fakeKey {
			t.Error("incorrect Brave request")
		}
		rw.Header().Set("Content-Type", "application/json")
		rw.Write([]byte(`{"web":{"results":[{"title":"Actual fixture","url":"https://example.com/a","description":"A <b>snippet</b>"},{"url":"http://127.0.0.1/"},{"url":"file:///etc/passwd"}]}}`))
	}))
	defer s.Close()
	w := &researchWeb{apiKey: fakeKey}
	w.searchTransport = researchRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.search.brave.com" {
			t.Fatal("not official fixed endpoint")
		}
		clone := r.Clone(r.Context())
		u := *r.URL
		clone.URL = &u
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(s.URL, "http://")
		return http.DefaultTransport.RoundTrip(clone)
	})
	if !w.searchAvailable() || w.searchProvider() != "Brave Search" {
		t.Fatal("provider unavailable")
	}
	out, err := w.search(context.Background(), "test query")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Results []struct{ Title, URL, Description string }
	}
	if err = json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 1 || got.Results[0].URL != "https://example.com/a" || got.Results[0].Description != "A snippet" {
		t.Fatalf("incorrect search output: %s", out)
	}
	for _, q := range []string{"", strings.Repeat("a", 401)} {
		if _, err := w.search(context.Background(), q); err == nil {
			t.Fatal("unbounded query")
		}
	}
}

func TestResearchHTMLUnicodeIndices(t *testing.T) {
	title, content := researchHTML("<title>İ proof</title><p>İ text</p><script>ignored</script>")
	if title != "İ proof" || strings.TrimSpace(content) != "İ text" {
		t.Fatalf("Unicode extraction corrupted: %q / %q", title, content)
	}
}

func TestResearchFetchLimitsRedirectsCancel(t *testing.T) {
	tests := []struct {
		name, media, encoding, body, redirect string
		status                                int
		wantErr                               bool
	}{
		{name: "plain", media: "text/plain", body: "real fixture"},
		{name: "media", media: "application/pdf", body: "%PDF", wantErr: true},
		{name: "compressed", media: "text/html", encoding: "gzip", body: "compressed", wantErr: true},
		{name: "oversized", media: "text/plain", body: strings.Repeat("x", (1<<20)+1), wantErr: true},
		{name: "invalid utf8", media: "text/plain", body: string([]byte{255}), wantErr: true},
		{name: "private redirect", redirect: "http://127.0.0.1/", wantErr: true},
		{name: "credentials redirect", redirect: "http://user:secret@example.com/", wantErr: true},
		{name: "DNS redirect", redirect: "http://unsafe.example/", wantErr: true},
		{name: "loop", redirect: "/loop", wantErr: true},
		{name: "status", status: 500, body: "SECRET_BODY", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits atomic.Int32
			w := researchFixture(t, func(rw http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if tt.redirect != "" {
					http.Redirect(rw, r, tt.redirect, 302)
					return
				}
				rw.Header().Set("Content-Type", tt.media)
				if tt.encoding != "" {
					rw.Header().Set("Content-Encoding", tt.encoding)
				}
				if tt.status != 0 {
					rw.WriteHeader(tt.status)
				}
				rw.Write([]byte(tt.body))
			})
			w.lookupIP = func(_ context.Context, h string) ([]netip.Addr, error) {
				if h == "unsafe.example" {
					return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
				}
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			}
			_, err := w.fetch(context.Background(), "http://example.com/")
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "SECRET_BODY")) {
				t.Fatal("error leaked data")
			}
			if tt.name == "loop" && hits.Load() != 4 {
				t.Fatalf("redirect count: %d", hits.Load())
			}
		})
	}
	w := researchFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/plain")
		rw.Write([]byte(strings.Repeat("é", 30000)))
	})
	out, err := w.fetch(context.Background(), "http://example.com")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Content string }
	json.Unmarshal([]byte(out), &got)
	if len([]rune(got.Content)) != 24000 {
		t.Fatal("text limit missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = w.fetch(ctx, "http://example.com"); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestResearchSearchFailuresAreBoundedAndSafe(t *testing.T) {
	for _, tt := range []struct {
		name, media, body string
		status            int
	}{
		{"media", "text/html", `{"web":{"results":[]}}`, 200},
		{"status", "application/json", "sensitive-provider-body", 401},
		{"malformed", "application/json", "not JSON sensitive-provider-body", 200},
		{"oversized", "application/json", strings.Repeat("x", (1<<20)+1), 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := &researchWeb{apiKey: "fixture-only-token", searchTransport: researchRoundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Header: http.Header{"Content-Type": {tt.media}}, Body: io.NopCloser(strings.NewReader(tt.body)), Request: r}, nil
			})}
			out, err := w.search(context.Background(), "query")
			if err == nil {
				t.Fatalf("accepted invalid response: %s", out)
			}
			if strings.Contains(err.Error(), "fixture-only-token") || strings.Contains(err.Error(), "sensitive-provider-body") {
				t.Fatal("error disclosed secret")
			}
		})
	}
}

func TestResearchFetchRedirectFinalURLAndNoCookies(t *testing.T) {
	w := researchFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("cookie retained")
		}
		if r.URL.Path == "/start" {
			rw.Header().Add("Set-Cookie", "private=session")
			http.Redirect(rw, r, "/final", 302)
			return
		}
		rw.Header().Set("Content-Type", "text/plain")
		rw.Write([]byte("final page"))
	})
	out, err := w.fetch(context.Background(), "http://example.com/start")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ URL string }
	json.Unmarshal([]byte(out), &got)
	if got.URL != "http://example.com/final" {
		t.Fatal("citation must use actual final URL")
	}
}

func TestResearchFetchSlowBodyCancellation(t *testing.T) {
	w := researchFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/plain")
		rw.WriteHeader(200)
		rw.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := w.fetch(ctx, "http://example.com/"); err == nil {
		t.Fatal("stalled body ignored cancellation")
	}
}

func TestResearchSearchUnavailable(t *testing.T) {
	t.Setenv("BRAVE_SEARCH_API_KEY", "synthetic-shared-key-must-be-ignored")
	w := newResearchWeb()
	if w.searchAvailable() || w.searchProvider() != "Unavailable (page retrieval only)" {
		t.Fatal("unconfigured search must be honest")
	}
	if _, err := w.search(context.Background(), "example"); err == nil {
		t.Fatal("expected unconfigured error")
	}
}

func TestResearchURLPolicy(t *testing.T) {
	bad := []string{"file:///etc/passwd", "ftp://example.com", "http://user:pass@example.com", "https://example.com/#frag", "http://example.com:8080", "http://localhost", "http://LOCALHOST.", "http://127.0.0.1", "http://10.0.0.1", "http://169.254.169.254", "http://100.100.100.200", "http://192.0.2.1", "http://198.18.0.1", "http://224.0.0.1", "http://[::1]", "http://[::ffff:8.8.8.8]", "http://[fc00::1]", "http://[fe80::1]", "http://[2001:db8::1]"}
	for _, raw := range bad {
		t.Run(raw, func(t *testing.T) {
			if _, err := researchURL(raw); err == nil {
				t.Fatal("allowed unsafe URL")
			}
		})
	}
	for _, raw := range []string{"https://example.com", "http://8.8.8.8:80/", "https://[2606:4700:4700::1111]/"} {
		if _, err := researchURL(raw); err != nil {
			t.Fatalf("valid URL rejected: %s: %v", raw, err)
		}
	}
}
