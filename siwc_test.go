package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testHostID = "urn:uuid:123e4567-e89b-42d3-a456-426614174000"

func testOAuthCredentials(accessToken, refreshToken string, expiresAt time.Time) oauthCredentials {
	return oauthCredentials{
		ClientID: "issued-client", HostID: testHostID, AccessToken: accessToken, RefreshToken: refreshToken,
		IDToken: "id-token-test", TokenType: "Bearer", Issuer: siwcIssuer, Subject: "account-subject",
		Scopes: strings.Fields(siwcScope), ExpiresAt: expiresAt, OAuthPreferred: true,
	}
}

func TestAuthStartRejectsNonLoopbackHost(t *testing.T) {
	t.Setenv("SIWC_DATA_DIR", t.TempDir())
	request := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	request.Host = "everyoneagent.example.ts.net"
	recorder := httptest.NewRecorder()

	newServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}

func TestSIWCRedirectURIRequiresLiteralLoopbackAddressAndValidPort(t *testing.T) {
	if !validSIWCRedirectURI("http://127.0.0.1:8080/auth/callback") {
		t.Fatal("valid loopback callback was rejected")
	}
	for _, redirectURI := range []string{
		"http://localhost:8080/auth/callback",
		"http://[::1]:8080/auth/callback",
		"http://0.0.0.0:8080/auth/callback",
		"https://127.0.0.1:8080/auth/callback",
		"http://127.0.0.1/auth/callback",
		"http://127.0.0.1:70000/auth/callback",
		"http://127.0.0.1:8080/auth/callback?next=/",
		"http://127.0.0.1:8080/auth/callback#fragment",
	} {
		if validSIWCRedirectURI(redirectURI) {
			t.Errorf("unsafe callback URI accepted: %q", redirectURI)
		}
	}
}

func TestAuthStartRejectsCrossSiteNavigationToLoopback(t *testing.T) {
	t.Setenv("SIWC_DATA_DIR", t.TempDir())
	request := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder := httptest.NewRecorder()

	newServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}

