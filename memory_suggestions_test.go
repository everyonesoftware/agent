package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemorySuggestionsCompatibleIsolation(t *testing.T) {
	s, path, a, b := memoryFixture(t)
	if _, err := s.saveMemory(a, "user", "Prefer concise replies."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.saveMemory(b, "user", "FOREIGN PRIVATE MEMORY"); err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
	}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"suggestions":[" prefer   CONCISE replies. ","Uses Go.","uses go."]}`}}}, "usage": map[string]int{"prompt_tokens": 11, "completion_tokens": 3, "total_tokens": 14}})
	}))
	defer upstream.Close()
	t.Setenv("APP_DATA_DIR", filepath.Dir(path))
	t.Setenv("LLM_BASE_URL", upstream.URL)
	t.Setenv("LLM_MODEL", "fixture")
	h := newServer()
	w := memoryRequest(t, h, a, "POST", "/api/memory-suggestions", `{"message":"I use Go.","model":"selected"}`)
	if w.Code != 200 {
		t.Fatalf("suggestions %d %s", w.Code, w.Body)
	}
	var result struct {
		Suggestions []struct {
			Content      string `json:"content"`
			AlreadySaved bool   `json:"already_saved"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 2 || !result.Suggestions[0].AlreadySaved || result.Suggestions[1].AlreadySaved {
		t.Fatalf("result %+v", result)
	}
	if calls != 1 || wire.Model != "fixture" || len(wire.Messages) != 2 || wire.Messages[0].Role != "system" || wire.Messages[1] != (chatMessage{Role: "user", Content: "I use Go."}) {
		t.Fatalf("wire %+v calls %d", wire, calls)
	}
	encoded, _ := json.Marshal(wire)
	if strings.Contains(string(encoded), "concise replies") || strings.Contains(string(encoded), "FOREIGN") {
		t.Fatal("saved memories disclosed")
	}
	snap, err := s.memorySnapshot(a)
	if err != nil || len(snap.Entries) != 1 {
		t.Fatalf("writes %+v %v", snap, err)
	}
	var conversations int
	if err := s.db.QueryRow(`SELECT count(*) FROM conversations`).Scan(&conversations); err != nil || conversations != 0 {
		t.Fatalf("conversation history written: %d %v", conversations, err)
	}
	id := w.Header().Get("X-Agent-Run-ID")
	if id == "" {
		t.Fatal("missing real call trace")
	}
	var data []byte
	if err := s.db.QueryRow(`SELECT data FROM runs WHERE id=? AND user_id=?`, id, a.ID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		t.Fatal(err)
	}
	if run.Provider != "openai-compatible" || run.Request == nil || run.Usage == nil || run.Usage.TotalTokens != 14 || run.Context.Memory != nil || run.ConversationID != "" {
		t.Fatalf("trace %+v", run)
	}
}

func TestMemorySuggestionAndBatchBoundaries(t *testing.T) {
	_, h, a, _ := memoryHTTPFixture(t)
	for _, path := range []string{"/api/memory-suggestions", "/api/memories/batch"} {
		if w := memoryRequest(t, h, authUser{}, "POST", path, `{}`); w.Code != 401 {
			t.Fatalf("auth %s %d", path, w.Code)
		}
		if w := memoryRequest(t, h, a, "POST", path, `{}`, "Origin", "https://evil.example"); w.Code != 403 {
			t.Fatalf("origin %s %d", path, w.Code)
		}
		for _, body := range []string{`{} {}`, `{"unknown":true}`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}), strings.Repeat(" ", maxMemoryRequestBytes+1)} {
			if w := memoryRequest(t, h, a, "POST", path, body); w.Code != 400 {
				t.Fatalf("strict %s %d %s", path, w.Code, w.Body)
			}
		}
	}
	for _, body := range []string{`{}`, `{"message":" "}`, `{"message":"ok","model":"` + strings.Repeat("x", 257) + `"}`, `{"message":"` + strings.Repeat("x", maxChatMessageBytes+1) + `"}`, `{"message":null}`, `{"message":"ok","model":null}`} {
		if w := memoryRequest(t, h, a, "POST", "/api/memory-suggestions", body); w.Code != 400 {
			t.Fatalf("invalid %d %s", w.Code, w.Body)
		}
	}
}

func TestMemoryBatchAtomicReplayAndIsolation(t *testing.T) {
	s, h, a, b := memoryHTTPFixture(t)
	old, err := s.saveMemory(a, "project", "Uses Go.")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"entries":[{"category":"user","content":"uses   GO."},{"category":"project","content":"Uses SQLite."},{"category":"user","content":"USES SQLite."}]}`
	for i := 0; i < 2; i++ {
		w := memoryRequest(t, h, a, "POST", "/api/memories/batch", body)
		var result struct {
			Entries    []MemoryEntry `json:"entries"`
			CreatedIDs []string      `json:"created_ids"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		wantCreated := 1
		if i == 1 {
			wantCreated = 0
		}
		if w.Code != 200 || len(result.Entries) != 2 || result.Entries[0] != old || len(result.CreatedIDs) != wantCreated {
			t.Fatalf("batch %d %s", w.Code, w.Body)
		}
	}
	other, err := s.memorySnapshot(b)
	if err != nil || len(other.Entries) != 0 {
		t.Fatalf("isolation %+v %v", other, err)
	}
	for _, body := range []string{`{"entries":[]}`, `{"entries":null}`, `{"entries":[{"category":"user","content":"valid first"},{"category":"bad","content":"bad"}]}`, `{"entries":[{"category":"user","content":"x","id":"injected"}]}`} {
		w := memoryRequest(t, h, a, "POST", "/api/memories/batch", body)
		if w.Code != 400 {
			t.Fatalf("invalid %d %s", w.Code, w.Body)
		}
	}
	for i := 2; i < maxMemoryEntries-1; i++ {
		if _, err := s.saveMemory(a, "user", fmt.Sprint("fact ", i)); err != nil {
			t.Fatal(err)
		}
	}
	w := memoryRequest(t, h, a, "POST", "/api/memories/batch", `{"entries":[{"category":"user","content":"fits alone"},{"category":"user","content":"overflow"}]}`)
	if w.Code != 409 {
		t.Fatalf("limit %d %s", w.Code, w.Body)
	}
	snap, err := s.memorySnapshot(a)
	if err != nil || len(snap.Entries) != 31 {
		t.Fatalf("partial write %+v %v", snap, err)
	}
}

