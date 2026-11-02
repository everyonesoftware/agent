package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistrationCreatesLocalAccountAndSession(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	handler := newServer()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"alice","password":"correct-horse-battery"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body %s; want %d", response.Code, response.Body.String(), http.StatusCreated)
	}
	if len(response.Result().Cookies()) == 0 {
		t.Fatal("registration did not set a session cookie")
	}
}

func TestUnauthenticatedChatIsRejected(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	handler := newServer()
	request := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated chat status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
