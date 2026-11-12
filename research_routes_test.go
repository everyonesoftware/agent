package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResearchRoutesRequireAuthenticationAndSameOrigin(t *testing.T) {
	h, c := researchServer(t)
	payload := `{"messages":[{"role":"user","content":"Research"}]}`
	id := researchApproved(t, h, c, payload)
	for _, tc := range []struct{ method, path, body string }{{"GET", "/api/research", ""}, {"POST", "/api/research/tasks", payload}, {"GET", "/api/research/tasks/" + id, ""}, {"POST", "/api/research/tasks/" + id + "/approve", `{}`}, {"POST", "/api/research/tasks/" + id + "/stop", `{}`}, {"POST", "/api/research/tasks/" + id + "/revoke", `{}`}} {
		w := researchRequest(h, nil, tc.method, tc.path, tc.body)
		if w.Code != 401 {
			t.Fatal("unauthenticated", tc.path, w.Code)
		}
		if tc.method == "POST" {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.AddCookie(c)
			r.Header.Set("Origin", "https://foreign.invalid")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatal("cross-site", tc.path, w.Code, w.Body.String())
			}
		}
	}
	w := researchRequest(h, c, "GET", "/api/research/tasks/"+id, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"status":"approved"`) {
		t.Fatal("cross-site request changed task", w.Code, w.Body.String())
	}
}
func TestResearchLegacyChatHasNoToolsAndRetainsHistory(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&payload)
		if _, ok := payload["tools"]; ok {
			t.Error("legacy chat has tools")
		}
		if !strings.Contains(string(payload["messages"]), "old private history") {
			t.Error("legacy chat history changed")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"legacy fixture answer"}}]}`)
	}))
	defer upstream.Close()
	t.Setenv("LLM_BASE_URL", upstream.URL)
	t.Setenv("LLM_MODEL", "fixture")
	h, c := researchServer(t)
	w := researchRequest(h, c, "POST", "/api/chat", `{"messages":[{"role":"user","content":"old private history"},{"role":"assistant","content":"old answer"},{"role":"user","content":"question"}]}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestResearchConcurrentGrantIsSingleUse(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture answer"}}]}`)
	}))
	defer upstream.Close()
	h, c, _ := researchHarness(t, &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}, &researchFixtureWeb{})
	payload := `{"messages":[{"role":"user","content":"Research"}]}`
	id := researchApproved(t, h, c, payload)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id)) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("provider not entered")
	}
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	close(release)
	if w.Code != 403 {
		t.Fatal("concurrent grant reuse", w.Code, w.Body.String())
	}
	select {
	case w = <-done:
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("first request did not finish")
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate model I/O", calls.Load())
	}
}
