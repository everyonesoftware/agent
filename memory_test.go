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
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func memoryFixture(t *testing.T) (*accountStore, string, authUser, authUser) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "everyoneagent.sqlite")
	s, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.db.Close() })
	a, b := authUser{ID: "memory-alice", Username: "memory-alice"}, authUser{ID: "memory-bob", Username: "memory-bob"}
	for _, user := range []authUser{a, b} {
		if _, err := s.db.Exec(`INSERT INTO users(id,username,password_hash,created_at) VALUES(?,?,?,?)`, user.ID, user.Username, []byte("unused"), time.Now().Unix()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, hashSession(user.ID), user.ID, time.Now().Add(time.Hour).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	return s, path, a, b
}

func memoryRequest(t *testing.T, h http.Handler, user authUser, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if user.ID != "" {
		r.AddCookie(&http.Cookie{Name: authCookieName, Value: user.ID})
	}
	for i := 0; i < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("%s %s cache=%q", method, path, w.Header().Get("Cache-Control"))
	}
	return w
}

func memoryHTTPFixture(t *testing.T) (*accountStore, http.Handler, authUser, authUser) {
	t.Helper()
	s, path, a, b := memoryFixture(t)
	t.Setenv("APP_DATA_DIR", filepath.Dir(path))
	return s, newServer(), a, b
}

