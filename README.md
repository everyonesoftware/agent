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

Remote browsers (for example through Tailscale Serve) can read their own connection list/status, choose a model, switch saved ChatGPT profiles, and disconnect. **Switching the active ChatGPT account and disconnecting still require a fresh confirmation of the currently signed-in user's Agent password**, on localhost as well as remotely; missing or incorrect passwords are rejected, and confirmation attempts are limited per Agent user across sessions. Reads/status and model choice do not require password confirmation. Initial linking, adding, and reconnecting are password-free after Agent sign-in, but authorization starts are accepted only as same-origin POST requests on a loopback host. They still require a browser on the Agent host: OpenAI authorization returns to the existing loopback callback, so a remote browser cannot complete that flow. No remote callback or automatic local-browser handoff is provided.

The connection UI starts authorization with JSON `POST /auth/start`, `Accept: application/json`, and a bounded body containing `expected_active_profile_id` captured when the action began; no password is needed. Adding uses `?mode=add`, and resuming an incomplete registration uses `?mode=retry&profile_id=<saved-profile-id>`. A successful JSON request returns `authorization_url`; application errors remain JSON. Default reconnect requires the captured active-profile field (including an empty string for initial linking), and rejects a changed active account before preparing authorization. GET starts remain disabled; existing callback validation and per-user connection state are unchanged. Authorization starts do not consume the password-confirmation attempt budget used for switching and disconnecting.

Create an Agent account in the app using a username and password of at least 12 characters. Passwords are hashed with bcrypt; browser sessions use random tokens whose hashes are stored in SQLite. Sign-in is required for chat, model discovery, ChatGPT account management, and conversation history. Each Agent user has separate OAuth profile files under `/data/account-data/` and separate conversation rows in `/data/everyoneagent.sqlite`. ChatGPT refresh tokens are kept server-side and never returned to the browser. New database and credential files request owner-only permissions; existing host directory permissions are left unchanged. Data is not encrypted at rest, so protect and back up the data storage accordingly. Disconnecting clears the selected user's profile session tokens but retains the registration for safe reauthorization; revoke the app's usage access in ChatGPT settings to revoke remotely.

ChatGPT sign-in uses the existing loopback callback. Complete **Continue with ChatGPT** from a browser on the Windows PC running Docker Desktop, and sign in to the ChatGPT account that belongs to the currently logged-in Agent user. Each user must connect their own account; their plan allowance is not shared through Agent with other app users, though ChatGPT may apply the same subscription limits across other apps using that account. The OAuth flow remains experimental and live authorization/eligibility have not been verified.

An OpenAI-compatible API key or local model remains available as a fallback. `LLM_BASE_URL` defaults to the OpenAI API base URL, `LLM_MODEL` selects the fallback model, and `LLM_API_KEY` is sent as a bearer key when set. For a local model, set `LLM_BASE_URL` to that provider's OpenAI-compatible API base and leave `LLM_API_KEY` blank if it does not require one. API-key requests use separate API billing. The application sends chat requests through the server; the key is not sent to the browser. `.env` is excluded from Git and the Docker build context.

To pin a specific commit-count release, set `APP_VERSION` before starting Compose; for example, `$env:APP_VERSION = "3"`. The GitHub Actions workflow tests the code and publishes `ghcr.io/everyonesoftware/agent:<commit-count>` on pushes to `main` and version tags. The numeric count is also embedded in the app's `/version` endpoint and OCI version label. It additionally publishes `latest`, branch/tag, and short-SHA tags.

Open http://127.0.0.1:8080. The container is published only on the host loopback interface. To reach it from other devices, configure Tailscale Serve on the host to proxy to `http://127.0.0.1:8080`; do not change the Compose binding to `0.0.0.0` or forward the port from your router. If the host-side port is changed, update `SIWC_REDIRECT_URI` to `http://127.0.0.1:<host-port>/auth/callback`.

