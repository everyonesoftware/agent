package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const resetPath = "/api/usage/resets/consume"
const attemptA = "d6549d1c-13d7-4cdf-9f44-258799eb98bf"
const attemptB = "d6549d1c-13d7-4cdf-9f44-258799eb98be"

type resetFixtureRPC struct {
	fakeUsageRPC
	summary      string
	outcome      string
	used         int
	writes       []string
	failReadback bool
}

func (rpc *resetFixtureRPC) Call(ctx context.Context, method string, params, result any) error {
	switch method {
	case "account/rateLimits/read":
		if rpc.failReadback && len(rpc.writes) != 0 {
			return fmt.Errorf("private-readback-error")
		}
		return json.Unmarshal([]byte(fmt.Sprintf(`{"rateLimits":{"limitId":"codex","primary":{"usedPercent":%d,"windowDurationMins":300,"resetsAt":2000000000}},"rateLimitResetCredits":%s}`, rpc.used, rpc.summary)), result)
	case "account/rateLimitResetCredit/consume":
		b, _ := json.Marshal(params)
		rpc.writes = append(rpc.writes, string(b))
		if rpc.outcome == "transport" {
			return fmt.Errorf("private-provider-secret")
		}
		if rpc.outcome == "reset" {
			rpc.used = 0
			rpc.summary = `{"availableCount":0,"credits":[]}`
		}
		return json.Unmarshal([]byte(fmt.Sprintf(`{"outcome":%q}`, rpc.outcome)), result)
	default:
		return rpc.fakeUsageRPC.Call(ctx, method, params, result)
	}
}

func newResetFixture(t *testing.T) (connectionFixture, *usageService, *resetFixtureRPC, string) {
	t.Helper()
	f := newConnectionFixture(t)
	app := f.server.(*applicationServer)
	t.Cleanup(app.close)
	s := app.usage
	s.now = func() time.Time { return time.Unix(1900000000, 0) }
	rpc := &resetFixtureRPC{fakeUsageRPC: fakeUsageRPC{email: "reset@example.test"}, used: 88, outcome: "reset", summary: `{"availableCount":2,"credits":[{"id":"later","status":"available","resetType":"codexRateLimits","expiresAt":null},{"id":"first","status":"available","resetType":"codexRateLimits","expiresAt":1900001000}]}`}
	s.executable = func() (string, error) { return "fixture", nil }
	s.factory = func(context.Context, string, string) (usageRPC, error) { return rpc, nil }
	start := usageBody(t, f.request("POST", "/auth/start", fmt.Sprintf(`{"expected_active_profile_id":%q}`, f.id), "agent.example.test"))
	rpc.signedIn = true
	return f, s, rpc, start.ConnectionID
}

func resetBody(profile, conn, credit, attempt string) string {
	expiry := "1900001000"
	if credit == "later" {
		expiry = "null"
	}
	return fmt.Sprintf(`{"profile_id":%q,"connection_id":%q,"credit_id":%q,"attempt_id":%q,"confirmed_count":2,"confirmed_expires_at":%s}`, profile, conn, credit, attempt, expiry)
}

