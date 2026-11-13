package main

import (
	"errors"
	"testing"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestMemoryStoreConnectionPragmas(t *testing.T) {
	s, _, _, _ := memoryFixture(t)
	// Each query opens a fresh physical connection with the production pool.
	for i := 0; i < 2; i++ {
		var timeout, foreignKeys int
		if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if timeout != 5000 || foreignKeys != 1 {
			t.Fatalf("connection settings: busy_timeout=%d foreign_keys=%d", timeout, foreignKeys)
		}
	}
}

func TestMemoryFailedCommitReleasesTransaction(t *testing.T) {
	s, path, user, _ := memoryFixture(t)
	other, err := newAccountStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.db.Close()
	// Keep the writer connection alive and force a fast, deterministic commit
	// failure while the other store retains a rollback-journal read lock.
	s.db.SetMaxIdleConns(1)
	if _, err := s.db.Exec("PRAGMA busy_timeout=1"); err != nil {
		t.Fatal(err)
	}
	reader, err := other.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	var count int
	if err := reader.QueryRow("SELECT count(*) FROM memory_entries").Scan(&count); err != nil {
		t.Fatal(err)
	}
	_, err = s.saveMemory(user, "user", "must roll back")
	var busy *sqlite.Error
	if !errors.As(err, &busy) || busy.Code()&0xff != sqlite3.SQLITE_BUSY {
		t.Fatalf("want busy commit failure, got %T %v", err, err)
	}
	if err := reader.Rollback(); err != nil {
		t.Fatal(err)
	}
	entry, err := s.saveMemory(user, "user", "saved after failure")
	if err != nil {
		t.Fatalf("subsequent save failed: %v", err)
	}
	snapshot, err := s.memorySnapshot(user)
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0] != entry {
		t.Fatalf("failed commit retained data: %+v err=%v", snapshot, err)
	}
}
