package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunTotalCaptureBound(t *testing.T) {
	store, err := newAccountStore(t.TempDir() + "/accounts.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	user, _, err := store.register("bounds", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	messages := []chatMessage{{Role: "system", Content: runtimeInstructions()}}
	for i := 0; i < 4; i++ {
		messages = append(messages, chatMessage{Role: "user", Content: strings.Repeat("\x01", 8000)})
	}
	c, err := store.createRun(user, "", "", messages, time.Now(), 2)
	if err != nil {
		t.Fatal(err)
	}
	traceRequest(context.WithValue(context.Background(), runContextKey{}, c), "openai-compatible", strings.Repeat("m", 200000), []byte(`{"model":"bounded request"}`))
	c.finish(strings.Repeat("\x01", 100000), nil)
	var b []byte
	if err = store.db.QueryRow(`SELECT data FROM runs WHERE id=?`, c.run.ID).Scan(&b); err != nil {
		t.Fatal(err)
	}
	if len(b) > 128<<10 {
		t.Fatalf("trace size=%d exceeds 128KiB", len(b))
	}
	var run Run
	json.Unmarshal(b, &run)
	if !run.ResponseTruncated || !run.RequestTruncated || run.Context.OmittedMessages != 2 || run.Context.CaptureOmittedMessages == 0 {
		t.Fatalf("missing truncation disclosure: %#v", run)
	}
}

func TestRunRetentionAndStartupRecovery(t *testing.T) {
	path := t.TempDir() + "/accounts.sqlite"
	store, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := store.register("retention", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	msgs := []chatMessage{{Role: "system", Content: runtimeInstructions()}, {Role: "user", Content: "hello"}}
	old, err := store.createRun(user, "", "", msgs, time.Now().Add(-8*24*time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	var latest *runCapture
	for i := 0; i < 52; i++ {
		latest, err = store.createRun(user, "", "", msgs, time.Now().Add(time.Duration(i)*time.Millisecond), 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	store.db.QueryRow(`SELECT count(*) FROM runs WHERE user_id=?`, user.ID).Scan(&count)
	if count != 50 {
		t.Fatalf("count=%d", count)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM runs WHERE id=?`, old.run.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired run retained")
	}
	traceRequest(context.WithValue(context.Background(), runContextKey{}, latest), "openai-compatible", "model", []byte(`{"model":"model"}`))
	store.db.Close()
	store, err = newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	var b []byte
	store.db.QueryRow(`SELECT data FROM runs WHERE id=?`, latest.run.ID).Scan(&b)
	var run Run
	json.Unmarshal(b, &run)
	if run.State != "interrupted" || run.FinishedAt == nil {
		t.Fatalf("not interrupted: %#v", run)
	}
	for _, event := range run.Events {
		if event.State == "running" {
			t.Fatal("interrupted run has running event")
		}
	}
	// Re-opening is idempotent and does not append another interruption.
	if err = migrateRuns(store.db); err != nil {
		t.Fatal(err)
	}
}

func TestRunRejectsForeignConversationAndDuplicateIDsBeforeProvider(t *testing.T) {
	store, err := newAccountStore(t.TempDir() + "/accounts.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	owner, _, _ := store.register("owner", "correct-horse-battery")
	other, token, _ := store.register("other", "correct-horse-battery")
	conversationID, err := store.saveExchange(owner, "", []chatMessage{{Role: "user", Content: "private"}, {Role: "assistant", Content: "private"}})
	if err != nil {
		t.Fatal(err)
	}
	p := &contextProbe{}
	h := store.middleware(chatHandlerWithStore(p, store))
	cookie := &http.Cookie{Name: authCookieName, Value: token}
	for _, id := range []string{conversationID, "missing"} {
		w := runRequest(h, "POST", "/api/chat", fmt.Sprintf(`{"conversation_id":%q,"messages":[{"role":"user","content":"hello"}]}`, id), cookie)
		if w.Code != 404 || p.messages != nil {
			t.Fatalf("foreign conversation called provider: %d", w.Code)
		}
	}
	c, err := store.createRun(owner, "", "", []chatMessage{{Role: "system", Content: runtimeInstructions()}}, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = other
	w := runRequest(h, "POST", "/api/chat", fmt.Sprintf(`{"run_id":%q,"messages":[{"role":"user","content":"hello"}]}`, c.run.ID), cookie)
	if w.Code != 409 || p.messages != nil {
		t.Fatalf("duplicate reached provider %d", w.Code)
	}
	for _, meta := range []string{`"run_id":"../bad"`, `"omitted_messages":-1`, `"omitted_messages":1000001`} {
		w = runRequest(h, "POST", "/api/chat", `{`+meta+`,"messages":[{"role":"user","content":"hello"}]}`, cookie)
		if w.Code != 400 {
			t.Fatalf("invalid metadata accepted: %s", meta)
		}
	}
}

func TestRunFailureSafeAndUnavailableCaptureDoesNotBlockProvider(t *testing.T) {
	store, err := newAccountStore(t.TempDir() + "/accounts.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	user, token, _ := store.register("failure", "correct-horse-battery")
	p := injectedChatFailureProvider{err: errors.New("private-upstream-secret")}
	h := store.middleware(chatHandlerWithStore(p, store))
	w := runRequest(h, "POST", "/api/chat", `{"messages":[{"role":"user","content":"hello"}]}`, &http.Cookie{Name: authCookieName, Value: token})
	if w.Code != 502 || w.Header().Get("X-Agent-Run-ID") == "" {
		t.Fatalf("failure not correlated: %d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "/api/runs/x", nil)
	r.SetPathValue("id", w.Header().Get("X-Agent-Run-ID"))
	r = r.WithContext(context.WithValue(r.Context(), authContextKey{}, user))
	got := httptest.NewRecorder()
	store.runHandler(got, r)
	run := decodeRunMap(t, got)
	if run["state"] != "failed" || strings.Contains(got.Body.String(), "private-upstream-secret") {
		t.Fatalf("unsafe failure: %s", got.Body)
	}
	// Trace-only failure must not block a successfully generated and saved chat.
	store.db.Exec(`DROP TABLE runs`)
	probe := &contextProbe{}
	h = store.middleware(chatHandlerWithStore(probe, store))
	w = runRequest(h, "POST", "/api/chat", `{"messages":[{"role":"user","content":"hello"}]}`, &http.Cookie{Name: authCookieName, Value: token})
	if w.Code != 200 || probe.messages == nil || w.Header().Get("X-Agent-Run-ID") != "" {
		t.Fatalf("capture failure blocked chat: %d %s", w.Code, w.Body)
	}
}