type suggestionFixtureProvider struct {
	call func(context.Context, []chatMessage, string) (string, error)
}

func (p suggestionFixtureProvider) configured() bool                              { return true }
func (p suggestionFixtureProvider) models(context.Context) ([]modelOption, error) { return nil, nil }
func (p suggestionFixtureProvider) complete(ctx context.Context, m []chatMessage, model string) (string, error) {
	return p.call(ctx, m, model)
}

func TestMemorySuggestionsStrictOutputAndSafeFailures(t *testing.T) {
	s, _, a, _ := memoryFixture(t)
	replies := []string{`{}`, `null`, `{"suggestions":null}`, `{"suggestions":[null]}`, `{"suggestions":[""]}`, `{"suggestions":["ok"],"other":true}`, `{"suggestions":[]} {}`, "```json\n{\"suggestions\":[]}\n```", `{"suggestions":["` + strings.Repeat("é", 513) + `"]}`, `{"suggestions":["a","b","c","d","e","f","g"]}`, `{"suggestions":[],"suggestions":["hidden"]}`, string([]byte{0xff})}
	for _, reply := range replies {
		mux := http.NewServeMux()
		registerMemorySuggestionRoutes(mux, s, suggestionFixtureProvider{call: func(ctx context.Context, m []chatMessage, model string) (string, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > memorySuggestionTimeout {
				t.Error("missing timeout")
			}
			if model != "requested" {
				t.Error("model lost")
			}
			return reply, nil
		}})
		w := memoryRequest(t, mux, a, "POST", "/api/memory-suggestions", `{"message":"selected","model":"requested"}`)
		if w.Code != 502 {
			t.Errorf("accepted output %q: %d %s", reply, w.Code, w.Body)
		}
	}
	mux := http.NewServeMux()
	registerMemorySuggestionRoutes(mux, s, suggestionFixtureProvider{call: func(context.Context, []chatMessage, string) (string, error) {
		return "", errors.New("SECRET raw provider error")
	}})
	w := memoryRequest(t, mux, a, "POST", "/api/memory-suggestions", `{"message":"selected"}`)
	if w.Code != 502 || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatalf("unsafe failure %d %s", w.Code, w.Body)
	}
	for _, table := range []string{"memory_entries", "memory_settings", "conversations"} {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("suggestions wrote %s: %d %v", table, n, err)
		}
	}
}

func TestMemorySuggestionsEmptyAndCancellation(t *testing.T) {
	s, _, a, _ := memoryFixture(t)
	for _, cancelled := range []bool{false, true} {
		mux := http.NewServeMux()
		registerMemorySuggestionRoutes(mux, s, suggestionFixtureProvider{call: func(ctx context.Context, _ []chatMessage, _ string) (string, error) {
			if cancelled {
				<-ctx.Done()
				return "", ctx.Err()
			}
			return `{"suggestions":[]}`, nil
		}})
		r := httptest.NewRequest("POST", "/api/memory-suggestions", strings.NewReader(`{"message":"hello"}`))
		r.AddCookie(&http.Cookie{Name: authCookieName, Value: a.ID})
		ctx, cancel := context.WithCancel(r.Context())
		if cancelled {
			cancel()
		}
		r = r.WithContext(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		cancel()
		if cancelled && w.Code != 408 {
			t.Fatalf("cancel %d %s", w.Code, w.Body)
		}
		if !cancelled && (w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"suggestions":[]}`) {
			t.Fatalf("empty %d %s", w.Code, w.Body)
		}
	}
}

func TestMemoryBatchRollbackEncodedLimitAndDBFailure(t *testing.T) {
	for _, databaseFailure := range []bool{false, true} {
		s, h, a, _ := memoryHTTPFixture(t)
		content := strings.Repeat("<", 1024)
		want := 409
		if databaseFailure {
			content = "reject me"
			want = 503
			if _, err := s.db.Exec(`CREATE TRIGGER reject_batch BEFORE INSERT ON memory_entries WHEN NEW.content='reject me' BEGIN SELECT RAISE(ABORT,'SECRET failure'); END`); err != nil {
				t.Fatal(err)
			}
		}
		payload, _ := json.Marshal(map[string]any{"entries": []memoryBatchInput{{"user", "first"}, {"project", content}, {"project", content + "different"}}})
		// Both large facts must be individually valid but exceed the encoded snapshot together.
		if !databaseFailure {
			payload, _ = json.Marshal(map[string]any{"entries": []memoryBatchInput{{"user", "first"}, {"project", content}, {"project", strings.Repeat(">", 1024)}}})
		}
		w := memoryRequest(t, h, a, "POST", "/api/memories/batch", string(payload))
		if w.Code != want || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("rollback %d %s", w.Code, w.Body)
		}
		snapshot, err := s.memorySnapshot(a)
		if err != nil || len(snapshot.Entries) != 0 {
			t.Fatalf("partial write %+v %v", snapshot, err)
		}
	}
}
