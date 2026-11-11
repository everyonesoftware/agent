package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type connectionFixture struct {
	server   http.Handler
	accounts *accountStore
	user     authUser
	cookie   *http.Cookie
	store    credentialStore
	id       string
}

func newConnectionFixture(t *testing.T) connectionFixture {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APP_DATA_DIR", dir)
	t.Setenv("SIWC_REDIRECT_URI", "invalid-test-callback")
	server := newServer()
	req := httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(`{"username":"confirmation-user","password":"correct-horse-battery"}`))
	out := httptest.NewRecorder()
	server.ServeHTTP(out, req)
	if out.Code != 201 {
		t.Fatalf("register: %d %s", out.Code, out.Body.String())
	}
	var user authUser
	if err := json.Unmarshal(out.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	accounts, err := newAccountStore(filepath.Join(dir, "everyoneagent.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { accounts.db.Close() })
	store := accounts.siwcForUser(newSIWCService(siwcConfig{}, newFileCredentialStore(t.TempDir())), user).store
	id, err := newProfileID()
	if err != nil {
		t.Fatal(err)
	}
	credentials := testOAuthCredentials("", "", time.Time{})
	credentials.IDToken = ""
	credentials.TokenType = ""
	credentials.Scopes = nil
	credentials.OAuthPreferred = true
	if err := store.SaveProfile(credentialProfile{ID: id, Label: "Private connection", Credentials: credentials}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetActiveProfile(id); err != nil {
		t.Fatal(err)
	}
	return connectionFixture{server, accounts, user, out.Result().Cookies()[0], store, id}
}
func (f connectionFixture) request(method, path, body, host string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = host
	req.AddCookie(f.cookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://"+host)
	out := httptest.NewRecorder()
	f.server.ServeHTTP(out, req)
	return out
}

func TestConnectionPasswordOAuthStartsArePOSTAndLocal(t *testing.T) {
	f := newConnectionFixture(t)
	out := f.request("GET", "/auth/start?password=correct-horse-battery", "", "127.0.0.1:8080")
	if out.Code != 405 {
		t.Fatalf("GET must never start OAuth: %d %s", out.Code, out.Body.String())
	}
	for _, mode := range []string{"", "?mode=add", "?mode=retry&profile_id=" + f.id} {
		for _, host := range []string{"127.0.0.1:8080", "agent.example.test"} {
			out = f.request("POST", "/auth/start"+mode, `{"expected_active_profile_id":"`+f.id+`"}`, host)
			want := http.StatusServiceUnavailable // Fixture callback is intentionally invalid.
			if host == "agent.example.test" {
				want = http.StatusForbidden
			}
			if out.Code != want {
				t.Fatalf("password-free local start boundary: %s %s: %d %s", host, mode, out.Code, out.Body.String())
			}
		}
	}
	out = f.request("POST", "/auth/start", `{"password":"correct-horse-battery","expected_active_profile_id":"`+f.id+`"}`, "agent.example.test")
	if out.Code != 403 || !strings.Contains(out.Body.String(), "loopback") || !strings.Contains(out.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("remote OAuth limitation: %d %s", out.Code, out.Body.String())
	}
}

func TestConnectionPasswordOAuthJSONAndCapturedTarget(t *testing.T) {
	f := newConnectionFixture(t)
	service := f.accounts.siwcForUser(nil, f.user)
	service.config = siwcConfig{redirectURI: "http://127.0.0.1:8080/auth/callback", authorizeURL: "https://provider.example.test/authorize"}
	mux := http.NewServeMux()
	mux.Handle("POST /auth/start", f.accounts.scopedPasswordSIWCHandler(service, true, (*siwcService).startHandler))
	f.server = mux
	for _, expected := range []string{"", "profile_abcdefghijklmnopqrstuv"} {
		out := f.request("POST", "/auth/start", `{"expected_active_profile_id":"`+expected+`"}`, "127.0.0.1:8080")
		if out.Code != 409 {
			t.Fatalf("changed target: %d %s", out.Code, out.Body.String())
		}
		if service.pending != nil {
			t.Fatal("stale confirmation started authorization")
		}
	}
	req := httptest.NewRequest("POST", "/auth/start", strings.NewReader(`{"expected_active_profile_id":"`+f.id+`"}`))
	req.Host = "127.0.0.1:8080"
	req.AddCookie(f.cookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	out := httptest.NewRecorder()
	f.server.ServeHTTP(out, req)
	var result struct {
		URL string `json:"authorization_url"`
	}
	json.Unmarshal(out.Body.Bytes(), &result)
	if out.Code != 200 || !strings.HasPrefix(result.URL, "https://provider.example.test/authorize?") || out.Header().Get("Location") != "" || service.pending == nil {
		t.Fatalf("JSON OAuth start: %d %s", out.Code, out.Body.String())
	}
	if strings.Contains(result.URL, "correct-horse-battery") {
		t.Fatal("password leaked to authorize URL")
	}
	req = httptest.NewRequest("POST", "/auth/start?mode=add", strings.NewReader("password=correct-horse-battery"))
	req.Host = "127.0.0.1:8080"
	req.AddCookie(f.cookie)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	out = httptest.NewRecorder()
	f.server.ServeHTTP(out, req)
	if out.Code != 302 || !strings.HasPrefix(out.Header().Get("Location"), "https://provider.example.test/authorize?") {
		t.Fatalf("form OAuth: %d %s", out.Code, out.Body.String())
	}
}

func TestConnectionPasswordErrorsAreJSONAndPreserveState(t *testing.T) {
	for _, tc := range []struct {
		name, body, host, origin string
		status                   int
		anonymous                bool
	}{
		{name: "local missing", body: `{"profile_id":"ID"}`, host: "127.0.0.1:8080", status: 401},
		{name: "local wrong", body: `{"profile_id":"ID","password":"wrong-password"}`, host: "127.0.0.1:8080", status: 401},
		{name: "invalid exact ID", body: `{"profile_id":" ID ","password":"correct-horse-battery"}`, host: "agent.example.test", status: 400},
		{name: "unknown profile", body: `{"profile_id":"profile_abcdefghijklmnopqrstuv","password":"correct-horse-battery"}`, host: "agent.example.test", status: 404},
		{name: "trailing JSON", body: `{"profile_id":"ID","password":"correct-horse-battery"} {}`, host: "agent.example.test", status: 400},
		{name: "cross-site", body: `{"profile_id":"ID","password":"correct-horse-battery"}`, host: "agent.example.test", origin: "https://evil.example.test", status: 403},
		{name: "unauthenticated", body: `{"profile_id":"ID","password":"correct-horse-battery"}`, host: "agent.example.test", status: 401, anonymous: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnectionFixture(t)
			req := httptest.NewRequest("POST", "/api/auth/profiles/active", strings.NewReader(strings.ReplaceAll(tc.body, "ID", f.id)))
			req.Host = tc.host
			req.Header.Set("Content-Type", "application/json")
			if !tc.anonymous {
				req.AddCookie(f.cookie)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			out := httptest.NewRecorder()
			f.server.ServeHTTP(out, req)
			if out.Code != tc.status || !strings.Contains(out.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("confirmation error: %d %s %s", out.Code, out.Header().Get("Content-Type"), out.Body.String())
			}
			active, err := f.store.ActiveProfileID()
			if err != nil || active != f.id {
				t.Fatalf("failed confirmation changed active ID: %q %v", active, err)
			}
		})
	}
}

func TestConnectionPasswordThrottlingDoesNotGateReads(t *testing.T) {
	f := newConnectionFixture(t)
	for i := 0; i < 10; i++ {
		out := f.request("POST", "/api/auth/disconnect", `{"profile_id":"`+f.id+`","password":"wrong-password"}`, "agent.example.test")
		if out.Code != 401 {
			t.Fatalf("attempt %d: %d %s", i, out.Code, out.Body.String())
		}
	}
	// A fresh session must not reset the authenticated user's attempt budget.
	f.cookie = &http.Cookie{Name: authCookieName, Value: tokenForTest(t, f.accounts, f.user.ID)}
	out := f.request("POST", "/api/auth/profiles/active", `{"profile_id":"`+f.id+`","password":"correct-horse-battery"}`, "agent.example.test")
	if out.Code != 429 || out.Header().Get("Retry-After") == "" {
		t.Fatalf("throttle: %d %s", out.Code, out.Body.String())
	}
	out = f.request("GET", "/api/auth/profiles", "", "agent.example.test")
	if out.Code != 200 {
		t.Fatalf("read was throttled: %d", out.Code)
	}
	profile, err := f.store.LoadProfile(f.id)
	if err != nil || !profile.Credentials.OAuthPreferred {
		t.Fatalf("throttled action changed profile: %v", err)
	}
}

func TestConnectionPasswordBoundedBcryptDoesNotGateReads(t *testing.T) {
	f := newConnectionFixture(t)
	for i := 0; i < cap(connectionPasswordSlots); i++ {
		connectionPasswordSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(connectionPasswordSlots); i++ {
			<-connectionPasswordSlots
		}
	}()
	out := f.request("POST", "/api/auth/disconnect", `{"profile_id":"`+f.id+`","password":"correct-horse-battery"}`, "agent.example.test")
	if out.Code != 429 {
		t.Fatalf("busy KDF: %d %s", out.Code, out.Body.String())
	}
	out = f.request("GET", "/api/auth/profiles", "", "agent.example.test")
	if out.Code != 200 {
		t.Fatalf("read blocked by KDF: %d", out.Code)
	}
}

func TestConnectionPasswordCannotUseAnotherUsersPasswordOrProfile(t *testing.T) {
	f := newConnectionFixture(t)
	_, token, err := f.accounts.register("another-user", "another-account-password")
	if err != nil {
		t.Fatal(err)
	}
	out := f.request("POST", "/api/auth/disconnect", `{"profile_id":"`+f.id+`","password":"another-account-password"}`, "agent.example.test")
	if out.Code != 401 {
		t.Fatalf("accepted other user's password: %d", out.Code)
	}
	f.cookie = &http.Cookie{Name: authCookieName, Value: token}
	out = f.request("GET", "/api/auth/profiles", "", "agent.example.test")
	if out.Code != 200 || strings.Contains(out.Body.String(), f.id) {
		t.Fatalf("profile ownership leaked: %d %s", out.Code, out.Body.String())
	}
	out = f.request("POST", "/api/auth/profiles/active", `{"profile_id":"`+f.id+`","password":"another-account-password"}`, "agent.example.test")
	if out.Code != 404 {
		t.Fatalf("cross-user selection: %d %s", out.Code, out.Body.String())
	}
	active, err := f.store.ActiveProfileID()
	if err != nil || active != f.id {
		t.Fatalf("cross-user mutation changed owner: %q %v", active, err)
	}
}

func TestConnectionPasswordRechecksSessionAndHashAfterBcrypt(t *testing.T) {
	for _, change := range []string{"logout", "password"} {
		t.Run(change, func(t *testing.T) {
			f := newConnectionFixture(t)
			gated, gate := passwordGatedStore(t, f.accounts.dataDir, "UPDATE users SET password_hash")
			called := false
			handler := gated.scopedPasswordSIWCHandler(nil, false, func(_ *siwcService, w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- passwordRequest(handler, "POST", "/api/auth/disconnect", `{"profile_id":"`+f.id+`","password":"correct-horse-battery"}`, f.cookie)
			}()
			var once sync.Once
			unblock := func() { once.Do(func() { close(gate.release) }) }
			defer unblock()
			select {
			case <-gate.reached:
			case <-time.After(10 * time.Second):
				t.Fatal("confirmation did not reach post-bcrypt check")
			}
			if change == "logout" {
				if err := f.accounts.logout(f.cookie.Value); err != nil {
					t.Fatal(err)
				}
			} else {
				out := passwordRequest(f.server, "POST", "/api/auth/password", passwordBody("correct-horse-battery", "replacement-account-password"), f.cookie)
				if out.Code != 200 {
					t.Fatalf("password rotation: %d %s", out.Code, out.Body.String())
				}
			}
			unblock()
			select {
			case out := <-done:
				if out.Code != 401 || called {
					t.Fatalf("stale confirmation: %d called=%v %s", out.Code, called, out.Body.String())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("confirmation did not complete")
			}
		})
	}
}

func TestConnectionPasswordRejectsSessionExpiringWhileWaitingForWriter(t *testing.T) {
	f := newConnectionFixture(t)
	expiry := time.Now().Unix() + 2
	if _, err := f.accounts.db.Exec(`UPDATE sessions SET expires_at=? WHERE token_hash=?`, expiry, hashSession(f.cookie.Value)); err != nil {
		t.Fatal(err)
	}
	gated, gate := passwordGatedStore(t, f.accounts.dataDir, "UPDATE users SET password_hash")
	gated.oauthServices = map[string]*siwcService{f.user.ID: f.accounts.siwcForUser(nil, f.user)}
	called := false
	handler := gated.scopedPasswordSIWCHandler(nil, false, func(_ *siwcService, w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- passwordRequest(handler, "POST", "/api/auth/disconnect", `{"profile_id":"`+f.id+`","password":"correct-horse-battery"}`, f.cookie)
	}()
	var once sync.Once
	unblock := func() { once.Do(func() { close(gate.release) }) }
	defer unblock()
	select {
	case <-gate.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("confirmation never reached writer")
	}
	time.Sleep(time.Until(time.Unix(expiry, 0).Add(20 * time.Millisecond)))
	unblock()
	select {
	case out := <-done:
		if out.Code != 401 || called {
			t.Fatalf("expired confirmation: %d called=%v", out.Code, called)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("confirmation did not complete")
	}
}

func TestConnectionPasswordAllRoutesRejectCrossSiteAndMissingSession(t *testing.T) {
	f := newConnectionFixture(t)
	for _, path := range []string{"/auth/start", "/api/auth/profiles", "/api/auth/profiles/active", "/api/auth/disconnect"} {
		for _, anonymous := range []bool{false, true} {
			method := "POST"
			if path == "/api/auth/profiles" {
				method = "GET"
			}
			req := httptest.NewRequest(method, path, strings.NewReader(`{"password":"correct-horse-battery","profile_id":"`+f.id+`"}`))
			req.Host = "agent.example.test"
			want := 401
			if !anonymous {
				req.AddCookie(f.cookie)
				req.Header.Set("Sec-Fetch-Site", "cross-site")
				want = 403
			}
			out := httptest.NewRecorder()
			f.server.ServeHTTP(out, req)
			if out.Code != want {
				t.Fatalf("%s anonymous=%v: %d %s", path, anonymous, out.Code, out.Body.String())
			}
		}
	}
	active, err := f.store.ActiveProfileID()
	if err != nil || active != f.id {
		t.Fatalf("rejected requests changed active profile: %q %v", active, err)
	}
}

func TestConnectionPasswordRemoteReadAndMutations(t *testing.T) {
	f := newConnectionFixture(t)
	out := f.request("GET", "/api/auth/profiles", "", "agent.example.test")
	if out.Code != 200 || !strings.Contains(out.Body.String(), f.id) {
		t.Fatalf("remote read: %d %s", out.Code, out.Body.String())
	}
	for _, path := range []string{"/api/auth/profiles/active", "/api/auth/disconnect"} {
		for _, password := range []string{"", "wrong-password"} {
			out = f.request("POST", path, `{"profile_id":"`+f.id+`","password":"`+password+`"}`, "agent.example.test")
			if out.Code != 401 || !strings.Contains(out.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("password rejection %s: %d %s", path, out.Code, out.Body.String())
			}
			profile, err := f.store.LoadProfile(f.id)
			if err != nil || !profile.Credentials.OAuthPreferred {
				t.Fatalf("failed confirmation mutated connection: %v", err)
			}
		}
		out = f.request("POST", path, `{"profile_id":"`+f.id+`","password":"correct-horse-battery"}`, "agent.example.test")
		if out.Code != 200 {
			t.Fatalf("remote confirmed %s: %d %s", path, out.Code, out.Body.String())
		}
	}
	profile, err := f.store.LoadProfile(f.id)
	if err != nil || profile.Credentials.OAuthPreferred {
		t.Fatalf("disconnect was not persisted: %v", err)
	}
}

func TestConnectionPasswordDoesNotHoldDatabaseDuringProviderAction(t *testing.T) {
	f := newConnectionFixture(t)
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	handler := f.accounts.scopedPasswordSIWCHandler(nil, false, func(_ *siwcService, w http.ResponseWriter, r *http.Request) {
		close(reached)
		<-release // Simulate waiting for provider revocation, not a database operation.
		w.WriteHeader(http.StatusNoContent)
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- passwordRequest(handler, "POST", "/api/auth/disconnect", `{"profile_id":"`+f.id+`","password":"correct-horse-battery"}`, f.cookie)
	}()
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("confirmed action did not start")
	}
	read := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		read <- passwordRequest(f.accounts.middleware(http.HandlerFunc(f.accounts.meHandler)), "GET", "/api/auth/me", "", f.cookie)
	}()
	select {
	case out := <-read:
		if out.Code != http.StatusOK {
			t.Fatalf("independent account read: %d %s", out.Code, out.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Error("provider action holds the account database and stalls independent requests")
	}
	unblock()
	select {
	case out := <-done:
		if out.Code != http.StatusNoContent {
			t.Fatalf("confirmed action: %d %s", out.Code, out.Body.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("confirmed action did not finish")
	}
}
