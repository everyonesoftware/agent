package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunResponsesCapturesOnlyTextAndUsage(t *testing.T) {
	c := &runCapture{}
	ctx := context.WithValue(context.Background(), runContextKey{}, c)
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: http.NoBody, Request: httptest.NewRequest("POST", "/responses", nil).WithContext(ctx)}
	response.Body = newTestReadCloser(`data: {"type":"response.reasoning.delta","delta":"PRIVATE REASONING"}

data: {"type":"response.output_text.delta","delta":"safe answer"}

data: {"type":"response.completed","response":{"usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11},"private":"not retained"}}

`)
	text, err := collectResponseText(response)
	if err != nil || text != "safe answer" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	if c.run.Usage == nil || c.run.Usage.TotalTokens != 11 {
		t.Fatalf("usage not captured: %#v", c.run.Usage)
	}
	b, _ := json.Marshal(c.run)
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "not retained") {
		t.Fatal("private stream retained")
	}
}

type testReadCloser struct{ *strings.Reader }

func (t testReadCloser) Close() error           { return nil }
func newTestReadCloser(s string) testReadCloser { return testReadCloser{strings.NewReader(s)} }

func TestRunResponsesActualProviderPayload(t *testing.T) {
	var payload struct {
		Model string        `json:"model"`
		Input []chatMessage `json:"input"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Write([]byte(`{"data":[{"id":"oauth-model","visibility":"list"}]}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&payload)
		w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"safe answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":8,\"output_tokens\":3,\"total_tokens\":11}}}\n\n"))
	}))
	defer server.Close()
	store, err := newAccountStore(t.TempDir() + "/accounts.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	user, token, err := store.register("oauth-owner", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	creds := newFileCredentialStore(t.TempDir())
	if err = creds.Save(testOAuthCredentials("PRIVATE-ACCESS", "PRIVATE-REFRESH", time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	service := newSIWCService(siwcConfig{apiBaseURL: server.URL}, creds)
	service.client = server.Client()
	p := &responsesProvider{auth: service}
	request := httptest.NewRequest("POST", "/api/chat", strings.NewReader(`{"omitted_messages":7,"messages":[{"role":"user","content":"question"}]}`))
	request.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
	w := httptest.NewRecorder()
	store.middleware(chatHandlerWithStore(p, store)).ServeHTTP(w, request)
	if w.Code != 200 {
		t.Fatalf("chat %d %s", w.Code, w.Body)
	}
	if payload.Model != "oauth-model" || len(payload.Input) != 2 || payload.Input[0].Role != "system" || !strings.Contains(payload.Input[0].Content, time.Now().Format("2006-01-02")) {
		t.Fatalf("payload=%#v", payload)
	}
	r := httptest.NewRequest("GET", "/api/runs/"+w.Header().Get("X-Agent-Run-ID"), nil)
	r.SetPathValue("id", w.Header().Get("X-Agent-Run-ID"))
	r = r.WithContext(context.WithValue(r.Context(), authContextKey{}, user))
	got := httptest.NewRecorder()
	store.runHandler(got, r)
	run := decodeRunMap(t, got)
	if run["provider"] != "chatgpt" || run["model"] != "oauth-model" || run["request"] == nil || run["context"].(map[string]any)["omitted_messages"] != float64(7) {
		t.Fatalf("run=%#v", run)
	}
	if strings.Contains(got.Body.String(), "PRIVATE-") {
		t.Fatal("credential captured")
	}
}
