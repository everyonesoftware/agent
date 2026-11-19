package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

// These acceptance checks exercise native HTTP clients' cookie-session contract
// against the production router and SQLite, without a browser or live provider.
func TestNativeClientSessionConversationAndPasswordRoundTrip(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_API_KEY", "")
	router := newServer()
	if server, ok := router.(*applicationServer); ok {
		t.Cleanup(server.close)
	}
	server := httptest.NewTLSServer(router)
	defer server.Close()
	client := &http.Client{Transport: server.Client().Transport}
	client.Jar, _ = cookiejar.New(nil)
	otherDevice := &http.Client{Transport: server.Client().Transport}
	otherDevice.Jar, _ = cookiejar.New(nil)

	var account authUser
	nativeContractRequest(t, client, server.URL, http.MethodPost, "/api/auth/register", map[string]string{"username": "native-alice", "password": "native-example-password"}, http.StatusCreated, &account)
	if account.ID == "" || account.Username != "native-alice" {
		t.Fatal("registration omitted account identity")
	}
	nativeContractRequest(t, client, server.URL, http.MethodGet, "/api/auth/me", nil, http.StatusOK, nil)
	nativeContractRequest(t, otherDevice, server.URL, http.MethodPost, "/api/auth/login", map[string]string{"username": "native-alice", "password": "native-example-password"}, http.StatusOK, nil)

	// Explicit /remember is server-local: this tests the actual chat persistence
	// path without a fake model reply or an outbound model request.
	var reply map[string]string
	nativeContractRequest(t, client, server.URL, http.MethodPost, "/api/chat", map[string]any{"messages": []chatMessage{{Role: "user", Content: "/remember I prefer clear practical answers."}}, "model": ""}, http.StatusOK, &reply)
	id, run := reply["conversation_id"], reply["run_id"]
	if id == "" || run == "" || !strings.Contains(reply["reply"], "Saved to your local user memory") {
		t.Fatal("chat did not return its acknowledged saved identities")
	}
	var list struct {
		Conversations []savedConversation `json:"conversations"`
	}
	nativeContractRequest(t, otherDevice, server.URL, http.MethodGet, "/api/conversations", nil, http.StatusOK, &list)
	if len(list.Conversations) != 1 || list.Conversations[0].ID != id {
		t.Fatal("second device did not see the acknowledged server conversation")
	}
	var messages struct {
		Messages []chatMessage `json:"messages"`
	}
	nativeContractRequest(t, otherDevice, server.URL, http.MethodGet, "/api/conversations/"+id, nil, http.StatusOK, &messages)
	if len(messages.Messages) != 2 || messages.Messages[0].Role != "user" || messages.Messages[1].Content != reply["reply"] {
		t.Fatal("conversation roundtrip lost roles or content")
	}
	nativeContractRequest(t, otherDevice, server.URL, http.MethodGet, "/api/runs/"+run, nil, http.StatusOK, nil)
	var memory MemorySnapshot
	nativeContractRequest(t, client, server.URL, http.MethodGet, "/api/memories", nil, http.StatusOK, &memory)
	if len(memory.Entries) != 1 || memory.Entries[0].Content != "I prefer clear practical answers." {
		t.Fatal("explicit saved memory was not returned")
	}

	// The current client must consume the rotated Set-Cookie; the other device's
	// preexisting token is revoked. A client cannot keep its old token after reset.
	nativeContractRequest(t, client, server.URL, http.MethodPost, "/api/auth/password", map[string]string{"current_password": "native-example-password", "new_password": "replacement-example-password"}, http.StatusOK, nil)
	nativeContractRequest(t, client, server.URL, http.MethodGet, "/api/auth/me", nil, http.StatusOK, nil)
	nativeContractRequest(t, otherDevice, server.URL, http.MethodGet, "/api/auth/me", nil, http.StatusUnauthorized, nil)
	nativeContractRequest(t, otherDevice, server.URL, http.MethodPost, "/api/auth/login", map[string]string{"username": "native-alice", "password": "native-example-password"}, http.StatusUnauthorized, nil)
	nativeContractRequest(t, otherDevice, server.URL, http.MethodPost, "/api/auth/login", map[string]string{"username": "native-alice", "password": "replacement-example-password"}, http.StatusOK, nil)
	nativeContractRequest(t, client, server.URL, http.MethodPost, "/api/auth/logout", map[string]string{}, http.StatusOK, nil)
	nativeContractRequest(t, client, server.URL, http.MethodGet, "/api/auth/me", nil, http.StatusUnauthorized, nil)
	nativeContractRequest(t, otherDevice, server.URL, http.MethodGet, "/api/conversations/"+id, nil, http.StatusOK, nil)
}

func TestNativeClientCannotReadAnotherUsersConversationOrRun(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_API_KEY", "")
	router := newServer()
	if app, ok := router.(*applicationServer); ok {
		t.Cleanup(app.close)
	}
	server := httptest.NewTLSServer(router)
	defer server.Close()
	a, b := &http.Client{Transport: server.Client().Transport}, &http.Client{Transport: server.Client().Transport}
	a.Jar, _ = cookiejar.New(nil)
	b.Jar, _ = cookiejar.New(nil)
	nativeContractRequest(t, a, server.URL, http.MethodPost, "/api/auth/register", map[string]string{"username": "native-owner", "password": "native-owner-password"}, http.StatusCreated, nil)
	nativeContractRequest(t, b, server.URL, http.MethodPost, "/api/auth/register", map[string]string{"username": "native-other", "password": "native-other-password"}, http.StatusCreated, nil)
	var reply map[string]string
	nativeContractRequest(t, a, server.URL, http.MethodPost, "/api/chat", map[string]any{"messages": []chatMessage{{Role: "user", Content: "/remember Private account fact."}}, "model": ""}, http.StatusOK, &reply)
	nativeContractRequest(t, b, server.URL, http.MethodGet, "/api/conversations/"+reply["conversation_id"], nil, http.StatusNotFound, nil)
	nativeContractRequest(t, b, server.URL, http.MethodGet, "/api/runs/"+reply["run_id"], nil, http.StatusNotFound, nil)
	var memory MemorySnapshot
	nativeContractRequest(t, b, server.URL, http.MethodGet, "/api/memories", nil, http.StatusOK, &memory)
	if len(memory.Entries) != 0 {
		t.Fatal("memory crossed account boundary")
	}
}

func nativeContractRequest(t *testing.T, client *http.Client, base, method, path string, payload any, status int, output any) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "AgentNativeContract/1")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != status {
		t.Fatalf("%s %s status=%d, want=%d", method, path, res.StatusCode, status)
	}
	if output != nil {
		if err := json.NewDecoder(res.Body).Decode(output); err != nil {
			t.Fatalf("%s %s invalid JSON response: %v", method, path, err)
		}
	} else {
		_, _ = io.Copy(io.Discard, res.Body)
	}
}
