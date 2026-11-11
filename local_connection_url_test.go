package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectionStatusLocalURLUsesConfiguredRedirectURI(t *testing.T) {
	for _, tt := range []struct {
		name, redirectURI, want string
	}{
		{"default", "", "http://127.0.0.1:8080"},
		{"published host port", "http://127.0.0.1:8182/auth/callback", "http://127.0.0.1:8182"},
		{"trimmed environment", "  http://127.0.0.1:9090/auth/callback  ", "http://127.0.0.1:9090"},
		{"invalid URI", "not-a-url", ""},
		{"remote host", "http://example.test:8182/auth/callback", ""},
		{"credentials", "http://user:password@127.0.0.1:8182/auth/callback", ""},
		{"unexpected path", "http://127.0.0.1:8182/other", ""},
		{"query", "http://127.0.0.1:8182/auth/callback?secret=value", ""},
		{"fragment", "http://127.0.0.1:8182/auth/callback#fragment", ""},
		{"missing port", "http://127.0.0.1/auth/callback", ""},
		{"zero port", "http://127.0.0.1:0/auth/callback", ""},
		{"invalid port", "http://127.0.0.1:70000/auth/callback", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SIWC_DATA_DIR", t.TempDir())
			t.Setenv("SIWC_REDIRECT_URI", tt.redirectURI)
			// Neither the container listener nor the remote request origin tells
			// us which loopback port is published by the deployment.
			t.Setenv("PORT", "7777")
			service := newSIWCServiceFromEnv()
			request := httptest.NewRequest(http.MethodGet, "https://agent.example.test/api/auth/status", nil)
			request.Header.Set("X-Forwarded-Host", "untrusted.example:1234")
			request.Header.Set("X-Forwarded-Proto", "https")
			response := httptest.NewRecorder()
			service.statusHandler(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			var status map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if got, ok := status["local_url"]; !ok || got != tt.want {
				t.Errorf("local_url = %#v (present %v), want %q", got, ok, tt.want)
			}
			for _, flag := range []string{"connected", "oauth_selected", "plan_usage_enabled"} {
				if status[flag] != false {
					t.Errorf("%s = %#v, want false for a new account", flag, status[flag])
				}
			}
		})
	}
}

func TestConnectionStatusLocalURLThroughAuthenticatedRouter(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("SIWC_DATA_DIR", t.TempDir())
	t.Setenv("SIWC_REDIRECT_URI", "http://127.0.0.1:8182/auth/callback")
	server := newServer()
	request := httptest.NewRequest(http.MethodGet, "https://agent.example.test/api/auth/status", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", response.Code)
	}
	registration := httptest.NewRecorder()
	server.ServeHTTP(registration, httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"local-url-test","password":"synthetic-test-password"}`)))
	if registration.Code != http.StatusCreated {
		t.Fatalf("registration = %d: %s", registration.Code, registration.Body.String())
	}
	request.AddCookie(registration.Result().Cookies()[0])
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	var status struct {
		LocalURL string `json:"local_url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || status.LocalURL != "http://127.0.0.1:8182" {
		t.Fatalf("authenticated status = %d, local_url = %q", response.Code, status.LocalURL)
	}
}
