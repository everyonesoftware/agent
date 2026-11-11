package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalAuthorizationStartStrictBody(t *testing.T) {
	f := newConnectionFixture(t)
	for _, body := range []string{
		`null`, `[]`, ``, `{`, `{} {}`, `{"unexpected":true}`,
		`{"expected_active_profile_id":42}`, `{"profile_id":"not-allowed"}`,
		`{"expected_active_profile_id":"` + strings.Repeat("x", 4096) + `"}`,
	} {
		out := f.request("POST", "/auth/start?mode=add", body, "127.0.0.1:8080")
		if out.Code != http.StatusBadRequest || !strings.Contains(out.Header().Get("Content-Type"), "application/json") {
			t.Errorf("body %.60q: %d %s", body, out.Code, out.Body.String())
		}
	}
}

func TestLocalAuthorizationStartModesPreserveOwnedStateWithoutPassword(t *testing.T) {
	for _, mode := range []string{"", "add", "retry"} {
		t.Run(mode, func(t *testing.T) {
			f := newConnectionFixture(t)
			service := f.accounts.siwcForUser(nil, f.user)
			service.config = siwcConfig{redirectURI: "http://127.0.0.1:8080/auth/callback", authorizeURL: "https://provider.example.test/authorize"}
			targetID := f.id
			if mode == "retry" {
				var err error
				targetID, err = newProfileID()
				if err != nil {
					t.Fatal(err)
				}
				pending := oauthCredentials{ClientID: "pending-client", HostID: testHostID, Issuer: siwcIssuer, RegistrationPending: true, OAuthPreferred: true}
				if err := f.store.SaveProfile(credentialProfile{ID: targetID, Label: "Incomplete account", Credentials: pending}); err != nil {
					t.Fatal(err)
				}
			}
			path := "/auth/start"
			if mode != "" {
				path += "?mode=" + mode
			}
			if mode == "retry" {
				path += "&profile_id=" + targetID
			}
			// Busy password derivation must not prevent authorization starts.
			for i := 0; i < cap(connectionPasswordSlots); i++ {
				connectionPasswordSlots <- struct{}{}
			}
			defer func() {
				for i := 0; i < cap(connectionPasswordSlots); i++ {
					<-connectionPasswordSlots
				}
			}()
			req := httptest.NewRequest("POST", "http://127.0.0.1:8080"+path, strings.NewReader(`{"expected_active_profile_id":"`+f.id+`"}`))
			req.AddCookie(f.cookie)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Origin", "http://127.0.0.1:8080")
			out := httptest.NewRecorder()
			f.accounts.scopedPasswordSIWCHandler(service, true, (*siwcService).startHandler).ServeHTTP(out, req)
			var result struct {
				URL string `json:"authorization_url"`
			}
			if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &result) != nil || !strings.HasPrefix(result.URL, "https://provider.example.test/authorize?") || out.Header().Get("Location") != "" {
				t.Fatalf("password-free %q: %d %s", mode, out.Code, out.Body.String())
			}
			if service.pending == nil || service.pending.previousActiveID != f.id {
				t.Fatal("lost captured active connection")
			}
			if mode != "add" && service.pending.profileID != targetID {
				t.Fatal("start changed named account")
			}
			if mode == "add" && service.pending.profileID == f.id {
				t.Fatal("add reused active account")
			}
			active, err := f.store.ActiveProfileID()
			if err != nil || active != f.id {
				t.Fatalf("start changed active account: %q %v", active, err)
			}
			_, token, err := f.accounts.register("isolated-user", "another-account-password")
			if err != nil {
				t.Fatal(err)
			}
			f.cookie = &http.Cookie{Name: authCookieName, Value: token}
			out = f.request("GET", "/api/auth/profiles", "", "127.0.0.1:8080")
			if out.Code != 200 || strings.Contains(out.Body.String(), f.id) {
				t.Fatalf("start leaked profiles: %d %s", out.Code, out.Body.String())
			}
		})
	}
}

