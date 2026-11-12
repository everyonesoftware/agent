package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

const researchGrantTTL = 5 * time.Minute
const researchTaskTimeout = 90 * time.Second

var errResearchDenied = errors.New("research permission denied")

type researchWebClient interface {
	search(context.Context, string) (string, error)
	fetch(context.Context, string) (string, error)
	searchAvailable() bool
	searchProvider() string
}
type researchTask struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	ExpiresAt       time.Time `json:"expires_at"`
	SearchProvider  string    `json:"search_provider"`
	SearchAvailable bool      `json:"search_available"`
	Scope           string    `json:"scope"`
	owner           string
	digest          [32]byte
	cancel          context.CancelFunc
	web             researchWebClient
	configRevision  string
}
type researchService struct {
	mu       sync.Mutex
	tasks    map[string]*researchTask
	web      researchWebClient
	now      func() time.Time
	settings *researchSettingsService
}

func newResearchService(web researchWebClient) *researchService {
	return &researchService{tasks: map[string]*researchTask{}, web: web, now: time.Now}
}
func researchDigest(request chatRequest) [32]byte {
	request.ResearchTaskID = ""
	b, _ := json.Marshal(request)
	return sha256.Sum256(b)
}
func (s *researchService) register(mux *http.ServeMux, accounts *accountStore) {
	if s.settings != nil {
		s.settings.register(mux)
	}
	mux.Handle("GET /api/research", accounts.middleware(http.HandlerFunc(s.capabilities)))
	mux.Handle("POST /api/research/tasks", accounts.middleware(http.HandlerFunc(s.create)))
	mux.Handle("GET /api/research/tasks/{id}", accounts.middleware(http.HandlerFunc(s.taskHandler)))
	for _, action := range []string{"approve", "stop", "revoke"} {
		mux.Handle("POST /api/research/tasks/{id}/"+action, accounts.middleware(http.HandlerFunc(s.taskHandler)))
	}
}
func (s *researchService) capabilities(w http.ResponseWriter, r *http.Request) {
	user, _ := currentAuthUser(r.Context())
	s.mu.Lock()
	web, _, err := s.webForUserLocked(user.ID)
	s.mu.Unlock()
	if err != nil {
		writeChatError(w, 503, "Research settings are unavailable.")
		return
	}
	writeChatJSON(w, 200, map[string]any{"available": true, "search_available": web.searchAvailable(), "search_provider": web.searchProvider(), "scope": "public_web", "message": "Task-scoped public web research only. Only the final user message is sent, without chat history. Retrieval is available; search requires a configured search provider. No private resources, files, signed-in sites, or publishing. Permission ends when the task finishes or is stopped."})
}
func decodeResearchJSON(w http.ResponseWriter, r *http.Request, value any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		writeChatError(w, 400, "Send one valid JSON request.")
		return false
	}
	return true
}
func (s *researchService) create(w http.ResponseWriter, r *http.Request) {
	if requestIsCrossSite(r) {
		writeChatError(w, 403, "Cross-site research requests are not allowed.")
		return
	}
	var req chatRequest
	if !decodeResearchJSON(w, r, &req, maxChatRequestBytes) {
		return
	}
	if validateChatMessages(req.Messages) != nil || len(req.Model) > 256 || req.ResearchTaskID != "" || (req.RunID != "" && !runIDPattern.MatchString(req.RunID)) || req.OmittedMessages < 0 || req.OmittedMessages > 1000000 {
		writeChatError(w, 400, "Invalid research request.")
		return
	}
	user, _ := currentAuthUser(r.Context())
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, t := range s.tasks {
		if !s.now().Before(t.ExpiresAt) && t.Status != "running" {
			delete(s.tasks, id)
		}
	}
	count := 0
	for _, t := range s.tasks {
		if t.owner == user.ID {
			count++
		}
	}
	if count >= 50 || len(s.tasks) >= 1000 {
		writeChatError(w, 429, "Too many research tasks. Try again later.")
		return
	}
	web, revision, err := s.webForUserLocked(user.ID)
	if err != nil {
		writeChatError(w, 503, "Research settings are unavailable.")
		return
	}
	t := &researchTask{ID: uuid.NewString(), Status: "pending", ExpiresAt: s.now().Add(researchGrantTTL), SearchProvider: web.searchProvider(), SearchAvailable: web.searchAvailable(), Scope: "public_web", owner: user.ID, digest: researchDigest(req), web: web, configRevision: revision}
	s.tasks[t.ID] = t
	writeChatJSON(w, 201, map[string]any{"task": t})
}
func terminalResearch(status string) bool {
	return status != "pending" && status != "approved" && status != "running"
}
func (s *researchService) taskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		if requestIsCrossSite(r) {
			writeChatError(w, 403, "Cross-site research requests are not allowed.")
			return
		}
		var empty struct{}
		if !decodeResearchJSON(w, r, &empty, 1024) {
			return
		}
	}
	user, _ := currentAuthUser(r.Context())
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[r.PathValue("id")]
	if t == nil || t.owner != user.ID {
		writeChatError(w, 404, "Research task not found.")
		return
	}
	if !s.now().Before(t.ExpiresAt) && !terminalResearch(t.Status) {
		t.Status = "expired"
		if t.cancel != nil {
			t.cancel()
		}
	}
	if r.Method == "POST" {
		switch {
		case r.URL.Path == "/api/research/tasks/"+t.ID+"/approve":
			if t.Status != "pending" {
				writeChatError(w, 409, "Research task cannot be approved.")
				return
			}
			t.Status = "approved"
		case !terminalResearch(t.Status):
			t.Status = "cancelled"
			if r.URL.Path == "/api/research/tasks/"+t.ID+"/revoke" {
				t.Status = "revoked"
			}
			if t.cancel != nil {
				t.cancel()
			}
		}
	}
	writeChatJSON(w, 200, map[string]any{"task": t})
}
func (s *researchService) consume(ctx context.Context, user authUser, req chatRequest) (context.Context, *researchTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[req.ResearchTaskID]
	if t == nil || t.owner != user.ID || t.Status != "approved" || !s.now().Before(t.ExpiresAt) || t.digest != researchDigest(req) || !s.taskCredentialValidLocked(t) {
		return nil, nil, errResearchDenied
	}
	ctx, cancel := context.WithDeadline(ctx, minTime(t.ExpiresAt, s.now().Add(researchTaskTimeout)))
	t.cancel = cancel
	t.Status = "running"
	return ctx, t, nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func (s *researchService) commit(ctx context.Context, t *researchTask, save func() error) error {
	// Serialize the final local save with Stop: a cancelled task cannot save a late
	// reply, and Stop after this linearization sees a terminal completed task.
	// No provider or web I/O is performed while this lock is held.
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.Status != "running" || !s.taskCredentialValidLocked(t) {
		return context.Canceled
	}
	if !s.now().Before(t.ExpiresAt) {
		return context.DeadlineExceeded
	}
	err := save()
	t.Status = "completed"
	if err != nil {
		t.Status = "failed"
	}
	return err
}
func (s *researchService) finish(t *researchTask, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.Status == "running" {
		t.Status = "completed"
		if err != nil {
			t.Status = "failed"
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			t.Status = "cancelled"
		}
	}
	if t.cancel != nil {
		t.cancel()
	}
}

type RunResearch struct {
	TaskID        string    `json:"task_id"`
	Scope         string    `json:"scope"`
	ConsentBasis  string    `json:"consent_basis"`
	HistoryPolicy string    `json:"history_policy"`
	ExpiresAt     time.Time `json:"expires_at"`
}
type RunResearchModelCall struct {
	Index             int       `json:"index"`
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	State             string    `json:"state"`
	HTTPStatus        int       `json:"http_status,omitempty"`
	Request           any       `json:"request,omitempty"`
	Response          any       `json:"response,omitempty"`
	RequestTruncated  bool      `json:"request_truncated"`
	ResponseTruncated bool      `json:"response_truncated"`
	Usage             *RunUsage `json:"usage,omitempty"`
}
type RunResearchToolCall struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	State        string `json:"state"`
	ConsentBasis string `json:"consent_basis"`
	Arguments    any    `json:"arguments,omitempty"`
	Result       any    `json:"result,omitempty"`
	Truncated    bool   `json:"truncated"`
}

