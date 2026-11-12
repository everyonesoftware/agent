package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxRunsPerUser = 50
const runRetention = 7 * 24 * time.Hour
const maxTraceFieldBytes = 32 << 10

var runIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var errRunExists = errors.New("run already exists")

type RunContext struct {
	CurrentDate            string        `json:"current_date"`
	Timezone               string        `json:"timezone"`
	UTCOffset              string        `json:"utc_offset"`
	InstructionsVersion    string        `json:"instructions_version"`
	Instructions           string        `json:"instructions"`
	Messages               []chatMessage `json:"messages"`
	OmittedMessages        int           `json:"omitted_messages"`
	CaptureOmittedMessages int           `json:"capture_omitted_messages"`
	Truncated              bool          `json:"truncated"`
	Tools                  []any         `json:"tools"`
}
type RunEvent struct {
	Name   string    `json:"name"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
	State  string    `json:"state"`
}
type RunUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
type Run struct {
	ID                string     `json:"id"`
	ConversationID    string     `json:"conversation_id,omitempty"`
	State             string     `json:"state"`
	StartedAt         time.Time  `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	DurationMS        int64      `json:"duration_ms"`
	Provider          string     `json:"provider"`
	Model             string     `json:"model"`
	Context           RunContext `json:"context"`
	Events            []RunEvent `json:"events"`
	Request           any        `json:"request,omitempty"`
	Response          any        `json:"response,omitempty"`
	RequestTruncated  bool       `json:"request_truncated"`
	ResponseTruncated bool       `json:"response_truncated"`
	Error             string     `json:"error,omitempty"`
	Usage             *RunUsage  `json:"usage,omitempty"`
	HTTPStatus        int        `json:"http_status,omitempty"`
	Retries           int        `json:"retries"`
}
type runCapture struct {
	store  *accountStore
	userID string
	run    Run
}
type runContextKey struct{}

