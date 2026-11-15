package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type modelOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ChatGPT operations use Codex app-server RPC; Agent never handles live tokens.
type codexProvider struct {
	auth         *siwcService
	defaultModel string
}

func (p *codexProvider) configured() bool {
	return p != nil && p.auth != nil && p.auth.codex != nil
}
func (p *codexProvider) complete(ctx context.Context, messages []chatMessage, model string) (string, error) {
	return p.codexComplete(ctx, messages, model)
}
func (p *codexProvider) models(ctx context.Context) ([]modelOption, error) {
	if p == nil || p.auth == nil {
		return nil, errChatGPTNotConnected
	}
	return p.auth.availableModels(ctx)
}
func (s *siwcService) availableModels(ctx context.Context) ([]modelOption, error) {
	id, err := s.store.ActiveProfileID()
	if err != nil || id == "" {
		return nil, errChatGPTNotConnected
	}
	return s.codexModels(ctx, id)
}
func (s *siwcService) availableModelsForProfile(ctx context.Context, id string) ([]modelOption, error) {
	return s.codexModels(ctx, id)
}
func firstModelValue(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func containsModel(models []modelOption, id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}
	return false
}
func readSafeAPIError(response *http.Response, kind string) error {
	var body struct {
		Error  json.RawMessage `json:"error"`
		Detail json.RawMessage `json:"detail"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&body)
	shape := "unknown"
	var e struct {
		Type  string `json:"type"`
		Code  string `json:"code"`
		Param string `json:"param"`
	}
	if len(body.Error) > 0 {
		shape = "error_field"
		if json.Unmarshal(body.Error, &e) == nil && len(body.Error) > 1 && body.Error[0] == '{' {
			shape = "error_object"
		}
	}
	if len(body.Detail) > 0 {
		shape = "detail"
	}
	return &providerFailure{kind: kind, status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id")), errorType: safeDiagnosticToken(e.Type), errorCode: safeDiagnosticToken(e.Code), errorParam: safeDiagnosticParam(e.Param), errorShape: shape}
}
