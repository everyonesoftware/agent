# Agent development

## Product and layout

Agent is a private household chat service with native Android/Windows clients and a server-served web client. Root Go files own the HTTP API, account/session/SQLite state, Codex-managed ChatGPT connections, public-web research and durable allowance/memory history. `page.go` embeds the production web UI. `pwa/` contains only the deliberately limited public offline/install assets.

- `apps/android/`: native Kotlin/Compose client; read its build and validation instructions before edits.
- `apps/windows/`: native WinUI 3 client and platform-independent C# protocol/update tests; read its build and validation instructions.
- `scripts/`: pinned runtime installation, native release orchestration, publication/readback and Services notification tools.
- `tests/`: Node protocol/browser/release checks. Browser tooling is locked under `tests/browser-tools/`.
- `docs/NATIVE-CLIENT-CONTRACT.md`: native design, protocol, lifecycle and publication boundaries.

Use the existing shared checkout. Preserve other work; do not add worktrees, commit, push, publish, replace signing keys or change another session's device without authorization. New code/scripts/tests must not use Python.

## Native fidelity and privacy

Native apps are not WebViews. Preserve the approved charcoal/sage conversation workspace and Music-style native title bar/update controls. Settings order is Updates, Connection, ChatGPT Connection, Web Research, Memory, Reset Password, Sign Out. The native ChatGPT panel mirrors current `page.go` compact account/status/allowance layout. Keep mockup sample accounts, quotas, credentials, simulation/viewer controls and calibration out of production.

Read actual Go handlers and web callers before implementing API consumers; never guess JSON names. Sessions use server-issued cookies, persisted only in OS-protected client storage. Passwords are transient; ChatGPT/Brave credentials stay on the Agent server. Require HTTPS outside explicit loopback development. Never forward cookies through redirects or to update/GitHub endpoints. Never automatically replay mutations or infer successful writes from an ambiguous response. Account/endpoint/navigation transitions invalidate stale async results.

Ordinary chat is tool-free. Public-web research requires task-specific approval; saving a Brave key is not consent. Memory saves are explicit and account-scoped. Distinguish unknown/stale allowance from zero or current values. Preserve history evidence across account connection changes.

## Verification

Server baseline:

```sh
go test ./...
go vet ./...
node --test tests/native-server-packaging.test.mjs
```

The production workflow `.github/workflows/publish-image.yml` is the complete validation/publication contract. Preserve every existing real-browser gate. Locally source `/opt/data/tools/agent-test-env.sh`, run its pinned-browser preflight and the workflow's browser checks. Do not disable Chromium sandboxing or weaken a gate to accommodate a host failure. Native unit tests, compile checks, real platform execution, installed-app smoke and end-to-end upgrades are different evidence; report each honestly.

`AGENT_NATIVE_FIXTURE=1 go test -run '^TestNativeClientAcceptanceFixture$' -count=1 -v -timeout=25m .` starts an opt-in twenty-minute loopback fixture using the real router/SQLite and a local test provider. It is excluded from the shipped executable and never uses live accounts. Set `AGENT_NATIVE_FIXTURE_PORT` if needed. Register ordinary test accounts; there is no authentication bypass. Native emulator access uses task-owned explicit-serial `adb reverse`, never a shared/public fixture listener.

## Release rules

Use one main push workflow, one build job per platform, selective rebuilds from verified publication baselines, and one serialized native-release/download-table publication job. Native versions are `0.1.<full Git commit count>`; retain the server container's existing numeric version identity. Retain unchanged platform outputs at their original release URLs, versions and source revisions. Never overwrite published binaries or label an unchanged installer as a newly built version.

Use a single stable `Agent 0.1.N` GitHub release containing Android APK and Windows installer/feed/package as applicable. README and release description share a verified Current downloads inventory. Android is signed with its dedicated Agent identity; Windows uses the existing unsigned distribution policy. Release verification requires exact-source manifest checks and downloaded-byte readback, not filenames or a successful upload alone. Monitor all exact-pushed-SHA GitHub Actions to completion, and report each platform's build/publication outcome with the exact release URL.

Keep signing recovery files and secrets outside the checkout. Never print credentials, read personal account storage, or commit keystores. Updaters are separate from app-auth transport and require explicit Download then Install/Restart and install; never downgrade or auto-apply at startup.
