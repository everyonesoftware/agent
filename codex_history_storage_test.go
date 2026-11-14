package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func codexHistoryBody(t *testing.T, f connectionFixture, window string) usageHistory {
	t.Helper()
	out := f.request("GET", "/api/usage/history?profile_id="+f.id+"&range=week&window_id="+window, "", "agent.example.test")
	if out.Code != 200 {
		t.Fatalf("history %d: %s", out.Code, out.Body.String())
	}
	var h usageHistory
	if err := json.Unmarshal(out.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	return h
}
func codexHistoryConnect(t *testing.T, f connectionFixture, rpc *fakeUsageRPC) usageStatus {
	t.Helper()
	usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	rpc.mu.Lock()
	rpc.signedIn = true
	rpc.mu.Unlock()
	return usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
}
func TestCodexHistoryObservationsSurviveRestart(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	now := time.Unix(1900000000, 0)
	s.now = func() time.Time { return now }
	codexHistoryConnect(t, f, rpc)
	h := codexHistoryBody(t, f, "")
	if len(h.Points) != 1 || h.RemainingPercent == nil || *h.RemainingPercent != 68 || h.ObservedAt != now.Unix() || h.TrackingStartedAt != now.Unix() {
		t.Fatalf("missing real observation: %+v", h)
	}
	s.close()
	now = now.Add(2 * time.Minute)
	restored := newUsageService(f.accounts, s.base)
	t.Cleanup(restored.close)
	restored.now = s.now
	restored.executable = s.executable
	restored.factory = func(context.Context, string, string) (usageRPC, error) {
		return &fakeUsageRPC{signedIn: true, email: "fixture@example.com"}, nil
	}
	mux := http.NewServeMux()
	restored.register(mux)
	f.server = mux
	// History validates restored identity and refreshes quota before exposing old samples.
	h = codexHistoryBody(t, f, "")
	if len(h.Points) != 2 || h.TrackingStartedAt != 1900000000 {
		t.Fatalf("lost durable observations: %+v", h)
	}
	if out := f.request("GET", "/api/usage/history?profile_id="+f.id+"&range=week&window_id=not-a-window", "", "agent.example.test"); out.Code != 400 {
		t.Fatalf("unknown window accepted: %d", out.Code)
	}
}
