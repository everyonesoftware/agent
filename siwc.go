package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	siwcAuthorizeURL = "https://auth.openai.com/api/accounts/authorize"
	siwcTokenURL     = "https://auth.openai.com/api/accounts/oauth/token"
	siwcIssuer       = "https://auth.openai.com"
	siwcResource     = "https://api.openai.com/v1"
	siwcClientID     = "dynamic_agent_client"
	siwcAgentName    = "EveryoneAgent"
	siwcScope        = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
)

var errChatGPTNotConnected = errors.New("ChatGPT is not connected")

type oauthCredentials struct {
	ClientID            string    `json:"client_id"`
	HostID              string    `json:"ext_agent_host_id"`
	AccessToken         string    `json:"access_token"`
	RefreshToken        string    `json:"refresh_token"`
	IDToken             string    `json:"id_token"`
	TokenType           string    `json:"token_type"`
	Issuer              string    `json:"issuer"`
	Subject             string    `json:"subject"`
	Scopes              []string  `json:"scopes"`
	ExpiresAt           time.Time `json:"expires_at"`
	OAuthPreferred      bool      `json:"oauth_preferred"`
	RegistrationPending bool      `json:"registration_pending"`
}

type credentialStore interface {
	Load() (oauthCredentials, error)
	Save(oauthCredentials) error
	Delete() error
	HostID() (string, error)
	LoadProfile(string) (credentialProfile, error)
	SaveProfile(credentialProfile) error
	ListProfiles() ([]credentialProfile, error)
	ActiveProfileID() (string, error)
	SetActiveProfile(string) error
	DeleteProfile(string) error
}

func validateOAuthCredentials(credentials oauthCredentials) error {
	if credentials.ClientID == "" || credentials.Issuer == "" || !validHostID(credentials.HostID) {
		return errors.New("credential registration is incomplete")
	}
	if credentials.RegistrationPending == (credentials.Subject != "") {
		return errors.New("credential identity state is inconsistent")
	}
	hasAccess, hasRefresh := credentials.AccessToken != "", credentials.RefreshToken != ""
	if hasAccess != hasRefresh {
		return errors.New("credential tokens are incomplete")
	}
	if !hasAccess {
		if credentials.TokenType != "" || len(credentials.Scopes) != 0 || !credentials.ExpiresAt.IsZero() ||
			(credentials.RegistrationPending && credentials.IDToken != "") ||
			(!credentials.RegistrationPending && !credentials.OAuthPreferred && credentials.IDToken != "") {
			return errors.New("signed-out credentials contain tokens")
		}
		return nil
	}
	if credentials.RegistrationPending || credentials.IDToken == "" || credentials.TokenType == "" || !strings.EqualFold(credentials.TokenType, "Bearer") || credentials.ExpiresAt.IsZero() {
		return errors.New("credential tokens are incomplete")
	}
	return nil
}

func hasPlanUsageScope(scopes []string) bool {
	return containsScope(scopes, "resource.invoke") && containsScope(scopes, "chatgpt.tokens.use.direct")
}

func credentialsConnected(credentials oauthCredentials) bool {
	return credentials.AccessToken != "" && credentials.RefreshToken != ""
}

func credentialsUseOAuth(credentials oauthCredentials) bool {
	return credentials.OAuthPreferred || credentialsConnected(credentials)
}

func credentialsPlanUsageEnabled(credentials oauthCredentials) bool {
	return credentialsConnected(credentials) && hasPlanUsageScope(credentials.Scopes)
}

type fileCredentialStore struct {
	dir string
	mu  sync.Mutex
}

type credentialProfile struct {
	ID          string           `json:"id"`
	Label       string           `json:"label,omitempty"`
	Email       string           `json:"email,omitempty"`
	Credentials oauthCredentials `json:"credentials"`
}

type credentialProfileState struct {
	Version         int                 `json:"version"`
	ActiveProfileID string              `json:"active_profile_id,omitempty"`
	Profiles        []credentialProfile `json:"profiles"`
}

const credentialProfileStateVersion = 1

func newFileCredentialStore(dir string) *fileCredentialStore {
	return &fileCredentialStore{dir: dir}
}

func (s *fileCredentialStore) Load() (oauthCredentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadStateLocked()
	if err != nil {
		return oauthCredentials{}, err
	}
	if state.ActiveProfileID == "" {
		return oauthCredentials{}, os.ErrNotExist
	}
	for _, profile := range state.Profiles {
		if profile.ID == state.ActiveProfileID {
			return profile.Credentials, nil
		}
	}
	return oauthCredentials{}, os.ErrNotExist
}

func (s *fileCredentialStore) Save(credentials oauthCredentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateOAuthCredentials(credentials); err != nil {
		return errors.New("credential data is incomplete")
	}
	state, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	id := state.ActiveProfileID
	if id == "" {
		id, err = newProfileID()
		if err != nil {
			return errors.New("profile ID could not be generated")
		}
		if credentials.Subject != "" && !credentials.RegistrationPending {
			state.ActiveProfileID = id
		}
	}
	profile := credentialProfile{ID: id, Credentials: credentials}
	state.Profiles = upsertCredentialProfile(state.Profiles, profile)
	return s.writeStateLocked(state)
}

func (s *fileCredentialStore) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	if state.ActiveProfileID == "" {
		return nil
	}
	state.Profiles = removeCredentialProfile(state.Profiles, state.ActiveProfileID)
	state.ActiveProfileID = ""
	return s.writeStateLocked(state)
}

