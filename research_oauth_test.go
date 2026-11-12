package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type researchFixtureWeb struct {
	calls    atomic.Int32
	searchFn func(context.Context, string) (string, error)
	fetchFn  func(context.Context, string) (string, error)
}

func (w *researchFixtureWeb) searchAvailable() bool { return w.searchFn != nil }
func (w *researchFixtureWeb) searchProvider() string {
	if w.searchAvailable() {
		return "Brave Search"
	}
	return "unavailable"
}
func (w *researchFixtureWeb) search(ctx context.Context, q string) (string, error) {
	w.calls.Add(1)
	return w.searchFn(ctx, q)
}
func (w *researchFixtureWeb) fetch(ctx context.Context, u string) (string, error) {
	w.calls.Add(1)
	return w.fetchFn(ctx, u)
}
func TestResearchUserScopedOAuthNeverUsesAnotherUsersToken(t *testing.T) {
	var ownerCalls, foreignCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer OWNER-ACCESS":
			ownerCalls.Add(1)
		case "Bearer FOREIGN-ACCESS":
			foreignCalls.Add(1)
		default:
			t.Error("unscoped credential", r.Header.Get("Authorization"))
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/models" {
			fmt.Fprint(w, `{"data":[{"id":"oauth-model","visibility":"list"}]}`)
			return
		}
		var payload map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&payload)
		if len(payload["tools"]) == 0 {
			t.Error("no structured tools")
		}
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Scoped fixture answer"}]}]}}`+"\n\n")
	}))
	defer upstream.Close()
	accounts, err := newAccountStore(filepath.Join(t.TempDir(), "accounts.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer accounts.db.Close()
	cookies := []*http.Cookie{}
	for _, name := range []string{"OWNER", "FOREIGN"} {
		user, token, err := accounts.register(name+"-user", "correct-horse-battery")
		if err != nil {
			t.Fatal(err)
		}
		cookies = append(cookies, &http.Cookie{Name: authCookieName, Value: token})
		creds := newFileCredentialStore(t.TempDir())
		if err = creds.Save(testOAuthCredentials(name+"-ACCESS", name+"-REFRESH", time.Now().Add(time.Hour))); err != nil {
			t.Fatal(err)
		}
		service := newSIWCService(siwcConfig{apiBaseURL: upstream.URL}, creds)
		service.client = upstream.Client()
		accounts.oauthServices[user.ID] = service
	}
	p := &userScopedProvider{accounts: accounts}
	s := newResearchService(&researchFixtureWeb{})
	mux := http.NewServeMux()
	s.register(mux, accounts)
	mux.Handle("POST /api/chat", accounts.middleware(chatHandlerWithResearch(p, accounts, s)))
	payload := `{"messages":[{"role":"user","content":"Research"}]}`
	id := researchApproved(t, mux, cookies[0], payload)
	w := researchRequest(mux, cookies[1], "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 403 || ownerCalls.Load() != 0 || foreignCalls.Load() != 0 {
		t.Fatal("foreign grant I/O", w.Code)
	}
	w = researchRequest(mux, cookies[0], "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 200 || ownerCalls.Load() != 2 || foreignCalls.Load() != 0 {
		t.Fatal("owner scope failed", w.Code, w.Body.String(), ownerCalls.Load(), foreignCalls.Load())
	}
}

func TestResearchOAuthResponsesStructuredLoop(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer FIXTURE-ACCESS" {
			t.Error("wrong OAuth owner token")
		}
		if r.URL.Path == "/models" {
			fmt.Fprint(w, `{"data":[{"id":"oauth-model","visibility":"list"}]}`)
			return
		}
		if r.URL.Path != "/responses" {
			t.Error(r.URL.Path)
		}
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if string(body["store"]) != "false" || string(body["stream"]) != "true" || strings.Contains(string(body["input"]), "old private history") {
			t.Error("invalid OAuth request", string(body["input"]))
		}
		if !strings.Contains(string(body["tools"]), `"name":"web_search"`) || strings.Contains(string(body["tools"]), `"function":`) {
			t.Error("wrong Responses function schema", string(body["tools"]))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, "data: {\"type\":\"response.reasoning.delta\",\"delta\":\"PRIVATE-REASONING\"}\n\n")
			fmt.Fprint(w, `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"PRIVATE-ENCRYPTED"},{"type":"function_call","id":"fc_1","call_id":"call_search","name":"web_search","arguments":"{\"query\":\"public fixture query\"}"}],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}}`+"\n\n")
			return
		}
		if !strings.Contains(string(body["input"]), `"call_id":"call_search"`) || !strings.Contains(string(body["input"]), "function_call_output") || !strings.Contains(string(body["input"]), "PRIVATE-ENCRYPTED") {
			t.Error("Responses continuation missing", string(body["input"]))
		}
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OAuth fixture answer https://example.com/"}]}],"usage":{"input_tokens":20,"output_tokens":8,"total_tokens":28}}}`+"\n\n")
	}))
	defer server.Close()
	creds := newFileCredentialStore(t.TempDir())
	if err := creds.Save(testOAuthCredentials("FIXTURE-ACCESS", "FIXTURE-REFRESH", time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	service := newSIWCService(siwcConfig{apiBaseURL: server.URL}, creds)
	service.client = server.Client()
	p := &responsesProvider{auth: service}
	web := &researchFixtureWeb{searchFn: func(context.Context, string) (string, error) {
		return `{"results":[{"url":"https://example.com/","title":"fixture evidence"}]}`, nil
	}}
	h, c, _ := researchHarness(t, p, web)
	payload := `{"messages":[{"role":"user","content":"old private history"},{"role":"assistant","content":"old answer"},{"role":"user","content":"Research public fixture query"}]}`
	id := researchApproved(t, h, c, payload)
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "OAuth fixture answer") {
		t.Fatal(w.Code, w.Body.String())
	}
	if calls.Load() != 2 || web.calls.Load() != 1 {
		t.Fatal(calls.Load(), web.calls.Load())
	}
	w = researchRequest(h, c, "GET", "/api/runs/"+w.Header().Get("X-Agent-Run-ID"), "")
	if strings.Contains(w.Body.String(), "PRIVATE-") || strings.Contains(w.Body.String(), "FIXTURE-ACCESS") || !strings.Contains(w.Body.String(), `"total_tokens":42`) {
		t.Fatal("unsafe/incomplete OAuth trace", w.Body.String())
	}
}