The ChatGPT connection panel derives its **Configured local address** from
`SIWC_REDIRECT_URI`, removing `/auth/callback` and retaining the configured host
port. For example, `http://127.0.0.1:8182/auth/callback` displays
`http://127.0.0.1:8182`. Remote users can copy this address and open it on the
computer hosting Agent. If the variable is unset or blank, Agent uses its default
callback at port `8080`; an invalid callback produces a configuration warning
instead of a guessed address. This is configuration discovery, not a reachability
test or Docker port inspection: keep the callback synchronized with your published
host port and restart Agent after changing the environment.

Stop with `docker compose down`.

Compose probes the public `/healthz` endpoint using `/agent healthz` every 30 seconds. The image also retains `/everyoneagent` as a compatibility alias for existing health checks. The command uses the container's `PORT` (default `8080`), requests the loopback endpoint with a three-second timeout, and exits successfully only for HTTP 200 without following redirects. It does not initialize storage or contact a model provider. This is a web-server liveness check, not a database or ChatGPT readiness check. Docker marks the container unhealthy after three consecutive failures; the restart policy does not restart a container solely because it is unhealthy.

## Install Agent as a web app

Agent is a single-page application (SPA): chat and Settings switch views without
loading another document, and Settings participates in browser Back/Forward
navigation. PWA installation adds an optional standalone window; it does not
replace that single-page navigation.

Agent can run in a standalone app window using the browser's **Install app** or
**Add to Home Screen** action. Use HTTPS (including a trusted Tailscale Serve
address), or localhost on the host computer. A plain HTTP address on another
device does not enable service workers. Installation does not change the existing
local account, server storage, network access, or loopback-only ChatGPT
authorization requirements.

In **Settings → Web app updates**, select **Check for updates** to compare the
running page with the deployed server. An available update offers an explicit
update/reload action or **Later**. Agent does not automatically reload an active
session; finish active requests first, and review the unsaved-work confirmation
before applying. The browser may download a new worker in the background, but it
does not replace an active worker until approved (or all old app windows close).
This updates browser code from the already-deployed server; it does not pull an
image, restart Docker, or update the server deployment.

Offline support is intentionally limited to a public disconnected page with a
keyboard-accessible retry link. The service worker caches only `/offline.html`,
never the app shell, conversations, account details, API responses, credentials,
or mutations. Requests are not queued or replayed. Reconnect to use chat and
account management. API/auth/version/health navigations never receive the
disconnected-page fallback. Cache cleanup only removes Agent's own old offline
caches, leaving other applications' caches intact.

The public `/version.json` endpoint exposes string `version` and `buildId` fields.
The build ID is a SHA-256 content fingerprint covering the release version,
embedded page, update script, manifest, worker, offline page, and icons, so code
changes are detectable even when the release label is unchanged. Root HTML uses
`Cache-Control: no-store`; PWA metadata, worker, script, offline page, and icons
use `no-cache` revalidation. All assets are embedded in the Go executable; the
existing Docker packaging needs no additional runtime asset directory.

Web app verification runs in the container publishing workflow before publishing:
`go test ./...`, `go vet ./...`, `node tests/pwa-updates.mjs`, and
`node tests/ui.mjs`. The real production-server browser check builds two release
versions and runs `node tests/pwa-browser.mjs <first-executable> <second-executable>`.
It creates isolated temporary account storage and a non-incognito Chromium
profile, checks installability and responsive Settings, takes the server offline,
and swaps builds to verify manual update/reload. Set `UI_BROWSER` to a Chromium
executable when it is not found automatically; optionally set `UI_SCREENSHOTS`
to an existing directory to save browser evidence. No production credentials or
account storage are used.

### Reproducible browser verification

