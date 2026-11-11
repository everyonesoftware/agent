package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func passwordRequest(h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func passwordBody(current, next string) string {
	data, _ := json.Marshal(map[string]string{"current_password": current, "new_password": next})
	return string(data)
}

func passwordAccount(t *testing.T, h http.Handler, username string) *http.Cookie {
	t.Helper()
	data, _ := json.Marshal(map[string]string{"username": username, "password": "original-test-password"})
	w := passwordRequest(h, http.MethodPost, "/api/auth/register", string(data), nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("register status = %d", w.Code)
	}
	return passwordCookie(t, w)
}

func passwordCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == authCookieName && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("missing session cookie")
	return nil
}

func TestPasswordChangeRejectsInvalidRequests(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	h := newServer()
	cookie := passwordAccount(t, h, "alice")
	tests := []struct {
		name, body, origin, fetchSite string
		status                        int
	}{
		{name: "cross-site origin", body: passwordBody("original-test-password", "replacement-test-password"), origin: "https://other.example", status: 403},
		{name: "cross-site fetch metadata", body: passwordBody("original-test-password", "replacement-test-password"), fetchSite: "cross-site", status: 403},
		{name: "wrong current", body: passwordBody("wrong-test-password", "replacement-test-password"), status: 401},
		{name: "missing current", body: passwordBody("", "replacement-test-password"), status: 400},
		{name: "short", body: passwordBody("original-test-password", "elevenchars"), status: 400},
		{name: "too large", body: passwordBody("original-test-password", strings.Repeat("x", 73)), status: 400},
		{name: "multibyte too short", body: passwordBody("original-test-password", strings.Repeat("界", 11)), status: 400},
		{name: "multibyte too large", body: passwordBody("original-test-password", strings.Repeat("界", 25)), status: 400},
		{name: "malformed", body: `{"current_password":`, status: 400},
		{name: "unknown field", body: `{"current_password":"original-test-password","new_password":"replacement-test-password","extra":true}`, status: 400},
		{name: "trailing JSON", body: passwordBody("original-test-password", "replacement-test-password") + ` {}`, status: 400},
		{name: "null", body: `null`, status: 400},
		{name: "oversized body", body: strings.Repeat(" ", 4097) + `{}`, status: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://localhost/api/auth/password", strings.NewReader(tt.body))
			r.AddCookie(cookie)
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.fetchSite != "" {
				r.Header.Set("Sec-Fetch-Site", tt.fetchSite)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("failed change set a cookie")
			}
			me := passwordRequest(h, http.MethodGet, "/api/auth/me", "", cookie)
			if me.Code != 200 {
				t.Fatalf("failed change invalidated session: %d", me.Code)
			}
		})
	}
}

func passwordLogin(h http.Handler, username, password string) *httptest.ResponseRecorder {
	data, _ := json.Marshal(map[string]string{"username": username, "password": password})
	return passwordRequest(h, http.MethodPost, "/api/auth/login", string(data), nil)
}

func TestPasswordChangeRotatesAllSessionsAndPersists(t *testing.T) {
	for _, tt := range []struct{ name, password string }{
		{"twelve Unicode codepoints", strings.Repeat("界", 12)},
		{"bcrypt byte limit", strings.Repeat("x", 72)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_DATA_DIR", t.TempDir())
			h := newServer()
			original := passwordAccount(t, h, "alice")
			sibling := passwordCookie(t, passwordLogin(h, "alice", "original-test-password"))
			other := passwordAccount(t, h, "bob")
			changed := passwordRequest(h, http.MethodPost, "/api/auth/password", passwordBody("original-test-password", tt.password), original)
			if changed.Code != 200 {
				t.Fatalf("change status = %d, want 200", changed.Code)
			}
			fresh := passwordCookie(t, changed)
			if fresh.Value == original.Value || fresh.Value == sibling.Value {
				t.Fatal("session not rotated")
			}
			if !fresh.HttpOnly || fresh.Path != "/" || fresh.SameSite != http.SameSiteLaxMode || fresh.MaxAge <= 0 {
				t.Fatal("unsafe session cookie attributes")
			}
			// A second production server opens the same on-disk database, rather than
			// relying on any in-memory state retained by the first handler.
			reopened := newServer()
			for _, server := range []http.Handler{h, reopened} {
				for _, old := range []*http.Cookie{original, sibling} {
					if w := passwordRequest(server, http.MethodGet, "/api/auth/me", "", old); w.Code != 401 {
						t.Fatalf("old session status = %d, want 401", w.Code)
					}
				}
				for _, valid := range []*http.Cookie{fresh, other} {
					if w := passwordRequest(server, http.MethodGet, "/api/auth/me", "", valid); w.Code != 200 {
						t.Fatalf("valid session status = %d, want 200", w.Code)
					}
				}
				if w := passwordLogin(server, "alice", "original-test-password"); w.Code != 401 {
					t.Fatalf("old password login status = %d, want 401", w.Code)
				}
				if w := passwordLogin(server, "alice", tt.password); w.Code != 200 {
					t.Fatalf("new password login status = %d, want 200", w.Code)
				}
				if w := passwordLogin(server, "bob", "original-test-password"); w.Code != 200 {
					t.Fatalf("other user login status = %d, want 200", w.Code)
				}
			}
		})
	}
}

// Pause the real SQLite statement, not bcrypt timing, to deterministically
// exercise a password replacement after verification but before session write.
var passwordDriverSequence atomic.Uint64

type passwordGateDriver struct {
	reached chan struct{}
	release chan struct{}
	once    sync.Once
	prefix  string
}

type passwordGateConn struct {
	driver.Conn
	gate *passwordGateDriver
}

