# Agent

<!-- current-downloads:start -->
## Current downloads

Each platform keeps its independently verified version and original download URL.

| Platform | Version | Download | Status |
| --- | --- | --- | --- |
| Windows x64 | 0.1.60 | [Download installer (.exe)](https://github.com/everyonesoftware/agent/releases/download/v0.1.60/AgentWindows-win-x64-Setup.exe) · [SHA-256](https://github.com/everyonesoftware/agent/releases/download/v0.1.60/AgentWindows-win-x64-Setup.exe.sha256) | Unsigned alpha; Windows may display a security warning. Requires your Agent server. |
| Android | 0.1.60 | [Download APK](https://github.com/everyonesoftware/agent/releases/download/v0.1.60/Agent-android.apk) · [SHA-256](https://github.com/everyonesoftware/agent/releases/download/v0.1.60/Agent-android.apk.sha256) | Signed alpha; user-consented installation. Requires your Agent server. |
| Container / Web server | 60 | `ghcr.io/everyonesoftware/agent:60` · [Publication manifest](https://github.com/everyonesoftware/agent/releases/download/v0.1.60/agent-container-release.json) | [Deployment instructions](https://github.com/everyonesoftware/agent/blob/main/README.md#run-locally); requires server and local Agent account. |

Unchanged platforms are not rebuilt, copied, or relabeled for newer project releases. Native clients connect to your self-hosted server; they do not include a model runtime.
<!-- current-downloads:end -->

[Native release and download verification](docs/NATIVE-RELEASES.md) explains selective builds, platform signing, and read-only table previews.

A private, self-hosted chatbot for a small household. Each person creates a local Agent account, connects their own ChatGPT account, and gets separate saved conversations. User accounts, sessions, and conversation histories are stored in a local SQLite database; no hosted identity or database service is used.

## Run locally

Requirements: Docker Desktop with Compose.

Copy the sample environment file, then start the latest published image:

```powershell
Copy-Item .env.example .env
# Edit .env; do not commit it.
docker compose up -d
```

ChatGPT features use the bundled Codex runtime and your eligible ChatGPT subscription, rather than Agent making direct Responses API requests. After signing in to Agent, select **Continue with ChatGPT**, open the verification page, and enter the device code. Approval can happen in a browser on the same device or another machine; no localhost callback or browser on the Agent server is needed. Live provider approval and model eligibility are not established by fixture tests.

One Codex connection powers conversations, model selection, memory suggestions, public-web research, and allowance/history collection. Each Agent user manages their own saved ChatGPT profiles; accounts, credentials, and conversations are not shared with other Agent users. Existing Codex usage connections are reused for the corresponding saved profile. A profile that only has the removed Responses authorization needs device-code sign-in; saved conversations and recorded history are retained. Legacy Responses tokens are not imported into Codex.

Remote browsers (for example through Tailscale Serve) can start and finish device sign-in as well as read status, choose a model, switch saved profiles, and disconnect. **Switching the active ChatGPT account and disconnecting still require a fresh confirmation of the currently signed-in user's Agent password.** Missing or incorrect passwords are rejected, and attempts are limited per Agent user across sessions. Initial linking, adding, and reconnecting need no extra password after Agent sign-in. Mutation requests remain same-origin and authenticated; device sign-in does not make Agent publicly accessible.

Create an Agent account using a username and password of at least 12 characters. Passwords are hashed with bcrypt; browser sessions use random tokens whose hashes are stored in SQLite. Sign-in is required for chat, model discovery, account management, and conversation history. Per-user data lives under `/data/account-data/`, and conversations are stored in `/data/everyoneagent.sqlite`. Codex credentials stay server-side and are never returned to the browser. New files request owner-only permissions; existing host directory permissions are left unchanged. Data is not encrypted at rest, so protect storage and backups. Disconnecting signs out the selected Codex connection for both conversations and allowance; local logout is not a guarantee of remote revocation. Use the provider's account controls for remote revocation.

An OpenAI-compatible API key or local model remains available as a fallback. `LLM_BASE_URL` defaults to the OpenAI API base URL, `LLM_MODEL` selects the fallback model, and `LLM_API_KEY` is sent as a bearer key when set. For a local model, set `LLM_BASE_URL` to that provider's OpenAI-compatible API base and leave `LLM_API_KEY` blank if it does not require one. API-key requests use separate API billing. The application sends chat requests through the server; the key is not sent to the browser. `.env` is excluded from Git and the Docker build context.

To pin a specific commit-count container release, set `APP_VERSION` before starting Compose; for example, `$env:APP_VERSION = "3"`. The unified GitHub Actions workflow retains all Go and hosted-browser gates on every push to `main` and version tag. Each platform is built only if its inputs changed since its last verified publication (or a manual force selects it). Containers keep the numeric full-history commit count in `ghcr.io/everyonesoftware/agent:<commit-count>`, `/version`, and OCI labels, plus `latest`, branch/tag, and short-SHA tags when built. Native packages use `0.1.<commit-count>` and share one stable `v0.1.N` release titled `Agent 0.1.N`; unchanged platforms keep their original versions and URLs.

Open http://127.0.0.1:8080. The container is published only on the host loopback interface. To reach it from other devices, configure Tailscale Serve on the host to proxy to `http://127.0.0.1:8080`; do not change the Compose binding to `0.0.0.0` or forward the port from your router. Device-code authentication needs no callback port or `SIWC_REDIRECT_URI` setting.

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
local account, server storage, network access, or device-code ChatGPT sign-in.

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

## Usage history in Settings

Choose **View usage history** in the ChatGPT allowance or Brave Search usage
section. Each popup shows only that provider, with **24 hours**, **1 week**, and
**Entire period** views. ChatGPT quota windows can be selected independently.
Milestone dots support hover, drag, tap-to-pin, and keyboard inspection.

ChatGPT allowance observations are collected about once per minute while the
Agent server is running, even with Settings closed, and retained for 90 days.
Collection uses the same Codex connection as conversations and resumes after server restart;
earlier unrecorded history and collection gaps are not reconstructed. Brave
remaining-search history is reconstructed from the account's successful-request
ledger and requires an enabled Agent cap; it is not Brave's billing balance.
History reads never issue Brave searches or model requests.

Visible Settings and popups refresh allowance/history about once per minute.
The main forecast uses covered portions of a rolling 24-hour window, including
observed idle time. Consumption is measured separately within compatible quota
periods, then added across resets; replenishment and missing intervals are never
counted as consumption or idle time. The popup distinguishes the lookback from
actual coverage, labels short-term or partial estimates, and discloses time spent
depleted. Estimates stop at reset or depletion and disappear when evidence is
insufficient or stale. No token-to-allowance conversion is assumed.

ChatGPT session bands are estimated usage clusters from raw allowance readings,
not individual conversations: 30 minutes without an observed decrease separates
sessions, as do collection gaps, resets and allowance-definition changes. Other
clients can contribute to the provider's allowance. Hover, click/tap, or keyboard
inspection shows session usage and duration in a floating chart popup; no session
caption is added below the chart. Outside interaction dismisses chart and
information popups; Escape dismisses disclosures before the history dialog.

Verify the history UI and production-router/SQLite integration with:

```sh
node tests/usage-history-ui.mjs --browser
AGENT_HISTORY_BROWSER=1 go test -run '^TestUsageHistoryProductionBrowser$' -count=1 -v .
```

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
separate project-access boundary. The **⋯** menu beside **You** offers
**Suggest memories…** without leaving the conversation. Choosing it sends only
that message and extraction instructions to your selected model provider—not
earlier chat history or saved memories. Suggestions are proposals, not saved
facts: review and edit the unchecked items, select the ones you want, and/or add
your own text under **Something else?**, then explicitly save. Empty results and
analysis failures still allow a manual entry. Existing exact matches are marked
as already remembered; other wording may still need your review. No automatic
analysis runs when messages are displayed. Selected entries are saved atomically;
the inline **Undo** removes only entries created by that save and refuses to
delete an entry edited afterward, including edits in another tab. Alternatively, send
`/remember [fact]` to save that exact fact as User memory on the server without
model inference. The model cannot autonomously write memory, and ordinary
natural-language requests do not implicitly save facts. Model-generated
suggestions can be incomplete or incorrect; they never authorize a memory write.

Memory is enabled by default. **Include saved memory in new chats** controls
whether saved entries are sent to your selected model provider on subsequent
ordinary chat requests, including turns in an existing conversation. Disabling
does not delete entries or prevent deliberate edits. Public-web research excludes
saved memory and does not permit `/remember` writes. This does not remove private
information you explicitly put in a research prompt.

Saved facts persist in the local account SQLite database across restarts, with
the same account isolation and plaintext-at-rest protections as conversations.
Do not save secrets. Extraction runs only when you choose **Suggest memories…**;
there is no background extraction or retroactive recall of old chats. Disabling
memory injection does not block an explicit suggestion request, which still
sends the selected message to your provider. Deleting or disabling memory affects future memory injection only:
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
timezone and UTC offset, in both the OpenAI-compatible and ChatGPT Codex paths.
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

## Unified ChatGPT connection (native Codex runtime)

The image includes the official **Codex CLI 0.167.2** (`rust-v0.167.2`)
standalone Linux musl binary at `/usr/local/bin/codex`. It owns the server-side
ChatGPT connection used for inference and allowance. No Codex install on the
host/laptop, Node/npm, Rust toolchain, or additional published port is required.
The runtime remains distroless and runs as UID/GID `65532:65532` with the existing
`/data` mount. An unavailable quota snapshot does not itself disable inference;
an unavailable Codex runtime or expired sign-in does prevent ChatGPT requests.

Agent starts `codex app-server --listen stdio:// -c
cli_auth_credentials_store=\"file\"` as a child process, not an HTTP/WebSocket
listener. `CODEX_USAGE_BINARY` optionally overrides the executable; otherwise
Agent resolves `codex` from `PATH`. An override must be an already-installed
compatible native executable, not a command with shell arguments. The private,
writable `CODEX_HOME` is scoped to the Agent user and selected ChatGPT profile
under `/data/account-data/<AgentUserID>/usage/<profileID>/`; never share it
between users/profiles or mount a personal laptop's Codex credentials there.
Codex manages these credentials on the server. They are plaintext at
rest: protect `/data`, backups, and host ACLs. Do not copy Agent's existing SIWC
refresh tokens into Codex or expose Codex credential files to the browser.

ChatGPT sign-in requires the user's explicit approval of the provider's device-code
flow; eligible accounts may need to enable device-code authorization in ChatGPT
security/beta settings. Live approval and signed-in rate-limit retrieval are not
part of the offline image check. Account operations use Codex account RPCs;
inference and model discovery also use Codex, not direct HTTP Responses calls.
This is **not** a promise that the provider grants a narrower OAuth scope.
Local logout/credential deletion
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
binaries; no dynamic loader or shared-library runtime is added. Agent is not a
general Codex coding/terminal environment: ordinary chat must not acquire local
tools, and research remains limited to Agent's consented, validated public-web
functions. The existing distroless image has no shell or general coding tools.

Operators should update the pin and hashes together after reviewing the official
release, then rerun protocol compatibility and image checks on both supported
architectures. The image never self-updates Codex. To exercise an installed pinned
runtime with a fresh, isolated home (no user credentials or model turn):

```sh
AGENT_CODEX_SMOKE_BINARY=/absolute/path/to/codex \
  go test -run '^TestCodexPinnedProtocolWithoutAuthorization$' -count=1 -v .
```

This checks stdio initialization, signed-out account reads, and the ephemeral,
no-environment thread configuration. It does not approve a login or start a model
turn, and it does not prove signed-in inference or a complete Docker image.

Run the device-flow UI and production-router/SQLite browser acceptance with the
same browser environment as the other application suites:

```sh
node tests/codex-connection-ui.mjs
AGENT_CODEX_BROWSER=1 go test -run '^TestCodexConnectionProductionBrowser$' -count=1 -v .
```

The browser check replaces only the Codex process boundary with explicit local
fixtures. It verifies first sign-in without an active profile, device approval,
model discovery, chat and provider-reported Inspector usage, cancellation that
preserves a working account, password-confirmed switching, and unified disconnect.
It checks narrow phone layouts and makes no live OpenAI requests. Both browser
checks are required release-workflow gates. Real provider sign-in remains a
separate, user-approved validation step.

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

The interface is branded **Agent**, with the **Classic ring** icon: a charcoal
circle inside a sage rounded square. Web, PWA, Android, and Windows share this
artwork. Run `node scripts/generate-branding.mjs` to regenerate the SVG, PNGs,
and Windows ICO, or add `--check` to verify the generated artwork is current.
Existing database, session cookie, model preference, and Compose
identifiers retain their original `everyoneagent` names for compatibility; no data
migration is needed for those identifiers. ChatGPT sign-in is managed by Codex;
saved profiles from the removed Responses integration can be reconnected without
deleting their conversations or recorded allowance history.

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

The app contains local account registration/login, owner-scoped saved conversations, per-user Codex-managed ChatGPT profiles, model discovery, and an OpenAI-compatible fallback. Agent no longer uses direct Responses API inference or loopback ChatGPT authorization. Live subscription sign-in and model eligibility require real provider approval and are not proven by fixture tests. Existing host-wide ChatGPT profile files are not automatically assigned to a new user; each user should connect their own account after signing in. Anyone who can reach the service can create an account, so expose it only to trusted household devices.