The release verification job uses the explicit `ubuntu-22.04` runner label and
installs full **Chrome for Testing 156.0.8078.80** instead of selecting an ambient
runner browser. `tests/browser-tools/package.json` pins Google's
`@puppeteer/browsers` installer to **3.2.7**, with its transitive dependencies in
`package-lock.json`, and pins the Linux browser archive's SHA-256. `npm ci` uses
the lock without running dependency lifecycle scripts. Browser installation uses
the archive integrity check and installs its declared Debian/Ubuntu dependencies;
the actual tests run as the ordinary runner user with Chromium sandboxing enabled.
There is no `--no-sandbox`, global sandbox-policy change, floating browser channel,
or automatic fallback to a different installed browser in this CI job.

An initial real-browser preflight verifies startup, the CDP endpoint, and the exact
browser version before app assertions run. The existing UI, Memory, Inspector,
research, and PWA suites remain required publication gates, using their existing
real-browser assertions. The full production-server suites use isolated SQLite
accounts and fixture upstreams, not production credentials or live model inference.

All browser suites share `tests/browser-launcher.mjs`: it reports the executable
and version, captures an 8 KiB stderr tail, fails promptly on spawn errors or early
exit (including termination signals), and bounds cleanup even after a crash. A
browser-specific 20-second readiness deadline is separate from application
assertion deadlines. Failure diagnostics are retained for seven days in the
workflow's `browser-diagnostics-<run>-<attempt>` artifact; these are browser
lifecycle logs, not cookies, profiles, or account databases. Local browser discovery
and explicit `UI_BROWSER` overrides remain available outside that controlled CI job.

The small lifecycle/CI-contract checks run with:

```sh
npm ci --prefix tests/browser-tools --ignore-scripts --no-audit --no-fund
node --test tests/browser-launcher.test.mjs tests/browser-ci.test.mjs
```

To update the browser, deliberately update the exact Chrome version and the
SHA-256 of its official Linux archive together, update the installer lock if needed,
and run preflight plus every browser suite on the chosen Ubuntu runner. Pinning
the OS label prevents automatic major-OS migration; it does not freeze hosted
runner patches or system packages. Local verification alone is not proof that the
hosted runner's sandbox startup passed.

## Public-web research with task-scoped consent

Ordinary chat has no tools. To research a task, enable **Research public web**
in the composer and submit the prompt. Agent shows an inline consent card before
making a model or web request for that task. The card identifies the search
provider, displays the task prompt, and explains which information leaves the
server. Choose **Allow for this task**, **Answer without browsing**, or **Cancel**.
Cancelling preserves the draft; declining uses ordinary no-tool chat. There is
no blanket or remembered approval in this first milestone.

Research sends only the final user prompt, trusted runtime instructions, and
research tool exchanges to the model; it does not include earlier conversation
messages. Search queries may include information from that displayed prompt.
Retrieved page text and search results also go to the selected model provider.
Do not put private information in a research prompt unless you intend to permit
that disclosure. The research permission never grants local-file access, signed-in
browser access, memory/skill writes, terminal execution, messaging, or publishing.
Unknown tools and targets outside the implemented public-web boundary are rejected,
not silently given additional permission.

Public-page retrieval works without a search credential. General web search uses
the official Brave Search API after you configure your own key in
**Settings → Web research**. Select **Verify & save key** to send one explicit
`Agent connection test` query to Brave; this may count toward provider usage and
does not send your chat history. The key is saved only after verification succeeds.
No environment variable or shared-key fallback is used for Brave Search, and no
search subscription is created automatically. Without your own saved key, the
consent card reports **page retrieval only** and the model has no search tool.

Each Agent user has separate search settings. The browser receives only status,
the key's final characters, revision metadata, verification timestamps, and
allowlisted usage metadata—not
the saved credential. Keys are excluded from model messages, conversation history,
Run Inspector captures, URLs, and browser storage. They are stored server-side in
the local SQLite database **without encryption at rest**, under the same storage
protections as the rest of Agent's account data; protect the data directory and
its backups. Connection status reflects the last explicit check, not continuous
provider monitoring. Opening Settings does not send a search request.