func TestAuthStatusDoesNotExposeCredentials(t *testing.T) {
	dir := t.TempDir()
	credentials := testOAuthCredentials("access-secret", "refresh-secret", time.Now().Add(time.Hour))
	credentials.ClientID = "client-secret"
	if err := newFileCredentialStore(dir).Save(credentials); err != nil {
		t.Fatalf("save test credentials: %v", err)
	}
	t.Setenv("SIWC_DATA_DIR", dir)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	recorder := httptest.NewRecorder()

	newServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var status struct {
		Connected        bool   `json:"connected"`
		OAuthSelected    bool   `json:"oauth_selected"`
		PlanUsageEnabled bool   `json:"plan_usage_enabled"`
		Email            string `json:"email"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&status); err != nil {
		t.Fatalf("decode auth status: %v", err)
	}
	if !status.Connected || !status.OAuthSelected || !status.PlanUsageEnabled || status.Email != "" {
		t.Fatalf("status = %#v, want connected without account details", status)
	}
	for _, forbidden := range []string{"access-secret", "refresh-secret", "owner@example.test", "client-secret"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Errorf("auth status exposed %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAuthStatusSeparatesSignInFromPlanUsagePermission(t *testing.T) {
	credentials := testOAuthCredentials("access-test", "refresh-test", time.Now().Add(time.Hour))
	credentials.Scopes = []string{"openid", "profile", "email", "offline_access"}
	store := newFileCredentialStore(t.TempDir())
	if err := store.Save(credentials); err != nil {
		t.Fatalf("save identity-only credentials: %v", err)
	}
	service := newSIWCService(siwcConfig{}, store)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	recorder := httptest.NewRecorder()
	service.statusHandler(recorder, request)
	var status map[string]bool
	if err := json.NewDecoder(recorder.Body).Decode(&status); err != nil {
		t.Fatalf("decode auth status: %v", err)
	}
	if !status["connected"] || !status["oauth_selected"] || status["plan_usage_enabled"] {
		t.Fatalf("status = %#v; expected sign-in without ChatGPT plan usage", status)
	}
	provider := &hybridProvider{oauth: &responsesProvider{auth: service}}
	if !provider.useOAuth() || !provider.configured() {
		t.Fatal("a signed-in account without plan-usage scope must remain selected instead of silently falling back")
	}
}

func TestAuthStartUsesSIWCDynamicRegistrationAndPKCE(t *testing.T) {
	store := newFileCredentialStore(t.TempDir())
	service := newSIWCService(siwcConfig{
		redirectURI:  "http://127.0.0.1:8080/auth/callback",
		authorizeURL: siwcAuthorizeURL,
	}, store)
	request := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()

	service.startHandler(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusFound, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := location.Query()
	for key, expected := range map[string]string{
		"client_id":             siwcClientID,
		"redirect_uri":          service.config.redirectURI,
		"response_type":         "code",
		"resource":              siwcResource,
		"agent_name_hint":       siwcAgentName,
		"code_challenge_method": "S256",
	} {
		if got := query.Get(key); got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
	for _, requiredScope := range []string{"openid", "profile", "email", "offline_access", "resource.invoke", "chatgpt.tokens.use.direct"} {
		if !strings.Contains(" "+query.Get("scope")+" ", " "+requiredScope+" ") {
			t.Errorf("scope %q is missing from %q", requiredScope, query.Get("scope"))
		}
	}
	if query.Get("state") == "" || query.Get("state") != service.pending.state || query.Get("nonce") != service.pending.nonce {
		t.Fatal("authorization request did not bind its state and nonce to the pending flow")
	}
	hostID, err := store.HostID()
	if err != nil {
		t.Fatalf("read host ID: %v", err)
	}
	if !validHostID(hostID) {
		t.Fatalf("host ID %q is not a UUID", hostID)
	}
	if query.Get("ext_agent_host_id") != hostID {
		t.Fatal("authorization request used an unstable host identifier")
	}
	challenge := sha256.Sum256([]byte(service.pending.codeVerifier))
	if got, want := query.Get("code_challenge"), base64.RawURLEncoding.EncodeToString(challenge[:]); got != want {
		t.Fatalf("PKCE challenge = %q, want %q", got, want)
	}
}

func TestAuthStartReusesIssuedRegistrationAndIdentityHint(t *testing.T) {
	store := newFileCredentialStore(t.TempDir())
	profileID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate profile ID: %v", err)
	}
	credentials := testOAuthCredentials("access-test", "refresh-test", time.Now().Add(time.Hour))
	if err := store.SaveProfile(credentialProfile{ID: profileID, Email: "owner@example.test", Credentials: credentials}); err != nil {
		t.Fatalf("save prior credentials: %v", err)
	}
	if err := store.SetActiveProfile(profileID); err != nil {
		t.Fatalf("select prior profile: %v", err)
	}
	service := newSIWCService(siwcConfig{
		redirectURI:  "http://127.0.0.1:8080/auth/callback",
		authorizeURL: siwcAuthorizeURL,
	}, store)
	request := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	service.startHandler(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusFound, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := location.Query()
	if query.Get("client_id") != "issued-client" || query.Get("id_token_hint") != "id-token-test" || query.Get("login_hint") != "owner@example.test" {
		t.Fatalf("reauthorization did not reuse the registered client and identity hint: client_id=%q id_token_hint=%q", query.Get("client_id"), query.Get("id_token_hint"))
	}
	if query.Has("agent_name_hint") || service.pending.newRegistration || service.pending.expectedSubject != "account-subject" {
		t.Fatal("reauthorization was treated as a new registration or did not bind the existing account")
	}
}

func TestHostIDIsRecoveredFromPersistedRegistration(t *testing.T) {
	store := newFileCredentialStore(t.TempDir())
	if err := store.Save(testOAuthCredentials("access-test", "refresh-test", time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
	if err := os.Remove(filepath.Join(store.dir, "host-id")); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remove host ID file: %v", err)
	}
	hostID, err := store.HostID()
	if err != nil || hostID != testHostID {
		t.Fatalf("HostID() = %q, %v; want persisted registration ID %q", hostID, err, testHostID)
	}
}

func TestSIWCDiscoversAuthorizationTokenAndRevocationEndpoints(t *testing.T) {
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openIDMetadata{
			Issuer: issuer, AuthorizationEndpoint: issuer + "/authorize",
			TokenEndpoint: issuer + "/token", RevocationEndpoint: issuer + "/revoke",
		})
	}))
	defer server.Close()
	issuer = server.URL
	service := newSIWCService(siwcConfig{
		redirectURI: "http://127.0.0.1:8080/auth/callback", issuerURL: issuer,
	}, newFileCredentialStore(t.TempDir()))
	service.client = server.Client()

	request := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	service.startHandler(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("discovered auth start status = %d, want %d; body = %s", recorder.Code, http.StatusFound, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil || location.String() == "" || location.Path != "/authorize" {
		t.Fatalf("authorization URL was not taken from discovery: %v", err)
	}
	tokenEndpoint, err := service.tokenEndpoint(t.Context())
	if err != nil || tokenEndpoint != issuer+"/token" {
		t.Fatalf("token endpoint = %q, err=%v; want discovered endpoint", tokenEndpoint, err)
	}
	metadata, err := service.discoveryMetadata(t.Context())
	if err != nil || metadata.RevocationEndpoint != issuer+"/revoke" {
		t.Fatalf("revocation endpoint = %#v, err=%v", metadata, err)
	}
}

func TestCredentialStoreWritesOwnerOnlyTokenFile(t *testing.T) {
	dir := t.TempDir()
	store := newFileCredentialStore(dir)
	if err := store.Save(testOAuthCredentials("access-test", "refresh-test", time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "siwc-profiles.json"))
	if err != nil {
		t.Fatalf("stat credential file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("credential file permissions = %04o, want 0600", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "siwc-profiles.json"))
	if err != nil {
		t.Fatalf("read credential metadata: %v", err)
	}
	var stored credentialProfileState
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("decode credential metadata: %v", err)
	}
	if len(stored.Profiles) != 1 {
		t.Fatalf("stored profiles = %d, want 1", len(stored.Profiles))
	}
	profile := stored.Profiles[0]
	if profile.Credentials.HostID != testHostID || !hasPlanUsageScope(profile.Credentials.Scopes) {
		t.Fatalf("stored OAuth metadata has incorrect host ID or scope representation")
	}
}

func TestCredentialStoreRefusesGroupOrWorldReadableTokens(t *testing.T) {
	dir := t.TempDir()
	store := newFileCredentialStore(dir)
	credentials := testOAuthCredentials("secret", "refresh", time.Now().Add(time.Hour))
	credentials.ClientID = "client"
	if err := store.Save(credentials); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
	path := filepath.Join(dir, "siwc-profiles.json")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("make token file too permissive: %v", err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("credential store loaded a group/world-readable token file")
	}
}

func TestDisconnectRevokesRefreshTokenAndRetainsRegistration(t *testing.T) {
	var gotForm url.Values
	var revokeCalls int
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(openIDMetadata{Issuer: issuer, AuthorizationEndpoint: issuer + "/authorize", TokenEndpoint: issuer + "/token", RevocationEndpoint: issuer + "/revoke"})
		case "/revoke":
			revokeCalls++
			body, _ := io.ReadAll(r.Body)
			gotForm, _ = url.ParseQuery(string(body))
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL
	store := newFileCredentialStore(t.TempDir())
	credentials := testOAuthCredentials("access-secret", "refresh-secret", time.Now().Add(time.Hour))
	credentials.Issuer = issuer
	if err := store.Save(credentials); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
	service := newSIWCService(siwcConfig{issuerURL: issuer, redirectURI: "http://127.0.0.1:8080/auth/callback"}, store)
	service.client = server.Client()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/disconnect", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	service.disconnectHandler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var result struct {
		Connected bool `json:"connected"`
		Revoked   bool `json:"remote_revocation_confirmed"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatalf("decode disconnect result: %v", err)
	}
	if result.Connected || !result.Revoked || revokeCalls != 1 || gotForm.Get("token") != "refresh-secret" ||
		gotForm.Get("token_type_hint") != "refresh_token" || gotForm.Get("client_id") != "issued-client" {
		t.Fatalf("disconnect result=%#v calls=%d form=%#v", result, revokeCalls, gotForm)
	}
	stored, err := store.Load()
	if err != nil {
		t.Fatalf("load signed-out registration: %v", err)
	}
	if stored.ClientID != "issued-client" || stored.Subject != "account-subject" || stored.AccessToken != "" || stored.RefreshToken != "" || stored.IDToken != "" || stored.OAuthPreferred {
		t.Fatalf("sign-out did not clear tokens while retaining registration identity: %#v", stored)
	}

	service.config.authorizeURL = siwcAuthorizeURL
	startRequest := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	startRequest.Host = "127.0.0.1:8080"
	startRecorder := httptest.NewRecorder()
	service.startHandler(startRecorder, startRequest)
	location, err := url.Parse(startRecorder.Header().Get("Location"))
	if startRecorder.Code != http.StatusFound || err != nil || location.Query().Get("client_id") != "issued-client" ||
		location.Query().Get("prompt") != "consent" || location.Query().Has("id_token_hint") || location.Query().Has("agent_name_hint") || service.pending.expectedSubject != "account-subject" {
		t.Fatalf("signed-out registration was not reused safely: status=%d location=%q pending=%#v", startRecorder.Code, startRecorder.Header().Get("Location"), service.pending)
	}
}

