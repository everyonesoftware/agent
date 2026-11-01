# EveryoneAgent

A private, self-hosted chatbot for a small household. Basic model-backed chat is available in a single Go application container. Conversations stay in the current browser tab only; per-user app logins, saved history, encryption, and web search are not implemented yet.

## Run locally

Requirements: Docker Desktop with Compose.

Copy the sample environment file, then start the latest published image:

```powershell
Copy-Item .env.example .env
# Edit .env; do not commit it.
docker compose up -d
```

If OpenAI enables plan usage for this app and the account is eligible, no API key or API credits are required for that path. The flow is experimental; live authorization and eligibility have not been verified. Public repository visibility alone does not guarantee OpenAI approval. To try it, open `http://127.0.0.1:8080` in a browser on the Windows PC running Docker Desktop and select **Continue with ChatGPT**. Sign in and approve the requested plan-usage access in OpenAI. This sign-in uses a loopback callback, so it must be completed from the host PC. You can add multiple ChatGPT registrations and explicitly choose which verified account is active; adding an account does not switch the active account. The account selector and OAuth management are host-local. Tailnet users all use the same active account and share its plan allowance. The model selector is populated from the active ChatGPT account's available models.

OAuth credentials and account profiles are stored in the `everyoneagent-data` Docker volume under `/data`, with owner-only file permissions. The app keeps refresh tokens server-side and never sends them to the browser. Disconnecting clears the selected profile's local session tokens but retains its client registration and account binding for safe reauthorization; revoke the app's usage access in ChatGPT settings if you also want to revoke the authorization. Everyone using this server shares the active ChatGPT account's plan allowance, which is also shared with that account's other participating apps.

An OpenAI-compatible API key or local model remains available as a fallback. `LLM_BASE_URL` defaults to the OpenAI API base URL, `LLM_MODEL` selects the fallback model, and `LLM_API_KEY` is sent as a bearer key when set. For a local model, set `LLM_BASE_URL` to that provider's OpenAI-compatible API base and leave `LLM_API_KEY` blank if it does not require one. API-key requests use separate API billing. The application sends chat requests through the server; the key is not sent to the browser. `.env` is excluded from Git and the Docker build context.

To pin a specific commit-count release, set `APP_VERSION` before starting Compose; for example, `$env:APP_VERSION = "3"`. The GitHub Actions workflow tests the code and publishes `ghcr.io/everyonesoftware/agent:<commit-count>` on pushes to `main` and version tags. The numeric count is also embedded in the app's `/version` endpoint and OCI version label. It additionally publishes `latest`, branch/tag, and short-SHA tags.

Open http://127.0.0.1:8080. The container is published only on the host loopback interface. To reach it from other devices, configure Tailscale Serve on the host to proxy to `http://127.0.0.1:8080`; do not change the Compose binding to `0.0.0.0` or forward the port from your router. If the host-side port is changed, update `SIWC_REDIRECT_URI` to `http://127.0.0.1:<host-port>/auth/callback`.

Stop with `docker compose down`.

## Current status

The app contains an experimental ChatGPT OAuth connection, direct Responses API requests with upstream streaming and `store: false`, profile-specific model discovery, multiple separately saved ChatGPT registrations, explicit host-wide active-account selection, and an OpenAI-compatible fallback. OAuth sign-in remains unverified through live authorization or account approval. The app does not provide per-user login or server-side conversation storage. Anyone who can reach it through the tailnet can use the active account's plan allowance, so expose it only to trusted household devices.