**Brave Search usage** tracks successful search requests made by your Agent
account after tracking began, including initial key verification, connection
tests and successful retries. It does not include other apps using the key or
unavailable history, and it is not Brave's account-wide usage or billing balance.
Tracking stays on with no cap configured and persists across restarts, key
replacement and key removal. Public-page retrieval does not use this counter.

Enable **Limit search requests** to set an optional maximum per calendar day or
calendar month, with an explicit IANA timezone governing midnight resets. The
progress bar fills toward requests used. Settings shows remaining capacity,
the reset time, and warnings near or at the cap. Changing the cap, period or
timezone does not delete history: Agent selects the requests recorded in the
selected calendar interval. Caps are disabled by default.

Capacity is reserved atomically before each Brave request, so concurrent tasks
and connection tests cannot bypass the cap. Successful HTTP responses consume
usage even if Agent cannot parse the response body; definitive error responses
release the reservation. In-flight requests and requests with uncertain outcomes
(for example, a connection failing after dispatch) remain reserved against the
cap and are shown separately, not mislabeled as successful or billed requests.
Reservations persist across restarts. At the cap, Agent stops new Brave searches
until the next period or an explicit cap change; it does not silently switch
providers. Existing evidence and public-page retrieval remain usable. Local
storage failures fail closed rather than allowing untracked requests.

The secondary **Brave-reported rate limits** disclosure retains the saved
response-header snapshot for the current credential. It shows quota and burst
windows separately, with observation and reset times in your browser's local
timezone. A zero provider limit means **No request-count cap**, not free searches
or unlimited credit. Missing or malformed headers mean unknown provider quota.
After a snapshot reset time passes, Agent does not invent replenishment. Only
this credential-specific snapshot is cleared when a key is removed or replaced;
the Agent request history is retained.

Opening Settings and saving a cap read or write local metadata only, never a
background Brave query. Use **Brave dashboard** for current provider plan and
billing details. Configuring a cap does not authorize research or change existing
task permissions.

Use **Replace key** to enter a new credential; a failed verification leaves the
previous saved key unchanged. **Test connection** checks the saved key without
returning it to the browser. **Remove** requires confirmation and removes the key
only from your Agent account, not from Brave. Removing or successfully replacing
a key invalidates that user's pending research approvals and stops active research
using the old configuration. Other users, public-page retrieval, and ChatGPT
connections are unaffected; already-sent requests cannot be recalled. Revision
checks prevent stale tabs or delayed verification from overwriting newer settings
or resurrecting a removed key. Saving a key never approves a research task.

The authenticated Settings API is `GET /api/research/settings` and JSON
`POST /api/research/settings/{verify|check|remove|budget}`. Each mutation requires the
captured `expected_revision`; verification additionally accepts `api_key` in the
bounded request body. Raw keys are never returned. Stale revisions return 409
without silently retrying a mutation. Budget saves additionally require
`expected_budget_revision`, `enabled`, `maximum` (1–10,000,000), `period` (`day`
or `month`) and `timezone` (an IANA timezone name). Budget changes have their own
revision and do not invalidate task approvals. The production-router/browser integration
uses real local account storage and isolated Brave/model HTTP fixtures:

```sh
AGENT_SETTINGS_BROWSER=1 go test -run '^TestResearchSettingsBrowser$' -count=1 -v .
```

This browser check requires Node and Chromium. Its package-private upstream
factory is a test seam, not a configurable proxy or permission to bypass public
URL checks in a deployed Agent server.

While a research request is running, **Public web only**, **Stop**, and
**Permissions** remain visible. Permissions exposes the current scope and a
**Revoke** action. Stop/revoke is enforced server-side and cancels in-flight
work; aborting a browser fetch alone is not treated as revocation. Cancellation
cannot undo model/search/page requests already sent. Tool calls, elapsed time,
network input, and captured output are bounded. Retrieval uses unauthenticated
HTTP(S) GET requests with no browser cookies, no JavaScript execution, and
public-address validation at connection and redirect boundaries. Private,
loopback, link-local, and metadata addresses are not permitted.