func (s *fileCredentialStore) LoadProfile(id string) (credentialProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadStateLocked()
	if err != nil {
		return credentialProfile{}, err
	}
	for _, profile := range state.Profiles {
		if profile.ID == id {
			return profile, nil
		}
	}
	return credentialProfile{}, os.ErrNotExist
}

func (s *fileCredentialStore) SaveProfile(profile credentialProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validProfileID(profile.ID) || validateOAuthCredentials(profile.Credentials) != nil {
		return errors.New("profile data is incomplete")
	}
	state, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	state.Profiles = upsertCredentialProfile(state.Profiles, profile)
	return s.writeStateLocked(state)
}

func (s *fileCredentialStore) ListProfiles() ([]credentialProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadStateLocked()
	if err != nil {
		return nil, err
	}
	profiles := append([]credentialProfile(nil), state.Profiles...)
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	return profiles, nil
}

func (s *fileCredentialStore) ActiveProfileID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadStateLocked()
	if err != nil {
		return "", err
	}
	return state.ActiveProfileID, nil
}

func (s *fileCredentialStore) SetActiveProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	for _, profile := range state.Profiles {
		if profile.ID == id {
			if profile.Credentials.RegistrationPending || profile.Credentials.Subject == "" {
				return errors.New("profile identity is not verified")
			}
			state.ActiveProfileID = id
			return s.writeStateLocked(state)
		}
	}
	return errors.New("profile does not exist")
}

func (s *fileCredentialStore) DeleteProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadStateLocked()
	if err != nil {
		return err
	}
	if !containsCredentialProfile(state.Profiles, id) {
		return nil
	}
	state.Profiles = removeCredentialProfile(state.Profiles, id)
	if state.ActiveProfileID == id {
		state.ActiveProfileID = ""
	}
	return s.writeStateLocked(state)
}

func (s *fileCredentialStore) loadStateLocked() (credentialProfileState, error) {
	var state credentialProfileState
	statePath := filepath.Join(s.dir, "siwc-profiles.json")
	info, err := os.Stat(statePath)
	if err == nil {
		if info.Mode().Perm()&0077 != 0 {
			return credentialProfileState{}, errors.New("profile file permissions are too broad")
		}
		data, readErr := os.ReadFile(statePath)
		if readErr != nil {
			return credentialProfileState{}, errors.New("profile file could not be read")
		}
		if json.Unmarshal(data, &state) != nil || validateCredentialProfileState(state) != nil {
			return credentialProfileState{}, errors.New("profile file is invalid")
		}
		if removeErr := os.Remove(filepath.Join(s.dir, "siwc-token.json")); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return credentialProfileState{}, errors.New("legacy credential file could not be removed")
		}
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return credentialProfileState{}, errors.New("profile file could not be inspected")
	}

	legacyPath := filepath.Join(s.dir, "siwc-token.json")
	legacyInfo, legacyErr := os.Stat(legacyPath)
	if errors.Is(legacyErr, os.ErrNotExist) {
		return credentialProfileState{Version: credentialProfileStateVersion}, nil
	}
	if legacyErr != nil {
		return credentialProfileState{}, errors.New("legacy credential file could not be inspected")
	}
	if legacyInfo.Mode().Perm()&0077 != 0 {
		return credentialProfileState{}, errors.New("credential file permissions are too broad")
	}
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return credentialProfileState{}, errors.New("legacy credential file could not be read")
	}
	var credentials oauthCredentials
	if json.Unmarshal(data, &credentials) != nil || validateOAuthCredentials(credentials) != nil {
		return credentialProfileState{}, errors.New("legacy credential file is invalid")
	}
	id, err := newProfileID()
	if err != nil {
		return credentialProfileState{}, errors.New("profile ID could not be generated")
	}
	activeID := ""
	if credentials.Subject != "" && !credentials.RegistrationPending {
		activeID = id
	}
	state = credentialProfileState{
		Version: credentialProfileStateVersion, ActiveProfileID: activeID,
		Profiles: []credentialProfile{{ID: id, Label: "ChatGPT account 1", Credentials: credentials}},
	}
	if err := s.writeStateLocked(state); err != nil {
		return credentialProfileState{}, err
	}
	if err := os.Remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return credentialProfileState{}, errors.New("legacy credential file could not be removed")
	}
	return state, nil
}

func (s *fileCredentialStore) writeStateLocked(state credentialProfileState) error {
	if err := validateCredentialProfileState(state); err != nil {
		return errors.New("profile state is invalid")
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return errors.New("credential directory is unavailable")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return errors.New("profile state could not be encoded")
	}
	temp, err := os.CreateTemp(s.dir, ".siwc-profiles-*.tmp")
	if err != nil {
		return errors.New("profile file could not be created")
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	// CreateTemp already requests mode 0600; do not chmod the host mount.
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return errors.New("profile state could not be written")
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return errors.New("profile state could not be synchronized")
	}
	if err := temp.Close(); err != nil {
		return errors.New("profile file could not be closed")
	}
	if err := os.Rename(tempName, filepath.Join(s.dir, "siwc-profiles.json")); err != nil {
		return errors.New("profile file could not be replaced")
	}
	return nil
}

func newProfileID() (string, error) {
	token, err := randomURLToken(24)
	if err != nil {
		return "", err
	}
	return "profile_" + token, nil
}

func nextProfileLabel(profiles []credentialProfile) string {
	used := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		used[profile.Label] = struct{}{}
	}
	for index := 1; ; index++ {
		label := "ChatGPT account " + strconv.Itoa(index)
		if _, exists := used[label]; !exists {
			return label
		}
	}
}

