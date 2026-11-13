package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemorySuggestionsResponsesUserScopedWire(t *testing.T) {
	s, _, a, b := memoryFixture(t)
	if _, err := s.saveMemory(a, "user", "PRIVATE SAVED FACT"); err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Model string        `json:"model"`
		Input []chatMessage `json:"input"`
		Store bool          `json:"store"`
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Write([]byte(`{"data":[{"id":"oauth-suggest","visibility":"list"}]}`))
			return
		}
		if r.URL.Path != "/responses" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
		}
		delta, _ := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": `{"suggestions":["Uses Go."]}`})
		fmt.Fprintf(w, "data: %s\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":9,\"output_tokens\":3,\"total_tokens\":12}}}\n\n", delta)
	}))
	defer upstream.Close()
	base := newSIWCService(siwcConfig{apiBaseURL: upstream.URL}, newFileCredentialStore(t.TempDir()))
	service := s.siwcForUser(base, a)
	service.client = upstream.Client()
	if err := service.store.Save(testOAuthCredentials("PRIVATE-ACCESS", "PRIVATE-REFRESH", time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerMemorySuggestionRoutes(mux, s, &userScopedProvider{accounts: s, baseAuth: base})
	w := memoryRequest(t, mux, a, "POST", "/api/memory-suggestions", `{"message":"I use Go.","model":"oauth-suggest"}`)
	if w.Code != 200 {
		t.Fatalf("oauth suggestions %d %s", w.Code, w.Body)
	}
	if wire.Store || wire.Model != "oauth-suggest" || len(wire.Input) != 2 || wire.Input[0] != (chatMessage{Role: "system", Content: memorySuggestionInstructions}) || wire.Input[1] != (chatMessage{Role: "user", Content: "I use Go."}) {
		t.Fatalf("wire %+v", wire)
	}
	var data []byte
	if err := s.db.QueryRow(`SELECT data FROM runs WHERE id=? AND user_id=?`, w.Header().Get("X-Agent-Run-ID"), a.ID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		t.Fatal(err)
	}
	if run.Provider != "chatgpt" || run.Model != "oauth-suggest" || run.Request == nil || run.Usage == nil || run.Usage.TotalTokens != 12 || strings.Contains(string(data), "PRIVATE") {
		t.Fatalf("unsafe/missing trace %s", data)
	}
	other := memoryRequest(t, mux, b, "POST", "/api/memory-suggestions", `{"message":"hello"}`)
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
