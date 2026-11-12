package main

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Resolver and dial hooks are internal test seams, never configured by environment.
type researchWeb struct {
	apiKey          string
	lookupIP        func(context.Context, string) ([]netip.Addr, error)
	dialContext     func(context.Context, string, string) (net.Conn, error)
	searchTransport http.RoundTripper // Internal fixture seam; never user-configurable.
}

const researchTimeout = 15 * time.Second

func (w *researchWeb) client() (*http.Client, *http.Transport) {
	tr := &http.Transport{
		Proxy: nil, DisableCompression: true, DisableKeepAlives: true,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 32 << 10,
	}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || (port != "80" && port != "443") {
			return nil, errors.New("blocked network destination")
		}
		var ips []netip.Addr
		if ip, e := netip.ParseAddr(host); e == nil {
			ips = []netip.Addr{ip}
		} else {
			lookup := w.lookupIP
			if lookup == nil {
				lookup = func(ctx context.Context, h string) ([]netip.Addr, error) {
					return net.DefaultResolver.LookupNetIP(ctx, "ip", h)
				}
			}
			ips, err = lookup(ctx, host)
			if err != nil {
				return nil, errors.New("public DNS lookup failed")
			}
		}
		if len(ips) == 0 || len(ips) > 32 {
			return nil, errors.New("invalid public DNS answer")
		}
		for _, ip := range ips {
			if !researchPublicIP(ip) {
				return nil, errors.New("blocked non-public DNS address")
			}
		}
		dial := w.dialContext
		if dial == nil {
			dial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
		}
		for _, ip := range ips {
			c, e := dial(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
			if ctx.Err() != nil {
				break
			}
		}
		return nil, errors.New("public connection failed")
	}
	client := &http.Client{Transport: tr, Timeout: researchTimeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("redirect limit exceeded")
		}
		if _, err := researchURL(req.URL.String()); err != nil {
			return err
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Del("X-Subscription-Token")
		return nil
	}}
	return client, tr
}

func (w *researchWeb) fetch(ctx context.Context, rawURL string) (string, error) {
	u, err := researchURL(rawURL)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, researchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", errors.New("invalid public request")
	}
	req.Header.Set("Accept", "text/html, text/plain")
	req.Header.Set("Accept-Encoding", "identity")
	client, tr := w.client()
	defer tr.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("public page request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("public page returned non-success status")
	}
	media, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || (media != "text/html" && media != "text/plain") {
		return "", errors.New("unsupported page media type")
	}
	if cs := strings.ToLower(params["charset"]); cs != "" && cs != "utf-8" && cs != "us-ascii" {
		return "", errors.New("unsupported page character encoding")
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return "", errors.New("compressed page responses are unsupported")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return "", errors.New("page body read failed")
	}
	if len(body) > 1<<20 {
		return "", errors.New("page exceeds byte limit")
	}
	if !utf8.Valid(body) {
		return "", errors.New("page is not valid UTF-8")
	}
	title, content := "", string(body)
	if media == "text/html" {
		title, content = researchHTML(content)
	}
	out := struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		Content string `json:"content"`
	}{resp.Request.URL.String(), researchText(title, 300), researchText(content, 24000)}
	data, _ := json.Marshal(out)
	return string(data), nil
}

func researchText(s string, limit int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r <= 32 || r == 127 }), " ")
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}

