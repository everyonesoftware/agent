package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResearchTraceBoundedAcrossSixTools(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 6 {
			fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"call_%d","type":"function","function":{"name":"web_fetch","arguments":"{\"url\":\"https://example.com/\"}"}}]}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`, n)
		} else {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": strings.Repeat("\x01", 20000)}}}, "usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3}})
		}
	}))
	defer upstream.Close()
	web := &researchFixtureWeb{fetchFn: func(context.Context, string) (string, error) { return strings.Repeat("\x01", 24000), nil }}
	h, c, _ := researchHarness(t, &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}, web)
	payload := `{"messages":[{"role":"user","content":"Read https://example.com/"}]}`
	id := researchApproved(t, h, c, payload)
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var answer map[string]string
	json.Unmarshal(w.Body.Bytes(), &answer)
	if len(answer["reply"]) != 20000 {
		t.Fatal("full reply lost", len(answer["reply"]))
	}
	w = researchRequest(h, c, "GET", "/api/runs/"+w.Header().Get("X-Agent-Run-ID"), "")
	if w.Body.Len() > 128<<10 {
		t.Fatal("trace exceeds 128KiB", w.Body.Len())
	}
	var trace struct {
		Run struct {
			ModelCalls           []json.RawMessage `json:"model_calls"`
			ToolCalls            []json.RawMessage `json:"tool_calls"`
			CaptureOmittedFields int               `json:"capture_omitted_fields"`
			Usage                RunUsage
		}
	}
	json.Unmarshal(w.Body.Bytes(), &trace)
	if len(trace.Run.ModelCalls) != 7 || len(trace.Run.ToolCalls) != 6 || trace.Run.CaptureOmittedFields == 0 || trace.Run.Usage.TotalTokens != 21 {
		t.Fatal("bounded trace incomplete", w.Body.String())
	}
}
func TestResearchToolBudgetRejectsBatchedOverflow(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		tools := []any{}
		for i := 0; i < 7; i++ {
			tools = append(tools, map[string]any{"id": fmt.Sprint(i), "type": "function", "function": map[string]any{"name": "web_fetch", "arguments": `{"url":"https://example.com/"}`}})
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"tool_calls": tools}}}})
	}))
	defer upstream.Close()
	web := &researchFixtureWeb{}
	h, c, _ := researchHarness(t, &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}, web)
	payload := `{"messages":[{"role":"user","content":"Read https://example.com/"}]}`
	id := researchApproved(t, h, c, payload)
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code == 200 || web.calls.Load() != 0 || calls.Load() != 1 {
		t.Fatal(w.Code, calls.Load(), web.calls.Load())
	}
}