Approvals belong to the signed-in Agent user and an immutable task request.
They expire, are consumed only once, and cannot authorize a different prompt,
model selection, conversation, or run. Completion closes the permission. Pending
and active permissions are held in server memory, so a server restart invalidates
them rather than restoring authorization. Research permissions are separate from
the existing persistent conversation and Run Inspector records.

Authenticated, no-store routes are `GET /api/research`,
`POST /api/research/tasks`, `GET /api/research/tasks/{id}`, and
`POST /api/research/tasks/{id}/{approve|stop|revoke}`. Creation takes the ordinary
chat payload and performs no model or web I/O. Approval actions accept `{}`;
`POST /api/chat` additionally accepts `research_task_id` for the approved immutable
request. Foreign task IDs look missing. Rejected, expired, changed, or already-used
approvals do not authorize tool execution.

The production-server browser regression uses isolated SQLite account storage
and a local model HTTP fixture, with no real credentials, inference, or public-web
requests:

```sh
go build -o agent-research-test .
node tests/research-browser.mjs ./agent-research-test
```

Passing fixture tests establishes protocol and consent behavior, not that a live
ChatGPT account is eligible for function calling or that a Brave credential works.
Provider capability/authorization errors are surfaced rather than silently
switching to a different account or billing path.

## Explicit account memory

In **Settings → Memory**, add, review, edit, delete, or export saved facts in the
**User** and **Project** categories. Both categories belong to your Agent account
and apply to ordinary chats across conversations; Project is a label, not a
separate project-access boundary. The **Remember** action on your own messages
opens an editable draft that you must explicitly save. Alternatively, send
`/remember [fact]` to save that exact fact as User memory on the server without
model inference. The model cannot autonomously write memory, and ordinary
natural-language requests do not implicitly save facts.

Memory is enabled by default. **Include saved memory in new chats** controls
whether saved entries are sent to your selected model provider on subsequent
ordinary chat requests, including turns in an existing conversation. Disabling
does not delete entries or prevent deliberate edits. Public-web research excludes
saved memory and does not permit `/remember` writes. This does not remove private
information you explicitly put in a research prompt.

Saved facts persist in the local account SQLite database across restarts, with
the same account isolation and plaintext-at-rest protections as conversations.
Do not save secrets. There is no automatic extraction or retroactive recall of
old chats. Deleting or disabling memory affects future memory injection only:
earlier messages, exports, and retained traces may still contain previous copies.
Run Inspector's **Context → Memory** shows the exact memory snapshot included
for that ordinary chat, or its disabled/empty state; research explicitly shows
memory was excluded. `/remember` records a local `memory_save` lifecycle event,
not a fabricated provider request or model-selected function call.

Each account supports at most 32 entries, 1024 UTF-8 bytes per trimmed fact, and
a 12 KiB encoded snapshot including metadata. Explicit saves are rejected rather
than silently evicting older entries. Capacity conflicts return 409; malformed
input returns 400; unavailable local memory storage returns 503 on saves.

Authenticated no-store routes are `GET /api/memories`, `POST /api/memories`,
`PUT /api/memories/{id}`, `DELETE /api/memories/{id}`,
`PUT /api/memory-settings`, and `GET /api/memories/export`. Create/update bodies
are `{ "category": "user", "content": "A fact" }`; settings accepts
`{ "enabled": false }`. Export downloads `memories.json` containing the current
enabled state and entries, never another account's memory.

Verification uses a real production server, isolated SQLite accounts and headless
Chromium, with only the model HTTP endpoint replaced by a local fixture:

```sh
go build -o agent-memory-test .
node tests/memory-ui.mjs
node tests/memory-browser.mjs ./agent-memory-test
```

The browser check covers explicit save, fresh-chat wire/context disclosure,
editing, deletion, export, disable/re-enable, account isolation, and server restart.
It performs no live model inference or production-account mutation. Optional
`MEMORY_SCRATCH`, `MEMORY_ARTIFACTS`, and `UI_SCREENSHOTS` paths keep test storage
and desktop/phone evidence in a chosen scratch directory.

