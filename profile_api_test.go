package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProfileManagementAllowsRemoteReadAndConfirmedSelection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APP_DATA_DIR", dir)
	server := newServer()
	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"profile-test","password":"correct-horse-battery"}`))
	registered := httptest.NewRecorder()
	server.ServeHTTP(registered, register)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", registered.Code, registered.Body.String())
	}
	var user authUser
	if err := json.NewDecoder(registered.Body).Decode(&user); err != nil {
		t.Fatal(err)
	}
	accounts, err := newAccountStore(filepath.Join(dir, "everyoneagent.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	base := newSIWCService(siwcConfig{}, newFileCredentialStore(filepath.Join(dir, "base")))
	store := accounts.siwcForUser(base, user).store
	firstID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate first profile ID: %v", err)
	}
	secondID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate second profile ID: %v", err)
	}
	first := testOAuthCredentials("first-access-secret", "first-refresh-secret", time.Now().Add(time.Hour))
	first.ClientID = "first-client-secret"
	first.Subject = "first-subject-private"
	second := testOAuthCredentials("second-access-secret", "second-refresh-secret", time.Now().Add(time.Hour))
	second.ClientID = "second-client-secret"
	second.Subject = "second-subject-private"
	if err := store.SaveProfile(credentialProfile{ID: firstID, Label: "first@example.test", Credentials: first}); err != nil {
		t.Fatalf("save first profile: %v", err)
	}
	if err := store.SaveProfile(credentialProfile{ID: secondID, Label: "second@example.test", Credentials: second}); err != nil {
		t.Fatalf("save second profile: %v", err)
	}
	if err := store.SetActiveProfile(firstID); err != nil {
		t.Fatalf("select first profile: %v", err)
	}
	cookie := registered.Result().Cookies()[0]
	remoteRequest := httptest.NewRequest(http.MethodGet, "/api/auth/profiles", nil)
	remoteRequest.Host = "everyoneagent.example.ts.net"
	remoteRequest.AddCookie(cookie)
	remoteRecorder := httptest.NewRecorder()
	server.ServeHTTP(remoteRecorder, remoteRequest)
	if remoteRecorder.Code != http.StatusOK {
		t.Fatalf("remote profile list status = %d, want %d", remoteRecorder.Code, http.StatusOK)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/auth/profiles", nil)
	listRequest.Host = "127.0.0.1:8080"
	listRequest.AddCookie(cookie)
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("profile list status = %d, want %d; body=%s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	bodyText := listRecorder.Body.String()
	var result struct {
		ActiveProfileID string `json:"active_profile_id"`
		Profiles        []struct {
			ID               string `json:"id"`
			Label            string `json:"label"`
			Connected        bool   `json:"connected"`
			OAuthSelected    bool   `json:"oauth_selected"`
			PlanUsageEnabled bool   `json:"plan_usage_enabled"`
		} `json:"profiles"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&result); err != nil {
		t.Fatalf("decode profile list: %v", err)
	}
	if result.ActiveProfileID != firstID || len(result.Profiles) != 2 {
		t.Fatalf("profile list active=%q count=%d; want active first and two profiles", result.ActiveProfileID, len(result.Profiles))
	}
	if !strings.Contains(bodyText, "first@example.test") || strings.Contains(bodyText, "first-access-secret") || strings.Contains(bodyText, "first-refresh-secret") || strings.Contains(bodyText, "first-client-secret") || strings.Contains(bodyText, "first-subject-private") {
		t.Fatalf("profile API did not return safe account labels: %s", bodyText)
	}

	selectRequest := httptest.NewRequest(http.MethodPost, "/api/auth/profiles/active", bytes.NewBufferString(`{"profile_id":"`+secondID+`","password":"correct-horse-battery"}`))
	selectRequest.Host = "127.0.0.1:8080"
	selectRequest.AddCookie(cookie)
	selectRecorder := httptest.NewRecorder()
	server.ServeHTTP(selectRecorder, selectRequest)
	if selectRecorder.Code != http.StatusOK {
		t.Fatalf("profile selection status = %d, want %d; body=%s", selectRecorder.Code, http.StatusOK, selectRecorder.Body.String())
	}
	activeID, err := store.ActiveProfileID()
	if err != nil || activeID != secondID {
		t.Fatalf("active profile after selection = %q, %v; want %q", activeID, err, secondID)
	}

	remoteSelect := httptest.NewRequest(http.MethodPost, "/api/auth/profiles/active", bytes.NewBufferString(`{"profile_id":"`+firstID+`","password":"correct-horse-battery"}`))
	remoteSelect.Host = "everyoneagent.example.ts.net"
	remoteSelect.AddCookie(cookie)
	remoteSelectRecorder := httptest.NewRecorder()
	server.ServeHTTP(remoteSelectRecorder, remoteSelect)
	if remoteSelectRecorder.Code != http.StatusOK {
		t.Fatalf("remote profile selection status = %d, want %d", remoteSelectRecorder.Code, http.StatusOK)
	}
	activeID, err = store.ActiveProfileID()
	if err != nil || activeID != firstID {
		t.Fatalf("remote selection did not change active profile to %q, %v", activeID, err)
	}
	pendingID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate pending profile ID: %v", err)
	}
	pending := oauthCredentials{ClientID: "pending-client", HostID: testHostID, Issuer: "https://auth.openai.com", RegistrationPending: true, OAuthPreferred: true}
	if err := store.SaveProfile(credentialProfile{ID: pendingID, Label: "ChatGPT account 3", Credentials: pending}); err != nil {
		t.Fatalf("save pending profile: %v", err)
	}
	pendingSelect := httptest.NewRequest(http.MethodPost, "/api/auth/profiles/active", bytes.NewBufferString(`{"profile_id":"`+pendingID+`","password":"correct-horse-battery"}`))
	pendingSelect.Host = "127.0.0.1:8080"
	pendingSelect.AddCookie(cookie)
	pendingRecorder := httptest.NewRecorder()
	server.ServeHTTP(pendingRecorder, pendingSelect)
	if pendingRecorder.Code != http.StatusConflict {
		t.Fatalf("pending profile selection status = %d, want %d", pendingRecorder.Code, http.StatusConflict)
	}
	activeID, err = store.ActiveProfileID()
	if err != nil || activeID != firstID {
		t.Fatalf("pending profile selection changed active profile to %q, %v", activeID, err)
	}
}
