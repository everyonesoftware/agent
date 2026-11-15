package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCodexMigrationNeverCallsLegacyResponses(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer upstream.Close()
	auth := newSIWCService(siwcConfig{apiBaseURL: upstream.URL}, newFileCredentialStore(t.TempDir()))
	if err := auth.store.Save(testOAuthCredentials("fixture-old-access", "fixture-old-refresh", time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	p := &responsesProvider{auth: auth}
	_, _ = p.complete(context.Background(), []chatMessage{{Role: "user", Content: "hello"}}, "")
	profile, _ := auth.store.ActiveProfileID()
	_, _ = auth.availableModelsForProfile(context.Background(), profile)
	if calls != 0 {
		t.Fatalf("legacy upstream requests = %d; want zero", calls)
	}
}

func TestCodexMigrationLegacyProfileCannotInfer(t *testing.T) {
	// A Responses-only profile must require Codex reconnect, not attempt refresh.
	auth := newSIWCService(siwcConfig{}, newFileCredentialStore(t.TempDir()))
	host, _ := auth.store.HostID()
	if err := auth.store.Save(oauthCredentials{ClientID: "legacy", Issuer: "https://auth.openai.com", HostID: host, Subject: "legacy", OAuthPreferred: true}); err != nil {
		t.Fatal(err)
	}
	p := &responsesProvider{auth: auth}
	if _, err := p.complete(context.Background(), []chatMessage{{Role: "user", Content: "hi"}}, ""); err != errChatGPTNotConnected {
		t.Fatalf("got %v; want reconnect required", err)
	}
}

func TestCodexMigrationNoLegacyAuthorizationCallback(t *testing.T) {
	f := newConnectionFixture(t)
	defer f.server.(*applicationServer).close()
	out := f.request("GET", "/auth/callback?code=fixture-code&state=fixture-state", "", "127.0.0.1:8080")
	if out.Code != 404 {
		t.Fatalf("legacy callback remains registered: %d", out.Code)
	}
}

func TestCodexMigrationReconnectErrorIsActionable(t *testing.T) {
	out := httptest.NewRecorder()
	writeChatProviderError(out, errChatGPTNotConnected)
	if out.Code != 401 {
		t.Fatalf("reconnect status %d: %s", out.Code, out.Body)
	}
}

func TestCodexMigrationAdoptsCodexIdentityForLegacyProfile(t *testing.T) {
	f := newConnectionFixture(t)
	app := f.server.(*applicationServer)
	defer app.close()
	rpc := &fakeUsageRPC{email: "current-codex@example.com"}
	app.usage.executable = func() (string, error) { return "fixture-codex", nil }
	app.usage.factory = func(context.Context, string, string) (usageRPC, error) { return rpc, nil }
	profile, err := f.store.LoadProfile(f.id)
	if err != nil {
		t.Fatal(err)
	}
	profile.Email = "old-responses@example.com"
	if err = f.store.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}
	start := f.request("POST", "/auth/start", `{"expected_active_profile_id":"`+f.id+`"}`, "agent.example.test")
	if start.Code != 200 {
		t.Fatal(start.Code, start.Body)
	}
	rpc.mu.Lock()
	rpc.signedIn = true
	rpc.mu.Unlock()
	status := f.request("GET", "/api/auth/status", "", "agent.example.test")
	if status.Code != 200 {
		t.Fatal(status.Code, status.Body)
	}
	after, err := f.store.LoadProfile(f.id)
	if err != nil || after.Email != "current-codex@example.com" {
		t.Fatalf("identity not adopted %+v %v", after, err)
	}
}

func TestCodexAddPendingCancelAndFailurePreserveActiveProfile(t *testing.T) {
	for _, mode := range []string{"pending-cancel", "failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newConnectionFixture(t)
			app := f.server.(*applicationServer)
			defer app.close()
			rpc := &fakeUsageRPC{}
			if mode == "failure" {
				rpc.fail = "account/login/start"
			}
			app.usage.executable = func() (string, error) { return "fixture-codex", nil }
			app.usage.factory = func(context.Context, string, string) (usageRPC, error) { return rpc, nil }
			out := f.request("POST", "/auth/start?mode=add", `{"expected_active_profile_id":"`+f.id+`"}`, "agent.example.test")
			active, err := f.store.ActiveProfileID()
			if err != nil || active != f.id {
				t.Fatalf("add replaced active profile %q %v", active, err)
			}
			if mode == "failure" {
				if out.Code != 503 {
					t.Fatal(out.Code, out.Body)
				}
				return
			}
			if out.Code != 200 {
				t.Fatal(out.Code, out.Body)
			}
			var challenge struct {
				ProfileID    string `json:"profile_id"`
				ConnectionID string `json:"connection_id"`
				LoginID      string `json:"login_id"`
			}
			if json.Unmarshal(out.Body.Bytes(), &challenge) != nil || challenge.ProfileID == "" {
				t.Fatal(out.Body)
			}
			retry := f.request("POST", "/auth/start?mode=retry&profile_id="+challenge.ProfileID, `{"expected_active_profile_id":"`+f.id+`"}`, "agent.example.test")
			if retry.Code != 200 {
				t.Fatalf("retry pending %d %s", retry.Code, retry.Body)
			}
			poll := f.request("GET", "/api/usage/status?profile_id="+challenge.ProfileID, "", "agent.example.test")
			if poll.Code != 200 {
				t.Fatalf("poll pending %d %s", poll.Code, poll.Body)
			}
			cancel := f.request("POST", "/api/usage/cancel", `{"profile_id":"`+challenge.ProfileID+`","connection_id":"`+challenge.ConnectionID+`","login_id":"`+challenge.LoginID+`"}`, "agent.example.test")
			if cancel.Code != 200 {
				t.Fatalf("cancel pending %d %s", cancel.Code, cancel.Body)
			}
			active, err = f.store.ActiveProfileID()
			if err != nil || active != f.id {
				t.Fatalf("cancel replaced active profile %q %v", active, err)
			}
		})
	}
}

