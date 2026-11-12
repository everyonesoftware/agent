package main

import (
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestResearchSettingsSessionExpiryAfterWriterWait(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "still_valid"
		if expired {
			name = "expired_while_waiting"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				dir := t.TempDir()
				a, err := newAccountStore(filepath.Join(dir, "everyoneagent.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				defer a.db.Close()
				user, token, err := a.register("writer-wait-user", "synthetic-password")
				if err != nil {
					t.Fatal(err)
				}
				service := newAccountResearchService(a).settings
				before := researchCredential{apiKey: settingsTestKey, researchSettings: researchSettings{Revision: "before", Status: "connected"}}
				if err := service.persist(user.ID, token, "", before); err != nil {
					t.Fatal(err)
				}
				expiry := time.Now().Add(time.Minute).Unix()
				if _, err := a.db.Exec(`UPDATE sessions SET expires_at=? WHERE token_hash=?`, expiry, hashSession(token)); err != nil {
					t.Fatal(err)
				}

				// Reuse the real SQLite driver gate at the first writer statement.
				// synctest advances the clock while it is blocked, without wall-clock
				// sleeps or rewriting the session expiry after arguments are captured.
				gated, gate := passwordGatedStore(t, dir, "UPDATE sessions SET expires_at")
				defer gated.db.Close()
				service.accounts = gated
				next := before
				next.apiKey = "synthetic-replacement-9999"
				next.Revision = "after"
				done := make(chan error, 1)
				go func() { done <- service.persist(user.ID, token, before.Revision, next) }()
				var once sync.Once
				unblock := func() { once.Do(func() { close(gate.release) }) }
				defer unblock()
				select {
				case <-gate.reached:
				case <-time.After(10 * time.Second):
					t.Fatal("persistence did not reach its first writer statement")
				}
				if expired {
					time.Sleep(time.Until(time.Unix(expiry, 0)))
				}
				unblock()
				err = <-done
				if expired {
					if !errors.Is(err, errResearchSettingsSignedOut) {
						t.Errorf("session expired during writer wait: got %v, want signed out", err)
					}
				} else if err != nil {
					t.Fatalf("still-valid session rejected: %v", err)
				}
				stored, err := a.researchCredentialForUser(user.ID)
				if err != nil {
					t.Fatal(err)
				}
				want := next
				if expired {
					want = before
				}
				if stored.apiKey != want.apiKey || stored.Revision != want.Revision {
					t.Errorf("persisted key/revision changed incorrectly: revision=%q want %q", stored.Revision, want.Revision)
				}
			})
		})
	}
}

func TestResearchSettingsCorruptShortCredentialFailsClosed(t *testing.T) {
	a, s, h, cookie, _ := settingsHarness(t, func(http.ResponseWriter, *http.Request) {
		t.Error("corrupt stored credential reached provider")
	})
	user := mustSettingsUser(t, a, cookie)
	for _, key := range []string{"x", "xy", "xyz"} {
		t.Run(key, func(t *testing.T) {
			if _, err := a.db.Exec(`INSERT INTO research_settings(user_id,api_key,revision,verified_at,last_checked_at,status) VALUES(?,?,?,'','','connected') ON CONFLICT(user_id) DO UPDATE SET api_key=excluded.api_key`, user.ID, key, "corrupt-revision"); err != nil {
				t.Fatal(err)
			}
			// Catch the panic as a failure so every corrupt length is exercised.
			defer func() {
				if v := recover(); v != nil {
					t.Errorf("short stored credential panicked instead of failing closed: %v", v)
				}
			}()
			stored, err := a.researchCredentialForUser(user.ID)
			if err == nil || stored.apiKey != "" || stored.Configured {
				t.Error("corrupt stored credential did not return an error with empty credentials")
			}
			settingsResult(t, researchRequest(h, cookie, "GET", "/api/research/settings", ""), http.StatusServiceUnavailable)
			s.mu.Lock()
			web, _, err := s.webForUserLocked(user.ID)
			s.mu.Unlock()
			if err == nil || web != nil {
				t.Error("research accepted corrupt stored credential")
			}
		})
	}
}
