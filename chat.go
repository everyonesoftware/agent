package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const (
	maxChatRequestBytes = 64 << 10
	maxChatMessages     = 40
	maxChatMessageBytes = 8 << 10
)

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Messages []chatMessage `json:"messages"`
}

func chatHandler(provider modelProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !provider.configured() {
			writeChatError(w, http.StatusServiceUnavailable, "Configure LLM_MODEL and the provider settings in the container environment before chatting.")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxChatRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request chatRequest
		if err := decoder.Decode(&request); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeChatError(w, http.StatusRequestEntityTooLarge, "The conversation is too large. Start a new chat or shorten the messages.")
				return
			}
			writeChatError(w, http.StatusBadRequest, "Send a valid JSON chat request.")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			writeChatError(w, http.StatusBadRequest, "Send one JSON chat request.")
			return
		}
		if err := validateChatMessages(request.Messages); err != nil {
			writeChatError(w, http.StatusBadRequest, err.Error())
			return
		}

		reply, err := provider.complete(r.Context(), request.Messages)
		if err != nil {
			writeChatError(w, http.StatusBadGateway, "The model provider could not complete this request.")
			return
		}
		writeChatJSON(w, http.StatusOK, map[string]string{"reply": reply})
	}
}

func validateChatMessages(messages []chatMessage) error {
	if len(messages) == 0 || len(messages) > maxChatMessages {
		return errors.New("The conversation must contain between 1 and 40 messages.")
	}
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			return errors.New("Messages may only have user or assistant roles.")
		}
		if strings.TrimSpace(message.Content) == "" {
			return errors.New("Messages cannot be empty.")
		}
		if len(message.Content) > maxChatMessageBytes {
			return errors.New("Each message must be 8 KB or less.")
		}
	}
	if messages[len(messages)-1].Role != "user" {
		return errors.New("The latest message must be from the user.")
	}
	return nil
}

func writeChatError(w http.ResponseWriter, status int, message string) {
	writeChatJSON(w, status, map[string]string{"error": message})
}

func writeChatJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