func TestCodexResetRejectsChangedConfirmationTerms(t *testing.T) {
	for _, tc := range []struct{ name, summary string }{
		{"count", `{"availableCount":1,"credits":[{"id":"first","status":"available","resetType":"codexRateLimits","expiresAt":1900001000}]}`},
		{"expiry", `{"availableCount":2,"credits":[{"id":"first","status":"available","resetType":"codexRateLimits","expiresAt":1900002000},{"id":"later","status":"available","resetType":"codexRateLimits","expiresAt":null}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, rpc, conn := newResetFixture(t)
			usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
			rpc.summary = tc.summary
			got := f.request("POST", resetPath, resetBody(f.id, conn, "first", attemptA), "agent.example.test")
			if got.Code != 409 || len(rpc.writes) != 0 {
				t.Fatalf("reset=%d %s writes=%v", got.Code, got.Body, rpc.writes)
			}
		})
	}
}

func TestCodexResetRequiresExplicitConfirmationTerms(t *testing.T) {
	f, _, rpc, conn := newResetFixture(t)
	rpc.summary = `{"availableCount":1,"credits":[{"id":"later","status":"available","resetType":"codexRateLimits","expiresAt":null}]}`
	usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	body := strings.Replace(resetBody(f.id, conn, "later", attemptA), `"confirmed_count":2`, `"confirmed_count":1`, 1)
	for _, tc := range []struct {
		name, before, after string
		status              int
	}{
		{"missing expiry", `,"confirmed_expires_at":null`, "", 400},
		{"invalid expiry", `"confirmed_expires_at":null`, `"confirmed_expires_at":1.5`, 400},
		{"nonmatching expiry", `"confirmed_expires_at":null`, `"confirmed_expires_at":1900001000`, 409},
		{"missing count", `,"confirmed_count":1`, "", 400},
		{"null count", `"confirmed_count":1`, `"confirmed_count":null`, 400},
		{"zero count", `"confirmed_count":1`, `"confirmed_count":0`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := f.request("POST", resetPath, strings.Replace(body, tc.before, tc.after, 1), "agent.example.test")
			if got.Code != tc.status || len(rpc.writes) != 0 {
				t.Errorf("reset=%d %s writes=%v", got.Code, got.Body, rpc.writes)
			}
		})
	}
	got := f.request("POST", resetPath, body, "agent.example.test")
	if got.Code != 200 || len(rpc.writes) != 1 {
		t.Fatalf("explicit null=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
}

func TestCodexResetStatusUsesProviderEvidenceWithoutWriting(t *testing.T) {
	for _, tc := range []struct{ name, summary, want string }{
		{"complete", "", `"expirations":[{"count":1,"expires_at":1900001000},{"count":1,"expires_at":null}],"details_complete":true,"next":{"credit_id":"first","expires_at":1900001000},"history":[]`},
		{"count-only", `{"availableCount":3,"credits":null}`, `"available_count":3,"expirations":[],"details_complete":false,"history":[]`},
		{"truncated", `{"availableCount":3,"credits":[{"id":"only","status":"available","resetType":"codexRateLimits","expiresAt":1900001000}]}`, `"available_count":3,"expirations":[{"count":1,"expires_at":1900001000}],"details_complete":false,"history":[]`},
		{"duplicate", `{"availableCount":2,"credits":[{"id":"same","status":"available","resetType":"codexRateLimits"},{"id":"same","status":"available","resetType":"codexRateLimits"}]}`, `"details_complete":false,"history":[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, rpc, _ := newResetFixture(t)
			if tc.summary != "" {
				rpc.summary = tc.summary
			}
			got := f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test")
			if got.Code != 200 || !strings.Contains(got.Body.String(), tc.want) || len(rpc.writes) != 0 {
				t.Fatalf("status=%d body=%s writes=%v", got.Code, got.Body, rpc.writes)
			}
		})
	}
}