func TestLocalAuthorizationStartInitialLinkRequiresCapturedEmptyTarget(t *testing.T) {
	f := newConnectionFixture(t)
	user, token, err := f.accounts.register("initial-link-user", "another-account-password")
	if err != nil {
		t.Fatal(err)
	}
	f.cookie = &http.Cookie{Name: authCookieName, Value: token}
	base := f.accounts.siwcForUser(nil, f.user)
	service := f.accounts.siwcForUser(base, user)
	service.config = siwcConfig{redirectURI: "http://127.0.0.1:8080/auth/callback", authorizeURL: "https://provider.example.test/authorize"}
	handler := f.accounts.scopedPasswordSIWCHandler(service, true, (*siwcService).startHandler)
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{}`, http.StatusBadRequest},
		{`{"expected_active_profile_id":null}`, http.StatusBadRequest},
		{`{"expected_active_profile_id":" "}`, http.StatusConflict},
		{`{"expected_active_profile_id":"` + f.id + `"}`, http.StatusConflict},
		{`{"expected_active_profile_id":""}`, http.StatusOK},
	} {
		req := httptest.NewRequest("POST", "http://127.0.0.1:8080/auth/start", strings.NewReader(tc.body))
		req.AddCookie(f.cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tc.want {
			t.Fatalf("initial link %s: %d %s", tc.body, out.Code, out.Body.String())
		}
		if tc.want != http.StatusOK && service.pending != nil {
			t.Fatal("missing or stale captured target prepared authorization")
		}
	}
	if service.pending == nil || !service.pending.activateIfUnset || service.pending.previousActiveID != "" {
		t.Fatal("initial linking lost its empty active-profile precondition")
	}
}

func TestLocalAuthorizationStartsDoNotConsumePasswordBudget(t *testing.T) {
	f := newConnectionFixture(t)
	for i := 0; i < 12; i++ {
		out := f.request("POST", "/auth/start?mode=add", `{}`, "127.0.0.1:8080")
		if out.Code != http.StatusServiceUnavailable {
			t.Fatalf("start %d: %d %s", i, out.Code, out.Body.String())
		}
	}
	out := f.request("POST", "/api/auth/profiles/active", `{"profile_id":"`+f.id+`","password":"correct-horse-battery"}`, "127.0.0.1:8080")
	if out.Code != http.StatusOK {
		t.Fatalf("starts consumed password confirmation budget: %d %s", out.Code, out.Body.String())
	}
}

func TestLocalAuthorizationStartRejectsBrowserOriginAndSessionFailures(t *testing.T) {
	f := newConnectionFixture(t)
	for _, tc := range []struct {
		name, origin, site string
		anonymous          bool
		want               int
	}{
		{name: "foreign origin", origin: "http://evil.example", want: 403},
		{name: "different loopback", origin: "http://localhost:8080", want: 403},
		{name: "different port", origin: "http://127.0.0.1:9090", want: 403},
		{name: "same-site", origin: "http://127.0.0.1:8080", site: "same-site", want: 403},
		{name: "cross-site", origin: "http://127.0.0.1:8080", site: "cross-site", want: 403},
		{name: "null origin", origin: "null", want: 403},
		{name: "signed out", anonymous: true, want: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://127.0.0.1:8080/auth/start?mode=add", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", tc.site)
			if !tc.anonymous {
				req.AddCookie(f.cookie)
			}
			out := httptest.NewRecorder()
			f.server.ServeHTTP(out, req)
			var result struct {
				Error string `json:"error"`
			}
			if out.Code != tc.want || json.Unmarshal(out.Body.Bytes(), &result) != nil || result.Error == "" {
				t.Fatalf("origin/session boundary: %d %s", out.Code, out.Body.String())
			}
		})
	}
	if err := f.accounts.logout(f.cookie.Value); err != nil {
		t.Fatal(err)
	}
	out := f.request("POST", "/auth/start?mode=add", `{}`, "127.0.0.1:8080")
	if out.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session: %d %s", out.Code, out.Body.String())
	}
}
