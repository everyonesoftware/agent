package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"

	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const authCookieName = "everyoneagent_session"

type authUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type authContextKey struct{}

type accountStore struct {
	db            *sql.DB
	dataDir       string
	oauthMu       sync.Mutex
	oauthServices map[string]*siwcService
}

func newAccountStore(path string) (*accountStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// Respect existing host directory/file permissions. Create a new database
	// privately before SQLite opens it, without truncating an existing database.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_foreign_keys=on&_journal_mode=DELETE&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, password_hash BLOB NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sessions (token_hash BLOB PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS conversations (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, title TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS conversations_user_updated ON conversations(user_id, updated_at DESC);
CREATE TABLE IF NOT EXISTS messages (id INTEGER PRIMARY KEY AUTOINCREMENT, conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, role TEXT NOT NULL CHECK(role IN ('user','assistant')), content TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS messages_conversation ON messages(user_id, conversation_id, id);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &accountStore{db: db, dataDir: filepath.Dir(path), oauthServices: make(map[string]*siwcService)}, nil
}

func (s *accountStore) siwcForUser(base *siwcService, user authUser) *siwcService {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	if service := s.oauthServices[user.ID]; service != nil {
		return service
	}
	dir := filepath.Join(s.dataDir, "account-data", user.ID, "oauth")

	_ = os.MkdirAll(dir, 0700)
	service := newSIWCService(base.config, newFileCredentialStore(dir))
	s.oauthServices[user.ID] = service
	return service
}

func (s *accountStore) scopedSIWCHandler(base *siwcService, handler func(*siwcService, http.ResponseWriter, *http.Request)) http.Handler {
	return s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestIsCrossSite(r) {
			writeChatError(w, http.StatusForbidden, "Cross-site account requests are not allowed.")
			return
		}
		user, ok := currentAuthUser(r.Context())
		if !ok {
			writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
			return
		}
		handler(s.siwcForUser(base, user), w, r)
	}))
}

func (s *accountStore) scopedLocalSIWCHandler(base *siwcService, handler func(*siwcService, http.ResponseWriter, *http.Request)) http.Handler {
	protected := s.scopedSIWCHandler(base, handler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) || requestIsCrossSite(r) {
			http.Error(w, "Manage ChatGPT accounts from a browser on the Agent host.", http.StatusForbidden)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

func (s *accountStore) scopedOAuthCallbackHandler(base *siwcService, handler func(*siwcService, http.ResponseWriter, *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "Sign-in callbacks are accepted only on the local Agent host.", http.StatusForbidden)
			return
		}
		s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := currentAuthUser(r.Context())
			if !ok {
				writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
				return
			}
			handler(s.siwcForUser(base, user), w, r)
		})).ServeHTTP(w, r)
	})
}

func (s *accountStore) register(username, password string) (authUser, string, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 || strings.ContainsAny(username, " \t\r\n") || len(password) < 12 || len(password) > 72 {
		return authUser{}, "", errors.New("invalid account details")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return authUser{}, "", err
	}
	id, err := randomURLToken(24)
	if err != nil {
		return authUser{}, "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return authUser{}, "", err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO users(id,username,password_hash,created_at) VALUES(?,?,?,?)`, id, username, hash, time.Now().Unix()); err != nil {
		return authUser{}, "", err
	}
	token, err := randomURLToken(32)
	if err != nil {
		return authUser{}, "", err
	}
	if err = insertSession(tx, id, token); err != nil {
		return authUser{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return authUser{}, "", err
	}
	return authUser{ID: id, Username: username}, token, nil
}

func (s *accountStore) login(username, password string) (authUser, string, error) {
	var user authUser
	var hash []byte
	err := s.db.QueryRow(`SELECT id, username, password_hash FROM users WHERE username = ? COLLATE NOCASE`, strings.TrimSpace(username)).Scan(&user.ID, &user.Username, &hash)
	if err != nil || bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil {
		return authUser{}, "", errors.New("invalid username or password")
	}
	token, err := randomURLToken(32)
	if err != nil {
		return authUser{}, "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return authUser{}, "", err
	}
	defer tx.Rollback()
	// Compare and insert under the same SQLite writer lock: a password change
	// committed after bcrypt verification must not create a surviving session.
	result, err := tx.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) SELECT ?,id,? FROM users WHERE id=? AND password_hash=?`, hashSession(token), time.Now().Add(30*24*time.Hour).Unix(), user.ID, hash)
	if err != nil {
		return authUser{}, "", err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return authUser{}, "", err
	}
	if count != 1 {
		return authUser{}, "", errors.New("invalid username or password")
	}
	if err = tx.Commit(); err != nil {
		return authUser{}, "", err
	}
	return user, token, nil
}

func insertSession(tx *sql.Tx, userID, token string) error {
	_, err := tx.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, hashSession(token), userID, time.Now().Add(30*24*time.Hour).Unix())
	return err
}

func hashSession(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }

func (s *accountStore) userForToken(token string) (authUser, error) {
	var user authUser
	err := s.db.QueryRow(`SELECT u.id,u.username FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, hashSession(token), time.Now().Unix()).Scan(&user.ID, &user.Username)
	return user, err
}

