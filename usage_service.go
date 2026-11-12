package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// The usage connection is deliberately independent of SIWC tokens and inference.
// The only credential owner is a private, managed Codex account context.
type usageIdentity struct {
	Email    string `json:"email"`
	PlanType string `json:"plan_type,omitempty"`
}
type usageLogin struct {
	LoginID         string `json:"login_id"`
	VerificationURL string `json:"verification_url"`
	UserCode        string `json:"user_code"`
	ExpiresAt       int64  `json:"expires_at"`
}
type usageWindow struct {
	ID            string  `json:"id"`
	Label         string  `json:"label"`
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes,omitempty"`
	ResetAt       int64   `json:"reset_at,omitempty"`
}
type usageSnapshot struct {
	FetchedAt int64         `json:"fetched_at"`
	Stale     bool          `json:"stale"`
	Windows   []usageWindow `json:"windows"`
}
type usageStatus struct {
	ProfileID     string         `json:"profile_id"`
	Available     bool           `json:"available"`
	State         string         `json:"state"`
	ConnectionID  string         `json:"connection_id"`
	Identity      *usageIdentity `json:"identity,omitempty"`
	IdentityMatch string         `json:"identity_match"`
	Login         *usageLogin    `json:"login,omitempty"`
	Snapshot      *usageSnapshot `json:"snapshot,omitempty"`
	Error         string         `json:"error,omitempty"`
}
type usageRecord struct {
	ConnectionID string `json:"connection_id"`
}
type usageSession struct {
	mu          sync.Mutex
	dir         string
	record      usageRecord
	rpc         usageRPC
	timer       *time.Timer
	workerEpoch uint64
	view        usageStatus
	lastRead    time.Time
}
type usageService struct {
	accounts    *accountStore
	base        *siwcService
	mu          sync.Mutex
	sessions    map[string]*usageSession
	factory     func(context.Context, string, string) (usageRPC, error)
	executable  func() (string, error)
	idleTimeout time.Duration
	now         func() time.Time
	slots       chan struct{}
	starts      connectionPasswordLimiter
	closed      bool
}

