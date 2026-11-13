package main

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxMemoryEntries       = 32
	maxMemoryContentBytes  = 1024
	maxMemorySnapshotBytes = 12 * 1024
	maxMemoryRequestBytes  = 16 * 1024
)

var (
	errMemoryInput = errors.New("Use category user or project and nonempty memory content of at most 1024 UTF-8 bytes.")
	errMemoryLimit = errors.New("Memory is full. Keep at most 32 entries and a 12 KiB encoded snapshot; edit or delete an entry first.")
)

type MemoryEntry struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
}

type MemorySnapshot struct {
	Enabled bool          `json:"enabled"`
	Entries []MemoryEntry `json:"entries"`
}

func migrateMemory(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS memory_settings (
		user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1))
	);
	CREATE TABLE IF NOT EXISTS memory_entries (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		category TEXT NOT NULL CHECK(category IN ('user','project')),
		content TEXT NOT NULL CHECK(length(CAST(content AS BLOB)) BETWEEN 1 AND 1024),
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS memory_entries_user ON memory_entries(user_id, updated_at, id);`)
	return err
}

// Reads never create settings or memories. A missing settings row means enabled.
func (s *accountStore) memorySnapshot(user authUser) (MemorySnapshot, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return MemorySnapshot{}, err
	}
	defer tx.Rollback()
	snapshot, err := memorySnapshotTx(tx, user.ID)
	if err != nil {
		return MemorySnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemorySnapshot{}, err
	}
	return snapshot, nil
}

func memorySnapshotTx(tx *sql.Tx, userID string) (MemorySnapshot, error) {
	snapshot := MemorySnapshot{Entries: []MemoryEntry{}}
	err := tx.QueryRow(`SELECT COALESCE((SELECT enabled FROM memory_settings WHERE user_id=users.id),1) FROM users WHERE id=?`, userID).Scan(&snapshot.Enabled)
	if err != nil {
		return MemorySnapshot{}, err
	}
	rows, err := tx.Query(`SELECT id,category,content,updated_at FROM memory_entries WHERE user_id=? ORDER BY updated_at,id`, userID)
	if err != nil {
		return MemorySnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry MemoryEntry
		if err := rows.Scan(&entry.ID, &entry.Category, &entry.Content, &entry.UpdatedAt); err != nil {
			return MemorySnapshot{}, err
		}
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	return snapshot, rows.Err()
}

func validatedMemoryContent(category, content string) (string, error) {
	content = strings.TrimSpace(content)
	if (category != "user" && category != "project") || content == "" || len(content) > maxMemoryContentBytes || !utf8.ValidString(content) {
		return "", errMemoryInput
	}
	return content, nil
}

// Pin the connection until commit/rollback cleanup finishes. SQLite can retain
// an active transaction after a failed COMMIT, but database/sql marks its Tx
// done even then. Discard that connection before another request can reuse it.
type memoryTransaction struct {
	*sql.Tx
	conn *sql.Conn
}

func (tx *memoryTransaction) Commit() error {
	err := tx.Tx.Commit()
	if err != nil {
		_ = tx.conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = tx.conn.Close()
	return err
}

func (tx *memoryTransaction) Rollback() error {
	err := tx.Tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		_ = tx.conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = tx.conn.Close()
	return err
}

// Acquire SQLite's writer lock before reading limits, including across separate
// accountStore instances/processes. A deferred read-then-write transaction races.
func (s *accountStore) memoryWriteTx(user authUser) (*memoryTransaction, error) {
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	sqlTx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	tx := &memoryTransaction{Tx: sqlTx, conn: conn}
	_, err = tx.Exec(`INSERT INTO memory_settings(user_id,enabled) VALUES(?,1) ON CONFLICT(user_id) DO UPDATE SET enabled=memory_settings.enabled`, user.ID)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func memoryBoundsTx(tx *sql.Tx, userID string) error {
	snapshot, err := memorySnapshotTx(tx, userID)
	if err != nil {
		return err
	}
	if len(snapshot.Entries) > maxMemoryEntries {
		return errMemoryLimit
	}
	// false is one byte longer than true. Reserve it so disabling is always
	// possible; count JSON escaping, metadata, and the response encoder newline.
	snapshot.Enabled = false
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if len(encoded)+1 > maxMemorySnapshotBytes {
		return errMemoryLimit
	}
	return nil
}

// saveMemory is an explicit, reusable server-side tool. It never infers memory
// from messages, and disabling injection does not prevent intentional edits.
func (s *accountStore) saveMemory(user authUser, category, content string) (MemoryEntry, error) {
	return s.writeMemory(user, "", category, content)
}

func (s *accountStore) writeMemory(user authUser, id, category, content string) (MemoryEntry, error) {
	content, err := validatedMemoryContent(category, content)
	if err != nil {
		return MemoryEntry{}, err
	}
	entry := MemoryEntry{ID: id, Category: category, Content: content, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	create := id == ""
	if create {
		entry.ID, err = randomURLToken(24)
		if err != nil {
			return MemoryEntry{}, err
		}
	}
	tx, err := s.memoryWriteTx(user)
	if err != nil {
		return MemoryEntry{}, err
	}
	defer tx.Rollback()
	if create {
		_, err = tx.Exec(`INSERT INTO memory_entries(id,user_id,category,content,updated_at) VALUES(?,?,?,?,?)`, entry.ID, user.ID, category, content, entry.UpdatedAt)
	} else {
		var result sql.Result
		result, err = tx.Exec(`UPDATE memory_entries SET category=?,content=?,updated_at=? WHERE id=? AND user_id=?`, category, content, entry.UpdatedAt, id, user.ID)
		if err == nil {
			err = memoryAffected(result)
		}
	}
	if err != nil {
		return MemoryEntry{}, err
	}
	if err := memoryBoundsTx(tx.Tx, user.ID); err != nil {
		return MemoryEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemoryEntry{}, err
	}
	return entry, nil
}

func memoryAffected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *accountStore) deleteMemory(user authUser, id string) error {
	tx, err := s.memoryWriteTx(user)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM memory_entries WHERE id=? AND user_id=?`, id, user.ID)
	if err != nil {
		return err
	}
	if err := memoryAffected(result); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *accountStore) setMemoryEnabled(user authUser, enabled bool) error {
	tx, err := s.memoryWriteTx(user)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE memory_settings SET enabled=? WHERE user_id=?`, enabled, user.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func registerMemoryRoutes(mux *http.ServeMux, accounts *accountStore) {
	// Set no-store before authentication too, including rejected requests.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		accounts.middleware(http.HandlerFunc(accounts.memoryHandler)).ServeHTTP(w, r)
	})
	for _, pattern := range []string{"GET /api/memories", "POST /api/memories", "PUT /api/memories/{id}", "DELETE /api/memories/{id}", "PUT /api/memory-settings", "GET /api/memories/export"} {
		mux.Handle(pattern, handler)
	}
}

func decodeMemoryJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMemoryRequestBytes))
	if err == nil && utf8.Valid(data) {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(value) == nil && decoder.Decode(new(any)) == io.EOF {
			return true
		}
	}
	writeChatError(w, http.StatusBadRequest, "Send one valid, bounded JSON memory request with no unknown fields.")
	return false
}

func memoryHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errMemoryInput):
		writeChatError(w, http.StatusBadRequest, errMemoryInput.Error())
	case errors.Is(err, errMemoryLimit):
		writeChatError(w, http.StatusConflict, errMemoryLimit.Error())
	case errors.Is(err, sql.ErrNoRows):
		writeChatError(w, http.StatusNotFound, "Memory entry not found.")
	default:
		writeChatError(w, http.StatusServiceUnavailable, "Local memory is unavailable.")
	}
}

func (s *accountStore) memoryHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := currentAuthUser(r.Context())
	if !ok {
		writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && requestIsCrossSite(r) {
		writeChatError(w, http.StatusForbidden, "Cross-site memory requests are not allowed.")
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		snapshot, err := s.memorySnapshot(user)
		if err != nil {
			memoryHTTPError(w, err)
			return
		}
		if r.URL.Path == "/api/memories/export" {
			w.Header().Set("Content-Disposition", `attachment; filename="memories.json"`)
		}
		writeChatJSON(w, http.StatusOK, snapshot)
		return
	}
	if r.URL.Path == "/api/memory-settings" {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if !decodeMemoryJSON(w, r, &body) {
			return
		}
		if body.Enabled == nil {
			writeChatError(w, http.StatusBadRequest, "Send enabled as a boolean.")
			return
		}
		if err := s.setMemoryEnabled(user, *body.Enabled); err != nil {
			memoryHTTPError(w, err)
			return
		}
		writeChatJSON(w, http.StatusOK, map[string]bool{"enabled": *body.Enabled})
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.deleteMemory(user, r.PathValue("id")); err != nil {
			memoryHTTPError(w, err)
			return
		}
		writeChatJSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return
	}
	var body struct {
		Category string `json:"category"`
		Content  string `json:"content"`
	}
	if !decodeMemoryJSON(w, r, &body) {
		return
	}
	var entry MemoryEntry
	var err error
	status := http.StatusOK
	if r.Method == http.MethodPost {
		entry, err = s.saveMemory(user, body.Category, body.Content)
		status = http.StatusCreated
	} else {
		entry, err = s.writeMemory(user, r.PathValue("id"), body.Category, body.Content)
	}
	if err != nil {
		memoryHTTPError(w, err)
		return
	}
	writeChatJSON(w, status, map[string]MemoryEntry{"entry": entry})
}