func (s *accountStore) logout(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hashSession(token))
	return err
}

func setSessionCookie(w http.ResponseWriter, token string, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(30 * 24 * time.Hour), MaxAge: 30 * 24 * 60 * 60})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: "", Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (s *accountStore) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(authCookieName)
		if err != nil || cookie.Value == "" {
			writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
			return
		}
		user, err := s.userForToken(cookie.Value)
		if err != nil {
			clearSessionCookie(w, r)
			writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, user)))
	})
}

func currentAuthUser(ctx context.Context) (authUser, bool) {
	user, ok := ctx.Value(authContextKey{}).(authUser)
	return user, ok && user.ID != ""
}

func (s *accountStore) registerHandler(w http.ResponseWriter, r *http.Request) {
	s.accountHandler(w, r, true)
}
func (s *accountStore) loginHandler(w http.ResponseWriter, r *http.Request) {
	s.accountHandler(w, r, false)
}
func (s *accountStore) accountHandler(w http.ResponseWriter, r *http.Request, register bool) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if requestIsCrossSite(r) {
		writeChatError(w, http.StatusForbidden, "Cross-site account requests are not allowed.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		writeChatError(w, http.StatusBadRequest, "Enter a username and password.")
		return
	}
	var user authUser
	var token string
	var err error
	if register {
		user, token, err = s.register(body.Username, body.Password)
	} else {
		user, token, err = s.login(body.Username, body.Password)
	}
	if err != nil {
		if !register {
			writeChatError(w, http.StatusUnauthorized, "Invalid username or password.")
			return
		}
		writeChatError(w, http.StatusBadRequest, "Choose a username (3–64 characters, no spaces) and a password (12–72 bytes). The username may already be taken.")
		return
	}
	setSessionCookie(w, token, r)
	status := http.StatusOK
	if register {
		status = http.StatusCreated
	}
	writeChatJSON(w, status, map[string]string{"id": user.ID, "username": user.Username})
}

func (s *accountStore) passwordChangeHandler(w http.ResponseWriter, r *http.Request) {
	if requestIsCrossSite(r) {
		writeChatError(w, http.StatusForbidden, "Cross-site account requests are not allowed.")
		return
	}
	user, ok := currentAuthUser(r.Context())
	if !ok {
		writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.CurrentPassword == "" || utf8.RuneCountInString(body.NewPassword) < 12 || len(body.NewPassword) > 72 {
		writeChatError(w, http.StatusBadRequest, "Enter your current password and a new password of at least 12 characters and at most 72 bytes.")
		return
	}
	var hash []byte
	if s.db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, user.ID).Scan(&hash) != nil || bcrypt.CompareHashAndPassword(hash, []byte(body.CurrentPassword)) != nil {
		writeChatError(w, http.StatusUnauthorized, "Invalid current password.")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "Password could not be changed.")
		return
	}
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
		return
	}
	token, err := s.replacePassword(user.ID, hash, newHash, cookie.Value)
	if errors.Is(err, sql.ErrNoRows) {
		writeChatError(w, http.StatusUnauthorized, "Sign in again before changing your password.")
		return
	}
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "Password could not be changed.")
		return
	}
	setSessionCookie(w, token, r)
	writeChatJSON(w, http.StatusOK, map[string]bool{"password_changed": true})
}