func TestCodexInactiveApprovedChallengeCanReachTerminalStatus(t *testing.T) {
	f := newConnectionFixture(t)
	app := f.server.(*applicationServer)
	defer app.close()
	rpc := &fakeUsageRPC{email: "added@example.com"}
	app.usage.executable = func() (string, error) { return "fixture-codex", nil }
	app.usage.factory = func(context.Context, string, string) (usageRPC, error) { return rpc, nil }
	out := f.request("POST", "/auth/start?mode=add", `{"expected_active_profile_id":"`+f.id+`"}`, "agent.example.test")
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	var challenge struct {
		ProfileID    string `json:"profile_id"`
		ConnectionID string `json:"connection_id"`
		LoginID      string `json:"login_id"`
	}
	if json.Unmarshal(out.Body.Bytes(), &challenge) != nil {
		t.Fatal(out.Body)
	}
	rpc.mu.Lock()
	rpc.signedIn = true
	rpc.mu.Unlock()
	profiles := f.request("GET", "/api/auth/profiles", "", "agent.example.test")
	if profiles.Code != 200 {
		t.Fatal(profiles.Code, profiles.Body)
	}
	poll := f.request("GET", "/api/usage/status?profile_id="+challenge.ProfileID+"&connection_id="+challenge.ConnectionID, "", "agent.example.test")
	if poll.Code != 200 {
		t.Fatalf("terminal captured status %d %s", poll.Code, poll.Body)
	}
	var status usageStatus
	if json.Unmarshal(poll.Body.Bytes(), &status) != nil || status.State != "connected" || status.Login != nil {
		t.Fatalf("terminal status %s", poll.Body)
	}
	canceled := f.request("POST", "/api/usage/cancel", `{"profile_id":"`+challenge.ProfileID+`","connection_id":"`+challenge.ConnectionID+`","login_id":"`+challenge.LoginID+`"}`, "agent.example.test")
	if canceled.Code != 200 {
		t.Fatalf("terminal cancel %d %s", canceled.Code, canceled.Body)
	}
	entry, err := app.usage.session(f.user, challenge.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.record.ConnectionID != challenge.ConnectionID {
		t.Fatal("terminal cancel disconnected approved account")
	}
	stale := f.request("GET", "/api/usage/status?profile_id="+challenge.ProfileID+"&connection_id=stale", "", "agent.example.test")
	if stale.Code != 409 {
		t.Fatalf("stale captured status %d", stale.Code)
	}
	active, _ := f.store.ActiveProfileID()
	if active != f.id {
		t.Fatal("terminal observation switched active profile")
	}
}

func TestCodexFirstProfilePromotesOnlyAfterApproval(t *testing.T) {
	f := newConnectionFixture(t)
	app := f.server.(*applicationServer)
	defer app.close()
	if err := f.store.DeleteProfile(f.id); err != nil {
		t.Fatal(err)
	}
	rpc := &fakeUsageRPC{email: "first@example.com"}
	app.usage.executable = func() (string, error) { return "fixture-codex", nil }
	app.usage.factory = func(context.Context, string, string) (usageRPC, error) { return rpc, nil }
	out := f.request("POST", "/auth/start?mode=add", `{}`, "agent.example.test")
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	var challenge struct {
		ProfileID string `json:"profile_id"`
	}
	if json.Unmarshal(out.Body.Bytes(), &challenge) != nil {
		t.Fatal(out.Body)
	}
	active, _ := f.store.ActiveProfileID()
	if active != "" {
		t.Fatalf("pending first profile active: %q", active)
	}
	rpc.mu.Lock()
	rpc.signedIn = true
	rpc.mu.Unlock()
	poll := f.request("GET", "/api/usage/status?profile_id="+challenge.ProfileID, "", "agent.example.test")
	if poll.Code != 200 {
		t.Fatal(poll.Code, poll.Body)
	}
	active, _ = f.store.ActiveProfileID()
	if active != challenge.ProfileID {
		t.Fatalf("approved first profile not active: %q", active)
	}
}

func TestCodexMigrationRemoteStartUsesDeviceFlow(t *testing.T) {
	f := newConnectionFixture(t)
	app := f.server.(*applicationServer)
	rpc := &fakeUsageRPC{}
	app.usage.executable = func() (string, error) { return "fixture-codex", nil }
	app.usage.factory = func(context.Context, string, string) (usageRPC, error) { return rpc, nil }
	defer app.close()
	out := f.request("POST", "/auth/start", `{"expected_active_profile_id":"`+f.id+`"}`, "agent.example.test")
	if out.Code != 200 {
		t.Fatalf("device start: %d %s", out.Code, out.Body.String())
	}
	if out.Header().Get("Location") != "" {
		t.Fatal("device login redirected")
	}
}
