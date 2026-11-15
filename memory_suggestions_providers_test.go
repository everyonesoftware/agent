package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestMemorySuggestionsResponsesUserScopedWire(t *testing.T) {
	f, provider, _, audit := codexInferenceFixture(t, "suggestions")
	s, a := f.accounts, f.user
	if _, err := s.saveMemory(a, "user", "PRIVATE SAVED FACT"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerMemorySuggestionRoutes(mux, s, &userScopedProvider{accounts: s, baseAuth: provider.auth})
	w := runRequest(mux, "POST", "/api/memory-suggestions", `{"message":"I prefer concise explanations.","model":"codex-fixture"}`, f.cookie)
	if w.Code != 200 {
		t.Fatalf("Codex suggestions %d %s", w.Code, w.Body)
	}
	wire, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire), "Extract memory suggestions") || !strings.Contains(string(wire), "I prefer concise explanations.") || strings.Contains(string(wire), "PRIVATE") {
		t.Fatalf("unsafe or missing selected-message-only RPC wire: %s", wire)
	}
	var data []byte
	if err := s.db.QueryRow(`SELECT data FROM runs WHERE id=? AND user_id=?`, w.Header().Get("X-Agent-Run-ID"), a.ID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		t.Fatal(err)
	}
	if run.Provider != "chatgpt" || run.Model != "codex-fixture" || run.Request == nil || run.Usage == nil || run.Usage.TotalTokens != 36 || strings.Contains(string(data), "PRIVATE") {
		t.Fatalf("unsafe/missing trace %s", data)
	}
	var request struct{ Messages []chatMessage }
	requestBytes, err := json.Marshal(run.Request)
	if err != nil || json.Unmarshal(requestBytes, &request) != nil || len(request.Messages) != 2 || request.Messages[0] != (chatMessage{Role: "system", Content: memorySuggestionInstructions}) || request.Messages[1] != (chatMessage{Role: "user", Content: "I prefer concise explanations."}) {
		t.Fatalf("selected-message trace changed: %s", requestBytes)
	}
	registered := runRequest(http.HandlerFunc(s.registerHandler), "POST", "/api/auth/register", `{"username":"other-suggestion-user","password":"isolated-test-only-password"}`, nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("other user registration: %d %s", registered.Code, registered.Body)
	}
	other := runRequest(mux, "POST", "/api/memory-suggestions", `{"message":"hello"}`, registered.Result().Cookies()[0])
	if other.Code != 503 {
		t.Fatalf("foreign account %d %s", other.Code, other.Body)
	}
}

func TestMemoryBatchConcurrentReplayAcrossStores(t *testing.T) {
	s, path, a, _ := memoryFixture(t)
	second, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.db.Close()
	start := make(chan struct{})
	results := make(chan memoryBatchResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, store := range []*accountStore{s, second} {
		wg.Add(1)
		go func(store *accountStore) {
			defer wg.Done()
			<-start
			result, err := store.saveMemoryBatch(a, []memoryBatchInput{{"user", "same fact"}, {"project", "SAME FACT"}})
			results <- result
			errs <- err
		}(store)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	created := 0
	id := ""
	for result := range results {
		created += len(result.CreatedIDs)
		if len(result.Entries) != 1 {
			t.Fatalf("result %+v", result)
		}
		if id != "" && id != result.Entries[0].ID {
			t.Fatal("duplicate IDs")
		}
		id = result.Entries[0].ID
	}
	snap, err := s.memorySnapshot(a)
	if err != nil || created != 1 || len(snap.Entries) != 1 {
		t.Fatalf("created %d snapshot %+v %v", created, snap, err)
	}
}