func TestDisconnectClearsLocalTokensWhenRevocationCannotBeConfirmed(t *testing.T) {
	var discoveryCalls int
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discoveryCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":"refresh-secret must never be logged","code":"temporarily_unavailable"}`)
	}))
	defer server.Close()
	issuer = server.URL
	store := newFileCredentialStore(t.TempDir())
	credentials := testOAuthCredentials("access-secret", "refresh-secret", time.Now().Add(time.Hour))
	credentials.Issuer = issuer
	if err := store.Save(credentials); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
	service := newSIWCService(siwcConfig{issuerURL: issuer, redirectURI: "http://127.0.0.1:8080/auth/callback"}, store)
	service.client = server.Client()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/disconnect", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	service.disconnectHandler(recorder, request)
	log.SetOutput(previousLogOutput)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var result struct {
		Connected bool `json:"connected"`
		Revoked   bool `json:"remote_revocation_confirmed"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatalf("decode disconnect result: %v", err)
	}
	stored, err := store.Load()
	if err != nil {
		t.Fatalf("load locally cleared registration: %v", err)
	}
	if result.Connected || result.Revoked || discoveryCalls != 3 || stored.AccessToken != "" || stored.RefreshToken != "" {
		t.Fatalf("disconnect failure handling result=%#v discovery_calls=%d stored=%#v", result, discoveryCalls, stored)
	}
	if strings.Contains(logs.String(), "refresh-secret") {
		t.Fatalf("safe diagnostics exposed a token: %s", logs.String())
	}
}

type testIDTokenVerifier struct{}

func (testIDTokenVerifier) Verify(_ context.Context, rawToken, clientID, nonce string) (verifiedIdentity, error) {
	if rawToken != "id-token-test" || clientID != "issued-client" || nonce == "" {
		return verifiedIdentity{}, errors.New("unexpected token verification inputs")
	}
	return verifiedIdentity{subject: "account-subject", email: "owner@example.test", name: "Owner"}, nil
}

type differentAccountIDTokenVerifier struct{}

func (differentAccountIDTokenVerifier) Verify(context.Context, string, string, string) (verifiedIdentity, error) {
	return verifiedIdentity{subject: "different-account"}, nil
}

func TestAuthCallbackExchangesCodeAndStoresCredentials(t *testing.T) {
	var gotForm url.Values
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("unexpected token request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read token request: %v", err)
		}
		gotForm, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-test","refresh_token":"refresh-test","id_token":"id-token-test","token_type":"Bearer","scope":"openid profile email offline_access resource.invoke chatgpt.tokens.use.direct","expires_in":3600}`))
	}))
	defer tokenEndpoint.Close()

	store := newFileCredentialStore(t.TempDir())
	service := newSIWCService(siwcConfig{
		redirectURI: "http://127.0.0.1:8080/auth/callback",
		tokenURL:    tokenEndpoint.URL,
	}, store)
	service.verifier = testIDTokenVerifier{}

	startRequest := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	startRequest.Host = "127.0.0.1:8080"
	service.startHandler(httptest.NewRecorder(), startRequest)
	state := service.pending.state
	verifier := service.pending.codeVerifier

	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(state)+"&code=auth-code&client_id=issued-client", nil)
	callbackRequest.Host = "127.0.0.1:8080"
	callbackRecorder := httptest.NewRecorder()
	service.callbackHandler(callbackRecorder, callbackRequest)

	if callbackRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", callbackRecorder.Code, http.StatusOK, callbackRecorder.Body.String())
	}
	if !strings.Contains(callbackRecorder.Body.String(), "ChatGPT connected") || !strings.Contains(callbackRecorder.Body.String(), "owner@example.test") {
		t.Fatalf("callback did not show a safe success page: %s", callbackRecorder.Body.String())
	}
	credentials, err := store.Load()
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	if credentials.AccessToken != "access-test" || credentials.RefreshToken != "refresh-test" || credentials.ClientID != "issued-client" {
		t.Fatalf("stored credentials do not match the token response: %#v", credentials)
	}
	storedBytes, err := os.ReadFile(filepath.Join(store.dir, "siwc-profiles.json"))
	if err != nil {
		t.Fatalf("read stored credentials for redaction check: %v", err)
	}
	if !strings.Contains(string(storedBytes), "id-token-test") || !strings.Contains(string(storedBytes), "owner@example.test") {
		t.Fatal("the profile store must retain the ID token hint and account label for local selection")
	}
	for key, expected := range map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     "issued-client",
		"code":          "auth-code",
		"code_verifier": verifier,
		"redirect_uri":  service.config.redirectURI,
		"resource":      siwcResource,
	} {
		if gotForm.Get(key) != expected {
			t.Errorf("token parameter %s = %q, want %q", key, gotForm.Get(key), expected)
		}
	}
}