func TestMemorySnapshotDefaultsAndDurability(t *testing.T) {
	s, path, a, b := memoryFixture(t)
	snap, err := s.memorySnapshot(a)
	if err != nil || !snap.Enabled || snap.Entries == nil || len(snap.Entries) != 0 {
		t.Fatalf("default=%+v err=%v", snap, err)
	}
	var settingsRows, entryRows int
	if err := s.db.QueryRow(`SELECT count(*) FROM memory_settings`).Scan(&settingsRows); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM memory_entries`).Scan(&entryRows); err != nil {
		t.Fatal(err)
	}
	if settingsRows != 0 || entryRows != 0 {
		t.Fatalf("read wrote state: settings=%d entries=%d", settingsRows, entryRows)
	}
	entry, err := s.saveMemory(a, "user", "  Prefer concise replies. \n")
	if err != nil || entry.Content != "Prefer concise replies." || entry.Category != "user" || entry.ID == "" || entry.UpdatedAt == "" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	if _, err := time.Parse(time.RFC3339Nano, entry.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	snap, err = reopened.memorySnapshot(a)
	if err != nil || len(snap.Entries) != 1 || snap.Entries[0] != entry {
		t.Fatalf("durable=%+v err=%v", snap, err)
	}
	other, err := reopened.memorySnapshot(b)
	if err != nil || !other.Enabled || len(other.Entries) != 0 {
		t.Fatalf("other=%+v err=%v", other, err)
	}
}

func TestMemoryHTTPCRUDExportAndDisabledPersistence(t *testing.T) {
	s, h, a, b := memoryHTTPFixture(t)
	w := memoryRequest(t, h, a, "GET", "/api/memories", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"enabled":true,"entries":[]}` {
		t.Fatalf("GET %d %s", w.Code, w.Body)
	}
	w = memoryRequest(t, h, a, "POST", "/api/memories", `{"category":"project","content":"  Repo uses Go.  "}`)
	var created struct {
		Entry MemoryEntry `json:"entry"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 || created.Entry.Content != "Repo uses Go." {
		t.Fatalf("POST %d %s", w.Code, w.Body)
	}
	id := created.Entry.ID
	w = memoryRequest(t, h, a, "PUT", "/api/memory-settings", `{"enabled":false}`)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"enabled":false}` {
		t.Fatalf("settings %d %s", w.Code, w.Body)
	}
	w = memoryRequest(t, h, a, "PUT", "/api/memories/"+id, `{"category":"user","content":"  Prefer tests first. "}`)
	var updated struct {
		Entry MemoryEntry `json:"entry"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || updated.Entry.ID != id || updated.Entry.Category != "user" || updated.Entry.Content != "Prefer tests first." {
		t.Fatalf("PUT %d %s", w.Code, w.Body)
	}
	if _, err := s.saveMemory(a, "project", "Disabled memory remains editable."); err != nil {
		t.Fatal(err)
	}
	w = memoryRequest(t, h, a, "GET", "/api/memories/export", "")
	var exported MemorySnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || exported.Enabled || len(exported.Entries) != 2 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment;") || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("export %d %s headers=%v", w.Code, w.Body, w.Header())
	}
	reopened, err := newAccountStore(filepath.Join(s.dataDir, "everyoneagent.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	snap, err := reopened.memorySnapshot(a)
	if err != nil || snap.Enabled || len(snap.Entries) != 2 {
		t.Fatalf("reopened=%+v err=%v", snap, err)
	}
	other, err := reopened.memorySnapshot(b)
	if err != nil || !other.Enabled || len(other.Entries) != 0 {
		t.Fatalf("other=%+v err=%v", other, err)
	}
	w = memoryRequest(t, h, a, "DELETE", "/api/memories/"+id, "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"deleted":true}` {
		t.Fatalf("DELETE %d %s", w.Code, w.Body)
	}
	snap, err = reopened.memorySnapshot(a)
	if err != nil || len(snap.Entries) != 1 {
		t.Fatalf("delete readback=%+v err=%v", snap, err)
	}
	w = memoryRequest(t, h, a, "PUT", "/api/memory-settings", `{"enabled":true}`)
	snap, err = reopened.memorySnapshot(a)
	if w.Code != 200 || err != nil || !snap.Enabled {
		t.Fatalf("reenabled=%+v err=%v", snap, err)
	}
}

func TestMemoryHTTPRejectsForeignAndMissingIDsIdentically(t *testing.T) {
	s, h, a, b := memoryHTTPFixture(t)
	entry, err := s.saveMemory(a, "user", "Private preference")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"PUT", "DELETE"} {
		foreign := memoryRequest(t, h, b, method, "/api/memories/"+entry.ID, `{"category":"user","content":"overwrite"}`)
		missing := memoryRequest(t, h, b, method, "/api/memories/not-an-id", `{"category":"user","content":"overwrite"}`)
		if foreign.Code != 404 || missing.Code != 404 || foreign.Body.String() != missing.Body.String() {
			t.Fatalf("%s foreign=%d %s missing=%d %s", method, foreign.Code, foreign.Body, missing.Code, missing.Body)
		}
	}
	snap, err := s.memorySnapshot(a)
	if err != nil || len(snap.Entries) != 1 || snap.Entries[0] != entry {
		t.Fatalf("foreign mutation=%+v err=%v", snap, err)
	}
}

func TestMemoryValidationAndUTF8ByteLimit(t *testing.T) {
	s, _, a, _ := memoryFixture(t)
	for _, tc := range []struct{ category, content string }{
		{"User", "hello"}, {"", "hello"}, {"system", "hello"}, {"user", " \n\t "},
		{"user", strings.Repeat("a", 1025)}, {"project", strings.Repeat("é", 513)}, {"user", string([]byte{0xff})},
	} {
		if _, err := s.saveMemory(a, tc.category, tc.content); err == nil {
			t.Errorf("accepted invalid category=%q content bytes=%d", tc.category, len(tc.content))
		}
	}
	if _, err := s.saveMemory(a, "project", " \n"+strings.Repeat("é", 512)+"\t"); err != nil {
		t.Fatalf("1024 UTF8 bytes rejected: %v", err)
	}
	snap, err := s.memorySnapshot(a)
	if err != nil || len(snap.Entries) != 1 {
		t.Fatalf("validation snapshot=%+v err=%v", snap, err)
	}
}

func TestMemoryCountAndEncodedSnapshotBounds(t *testing.T) {
	t.Run("count", func(t *testing.T) {
		s, _, a, b := memoryFixture(t)
		for i := 0; i < 32; i++ {
			if _, err := s.saveMemory(a, "user", fmt.Sprint(i)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.saveMemory(a, "project", "overflow"); err == nil {
			t.Fatal("accepted entry 33")
		}
		if _, err := s.saveMemory(b, "project", "independent limit"); err != nil {
			t.Fatal(err)
		}
		snap, err := s.memorySnapshot(a)
		if err != nil || len(snap.Entries) != 32 {
			t.Fatalf("count=%d err=%v", len(snap.Entries), err)
		}
	})
	t.Run("encoded bytes including JSON escaping", func(t *testing.T) {
		s, _, a, _ := memoryFixture(t)
		accepted := 0
		for i := 0; i < 32; i++ {
			if _, err := s.saveMemory(a, "project", strings.Repeat("<", 1024)); err != nil {
				break
			}
			accepted++
		}
		if accepted < 1 || accepted >= 32 {
			t.Fatalf("aggregate bound accepted=%d", accepted)
		}
		snap, err := s.memorySnapshot(a)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(snap)
		if err != nil || len(data) > 12*1024 || len(snap.Entries) != accepted {
			t.Fatalf("encoded=%d entries=%d accepted=%d err=%v", len(data), len(snap.Entries), accepted, err)
		}
	})
}

func TestMemoryConcurrentWritersCannotExceedLimit(t *testing.T) {
	s, path, a, _ := memoryFixture(t)
	other, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.db.Close()
	for i := 0; i < 30; i++ {
		if _, err := s.saveMemory(a, "user", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	// Separate stores contend for SQLite's writer lock. Retry only explicit
	// BUSY/LOCKED errors, never generic storage failures or capacity rejections.
	isContention := func(err error) bool {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return false
		}
		code := sqliteErr.Code() & 0xff // Include extended BUSY/LOCKED codes.
		return code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED
	}
	const writers, maxAttempts = 10, 20
	var attempts [writers][]error
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := s
			if i%2 == 1 {
				store = other
			}
			for attempt := 0; attempt < maxAttempts; attempt++ {
				_, err := store.saveMemory(a, "project", fmt.Sprint(i))
				attempts[i] = append(attempts[i], err)
				if !isContention(err) || attempt == maxAttempts-1 {
					return
				}
				time.Sleep(time.Duration(attempt+1) * time.Millisecond)
			}
		}(i)
	}
	wg.Wait()
	successes, capacityFailures, unexpected, contention := 0, 0, 0, 0
	for i, errs := range attempts {
		for attempt, err := range errs {
			if isContention(err) {
				contention++
				t.Logf("writer=%d attempt=%d contention: %v", i, attempt+1, err)
			}
		}
		err := errs[len(errs)-1]
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errMemoryLimit):
			capacityFailures++
		default:
			unexpected++
			t.Errorf("writer=%d stopped after %d attempts: %T %v", i, len(errs), err, err)
		}
	}
	t.Logf("successes=%d capacity failures=%d unexpected=%d contention attempts=%d", successes, capacityFailures, unexpected, contention)
	if successes != 2 || capacityFailures != writers-2 || unexpected != 0 {
		t.Errorf("want exactly 2 successes and 8 capacity rejections; got successes=%d capacity failures=%d unexpected=%d", successes, capacityFailures, unexpected)
	}
	for i, store := range []*accountStore{s, other} {
		snap, err := store.memorySnapshot(a)
		if err != nil || len(snap.Entries) != 32 {
			t.Errorf("store=%d concurrent count=%d err=%v", i, len(snap.Entries), err)
		}
	}
}