func TestCodexResetConsumesConfirmedCreditAndPersistsAcknowledgement(t *testing.T) {
	f, s, rpc, conn := newResetFixture(t)
	statusURL := "/api/usage/status?profile_id=" + f.id
	usageBody(t, f.request("GET", statusURL, "", "agent.example.test"))
	s.now = func() time.Time { return time.Unix(1900000001, 0) }
	body := resetBody(f.id, conn, "first", attemptA)
	got := f.request("POST", resetPath, body, "agent.example.test")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"outcome":"reset"`) || !strings.Contains(got.Body.String(), `"used_percent":0`) || !strings.Contains(got.Body.String(), `"history":[{"kind":"used","at":1900000001}]`) {
		t.Fatalf("reset=%d %s", got.Code, got.Body)
	}
	if len(rpc.writes) != 1 || rpc.writes[0] != fmt.Sprintf(`{"creditId":"first","idempotencyKey":%q}`, attemptA) {
		t.Fatalf("writes=%v", rpc.writes)
	}
	s2 := newUsageService(s.accounts, s.base)
	t.Cleanup(s2.close)
	s2.now, s2.factory, s2.executable = s.now, s.factory, s.executable
	mux := http.NewServeMux()
	s2.register(mux)
	f.server = mux
	got = f.request("GET", statusURL, "", "agent.example.test")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"history":[{"kind":"used","at":1900000001}]`) {
		t.Fatalf("reloaded status=%d %s", got.Code, got.Body)
	}
	got = f.request("POST", resetPath, body, "agent.example.test")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"outcome":"alreadyRedeemed"`) || len(rpc.writes) != 1 {
		t.Fatalf("duplicate=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
}

func TestCodexResetRejectsChangedTargetAndUnconfirmedScope(t *testing.T) {
	f, _, rpc, conn := newResetFixture(t)
	usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	cases := []struct {
		name, profile, connection, credit, key string
		code                                   int
	}{
		{"stale connection", f.id, "changed", "first", attemptA, 409},
		{"stale target", f.id, conn, "later", attemptA, 409},
		{"invalid UUID", f.id, conn, "first", "not-uuid", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := f.request("POST", resetPath, resetBody(tc.profile, tc.connection, tc.credit, tc.key), "agent.example.test")
			if got.Code != tc.code {
				t.Fatalf("reset=%d %s", got.Code, got.Body)
			}
		})
	}
	for _, tc := range []struct {
		name, origin  string
		authenticated bool
		code          int
	}{{"no session", "https://agent.example.test", false, 401}, {"cross site", "https://other.example.test", true, 403}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://agent.example.test"+resetPath, strings.NewReader(resetBody(f.id, conn, "first", attemptA)))
			r.Header.Set("Origin", tc.origin)
			if tc.authenticated {
				r.AddCookie(f.cookie)
			}
			out := httptest.NewRecorder()
			f.server.ServeHTTP(out, r)
			if out.Code != tc.code {
				t.Fatalf("reset=%d %s", out.Code, out.Body)
			}
		})
	}
	rpc.email = "changed@example.test"
	got := f.request("POST", resetPath, resetBody(f.id, conn, "first", attemptA), "agent.example.test")
	if got.Code != 409 || len(rpc.writes) != 0 {
		t.Fatalf("reset=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
}

func TestCodexResetAmbiguityIsNotRetriedOrTransferredToNewKey(t *testing.T) {
	f, s, rpc, conn := newResetFixture(t)
	statusURL := "/api/usage/status?profile_id=" + f.id
	usageBody(t, f.request("GET", statusURL, "", "agent.example.test"))
	rpc.outcome = "transport"
	got := f.request("POST", resetPath, resetBody(f.id, conn, "first", attemptA), "agent.example.test")
	if got.Code != 503 || strings.Contains(got.Body.String(), "private-provider-secret") || len(rpc.writes) != 1 {
		t.Fatalf("reset=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
	reopened := newUsageService(s.accounts, s.base)
	t.Cleanup(reopened.close)
	reopened.now, reopened.factory, reopened.executable = s.now, s.factory, s.executable
	mux := http.NewServeMux()
	reopened.register(mux)
	f.server = mux
	got = f.request("GET", statusURL, "", "agent.example.test")
	if got.Code != 200 || resetPendingID(t, got) != attemptA || strings.Contains(got.Body.String(), `"kind":"used"`) || len(rpc.writes) != 1 {
		t.Fatalf("status=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
	got = f.request("POST", resetPath, resetBody(f.id, conn, "first", attemptB), "agent.example.test")
	if got.Code != 409 || len(rpc.writes) != 1 {
		t.Fatalf("new key=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
	stale := strings.Replace(resetBody(f.id, conn, "first", attemptA), `"confirmed_expires_at":1900001000`, `"confirmed_expires_at":null`, 1)
	got = f.request("POST", resetPath, stale, "agent.example.test")
	if got.Code != 409 || len(rpc.writes) != 1 {
		t.Fatalf("same key with stale terms=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
	rpc.outcome = "alreadyRedeemed"
	got = f.request("POST", resetPath, resetBody(f.id, conn, "first", attemptA), "agent.example.test")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"outcome":"alreadyRedeemed"`) || len(rpc.writes) != 2 || rpc.writes[0] != rpc.writes[1] {
		t.Fatalf("same key=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
	if resetPendingID(t, got) != "" || strings.Contains(got.Body.String(), `"pending_attempt_id"`) {
		t.Fatalf("terminal acknowledgement exposed pending attempt: %s", got.Body)
	}
}

func resetPendingID(t *testing.T, out *httptest.ResponseRecorder) string {
	t.Helper()
	type status struct {
		Resets struct {
			Next struct {
				PendingID string `json:"pending_attempt_id"`
			} `json:"next"`
		} `json:"resets"`
	}
	var payload struct {
		status
		Status *status `json:"status"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode reset status: %v", err)
	}
	if payload.Status != nil {
		return payload.Status.Resets.Next.PendingID
	}
	return payload.Resets.Next.PendingID
}

func TestCodexResetPendingAttemptIsOnlyExposedInItsExactScope(t *testing.T) {
	f, s, rpc, conn := newResetFixture(t)
	registered := f.request("POST", "/api/auth/register", `{"username":"other-reset-user","password":"test-account-password"}`, "agent.example.test")
	var other authUser
	if registered.Code != 201 || json.Unmarshal(registered.Body.Bytes(), &other) != nil || other.ID == "" {
		t.Fatalf("register second owner: %d %s", registered.Code, registered.Body)
	}
	statusURL := "/api/usage/status?profile_id=" + f.id
	usageBody(t, f.request("GET", statusURL, "", "agent.example.test"))
	rpc.outcome = "transport"
	got := f.request("POST", resetPath, resetBody(f.id, conn, "first", attemptA), "agent.example.test")
	if got.Code != 503 || len(rpc.writes) != 1 {
		t.Fatalf("initial attempt=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
	for _, tc := range []struct{ column, original, different string }{
		{"user_id", f.user.ID, other.ID},
		{"credit_id", "first", "later"},
		{"profile_id", f.id, "other-profile"},
		{"connection_id", conn, "other-connection"},
		{"identity_key", rpc.email, "other@example.test"},
	} {
		t.Run(tc.column, func(t *testing.T) {
			// Move the persisted attempt outside this scope while retaining all
			// the other fields, to catch missing predicates individually.
			update := func(value string) {
				t.Helper()
				if _, err := s.accounts.db.Exec(`UPDATE usage_reset_attempts SET `+tc.column+`=? WHERE user_id IN (?,?) AND attempt_id=?`, value, f.user.ID, other.ID, attemptA); err != nil {
					t.Fatal(err)
				}
			}
			update(tc.different)
			defer update(tc.original)
			got := f.request("GET", statusURL, "", "agent.example.test")
			if got.Code != 200 || !strings.Contains(got.Body.String(), `"next":{"credit_id":"first"`) || strings.Contains(got.Body.String(), `"pending_attempt_id"`) || len(rpc.writes) != 1 {
				t.Fatalf("out-of-scope status=%d %s writes=%v", got.Code, got.Body, rpc.writes)
			}
		})
	}
	got = f.request("GET", statusURL, "", "agent.example.test")
	if got.Code != 200 || resetPendingID(t, got) != attemptA || len(rpc.writes) != 1 {
		t.Fatalf("in-scope status=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
}

func TestCodexResetPendingTargetDisappearsWithoutSubstitutingAnotherCredit(t *testing.T) {
	f, _, rpc, conn := newResetFixture(t)
	usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
	rpc.outcome = "transport"
	got := f.request("POST", resetPath, resetBody(f.id, conn, "first", attemptA), "agent.example.test")
	if got.Code != 503 || len(rpc.writes) != 1 {
		t.Fatalf("initial attempt=%d %s writes=%v", got.Code, got.Body, rpc.writes)
	}
	rpc.summary = `{"availableCount":1,"credits":[{"id":"later","status":"available","resetType":"codexRateLimits","expiresAt":null}]}`
	status := f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test")
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"next":{"credit_id":"later"`) || strings.Contains(status.Body.String(), `"pending_attempt_id"`) {
		t.Fatalf("replacement credit status=%d %s", status.Code, status.Body)
	}
	for _, credit := range []string{"first", "later"} {
		body := strings.Replace(resetBody(f.id, conn, credit, attemptA), `"confirmed_count":2`, `"confirmed_count":1`, 1)
		got = f.request("POST", resetPath, body, "agent.example.test")
		if got.Code != 409 || len(rpc.writes) != 1 || strings.Contains(got.Body.String(), `"outcome"`) {
			t.Fatalf("pending reconciliation=%d %s writes=%v", got.Code, got.Body, rpc.writes)
		}
	}
}

func TestCodexResetNoOpAllowsLaterExplicitAttemptWithNewKey(t *testing.T) {
	for _, outcome := range []string{"nothingToReset", "noCredit"} {
		t.Run(outcome, func(t *testing.T) {
			f, _, rpc, conn := newResetFixture(t)
			usageBody(t, f.request("GET", "/api/usage/status?profile_id="+f.id, "", "agent.example.test"))
			rpc.outcome = outcome
			for _, key := range []string{attemptA, attemptA} {
				got := f.request("POST", resetPath, resetBody(f.id, conn, "first", key), "agent.example.test")
				if got.Code != 200 || !strings.Contains(got.Body.String(), `"outcome":"`+outcome+`"`) || strings.Contains(got.Body.String(), `"kind":"used"`) {
					t.Fatalf("no-op=%d %s", got.Code, got.Body)
				}
			}
			if len(rpc.writes) != 1 {
				t.Fatalf("same-key no-op replay sent a provider write: %v", rpc.writes)
			}
			rpc.outcome = "reset"
			got := f.request("POST", resetPath, resetBody(f.id, conn, "first", attemptB), "agent.example.test")
			if got.Code != 200 || !strings.Contains(got.Body.String(), `"outcome":"reset"`) || !strings.Contains(got.Body.String(), `"kind":"used"`) || len(rpc.writes) != 2 || !strings.Contains(rpc.writes[1], attemptB) {
				t.Fatalf("new key=%d %s writes=%v", got.Code, got.Body, rpc.writes)
			}
		})
	}
}

func TestCodexResetAcknowledgementWithFailedReadbackDoesNotInventAllowance(t *testing.T) {
	f, s, rpc, conn := newResetFixture(t)
	statusURL := "/api/usage/status?profile_id=" + f.id
	usageBody(t, f.request("GET", statusURL, "", "agent.example.test"))
	s.now = func() time.Time { return time.Unix(1900000001, 0) }
	rpc.failReadback = true
	got := f.request("POST", resetPath, resetBody(f.id, conn, "first", attemptA), "agent.example.test")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"outcome":"reset"`) || !strings.Contains(got.Body.String(), `"stale":true`) ||
		!strings.Contains(got.Body.String(), `"used_percent":88`) || strings.Contains(got.Body.String(), `"resets":`) || strings.Contains(got.Body.String(), "private-readback-error") {
		t.Fatalf("reset=%d %s", got.Code, got.Body)
	}
	var count int
	if err := s.accounts.db.QueryRow(`SELECT COUNT(*) FROM usage_observations WHERE remaining=100`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invented quota observations: count=%d err=%v", count, err)
	}
	rpc.failReadback = false
	recovered := f.request("GET", statusURL, "", "agent.example.test")
	if recovered.Code != 200 || !strings.Contains(recovered.Body.String(), `"used_percent":0`) || !strings.Contains(recovered.Body.String(), `"kind":"used"`) || len(rpc.writes) != 1 {
		t.Fatalf("recovered=%d %s writes=%v", recovered.Code, recovered.Body, rpc.writes)
	}
}
