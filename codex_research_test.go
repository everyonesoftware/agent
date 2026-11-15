package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

type codexEvidenceWeb struct{ calls int }

func (w *codexEvidenceWeb) searchAvailable() bool  { return false }
func (w *codexEvidenceWeb) searchProvider() string { return "unavailable" }
func (w *codexEvidenceWeb) search(context.Context, string) (string, error) {
	return "", errResearchDenied
}
func (w *codexEvidenceWeb) fetch(_ context.Context, url string) (string, error) {
	w.calls++
	if url != "https://example.com/evidence" {
		return "", errResearchDenied
	}
	return `{"url":"https://example.com/evidence","title":"Public evidence","content":"bounded-public-evidence"}`, nil
}
func TestCodexResearchConsentMemoryBoundaryAndTrace(t *testing.T) {
	f, _, u, audit := codexInferenceFixture(t, "research")
	if _, err := f.accounts.saveMemory(f.user, "user", "private-memory-never-research"); err != nil {
		t.Fatal(err)
	}
	provider := &userScopedProvider{accounts: f.accounts, baseAuth: u.base, usage: u}
	web := &codexEvidenceWeb{}
	research := newResearchService(web)
	mux := http.NewServeMux()
	research.register(mux, f.accounts)
	mux.Handle("POST /api/chat", f.accounts.middleware(chatHandlerWithResearch(provider, f.accounts, research)))
	mux.Handle("GET /api/runs/{id}", f.accounts.middleware(http.HandlerFunc(f.accounts.runHandler)))
	payload := `{"messages":[{"role":"user","content":"private-history-never-research"},{"role":"assistant","content":"old reply"},{"role":"user","content":"Read https://example.com/evidence"}],"run_id":"00000000-0000-0000-0000-000000000046"}`
	id := researchApproved(t, mux, f.cookie, payload)
	out := researchRequest(mux, f.cookie, "POST", "/api/chat", researchWithTask(payload, id))
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Codex fixture answer") {
		t.Fatalf("research %d %s", out.Code, out.Body)
	}
	if web.calls != 1 {
		t.Fatalf("web calls %d", web.calls)
	}
	b, _ := os.ReadFile(audit)
	if strings.Contains(string(b), "private-memory-never-research") || strings.Contains(string(b), "private-history-never-research") || !strings.Contains(string(b), "bounded-public-evidence") {
		t.Fatalf("research scope wire %s", b)
	}
	trace := researchRequest(mux, f.cookie, "GET", "/api/runs/00000000-0000-0000-0000-000000000046", "")
	var result struct {
		Run struct {
			ModelCalls []json.RawMessage `json:"model_calls"`
			ToolCalls  []json.RawMessage `json:"tool_calls"`
			Usage      RunUsage          `json:"usage"`
		}
	}
	if json.Unmarshal(trace.Body.Bytes(), &result) != nil || len(result.Run.ModelCalls) != 2 || len(result.Run.ToolCalls) != 1 || result.Run.Usage.TotalTokens != 36 {
		t.Fatalf("research trace %d %s", trace.Code, trace.Body)
	}
	replay := researchRequest(mux, f.cookie, "POST", "/api/chat", researchWithTask(payload, id))
	if replay.Code != 403 {
		t.Fatalf("consent replay %d", replay.Code)
	}
}