func validProfileID(id string) bool {
	if !strings.HasPrefix(id, "profile_") || len(id) < 20 || len(id) > 64 {
		return false
	}
	for _, r := range strings.TrimPrefix(id, "profile_") {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func validateCredentialProfileState(state credentialProfileState) error {
	if state.Version != credentialProfileStateVersion {
		return errors.New("unsupported profile state version")
	}
	seen := make(map[string]struct{}, len(state.Profiles))
	activeFound := state.ActiveProfileID == ""
	for _, profile := range state.Profiles {
		if !validProfileID(profile.ID) || validateOAuthCredentials(profile.Credentials) != nil {
			return errors.New("invalid profile")
		}
		if _, exists := seen[profile.ID]; exists {
			return errors.New("duplicate profile")
		}
		seen[profile.ID] = struct{}{}
		if profile.ID == state.ActiveProfileID {
			if profile.Credentials.RegistrationPending || profile.Credentials.Subject == "" {
				return errors.New("active profile identity is not verified")
			}
			activeFound = true
		}
	}
	if !activeFound {
		return errors.New("active profile does not exist")
	}
	return nil
}

func upsertCredentialProfile(profiles []credentialProfile, value credentialProfile) []credentialProfile {
	for i := range profiles {
		if profiles[i].ID == value.ID {
			profiles[i] = value
			return profiles
		}
	}
	return append(profiles, value)
}

func containsCredentialProfile(profiles []credentialProfile, id string) bool {
	for _, profile := range profiles {
		if profile.ID == id {
			return true
		}
	}
	return false
}

func removeCredentialProfile(profiles []credentialProfile, id string) []credentialProfile {
	filtered := profiles[:0]
	for _, profile := range profiles {
		if profile.ID != id {
			filtered = append(filtered, profile)
		}
	}
	return filtered
}

func (s *fileCredentialStore) HostID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return "", errors.New("credential directory is unavailable")
	}
	path := filepath.Join(s.dir, "host-id")
	if data, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(data))
		if validHostID(value) {
			return value, nil
		}
		return "", errors.New("host identifier is invalid")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("host identifier could not be read")
	}
	state, stateErr := s.loadStateLocked()
	var credentials oauthCredentials
	if stateErr == nil {
		for _, profile := range state.Profiles {
			if profile.ID == state.ActiveProfileID {
				credentials = profile.Credentials
				break
			}
		}
	}
	if stateErr == nil && state.ActiveProfileID != "" && credentials.HostID != "" {
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(createErr, os.ErrExist) {
			data, readErr := os.ReadFile(path)
			value := strings.TrimSpace(string(data))
			if readErr == nil && validHostID(value) {
				return value, nil
			}
			return "", errors.New("host identifier is invalid")
		}
		if createErr != nil {
			return "", errors.New("host identifier could not be created")
		}
		if _, writeErr := file.WriteString(credentials.HostID + "\n"); writeErr != nil {
			_ = file.Close()
			return "", errors.New("host identifier could not be written")
		}
		if closeErr := file.Close(); closeErr != nil {
			return "", errors.New("host identifier could not be written")
		}
		return credentials.HostID, nil
	} else if stateErr != nil {
		return "", errors.New("registered host identity could not be loaded")
	}
	id, err := newUUID()
	if err != nil {
		return "", errors.New("host identifier could not be generated")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", errors.New("host identifier could not be read")
		}
		value := strings.TrimSpace(string(data))
		if !validHostID(value) {
			return "", errors.New("host identifier is invalid")
		}
		return value, nil
	}
	if err != nil {
		return "", errors.New("host identifier could not be created")
	}
	defer file.Close()
	if _, err := file.WriteString(id + "\n"); err != nil {
		return "", errors.New("host identifier could not be written")
	}
	return id, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return "urn:uuid:" + fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func validHostID(value string) bool {
	if !strings.HasPrefix(value, "urn:uuid:") {
		return false
	}
	value = strings.TrimPrefix(value, "urn:uuid:")
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(decoded) == 16
}

type siwcConfig struct {
	redirectURI  string
	authorizeURL string
	tokenURL     string
	issuerURL    string
	apiBaseURL   string
}

type authAttempt struct {
	state            string
	nonce            string
	codeVerifier     string
	clientID         string
	hostID           string
	profileID        string
	profileLabel     string
	previousActiveID string
	activateIfUnset  bool
	expectedSubject  string
	newRegistration  bool
	expiresAt        time.Time
}

type siwcService struct {
	config          siwcConfig
	store           credentialStore
	client          *http.Client
	verifier        idTokenVerifier
	mu              sync.Mutex
	tokenMu         sync.Mutex
	modelsMu        sync.Mutex
	modelsAt        time.Time
	models          []modelOption
	modelsProfileID string
	pending         *authAttempt
	now             func() time.Time
	metadataMu      sync.Mutex
	metadata        *openIDMetadata
}

func newSIWCService(config siwcConfig, store credentialStore) *siwcService {
	if config.issuerURL == "" {
		config.issuerURL = siwcIssuer
	}
	if config.apiBaseURL == "" {
		config.apiBaseURL = siwcResource
	}
	return &siwcService{
		config:   config,
		store:    store,
		client:   &http.Client{Timeout: 90 * time.Second},
		verifier: &openAIIDTokenVerifier{issuerURL: config.issuerURL},
		now:      time.Now,
	}
}