// The conditional UPDATE is the first transaction statement, so SQLite acquires
// its writer lock before comparing the verified hash and the active session.
// Concurrent password changes cannot reuse an earlier password verification.
func (s *accountStore) replacePassword(userID string, verifiedHash, newHash []byte, currentToken string) (string, error) {
	token, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE users SET password_hash=? WHERE id=? AND password_hash=? AND EXISTS (SELECT 1 FROM sessions WHERE token_hash=? AND user_id=users.id AND expires_at>?)`, newHash, userID, verifiedHash, hashSession(currentToken), time.Now().Unix())
	if err != nil {
		return "", err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", sql.ErrNoRows
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return "", err
	}
	if err = insertSession(tx, userID, token); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *accountStore) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if requestIsCrossSite(r) {
		writeChatError(w, http.StatusForbidden, "Cross-site account requests are not allowed.")
		return
	}
	if cookie, err := r.Cookie(authCookieName); err == nil {
		_ = s.logout(cookie.Value)
	}
	clearSessionCookie(w, r)
	writeChatJSON(w, http.StatusOK, map[string]bool{"logged_out": true})
}

func (s *accountStore) meHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := currentAuthUser(r.Context())
	if !ok {
		writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
		return
	}
	writeChatJSON(w, http.StatusOK, map[string]string{"id": user.ID, "username": user.Username})
}

type savedConversation struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updated_at"`
}

type savedMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}

func (s *accountStore) saveExchange(user authUser, conversationID string, messages []chatMessage) (string, error) {
	now := time.Now().Unix()
	newConversation := conversationID == ""
	if newConversation {
		var err error
		conversationID, err = randomURLToken(24)
		if err != nil {
			return "", err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if newConversation {
		title := strings.TrimSpace(messages[len(messages)-1].Content)
		if len(title) > 120 {
			title = title[:120]
		}
		if _, err = tx.Exec(`INSERT INTO conversations(id,user_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`, conversationID, user.ID, title, now, now); err != nil {
			return "", err
		}
	} else {
		result, updateErr := tx.Exec(`UPDATE conversations SET updated_at=? WHERE id=? AND user_id=?`, now, conversationID, user.ID)
		if updateErr != nil {
			return "", updateErr
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return "", errors.New("conversation not found")
		}
	}
	for _, message := range messages {
		if _, err = tx.Exec(`INSERT INTO messages(conversation_id,user_id,role,content,created_at) VALUES(?,?,?,?,?)`, conversationID, user.ID, message.Role, message.Content, now); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return conversationID, nil
}

func (s *accountStore) listConversations(w http.ResponseWriter, r *http.Request) {
	user, _ := currentAuthUser(r.Context())
	rows, err := s.db.Query(`SELECT id,title,updated_at FROM conversations WHERE user_id=? ORDER BY updated_at DESC LIMIT 100`, user.ID)
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "Conversations could not be loaded.")
		return
	}
	defer rows.Close()
	items := make([]savedConversation, 0)
	for rows.Next() {
		var item savedConversation
		if rows.Scan(&item.ID, &item.Title, &item.UpdatedAt) != nil {
			writeChatError(w, http.StatusInternalServerError, "Conversations could not be loaded.")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeChatError(w, http.StatusInternalServerError, "Conversations could not be loaded.")
		return
	}
	writeChatJSON(w, http.StatusOK, map[string]any{"conversations": items})
}

func (s *accountStore) conversationMessages(w http.ResponseWriter, r *http.Request) {
	user, _ := currentAuthUser(r.Context())
	id := r.PathValue("id")
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM conversations WHERE id=? AND user_id=?`, id, user.ID).Scan(&exists); err != nil {
		writeChatError(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	rows, err := s.db.Query(`SELECT role,content,created_at FROM messages WHERE conversation_id=? AND user_id=? ORDER BY id`, id, user.ID)
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "Conversation could not be loaded.")
		return
	}
	defer rows.Close()
	messages := make([]savedMessage, 0)
	for rows.Next() {
		var m savedMessage
		if rows.Scan(&m.Role, &m.Content, &m.CreatedAt) != nil {
			writeChatError(w, http.StatusInternalServerError, "Conversation could not be loaded.")
			return
		}
		messages = append(messages, m)
	}
	writeChatJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

func (s *accountStore) deleteConversation(w http.ResponseWriter, r *http.Request) {
	user, _ := currentAuthUser(r.Context())
	tx, err := s.db.Begin()
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "Conversation could not be deleted.")
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM messages WHERE conversation_id=? AND user_id=?`, r.PathValue("id"), user.ID); err != nil {
		writeChatError(w, http.StatusInternalServerError, "Conversation could not be deleted.")
		return
	}
	result, err := tx.Exec(`DELETE FROM conversations WHERE id=? AND user_id=?`, r.PathValue("id"), user.ID)
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "Conversation could not be deleted.")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeChatError(w, http.StatusNotFound, "Conversation not found.")
		return
	}

	if err = tx.Commit(); err != nil {
		writeChatError(w, http.StatusInternalServerError, "Conversation could not be deleted.")
		return
	}
	writeChatJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
