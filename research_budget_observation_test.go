package main

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

// A durable settlement may wait on storage. Preserve the receipt timestamp of
// provider metadata rather than making an older response look newer afterwards.
func TestResearchBudgetSettlementWaitPreservesObservationOrder(t *testing.T) {
	a, s, h, c, _ := settingsHarness(t, func(w http.ResponseWriter, r *http.Request) {
		remaining := "20"
		if r.URL.Query().Get("q") == "older response" {
			remaining = "10"
		}
		writeUsageFixture(w, remaining)
	})
	saved := settingsResult(t, researchRequest(h, c, "POST", "/api/research/settings/verify", settingsBody(settingsTestKey, "")), 200)
	user := mustSettingsUser(t, a, c)
	credential, err := a.researchCredentialForUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	web := s.settings.webWithUsage(user.ID, credential)
	original := web.reserveRequest
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var reservations atomic.Int32
	web.reserveRequest = func() (func(int) error, error) {
		settle, err := original()
		if err != nil {
			return nil, err
		}
		first := reservations.Add(1) == 1
		return func(status int) error {
			if first {
				close(entered)
				<-release
			}
			return settle(status)
		}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := web.search(context.Background(), "older response"); done <- err }()
	<-entered
	if _, err = web.search(context.Background(), "newer response"); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	after, err := a.researchCredentialForUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != saved.Revision || after.Usage == nil || after.Usage.Windows[1].Remaining != 20 {
		t.Fatal("slow budget settlement reordered provider observations", after.Usage)
	}
}
