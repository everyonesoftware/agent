package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok\n" {
		t.Fatalf("body = %q, want %q", got, "ok\n")
	}
}

func TestHomePageOffersAChatComposer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `id="chat-form"`) || !strings.Contains(rec.Body.String(), `id="message-input"`) || !strings.Contains(rec.Body.String(), `id="connect-chatgpt"`) || !strings.Contains(rec.Body.String(), `id="model-select"`) || !strings.Contains(rec.Body.String(), `id="account-select"`) || !strings.Contains(rec.Body.String(), `id="add-chatgpt-account"`) {
		t.Fatalf("home page does not contain the chat composer and account management controls")
	}
}

func TestVersionEndpointReturnsBuildVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "dev\n" {
		t.Fatalf("body = %q, want %q", got, "dev\n")
	}
}
