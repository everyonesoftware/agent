package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func disconnectPreconditionFixture(t *testing.T) (*siwcService, *fileCredentialStore, []credentialProfile, *atomic.Int32) {
	t.Helper()
	store := newFileCredentialStore(t.TempDir())
	profiles := make([]credentialProfile, 2)
	for i := range profiles {
		id, err := newProfileID()
		if err != nil {
			t.Fatal(err)
		}
		credentials := testOAuthCredentials("access-"+id, "refresh-"+id, time.Now().Add(time.Hour))
		credentials.Subject = "subject-" + id
		profiles[i] = credentialProfile{ID: id, Credentials: credentials}
		if err := store.SaveProfile(profiles[i]); err != nil {
			t.Fatal(err)
		}
		profiles[i], err = store.LoadProfile(id)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetActiveProfile(profiles[0].ID); err != nil {
		t.Fatal(err)
	}
	calls := new(atomic.Int32)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(provider.Close)
	service := newSIWCService(siwcConfig{}, store)
	service.client = provider.Client()
	service.metadata = &openIDMetadata{RevocationEndpoint: provider.URL + "/revoke"}
	return service, store, profiles, calls
}

func disconnectPreconditionRequest(service *siwcService, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/auth/disconnect", strings.NewReader(body))
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	service.disconnectHandler(recorder, request)
	return recorder
}

func assertDisconnectProfilesUnchanged(t *testing.T, store *fileCredentialStore, profiles []credentialProfile) {
	t.Helper()
	for _, expected := range profiles {
		actual, err := store.LoadProfile(expected.ID)
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("profile %q changed or unreadable: %v", expected.ID, err)
		}
	}
}

func TestDisconnectPreconditionRejectsInvalidBody(t *testing.T) {
	for _, body := range []string{
		`{`, `null`, `[]`, `"profile"`, `{}`, `{"profile_id":null}`,
		`{"profile_id":123}`, `{"profile_id":""}`, `{"profile_id":"profile_short"}`,
		`{"profile_id":"profile_abcdefghijkl!"}`,
		`{"profile_id":"profile_abcdefghijklmnop","unknown":true}`,
		`{"profile_id":"profile_abcdefghijklmnop"} {}`,
		`{"profile_id":"profile_abcdefghijklmnop"} trailing`,
		`{"profile_id":"` + strings.Repeat("a", 65) + `"}`,
		`{"profile_id":"profile_abcdefghijklmnop"}` + strings.Repeat(" ", 4096) + `{}`,
	} {
		t.Run(body[:min(len(body), 80)], func(t *testing.T) {
			service, store, profiles, calls := disconnectPreconditionFixture(t)
			response := disconnectPreconditionRequest(service, body)
			if response.Code != http.StatusBadRequest {
				t.Errorf("invalid body status = %d, want 400", response.Code)
			}
			if calls.Load() != 0 {
				t.Errorf("invalid body made %d provider calls, want 0", calls.Load())
			}
			assertDisconnectProfilesUnchanged(t, store, profiles)
		})
	}
}

func TestDisconnectPreconditionMatchingAndLegacyBodies(t *testing.T) {
	for _, bodyKind := range []string{"matching", "empty", "whitespace"} {
		t.Run(bodyKind, func(t *testing.T) {
			service, store, profiles, calls := disconnectPreconditionFixture(t)
			body := ""
			if bodyKind == "matching" {
				body = `{"profile_id":"` + profiles[0].ID + `"}`
			} else if bodyKind == "whitespace" {
				body = " \n\t"
			}
			response := disconnectPreconditionRequest(service, body)
			if response.Code != http.StatusOK || calls.Load() != 1 {
				t.Fatalf("disconnect status=%d calls=%d; body=%s", response.Code, calls.Load(), response.Body.String())
			}
			actual, err := store.LoadProfile(profiles[0].ID)
			if err != nil || actual.Credentials.AccessToken != "" || actual.Credentials.RefreshToken != "" || actual.Credentials.IDToken != "" {
				t.Fatalf("matching profile tokens not cleared: %v", err)
			}
			assertDisconnectProfilesUnchanged(t, store, profiles[1:])
		})
	}
}

func TestDisconnectPreconditionSelectionWaitsForTokenLock(t *testing.T) {
	service, store, profiles, calls := disconnectPreconditionFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/profiles/active", strings.NewReader(`{"profile_id":"`+profiles[1].ID+`"}`))
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	started := make(chan struct{})
	done := make(chan struct{})
	service.tokenMu.Lock()
	go func() {
		close(started)
		service.selectProfileHandler(response, request)
		close(done)
	}()
	<-started
	select {
	case <-done:
		t.Error("profile selection completed while disconnect's token lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	active, err := store.ActiveProfileID()
	if err != nil || active != profiles[0].ID {
		t.Errorf("selection changed active profile while token lock held: %q, %v", active, err)
	}
	service.tokenMu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("profile selection did not finish after token lock released")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("selection status=%d; body=%s", response.Code, response.Body.String())
	}
	stale := disconnectPreconditionRequest(service, `{"profile_id":"`+profiles[0].ID+`"}`)
	if stale.Code != http.StatusConflict || calls.Load() != 0 {
		t.Fatalf("stale confirmation after selection status=%d provider calls=%d", stale.Code, calls.Load())
	}
	assertDisconnectProfilesUnchanged(t, store, profiles)
}

func TestDisconnectPreconditionRejectsStaleConfirmation(t *testing.T) {
	service, store, profiles, calls := disconnectPreconditionFixture(t)
	if err := store.SetActiveProfile(profiles[1].ID); err != nil {
		t.Fatal(err)
	}
	response := disconnectPreconditionRequest(service, `{"profile_id":"`+profiles[0].ID+`"}`)
	if response.Code != http.StatusConflict {
		t.Errorf("stale confirmation status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
	if calls.Load() != 0 {
		t.Errorf("stale confirmation made %d provider calls, want 0", calls.Load())
	}
	assertDisconnectProfilesUnchanged(t, store, profiles)
	if active, err := store.ActiveProfileID(); err != nil || active != profiles[1].ID {
		t.Fatalf("active profile changed: %q, %v", active, err)
	}
}
