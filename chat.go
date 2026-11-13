package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
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
	ResearchTaskID  string        `json:"research_task_id,omitempty"`
	RunID           string        `json:"run_id,omitempty"`
	OmittedMessages int           `json:"omitted_messages,omitempty"`
	Messages        []chatMessage `json:"messages"`
	Model           string        `json:"model"`
	ConversationID  string        `json:"conversation_id,omitempty"`
}

func chatHandler(provider modelProvider) http.HandlerFunc {
	return chatHandlerWithStore(provider, nil)
}

func chatHandlerWithStore(provider modelProvider, accounts *accountStore) http.HandlerFunc {
	return chatHandlerWithResearch(provider, accounts, nil)
}
func chatHandlerWithResearch(provider modelProvider, accounts *accountStore, research *researchService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		if request.RunID != "" && !runIDPattern.MatchString(request.RunID) || request.OmittedMessages < 0 || request.OmittedMessages > 1000000 {
			writeChatError(w, 400, "Invalid run metadata.")
			return
		}
		var user authUser
		if accounts != nil {
			var ok bool
			user, ok = currentAuthUser(r.Context())
			if !ok {
				writeChatError(w, 401, "Sign in to continue.")
				return
			}
			if request.ConversationID != "" {
				var exists int
				if err := accounts.db.QueryRow(`SELECT 1 FROM conversations WHERE id=? AND user_id=?`, request.ConversationID, user.ID).Scan(&exists); err != nil {
					writeChatError(w, 404, "Conversation not found.")
					return
				}
			}
		}
		ctx := r.Context()
		var task *researchTask
		var researchErr error
		if request.ResearchTaskID != "" {
			if research == nil || accounts == nil || requestIsCrossSite(r) {
				writeChatError(w, 403, "Research permission is required.")
				return
			}
			var err error
			ctx, task, err = research.consume(ctx, user, request)
			if err != nil {
				writeChatError(w, 403, "Research permission is missing, expired, already used, or does not match this request.")
				return
			}
			defer func() { research.finish(task, researchErr) }()
		}
		rememberContent, remember := explicitMemoryCommand(request.Messages[len(request.Messages)-1].Content)
		if remember && (accounts == nil || task != nil || requestIsCrossSite(r)) {
			writeChatError(w, http.StatusForbidden, "Memory saves require an explicit local chat action, outside public-web research.")
			return
		}
		if !remember && !provider.configured() {
			writeChatError(w, http.StatusServiceUnavailable, "Connect ChatGPT from a browser on the Agent host, or configure LLM_MODEL and provider settings.")
			return
		}
		now := time.Now()
		messages := append([]chatMessage{{Role: "system", Content: instructionsAt(now)}}, request.Messages...)
		var memory *MemorySnapshot
		if accounts != nil && task == nil && !remember {
			snapshot, err := accounts.memorySnapshot(user)
			if err != nil {
				writeChatError(w, 500, "Memory could not be loaded. No model request was sent; try again.")
				return
			}
			if !snapshot.Enabled {
				snapshot.Entries = []MemoryEntry{}
			}
			memory = &snapshot
			messages[0].Content += memoryInstructions(snapshot)
		}
		if task != nil {
			messages = []chatMessage{{Role: "system", Content: researchInstructions(now, task.SearchAvailable)}, request.Messages[len(request.Messages)-1]}
		}
		var capture *runCapture
		if accounts != nil {
			var captureErr error
			capture, captureErr = accounts.createRun(user, request.RunID, request.ConversationID, messages, now, request.OmittedMessages)
			if errors.Is(captureErr, errRunExists) {
				researchErr = captureErr
				writeChatError(w, 409, "Run ID already exists.")
				return
			}
			// Unlike inference, a local save has a durable side effect. Reserve
			// its run ID first so retries cannot bypass duplicate-write protection.
			if remember && captureErr != nil {
				writeChatError(w, http.StatusServiceUnavailable, "Memory was not saved because its run could not be recorded. Try again later.")
				return
			}
			if capture != nil {
				capture.run.Context.Memory = memory
				capture.persist()
				ctx = context.WithValue(ctx, runContextKey{}, capture)
				w.Header().Set("X-Agent-Run-ID", capture.run.ID)
			}
		}
		if task != nil {
			if c := captureFrom(ctx); c != nil {
				c.researchBudget = 16 << 10
				c.run.Research = &RunResearch{TaskID: task.ID, Scope: "public_web", ConsentBasis: "explicit_task_approval", HistoryPolicy: "final_user_message_only", ExpiresAt: task.ExpiresAt}
				c.run.Events = append(c.run.Events, RunEvent{"consent", "Explicit task approval: public web only; earlier chat history excluded", now, "completed"})
				c.run.Context.Tools = researchTools(task.SearchAvailable, false)
				c.run.Context.OmittedMessages += len(request.Messages) - 1
				c.persist()
			}
		}
		var reply string
		var err error
		if remember {
			_, err = accounts.saveMemory(user, "user", rememberContent)
			if err == nil {
				reply = "Saved to your local user memory. Manage it in Memory settings. When memory is enabled, it is included in future normal chats sent to your model provider, but not public-web research."
				if capture != nil {
					capture.run.Events = append(capture.run.Events, RunEvent{"memory_save", "Explicit /remember command saved local user memory; no model request sent", time.Now(), "completed"})
				}
			} else {
				if capture != nil {
					capture.finish("", err)
				}
				memoryHTTPError(w, err)
				return
			}
		} else if task != nil {
			reply, err = research.run(ctx, task, provider, messages, request.Model)
			researchErr = err
		} else {
			reply, err = provider.complete(ctx, messages, request.Model)
		}
		if capture != nil && err != nil {
			capture.finish("", err)
		}
		if err != nil {
			if task != nil && errors.Is(err, errResearchDenied) {
				writeChatJSON(w, 403, map[string]string{"error": "Research stopped because a tool request exceeded this task's approved public-web scope or had invalid arguments. Review the task before starting new research; no additional access was granted.", "action": "review_research"})
				return
			}
			if task != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				writeChatError(w, 409, "Research was stopped or its time limit expired.")
				return
			}
			if errors.Is(err, errModelNotConfigured) {
				writeChatError(w, http.StatusServiceUnavailable, "Connect your ChatGPT account or configure LLM_MODEL and provider settings.")
				return
			}
			logProviderFailure(err)
			writeChatProviderError(w, err)
			return
		}
		result := map[string]string{"reply": reply}
		runID := ""
		if capture != nil {
			runID = capture.run.ID
			result["run_id"] = runID
		}
		if accounts != nil {
			user, ok := currentAuthUser(r.Context())
			if !ok {
				writeChatError(w, http.StatusUnauthorized, "Sign in to continue.")
				return
			}
			var conversationID string
			var saveErr error
			save := func() error {
				var err error
				conversationID, err = accounts.saveRunExchange(user, request.ConversationID, []chatMessage{request.Messages[len(request.Messages)-1], {Role: "assistant", Content: reply}}, runID)
				return err
			}
			if task != nil {
				saveErr = research.commit(ctx, task, save)
				researchErr = saveErr
			} else {
				saveErr = save()
			}
			if saveErr != nil {
				if capture != nil {
					capture.finish(reply, saveErr)
				}
				if task != nil && (errors.Is(saveErr, context.Canceled) || errors.Is(saveErr, context.DeadlineExceeded)) {
					writeChatError(w, 409, "Research was stopped or its time limit expired.")
					return
				}
				writeChatError(w, http.StatusInternalServerError, "The reply was generated, but the conversation could not be saved.")
				return
			}
			result["conversation_id"] = conversationID
			if capture != nil {
				capture.run.ConversationID = conversationID
			}
		}
		if capture != nil {
			capture.finish(reply, nil)
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
