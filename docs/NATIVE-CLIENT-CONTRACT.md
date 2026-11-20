# Agent native clients — implementation contract

## Scope and design

Native Android (Kotlin/Compose) and native Windows (WinUI 3/C#) clients connect to the existing Agent HTTP server. They are not WebViews, do not embed an LLM runtime, and do not store ChatGPT/Brave credentials locally. Preserve the web/container product. No mockup controls, sample messages/accounts, simulated requests or fabricated allowance values in production.

Approved visual reference: [`design/agent-native-approved.html`](design/agent-native-approved.html), preserved from `/opt/data/cache/scratch/agent-native-study/index.html`. It is a self-contained design study, not shipped client code. Charcoal `#202322`, sidebar `#181b1a`, field `#252a27`, borders `#333936`, text `#e1e6e2`, sage `#b4d6a3`. Native title bar follows Music. Android uses a conversation drawer and reachable composer, not desktop navigation squeezed onto a phone.

The approved production icon supersedes the study's original mark: **Classic ring**, a charcoal circle inside a sage rounded square. `scripts/generate-branding.mjs` owns the exact SVG/raster geometry and regenerates web/PWA, Android launcher/in-app, and Windows PNG/ICO artwork together. Run it with `--check` to verify all generated outputs; do not introduce per-platform text marks or alternate logo proportions.

Windows taskbar branding is separate from the custom title-bar image. Bind the running HWND's taskbar icon to an immutable content-addressed ICO while retaining Velopack's existing application identity and stable launcher. Clear window-owned Shell properties before close. Native acceptance must inspect those Shell properties, actual native icon pixels, and the visible Explorer taskbar; a correct title-bar screenshot alone is not taskbar evidence. Do not clear global icon caches, restart Explorer, or alter user pins as a branding repair.

Settings order: Updates, Connection, ChatGPT Connection, Web Research, Memory, Reset Password, Sign Out. ChatGPT Connection follows CURRENT `page.go` markup and `refreshConnectionData`/`renderUsage`: status-refresh pill, accessible active-account selector, Add account / Disconnect, Allowance freshness and refresh, compact remaining/window/reset rows, View usage history, collapsed About allowance. Omit the mockup's original avatar and Default model control. Native accessibility/font scaling must retain readable text and touch targets.

## Server protocol — authoritative existing Go implementation

Do not invent or change endpoints. Inspect handler request/reply structs and `page.go` call sites. Use normal cookie sessions, never put credentials in URLs or persist passwords. Native persistent session tokens belong in Android Keystore-protected storage / Windows PasswordVault. Endpoint metadata may be local; conversations remain server-side. Require HTTPS except explicit loopback development. Never forward cookies through redirects or send them to GitHub/updater URLs. No automatic mutation retries.

- `/api/auth/register`, `/api/auth/login`: POST `{username,password}`, receive `Set-Cookie: everyoneagent_session=…`; `/api/auth/me` identifies the signed-in account. `/api/auth/logout` signs out this session. `/api/auth/password` requires current/new passwords and revokes other sessions. Read actual JSON keys.
- `/api/conversations`, `/api/conversations/{id}`: authenticated listing and messages. `/api/chat` POST `{messages:[{role,content}], model, conversation_id?, run_id?, omitted_messages?, research_task_id?}` returns `{reply,conversation_id?,run_id?}`. Observe server 40-message / 8KiB-per-message / 64KiB-request bounds. No hidden inference on startup.
- `/api/models` discovers real models. `/api/runs/{id}` provides actual run inspection.
- `/api/auth/status`, `/api/auth/profiles`; POST `/auth/start` with `{expected_active_profile_id}` and optional `?mode=add|retry&profile_id=…`; device approval/poll/cancel routes are in `usage_service.go`. Use returned verification URL/code; validate browser URL. Preserve old active account on failed/canceled add. Active switching and disconnect require a freshly entered Agent password via the existing protected endpoints; never reuse stored sign-in passwords.
- `/api/usage/status`, `/api/usage/refresh`, `/api/usage/history`: inspect actual profile/connection preconditions, query schema and timestamp units. Missing/stale/error data must remain honest. Opening Settings/history must not cause inference or searches.
- Web Research, task consent, budgets, and memory use the existing `research*.go`, `memory.go`, and web call sites. Real task-scoped explicit consent is required before research; saving a key does not grant task consent. Keep ordinary chat tool-free.
- Endpoint changes, sign-out, expired sessions, canceled requests and account switches invalidate in-flight responses and account-scoped UI state. No default example server/account.

## Versioning and release interface

Native version = `0.1.<full Git commit count>`; Android versionCode = count. Build source SHA is recorded in release metadata. Derive from full history, reject shallow/uncommitted publication. Initial native releases have no legacy installed clients; do not create Music-specific compatibility aliases.

One main push workflow (extend `.github/workflows/publish-image.yml`), one build job per platform (Android, Windows, container). Shared validation/final release/Services notification jobs allowed. Select each build against its last verified publication; unchanged platforms retain original release links/version/source. Do not rebuild/relabel unchanged binaries. One stable main release `v0.1.N`, titled `Agent 0.1.N`, with Music-style managed Current downloads table. Keep native updater discovery independent of GitHub Latest. README and release use the same verified inventory. No separate Windows prerelease.

Android artifact: `Agent-android.apk`, `Agent-android.apk.sha256`, `agent-android-release.json`.
Manifest schema: `{schemaVersion:1, platform:"android", versionName:"0.1.N", versionCode:N, commit:"<40-char SHA>", apk:{name:"Agent-android.apk",size:<bytes>,sha256:"<lowercase hex>"}, signingCertificateSha256:"<lowercase 64 hex>"}`. Updater enumerates paginated stable GitHub releases and chooses highest valid platform version, validates exact package `com.everyonesoftware.agent`, versionCode, size/hash and same signer before user-consented install. Do not downgrade or fall back to an older candidate after highest-candidate validation failure. Signing env: AGENT_KEYSTORE_PATH, AGENT_KEYSTORE_PASSWORD, AGENT_KEY_ALIAS, AGENT_KEY_PASSWORD; Actions also AGENT_KEYSTORE_BASE64. Dedicated signing key provisioning is user-authorized.

Windows artifacts: `AgentWindows-win-x64-Setup.exe`, checksum with `.sha256` suffix, Velopack `releases.win-x64.json`, matching `AgentWindows-0.1.N-win-x64-full.nupkg`, legacy RELEASES file only if real pinned vpk emits it, and `agent-windows-release.json`.
Windows manifest: `{schemaVersion:1, platform:"windows", version:"0.1.N", versionCode:N, commit:"<40-char SHA>", installer:{name:"AgentWindows-win-x64-Setup.exe",size:<bytes>,sha256:"<lowercase hex>"}, package:{name:"AgentWindows-0.1.N-win-x64-full.nupkg",size:<bytes>,sha256:"<lowercase hex>"}}`. Windows pack script may emit this directly; release tooling must verify bytes rather than regenerate conflicting metadata. Velopack pinned 1.2.161, app/package id AgentWindows. SetAutoApplyOnStartup(false), AllowVersionDowngrade=false. Check → Download with progress → explicit Restart and install. Publish full self-contained payload and real feed; native installed-window smoke gates publication.

Native updater transport is independent of Agent authentication. Never show illustrative versions or fabricated current/available results. First-run Not checked yet, checking, unavailable/retry, current, available/changelog, download/verify, ready, installation failure are distinct. Use the compact Music Current Version header, top-right offered version, collapsed What's new grouped as Improvements/Fixes with original source details; notes use actual installed/offered source range, not release-description tables.

## Ownership during parallel implementation

- Android worker: `apps/android/**` only.
- Windows worker: `apps/windows/**`, `scripts/windows-build.ps1`, `scripts/test-windows-installer.ps1` only.
- CI/release worker: `.github/workflows/publish-image.yml`, new native release/selection/download tooling and tests under `scripts/` and `tests/`, root README/release docs/.gitignore. Do not modify existing Services notification tooling or existing browser CI assertions without coordinating parent.
- Parent owns this contract, AGENTS.md, server compatibility/integration tests, signing provisioning, final integration/review/commit/push/publication verification. No child commits, pushes, or remote writes.

Use test-first vertical slices, real protocol fixtures, platform builds and actual native verification where available. Compilation, fixture integration, native launch, signed packaging, installation/update acceptance, and hosted publication are separate gates. Record unverified gates plainly.
