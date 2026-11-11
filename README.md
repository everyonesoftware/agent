# EveryoneAgent

A private, self-hosted chatbot for a small household. Each person creates a local EveryoneAgent account, connects their own ChatGPT account, and gets separate saved conversations. User accounts, sessions, and conversation histories are stored in a local SQLite database; no hosted identity or database service is used.

## Run locally

Requirements: Docker Desktop with Compose.

Copy the sample environment file, then start the latest published image:

```powershell
Copy-Item .env.example .env
# Edit .env; do not commit it.
docker compose up -d
```

If OpenAI enables plan usage for this app and the account is eligible, no API key or API credits are required for that path. The flow is experimental; live authorization and eligibility have not been verified. Public repository visibility alone does not guarantee OpenAI approval. After signing in to EveryoneAgent, select **Continue with ChatGPT** to connect the ChatGPT account for that EveryoneAgent user. ChatGPT authorization uses a loopback callback, so complete the connection from a browser on the Windows PC running Docker Desktop at `http://127.0.0.1:8080`. Every app user manages their own ChatGPT profiles and model selection; profiles are not shared between app users.

Create an EveryoneAgent account in the app using a username and password of at least 12 characters. Passwords are hashed with bcrypt; browser sessions use random tokens whose hashes are stored in SQLite. Sign-in is required for chat, model discovery, ChatGPT account management, and conversation history. Each EveryoneAgent user has separate OAuth profile files under `/data/account-data/` and separate conversation rows in `/data/everyoneagent.sqlite`. ChatGPT refresh tokens are kept server-side and never returned to the browser. The SQLite file and credential directories are restricted to the container owner; they are not encrypted at rest, so protect and back up the Docker volume accordingly. Disconnecting clears the selected user's profile session tokens but retains the registration for safe reauthorization; revoke the app's usage access in ChatGPT settings to revoke remotely.

ChatGPT sign-in uses the existing loopback callback. Complete **Continue with ChatGPT** from a browser on the Windows PC running Docker Desktop, and sign in to the ChatGPT account that belongs to the currently logged-in EveryoneAgent user. Each user must connect their own account; their plan allowance is not shared through EveryoneAgent with other app users, though ChatGPT may apply the same subscription limits across other apps using that account. The OAuth flow remains experimental and live authorization/eligibility have not been verified.

An OpenAI-compatible API key or local model remains available as a fallback. `LLM_BASE_URL` defaults to the OpenAI API base URL, `LLM_MODEL` selects the fallback model, and `LLM_API_KEY` is sent as a bearer key when set. For a local model, set `LLM_BASE_URL` to that provider's OpenAI-compatible API base and leave `LLM_API_KEY` blank if it does not require one. API-key requests use separate API billing. The application sends chat requests through the server; the key is not sent to the browser. `.env` is excluded from Git and the Docker build context.

To pin a specific commit-count release, set `APP_VERSION` before starting Compose; for example, `$env:APP_VERSION = "3"`. The GitHub Actions workflow tests the code and publishes `ghcr.io/everyonesoftware/agent:<commit-count>` on pushes to `main` and version tags. The numeric count is also embedded in the app's `/version` endpoint and OCI version label. It additionally publishes `latest`, branch/tag, and short-SHA tags.

Open http://127.0.0.1:8080. The container is published only on the host loopback interface. To reach it from other devices, configure Tailscale Serve on the host to proxy to `http://127.0.0.1:8080`; do not change the Compose binding to `0.0.0.0` or forward the port from your router. If the host-side port is changed, update `SIWC_REDIRECT_URI` to `http://127.0.0.1:<host-port>/auth/callback`.

Stop with `docker compose down`.

Compose probes the public `/healthz` endpoint using `/everyoneagent healthz` every 30 seconds. The command uses the container's `PORT` (default `8080`), requests the loopback endpoint with a three-second timeout, and exits successfully only for HTTP 200 without following redirects. It does not initialize storage or contact a model provider. This is a web-server liveness check, not a database or ChatGPT readiness check. Docker marks the container unhealthy after three consecutive failures; the restart policy does not restart a container solely because it is unhealthy.

## Current status

The app contains local account registration/login, owner-scoped saved conversations, per-user ChatGPT OAuth profile storage, direct Responses API requests with upstream streaming and `store: false`, model discovery, and an OpenAI-compatible fallback. OAuth sign-in remains unverified through live authorization or account approval. Existing host-wide ChatGPT profile files are left untouched and are not automatically assigned to a new user; each user should connect their own ChatGPT account after signing in. Anyone who can reach the service can create an account, so expose it only to trusted household devices.
