package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountRegistrationFromSameOrigin(t *testing.T) {
	for _, tc := range []struct{ name, target, origin, site string }{
		{"LAN", "http://192.168.1.20:8080", "http://192.168.1.20:8080", ""},
		{"HTTPS", "https://agent.example.test", "https://agent.example.test", "same-origin"},
		{"TLS proxy", "http://127.0.0.1:8080", "https://agent.example.test", "same-origin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_DATA_DIR", t.TempDir())
			handler := newServer()
			request := httptest.NewRequest(http.MethodPost, tc.target+"/api/auth/register", strings.NewReader(`{"username":"alice","password":"correct-horse-battery"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("Sec-Fetch-Site", tc.site)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusCreated {
				t.Fatalf("same-origin registration: status %d, body %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCrossSiteAccountErrorsAreJSON(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	handler := newServer()
	for _, path := range []string{"/api/auth/register", "/api/auth/login", "/api/auth/logout"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://localhost:8080"+path, nil)
			request.Header.Set("Origin", "https://evil.example")
			request.Header.Set("Sec-Fetch-Site", "cross-site")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			var result struct {
				Error string `json:"error"`
			}
			if response.Code != http.StatusForbidden || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Error == "" || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("want JSON forbidden error; got %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestRequestIsCrossSiteBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, origin, site string
		blocked            bool
	}{
		{"same host", "http://localhost:8080", "", false},
		{"other port", "http://localhost:9090", "", true},
		{"other loopback host", "http://127.0.0.1:8080", "", true},
		{"foreign host", "https://evil.example", "", true},
		{"null origin", "null", "", true},
		{"malformed origin", "://", "", true},
		{"same-site is not same-origin", "http://localhost:8080", "same-site", true},
		{"cross-site metadata", "http://localhost:8080", "cross-site", true},
		{"no browser headers", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/auth/login", nil)
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("Sec-Fetch-Site", tc.site)
			if got := requestIsCrossSite(request); got != tc.blocked {
				t.Fatalf("blocked = %v, want %v", got, tc.blocked)
			}
		})
	}
}