func TestMemoryHTTPStrictBoundedJSONAndNoMutationOnErrors(t *testing.T) {
	s, h, a, _ := memoryHTTPFixture(t)
	entry, err := s.saveMemory(a, "user", "unchanged")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		``, `null`, `[]`, `{`, `{"category":"user","content":"x","extra":true}`, `{"category":"user","content":"x"} {}`,
		`{"category":"user","content":"x"} garbage`, `{"category":"user","content":null}`, `{"category":"user","content":""}`,
		`{"category":"USER","content":"x"}`, `{"category":"user","content":` + fmt.Sprintf("%q", strings.Repeat("a", 1025)) + `}`,
		"{\"category\":\"user\",\"content\":\"" + string([]byte{0xff}) + "\"}",
		`{"category":"user","content":"x"}` + strings.Repeat(" ", 17000),
	} {
		for _, target := range []struct{ method, path string }{{"POST", "/api/memories"}, {"PUT", "/api/memories/" + entry.ID}} {
			w := memoryRequest(t, h, a, target.method, target.path, body)
			if w.Code != 400 {
				t.Errorf("%s body bytes=%d got %d %s", target.method, len(body), w.Code, w.Body)
			}
		}
	}
	for _, body := range []string{`{}`, `null`, `{"enabled":null}`, `{"enabled":"false"}`, `{"enabled":false,"extra":1}`, `{"enabled":false} {}`, `{"enabled":false}` + strings.Repeat(" ", 17000)} {
		w := memoryRequest(t, h, a, "PUT", "/api/memory-settings", body)
		if w.Code != 400 {
			t.Errorf("settings body=%q got %d", body[:min(len(body), 80)], w.Code)
		}
	}
	snap, err := s.memorySnapshot(a)
	if err != nil || !snap.Enabled || len(snap.Entries) != 1 || snap.Entries[0] != entry {
		t.Fatalf("error mutated snapshot=%+v err=%v", snap, err)
	}
}