func (d *passwordGateDriver) Open(name string) (driver.Conn, error) {
	conn, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &passwordGateConn{Conn: conn, gate: d}, nil
}

func (c *passwordGateConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, c.gate.prefix) {
		c.gate.once.Do(func() {
			close(c.gate.reached)
			select {
			case <-c.gate.release:
			case <-ctx.Done():
			}
		})
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func passwordGatedStore(t *testing.T, dir, prefix string) (*accountStore, *passwordGateDriver) {
	t.Helper()
	gate := &passwordGateDriver{reached: make(chan struct{}), release: make(chan struct{}), prefix: prefix}
	name := fmt.Sprintf("password-gate-%d", passwordDriverSequence.Add(1))
	sql.Register(name, gate)
	db, err := sql.Open(name, filepath.Join(dir, "everyoneagent.sqlite")+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return &accountStore{db: db}, gate
}

func TestLoginRejectsHashVerifiedBeforePasswordChange(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APP_DATA_DIR", dir)
	h := newServer()
	cookie := passwordAccount(t, h, "alice")
	store, gate := passwordGatedStore(t, dir, "INSERT INTO sessions")
	done := make(chan error, 1)
	go func() { _, _, err := store.login("alice", "original-test-password"); done <- err }()
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate.release) }) }
	defer unblock()
	select {
	case <-gate.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("login never reached session write")
	}
	changed := passwordRequest(h, http.MethodPost, "/api/auth/password", passwordBody("original-test-password", "replacement-test-password"), cookie)
	if changed.Code != 200 {
		t.Fatalf("concurrent change status = %d, want 200", changed.Code)
	}
	unblock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("login inserted a session using a stale verified password")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("login did not finish")
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("session count = %d, want only the replacement session", count)
	}
	if w := passwordRequest(h, http.MethodGet, "/api/auth/me", "", passwordCookie(t, changed)); w.Code != 200 {
		t.Fatalf("fresh session status = %d", w.Code)
	}
}

func TestPasswordChangeRollsBackIfSessionRotationFails(t *testing.T) {
	for _, statement := range []string{"DELETE", "INSERT"} {
		t.Run(statement, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("APP_DATA_DIR", dir)
			h := newServer()
			cookie := passwordAccount(t, h, "alice")
			sibling := passwordCookie(t, passwordLogin(h, "alice", "original-test-password"))
			store, err := newAccountStore(filepath.Join(dir, "everyoneagent.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.db.Close()
			_, err = store.db.Exec(`CREATE TRIGGER fail_rotation BEFORE ` + statement + ` ON sessions BEGIN SELECT RAISE(ABORT, 'test rotation failure'); END`)
			if err != nil {
				t.Fatal(err)
			}
			w := passwordRequest(h, http.MethodPost, "/api/auth/password", passwordBody("original-test-password", "replacement-test-password"), cookie)
			if w.Code != 500 {
				t.Fatalf("failed rotation status = %d, want 500", w.Code)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("failed transaction issued session cookie")
			}
			if _, err = store.db.Exec(`DROP TRIGGER fail_rotation`); err != nil {
				t.Fatal(err)
			}
			for _, old := range []*http.Cookie{cookie, sibling} {
				if w := passwordRequest(h, http.MethodGet, "/api/auth/me", "", old); w.Code != 200 {
					t.Fatalf("rollback lost existing session: %d", w.Code)
				}
			}
			if w := passwordLogin(h, "alice", "original-test-password"); w.Code != 200 {
				t.Fatalf("rollback lost old password: %d", w.Code)
			}
			if w := passwordLogin(h, "alice", "replacement-test-password"); w.Code != 401 {
				t.Fatalf("rollback persisted new password: %d", w.Code)
			}
		})
	}
}

func TestPasswordChangeRejectsConcurrentStaleVerification(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APP_DATA_DIR", dir)
	h := newServer()
	cookie := passwordAccount(t, h, "alice")
	store, gate := passwordGatedStore(t, dir, "UPDATE users SET password_hash")
	protected := store.middleware(http.HandlerFunc(store.passwordChangeHandler))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- passwordRequest(protected, http.MethodPost, "/api/auth/password", passwordBody("original-test-password", "losing-test-password"), cookie)
	}()
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate.release) }) }
	defer unblock()
	select {
	case <-gate.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("password change never reached update")
	}
	winner := passwordRequest(h, http.MethodPost, "/api/auth/password", passwordBody("original-test-password", "winning-test-password"), cookie)
	if winner.Code != 200 {
		t.Fatalf("winning change status = %d", winner.Code)
	}
	unblock()
	select {
	case loser := <-done:
		if loser.Code != 401 {
			t.Fatalf("stale change status = %d, want 401", loser.Code)
		}
		if len(loser.Result().Cookies()) != 0 {
			t.Fatal("stale change issued cookie")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stale change did not finish")
	}
	if w := passwordLogin(h, "alice", "losing-test-password"); w.Code != 401 {
		t.Fatalf("stale change overwrote password: %d", w.Code)
	}
	if w := passwordLogin(h, "alice", "winning-test-password"); w.Code != 200 {
		t.Fatalf("winning password rejected: %d", w.Code)
	}
	if w := passwordRequest(h, http.MethodGet, "/api/auth/me", "", passwordCookie(t, winner)); w.Code != 200 {
		t.Fatalf("winning session invalidated: %d", w.Code)
	}
}

func TestPasswordChangeRequiresSession(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	h := newServer()
	w := passwordRequest(h, http.MethodPost, "/api/auth/password", passwordBody("original-test-password", "replacement-test-password"), nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", w.Code)
	}
}