func newSIWCServiceFromEnv() *siwcService {
	dir := strings.TrimSpace(os.Getenv("SIWC_DATA_DIR"))
	if dir == "" {
		dir = "/data"
	}
	redirectURI := strings.TrimSpace(os.Getenv("SIWC_REDIRECT_URI"))
	if redirectURI == "" {
		redirectURI = "http://127.0.0.1:8080/auth/callback"
	}
	return newSIWCService(siwcConfig{redirectURI: redirectURI}, newFileCredentialStore(dir))
}

type openIDMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
}

func (s *siwcService) discoveryMetadata(ctx context.Context) (*openIDMetadata, error) {
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	if s.metadata != nil {
		return s.metadata, nil
	}
	discoveryURL := strings.TrimRight(s.config.issuerURL, "/") + "/.well-known/openid-configuration"
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, &providerFailure{kind: "oauth_discovery_build"}
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, &providerFailure{kind: "oauth_discovery_transport"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, readSafeOAuthFailure(response, "oauth_discovery")
	}
	var metadata openIDMetadata
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&metadata); err != nil {
		return nil, &providerFailure{kind: "oauth_discovery_decode", status: response.StatusCode}
	}
	if metadata.Issuer != s.config.issuerURL || !sameProviderOrigin(s.config.issuerURL, metadata.AuthorizationEndpoint) || !sameProviderOrigin(s.config.issuerURL, metadata.TokenEndpoint) ||
		(metadata.RevocationEndpoint != "" && !sameProviderOrigin(s.config.issuerURL, metadata.RevocationEndpoint)) {
		return nil, &providerFailure{kind: "oauth_discovery_invalid", status: response.StatusCode}
	}
	s.metadata = &metadata
	return s.metadata, nil
}

func sameProviderOrigin(issuer, endpoint string) bool {
	issuerURL, issuerErr := url.Parse(issuer)
	endpointURL, endpointErr := url.Parse(endpoint)
	if issuerErr != nil || endpointErr != nil || issuerURL.Host == "" || endpointURL.Host == "" ||
		issuerURL.Scheme != endpointURL.Scheme || !strings.EqualFold(issuerURL.Host, endpointURL.Host) ||
		endpointURL.User != nil || endpointURL.RawQuery != "" || endpointURL.Fragment != "" || endpointURL.Path == "" {
		return false
	}
	return issuerURL.Scheme == "https" || isLoopbackHost(issuerURL.Host)
}

func (s *siwcService) tokenEndpoint(ctx context.Context) (string, error) {
	if s.config.tokenURL != "" {
		return s.config.tokenURL, nil
	}
	metadata, err := s.discoveryMetadata(ctx)
	if err != nil {
		return "", err
	}
	return metadata.TokenEndpoint, nil
}

