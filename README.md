# Agent

A private, self-hosted chatbot for a small household. Each person creates a local Agent account, connects their own ChatGPT account, and gets separate saved conversations. User accounts, sessions, and conversation histories are stored in a local SQLite database; no hosted identity or database service is used.

## Run locally

Requirements: Docker Desktop with Compose.

Copy the sample environment file, then start the latest published image:

```powershell
Copy-Item .env.example .env
# Edit .env; do not commit it.
docker compose up -d
```

If OpenAI enables plan usage for this app and the account is eligible, no API key or API credits are required for that path. The flow is experimental; live authorization and eligibility have not been verified. Public repository visibility alone does not guarantee OpenAI approval. After signing in to Agent, select **Continue with ChatGPT** to connect the ChatGPT account for that Agent user. ChatGPT authorization uses a loopback callback, so complete the connection from a browser on the Windows PC running Docker Desktop at `http://127.0.0.1:8080`. Every app user manages their own ChatGPT profiles and model selection; profiles are not shared between app users.

Create an Agent account in the app using a username and password of at least 12 characters. Passwords are hashed with bcrypt; browser sessions use random tokens whose hashes are stored in SQLite. Sign-in is required for chat, model discovery, ChatGPT account management, and conversation history. Each Agent user has separate OAuth profile files under `/data/account-data/` and separate conversation rows in `/data/everyoneagent.sqlite`. ChatGPT refresh tokens are kept server-side and never returned to the browser. New database and credential files request owner-only permissions; existing host directory permissions are left unchanged. Data is not encrypted at rest, so protect and back up the data storage accordingly. Disconnecting clears the selected user's profile session tokens but retains the registration for safe reauthorization; revoke the app's usage access in ChatGPT settings to revoke remotely.

ChatGPT sign-in uses the existing loopback callback. Complete **Continue with ChatGPT** from a browser on the Windows PC running Docker Desktop, and sign in to the ChatGPT account that belongs to the currently logged-in Agent user. Each user must connect their own account; their plan allowance is not shared through Agent with other app users, though ChatGPT may apply the same subscription limits across other apps using that account. The OAuth flow remains experimental and live authorization/eligibility have not been verified.

An OpenAI-compatible API key or local model remains available as a fallback. `LLM_BASE_URL` defaults to the OpenAI API base URL, `LLM_MODEL` selects the fallback model, and `LLM_API_KEY` is sent as a bearer key when set. For a local model, set `LLM_BASE_URL` to that provider's OpenAI-compatible API base and leave `LLM_API_KEY` blank if it does not require one. API-key requests use separate API billing. The application sends chat requests through the server; the key is not sent to the browser. `.env` is excluded from Git and the Docker build context.

To pin a specific commit-count release, set `APP_VERSION` before starting Compose; for example, `$env:APP_VERSION = "3"`. The GitHub Actions workflow tests the code and publishes `ghcr.io/everyonesoftware/agent:<commit-count>` on pushes to `main` and version tags. The numeric count is also embedded in the app's `/version` endpoint and OCI version label. It additionally publishes `latest`, branch/tag, and short-SHA tags.

Open http://127.0.0.1:8080. The container is published only on the host loopback interface. To reach it from other devices, configure Tailscale Serve on the host to proxy to `http://127.0.0.1:8080`; do not change the Compose binding to `0.0.0.0` or forward the port from your router. If the host-side port is changed, update `SIWC_REDIRECT_URI` to `http://127.0.0.1:<host-port>/auth/callback`.

Stop with `docker compose down`.

Compose probes the public `/healthz` endpoint using `/everyoneagent healthz` every 30 seconds. The command uses the container's `PORT` (default `8080`), requests the loopback endpoint with a three-second timeout, and exits successfully only for HTTP 200 without following redirects. It does not initialize storage or contact a model provider. This is a web-server liveness check, not a database or ChatGPT readiness check. Docker marks the container unhealthy after three consecutive failures; the restart policy does not restart a container solely because it is unhealthy.

## Host-accessible data storage

The image runs as UID/GID `65532:65532`. You can use a host folder instead of
the default named volume without an extra permissions setting:

```yaml
services:
  everyoneagent:
    environment:
      SIWC_DATA_DIR: /data
    volumes:
      - ./data:/data
```

Merge these settings into your existing service; keep its other environment,
networking, and health-check settings. If using the repository's Compose file,
replace its named-volume mapping with the bind mount.

The app leaves existing directories and database file permissions unchanged,
requests mode `0700` for new directories and `0600` for new database/credential
files, and does not run `chmod` on your mount. OAuth credential files with
group/world-readable Unix permissions are still rejected; credential validation
and actual filesystem read/write errors are not bypassed.

**Protect the host folder with host ACLs/permissions.** It contains password
hashes, sessions, conversation history, and plaintext OAuth credentials; do not
share it with untrusted users. The container's UID must still have read/write
access. A read-only mount or missing host access still prevents writes.

To migrate a bind-mounted deployment, stop the app before copying the entire
`data` folder to avoid an inconsistent SQLite backup. Copy your Compose
configuration and privately managed environment settings too, restore the host
folder's access controls, then start the new deployment. Preserve separately
mounted Tailscale state separately when applicable. No database migration or
data deletion is required to use a bind mount with the same existing data.

## Interface and development checks

The interface is branded **Agent**, with a compact **e1** mark on login and in the
sidebar. Existing database, session cookie, model preference, executable, and Compose
identifiers retain their original `everyoneagent` names for compatibility; no data
migration is needed. New ChatGPT registrations request the Agent display name;
previously registered connections retain their existing provider-side identity.

The Focus interface uses a dark chat workspace with searchable conversation history
in the left sidebar, the current user at the bottom left, and model selection in
the message composer. Open the user menu to manage connected ChatGPT accounts or
sign out. On phones, the header menu opens session history over the conversation.
The Quiet entry login keeps local Agent sign-in separate from connecting
a ChatGPT account afterward.

Run backend tests and build with Go 1.25:

```sh
go test ./...
go vet ./...
go build ./...
```

Run the dependency-free browser regressions with Node 22+ and an installed
Chromium-based browser:

```sh
node tests/ui.mjs
```

The browser runner detects standard Edge/Chrome paths on Windows and Chrome/Chromium
paths on Linux. Set `UI_BROWSER` to override the executable path. It serves the
actual embedded page against isolated in-memory API fixtures and uses a temporary
browser profile; it does not access the running application's accounts or data.
Set `UI_SCREENSHOTS` to an existing output directory to save desktop/phone evidence.
These checks cover UI wiring and layout, not live ChatGPT authorization.

## Current status

The app contains local account registration/login, owner-scoped saved conversations, per-user ChatGPT OAuth profile storage, direct Responses API requests with upstream streaming and `store: false`, model discovery, and an OpenAI-compatible fallback. OAuth sign-in remains unverified through live authorization or account approval. Existing host-wide ChatGPT profile files are left untouched and are not automatically assigned to a new user; each user should connect their own ChatGPT account after signing in. Anyone who can reach the service can create an account, so expose it only to trusted household devices.
