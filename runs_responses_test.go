package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	f, p, _, audit := codexInferenceFixture(t, "chat")
	store, user := f.accounts, f.user
	request := httptest.NewRequest("POST", "/api/chat", strings.NewReader(`{"omitted_messages":7,"messages":[{"role":"user","content":"question"}]}`))
	request.AddCookie(f.cookie)
	w := httptest.NewRecorder()
	store.middleware(chatHandlerWithStore(p, store)).ServeHTTP(w, request)
	if w.Code != 200 {
		t.Fatalf("chat %d %s", w.Code, w.Body)
	}
	wire, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	var thread map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(wire)), "\n") {
		var call struct {
			Method string
			Params map[string]any
		}
		if json.Unmarshal([]byte(line), &call) != nil {
			t.Fatal("invalid RPC audit")
		}
		if call.Method == "turn/start" {
			payload = call.Params
		}
		if call.Method == "thread/start" {
			thread = call.Params
		}
	}
	if payload == nil || thread["model"] != "codex-fixture" || !strings.Contains(string(wire), time.Now().Format("2006-01-02")) || !strings.Contains(string(wire), "question") {
		t.Fatalf("payload=%#v wire=%s", payload, wire)
	}
	r := httptest.NewRequest("GET", "/api/runs/"+w.Header().Get("X-Agent-Run-ID"), nil)
	r.SetPathValue("id", w.Header().Get("X-Agent-Run-ID"))
	r = r.WithContext(context.WithValue(r.Context(), authContextKey{}, user))
	got := httptest.NewRecorder()
	store.runHandler(got, r)
	run := decodeRunMap(t, got)
	if run["provider"] != "chatgpt" || run["model"] != "codex-fixture" || run["request"] == nil || run["context"].(map[string]any)["omitted_messages"] != float64(7) {
		t.Fatalf("run=%#v", run)
	}
	if strings.Contains(got.Body.String(), "PRIVATE-") {
		t.Fatal("credential captured")
	}
	actual, _ := json.Marshal(run["request"])
	var normalized struct {
		Model    string
		Messages []chatMessage
	}
	if json.Unmarshal(actual, &normalized) != nil || normalized.Model != thread["model"] || len(normalized.Messages) != 2 || normalized.Messages[0].Role != "system" || !strings.Contains(thread["baseInstructions"].(string), normalized.Messages[0].Content) || normalized.Messages[1].Content != "question" || run["usage"].(map[string]any)["total_tokens"] != float64(36) {
		t.Fatalf("trace differs from Codex request/usage: %s", got.Body)
	}
}
