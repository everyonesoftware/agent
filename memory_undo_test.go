package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMemoryUndoConditionalDelete(t *testing.T) {
	for _, mode := range []string{"unchanged", "edited", "same content newer revision", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			s, h, a, _ := memoryHTTPFixture(t)
			original, err := s.saveMemory(a, "user", "Original private fact")
			if err != nil {
				t.Fatal(err)
			}
			current := original
			if mode != "unchanged" {
				content := "Edited private fact"
				if mode == "same content newer revision" {
					content = original.Content
				}
				current, err = s.writeMemory(a, original.ID, original.Category, content)
				if err != nil {
					t.Fatal(err)
				}
				if current.UpdatedAt == original.UpdatedAt {
					t.Fatal("fixture must change revision")
				}
			}
			tag, _ := json.Marshal(original.UpdatedAt)
			headers := []string{"If-Match", string(tag)}
			if mode == "legacy" {
				headers = nil
			}
			w := memoryRequest(t, h, a, "DELETE", "/api/memories/"+original.ID, "", headers...)
			want := http.StatusOK
			if mode == "edited" || mode == "same content newer revision" {
				want = http.StatusPreconditionFailed
			}
			if w.Code != want {
				t.Fatalf("DELETE status=%d want=%d body=%s", w.Code, want, w.Body)
			}
			snapshot, err := s.memorySnapshot(a)
			if err != nil {
				t.Fatal(err)
			}
			if want == http.StatusPreconditionFailed {
				if len(snapshot.Entries) != 1 || snapshot.Entries[0] != current {
					t.Fatalf("edit lost: %+v", snapshot)
				}
				if w.Body.String() != "{\"error\":\"Memory entry changed. Refresh before trying to undo.\"}\n" {
					t.Fatalf("unsafe or unclear conflict: %s", w.Body)
				}
			} else if len(snapshot.Entries) != 0 {
				t.Fatalf("not deleted: %+v", snapshot)
			}
		})
	}
}

func TestMemoryUndoConditionalHeaderValidation(t *testing.T) {
	s, h, a, _ := memoryHTTPFixture(t)
	entry, err := s.saveMemory(a, "user", "Keep this fact")
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"", "*", "W/\"2026-11-13T00:00:00Z\"", "2026-11-13T00:00:00Z", "\"not-a-timestamp\"", "\"2026-11-13T00:00:00Z\", \"2026-11-14T00:00:00Z\"", "null", "\"2026-11-13T00:00:00\\u005a\""} {
		w := memoryRequest(t, h, a, "DELETE", "/api/memories/"+entry.ID, "", "If-Match", header)
		if w.Code != http.StatusBadRequest {
			t.Errorf("header %q: status=%d body=%s", header, w.Code, w.Body)
		}
	}
	snapshot, err := s.memorySnapshot(a)
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0] != entry {
		t.Fatalf("invalid header deleted memory: %+v %v", snapshot, err)
	}
}

func TestMemoryUndoConditionalForeignAndMissing(t *testing.T) {
	s, h, a, b := memoryHTTPFixture(t)
	entry, err := s.saveMemory(a, "user", "Private owner fact")
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{entry.UpdatedAt, "2000-01-01T00:00:00Z"} {
		tag, _ := json.Marshal(revision)
		foreign := memoryRequest(t, h, b, "DELETE", "/api/memories/"+entry.ID, "", "If-Match", string(tag))
		missing := memoryRequest(t, h, b, "DELETE", "/api/memories/missing", "", "If-Match", string(tag))
		if foreign.Code != 404 || missing.Code != 404 || foreign.Body.String() != missing.Body.String() {
			t.Fatalf("foreign=%d %s missing=%d %s", foreign.Code, foreign.Body, missing.Code, missing.Body)
		}
	}
	snapshot, err := s.memorySnapshot(a)
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0] != entry {
		t.Fatalf("foreign delete mutated memory: %+v %v", snapshot, err)
	}
}
