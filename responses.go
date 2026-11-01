package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type modelOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type responsesProvider struct {
	auth         *siwcService
	defaultModel string
}

func (p *responsesProvider) configured() bool {
	if p == nil || p.auth == nil {
		return false
	}
	credentials, err := p.auth.store.Load()
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	return credentialsUseOAuth(credentials)
}

func (p *responsesProvider) models(ctx context.Context) ([]modelOption, error) {
	if p == nil || p.auth == nil {
		return nil, errChatGPTNotConnected
	}
	return p.auth.availableModels(ctx)
}

func (p *responsesProvider) complete(ctx context.Context, messages []chatMessage, requestedModel string) (string, error) {
	if p == nil || p.auth == nil || p.auth.store == nil {
		return "", errChatGPTNotConnected
	}
	profileID, err := p.auth.store.ActiveProfileID()
	if err != nil {
		return "", &providerFailure{kind: "oauth_credentials"}
	}
	if profileID == "" {
		return "", errChatGPTNotConnected
	}
	models, err := p.auth.availableModelsForProfile(ctx, profileID)
	if err != nil {
		return "", err
	}
	model := requestedModel
	if model == "" {
		for _, available := range models {
			if available.ID == p.defaultModel {
				model = available.ID
				break
			}
		}
		if model == "" {
			model = models[0].ID
		}
	}
	if !containsModel(models, model) {
		return "", &providerFailure{kind: "model_unavailable"}
	}

	accessToken, err := p.auth.accessTokenForProfile(ctx, profileID)
	if err != nil {
		return "", err
	}
	payload := struct {
		Model  string        `json:"model"`
		Input  []chatMessage `json:"input"`
		Store  bool          `json:"store"`
		Stream bool          `json:"stream"`
	}{Model: model, Input: messages, Store: false, Stream: true}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", &providerFailure{kind: "responses_request_encode"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.auth.config.apiBaseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", &providerFailure{kind: "responses_request_build"}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	response, err := p.auth.client.Do(request)
	if err != nil {
		return "", &providerFailure{kind: "responses_transport"}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", readSafeAPIError(response, "responses_http")
	}
	return collectResponseText(response)
}

func (s *siwcService) availableModels(ctx context.Context) ([]modelOption, error) {
	profileID, err := s.store.ActiveProfileID()
	if err != nil {
		return nil, &providerFailure{kind: "oauth_credentials"}
	}
	if profileID == "" {
		return nil, errChatGPTNotConnected
	}
	return s.availableModelsForProfile(ctx, profileID)
}

func (s *siwcService) availableModelsForProfile(ctx context.Context, profileID string) ([]modelOption, error) {
	s.modelsMu.Lock()
	if s.modelsProfileID == profileID && len(s.models) > 0 && s.now().Sub(s.modelsAt) < 5*time.Minute {
		models := append([]modelOption(nil), s.models...)
		s.modelsMu.Unlock()
		return models, nil
	}
	s.modelsMu.Unlock()

	accessToken, err := s.accessTokenForProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.config.apiBaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, &providerFailure{kind: "models_request_build"}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, &providerFailure{kind: "models_transport"}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, readSafeAPIError(response, "models_http")
	}
	var result struct {
		Data   []apiModel `json:"data"`
		Models []apiModel `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result); err != nil {
		return nil, &providerFailure{kind: "models_decode", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id"))}
	}
	items := result.Data
	if len(items) == 0 {
		items = result.Models
	}
	models := make([]modelOption, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.Visibility != "list" {
			continue
		}
		id := firstModelValue(item.Slug, item.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		name := firstModelValue(item.DisplayName, item.Name, id)
		models = append(models, modelOption{ID: id, Name: name})
	}
	if len(models) == 0 {
		return nil, &providerFailure{kind: "models_empty", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id"))}
	}
	s.modelsMu.Lock()
	s.models, s.modelsAt, s.modelsProfileID = append([]modelOption(nil), models...), s.now(), profileID
	s.modelsMu.Unlock()
	return models, nil
}

type apiModel struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Visibility  string `json:"visibility"`
}

func firstModelValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func containsModel(models []modelOption, id string) bool {
	for _, model := range models {
		if model.ID == id {
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
	var providerError struct {
		Type  string `json:"type"`
		Code  string `json:"code"`
		Param string `json:"param"`
	}
	if len(body.Error) > 0 {
		shape = "error_field"
		if json.Unmarshal(body.Error, &providerError) == nil && len(body.Error) > 1 && body.Error[0] == '{' {
			shape = "error_object"
		}
	}
	if len(body.Detail) > 0 {
		shape = "detail"
	}
	return &providerFailure{
		kind: kind, status: response.StatusCode,
		requestID:  safeDiagnosticToken(response.Header.Get("X-Request-Id")),
		errorType:  safeDiagnosticToken(providerError.Type),
		errorCode:  safeDiagnosticToken(providerError.Code),
		errorParam: safeDiagnosticParam(providerError.Param), errorShape: shape,
	}
}

func collectResponseText(response *http.Response) (string, error) {
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 32<<10), 4<<20)
	var dataLines []string
	var output strings.Builder
	var completedText string
	completed := false

	processEvent := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if data == "[DONE]" {
			return nil
		}
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Text  string `json:"text"`
			Error struct {
				Type  string `json:"type"`
				Code  string `json:"code"`
				Param string `json:"param"`
			} `json:"error"`
			Response struct {
				Error struct {
					Type  string `json:"type"`
					Code  string `json:"code"`
					Param string `json:"param"`
				} `json:"error"`
				Output []struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return &providerFailure{kind: "responses_event_decode", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id"))}
		}
		switch event.Type {
		case "response.output_text.delta":
			output.WriteString(event.Delta)
		case "response.output_text.done":
			completedText = event.Text
		case "response.completed":
			completed = true
			if output.Len() == 0 && completedText == "" {
				for _, item := range event.Response.Output {
					for _, content := range item.Content {
						if content.Type == "output_text" {
							completedText += content.Text
						}
					}
				}
			}
		case "response.failed", "response.incomplete", "error":
			failure := event.Error
			if failure.Type == "" && failure.Code == "" {
				failure = event.Response.Error
			}
			if event.Type == "response.incomplete" && failure.Code == "" {
				failure.Code = "response_incomplete"
			}
			return &providerFailure{
				kind: "responses_stream", status: response.StatusCode,
				requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id")),
				errorType: safeDiagnosticToken(failure.Type), errorCode: safeDiagnosticToken(failure.Code),
				errorParam: safeDiagnosticParam(failure.Param), errorShape: "stream_event",
			}
		}
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := processEvent(); err != nil {
				return "", err
			}
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimSpace(value))
		}
	}
	if err := scanner.Err(); err != nil {
		return "", &providerFailure{kind: "responses_stream_read", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id"))}
	}
	if err := processEvent(); err != nil {
		return "", err
	}
	if !completed {
		return "", &providerFailure{kind: "responses_incomplete", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id"))}
	}
	if output.Len() > 0 {
		return output.String(), nil
	}
	if strings.TrimSpace(completedText) != "" {
		return completedText, nil
	}
	return "", &providerFailure{kind: "responses_empty", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id"))}
}