func newUsageService(accounts *accountStore, base *siwcService) *usageService {
	return &usageService{accounts: accounts, base: base, sessions: make(map[string]*usageSession), factory: startCodexUsageRPC,
		executable: func() (string, error) {
			name := strings.TrimSpace(os.Getenv("CODEX_USAGE_BINARY"))
			if name == "" {
				name = "codex"
			}
			return exec.LookPath(name)
		},
		idleTimeout: 2 * time.Minute, now: time.Now, slots: make(chan struct{}, 8)}
}
func (s *usageService) close() {
	s.mu.Lock()
	s.closed = true
	all := make([]*usageSession, 0, len(s.sessions))
	for _, entry := range s.sessions {
		all = append(all, entry)
	}
	s.mu.Unlock()
	for _, entry := range all {
		entry.mu.Lock()
		s.stopWorker(entry)
		entry.mu.Unlock()
	}
}
func (s *usageService) session(user authUser, profile string) (*usageSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("usage service closed")
	}
	key := user.ID + ":" + profile
	if entry := s.sessions[key]; entry != nil {
		return entry, nil
	}
	if len(s.sessions) >= 128 {
		return nil, errors.New("usage connection capacity reached")
	}
	dir := filepath.Join(s.accounts.dataDir, "account-data", user.ID, "usage", profile)
	entry := &usageSession{dir: dir, view: usageStatus{ProfileID: profile, State: "disconnected", IdentityMatch: "unverified"}}
	// Refuse symlinked credential parents without creating anything for a mere read.
	if err := checkUsageParents(s.accounts.dataDir, dir); err != nil {
		return nil, err
	}
	record, err := readUsageRecord(filepath.Join(dir, "connection.json"))
	if err == nil {
		entry.record = record
		if entry.record.ConnectionID != "" && !validUsageConnectionID(entry.record.ConnectionID) {
			return nil, errors.New("usage registration invalid")
		}
		entry.view.ConnectionID = entry.record.ConnectionID
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s.sessions[key] = entry
	return entry, nil
}
func validUsageConnectionID(id string) bool {
	if len(id) != 43 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func checkUsageParents(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("invalid usage storage")
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("unsafe usage storage")
		}
	}
	return nil
}
func (s *usageService) saveRecord(entry *usageSession, record usageRecord) error {
	if err := checkUsageParents(s.accounts.dataDir, entry.dir); err != nil {
		return err
	}
	if err := os.MkdirAll(entry.dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(entry.dir, ".connection-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		err = json.NewEncoder(file).Encode(record)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(entry.dir, "connection.json")); err != nil {
		return err
	}
	entry.record = record
	entry.view.ConnectionID = record.ConnectionID
	return nil
}
func (s *usageService) openWorker(ctx context.Context, entry *usageSession) error {
	if entry.rpc != nil {
		return nil
	}
	binary, err := s.executable()
	if err != nil {
		return errors.New("Codex usage runtime is unavailable. Check the Agent deployment.")
	}
	if err = checkUsageParents(s.accounts.dataDir, entry.dir); err != nil {
		return errors.New("Usage credential storage is unavailable.")
	}
	home := filepath.Join(entry.dir, "codex-home")
	if err = checkUsageParents(s.accounts.dataDir, home); err != nil {
		return errors.New("Usage credential storage is unavailable.")
	}
	if err = os.MkdirAll(home, 0700); err != nil {
		return errors.New("Usage credential storage is unavailable.")
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return errors.New("Usage service is busy. Try again later.")
	}
	rpc, err := s.factory(ctx, binary, home)
	if err != nil {
		<-s.slots
		return errors.New("Codex usage service could not start. Your conversation connection was kept.")
	}
	entry.rpc = rpc
	return nil
}
func (s *usageService) stopWorker(entry *usageSession) {
	entry.workerEpoch++
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	if entry.rpc != nil {
		_ = entry.rpc.Close()
		entry.rpc = nil
		<-s.slots
	}
}
func (s *usageService) touchWorker(entry *usageSession) {
	if entry.rpc == nil {
		return
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.workerEpoch++
	epoch := entry.workerEpoch
	delay := s.idleTimeout
	if entry.view.Login != nil {
		if remaining := time.Unix(entry.view.Login.ExpiresAt, 0).Sub(s.now()); remaining > delay {
			delay = remaining
		}
	}
	entry.timer = time.AfterFunc(delay, func() {
		entry.mu.Lock()
		defer entry.mu.Unlock()
		if entry.workerEpoch == epoch {
			s.stopWorker(entry)
		}
	})
}
func (s *usageService) call(ctx context.Context, entry *usageSession, method string, params, result any) error {
	if err := s.openWorker(ctx, entry); err != nil {
		return err
	}
	if err := entry.rpc.Call(ctx, method, params, result); err != nil {
		s.stopWorker(entry)
		return errors.New("Codex usage service could not complete this request. Your conversation connection was kept.")
	}
	s.touchWorker(entry)
	return nil
}
func (s *usageService) view(entry *usageSession) usageStatus {
	result := entry.view
	_, err := s.executable()
	result.Available = err == nil
	if result.Snapshot != nil {
		copySnapshot := *result.Snapshot
		copySnapshot.Windows = append([]usageWindow(nil), result.Snapshot.Windows...)
		if s.now().Unix()-copySnapshot.FetchedAt > 60 {
			copySnapshot.Stale = true
		}
		for _, window := range copySnapshot.Windows {
			if window.ResetAt > 0 && window.ResetAt <= s.now().Unix() {
				copySnapshot.Stale = true
			}
		}
		result.Snapshot = &copySnapshot
	}
	return result
}
func (s *usageService) status(ctx context.Context, entry *usageSession, email string) {
	if entry.record.ConnectionID == "" {
		return
	}
	if entry.view.Login != nil && s.now().Unix() >= entry.view.Login.ExpiresAt {
		// Stop polling an expired ceremony; never silently start a fresh login.
		s.stopWorker(entry)
		entry.view.Login = nil
		entry.view.State = "expired"
		entry.view.Error = "The usage sign-in request expired. Start sign-in again."
		return
	}
	if entry.view.State == "connected" && entry.view.Login == nil && s.now().Sub(entry.lastRead) < time.Minute {
		return
	}
	var result struct {
		Account *struct {
			Type     string `json:"type"`
			Email    string `json:"email"`
			PlanType string `json:"planType"`
		} `json:"account"`
	}
	if err := s.call(ctx, entry, "account/read", map[string]bool{"refreshToken": false}, &result); err != nil {
		entry.view.State = "unavailable"
		entry.view.Login = nil
		entry.view.Error = err.Error()
		if entry.view.Snapshot != nil {
			entry.view.Snapshot.Stale = true
		}
		return
	}
	entry.lastRead = s.now()
	if result.Account == nil {
		if entry.view.Login == nil {
			entry.view.State = "expired"
			entry.view.Identity = nil
			entry.view.Snapshot = nil
			entry.view.Error = "Sign in again to see allowance. Your conversation connection is unchanged."
		}
		return
	}
	if result.Account.Type != "chatgpt" {
		entry.view.State = "unavailable"
		entry.view.Error = "The usage connection must use Codex-managed ChatGPT sign-in."
		entry.view.Identity = nil
		entry.view.Snapshot = nil
		return
	}
	wasPending := entry.view.Login != nil
	entry.view.Identity = &usageIdentity{Email: safeProfileLabel(result.Account.Email), PlanType: safeProfileLabel(result.Account.PlanType)}
	entry.view.Login = nil
	entry.view.State = "connected"
	entry.view.Error = ""
	entry.view.IdentityMatch = "unverified"
	if email != "" && result.Account.Email != "" && !strings.EqualFold(strings.TrimSpace(email), strings.TrimSpace(result.Account.Email)) {
		entry.view.IdentityMatch = "different"
		entry.view.Snapshot = nil
		return
	}
	if wasPending || entry.view.Snapshot == nil {
		s.refreshQuota(ctx, entry)
	}
}
func (s *usageService) refreshQuota(ctx context.Context, entry *usageSession) {
	if entry.view.Identity == nil || entry.view.IdentityMatch == "different" {
		return
	}
	var raw usageRateLimits
	if err := s.call(ctx, entry, "account/rateLimits/read", struct{}{}, &raw); err != nil {
		entry.view.Error = err.Error()
		if entry.view.Snapshot != nil {
			entry.view.Snapshot.Stale = true
		} else {
			entry.view.State = "unavailable"
		}
		return
	}
	windows := normalizeUsageWindows(raw)
	if len(windows) == 0 {
		entry.view.Error = "No quota windows were returned for this usage account."
		if entry.view.Snapshot != nil {
			entry.view.Snapshot.Stale = true
		}
		return
	}
	entry.view.State = "connected"
	entry.view.Error = ""
	entry.view.Snapshot = &usageSnapshot{FetchedAt: s.now().Unix(), Windows: windows}
}

type usageRateWindow struct {
	UsedPercent *float64 `json:"usedPercent"`
	Minutes     *int64   `json:"windowDurationMins"`
	ResetAt     *int64   `json:"resetsAt"`
}
type usageRateBucket struct {
	LimitID   string           `json:"limitId"`
	LimitName string           `json:"limitName"`
	Primary   *usageRateWindow `json:"primary"`
	Secondary *usageRateWindow `json:"secondary"`
}
type usageRateLimits struct {
	RateLimits *usageRateBucket           `json:"rateLimits"`
	ByID       map[string]usageRateBucket `json:"rateLimitsByLimitId"`
}

func normalizeUsageWindows(raw usageRateLimits) []usageWindow {
	buckets := raw.ByID
	if len(buckets) == 0 && raw.RateLimits != nil {
		id := raw.RateLimits.LimitID
		if id == "" {
			id = "codex"
		}
		buckets = map[string]usageRateBucket{id: *raw.RateLimits}
	}
	keys := make([]string, 0, len(buckets))
	for id := range buckets {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	var out []usageWindow
	for _, id := range keys {
		bucket := buckets[id]
		label := safeProfileLabel(bucket.LimitName)
		if label == "" {
			label = safeProfileLabel(id)
		}
		for _, item := range []struct {
			name   string
			window *usageRateWindow
		}{{"primary", bucket.Primary}, {"secondary", bucket.Secondary}} {
			w := item.window
			if w == nil || w.UsedPercent == nil || math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0 || *w.UsedPercent > 100 {
				continue
			}
			window := usageWindow{ID: id + ":" + item.name, Label: label + " · " + item.name + " window", UsedPercent: *w.UsedPercent}
			if w.Minutes != nil && *w.Minutes > 0 && *w.Minutes <= 525600 {
				window.WindowMinutes = *w.Minutes
				window.Label = label + " · " + usageWindowLabel(*w.Minutes)
			}
			if w.ResetAt != nil && *w.ResetAt > 0 && *w.ResetAt <= 253402300799 {
				window.ResetAt = *w.ResetAt
			}
			out = append(out, window)
			if len(out) >= 32 {
				return out
			}
		}
	}
	return out
}
func usageWindowLabel(minutes int64) string {
	if minutes == 10080 {
		return "weekly window"
	}
	if minutes%1440 == 0 {
		return fmt.Sprintf("%d-day window", minutes/1440)
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("%d-hour window", minutes/60)
	}
	return fmt.Sprintf("%d-minute window", minutes)
}
func validUsageVerificationURL(value string) bool {
	return value == "https://auth.openai.com/codex/device"
}
func (s *usageService) connect(ctx context.Context, entry *usageSession) error {
	if entry.view.Login != nil {
		return errors.New("A usage sign-in is already awaiting approval. Cancel it first.")
	}
	// Logout/clear the prior context rather than letting a different login's quota linger.
	if entry.record.ConnectionID != "" {
		if err := s.clear(ctx, entry); err != nil {
			return err
		}
	}
	// A tombstoned or never-registered context must never import leftover auth.
	s.stopWorker(entry)
	home := filepath.Join(entry.dir, "codex-home")
	if err := checkUsageParents(s.accounts.dataDir, home); err != nil {
		return errors.New("Usage credential storage is unavailable.")
	}
	if err := os.RemoveAll(home); err != nil {
		return errors.New("Usage credential storage is unavailable.")
	}
	id, err := randomURLToken(32)
	if err != nil {
		return errors.New("Usage sign-in could not be prepared.")
	}
	if err = s.saveRecord(entry, usageRecord{ConnectionID: id}); err != nil {
		return errors.New("Usage credential storage is unavailable.")
	}
	var result struct {
		Type    string `json:"type"`
		LoginID string `json:"loginId"`
		URL     string `json:"verificationUrl"`
		Code    string `json:"userCode"`
	}
	if err = s.call(ctx, entry, "account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &result); err != nil {
		return err
	}
	if result.Type != "chatgptDeviceCode" || result.LoginID == "" || len(result.LoginID) > 256 || result.Code == "" || len(result.Code) > 128 || !validUsageVerificationURL(result.URL) {
		s.stopWorker(entry)
		return errors.New("Codex returned an invalid sign-in challenge. Try again later.")
	}
	entry.view = usageStatus{ProfileID: entry.view.ProfileID, State: "pending", ConnectionID: id, IdentityMatch: "unverified", Login: &usageLogin{LoginID: result.LoginID, VerificationURL: result.URL, UserCode: result.Code, ExpiresAt: s.now().Add(10 * time.Minute).Unix()}}
	s.touchWorker(entry)
	return nil
}
func (s *usageService) clear(ctx context.Context, entry *usageSession) error {
	// Persist the tombstone before dropping tokens: a restart must not adopt leftover
	// credentials if local cleanup or the provider request fails halfway through.
	if err := s.saveRecord(entry, usageRecord{}); err != nil {
		return errors.New("Usage connection could not be removed from storage.")
	}
	warning := "Usage disconnected locally. Manage access in ChatGPT if you also want to revoke remote authorization."
	entry.view = usageStatus{ProfileID: entry.view.ProfileID, State: "disconnected", IdentityMatch: "unverified", Error: warning}
	if entry.rpc != nil {
		var result struct{}
		if err := entry.rpc.Call(ctx, "account/logout", struct{}{}, &result); err != nil {
			warning = "Usage disconnected locally. Remote revocation was not confirmed; manage access in ChatGPT if needed."
		}
	}
	s.stopWorker(entry)
	home := filepath.Join(entry.dir, "codex-home")
	if err := checkUsageParents(s.accounts.dataDir, home); err != nil {
		return errors.New("Usage connection disabled, but credential cleanup needs attention.")
	}
	if err := os.RemoveAll(home); err != nil {
		return errors.New("Usage connection disabled, but credential cleanup failed. Check storage permissions.")
	}
	entry.view = usageStatus{ProfileID: entry.view.ProfileID, State: "disconnected", IdentityMatch: "unverified", Error: warning}
	return nil
}
func (s *usageService) confirmPassword(w http.ResponseWriter, r *http.Request, user authUser, password string) bool {
	if !s.accounts.connectionPasswords.allow(user.ID) {
		w.Header().Set("Retry-After", "60")
		writeChatError(w, 429, "Too many password confirmations. Try again in a minute.")
		return false
	}
	if password == "" || len(password) > 72 {
		writeChatError(w, 401, "Enter your current Agent password.")
		return false
	}
	select {
	case connectionPasswordSlots <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "1")
		writeChatError(w, 429, "Password confirmation is busy. Try again.")
		return false
	}
	var hash []byte
	err := s.accounts.db.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=?`, user.ID).Scan(&hash)
	valid := err == nil && bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
	<-connectionPasswordSlots
	if !valid {
		writeChatError(w, 401, "Invalid current Agent password.")
		return false
	}
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		writeChatError(w, 401, "Sign in again.")
		return false
	}
	tx, err := s.accounts.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeChatError(w, 503, "Password confirmation is unavailable.")
		return false
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE users SET password_hash=password_hash WHERE id=? AND password_hash=? AND EXISTS (SELECT 1 FROM sessions WHERE token_hash=? AND user_id=users.id AND expires_at>unixepoch())`, user.ID, hash, hashSession(cookie.Value))
	if err != nil {
		writeChatError(w, 503, "Password confirmation is unavailable.")
		return false
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		writeChatError(w, 401, "Sign in again and confirm your current Agent password.")
		return false
	}
	if tx.Commit() != nil {
		writeChatError(w, 503, "Password confirmation is unavailable.")
		return false
	}
	return true
}

// Limit the persisted registration read as well as JSON decoding: credential
// metadata is server-owned, but malformed/oversized files must fail closed.
func readUsageRecord(path string) (usageRecord, error) {
	var record usageRecord
	info, err := os.Lstat(path)
	if err != nil {
		return record, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024 {
		return record, errors.New("invalid usage registration")
	}
	file, err := os.Open(path)
	if err != nil {
		return record, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1025))
	err = decoder.Decode(&record)
	if err == nil && decoder.Decode(new(any)) != io.EOF {
		err = errors.New("invalid usage registration")
	}
	return record, err
}
