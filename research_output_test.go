package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Exercise the actual provider loop and inspect the tool message it sends back.
func researchOutputLoop(t *testing.T, raw, tool, nextURL string) (string, error, int32) {
	t.Helper()
	output := ""
	step := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		step++
		callTool, args := tool, `{"url":"https://example.com/"}`
		if tool == "web_search" {
			args = `{"query":"fixture evidence"}`
		}
		if step > 1 {
			for _, m := range request.Messages {
				if m.Role == "tool" {
					output = m.Content
				}
			}
			if nextURL == "" {
				fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture answer"}}]}`)
				return
			}
			callTool = "web_fetch"
			encoded, _ := json.Marshal(map[string]string{"url": nextURL})
			args = string(encoded)
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`, fmt.Sprint(step), callTool, args)
	}))
	defer upstream.Close()
	web := &researchFixtureWeb{fetchFn: func(context.Context, string) (string, error) { return raw, nil }, searchFn: func(context.Context, string) (string, error) { return raw, nil }}
	s := newResearchService(web)
	task := &researchTask{Status: "running", ExpiresAt: time.Now().Add(time.Minute), SearchAvailable: true}
	_, err := s.run(context.Background(), task, &openAICompatibleProvider{baseURL: upstream.URL, model: "fixture", client: upstream.Client()}, []chatMessage{{Role: "user", Content: "Read https://example.com/"}}, "")
	return output, err, web.calls.Load()
}

func TestResearchOutputPreservesSmallSearch(t *testing.T) {
	const raw = `{"results":[{"url":"https://example.com/source?a=1&b=2","title":"Actual title 界"}]}`
	output, err := researchBoundOutput("web_search", raw)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	json.Unmarshal([]byte(raw), &want)
	json.Unmarshal([]byte(output), &got)
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if string(wantJSON) != string(gotJSON) {
		t.Fatal("source metadata changed", output)
	}
}

func TestResearchOutputRejectsUnsafeSuccess(t *testing.T) {
	const hidden = "https://hidden.example/evidence"
	oversizedMetadata, _ := json.Marshal(map[string]string{"url": hidden, "title": strings.Repeat("T", maxResearchOutputBytes), "content": "evidence"})
	oversizedSearch, _ := json.Marshal(map[string]any{"results": []any{map[string]string{"url": hidden, "title": "actual", "description": strings.Repeat("界", maxResearchOutputBytes)}}})
	for _, tc := range []struct{ name, tool, raw string }{
		{"invalid fetch", "web_fetch", strings.Repeat("x", maxResearchOutputBytes) + hidden},
		{"unfittable metadata", "web_fetch", string(oversizedMetadata)},
		{"invalid search", "web_search", strings.Repeat("x", maxResearchOutputBytes) + hidden},
		{"oversized search", "web_search", string(oversizedSearch)},
		{"invalid small search", "web_search", hidden},
		{"wrong search schema", "web_search", `{"results":"` + hidden + `"}`},
		{"invalid search URL", "web_search", `{"results":[{"url":"http://127.0.0.1/","title":"bad","description":"` + hidden + `"}]}`},
		{"oversized parse input", "web_search", strings.Repeat(" ", (1<<20)+1) + `{"results":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err, calls := researchOutputLoop(t, tc.raw, tc.tool, hidden)
			if err != errResearchDenied || calls != 1 {
				t.Fatalf("failed output authorized a URL: err=%v calls=%d", err, calls)
			}
			var got map[string]any
			if !utf8.ValidString(output) || json.Unmarshal([]byte(output), &got) != nil || got["error"] == nil || len(output) > maxResearchOutputBytes || strings.Contains(output, hidden) {
				t.Fatalf("unsafe success instead of clear failure: %d bytes", len(output))
			}
		})
	}
}

func TestResearchOutputOmittedURLsNotAuthorized(t *testing.T) {
	const hidden = "https://omitted.example/evidence"
	raw, _ := json.Marshal(map[string]string{"url": "https://example.com/actual", "title": "Actual source", "content": strings.Repeat("界", 24000) + " " + hidden})
	output, err, calls := researchOutputLoop(t, string(raw), "web_fetch", hidden)
	if err != errResearchDenied || calls != 1 {
		t.Fatalf("omitted URL authorized: err=%v calls=%d", err, calls)
	}
	var got struct{ Truncated bool }
	if json.Unmarshal([]byte(output), &got) != nil || !got.Truncated || strings.Contains(output, hidden) {
		t.Fatal("omission was not explicit")
	}
}

func TestResearchOutputFetchBoundedJSON(t *testing.T) {
	for _, content := range []string{strings.Repeat("界🙂", 24000), strings.Repeat("\x01\"\\<>&\n", 12000)} {
		t.Run(fmt.Sprintf("rune_%x", []rune(content)[0]), func(t *testing.T) {
			const source = "https://example.com/actual?a=1&b=2"
			const title = "Actual title 界 <&>"
			raw, _ := json.Marshal(map[string]string{"url": source, "title": title, "content": content})
			output, err, _ := researchOutputLoop(t, string(raw), "web_fetch", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(output) > maxResearchOutputBytes || !utf8.ValidString(output) || !json.Valid([]byte(output)) {
				t.Fatalf("unsafe tool output: %d bytes, UTF8=%v, JSON=%v", len(output), utf8.ValidString(output), json.Valid([]byte(output)))
			}
			var got struct {
				URL, Title, Content string
				Truncated           bool
			}
			if err := json.Unmarshal([]byte(output), &got); err != nil {
				t.Fatal(err)
			}
			if got.URL != source || got.Title != title {
				t.Fatalf("source metadata changed: URL=%q title=%q", got.URL, got.Title)
			}
			if !got.Truncated || got.Content == "" || got.Content == content || !strings.HasPrefix(content, got.Content) {
				t.Fatalf("missing explicit rune-safe truncation: truncated=%v content bytes=%d", got.Truncated, len(got.Content))
			}
		})
	}
}