func (c *runCapture) boundResearchSnapshot() []byte {
	// The shared call budget bounds payload data, but JSON-escaped metadata and
	// backward-compatible top-level snapshots also count toward the hard limit.
	// Prefer dropping the duplicate top-level request; preserve call identities.
	encode := func() []byte { b, _ := json.Marshal(c.run); return b }
	if b := encode(); len(b) <= 128<<10 {
		return b
	}
	if c.run.Request != nil {
		c.run.Request = nil
		c.run.RequestTruncated = true
		c.run.CaptureOmittedFields++
	}
	b := encode()
	if len(b) <= 128<<10 {
		return b
	}
	if c.run.Response != nil {
		c.run.Response = nil
		c.run.ResponseTruncated = true
		c.run.CaptureOmittedFields++
	}
	b = encode()
	if len(b) <= 128<<10 {
		return b
	}
	for i := range c.run.ModelCalls {
		v := &c.run.ModelCalls[i]
		if v.Request != nil {
			v.Request = nil
			v.RequestTruncated = true
			c.run.CaptureOmittedFields++
		}
		if v.Response != nil {
			v.Response = nil
			v.ResponseTruncated = true
			c.run.CaptureOmittedFields++
		}
	}
	for i := range c.run.ToolCalls {
		v := &c.run.ToolCalls[i]
		if v.Arguments != nil || v.Result != nil {
			v.Arguments = nil
			v.Result = nil
			v.Truncated = true
			c.run.CaptureOmittedFields++
		}
	}
	b = encode()
	for len(b) > 128<<10 && len(c.run.Context.Messages) > 0 {
		c.run.Context.Messages = c.run.Context.Messages[:len(c.run.Context.Messages)-1]
		c.run.Context.CaptureOmittedMessages++
		c.run.Context.Truncated = true
		b = encode()
	}
	return b
}

