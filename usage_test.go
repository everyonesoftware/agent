package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUsageRoutesRequireAgentSignIn(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	server := newServer()
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/usage/status"},
		{"POST", "/api/usage/connect"},
		{"POST", "/api/usage/cancel"},
		{"POST", "/api/usage/refresh"},
		{"POST", "/api/usage/disconnect"},
	} {
		out := httptest.NewRecorder()
		server.ServeHTTP(out, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
		if out.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want authenticated-only 401", route.method, route.path, out.Code)
		}
	}
}

func TestUsageStatusChecksActiveProfileAndOwnership(t *testing.T) {
	f := newConnectionFixture(t)
	for _, profile := range []string{"", "../other", "profile_abcdefghijklmnopqrstuv"} {
		out := f.request("GET", "/api/usage/status?profile_id="+profile, "", "agent.example.test")
		if out.Code != http.StatusBadRequest && out.Code != http.StatusConflict {
			t.Errorf("invalid/nonactive profile returned %d", out.Code)
		}
	}
	out := f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"profile_id":"`+f.id+`"`) || !strings.Contains(out.Body.String(), `"state":"disconnected"`) {
		t.Fatalf("owned profile: %d %s", out.Code, out.Body.String())
	}
	if out.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Usage identity may be cached")
	}
}

func TestUsageRejectsCrossSiteAndUnboundedMutations(t *testing.T) {
	f := newConnectionFixture(t)
	for _, path := range []string{"connect", "cancel", "refresh", "disconnect"} {
		req := httptest.NewRequest("POST", "/api/usage/"+path, strings.NewReader(`{}`))
		req.Host = "agent.example.test"
		req.Header.Set("Origin", "https://evil.example.test")
		req.AddCookie(f.cookie)
		out := httptest.NewRecorder()
		f.server.ServeHTTP(out, req)
		if out.Code != 403 {
			t.Errorf("cross-site %s = %d", path, out.Code)
		}
	}
	for _, body := range []string{`null`, `{} {}`, `{"profile_id":"../other"}`, strings.Repeat("x", 5000)} {
		out := f.request("POST", "/api/usage/connect", body, "agent.example.test")
		if out.Code != 400 && out.Code != 413 {
			t.Errorf("invalid payload: %d", out.Code)
		}
	}
}