func TestAuthCallbackReauthorizesExistingRegistrationWithoutCallbackClientID(t *testing.T) {
	var gotClientID string
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		gotClientID = values.Get("client_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","id_token":"id-token-test","token_type":"Bearer","scope":"openid profile email offline_access resource.invoke chatgpt.tokens.use.direct","expires_in":3600}`))
	}))
	defer tokenEndpoint.Close()
	store := newFileCredentialStore(t.TempDir())
	if err := store.Save(testOAuthCredentials("access-old", "refresh-old", time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("save existing credentials: %v", err)
	}
	profileID, err := store.ActiveProfileID()
	if err != nil || profileID == "" {
		t.Fatalf("load existing profile ID: %q, %v", profileID, err)
	}
	service := newSIWCService(siwcConfig{redirectURI: "http://127.0.0.1:8080/auth/callback", tokenURL: tokenEndpoint.URL}, store)
	service.verifier = testIDTokenVerifier{}
	service.pending = &authAttempt{
		state: "state", nonce: "nonce", codeVerifier: "verifier", clientID: "issued-client", hostID: testHostID, profileID: profileID,
		expectedSubject: "account-subject", expiresAt: time.Now().Add(time.Minute),
	}
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state&code=code", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	service.callbackHandler(recorder, request)
	if recorder.Code != http.StatusOK || gotClientID != "issued-client" {
		t.Fatalf("callback status=%d exchange client_id=%q; want reauthorization to use the stored client ID", recorder.Code, gotClientID)
	}
	credentials, err := store.Load()
	if err != nil {
		t.Fatalf("load reauthorized credentials: %v", err)
	}
	if credentials.AccessToken != "access-new" || credentials.Subject != "account-subject" {
		t.Fatalf("reauthorized credentials = %#v", credentials)
	}
}

func TestAuthCallbackPersistsIssuedClientIDBeforeTokenExchange(t *testing.T) {
	store := newFileCredentialStore(t.TempDir())
	var registrationProfileID string
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		activeID, activeErr := store.ActiveProfileID()
		profile, profileErr := store.LoadProfile(registrationProfileID)
		credentials := profile.Credentials
		if activeErr != nil || activeID != "" || profileErr != nil || !credentials.RegistrationPending || credentials.ClientID != "issued-client" {
			t.Errorf("issued registration was not safely persisted before token exchange: active=%q credentials=%#v err=%v", activeID, credentials, profileErr)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"authorization code expired"}`)
	}))
	defer tokenEndpoint.Close()
	service := newSIWCService(siwcConfig{
		redirectURI:  "http://127.0.0.1:8080/auth/callback",
		authorizeURL: siwcAuthorizeURL,
		tokenURL:     tokenEndpoint.URL,
	}, store)
	startRequest := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	startRequest.Host = "127.0.0.1:8080"
	startRecorder := httptest.NewRecorder()
	service.startHandler(startRecorder, startRequest)
	state := service.pending.state
	registrationProfileID = service.pending.profileID
	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(state)+"&code=expired-code&client_id=issued-client", nil)
	callbackRequest.Host = "127.0.0.1:8080"
	callbackRecorder := httptest.NewRecorder()
	service.callbackHandler(callbackRecorder, callbackRequest)
	if callbackRecorder.Code != http.StatusBadGateway {
		t.Fatalf("callback status=%d; want a safe token-exchange failure", callbackRecorder.Code)
	}
	activeID, err := store.ActiveProfileID()
	if err != nil || activeID != "" {
		t.Fatalf("unverified registration became active: active=%q err=%v", activeID, err)
	}
	profile, err := store.LoadProfile(registrationProfileID)
	credentials := profile.Credentials
	if err != nil || !credentials.RegistrationPending || credentials.ClientID != "issued-client" || !credentials.OAuthPreferred || !validHostID(credentials.HostID) {
		t.Fatalf("issued client registration was not retained for retry: credentials=%#v err=%v", credentials, err)
	}
	service.config.authorizeURL = siwcAuthorizeURL
	retryRequest := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	retryRequest.Host = "127.0.0.1:8080"
	retryRecorder := httptest.NewRecorder()
	service.startHandler(retryRecorder, retryRequest)
	location, err := url.Parse(retryRecorder.Header().Get("Location"))
	if retryRecorder.Code != http.StatusFound || err != nil || location.Query().Get("client_id") != "issued-client" || location.Query().Has("agent_name_hint") || location.Query().Get("prompt") != "consent" || service.pending.profileID != registrationProfileID || service.pending.newRegistration {
		t.Fatalf("retry did not reuse issued registration: status=%d location=%q err=%v pending=%#v", retryRecorder.Code, retryRecorder.Header().Get("Location"), err, service.pending)
	}
}