## Run Inspector and trusted runtime context

Each chat turn prepends a server-owned system message with the current server date,
timezone and UTC offset, in both the OpenAI-compatible and ChatGPT Responses paths.
The clock is read on every turn; the server timezone (`TZ`/Go `time.Local`) is not
assumed to be the browser's timezone. Timezone data is embedded for minimal container
images. The baseline explicitly distinguishes missing model knowledge from future
events and states that ordinary chat has no live-verification tools. Approved
research runs instead describe their actual public-web tools and permission scope.
It is context,
not a guarantee that the model will follow every instruction.

The inspector records backend lifecycle events, the effective provider/model,
context messages, actual provider JSON request body, allowlisted reply text and
provider-reported token usage. It does not record private reasoning, authorization
headers, API keys, OAuth grants, provider URLs, or arbitrary upstream error bodies.
**It does contain your chat text and model replies**; there is no promise to detect
or redact secrets that you type into a conversation. Traces share the private local
SQLite account database and its access controls; they are not encrypted at rest.

`POST /api/chat` accepts an optional UUID-format `run_id` (otherwise generated by
the server) and optional `omitted_messages` integer from 0 through 1,000,000,
representing messages the client excluded before inference. Existing run IDs are
rejected without invoking the provider. Once a run is created, its ID is returned
in `X-Agent-Run-ID`, including on provider errors; successful JSON also includes
`run_id`. Authenticated `GET /api/runs/{id}` returns `{ "run": ... }` with
`Cache-Control: no-store` and `nosniff`. Another user's ID is indistinguishable from
a missing ID (404); polling before creation can also return 404. Existing
conversation ownership is checked before any model request.

At most the latest 50 runs per user are retained, for up to seven days. Expired
records are excluded from reads and pruned at startup and subsequent run creation.
Context message capture, request bodies and reply text each have a 32 KiB encoded
budget (the complete normal trace remains below 128 KiB); oversized request bodies
are omitted, and oversized replies are shortened, with explicit truncation flags.
`context.omitted_messages` counts only client-side inference omissions;
`context.capture_omitted_messages` and `context.truncated` disclose messages sent
to the model but excluded from stored capture. Trace-only storage failures do not
block model generation. Successful assistant history messages include optional
`run_id` for reopening retained runs; deleting a conversation removes associated
traces. Runs left running when the server starts are marked `interrupted` once;
reading a run never changes its state. Run one server process per account database.
Provider retries are currently zero.

The isolated integration check exercises the production server, SQLite persistence,
and a real browser against a local model-provider stub, without live credentials:

```sh
go build -o agent-inspector-test .
node tests/run-inspector-browser.mjs ./agent-inspector-test
```

## Optional ChatGPT usage status (native Codex runtime)

The image includes the official **Codex CLI 0.167.2** (`rust-v0.167.2`)
standalone Linux musl binary at `/usr/local/bin/codex`. This is a separate,
server-side usage-status connection; it does not replace Agent's existing
**Continue with ChatGPT** chat authorization or send chats through Codex. Missing
usage authorization, unavailable usage data, and Codex failures must not prevent
chat. No Codex install on the host/laptop, Node/npm, Rust toolchain, or additional
published port is required. The runtime remains distroless and runs as
UID/GID `65532:65532` with the existing `/data` mount.

Agent starts `codex app-server --listen stdio:// -c
cli_auth_credentials_store=\"file\"` as a child process, not an HTTP/WebSocket
listener. `CODEX_USAGE_BINARY` optionally overrides the executable; otherwise
Agent resolves `codex` from `PATH`. An override must be an already-installed
compatible native executable, not a command with shell arguments. The private,
writable `CODEX_HOME` is scoped to the Agent user and selected ChatGPT profile
under `/data/account-data/<AgentUserID>/usage/<SIWCprofileID>/`; never share it
between users/profiles or mount a personal laptop's Codex credentials there.
Codex manages these separate credentials on the server. They are plaintext at
rest: protect `/data`, backups, and host ACLs. Do not copy Agent's existing SIWC
refresh tokens into Codex or expose Codex credential files to the browser.

