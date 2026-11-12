package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func writeUsageFixture(w http.ResponseWriter, remaining string) {
	for k, v := range usageHeaders("1,15000", "0,"+remaining, "1,100", "1;w=1,15000;w=2592000") {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"web":{"results":[]}}`)
}

func TestResearchUsageFailedSameKeyVerifyCaptures429(t *testing.T) {
	var mu sync.Mutex
	status := 200
	_, _, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		code := status
		mu.Unlock()
		for k, v := range usageHeaders("1", "0", "1", "1;w=1") {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		fmt.Fprint(w, `{"web":{"results":[]}}`)
	})
	saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	mu.Lock()
	status = 429
	mu.Unlock()
	got := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, saved.Revision)), 429)
	if got.Usage == nil || got.Usage.ObservedAt == saved.Usage.ObservedAt || got.Revision != saved.Revision {
		t.Fatal("same-key failed verify lost trusted observation")
	}
}

func TestResearchUsageInflightOldKeyCannotOverwrite(t *testing.T) {
	for _, action := range []string{"replace", "remove"} {
		t.Run(action, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("q") == "blocked old search" {
					close(entered)
					<-release
					writeUsageFixture(w, "10")
					return
				}
				writeUsageFixture(w, "14000")
			})
			saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
			user := mustSettingsUser(t, a, c)
			s.mu.Lock()
			web, _, err := s.webForUserLocked(user.ID)
			s.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, e := web.search(context.Background(), "blocked old search"); done <- e }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("search did not enter")
			}
			var current researchSettings
			if action == "remove" {
				current = settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/remove", settingsRevision(saved.Revision)), 200)
			} else {
				current = settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody("synthetic-replacement-9999", saved.Revision)), 200)
			}
			once.Do(func() { close(release) })
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("old search blocked")
			}
			after := settingsResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
			if action == "remove" {
				if after.Usage != nil {
					t.Fatal("removed snapshot resurrected")
				}
			} else if after.Usage == nil || after.Usage.ObservedAt != current.Usage.ObservedAt || after.Usage.Windows[1].Remaining != 14000 {
				t.Fatal("inflight old key overwrote new usage")
			}
		})
	}
}

func TestResearchUsageConcurrentCheckDoesNotEraseSearch(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var mu sync.Mutex
	block := false
	a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		shouldBlock := block && r.URL.Query().Get("q") == "Agent connection test"
		mu.Unlock()
		if shouldBlock {
			close(entered)
			<-release
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"web":{"results":[]}}`)
			return
		}
		writeUsageFixture(w, "14000")
	})
	saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	mu.Lock()
	block = true
	mu.Unlock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- researchRequest(h, c, "POST", "/api/research/settings/check", settingsRevision(saved.Revision))
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("check did not enter")
	}
	settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/check", settingsRevision(saved.Revision)), 429)
	user := mustSettingsUser(t, a, c)
	s.mu.Lock()
	web, _, err := s.webForUserLocked(user.ID)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = web.search(context.Background(), "parallel ordinary search"); err != nil {
		t.Fatal(err)
	}
	during := settingsResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
	if during.Usage == nil || during.Usage.ObservedAt == saved.Usage.ObservedAt {
		t.Fatal("search missed observation")
	}
	once.Do(func() { close(release) })
	select {
	case response := <-done:
		checked := settingsResult(t, response, 200)
		if checked.Usage == nil || checked.Usage.ObservedAt != during.Usage.ObservedAt {
			t.Fatal("stale check erased search in reply")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("check blocked")
	}
	after := settingsResult(t, researchRequest(h, c, "GET", "/api/research/settings", ""), 200)
	if after.Usage == nil || after.Usage.ObservedAt != during.Usage.ObservedAt {
		t.Fatal("stale check erased persisted snapshot")
	}
}

func TestResearchUsageSameKeyRevisionKeepsNewestSnapshot(t *testing.T) {
	a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) { writeUsageFixture(w, "14000") })
	saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	user := mustSettingsUser(t, a, c)
	stale, e := a.researchCredentialForUser(user.ID)
	if e != nil {
		t.Fatal(e)
	}
	newer := parseResearchUsage(usageHeaders("1", "0", "1", "1;w=1"), time.Now().Add(time.Minute))
	if e = a.saveResearchUsage(user.ID, saved.Revision, newer); e != nil {
		t.Fatal(e)
	}
	stale.Revision = "new-same-key-revision"
	s.mu.Lock()
	e = s.settings.persist(user.ID, c.Value, saved.Revision, stale)
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	got, e := a.researchCredentialForUser(user.ID)
	if e != nil || got.Usage == nil || got.Usage.ObservedAt != newer.ObservedAt {
		t.Fatal("same-key verification erased newer ordinary search", e)
	}
}

func TestResearchUsageConcurrentMonotonicWrites(t *testing.T) {
	a, _, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) { writeUsageFixture(w, "14000") })
	saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	user := mustSettingsUser(t, a, c)
	base := time.Now().Add(time.Minute).UTC()
	newest := parseResearchUsage(usageHeaders("1", "0", "1", "1;w=1"), base)
	var wg sync.WaitGroup
	errors := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			usage := parseResearchUsage(usageHeaders("1", "0", "1", "1;w=1"), base.Add(-time.Duration(i)*time.Millisecond))
			errors <- a.saveResearchUsage(user.ID, saved.Revision, usage)
		}(i)
	}
	wg.Wait()
	close(errors)
	for e := range errors {
		if e != nil {
			t.Fatal(e)
		}
	}
	got, e := a.researchCredentialForUser(user.ID)
	if e != nil || got.Usage == nil || got.Usage.ObservedAt != newest.ObservedAt {
		t.Fatal("concurrent snapshots went backwards", e)
	}
}
