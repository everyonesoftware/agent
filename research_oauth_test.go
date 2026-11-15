package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	if w.Code != 401 || !strings.Contains(w.Body.String(), "reauthorize") || ownerCalls.Load() != 0 || foreignCalls.Load() != 0 {
		t.Fatal("legacy owner did not require Codex reconnect without transport", w.Code, w.Body.String(), ownerCalls.Load(), foreignCalls.Load())
	}
}

func TestResearchOAuthResponsesStructuredLoop(t *testing.T) {
	_, p, _, audit := codexInferenceFixture(t, "research")
	web := &researchFixtureWeb{fetchFn: func(_ context.Context, url string) (string, error) {
		if url != "https://example.com/evidence" {
			t.Errorf("unexpected fetch %q", url)
		}
		return `{"url":"https://example.com/evidence","title":"fixture evidence","content":"bounded public evidence"}`, nil
	}}
	h, c, _ := researchHarness(t, p, web)
	payload := `{"messages":[{"role":"user","content":"old private history"},{"role":"assistant","content":"old answer"},{"role":"user","content":"Read https://example.com/evidence"}]}`
	id := researchApproved(t, h, c, payload)
	w := researchRequest(h, c, "POST", "/api/chat", researchWithTask(payload, id))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Codex fixture answer") {
		t.Fatal(w.Code, w.Body.String())
	}
	if web.calls.Load() != 1 {
		t.Fatal(web.calls.Load())
	}
	wire, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "old private history") || !strings.Contains(string(wire), "fixture evidence") || !strings.Contains(string(wire), "dynamicTools") {
		t.Fatalf("unsafe/incomplete Codex continuation: %s", wire)
	}
	w = researchRequest(h, c, "GET", "/api/runs/"+w.Header().Get("X-Agent-Run-ID"), "")
	if strings.Contains(w.Body.String(), "PRIVATE-") || strings.Contains(w.Body.String(), "FIXTURE-ACCESS") || !strings.Contains(w.Body.String(), `"total_tokens":36`) {
		t.Fatal("unsafe/incomplete OAuth trace", w.Body.String())
	}
}
