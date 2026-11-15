package main

// Historical SSE parser fixtures are test-only. There is no Responses transport
// or SSE parser in the production ChatGPT integration.
import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
)

type responsesProvider = codexProvider

func collectResponseText(response *http.Response) (string, error) {
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 32<<10), 4<<20)
	lines := []string{}
	var output strings.Builder
	done := ""
	completed := false
	process := func() error {
		if len(lines) == 0 {
			return nil
		}
		data := strings.Join(lines, "\n")
		lines = nil
		if data == "[DONE]" {
			return nil
		}
		var e struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Text  string `json:"text"`
			Error struct {
				Type  string
				Code  string
				Param string
			}
			Response struct {
				Usage *RunUsage
				Error struct {
					Type  string
					Code  string
					Param string
				}
				Output []struct {
					Content []struct {
						Type string
						Text string
					}
				}
			}
		}
		if json.Unmarshal([]byte(data), &e) != nil {
			return &providerFailure{kind: "responses_event_decode", status: response.StatusCode}
		}
		switch e.Type {
		case "response.output_text.delta":
			output.WriteString(e.Delta)
		case "response.output_text.done":
			done = e.Text
		case "response.completed":
			completed = true
			if response.Request != nil {
				traceUsage(response.Request.Context(), e.Response.Usage)
			}
			if output.Len() == 0 && done == "" {
				for _, item := range e.Response.Output {
					for _, part := range item.Content {
						if part.Type == "output_text" {
							done += part.Text
						}
					}
				}
			}
		case "response.failed", "response.incomplete", "error":
			f := e.Error
			if f.Type == "" && f.Code == "" {
				f = e.Response.Error
			}
			if e.Type == "response.incomplete" && f.Code == "" {
				f.Code = "response_incomplete"
			}
			return &providerFailure{kind: "responses_stream", status: response.StatusCode, requestID: safeDiagnosticToken(response.Header.Get("X-Request-Id")), errorType: safeDiagnosticToken(f.Type), errorCode: safeDiagnosticToken(f.Code), errorParam: safeDiagnosticParam(f.Param), errorShape: "stream_event"}
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := process(); err != nil {
				return "", err
			}
		} else if v, ok := strings.CutPrefix(line, "data:"); ok {
			lines = append(lines, strings.TrimSpace(v))
		}
	}
	if scanner.Err() != nil {
		return "", &providerFailure{kind: "responses_stream_read", status: response.StatusCode}
	}
	if err := process(); err != nil {
		return "", err
	}
	if !completed {
		return "", &providerFailure{kind: "responses_incomplete", status: response.StatusCode}
	}
	if output.Len() > 0 {
		return output.String(), nil
	}
	if strings.TrimSpace(done) != "" {
		return done, nil
	}
	return "", &providerFailure{kind: "responses_empty", status: response.StatusCode}
}
