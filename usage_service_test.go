package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeUsageRPC struct {
	mu       sync.Mutex
	signedIn bool
	email    string
	fail     string
	calls    []string
	closed   bool
}

func (f *fakeUsageRPC) Call(_ context.Context, method string, params, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	if f.fail == method {
		return errors.New("provider diagnostic containing private-token must never escape")
	}
	var response any = map[string]any{}
	switch method {
	case "account/read":
		response = map[string]any{"account": nil}
		if f.signedIn {
			response = map[string]any{"account": map[string]any{"type": "chatgpt", "email": f.email, "planType": "plus"}}
		}
	case "account/login/start":
		response = map[string]any{"type": "chatgptDeviceCode", "loginId": "fixture-login", "verificationUrl": "https://auth.openai.com/codex/device", "userCode": "FIXTURE-ONLY"}
	case "account/rateLimits/read":
		response = map[string]any{"rateLimits": map[string]any{"limitId": "codex", "primary": map[string]any{"usedPercent": 32, "windowDurationMins": 300, "resetsAt": 2000000000}, "secondary": nil}}
	case "account/logout":
		f.signedIn = false
	}
	b, _ := json.Marshal(response)
	return json.Unmarshal(b, result)
}
func (f *fakeUsageRPC) Close() error { f.mu.Lock(); defer f.mu.Unlock(); f.closed = true; return nil }
func newUsageFixture(t *testing.T) (connectionFixture, *usageService, *fakeUsageRPC) {
	t.Helper()
	f := newConnectionFixture(t)
	p, err := f.store.LoadProfile(f.id)
	if err != nil {
		t.Fatal(err)
	}
	p.Email = "fixture@example.com"
	if err = f.store.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	rpc := &fakeUsageRPC{email: "fixture@example.com"}
	s := newUsageService(f.accounts, newSIWCService(siwcConfig{}, newFileCredentialStore(t.TempDir())))
	s.executable = func() (string, error) { return "fixture-codex", nil }
	s.factory = func(context.Context, string, string) (usageRPC, error) { return rpc, nil }
	mux := http.NewServeMux()
	s.register(mux)
	f.server = mux
	t.Cleanup(s.close)
	return f, s, rpc
}
func usageBody(t *testing.T, out *httptest.ResponseRecorder) usageStatus {
	t.Helper()
	var view usageStatus
	if out.Code != 200 {
		t.Fatalf("usage HTTP %d: %s", out.Code, out.Body.String())
	}
	if err := json.Unmarshal(out.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	return view
}
func TestUsageDeviceFlowAndQuotaAreIndependentOfConversations(t *testing.T) {
	f, _, rpc := newUsageFixture(t)
	before, _ := f.store.LoadProfile(f.id)
	body := `{"profile_id":"` + f.id + `","expected_connection_id":""}`
	pending := usageBody(t, f.request("POST", "/api/usage/connect", body, "agent.example.test"))
	if pending.State != "pending" || pending.Login == nil || pending.Login.UserCode != "FIXTURE-ONLY" || pending.ConnectionID == "" {
		t.Fatalf("missing device-code state: %+v", pending)
	}
	rpc.mu.Lock()
	rpc.signedIn = true
	rpc.mu.Unlock()
	connected := usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	if connected.State != "connected" || connected.Login != nil || connected.Snapshot == nil || len(connected.Snapshot.Windows) != 1 || connected.Snapshot.Windows[0].UsedPercent != 32 {
		t.Fatalf("missing quota: %+v", connected)
	}
	if connected.IdentityMatch != "unverified" {
		t.Fatal("email equality was treated as verified workspace identity")
	}
	after, _ := f.store.LoadProfile(f.id)
	if !reflect.DeepEqual(before.Credentials, after.Credentials) {
		t.Fatal("conversation credentials changed")
	}
	rpc.mu.Lock()
	defer rpc.mu.Unlock()
	for _, method := range rpc.calls {
		if method == "turn/start" || method == "thread/start" {
			t.Fatal("usage sent inference")
		}
	}
}
func TestUsageRefreshFailureKeepsStaleSnapshotAndSanitizesErrors(t *testing.T) {
	f, _, rpc := newUsageFixture(t)
	start := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	rpc.signedIn = true
	usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	rpc.fail = "account/rateLimits/read"
	out := f.request("POST", "/api/usage/refresh", `{"profile_id":"`+f.id+`","connection_id":"`+start.ConnectionID+`"}`, "agent.example.test")
	v := usageBody(t, out)
	if v.Snapshot == nil || !v.Snapshot.Stale || v.Error == "" || strings.Contains(out.Body.String(), "private-token") {
		t.Fatalf("unsafe refresh failure: %s", out.Body.String())
	}
}
func TestUsageDisconnectRequiresPasswordAndCapturedConnection(t *testing.T) {
	f, _, rpc := newUsageFixture(t)
	start := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	for _, body := range []string{
		`{"profile_id":"` + f.id + `","connection_id":"` + start.ConnectionID + `"}`,
		`{"profile_id":"` + f.id + `","connection_id":"` + start.ConnectionID + `","password":"wrong-password"}`,
	} {
		if out := f.request("POST", "/api/usage/disconnect", body, "agent.example.test"); out.Code != 401 {
			t.Fatalf("disconnect accepted bad password: %d", out.Code)
		}
	}
	if out := f.request("POST", "/api/usage/disconnect", `{"profile_id":"`+f.id+`","connection_id":"stale","password":"correct-horse-battery"}`, "agent.example.test"); out.Code != 409 {
		t.Fatalf("stale disconnect: %d", out.Code)
	}
	v := usageBody(t, f.request("POST", "/api/usage/disconnect", `{"profile_id":"`+f.id+`","connection_id":"`+start.ConnectionID+`","password":"correct-horse-battery"}`, "agent.example.test"))
	if v.State != "disconnected" || v.ConnectionID != "" || !rpc.closed {
		t.Fatalf("not disconnected: %+v", v)
	}
	if _, err := f.store.LoadProfile(f.id); err != nil {
		t.Fatal("conversation registration removed")
	}
}
func TestUsageSessionSurvivesServiceRestartAndUsesPrivateHome(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	rpc.signedIn = true
	s.close()
	s2 := newUsageService(f.accounts, s.base)
	t.Cleanup(s2.close)
	s2.executable = s.executable
	var home string
	s2.factory = func(_ context.Context, _, dir string) (usageRPC, error) {
		home = dir
		return &fakeUsageRPC{signedIn: true, email: "fixture@example.com"}, nil
	}
	mux := http.NewServeMux()
	s2.register(mux)
	f.server = mux
	v := usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	if v.State != "connected" || home == "" || !strings.HasPrefix(home, filepath.Join(f.accounts.dataDir, "account-data", f.user.ID, "usage", f.id)) {
		t.Fatalf("private persisted usage connection not restored: %+v home=%q", v, home)
	}
	info, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("usage credentials directory is not private")
	}
}
func TestUsagePendingCancelHasLoginAndConnectionPreconditions(t *testing.T) {
	f, _, _ := newUsageFixture(t)
	start := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	if out := f.request("POST", "/api/usage/cancel", `{"profile_id":"`+f.id+`","connection_id":"`+start.ConnectionID+`","login_id":"other"}`, "agent.example.test"); out.Code != 409 {
		t.Fatalf("wrong login cancel=%d", out.Code)
	}
	v := usageBody(t, f.request("POST", "/api/usage/cancel", `{"profile_id":"`+f.id+`","connection_id":"`+start.ConnectionID+`","login_id":"fixture-login"}`, "agent.example.test"))
	if v.State != "disconnected" || v.Login != nil {
		t.Fatalf("cancel did not clear challenge: %+v", v)
	}
}
func TestUsageIdleWorkerStopsWithoutClearingSavedSession(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	s.idleTimeout = 20 * time.Millisecond
	start := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	rpc.signedIn = true
	usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		rpc.mu.Lock()
		closed := rpc.closed
		rpc.mu.Unlock()
		if closed {
			break
		}
		time.Sleep(time.Millisecond * 5)
	}
	rpc.mu.Lock()
	closed := rpc.closed
	rpc.mu.Unlock()
	if !closed {
		t.Fatal("idle worker not stopped")
	}
	v := usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	if v.ConnectionID != start.ConnectionID {
		t.Fatal("idle stop cleared saved connection")
	}
}
