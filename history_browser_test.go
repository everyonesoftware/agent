package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Only upstream quota RPC is replaced. SQLite history, auth, Settings HTML,
// history handlers and the browser all run through their production paths.
type historyBrowserRPC struct {
	*fakeUsageRPC
	used  atomic.Int64
	reset int64
}

func (r *historyBrowserRPC) Call(ctx context.Context, method string, params, result any) error {
	if method != "account/rateLimits/read" {
		return r.fakeUsageRPC.Call(ctx, method, params, result)
	}
	value := float64(r.used.Load()) / 100
	payload := map[string]any{"rateLimits": map[string]any{"limitId": "codex", "primary": map[string]any{"usedPercent": value, "windowDurationMins": 300, "resetsAt": r.reset - 4*3600}, "secondary": map[string]any{"usedPercent": value, "windowDurationMins": 10080, "resetsAt": r.reset}}}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, result)
}
func TestUsageHistoryProductionBrowser(t *testing.T) {
	if os.Getenv("AGENT_HISTORY_BROWSER") != "1" {
		t.Skip("set AGENT_HISTORY_BROWSER=1 to run the production usage history browser check")
	}
	f, s, base := newUsageFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	rpc := &historyBrowserRPC{fakeUsageRPC: base, reset: now.Add(6 * time.Hour).Unix()}
	s.factory = func(context.Context, string, string) (usageRPC, error) { return rpc, nil }
	clock := now.Add(-time.Hour)
	s.now = func() time.Time { return clock }
	rpc.used.Store(2000)
	started := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	base.mu.Lock()
	base.signedIn = true
	base.mu.Unlock()
	usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	for i := 1; i <= 60; i++ {
		clock = now.Add(time.Duration(i-60) * time.Minute)
		rpc.used.Store(int64(2000 + i*20))
		usageBody(t, f.request("POST", "/api/usage/refresh", `{"profile_id":"`+f.id+`","connection_id":"`+started.ConnectionID+`"}`, "agent.example.test"))
	}
	s.now = time.Now
	// Explicit synthetic ledger fixtures, not invented provider responses.
	if _, err := f.accounts.researchBudgetForUser(f.user.ID, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.db.Exec(`UPDATE research_budget SET enabled=1,maximum=10 WHERE user_id=?`, f.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.db.Exec(`INSERT INTO research_settings(user_id,api_key,revision,verified_at,last_checked_at,status) VALUES(?,?,?,?,?,?)`, f.user.ID, "synthetic-history-browser-key", "history-fixture", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "connected"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := f.accounts.db.Exec(`INSERT INTO research_budget_events(id,user_id,started_ns,state) VALUES(?,?,?,'used')`, fmt.Sprintf("history-request-%d", i), f.user.ID, now.Add(time.Duration(-50+i*10)*time.Minute).UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LLM_MODEL", "history-browser-model")
	t.Setenv("LLM_BASE_URL", "http://127.0.0.1:1/v1")
	t.Setenv("LLM_API_KEY", "")
	root := newServer()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/models" {
			writeChatJSON(w, http.StatusOK, map[string]any{"models": []map[string]string{{"id": "history-browser-model", "name": "History fixture"}}, "default_model": "history-browser-model"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/usage/") {
			f.server.ServeHTTP(w, r)
			return
		}
		root.ServeHTTP(w, r)
	}))
	defer server.Close()
	command := exec.Command("node", "tests/history-browser.mjs", server.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("usage history browser integration: %v", err)
	}
}
