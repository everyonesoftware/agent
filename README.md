# EveryoneAgent

A private, self-hosted chatbot for a small household. Basic model-backed chat is available in a single Go application container. Conversations stay in the current browser tab only; accounts, saved history, encryption, and web search are not implemented yet.

## Run locally

Requirements: Docker Desktop with Compose.

Copy the sample environment file, set a model name and provider credentials as needed, then start the latest published image:

```powershell
Copy-Item .env.example .env
# Edit .env; do not commit it.
docker compose up -d
```

`LLM_BASE_URL` defaults to the OpenAI API base URL, `LLM_MODEL` selects the model, and `LLM_API_KEY` is sent as a bearer key when set. For a local model, set `LLM_BASE_URL` to that provider's OpenAI-compatible API base and leave `LLM_API_KEY` blank if it does not require one. The application sends chat requests through the server; the key is not sent to the browser. `.env` is excluded from Git and the Docker build context.

To pin a specific commit-count release, set `APP_VERSION` before starting Compose; for example, `$env:APP_VERSION = "3"`. The GitHub Actions workflow tests the code and publishes `ghcr.io/everyonesoftware/agent:<commit-count>` on pushes to `main` and version tags. The numeric count is also embedded in the app's `/version` endpoint and OCI version label. It additionally publishes `latest`, branch/tag, and short-SHA tags.

Open http://127.0.0.1:8080. The container is published only on the host loopback interface. To reach it from other devices, configure Tailscale Serve on the host to proxy to `http://127.0.0.1:8080`; do not change the Compose binding to `0.0.0.0` or forward the port from your router.

Stop with `docker compose down`.

## Current status

The app now provides a basic chat interface and a configurable OpenAI-compatible chat-completions client. It does not yet provide authentication, server-side conversation storage, encryption, or web search. Treat this as a private prototype and expose it only to trusted devices on your tailnet.
