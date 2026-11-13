package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestMemoryRememberReportsCapacityAndStorageFailures(t *testing.T) {
	for _, kind := range []string{"capacity", "storage"} {
		t.Run(kind, func(t *testing.T) {
			store, err := newAccountStore(t.TempDir() + "/accounts.sqlite")
			if err != nil {
				t.Fatal(err)
			}
			defer store.db.Close()
			user, token, err := store.register("memory-errors", "correct-horse-battery")
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := http.StatusConflict
			if kind == "capacity" {
				for i := 0; i < maxMemoryEntries; i++ {
					if _, err := store.saveMemory(user, "user", fmt.Sprintf("Fact %d", i)); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				wantStatus = http.StatusServiceUnavailable
				if _, err := store.db.Exec(`DROP TABLE memory_entries`); err != nil {
					t.Fatal(err)
				}
			}
			probe := &contextProbe{}
			handler := store.middleware(chatHandlerWithStore(probe, store))
			response := runRequest(handler, "POST", "/api/chat", `{"messages":[{"role":"user","content":"/remember additional fact"}]}`, &http.Cookie{Name: authCookieName, Value: token})
			if response.Code != wantStatus {
				t.Fatalf("%s status=%d want=%d body=%s", kind, response.Code, wantStatus, response.Body)
			}
			if len(probe.messages) != 0 {
				t.Fatal("failed local save invoked the model")
			}
			if strings.Contains(response.Body.String(), "memory_entries") {
				t.Fatal("storage details leaked")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("memory failure was cacheable")
			}
			mux := http.NewServeMux()
			mux.Handle("GET /api/runs/{id}", store.middleware(http.HandlerFunc(store.runHandler)))
			run := decodeRunMap(t, runRequest(mux, "GET", "/api/runs/"+response.Header().Get("X-Agent-Run-ID"), "", &http.Cookie{Name: authCookieName, Value: token}))
			if run["state"] != "failed" || run["request"] != nil {
				t.Fatal("failed save trace invented successful inference")
			}
		})
	}
}

func TestMemoryRememberRequiresRunReservation(t *testing.T) {
	store, err := newAccountStore(t.TempDir() + "/accounts.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	user, token, err := store.register("memory-run-failure", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_run BEFORE INSERT ON runs BEGIN SELECT RAISE(ABORT,'private trace failure'); END`); err != nil {
		t.Fatal(err)
	}
	probe := &contextProbe{}
	handler := store.middleware(chatHandlerWithStore(probe, store))
	response := runRequest(handler, "POST", "/api/chat", `{"run_id":"62434567-1234-4567-890a-123456789abc","messages":[{"role":"user","content":"/remember must reserve first"}]}`, &http.Cookie{Name: authCookieName, Value: token})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("run reservation failure: %d %s", response.Code, response.Body)
	}
	snapshot, err := store.memorySnapshot(user)
	if err != nil || len(snapshot.Entries) != 0 {
		t.Fatalf("unreserved save persisted: %+v err=%v", snapshot, err)
	}
	if len(probe.messages) != 0 || response.Header().Get("X-Agent-Run-ID") != "" {
		t.Fatal("failed reservation invented model work or a trace ID")
	}
	if strings.Contains(response.Body.String(), "private trace failure") {
		t.Fatal("private storage failure disclosed")
	}
}
