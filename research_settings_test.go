package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const settingsTestKey = "synthetic-Brave-key-1234"

type settingsFixtureTransport struct {
	target    string
	transport http.RoundTripper
}

func (f settingsFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Keep production URL/header creation intact, redirect only inside this fixture.
	if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "api.search.brave.com" || r.URL.Path != "/res/v1/web/search" || r.URL.Query().Get("count") != "5" || r.URL.Query().Get("text_decorations") != "false" || r.URL.Query().Get("api_key") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
		return nil, fmt.Errorf("unexpected Brave request boundary")
	}
	c := r.Clone(r.Context())
	u := *r.URL
	c.URL = &u
	target := strings.TrimPrefix(f.target, "http://")
	c.URL.Scheme = "http"
	c.URL.Host = target
	return f.transport.RoundTrip(c)
}
func settingsHarness(t *testing.T, handler http.HandlerFunc) (*accountStore, *researchService, http.Handler, *http.Cookie, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	a, e := newAccountStore(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.db.Close() })
	_, token, e := a.register("settings-user", "synthetic-password")
	if e != nil {
		t.Fatal(e)
	}
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	s := newAccountResearchService(a)
	s.settings.webFactory = func(key string) *researchWeb {
		return &researchWeb{apiKey: key, searchTransport: settingsFixtureTransport{upstream.URL, upstream.Client().Transport}}
	}
	m := http.NewServeMux()
	s.register(m, a)
	return a, s, m, &http.Cookie{Name: authCookieName, Value: token}, path
}
func settingsResult(t *testing.T, w *httptest.ResponseRecorder, code int) researchSettings {
	t.Helper()
	if w.Code != code {
		t.Fatalf("status %d want %d: %s", w.Code, code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	if strings.Contains(w.Body.String(), settingsTestKey) {
		t.Fatal("credential exposed")
	}
	var v struct {
		Settings researchSettings `json:"settings"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return v.Settings
}
func settingsBody(key, rev string) string {
	b, _ := json.Marshal(map[string]string{"api_key": key, "expected_revision": rev})
	return string(b)
}
func settingsRevision(rev string) string {
	b, _ := json.Marshal(map[string]string{"expected_revision": rev})
	return string(b)
}
func TestResearchSettingsLifecyclePersistenceAndIsolation(t *testing.T) {
	var calls atomic.Int32
	a, _, h, c, path := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Subscription-Token") != settingsTestKey || r.URL.Query().Get("q") != "Agent connection test" {
			t.Error("wrong verification request")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"web":{"results":[]}}`)
	})
	v := settingsResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
	if v.Configured || v.Revision != "" || v.Status != "not_configured" || v.Provider != "Brave Search" || calls.Load() != 0 {
		t.Fatalf("initial: %+v", v)
	}
	v = settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	if !v.Configured || v.KeySuffix != "1234" || v.Status != "connected" || v.VerifiedAt == "" || v.LastCheckedAt == "" || v.Revision == "" || calls.Load() != 1 {
		t.Fatalf("saved: %+v", v)
	}
	b, e := newAccountStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer b.db.Close()
	stored, e := b.researchCredentialForUser(mustSettingsUser(t, a, c).ID)
	if e != nil || stored.apiKey != settingsTestKey || stored.Revision != v.Revision {
		t.Fatal("not persisted", e)
	}
	_, token, e := a.register("settings-other", "synthetic-password")
	if e != nil {
		t.Fatal(e)
	}
	other := &http.Cookie{Name: authCookieName, Value: token}
	ov := settingsResult(t, researchRequest(h, other, "GET", "/api/research/settings", ""), 200)
	if ov.Configured {
		t.Fatal("cross-owner key")
	}
	settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/remove", settingsRevision("stale-revision")), 409)
	settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/remove", settingsRevision("")), 409)
	removed := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/remove", settingsRevision(v.Revision)), 200)
	if removed.Configured || removed.Revision == "" || removed.Revision == v.Revision || removed.KeySuffix != "" {
		t.Fatal("missing tombstone", removed)
	}
	settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, v.Revision)), 409)
	if calls.Load() != 1 {
		t.Fatal("stale mutation made network call")
	}
}
func mustSettingsUser(t *testing.T, a *accountStore, c *http.Cookie) authUser {
	t.Helper()
	u, e := a.userForToken(c.Value)
	if e != nil {
		t.Fatal(e)
	}
	return u
}
func TestResearchSettingsFailurePreservesKeyAndCheckPersists(t *testing.T) {
	var status atomic.Int32
	status.Store(200)
	a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(status.Load()))
		fmt.Fprint(w, `{"web":{"results":[]},"secret":"`+settingsTestKey+`"}`)
	})
	saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	for _, tc := range []struct {
		up, want int
		code     string
	}{{401, 422, "invalid_key"}, {403, 422, "invalid_key"}, {429, 429, "rate_limited"}, {500, 503, "unavailable"}} {
		status.Store(int32(tc.up))
		w := researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody("synthetic-replacement-9999", saved.Revision))
		settingsResult(t, w, tc.want)
		if !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatal(w.Body.String())
		}
	}
	settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody("synthetic-replacement-9999", saved.Revision)), 429)
	stored, e := a.researchCredentialForUser(mustSettingsUser(t, a, c).ID)
	if e != nil || stored.apiKey != settingsTestKey || stored.Revision != saved.Revision {
		t.Fatal("failed replacement changed key", e)
	}
	s.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	status.Store(401)
	failed := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/check", settingsRevision(saved.Revision)), 422)
	if failed.Status != "invalid" || failed.Revision != saved.Revision || !failed.Configured {
		t.Fatal("check status", failed)
	}
	got := settingsResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
	if got.Status != "invalid" || got.LastCheckedAt == "" {
		t.Fatal("check not persisted", got)
	}
}
func TestResearchSettingsInputAndAuthentication(t *testing.T) {
	var calls atomic.Int32
	_, _, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("invalid input reached upstream") })
	for _, path := range []string{"/api/research/settings", "/api/research/settings/verify", "/api/research/settings/check", "/api/research/settings/remove"} {
		method := "POST"
		if path == "/api/research/settings" {
			method = "GET"
		}
		settingsResult(t, researchRequest(h, nil, method, path, `{}`), 401)
	}
	for _, body := range []string{`{}`, `{"api_key":"synthetic-Brave-key-1234"}`, `{"api_key":"synthetic-Brave-key-1234","expected_revision":null}`, settingsBody(" short ", ""), settingsBody("synthetic-key-\ncontrol", ""), settingsBody(strings.Repeat("x", 257), ""), settingsBody(settingsTestKey, "") + ` {}`, `{"api_key":"synthetic-Brave-key-1234","expected_revision":"","unknown":1}`, strings.Repeat("x", 4097)} {
		settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", body), 400)
	}
	r := httptest.NewRequest("POST", "/api/research/settings/verify", strings.NewReader(settingsBody(settingsTestKey, "")))
	r.AddCookie(c)
	r.Header.Set("Origin", "https://evil.invalid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	settingsResult(t, w, 403)
	settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify?api_key="+settingsTestKey, settingsBody(settingsTestKey, "")), 400)
	if calls.Load() != 0 {
		t.Fatal("unexpected network")
	}
}
