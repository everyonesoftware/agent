package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The browser uses the production router, account store and connection state.
// Only the Codex child-process boundary is replaced; no real sign-in is started.
func TestCodexConnectionProductionBrowser(t *testing.T) {
	if os.Getenv("AGENT_CODEX_BROWSER") != "1" {
		t.Skip("set AGENT_CODEX_BROWSER=1 for isolated unified Codex browser acceptance")
	}
	f := newConnectionFixture(t)
	app := f.server.(*applicationServer)
	defer app.close()
	if err := f.store.DeleteProfile(f.id); err != nil {
		t.Fatal(err)
	}
	u := app.usage
	u.executable = func() (string, error) { return "isolated-codex-fixture", nil }
	u.factory = func(context.Context, string, string) (usageRPC, error) {
		return &fakeUsageRPC{email: "fixture@example.com"}, nil
	}
	audit := filepath.Join(t.TempDir(), "inference.jsonl")
	u.inferenceFactory = func(ctx context.Context, _, home string) (*codexUsageRPC, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCodexInferenceHelper$")
		cmd.Dir = home
		cmd.Env = append(os.Environ(), "GO_CODEX_INFERENCE_HELPER=1", "GO_CODEX_INFERENCE_MODE=chat", "GO_CODEX_AUDIT="+audit)
		rpc, err := startCodexProtocolCommand(ctx, cmd, true)
		if err != nil {
			return nil, err
		}
		return rpc.(*codexUsageRPC), nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/fixture/approve" {
			var body struct {
				ProfileID string `json:"profile_id"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || !validProfileID(body.ProfileID) {
				http.Error(w, "invalid fixture target", 400)
				return
			}
			entry, err := u.session(f.user, body.ProfileID)
			if err != nil {
				http.Error(w, "missing fixture session", 400)
				return
			}
			entry.mu.Lock()
			rpc, ok := entry.rpc.(*fakeUsageRPC)
			if !ok {
				entry.mu.Unlock()
				http.Error(w, "fixture worker not running", 409)
				return
			}
			rpc.mu.Lock()
			rpc.signedIn = true
			rpc.mu.Unlock()
			entry.mu.Unlock()
			writeChatJSON(w, 200, map[string]bool{"approved": true})
			return
		}
		app.ServeHTTP(w, r)
	}))
	defer server.Close()
	cmd := exec.Command("node", "tests/codex-connection-browser.mjs", server.URL)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("unified Codex production browser acceptance: %v", err)
	}
	requests, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	if turns := strings.Count(string(requests), `"method":"turn/start"`); turns != 1 {
		t.Fatalf("model discovery and disconnected chat must not infer: got %d turns, want one explicit chat", turns)
	}
}