func TestAuthCallbackRetainsSignInWhenPlanUsageScopeWasNotGranted(t *testing.T) {
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-identity","refresh_token":"refresh-identity","id_token":"id-token-test","token_type":"Bearer","scope":"openid profile email offline_access","expires_in":3600}`))
	}))
	defer tokenEndpoint.Close()
	store := newFileCredentialStore(t.TempDir())
	service := newSIWCService(siwcConfig{redirectURI: "http://127.0.0.1:8080/auth/callback", tokenURL: tokenEndpoint.URL, authorizeURL: siwcAuthorizeURL}, store)
	service.verifier = testIDTokenVerifier{}
	profileID, err := newProfileID()
	if err != nil {
		t.Fatalf("generate profile ID: %v", err)
	}
	service.pending = &authAttempt{
		state: "state", nonce: "nonce", codeVerifier: "verifier", clientID: siwcClientID, hostID: testHostID, profileID: profileID,
		newRegistration: true, activateIfUnset: true, expiresAt: time.Now().Add(time.Minute),
	}
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state&code=code&client_id=issued-client", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	service.callbackHandler(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "plan usage is not enabled") {
		t.Fatalf("sign-in status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	credentials, err := store.Load()
	if err != nil || !credentialsConnected(credentials) || credentialsPlanUsageEnabled(credentials) {
		t.Fatalf("credentials=%#v err=%v; expected sign-in without plan usage", credentials, err)
	}
	startRequest := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	startRequest.Host = "127.0.0.1:8080"
	startRecorder := httptest.NewRecorder()
	service.startHandler(startRecorder, startRequest)
	location, err := url.Parse(startRecorder.Header().Get("Location"))
	if startRecorder.Code != http.StatusFound || err != nil || location.Query().Get("prompt") != "consent" || location.Query().Get("client_id") != "issued-client" {
		t.Fatalf("missing plan-usage scope did not offer an explicit consent retry: status=%d location=%q err=%v", startRecorder.Code, startRecorder.Header().Get("Location"), err)
	}
}

