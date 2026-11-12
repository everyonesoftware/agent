package main

import (
	"context"
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

func TestResearchStopCancelsBlockedProviderAndTrace(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		calls.Add(1)
		close(entered)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	p := &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}
	web := &researchFixtureWeb{}
	h, c, _ := researchHarness(t, p, web)
	payload := `{"messages":[{"role":"user","content":"Research public web"}],"run_id":"00000000-0000-0000-0000-000000000003"}`
	id := researchApproved(t, h, c, payload)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id)) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider not entered")
	}
	w := researchRequest(h, c, "GET", "/api/runs/00000000-0000-0000-0000-000000000003", "")
	if !strings.Contains(w.Body.String(), `"state":"running"`) {
		t.Fatal(w.Body.String())
	}
	w = researchRequest(h, c, "POST", "/api/research/tasks/"+id+"/stop", `{}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"cancelled"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case w = <-done:
		if w.Code == 200 {
			t.Fatal("cancelled chat succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("chat did not cancel")
	}
	if calls.Load() != 1 || web.calls.Load() != 0 {
		t.Fatal("calls after cancellation", calls.Load(), web.calls.Load())
	}
	w = researchRequest(h, c, "GET", "/api/runs/00000000-0000-0000-0000-000000000003", "")
	var trace struct {
		Run struct {
			State      string
			ModelCalls []struct{ State string } `json:"model_calls"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &trace)
	if trace.Run.State != "cancelled" || len(trace.Run.ModelCalls) != 1 || trace.Run.ModelCalls[0].State != "cancelled" {
		t.Fatal("untruthful cancellation trace", w.Body.String())
	}
}
func TestResearchToolRejectionsMakeNoWebRequests(t *testing.T) {
	for _, tc := range []struct{ name, fn, args string }{
		{"unknown", "send_email", `{"to":"someone"}`},
		{"extra", "web_fetch", `{"url":"https://example.com/","publish":true}`},
		{"wrong_type", "web_fetch", `{"url":123}`},
		{"no_approval_URL", "web_fetch", `{"url":"https://other.example/"}`},
		{"private_URL", "web_fetch", `{"url":"http://127.0.0.1/"}`},
		{"missing_search", "web_search", `{"query":"public"}`},
		{"duplicate_key", "web_fetch", `{"url":"https://example.com/","url":"https://example.com/"}`},
		{"malformed", "web_fetch", `{"url":"https://example.com/"} {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":%q,"arguments":%q}}]}}]}`, tc.fn, tc.args)
			}))
			defer upstream.Close()
			web := &researchFixtureWeb{fetchFn: func(context.Context, string) (string, error) { return "fixture", nil }}
			h, c, _ := researchHarness(t, &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}, web)
			payload := `{"messages":[{"role":"user","content":"Read https://example.com/ and http://127.0.0.1/"}]}`
			id := researchApproved(t, h, c, payload)
			w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
			if tc.name == "unknown" && (w.Code != 403 || !strings.Contains(w.Body.String(), `"action":"review_research"`)) {
				t.Fatal("scope expansion did not visibly pause", w.Code, w.Body.String())
			}
			if w.Code == 200 || calls.Load() != 1 || web.calls.Load() != 0 {
				t.Fatal(w.Code, w.Body.String(), calls.Load(), web.calls.Load())
			}
		})
	}
}
func TestResearchTaskOwnershipExpiryAndMismatch(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture answer"}}]}`)
	}))
	defer upstream.Close()
	h, c, s := researchHarness(t, &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}, &researchFixtureWeb{})
	payload := `{"messages":[{"role":"user","content":"Research"}]}`
	id := researchApproved(t, h, c, payload)
	foreign := researchRequest(h, nil, "POST", "/api/auth/register", `{"username":"foreign-user","password":"correct-horse-battery"}`).Result().Cookies()[0]
	for _, path := range []string{"/api/research/tasks/" + id, "/api/research/tasks/" + id + "/approve", "/api/research/tasks/" + id + "/stop", "/api/research/tasks/" + id + "/revoke"} {
		method := "POST"
		if path == "/api/research/tasks/"+id {
			method = "GET"
		}
		w := researchRequest(h, foreign, method, path, `{}`)
		if w.Code != 404 {
			t.Fatal("foreign", path, w.Code)
		}
	}
	if w := researchRequest(h, foreign, "POST", "/api/chat", researchWithTask(payload, id)); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(strings.Replace(payload, "Research", "Changed", 1), id)); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatal("denied model I/O")
	}
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 200 {
		t.Fatal("mismatch consumed approval", w.Code, w.Body.String())
	}
	id = researchApproved(t, h, c, payload)
	s.mu.Lock()
	s.tasks[id].ExpiresAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if w = researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id)); w.Code != 403 {
		t.Fatal("expired", w.Code, w.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatal("expired model I/O", calls.Load())
	}
}

func TestResearchStopDuringProviderBodyIsCancelled(t *testing.T) {
	entered := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		fmt.Fprint(w, `{"choices":[`)
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
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
		t.Fatal("body not entered")
	}
	researchRequest(h, c, "POST", "/api/research/tasks/"+id+"/stop", `{}`)
	select {
	case w := <-done:
		trace := researchRequest(h, c, "GET", "/api/runs/"+w.Header().Get("X-Agent-Run-ID"), "")
		var v struct{ Run struct{ State string } }
		json.Unmarshal(trace.Body.Bytes(), &v)
		if v.Run.State != "cancelled" {
			t.Fatal("body cancellation marked failure", trace.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("body did not cancel")
	}
}

func TestResearchStopCancelsBlockedToolAndPreventsSave(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"web_fetch","arguments":"{\"url\":\"https://example.com/\"}"}}]}}]}`)
	}))
	defer upstream.Close()
	web := &researchFixtureWeb{fetchFn: func(ctx context.Context, u string) (string, error) {
		close(entered)
		<-release
		return "late evidence", nil
	}}
	h, c, _ := researchHarness(t, &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}, web)
	payload := `{"messages":[{"role":"user","content":"Read https://example.com/"}]}`
	id := researchApproved(t, h, c, payload)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id)) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("tool not entered")
	}
	w := researchRequest(h, c, "POST", "/api/research/tasks/"+id+"/revoke", `{}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"revoked"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	close(release)
	select {
	case w = <-done:
		if w.Code == 200 || strings.Contains(w.Body.String(), "conversation_id") {
			t.Fatal(w.Code, w.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("tool cancellation did not terminate")
	}
	if calls.Load() != 1 || web.calls.Load() != 1 {
		t.Fatal("later calls after revoke", calls.Load(), web.calls.Load())
	}
	w = researchRequest(h, c, "GET", "/api/conversations", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"conversations":[]`) {
		t.Fatal("cancelled reply was saved", w.Code, w.Body.String())
	}
}
