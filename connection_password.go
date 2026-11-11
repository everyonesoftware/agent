package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Bound expensive confirmations independently of the cheap per-user limiter.
var connectionPasswordSlots = make(chan struct{}, 2)

type expectedConnectionProfileKey struct{}

type connectionPasswordAttempt struct {
	until time.Time
	count int
}
type connectionPasswordLimiter struct {
	mu       sync.Mutex
	attempts map[string]connectionPasswordAttempt
}

func (l *connectionPasswordLimiter) allow(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.attempts == nil {
		l.attempts = make(map[string]connectionPasswordAttempt)
	}
	for key, attempt := range l.attempts {
		if !now.Before(attempt.until) {
			delete(l.attempts, key)
		}
	}
	attempt, exists := l.attempts[userID]
	if !exists {
		if len(l.attempts) >= 4096 {
			return false
		}
		attempt.until = now.Add(time.Minute)
	}
	if attempt.count >= 10 {
		return false
	}
	attempt.count++
	l.attempts[userID] = attempt
	return true
}

// Production starts are authenticated/local-only; other connection mutations
// require password confirmation. Passwords are never forwarded to providers.
func (s *accountStore) scopedPasswordSIWCHandler(base *siwcService, start bool, handler func(*siwcService, http.ResponseWriter, *http.Request)) http.Handler {
	return s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			writeChatError(w, http.StatusMethodNotAllowed, "Use POST to confirm this connection action.")
			return
		}
		if requestIsCrossSite(r) {
			writeChatError(w, http.StatusForbidden, "Cross-site account requests are not allowed.")
			return
		}
		user, _ := currentAuthUser(r.Context())
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		body := &struct {
			ProfileID               string  `json:"profile_id,omitempty"`
			Password                string  `json:"password"`
			ExpectedActiveProfileID *string `json:"expected_active_profile_id,omitempty"`
		}{}
		mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if start && mediaType == "application/x-www-form-urlencoded" {
			if r.ParseForm() != nil || len(r.PostForm["password"]) != 1 {
				writeChatError(w, http.StatusBadRequest, "Enter your Agent password.")
				return
			}
			for key, values := range r.PostForm {
				if (key != "password" && key != "expected_active_profile_id") || len(values) != 1 {
					writeChatError(w, http.StatusBadRequest, "Enter a valid connection confirmation.")
					return
				}
			}
			body.Password = r.PostForm.Get("password")
			if values, ok := r.PostForm["expected_active_profile_id"]; ok {
				body.ExpectedActiveProfileID = &values[0]
			}
		} else {
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if decoder.Decode(&body) != nil || body == nil || decoder.Decode(new(any)) != io.EOF || (start && body.ProfileID != "") {
				writeChatError(w, http.StatusBadRequest, "Enter a valid connection confirmation.")
				return
			}
		}
		// Starting OAuth is local-only and authenticated, but does not confirm a
		// password. Keep it outside the password limiter and expensive bcrypt gate
		// used for selecting and disconnecting saved connections.
		if start {
			if !isLoopbackHost(r.Host) {
				writeChatError(w, http.StatusForbidden, "ChatGPT authorization requires a browser on the local Agent host because the callback uses loopback.")
				return
			}
			if r.URL.Query().Get("mode") == "" && body.ExpectedActiveProfileID == nil {
				writeChatError(w, http.StatusBadRequest, "Confirm the current ChatGPT account before reconnecting.")
				return
			}
			if body.ExpectedActiveProfileID != nil {
				r = r.WithContext(context.WithValue(r.Context(), expectedConnectionProfileKey{}, *body.ExpectedActiveProfileID))
			}
			r.Body = io.NopCloser(strings.NewReader(""))
			r.Form = nil
			r.PostForm = nil
			handler(s.siwcForUser(base, user), w, r)
			return
		}
		if !s.connectionPasswords.allow(user.ID) {
			w.Header().Set("Retry-After", "60")
			writeChatError(w, http.StatusTooManyRequests, "Too many password confirmations. Try again in a minute.")
			return
		}
		if body.Password == "" || len(body.Password) > 72 {
			writeChatError(w, http.StatusUnauthorized, "Enter your current Agent password.")
			return
		}
		if !start && body.ExpectedActiveProfileID != nil {
			writeChatError(w, http.StatusBadRequest, "Enter a valid connection confirmation.")
			return
		}
		if !start && !validProfileID(body.ProfileID) {
			writeChatError(w, http.StatusBadRequest, "A valid ChatGPT account confirmation is required.")
			return
		}
		select {
		case connectionPasswordSlots <- struct{}{}:
		default:
			w.Header().Set("Retry-After", "1")
			writeChatError(w, http.StatusTooManyRequests, "Password confirmation is busy. Try again.")
			return
		}
		var hash []byte
		err := s.db.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=?`, user.ID).Scan(&hash)
		valid := err == nil && bcrypt.CompareHashAndPassword(hash, []byte(body.Password)) == nil
		<-connectionPasswordSlots
		body.Password = ""
		if !valid {
			writeChatError(w, http.StatusUnauthorized, "Invalid current Agent password.")
			return
		}
		cookie, _ := r.Cookie(authCookieName)
		// Atomically authorize this one request against the current password/session.
		// Release the database before provider I/O: an already-authorized request may
		// finish after logout, but it must not block unrelated users while doing so.
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			writeChatError(w, http.StatusServiceUnavailable, "Password confirmation is unavailable.")
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(r.Context(), `UPDATE users SET password_hash=password_hash WHERE id=? AND password_hash=? AND EXISTS (SELECT 1 FROM sessions WHERE token_hash=? AND user_id=users.id AND expires_at>unixepoch())`, user.ID, hash, hashSession(cookie.Value))
		if err != nil {
			writeChatError(w, http.StatusServiceUnavailable, "Password confirmation is unavailable.")
			return
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			writeChatError(w, http.StatusUnauthorized, "Sign in again and confirm your current Agent password.")
			return
		}
		if err := tx.Commit(); err != nil {
			writeChatError(w, http.StatusServiceUnavailable, "Password confirmation is unavailable.")
			return
		}
		payload, _ := json.Marshal(struct {
			ProfileID string `json:"profile_id"`
		}{body.ProfileID})
		r.Body = io.NopCloser(bytes.NewReader(payload))
		r.Form = nil
		r.PostForm = nil
		handler(s.siwcForUser(base, user), w, r)
	}))
}