func TestAuthCallbackRejectsDifferentAccountWithoutReplacingCredentials(t *testing.T) {
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","id_token":"id-token-test","token_type":"Bearer","scope":"openid profile email offline_access resource.invoke chatgpt.tokens.use.direct","expires_in":3600}`))
	}))
	defer tokenEndpoint.Close()
	store := newFileCredentialStore(t.TempDir())
	if err := store.Save(testOAuthCredentials("access-old", "refresh-old", time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("save existing credentials: %v", err)
	}
	profileID, err := store.ActiveProfileID()
	if err != nil || profileID == "" {
		t.Fatalf("load existing profile ID: %q, %v", profileID, err)
	}
	service := newSIWCService(siwcConfig{redirectURI: "http://127.0.0.1:8080/auth/callback", tokenURL: tokenEndpoint.URL}, store)
	service.verifier = differentAccountIDTokenVerifier{}
	service.pending = &authAttempt{
		state: "state", nonce: "nonce", codeVerifier: "verifier", clientID: "issued-client", hostID: testHostID, profileID: profileID,
		expectedSubject: "account-subject", expiresAt: time.Now().Add(time.Minute),
	}
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state&code=code", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	service.callbackHandler(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	credentials, err := store.Load()
	if err != nil {
		t.Fatalf("load retained credentials: %v", err)
	}
	if credentials.AccessToken != "access-old" || credentials.RefreshToken != "refresh-old" {
		t.Fatal("the existing connection was replaced after a different account was selected")
	}
}

func TestAuthCallbackRejectsStateMismatchWithoutTokenExchange(t *testing.T) {
	called := false
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer tokenEndpoint.Close()
	service := newSIWCService(siwcConfig{redirectURI: "http://127.0.0.1:8080/auth/callback", tokenURL: tokenEndpoint.URL}, newFileCredentialStore(t.TempDir()))
	service.pending = &authAttempt{state: "expected-state", expiresAt: time.Now().Add(time.Minute)}
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?state=wrong&code=code&client_id=client", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()

	service.callbackHandler(recorder, request)

	if recorder.Code != http.StatusBadRequest || called {
		t.Fatalf("status = %d, token endpoint called = %t; want state rejection before exchange", recorder.Code, called)
	}
}

func TestAccessTokenRefreshesAndPersistsRotatedRefreshToken(t *testing.T) {
	var gotForm url.Values
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read refresh request: %v", err)
		}
		gotForm, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-rotated","refresh_token":"refresh-rotated","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenEndpoint.Close()

	store := newFileCredentialStore(t.TempDir())
	if err := store.Save(testOAuthCredentials("access-expired", "refresh-current", time.Now().Add(-time.Minute))); err != nil {
		t.Fatalf("save expired credentials: %v", err)
	}
	service := newSIWCService(siwcConfig{tokenURL: tokenEndpoint.URL}, store)
	service.client = tokenEndpoint.Client()

	accessToken, err := service.accessToken(t.Context())
	if err != nil {
		t.Fatalf("refresh access token: %v", err)
	}
	if accessToken != "access-rotated" {
		t.Fatalf("access token = %q, want rotated token", accessToken)
	}
	for key, expected := range map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     "issued-client",
		"refresh_token": "refresh-current",
		"resource":      siwcResource,
	} {
		if gotForm.Get(key) != expected {
			t.Errorf("refresh parameter %s = %q, want %q", key, gotForm.Get(key), expected)
		}
	}
	updated, err := store.Load()
	if err != nil {
		t.Fatalf("load rotated credentials: %v", err)
	}
	if updated.RefreshToken != "refresh-rotated" {
		t.Fatalf("refresh did not preserve or rotate credentials correctly: %#v", updated)
	}
}