func migrateRuns(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS runs(id TEXT PRIMARY KEY,user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,conversation_id TEXT NOT NULL DEFAULT '',started_at INTEGER NOT NULL,state TEXT NOT NULL,data BLOB NOT NULL); CREATE INDEX IF NOT EXISTS runs_user_started ON runs(user_id,started_at DESC);`); err != nil {
		return err
	}
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "run_id" {
			found = true
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !found {
		if _, err = db.Exec(`ALTER TABLE messages ADD COLUMN run_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	rows, err = db.Query(`SELECT data FROM runs WHERE state='running'`)
	if err != nil {
		return err
	}
	var interrupted []Run
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		var run Run
		if json.Unmarshal(b, &run) == nil {
			interrupted = append(interrupted, run)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, run := range interrupted {
		now := time.Now()
		run.State = "interrupted"
		run.FinishedAt = &now
		run.DurationMS = now.Sub(run.StartedAt).Milliseconds()
		run.Error = "The server stopped before this run finished."
		for i := range run.Events {
			if run.Events[i].State == "running" {
				run.Events[i].State = "failed"
			}
		}
		run.Events = append(run.Events, RunEvent{"interrupted", "Server restart", now, "failed"})
		b, _ := json.Marshal(run)
		if _, err = db.Exec(`UPDATE runs SET state=?,data=? WHERE id=?`, run.State, b, run.ID); err != nil {
			return err
		}
	}
	_, err = db.Exec(`DELETE FROM runs WHERE started_at<?`, time.Now().Add(-runRetention).UnixNano())
	return err
}
func (s *accountStore) createRun(user authUser, id, conversationID string, messages []chatMessage, now time.Time, omitted int) (*runCapture, error) {
	if id == "" {
		id = uuid.NewString()
	}
	c := &runCapture{store: s, userID: user.ID, run: Run{ID: id, ConversationID: conversationID, State: "running", StartedAt: now, Context: RunContext{CurrentDate: now.Format("2006-01-02"), Timezone: now.Location().String(), UTCOffset: now.Format("-07:00"), InstructionsVersion: instructionsVersion, Instructions: messages[0].Content, Tools: []any{}, Messages: []chatMessage{}, OmittedMessages: omitted}, Events: []RunEvent{{"context", "Trusted runtime context assembled", now, "completed"}}}}
	budget := maxTraceFieldBytes
	for _, m := range messages {
		encoded, _ := json.Marshal(m)
		if len(encoded) > budget {
			c.run.Context.CaptureOmittedMessages++
			c.run.Context.Truncated = true
			c.run.RequestTruncated = true
			continue
		}
		c.run.Context.Messages = append(c.run.Context.Messages, m)
		budget -= len(encoded)
	}
	b, _ := json.Marshal(c.run)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existing int
	if err = tx.QueryRow(`SELECT 1 FROM runs WHERE id=?`, id).Scan(&existing); err == nil {
		return nil, errRunExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if _, err = tx.Exec(`INSERT INTO runs(id,user_id,conversation_id,started_at,state,data) VALUES(?,?,?,?,?,?)`, id, user.ID, conversationID, now.UnixNano(), "running", b); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`DELETE FROM runs WHERE user_id=? AND (started_at<? OR id NOT IN (SELECT id FROM runs WHERE user_id=? ORDER BY started_at DESC LIMIT ?))`, user.ID, now.Add(-runRetention).UnixNano(), user.ID, maxRunsPerUser); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return c, nil
}
func (c *runCapture) persist() {
	b, err := json.Marshal(c.run)
	if err == nil {
		_, _ = c.store.db.Exec(`UPDATE runs SET conversation_id=?,state=?,data=? WHERE id=? AND user_id=?`, c.run.ConversationID, c.run.State, b, c.run.ID, c.userID)
	}
}
func captureFrom(ctx context.Context) *runCapture {
	c, _ := ctx.Value(runContextKey{}).(*runCapture)
	return c
}
func traceRequest(ctx context.Context, provider, model string, body []byte) {
	if c := captureFrom(ctx); c != nil {
		c.run.Provider = provider
		c.run.Model = model
		if len(c.run.Model) > 256 {
			c.run.Model = c.run.Model[:256]
			for !utf8.ValidString(c.run.Model) {
				c.run.Model = c.run.Model[:len(c.run.Model)-1]
			}
			c.run.RequestTruncated = true
		}
		if len(body) <= maxTraceFieldBytes {
			var v any
			if json.Unmarshal(body, &v) == nil {
				c.run.Request = v
			}
		} else {
			c.run.RequestTruncated = true
		}
		c.run.Events = append(c.run.Events, RunEvent{"provider", "Model request sent", time.Now(), "running"})
		c.persist()
	}
}
func traceStatus(ctx context.Context, status int) {
	if c := captureFrom(ctx); c != nil {
		c.run.HTTPStatus = status
	}
}
func traceUsage(ctx context.Context, u *RunUsage) {
	if c := captureFrom(ctx); c != nil && u != nil {
		c.run.Usage = u
	}
}
func boundedTraceText(s string) (string, bool) {
	originalBytes := len(s)
	if len(s) > maxTraceFieldBytes {
		s = s[:maxTraceFieldBytes]
	}
	for {
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		encoded, _ := json.Marshal(s)
		if len(encoded) <= maxTraceFieldBytes {
			return s, len(s) != originalBytes
		}
		s = s[:len(s)/2]
	}
}
func (c *runCapture) finish(reply string, err error) {
	now := time.Now()
	c.run.FinishedAt = &now
	c.run.DurationMS = now.Sub(c.run.StartedAt).Milliseconds()
	c.run.State = "completed"
	detail := "Reply generated"
	for i := range c.run.Events {
		if c.run.Events[i].State == "running" {
			c.run.Events[i].State = "completed"
			if err != nil {
				c.run.Events[i].State = "failed"
			}
		}
	}
	if err != nil {
		c.run.State = "failed"
		c.run.Error = "The request could not be completed."
		detail = "Request failed"
	} else {
		text, truncated := boundedTraceText(reply)
		c.run.ResponseTruncated = truncated
		c.run.Response = map[string]any{"text": text}
		if c.run.Usage != nil {
			c.run.Response.(map[string]any)["usage"] = c.run.Usage
		}
	}
	c.run.Events = append(c.run.Events, RunEvent{c.run.State, detail, now, c.run.State})
	c.persist()
}
func (s *accountStore) runHandler(w http.ResponseWriter, r *http.Request) {
	user, _ := currentAuthUser(r.Context())
	var b []byte
	if err := s.db.QueryRow(`SELECT data FROM runs WHERE id=? AND user_id=? AND started_at>=?`, r.PathValue("id"), user.ID, time.Now().Add(-runRetention).UnixNano()).Scan(&b); err != nil {
		writeChatError(w, 404, "Run not found.")
		return
	}
	var run Run
	if json.Unmarshal(b, &run) != nil {
		writeChatError(w, 404, "Run not found.")
		return
	}
	if run.State == "running" {
		run.DurationMS = time.Since(run.StartedAt).Milliseconds()
	}
	writeChatJSON(w, 200, map[string]any{"run": run})
}
