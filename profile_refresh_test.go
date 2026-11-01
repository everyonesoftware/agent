package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenRefreshPersistsToOriginalProfileIfActiveSelectionChanges(t *testing.T) {
	store := newFileCredentialStore(t.TempDir())
	firstID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate first profile ID: %v", err)
	}
	secondID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate second profile ID: %v", err)
	}
	first := testOAuthCredentials("first-expired-access", "first-refresh", time.Now().Add(-time.Minute))
	first.ClientID, first.Subject = "first-client", "first-subject"
	second := testOAuthCredentials("second-access", "second-refresh", time.Now().Add(time.Hour))
	second.ClientID, second.Subject = "second-client", "second-subject"
	if err := store.SaveProfile(credentialProfile{ID: firstID, Credentials: first}); err != nil {
		t.Fatalf("save first profile: %v", err)
	}
	if err := store.SaveProfile(credentialProfile{ID: secondID, Credentials: second}); err != nil {
		t.Fatalf("save second profile: %v", err)
	}
	if err := store.SetActiveProfile(firstID); err != nil {
		t.Fatalf("select first profile: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := store.SetActiveProfile(secondID); err != nil {
			t.Errorf("switch active profile during refresh: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"first-rotated-access","refresh_token":"first-rotated-refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()
	service := newSIWCService(siwcConfig{tokenURL: server.URL}, store)
	service.client = server.Client()

	if _, err := service.accessToken(t.Context()); err != nil {
		t.Fatalf("refresh first profile token: %v", err)
	}
	storedFirst, err := store.LoadProfile(firstID)
	if err != nil || storedFirst.Credentials.AccessToken != "first-rotated-access" || storedFirst.Credentials.RefreshToken != "first-rotated-refresh" {
		t.Fatalf("refreshed credentials were not saved to original profile: %#v, %v", storedFirst, err)
	}
	storedSecond, err := store.LoadProfile(secondID)
	if err != nil || storedSecond.Credentials.AccessToken != "second-access" || storedSecond.Credentials.RefreshToken != "second-refresh" || storedSecond.Credentials.Subject != "second-subject" {
		t.Fatalf("refresh changed the other profile: %#v, %v", storedSecond, err)
	}
}
