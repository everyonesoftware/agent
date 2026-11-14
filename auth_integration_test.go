package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConversationMessagesAreIsolatedBetweenAccounts(t *testing.T) {
	store, err := newAccountStore(t.TempDir() + "/users.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := store.register("alice", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := store.register("bob", "another-correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.saveExchange(a, "", []chatMessage{{Role: "user", Content: "private question"}, {Role: "assistant", Content: "private answer"}})
	if err != nil {
		t.Fatal(err)
	}
	handler := store.middleware(http.HandlerFunc(store.conversationMessages))
	request := httptest.NewRequest(http.MethodGet, "/api/conversations/"+id, nil)
	request.SetPathValue("id", id)
	request.AddCookie(&http.Cookie{Name: authCookieName, Value: tokenForTest(t, store, b.ID)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("other account conversation status = %d, want 404", response.Code)
	}
}

func TestNewConversationTitleUsesFirstUserRequest(t *testing.T) {
	store, err := newAccountStore(t.TempDir() + "/users.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := store.register("alice", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.saveExchange(user, "", []chatMessage{
		{Role: "user", Content: "How can I organize my weekly notes?"},
		{Role: "assistant", Content: "Here are a few ways to organize your notes."},
	})
	if err != nil {
		t.Fatal(err)
	}
	var title string
	if err := store.db.QueryRow(`SELECT title FROM conversations WHERE id=?`, id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "How can I organize my weekly notes?" {
		t.Fatalf("saved title = %q, want first user request", title)
	}
}

func TestNewConversationTitleSummarizesOpeningLine(t *testing.T) {
	store, err := newAccountStore(t.TempDir() + "/users.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := store.register("alice", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.saveExchange(user, "", []chatMessage{
		{Role: "user", Content: "  Plan   a quieter week with café notes 🌿  \n\nConstraints: keep meetings in the afternoon."},
		{Role: "assistant", Content: "Let's do it."},
	})
	if err != nil {
		t.Fatal(err)
	}
	var title string
	if err := store.db.QueryRow(`SELECT title FROM conversations WHERE id=?`, id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Plan a quieter week with café notes 🌿" {
		t.Fatalf("saved title = %q, want a concise opening-line title", title)
	}
}

func TestNewConversationTitleStopsAtWholeWord(t *testing.T) {
	store, err := newAccountStore(t.TempDir() + "/users.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := store.register("alice", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.saveExchange(user, "", []chatMessage{
		{Role: "user", Content: strings.Repeat("Planning ", 15)},
		{Role: "assistant", Content: "Sure."},
	})
	if err != nil {
		t.Fatal(err)
	}
	var title string
	if err := store.db.QueryRow(`SELECT title FROM conversations WHERE id=?`, id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(strings.Repeat("Planning ", 9))
	if title != want {
		t.Fatalf("saved title = %q, want %q", title, want)
	}
}

func TestNewConversationTitleTruncatesUnicodeSafely(t *testing.T) {
	got := requestTitle(strings.Repeat("界", 90))
	want := strings.Repeat("界", 80)
	if got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
}

func TestLoginRejectsWrongPasswordAndAcceptsCorrectPassword(t *testing.T) {
	t.Setenv("APP_DATA_DIR", t.TempDir())
	handler := newServer()
	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"username":"alice","password":"correct-horse-battery"}`))
	registerResponse := httptest.NewRecorder()
	handler.ServeHTTP(registerResponse, register)
	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", registerResponse.Code, registerResponse.Body.String())
	}
	wrong := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"incorrect-password"}`))
	wrongResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongResponse, wrong)
	if wrongResponse.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password login status=%d, want 401", wrongResponse.Code)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"correct-horse-battery"}`))
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK || len(loginResponse.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
}

func tokenForTest(t *testing.T, store *accountStore, userID string) string {
	t.Helper()
	token, err := randomURLToken(32)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,strftime('%s','now')+3600)`, hashSession(token), userID)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