func (c *runCapture) researchValue(value any) (any, bool) {
	b, err := json.Marshal(value)
	if err != nil || len(b) > c.researchBudget {
		c.run.CaptureOmittedFields++
		return nil, true
	}
	c.researchBudget -= len(b)
	return value, false
}
func traceResearchModelStart(ctx context.Context, provider, model string, body []byte) {
	if c := captureFrom(ctx); c != nil && c.run.Research != nil {
		var v any
		json.Unmarshal(body, &v)
		value, truncated := c.researchValue(v)
		m, _ := boundedTraceText(model)
		if len(m) > 256 {
			m = m[:256]
		}
		c.run.ModelCalls = append(c.run.ModelCalls, RunResearchModelCall{Index: len(c.run.ModelCalls) + 1, Provider: provider, Model: m, State: "running", Request: value, RequestTruncated: truncated})
		c.persist()
	}
}
func traceResearchModelEnd(ctx context.Context, turn researchTurn, err error) {
	if c := captureFrom(ctx); c != nil && len(c.run.ModelCalls) > 0 {
		call := &c.run.ModelCalls[len(c.run.ModelCalls)-1]
		call.State = "completed"
		call.HTTPStatus = c.run.HTTPStatus
		if err != nil {
			call.State = "failed"
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				call.State = "cancelled"
			}
		} else {
			call.Response, call.ResponseTruncated = c.researchValue(map[string]any{"text": turn.Text, "tool_calls": turn.Calls})
			if u := turn.Usage; u != nil && u.InputTokens >= 0 && u.OutputTokens >= 0 && u.TotalTokens >= 0 && u.InputTokens <= 1000000000 && u.OutputTokens <= 1000000000 && u.TotalTokens <= 1000000000 {
				call.Usage = u
				if c.run.Usage == nil {
					c.run.Usage = &RunUsage{}
				}
				c.run.Usage.InputTokens += u.InputTokens
				c.run.Usage.OutputTokens += u.OutputTokens
				c.run.Usage.TotalTokens += u.TotalTokens
			}
		}
		for i := len(c.run.Events) - 1; i >= 0; i-- {
			if c.run.Events[i].Name == "provider" && c.run.Events[i].State == "running" {
				c.run.Events[i].State = call.State
				break
			}
		}
		c.persist()
	}
}
func traceResearchTool(ctx context.Context, call researchCall, output, state string) {
	if c := captureFrom(ctx); c != nil && c.run.Research != nil {
		if state == "running" || state == "rejected" {
			id, _ := boundedTraceText(call.ID)
			if len(id) > 128 {
				id = id[:128]
			}
			name := call.Name
			if name != "web_search" && name != "web_fetch" {
				name = "unknown"
			}
			args, truncated := c.researchValue(call.Arguments)
			c.run.ToolCalls = append(c.run.ToolCalls, RunResearchToolCall{ID: id, Name: name, State: state, ConsentBasis: "explicit_task_approval", Arguments: args, Truncated: truncated})
			c.run.Events = append(c.run.Events, RunEvent{"tool", name, time.Now(), state})
		} else if len(c.run.ToolCalls) > 0 {
			v := &c.run.ToolCalls[len(c.run.ToolCalls)-1]
			v.State = state
			value, truncated := c.researchValue(output)
			v.Result = value
			v.Truncated = v.Truncated || truncated
			for i := len(c.run.Events) - 1; i >= 0; i-- {
				if c.run.Events[i].Name == "tool" && c.run.Events[i].State == "running" {
					c.run.Events[i].State = state
					break
				}
			}
		}
		c.persist()
	}
}
