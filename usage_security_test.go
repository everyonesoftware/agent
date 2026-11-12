package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUsageDifferentAccountDoesNotExposeQuota(t *testing.T) {
	f, _, rpc := newUsageFixture(t)
	usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	rpc.signedIn = true
	rpc.email = "different@example.com"
	v := usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	if v.IdentityMatch != "different" || v.Snapshot != nil || v.Identity == nil || v.Identity.Email != "different@example.com" {
		t.Fatalf("mismatched quota exposed: %+v", v)
	}
	for _, method := range rpc.calls {
		if method == "account/rateLimits/read" {
			t.Fatal("read quota for mismatched identity")
		}
	}
}
func TestUsagePendingCannotBeReplacedWithoutCancel(t *testing.T) {
	f, _, _ := newUsageFixture(t)
	start := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	out := f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":"`+start.ConnectionID+`"}`, "agent.example.test")
	if out.Code != http.StatusConflict {
		t.Errorf("duplicate ceremony = %d, want 409", out.Code)
	}
	v := usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	if v.State != "pending" || v.Login == nil || v.ConnectionID != start.ConnectionID {
		t.Fatal("duplicate connect destroyed original sign-in")
	}
}
func TestUsageInterruptedCeremonyCanBeRestarted(t *testing.T) {
	f, _, rpc := newUsageFixture(t)
	start := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	rpc.fail = "account/read"
	failed := usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	if failed.Login != nil {
		t.Error("interrupted worker left a usable-looking challenge")
	}
	rpc.fail = ""
	restart := f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":"`+start.ConnectionID+`"}`, "agent.example.test")
	if restart.Code != 200 {
		t.Fatalf("interrupted sign-in cannot restart: %d", restart.Code)
	}
}

func TestUsageNeverReadsAnotherAgentUsersConnection(t *testing.T) {
	f, _, rpc := newUsageFixture(t)
	_, token, err := f.accounts.register("second-usage-user", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	otherCookie := f.cookie
	f.cookie = &http.Cookie{Name: otherCookie.Name, Value: token}
	out := f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test")
	if out.Code != 409 {
		t.Fatalf("cross-user read = %d", out.Code)
	}
	if len(rpc.calls) != 0 {
		t.Fatal("cross-user request contacted Codex")
	}
}
func TestUsageMissingRuntimeDoesNotRequireNewConversationAuthorization(t *testing.T) {
	f, s, _ := newUsageFixture(t)
	s.executable = func() (string, error) { return "", errors.New("missing binary") }
	out := f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test")
	v := usageBody(t, out)
	if v.Available || v.State != "disconnected" {
		t.Fatalf("missing dependency: %+v", v)
	}
	out = f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test")
	if out.Code != 503 || !strings.Contains(out.Body.String(), "runtime") {
		t.Fatalf("missing binary start: %d %s", out.Code, out.Body.String())
	}
	if _, err := f.store.LoadProfile(f.id); err != nil {
		t.Fatal("SIWC account removed")
	}
}
func TestUsageNewConnectionNeverAdoptsOrphanedCredentials(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	home := filepath.Join(f.accounts.dataDir, "account-data", f.user.ID, "usage", f.id, "codex-home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte("orphaned-test-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	s.factory = func(_ context.Context, _ string, dir string) (usageRPC, error) {
		if _, err := os.Stat(filepath.Join(dir, "auth.json")); !errors.Is(err, os.ErrNotExist) {
			t.Error("new ceremony adopted orphaned credentials")
		}
		return rpc, nil
	}
	usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
}

func TestUsageExpiredChallengeDoesNotRemainUsable(t *testing.T) {
	f, s, rpc := newUsageFixture(t)
	start := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	s.now = func() time.Time { return time.Unix(start.Login.ExpiresAt+1, 0) }
	v := usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	if v.State != "expired" || v.Login != nil || !rpc.closed {
		t.Fatalf("expired challenge: %+v", v)
	}
}
func TestUsageLocalDisconnectDoesNotClaimRemoteRevocation(t *testing.T) {
	f, _, _ := newUsageFixture(t)
	start := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	v := usageBody(t, f.request("POST", "/api/usage/disconnect", `{"profile_id":"`+f.id+`","connection_id":"`+start.ConnectionID+`","password":"correct-horse-battery"}`, "agent.example.test"))
	if v.Error == "" || !strings.Contains(v.Error, "locally") {
		t.Fatal("local logout implied confirmed remote revocation")
	}
}
func TestUsageFailedCleanupCannotKeepCachedIdentityActive(t *testing.T) {
	f, _, rpc := newUsageFixture(t)
	start := usageBody(t, f.request("POST", "/api/usage/connect", `{"profile_id":"`+f.id+`","expected_connection_id":""}`, "agent.example.test"))
	rpc.signedIn = true
	usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	home := filepath.Join(f.accounts.dataDir, "account-data", f.user.ID, "usage", f.id, "codex-home")
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), home); err != nil {
		t.Skip("symlink not supported")
	}
	out := f.request("POST", "/api/usage/disconnect", `{"profile_id":"`+f.id+`","connection_id":"`+start.ConnectionID+`","password":"correct-horse-battery"}`, "agent.example.test")
	if out.Code != 503 {
		t.Fatalf("unsafe cleanup should fail closed, got %d", out.Code)
	}
	v := usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	if v.Identity != nil || v.Snapshot != nil || v.State == "connected" {
		t.Fatalf("disabled context remained active: %+v", v)
	}
}
func TestUsageNormalizerKeepsBucketsAndUnknownFieldsUnknown(t *testing.T) {
	percent := 12.5
	minutes := int64(75)
	reset := int64(2000000000)
	invalid := -1.0
	raw := usageRateLimits{ByID: map[string]usageRateBucket{
		"codex": {Primary: &usageRateWindow{UsedPercent: &percent, Minutes: &minutes, ResetAt: &reset}, Secondary: &usageRateWindow{}},
		"other": {Primary: &usageRateWindow{UsedPercent: &invalid}},
	}}
	out := normalizeUsageWindows(raw)
	if len(out) != 1 || out[0].ID != "codex:primary" || out[0].UsedPercent != percent || out[0].WindowMinutes != 75 || out[0].ResetAt != reset {
		t.Fatalf("bad normalized windows: %+v", out)
	}
	if validUsageVerificationURL("https://auth.openai.com.evil.test/codex/device") || validUsageVerificationURL("javascript:alert(1)") {
		t.Fatal("untrusted verification URL accepted")
	}
}
