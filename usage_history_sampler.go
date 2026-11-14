package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Startup is explicit: unit constructors never launch provider work.
func (s *usageService) startSampler(interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	s.mu.Lock()
	if s.closed || s.samplerCancel != nil || s.historyErr != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.samplerCancel = cancel
	s.samplerDone = done
	s.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sampleScopes(ctx)
			}
		}
	}()
}

// Discover registrations from database owners and profile stores, never by
// crawling credential homes or reading credentials. Bound each discovery pass.
func (s *usageService) sampleScopes(ctx context.Context) {
	rows, err := s.accounts.db.QueryContext(ctx, `SELECT id FROM users WHERE id>? ORDER BY id LIMIT 128`, s.samplerCursor)
	if err != nil {
		return
	}
	var users []authUser
	for rows.Next() {
		var user authUser
		if rows.Scan(&user.ID) != nil {
			rows.Close()
			return
		}
		users = append(users, user)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	if len(users) < 128 {
		s.samplerCursor = ""
	} else {
		s.samplerCursor = users[len(users)-1].ID
	}
	for _, user := range users {
		if ctx.Err() != nil {
			return
		}
		// Database ownership is authoritative, but still reject unsafe path tokens.
		if user.ID == "" || user.ID == "." || user.ID == ".." || strings.ContainsAny(user.ID, "/\\") {
			continue
		}
		dir := filepath.Join(s.accounts.dataDir, "account-data", user.ID, "oauth")
		if checkUsageParents(s.accounts.dataDir, dir) != nil {
			continue
		}
		// Do not let a background read migrate legacy credentials or follow a
		// linked profile file outside this owner's private registration store.
		info, err := os.Lstat(filepath.Join(dir, "siwc-profiles.json"))
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			continue
		}
		profiles, err := newFileCredentialStore(dir).ListProfiles()
		if err != nil {
			continue
		}
		for i, profile := range profiles {
			if i >= 128 || ctx.Err() != nil {
				break
			}
			if !validProfileID(profile.ID) {
				continue
			}
			registrationDir := filepath.Join(s.accounts.dataDir, "account-data", user.ID, "usage", profile.ID)
			if checkUsageParents(s.accounts.dataDir, registrationDir) != nil {
				continue
			}
			record, err := readUsageRecord(filepath.Join(registrationDir, "connection.json"))
			if err != nil || !validUsageConnectionID(record.ConnectionID) {
				continue
			}
			entry, err := s.session(user, profile.ID)
			if err != nil {
				continue
			}
			entry.mu.Lock()
			// A concurrent disconnect is authoritative, not the discovery snapshot.
			if entry.record.ConnectionID == record.ConnectionID {
				callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				s.status(callCtx, entry, profile.Email)
				cancel()
				// Background workers must not occupy the eight shared slots between ticks.
				// Pending device ceremonies retain their existing expiry/idle lifecycle.
				if entry.view.Login == nil {
					s.stopWorker(entry)
				}
			}
			entry.mu.Unlock()
		}
	}
}