func TestTerminalRefreshFailureClearsTokensAndOffersReauthorization(t *testing.T) {
	tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_refresh_expired")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"private refresh token detail"}`)
	}))
	defer tokenEndpoint.Close()
	store := newFileCredentialStore(t.TempDir())
	if err := store.Save(testOAuthCredentials("access-expired", "refresh-expired", time.Now().Add(-time.Minute))); err != nil {
		t.Fatalf("save expired credentials: %v", err)
	}
	service := newSIWCService(siwcConfig{tokenURL: tokenEndpoint.URL, redirectURI: "http://127.0.0.1:8080/auth/callback", authorizeURL: siwcAuthorizeURL}, store)
	service.client = tokenEndpoint.Client()
	_, refreshErr := service.accessToken(t.Context())
	if !isTerminalRefreshError(refreshErr) {
		t.Fatalf("refresh error = %v; expected terminal invalid_grant", refreshErr)
	}
	var failure *providerFailure
	if !errors.As(refreshErr, &failure) || failure.errorCode != "invalid_grant" || failure.requestID != "req_refresh_expired" {
		t.Fatalf("safe terminal refresh diagnostics = %#v", failure)
	}
	credentials, err := store.Load()
	if err != nil {
		t.Fatalf("load retained registration: %v", err)
	}
	if credentials.AccessToken != "" || credentials.RefreshToken != "" || credentials.IDToken != "id-token-test" || credentials.ClientID != "issued-client" || !credentials.OAuthPreferred {
		t.Fatalf("terminal refresh did not clear unusable tokens while retaining reauthorization identity: %#v", credentials)
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	statusRecorder := httptest.NewRecorder()
	service.statusHandler(statusRecorder, statusRequest)
	var status map[string]bool
	if err := json.NewDecoder(statusRecorder.Body).Decode(&status); err != nil {
		t.Fatalf("decode expired-session status: %v", err)
	}
	if status["connected"] || !status["oauth_selected"] || status["plan_usage_enabled"] {
		t.Fatalf("expired OAuth state should offer reauthorization without API fallback: %#v", status)
	}
	service.config.authorizeURL = siwcAuthorizeURL
	request := httptest.NewRequest(http.MethodGet, "/auth/start", nil)
	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()
	service.startHandler(recorder, request)
	location, err := url.Parse(recorder.Header().Get("Location"))
	if recorder.Code != http.StatusFound || err != nil || location.Query().Get("client_id") != "issued-client" || location.Query().Get("id_token_hint") != "id-token-test" || location.Query().Get("prompt") != "consent" {
		t.Fatalf("terminal refresh recovery did not offer reauthorization: status=%d location=%q err=%v", recorder.Code, recorder.Header().Get("Location"), err)
	}
}

