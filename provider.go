package main

import (
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

const defaultLLMBaseURL = "https://api.openai.com/v1"

var errModelNotConfigured = errors.New("model provider is not configured")

type modelProvider interface {
	configured() bool
	complete(context.Context, []chatMessage) (string, error)
}

type openAICompatibleProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func newModelProviderFromEnv() *openAICompatibleProvider {
	baseURL := strings.TrimSpace(os.Getenv("LLM_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultLLMBaseURL
	}

	return &openAICompatibleProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		model:   strings.TrimSpace(os.Getenv("LLM_MODEL")),
		client:  &http.Client{Timeout: 90 * time.Second},
	}
}

func (p *openAICompatibleProvider) configured() bool {
	return p != nil && p.baseURL != "" && p.model != ""
}

func (p *openAICompatibleProvider) complete(ctx context.Context, messages []chatMessage) (string, error) {
	if !p.configured() {
		return "", errModelNotConfigured
	}

	payload := struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
	}{Model: p.model, Messages: messages}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", errors.New("model provider returned an unsuccessful status")
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", errors.New("model provider returned no message")
	}
	return result.Choices[0].Message.Content, nil
}