func TestMemoryHTTPAuthenticationAndCSRF(t *testing.T) {
	s, h, a, _ := memoryHTTPFixture(t)
	entry, err := s.saveMemory(a, "project", "protected")
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/memories", ""}, {"GET", "/api/memories/export", ""},
		{"POST", "/api/memories", `{"category":"user","content":"new"}`},
		{"PUT", "/api/memories/" + entry.ID, `{"category":"user","content":"changed"}`},
		{"DELETE", "/api/memories/" + entry.ID, ""}, {"PUT", "/api/memory-settings", `{"enabled":false}`},
	} {
		w := memoryRequest(t, h, authUser{}, route.method, route.path, route.body)
		if w.Code != 401 {
			t.Errorf("unauth %s %s=%d", route.method, route.path, w.Code)
		}
		if route.method == "GET" {
			continue
		}
		for _, header := range [][]string{{"Origin", "https://evil.example"}, {"Sec-Fetch-Site", "cross-site"}} {
			w = memoryRequest(t, h, a, route.method, route.path, route.body, header...)
			if w.Code != 403 {
				t.Errorf("CSRF %s %s=%d", route.method, route.path, w.Code)
			}
		}
	}
	snap, err := s.memorySnapshot(a)
	if err != nil || !snap.Enabled || len(snap.Entries) != 1 || snap.Entries[0] != entry {
		t.Fatalf("CSRF mutated=%+v err=%v", snap, err)
	}
}

func TestMemoryHTTPUpdateBoundRollbackAndStorageErrors(t *testing.T) {
	s, h, a, _ := memoryHTTPFixture(t)
	var first MemoryEntry
	for i := 0; i < 8; i++ {
		entry, err := s.saveMemory(a, "user", strings.Repeat("a", 1024))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = entry
		}
	}
	w := memoryRequest(t, h, a, "PUT", "/api/memories/"+first.ID, `{"category":"user","content":"`+strings.Repeat("<", 1024)+`"}`)
	if w.Code != 409 {
		t.Fatalf("aggregate update status=%d %s", w.Code, w.Body)
	}
	snap, err := s.memorySnapshot(a)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range snap.Entries {
		if entry.ID == first.ID {
			found = entry == first
		}
	}
	if !found {
		t.Fatalf("failed update was not rolled back: %+v", snap)
	}
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.memorySnapshot(a); err == nil {
		t.Fatal("closed database snapshot succeeded")
	}
	if _, err := s.saveMemory(a, "user", "x"); err == nil {
		t.Fatal("closed database save succeeded")
	}
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/memories", ""}, {"GET", "/api/memories/export", ""},
		{"POST", "/api/memories", `{"category":"user","content":"new"}`},
		{"PUT", "/api/memories/" + first.ID, `{"category":"user","content":"changed"}`},
		{"DELETE", "/api/memories/" + first.ID, ""}, {"PUT", "/api/memory-settings", `{"enabled":false}`},
	} {
		r := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		r.SetPathValue("id", first.ID)
		r = r.WithContext(context.WithValue(r.Context(), authContextKey{}, a))
		w := httptest.NewRecorder()
		s.memoryHandler(w, r)
		if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), s.dataDir) {
			t.Errorf("storage failure %s %s=%d %s", route.method, route.path, w.Code, w.Body)
		}
	}
}