func TestOAuthPreferredSessionDoesNotFallBackUntilExplicitDisconnect(t *testing.T) {
	var fallbackCalls atomic.Int32
	fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"API fallback reply"}}]}`)
	}))
	defer fallbackServer.Close()
	store := newFileCredentialStore(t.TempDir())
	credentials := testOAuthCredentials("access-test", "refresh-test", time.Now().Add(time.Hour))
	service := newSIWCService(siwcConfig{}, store)
	if err := service.clearSessionTokens(credentials, true, true, false); err != nil {
		t.Fatalf("retain registration after refresh failure: %v", err)
	}
	api := &openAICompatibleProvider{baseURL: fallbackServer.URL + "/v1", model: "fallback-model", client: fallbackServer.Client()}
	hybrid := newHybridProvider(api, service)
	_, err := hybrid.complete(context.Background(), []chatMessage{{Role: "user", Content: "Hi"}}, "")
	var failure *providerFailure
	if !errors.As(err, &failure) || failure.kind != "oauth_reauthorize" || fallbackCalls.Load() != 0 {
		t.Fatalf("expired OAuth selection fell back unexpectedly: err=%#v calls=%d", err, fallbackCalls.Load())
	}
	stored, err := store.Load()
	if err != nil {
		t.Fatalf("load registration before explicit disconnect: %v", err)
	}
	if err := service.clearSessionTokens(stored, false, false, true); err != nil {
		t.Fatalf("disconnect OAuth preference: %v", err)
	}
	reply, err := hybrid.complete(context.Background(), []chatMessage{{Role: "user", Content: "Hi"}}, "")
	if err != nil || reply != "API fallback reply" || fallbackCalls.Load() != 1 {
		t.Fatalf("explicit disconnect did not enable configured fallback: reply=%q err=%v calls=%d", reply, err, fallbackCalls.Load())
	}
}

func TestResponsesProviderUsesOAuthModelsAndStreamingResponsesAPI(t *testing.T) {
	const accessToken = "private-access-token"
	var sawModelList, sawResponse, sawLegacy bool
	var gotAuthorization string
	var gotPayload struct {
		Model  string        `json:"model"`
		Input  []chatMessage `json:"input"`
		Store  bool          `json:"store"`
		Stream bool          `json:"stream"`
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/v1/models":
			sawModelList = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"slug":"model-test","display_name":"Test model","visibility":"list"},{"slug":"private-model","display_name":"Private model","visibility":"hidden"}]}`))
		case "/v1/responses":
			sawResponse = true
			if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
				t.Errorf("decode Responses API request: %v", err)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\" from ChatGPT\"}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		case "/v1/chat/completions":
			sawLegacy = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Legacy provider"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	store := newFileCredentialStore(t.TempDir())
	if err := store.Save(testOAuthCredentials(accessToken, "refresh-test", time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
	auth := newSIWCService(siwcConfig{apiBaseURL: api.URL + "/v1"}, store)
	auth.client = api.Client()
	provider := &hybridProvider{
		api:   &openAICompatibleProvider{baseURL: api.URL + "/v1", apiKey: "legacy-key", model: "legacy-model", client: api.Client()},
		oauth: &responsesProvider{auth: auth, defaultModel: "model-test"},
	}

	models, err := provider.models(t.Context())
	if err != nil {
		t.Fatalf("fetch models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "model-test" || models[0].Name != "Test model" {
		t.Fatalf("models = %#v, want the account model catalog", models)
	}
	reply, err := provider.complete(t.Context(), []chatMessage{{Role: "user", Content: "Hi"}}, "model-test")
	if err != nil {
		t.Fatalf("complete with Responses API: %v", err)
	}
	if reply != "Hello from ChatGPT" {
		t.Fatalf("reply = %q, want streamed text", reply)
	}
	if !sawModelList || !sawResponse || sawLegacy || gotAuthorization != "Bearer "+accessToken {
		t.Fatalf("models=%t responses=%t legacy=%t authorization=%q", sawModelList, sawResponse, sawLegacy, gotAuthorization)
	}
	if gotPayload.Model != "model-test" || !gotPayload.Stream || gotPayload.Store || len(gotPayload.Input) != 1 || gotPayload.Input[0].Content != "Hi" {
		t.Fatalf("Responses API payload = %#v, want chosen model, stream=true, store=false, and input", gotPayload)
	}
}

func TestOpenAIIDTokenVerifierChecksSignatureAudienceAndNonce(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test signing key: %v", err)
	}
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": issuer, "authorization_endpoint": issuer + "/authorize",
				"token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks",
				"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			exponent := big.NewInt(int64(key.PublicKey.E)).Bytes()
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "kid": "test-key", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(exponent),
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL

	clientID := "issued-client"
	nonce := "nonce-from-auth-request"
	rawToken := signTestIDToken(t, key, map[string]any{
		"iss": issuer, "sub": "account-subject", "aud": clientID,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"nonce": nonce, "email": "owner@example.test", "name": "Owner",
	})
	verifier := &openAIIDTokenVerifier{issuerURL: issuer}
	identity, err := verifier.Verify(t.Context(), rawToken, clientID, nonce)
	if err != nil {
		t.Fatalf("verify valid ID token: %v", err)
	}
	if identity.subject != "account-subject" || identity.email != "owner@example.test" || identity.name != "Owner" {
		t.Fatalf("identity = %#v, want verified test identity", identity)
	}
	if _, err := verifier.Verify(t.Context(), rawToken, clientID, "wrong-nonce"); err == nil {
		t.Fatal("ID token verifier accepted a mismatched nonce")
	}
	if _, err := verifier.Verify(t.Context(), rawToken, "wrong-client", nonce); err == nil {
		t.Fatal("ID token verifier accepted a mismatched audience")
	}
}

func TestResponsesStreamRequiresCompletedEventEvenAfterDoneSentinel(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: [DONE]\n\n")),
	}
	if _, err := collectResponseText(response); err == nil {
		t.Fatal("Responses API stream was accepted without response.completed")
	} else {
		var failure *providerFailure
		if !errors.As(err, &failure) || failure.kind != "responses_incomplete" {
			t.Fatalf("stream error = %#v; expected incomplete response failure", err)
		}
	}
}

func TestResponsesIncompleteEventIsFailure(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n")),
	}
	if _, err := collectResponseText(response); err == nil {
		t.Fatal("Responses API incomplete event was accepted as a successful reply")
	} else {
		var failure *providerFailure
		if !errors.As(err, &failure) || failure.errorCode != "response_incomplete" {
			t.Fatalf("incomplete event error = %#v", err)
		}
	}
}

func TestResponsesFailedEventPreservesSanitizedUsageError(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"req_usage_limit"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"usage_limit_reached\",\"code\":\"subscription_sharing_usage_limit_exceeded\",\"param\":\"input[0]\"}}}\n\n")),
	}
	if _, err := collectResponseText(response); err == nil {
		t.Fatal("Responses API failure event was accepted as a successful reply")
	} else {
		var failure *providerFailure
		if !errors.As(err, &failure) || failure.errorCode != "subscription_sharing_usage_limit_exceeded" || failure.errorParam != "input[0]" || failure.requestID != "req_usage_limit" || failure.errorShape != "stream_event" {
			t.Fatalf("safe usage failure = %#v", failure)
		}
	}
}

func signTestIDToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"})
	if err != nil {
		t.Fatalf("encode test token header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("encode test token claims: %v", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}
