package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
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
	Messages       []chatMessage `json:"messages"`
	Model          string        `json:"model"`
	ConversationID string        `json:"conversation_id,omitempty"`
}

func chatHandler(provider modelProvider) http.HandlerFunc {
	return chatHandlerWithStore(provider, nil)
}

func chatHandlerWithStore(provider modelProvider, accounts *accountStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !provider.configured() {
			writeChatError(w, http.StatusServiceUnavailable, "Connect ChatGPT from a browser on the Agent host, or configure LLM_MODEL and provider settings.")
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
		if len(request.Model) > 256 {
			writeChatError(w, http.StatusBadRequest, "Choose a model from the available model list.")
			return
		}

		reply, err := provider.complete(r.Context(), request.Messages, request.Model)
		if err != nil {
			if errors.Is(err, errModelNotConfigured) {
				writeChatError(w, http.StatusServiceUnavailable, "Connect your ChatGPT account or configure LLM_MODEL and provider settings.")
				return
			}
			logProviderFailure(err)
			writeChatProviderError(w, err)
			return
		}
		result := map[string]string{"reply": reply}
		if accounts != nil {
			user, ok := currentAuthUser(r.Context())
			if !ok {
				writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
				return
			}
			conversationID, saveErr := accounts.saveExchange(user, request.ConversationID, []chatMessage{request.Messages[len(request.Messages)-1], {Role: "assistant", Content: reply}})
			if saveErr != nil {
				writeChatError(w, http.StatusInternalServerError, "The reply was generated, but the conversation could not be saved.")
				return
			}
			result["conversation_id"] = conversationID
		}
		writeChatJSON(w, http.StatusOK, result)
	}
}

func logProviderFailure(err error) {
	var failure *providerFailure
	if !errors.As(err, &failure) {
		log.Print("chat provider failure kind=internal")
		return
	}
	log.Printf("chat provider failure kind=%s status=%d request_id=%q error_type=%q error_code=%q error_param=%q error_shape=%q", failure.kind, failure.status, failure.requestID, failure.errorType, failure.errorCode, failure.errorParam, failure.errorShape)
}

func writeChatProviderError(w http.ResponseWriter, err error) {
	var failure *providerFailure
	if !errors.As(err, &failure) {
		writeChatError(w, http.StatusBadGateway, "The model provider could not complete this request.")
		return
	}
	switch failure.errorCode {
	case "subscription_sharing_usage_limit_exceeded":
		writeChatJSON(w, http.StatusTooManyRequests, map[string]string{
			"error":  "The ChatGPT plan usage limit has been reached.",
			"action": "manage_usage",
		})
		return
	case "subscription_sharing_usage_unavailable":
		w.Header().Set("Retry-After", "5")
		writeChatError(w, http.StatusServiceUnavailable, "ChatGPT plan usage is temporarily unavailable. Try again shortly.")
		return
	case "subscription_sharing_user_unavailable":
		w.Header().Set("Retry-After", "5")
		writeChatError(w, http.StatusServiceUnavailable, "ChatGPT could not check this user's plan usage right now. Credentials were kept; try again later.")
		return
	case "subscription_sharing_user_not_eligible":
		writeChatError(w, http.StatusForbidden, "This ChatGPT account is not eligible for plan usage in this app. Check account and workspace eligibility.")
		return
	case "subscription_sharing_unsupported_capability":
		writeChatError(w, http.StatusBadRequest, "This model or request feature is not supported for ChatGPT plan usage. Change the selection or request before trying again.")
		return
	case "subscription_sharing_route_not_supported":
		writeChatError(w, http.StatusForbidden, "This API route is not supported for ChatGPT plan usage. Use the supported Responses API route.")
		return
	case "subscription_sharing_invalid_user":
		writeChatError(w, http.StatusUnauthorized, "ChatGPT could not validate the subscriber context. The connection was kept; check the server request ID and reconnect only after confirming revocation or a terminal refresh error.")
		return
	case "chatpass_v2_scope_not_authorized", "chatpass_v2_invalid_authorization_context":
		writeChatError(w, http.StatusForbidden, "The ChatGPT permission context does not authorize this operation. Check the connection and grant configuration.")
		return
	}
	if failure.kind == "oauth_reauthorize" || (failure.kind == "oauth_refresh" && isTerminalRefreshError(err)) {
		writeChatJSON(w, http.StatusUnauthorized, map[string]string{
			"error":  "The ChatGPT session expired or was revoked. Continue with ChatGPT to sign in again.",
			"action": "reauthorize",
		})
		return
	}
	if failure.kind == "oauth_scope_unavailable" {
		writeChatError(w, http.StatusForbidden, "ChatGPT is connected, but plan-usage permission was not granted. Continue with ChatGPT on the host to review access.")
		return
	}
	if failure.status < http.StatusBadRequest || failure.status > 599 {
		writeChatError(w, http.StatusBadGateway, "The model provider could not complete this request.")
		return
	}
	message := "The model provider rejected this request."
	switch failure.status {
	case http.StatusUnauthorized:
		if isChatGPTProviderFailure(failure.kind) {
			message = "ChatGPT could not validate the current session. Retry or reconnect your account."
		} else {
			message = "The configured model provider rejected authentication. Check its credentials."
		}
	case http.StatusForbidden:
		if isChatGPTProviderFailure(failure.kind) {
			message = "ChatGPT denied this request because of account eligibility or permissions."
		} else {
			message = "The configured model provider denied this request because of its permissions or policy."
		}
	case http.StatusTooManyRequests:
		message = "The model provider limited this request. Check that provider's usage or rate limit."
	case http.StatusServiceUnavailable:
		w.Header().Set("Retry-After", "5")
		message = "The model provider is temporarily unavailable. Try again shortly."
	}
	writeChatError(w, failure.status, message)
}

func isChatGPTProviderFailure(kind string) bool {
	return strings.HasPrefix(kind, "responses_") || strings.HasPrefix(kind, "oauth_") || kind == "models_http"
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
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func modelsHandler(provider modelProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !provider.configured() {
			writeChatError(w, http.StatusServiceUnavailable, "Connect ChatGPT or configure a model provider before loading models.")
			return
		}
		models, err := provider.models(r.Context())
		if err != nil {
			if errors.Is(err, errModelNotConfigured) {
				writeChatError(w, http.StatusServiceUnavailable, "Connect your ChatGPT account or configure LLM_MODEL and provider settings before loading models.")
				return
			}
			logProviderFailure(err)
			writeChatProviderError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeChatJSON(w, http.StatusOK, map[string]any{"models": models})
	}
}