Usage sign-in requires the user's explicit approval of the provider's device-code
flow; eligible accounts may need to enable device-code authorization in ChatGPT
security/beta settings. Live approval and signed-in rate-limit retrieval are not
part of the offline image check. The adapter restricts its calls to initialization
and account login/read/rate-limit/logout RPCs; this is **not** a promise that the
provider grants a narrower read-only OAuth scope. Local logout/credential deletion
does not guarantee remote revocation; use the provider's account controls when
remote revocation is required.

`scripts/install-codex.sh` downloads only from the pinned official release during
the build, verifies the exact archive SHA256 before extraction, and selects the
binary from Docker's `TARGETARCH`. Unsupported architectures fail the build.
The computed hashes of the downloaded official archive bytes are:

| Platform | Official archive | SHA256 |
| --- | --- | --- |
| `linux/amd64` | `codex-x86_64-unknown-linux-musl.tar.gz` | `8246e846c550ea21197fcc4067bc270dca895e94ea42ed6a282e6f4e0125b15f` |
| `linux/arm64` | `codex-aarch64-unknown-linux-musl.tar.gz` | `ed81fa2c7d209689af061b6f305ed554218de92a1c2c4019142b28f66fa64add` |

The stage also verifies and preserves that release's `LICENSE` and `NOTICE` under
`/usr/share/licenses/codex/`, together with a `BUILD-INFO` pin/target/hash record.
Download tools stay in the build stage and its layer is reused until the pin or
installer changes. Both musl executables were inspected as statically linked ELF
binaries; no dynamic loader or shared-library runtime is added. Account-only RPCs
need no shell, execution-server/code-mode/voice companions, or sandbox tools.
The standalone app-server currently logs a missing-PATH-bubblewrap warning at
startup; it is nonfatal for these account RPCs. This image is not a general Codex
coding/terminal environment.

Operators should update the pin and hashes together after reviewing the official
release, then rerun protocol compatibility and image checks on both supported
architectures. The image never self-updates Codex. To exercise a built image using
host Python 3 and Docker (no live credentials or inference):

```sh
docker build -t agent:codex-smoke .
python scripts/codex-runtime-smoke.py agent:codex-smoke
```

The check uses network-disabled ephemeral containers, a private writable temporary
`CODEX_HOME`, and a read-only Codex root filesystem. It verifies the pinned version,
nonroot configuration, stdio initialize/initialized, a signed-out `account/read`,
the real signed-out rate-limit error, and both `/agent healthz` and the retained
`/everyoneagent healthz` alias against the running application. It never mounts
existing credentials, publishes a port, approves a login, or starts a model turn.

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
sidebar. Existing database, session cookie, model preference, and Compose
identifiers retain their original `everyoneagent` names for compatibility; no data
migration is needed. New ChatGPT registrations request the Agent display name;
previously registered connections retain their existing provider-side identity.

The Focus interface uses a dark chat workspace with searchable conversation history
in the left sidebar, the current user at the bottom left, and model selection in
the message composer. Open the user menu to manage connected ChatGPT accounts or
sign out. On phones, the header menu opens session history over the conversation.
The Quiet entry login keeps local Agent sign-in separate from connecting
a ChatGPT account afterward.

Settings uses `#settings` in the browser URL, including refresh and browser
Back/Forward navigation. On desktop, drag the sidebar's right edge to change its
width, or focus the divider and use Left/Right arrow keys. The message box has a
top-right resize arrow: drag upward to expand or downward to shrink; when focused,
Up/Down arrow keys also resize it. Both controls enforce bounds to keep the
workspace usable; phone session navigation retains its existing drawer layout.

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