func (s *siwcService) startHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackHost(r.Host) || requestIsCrossSite(r) {
		http.Error(w, "Start ChatGPT sign-in from a browser on the EveryoneAgent host.", http.StatusForbidden)
		return
	}
	if !validSIWCRedirectURI(s.config.redirectURI) {
		http.Error(w, "ChatGPT sign-in is not configured for this local server.", http.StatusServiceUnavailable)
		return
	}
	authorizeURL := s.config.authorizeURL
	if authorizeURL == "" {
		metadata, discoveryErr := s.discoveryMetadata(r.Context())
		if discoveryErr != nil {
			logProviderFailure(discoveryErr)
			http.Error(w, "ChatGPT sign-in endpoints are unavailable.", http.StatusServiceUnavailable)
			return
		}
		authorizeURL = metadata.AuthorizationEndpoint
	}
	hostID, err := s.store.HostID()
	if err != nil {
		http.Error(w, "ChatGPT sign-in could not be prepared.", http.StatusServiceUnavailable)
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode != "" && mode != "add" && mode != "retry" {
		http.Error(w, "The requested ChatGPT account action is invalid.", http.StatusBadRequest)
		return
	}
	addAccount := mode == "add"
	activeID, err := s.store.ActiveProfileID()
	if err != nil {
		http.Error(w, "ChatGPT sign-in could not be prepared.", http.StatusServiceUnavailable)
		return
	}
	profileID, profileLabel := activeID, ""
	loginHint := ""
	clientID, idTokenHint, expectedSubject, existingScopes, newRegistration := siwcClientID, "", "", []string(nil), true
	activateIfUnset := activeID == ""
	if mode == "retry" {
		profileID = r.URL.Query().Get("profile_id")
		profile, loadErr := s.store.LoadProfile(profileID)
		if !validProfileID(profileID) || loadErr != nil || !profile.Credentials.RegistrationPending || profile.Credentials.Subject != "" {
			http.Error(w, "That incomplete ChatGPT account could not be resumed.", http.StatusBadRequest)
			return
		}
		credentials := profile.Credentials
		profileLabel = profile.Label
		loginHint = profile.Email
		clientID, idTokenHint, expectedSubject, existingScopes, newRegistration = credentials.ClientID, credentials.IDToken, credentials.Subject, credentials.Scopes, false
	} else if !addAccount && activeID != "" {
		profile, loadErr := s.store.LoadProfile(activeID)
		if loadErr != nil {
			http.Error(w, "ChatGPT sign-in could not be prepared.", http.StatusServiceUnavailable)
			return
		}
		credentials := profile.Credentials
		profileLabel = profile.Label
		loginHint = profile.Email
		if profileLabel == "" {
			profileLabel = "ChatGPT account"
		}
		clientID, idTokenHint, expectedSubject, existingScopes, newRegistration = credentials.ClientID, credentials.IDToken, credentials.Subject, credentials.Scopes, false
	} else {
		profiles, listErr := s.store.ListProfiles()
		if listErr != nil {
			http.Error(w, "ChatGPT sign-in could not be prepared.", http.StatusServiceUnavailable)
			return
		}
		if !addAccount {
			pendingProfiles := make([]credentialProfile, 0, len(profiles))
			for _, profile := range profiles {
				if profile.Credentials.RegistrationPending && profile.Credentials.Subject == "" && profile.Credentials.ClientID != siwcClientID {
					pendingProfiles = append(pendingProfiles, profile)
				}
			}
			if len(pendingProfiles) > 1 {
				http.Error(w, "Select the incomplete ChatGPT account from the host account selector to resume sign-in.", http.StatusConflict)
				return
			}
			if len(pendingProfiles) == 1 {
				profile := pendingProfiles[0]
				profileID, profileLabel = profile.ID, profile.Label
				loginHint = profile.Email
				credentials := profile.Credentials
				clientID, idTokenHint, expectedSubject, existingScopes, newRegistration = credentials.ClientID, credentials.IDToken, credentials.Subject, credentials.Scopes, false
			}
		}
		if newRegistration {
			profileID, err = newProfileID()
			if err != nil {
				http.Error(w, "ChatGPT sign-in could not be prepared.", http.StatusInternalServerError)
				return
			}
			profileLabel = nextProfileLabel(profiles)
		}
	}
	if profileID == "" {
		http.Error(w, "ChatGPT sign-in could not be prepared.", http.StatusServiceUnavailable)
		return
	}
	state, err := randomURLToken(32)
	if err != nil {
		http.Error(w, "ChatGPT sign-in could not be prepared.", http.StatusInternalServerError)
		return
	}
	nonce, err := randomURLToken(32)
	if err != nil {
		http.Error(w, "ChatGPT sign-in could not be prepared.", http.StatusInternalServerError)
		return
	}
	verifier, err := randomURLToken(32)
	if err != nil {
		http.Error(w, "ChatGPT sign-in could not be prepared.", http.StatusInternalServerError)
		return
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	attempt := &authAttempt{
		state: state, nonce: nonce, codeVerifier: verifier,
		clientID: clientID, hostID: hostID, profileID: profileID, profileLabel: profileLabel, previousActiveID: activeID,
		activateIfUnset: activateIfUnset,
		expectedSubject: expectedSubject, newRegistration: newRegistration,
		expiresAt: s.now().Add(10 * time.Minute),
	}
	s.mu.Lock()
	s.pending = attempt
	s.mu.Unlock()

	query := url.Values{}
	query.Set("client_id", clientID)
	query.Set("redirect_uri", s.config.redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", siwcScope)
	query.Set("state", state)
	query.Set("nonce", nonce)
	query.Set("resource", siwcResource)
	if newRegistration {
		query.Set("agent_name_hint", siwcAgentName)
	} else {
		if idTokenHint != "" {
			query.Set("id_token_hint", idTokenHint)
		}
		if loginHint != "" {
			query.Set("login_hint", loginHint)
		}
	}
	if !newRegistration && !hasPlanUsageScope(existingScopes) {
		query.Set("prompt", "consent")
	}
	query.Set("ext_agent_host_id", hostID)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, authorizeURL+"?"+query.Encode(), http.StatusFound)
}

func (s *siwcService) statusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	credentials, err := s.store.Load()
	connected := err == nil && credentialsConnected(credentials)
	oauthSelected := err == nil && credentialsUseOAuth(credentials)
	planUsageEnabled := err == nil && credentialsPlanUsageEnabled(credentials)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, "ChatGPT connection status is unavailable.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeChatJSON(w, http.StatusOK, map[string]bool{"connected": connected, "oauth_selected": oauthSelected, "plan_usage_enabled": planUsageEnabled})
}

func (s *siwcService) disconnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackHost(r.Host) || requestIsCrossSite(r) {
		http.Error(w, "Manage the ChatGPT connection from a browser on the EveryoneAgent host.", http.StatusForbidden)
		return
	}
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	profileID, err := s.store.ActiveProfileID()
	if err != nil {
		http.Error(w, "The ChatGPT connection could not be read on this host.", http.StatusInternalServerError)
		return
	}
	if profileID == "" {
		writeChatJSON(w, http.StatusOK, map[string]any{"connected": false, "remote_revocation_confirmed": true})
		return
	}
	profile, err := s.store.LoadProfile(profileID)
	if err != nil {
		http.Error(w, "The ChatGPT connection could not be read on this host.", http.StatusInternalServerError)
		return
	}
	credentials := profile.Credentials
	remoteRevoked := true
	if credentials.RefreshToken != "" {
		var revokeErr error
		for attempt := 0; attempt < 3; attempt++ {
			revokeErr = s.revokeRefreshToken(r.Context(), credentials)
			if revokeErr == nil || !retryableRevocationError(revokeErr) || attempt == 2 {
				break
			}
			timer := time.NewTimer(time.Duration(attempt+1) * 200 * time.Millisecond)
			select {
			case <-r.Context().Done():
				timer.Stop()
				revokeErr = r.Context().Err()
			case <-timer.C:
			}
			if r.Context().Err() != nil {
				break
			}
		}
		if revokeErr != nil {
			remoteRevoked = false
			logProviderFailure(revokeErr)
		}
	}
	if err := s.clearProfileSessionTokens(profile, false, false, true); err != nil {
		http.Error(w, "The ChatGPT connection could not be cleared on this host.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeChatJSON(w, http.StatusOK, map[string]any{
		"connected":                   false,
		"remote_revocation_confirmed": remoteRevoked,
	})
}