// This is deliberately a conservative text extractor, not a browser or DOM.
// Tags/attributes never enter output; raw-text and active containers are skipped.
func researchHTML(s string) (string, string) {
	var content, title strings.Builder
	inTitle := false
	// ASCII-only folding preserves byte offsets of non-ASCII page content.
	folded := []byte(s)
	for i, c := range folded {
		if c >= 'A' && c <= 'Z' {
			folded[i] = c + ('a' - 'A')
		}
	}
	lower := string(folded)
	for i := 0; i < len(s); {
		if s[i] != '<' {
			j := strings.IndexByte(s[i:], '<')
			if j < 0 {
				j = len(s) - i
			}
			piece := html.UnescapeString(s[i : i+j])
			if inTitle {
				title.WriteString(piece)
			} else {
				content.WriteString(piece)
			}
			i += j
			continue
		}
		if strings.HasPrefix(lower[i:], "<!--") {
			j := strings.Index(lower[i+4:], "-->")
			if j < 0 {
				break
			}
			i += 4 + j + 3
			continue
		}
		end := i + 1
		var quote byte
		for ; end < len(s); end++ {
			c := s[end]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
			} else if c == '\'' || c == '"' {
				quote = c
			} else if c == '>' {
				break
			}
		}
		if end >= len(s) {
			break
		}
		tag := strings.TrimSpace(lower[i+1 : end])
		fields := strings.Fields(tag)
		name := ""
		if len(fields) > 0 {
			name = fields[0]
		}
		name = strings.TrimSuffix(name, "/")
		i = end + 1
		skip := false
		for _, active := range []string{"script", "style", "iframe", "object", "svg", "template", "noscript"} {
			if name == active {
				j := strings.Index(lower[i:], "</"+active)
				if j < 0 {
					return title.String(), content.String()
				}
				i += j
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if name == "title" {
			inTitle = true
		} else if name == "/title" {
			inTitle = false
		}
		content.WriteByte(' ')
	}
	return title.String(), content.String()
}
func newResearchWeb() *researchWeb { return &researchWeb{} }

// Only status classification crosses the adapter boundary, never response bodies.
type researchSearchError struct{ status int }

func (e *researchSearchError) Error() string { return "Brave Search request failed" }
func (w *researchWeb) searchAvailable() bool { return w != nil && w.apiKey != "" }
func (w *researchWeb) searchProvider() string {
	if w.searchAvailable() {
		return "Brave Search"
	}
	return "Unavailable (page retrieval only)"
}

// Official contract: api-dashboard.search.brave.com/documentation/services/web-search
// The subscription token is sent only to the fixed API host; redirects are refused.
func (w *researchWeb) search(ctx context.Context, query string) (string, error) {
	if !w.searchAvailable() {
		return "", errors.New("web search unavailable: save a verified key in Research settings")
	}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 1600 || len([]rune(query)) > 400 {
		return "", errors.New("search query must be 1–400 characters")
	}
	ctx, cancel := context.WithTimeout(ctx, researchTimeout)
	defer cancel()
	u := "https://api.search.brave.com/res/v1/web/search?" + url.Values{"q": {query}, "count": {"5"}, "text_decorations": {"false"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", errors.New("invalid search request")
	}
	req.Header.Set("X-Subscription-Token", w.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	client, tr := w.client()
	defer tr.CloseIdleConnections()
	if w.searchTransport != nil {
		client.Transport = w.searchTransport
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("search redirects refused") }
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("Brave Search request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &researchSearchError{status: resp.StatusCode}
	}
	media, _, mediaErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaErr != nil || media != "application/json" {
		return "", errors.New("unsupported search media type")
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return "", errors.New("unsupported search encoding")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return "", errors.New("search response read failed or exceeded byte limit")
	}
	type result struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	var payload struct {
		Web struct {
			Results []result `json:"results"`
		} `json:"web"`
	}
	if err = json.Unmarshal(body, &payload); err != nil {
		return "", errors.New("invalid Brave Search response")
	}
	out := struct {
		Results []result `json:"results"`
	}{Results: make([]result, 0, 5)}
	for _, r := range payload.Web.Results {
		if _, err := researchURL(r.URL); err != nil {
			continue
		}
		_, title := researchHTML(r.Title)
		_, desc := researchHTML(r.Description)
		out.Results = append(out.Results, result{researchText(title, 300), r.URL, researchText(desc, 1200)})
		if len(out.Results) == 5 {
			break
		}
	}
	data, _ := json.Marshal(out)
	return string(data), nil
}

var researchDeniedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/3", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func researchPublicIP(a netip.Addr) bool {
	if !a.IsValid() || a.Is4In6() || a.Zone() != "" || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLinkLocalUnicast() {
		return false
	}
	// Only presently allocated global-unicast IPv6 space; excludes translation/local ranges.
	if a.Is6() && !netip.MustParsePrefix("2000::/3").Contains(a) {
		return false
	}
	for _, p := range researchDeniedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
func researchURL(raw string) (*url.URL, error) {
	reject := errors.New("public HTTP(S) URL required (ports 80/443, no credentials or fragments)")
	if len(raw) > 8192 || strings.Contains(raw, "#") {
		return nil, reject
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, reject
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		return nil, reject
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if h == "" || h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") || strings.Contains(h, "%") {
		return nil, reject
	}
	if a, e := netip.ParseAddr(h); e == nil {
		if !researchPublicIP(a) {
			return nil, reject
		}
	} else {
		if !strings.Contains(h, ".") {
			return nil, reject
		}
		for _, r := range h {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
				return nil, reject
			}
		}
	}
	return u, nil
}
