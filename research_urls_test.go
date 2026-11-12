package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestResearchFetchMayUseSuccessfulSearchURLWithEscapedQuery(t *testing.T) {
	target := "https://example.com/evidence?a=1&b=2"
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"search_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"public evidence\"}"}}]}}]}`)
		case 2:
			args, _ := json.Marshal(map[string]string{"url": target})
			fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"fetch_1","type":"function","function":{"name":"web_fetch","arguments":%q}}]}}]}`, string(args))
		default:
			fmt.Fprint(w, `{"choices":[{"message":{"content":"Sourced fixture answer"}}]}`)
		}
	}))
	defer upstream.Close()
	web := &researchFixtureWeb{searchFn: func(context.Context, string) (string, error) {
		b, _ := json.Marshal(map[string]any{"results": []any{map[string]string{"url": target, "title": "public source"}}})
		return string(b), nil
	}, fetchFn: func(ctx context.Context, url string) (string, error) {
		if url != target {
			t.Error("changed URL", url)
		}
		return "public evidence", nil
	}}
	h, c, _ := researchHarness(t, &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}, web)
	payload := `{"messages":[{"role":"user","content":"Research public evidence"}]}`
	id := researchApproved(t, h, c, payload)
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 200 || calls.Load() != 3 || web.calls.Load() != 2 {
		t.Fatal(w.Code, w.Body.String(), calls.Load(), web.calls.Load())
	}
}