func retryableRevocationError(err error) bool {
	var failure *providerFailure
	if !errors.As(err, &failure) {
		return true
	}
	if failure.kind == "oauth_revoke_unavailable" || failure.kind == "oauth_discovery_invalid" {
		return false
	}
	return failure.status == 0 || failure.status >= http.StatusInternalServerError
}

func (s *siwcService) revokeRefreshToken(ctx context.Context, credentials oauthCredentials) error {
	metadata, err := s.discoveryMetadata(ctx)
	if err != nil {
		return err
	}
	if metadata.RevocationEndpoint == "" {
		return &providerFailure{kind: "oauth_revoke_unavailable"}
	}
	values := url.Values{}
	values.Set("token", credentials.RefreshToken)
	values.Set("token_type_hint", "refresh_token")
	values.Set("client_id", credentials.ClientID)
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, metadata.RevocationEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return &providerFailure{kind: "oauth_revoke_build"}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return &providerFailure{kind: "oauth_revoke_transport"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return readSafeOAuthFailure(response, "oauth_revoke")
	}
	return nil
}

func (s *siwcService) clearSessionTokens(credentials oauthCredentials, keepIDTokenHint, oauthPreferred, deleteOnSaveFailure bool) error {
	profileID, err := s.store.ActiveProfileID()
	if err != nil {
		return errors.New("credentials could not be cleared")
	}
	selectIfUnset := profileID == ""
	var profile credentialProfile
	if profileID == "" {
		profileID, err = newProfileID()
		if err != nil {
			return errors.New("credentials could not be cleared")
		}
		profile = credentialProfile{ID: profileID}
	} else {
		profile, err = s.store.LoadProfile(profileID)
		if err != nil {
			return errors.New("credentials could not be cleared")
		}
	}
	profile.Credentials = credentials
	if err := s.clearProfileSessionTokens(profile, keepIDTokenHint, oauthPreferred, deleteOnSaveFailure); err != nil {
		return err
	}
	if selectIfUnset {
		activeID, err := s.store.ActiveProfileID()
		if err != nil {
			return errors.New("credentials could not be cleared")
		}
		if activeID == "" {
			if err := s.store.SetActiveProfile(profileID); err != nil {
				return errors.New("credentials could not be cleared")
			}
		}
	}
	return nil
}

func (s *siwcService) clearProfileSessionTokens(profile credentialProfile, keepIDTokenHint, oauthPreferred, _ bool) error {
	credentials := profile.Credentials
	if !keepIDTokenHint {
		credentials.IDToken = ""
	}
	credentials.AccessToken = ""
	credentials.RefreshToken = ""
	credentials.TokenType = ""
	credentials.Scopes = nil
	credentials.ExpiresAt = time.Time{}
	credentials.OAuthPreferred = oauthPreferred
	profile.Credentials = credentials
	if err := s.store.SaveProfile(profile); err != nil {
		return errors.New("credentials could not be cleared")
	}
	s.modelsMu.Lock()
	s.models, s.modelsAt, s.modelsProfileID = nil, time.Time{}, ""
	s.modelsMu.Unlock()
	return nil
}

func randomURLToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func isLoopbackHost(hostPort string) bool {
	host := hostPort
	if parsedHost, _, err := net.SplitHostPort(hostPort); err == nil {
		host = parsedHost
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requestIsCrossSite(r *http.Request) bool {
	// Browser-provided metadata survives TLS termination and Host rewrites.
	// Do not trust forwarding headers to authorize an origin.
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "same-origin", "none":
		return false
	case "":
		// Older browsers fall back to comparing Origin with the request Host.
	default:
		return true
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || !strings.EqualFold(parsed.Host, r.Host)
}

func validSIWCRedirectURI(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Path != "/auth/callback" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return false
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return false
	}
	portNumber, err := strconv.Atoi(port)
	return err == nil && portNumber > 0 && portNumber <= 65535
}

type verifiedIdentity struct {
	subject string
	email   string
	name    string
}

type idTokenVerifier interface {
	Verify(context.Context, string, string, string) (verifiedIdentity, error)
}

type openAIIDTokenVerifier struct {
	issuerURL string
	mu        sync.Mutex
	provider  *oidc.Provider
}

func (v *openAIIDTokenVerifier) Verify(ctx context.Context, rawToken, clientID, nonce string) (verifiedIdentity, error) {
	provider, err := v.getProvider(ctx)
	if err != nil || provider == nil {
		return verifiedIdentity{}, errors.New("OpenID provider metadata is unavailable")
	}
	token, err := provider.Verifier(&oidc.Config{ClientID: clientID}).Verify(ctx, rawToken)
	if err != nil {
		return verifiedIdentity{}, errors.New("ID token verification failed")
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(token.Nonce), []byte(nonce)) != 1 || token.Subject == "" {
		return verifiedIdentity{}, errors.New("ID token claims did not match the sign-in request")
	}
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := token.Claims(&claims); err != nil {
		return verifiedIdentity{}, errors.New("ID token claims could not be read")
	}
	return verifiedIdentity{subject: token.Subject, email: claims.Email, name: claims.Name}, nil
}

func (v *openAIIDTokenVerifier) getProvider(ctx context.Context) (*oidc.Provider, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.provider != nil {
		return v.provider, nil
	}
	providerCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(providerCtx, v.issuerURL)
	if err != nil {
		return nil, err
	}
	v.provider = provider
	return v.provider, nil
}

type siwcTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (s *siwcService) callbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackHost(r.Host) {
		http.Error(w, "Sign-in callbacks are accepted only on the local EveryoneAgent host.", http.StatusForbidden)
		return
	}
	state := r.URL.Query().Get("state")
	s.mu.Lock()
	pending := s.pending
	validState := pending != nil && pending.expiresAt.After(s.now()) && validHostID(pending.hostID) && state != "" && len(state) == len(pending.state) && subtle.ConstantTimeCompare([]byte(state), []byte(pending.state)) == 1
	if validState {
		s.pending = nil
	}
	s.mu.Unlock()
	if !validState {
		http.Error(w, "This sign-in request is invalid or has expired. Start again from EveryoneAgent.", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("error") != "" {
		http.Error(w, "ChatGPT sign-in was not completed. Return to EveryoneAgent and try again.", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	callbackClientID := r.URL.Query().Get("client_id")
	clientID := pending.clientID
	if code == "" || len(code) > 4096 || len(callbackClientID) > 512 || clientID == "" {
		http.Error(w, "ChatGPT did not return a valid sign-in response.", http.StatusBadRequest)
		return
	}
	if pending.newRegistration {
		if callbackClientID == "" || callbackClientID == siwcClientID {
			http.Error(w, "ChatGPT did not issue an application client ID.", http.StatusBadRequest)
			return
		}
		clientID = callbackClientID
	} else if callbackClientID != "" && callbackClientID != pending.clientID {
		http.Error(w, "ChatGPT returned a different application registration. Start again from EveryoneAgent.", http.StatusBadRequest)
		return
	}
	if pending.newRegistration {
		registration := oauthCredentials{
			ClientID: clientID, HostID: pending.hostID, Issuer: s.config.issuerURL,
			RegistrationPending: true, OAuthPreferred: true,
		}
		if err := s.store.SaveProfile(credentialProfile{ID: pending.profileID, Label: pending.profileLabel, Credentials: registration}); err != nil {
			http.Error(w, "The ChatGPT application registration could not be saved on this host.", http.StatusInternalServerError)
			return
		}
	}
	tokens, err := s.exchangeCode(r.Context(), code, clientID, pending.codeVerifier)
	if err != nil {
		logProviderFailure(err)
		http.Error(w, "ChatGPT sign-in could not be completed. Check the server log for a safe diagnostic and retry.", http.StatusBadGateway)
		return
	}
	identity, err := s.verifier.Verify(r.Context(), tokens.IDToken, clientID, pending.nonce)
	if err != nil {
		log.Print("ChatGPT sign-in failed kind=id_token_verification")
		http.Error(w, "ChatGPT sign-in could not be verified. Return to EveryoneAgent and try again.", http.StatusBadGateway)
		return
	}
	if pending.expectedSubject != "" && identity.subject != pending.expectedSubject {
		http.Error(w, "ChatGPT returned a different account than the one previously connected. The existing connection was not changed.", http.StatusForbidden)
		return
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.IDToken == "" || tokens.ExpiresIn <= 0 || !strings.EqualFold(tokens.TokenType, "Bearer") {
		http.Error(w, "ChatGPT did not return the required offline sign-in tokens.", http.StatusBadGateway)
		return
	}
	scopes := strings.Fields(tokens.Scope)
	credentials := oauthCredentials{
		ClientID: clientID, HostID: pending.hostID, AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken,
		IDToken: tokens.IDToken, TokenType: tokens.TokenType, Issuer: s.config.issuerURL, Subject: identity.subject, Scopes: scopes,
		ExpiresAt: s.now().Add(time.Duration(tokens.ExpiresIn) * time.Second), OAuthPreferred: true,
	}
	profile, err := s.store.LoadProfile(pending.profileID)
	if err != nil {
		http.Error(w, "The ChatGPT account could not be loaded on this host.", http.StatusInternalServerError)
		return
	}
	if profile.Label == "" {
		profile.Label = pending.profileLabel
	}
	if profile.Label == "" {
		profile.Label = "ChatGPT account"
	}
	profile.Email = safeProfileLabel(identity.email)
	profile.Credentials = credentials
	if err := s.store.SaveProfile(profile); err != nil {
		http.Error(w, "The ChatGPT connection could not be saved on this host.", http.StatusInternalServerError)
		return
	}
	if pending.activateIfUnset {
		activeID, activeErr := s.store.ActiveProfileID()
		if activeErr != nil {
			http.Error(w, "The ChatGPT account could not be selected on this host.", http.StatusInternalServerError)
			return
		}
		if activeID == "" {
			if err := s.store.SetActiveProfile(pending.profileID); err != nil {
				http.Error(w, "The ChatGPT account could not be selected on this host.", http.StatusInternalServerError)
				return
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'")
	connectionNotice := "ChatGPT sign-in completed, but plan usage is not enabled for this account. Return to EveryoneAgent and choose Continue with ChatGPT to review the permission."
	if hasPlanUsageScope(scopes) {
		connectionNotice = "EveryoneAgent is authorized to use the ChatGPT plan for this household."
	}
	if pending.newRegistration && pending.previousActiveID != "" {
		connectionNotice = "The ChatGPT account was added. The active household account was not changed; choose the new account from the account selector on the EveryoneAgent host to use it."
	}
	_, _ = fmt.Fprintf(w, "<!doctype html><html lang=\"en\"><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>ChatGPT connected</title><body><main><h1>ChatGPT connected</h1><p>%s</p><p>%s</p><p><a href=\"/\">Return to EveryoneAgent</a></p></main></body></html>", html.EscapeString(connectionNotice), html.EscapeString(identity.email))
}

func (s *siwcService) exchangeCode(ctx context.Context, code, clientID, verifier string) (siwcTokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", clientID)
	values.Set("code", code)
	values.Set("code_verifier", verifier)
	values.Set("redirect_uri", s.config.redirectURI)
	values.Set("resource", siwcResource)
	endpoint, err := s.tokenEndpoint(ctx)
	if err != nil {
		return siwcTokenResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return siwcTokenResponse{}, &providerFailure{kind: "oauth_request_build"}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return siwcTokenResponse{}, &providerFailure{kind: "oauth_transport"}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return siwcTokenResponse{}, readSafeOAuthFailure(response, "oauth_token_exchange")
	}
	var tokens siwcTokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&tokens); err != nil {
		return siwcTokenResponse{}, &providerFailure{kind: "oauth_response_decode", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id"))}
	}
	return tokens, nil
}

func (s *siwcService) accessToken(ctx context.Context) (string, error) {
	profileID, err := s.store.ActiveProfileID()
	if err != nil || profileID == "" {
		return "", errChatGPTNotConnected
	}
	return s.accessTokenForProfile(ctx, profileID)
}

func (s *siwcService) accessTokenForProfile(ctx context.Context, profileID string) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	profile, err := s.store.LoadProfile(profileID)
	if errors.Is(err, os.ErrNotExist) {
		return "", errChatGPTNotConnected
	}
	if err != nil {
		return "", &providerFailure{kind: "oauth_credentials"}
	}
	credentials := profile.Credentials
	if !credentialsConnected(credentials) {
		if credentials.OAuthPreferred {
			return "", &providerFailure{kind: "oauth_reauthorize", status: http.StatusUnauthorized, errorCode: "invalid_grant"}
		}
		return "", errChatGPTNotConnected
	}
	if !hasPlanUsageScope(credentials.Scopes) {
		return "", &providerFailure{kind: "oauth_scope_unavailable"}
	}
	if credentials.ExpiresAt.After(s.now().Add(time.Minute)) {
		return credentials.AccessToken, nil
	}
	if credentials.RefreshToken == "" || credentials.ClientID == "" {
		return "", &providerFailure{kind: "oauth_refresh_unavailable"}
	}

	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("client_id", credentials.ClientID)
	values.Set("refresh_token", credentials.RefreshToken)
	values.Set("resource", siwcResource)
	endpoint, err := s.tokenEndpoint(ctx)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return "", &providerFailure{kind: "oauth_refresh_build"}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return "", &providerFailure{kind: "oauth_refresh_transport"}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		refreshErr := readSafeOAuthFailure(response, "oauth_refresh")
		if isTerminalRefreshError(refreshErr) {
			if clearErr := s.clearProfileSessionTokens(profile, true, true, false); clearErr != nil {
				return "", &providerFailure{kind: "oauth_refresh_clear"}
			}
		}
		return "", refreshErr
	}
	var refreshed siwcTokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&refreshed); err != nil {
		return "", &providerFailure{kind: "oauth_refresh_decode", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id"))}
	}
	if refreshed.AccessToken == "" || refreshed.ExpiresIn <= 0 || !strings.EqualFold(refreshed.TokenType, "Bearer") {
		return "", &providerFailure{kind: "oauth_refresh_incomplete", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id"))}
	}
	credentials.AccessToken = refreshed.AccessToken
	credentials.TokenType = refreshed.TokenType
	if refreshed.RefreshToken != "" {
		credentials.RefreshToken = refreshed.RefreshToken
	}
	if refreshed.Scope != "" {
		credentials.Scopes = strings.Fields(refreshed.Scope)
	}
	credentials.ExpiresAt = s.now().Add(time.Duration(refreshed.ExpiresIn) * time.Second)
	profile.Credentials = credentials
	if err := s.store.SaveProfile(profile); err != nil {
		return "", &providerFailure{kind: "oauth_refresh_store"}
	}
	if !hasPlanUsageScope(credentials.Scopes) {
		return "", &providerFailure{kind: "oauth_scope_unavailable"}
	}
	return credentials.AccessToken, nil
}

func readSafeOAuthFailure(response *http.Response, kind string) error {
	var body struct {
		Error     string `json:"error"`
		ErrorType string `json:"error_type"`
		Code      string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&body)
	errorCode := safeDiagnosticToken(body.Code)
	if errorCode == "" && isKnownOAuthErrorCode(body.Error) {
		errorCode = body.Error
	}
	return &providerFailure{
		kind: kind, status: response.StatusCode,
		requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id")),
		errorType: safeDiagnosticToken(body.ErrorType),
		errorCode: errorCode,
	}
}

func isKnownOAuthErrorCode(value string) bool {
	switch value {
	case "invalid_grant", "invalid_client", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	default:
		return false
	}
}

func isTerminalRefreshError(err error) bool {
	var failure *providerFailure
	if !errors.As(err, &failure) {
		return false
	}
	switch failure.errorCode {
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	default:
		return false
	}
}

func containsScope(scopes []string, expected string) bool {
	for _, value := range scopes {
		if value == expected {
			return true
		}
	}
	return false
}
