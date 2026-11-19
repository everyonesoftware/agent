# Native release publication and Current downloads

## One pipeline, independent platform identity

`.github/workflows/publish-image.yml` is the single main push pipeline. `verify` retains every existing Go, browser, production-server and sandbox preflight gate. One job builds each platform: `android`, `windows`, and `publish` (the existing container job ID). The final `release` job accepts successful or deliberately skipped platforms. It publishes only changed output, then refreshes both managed download tables even on a docs-only/no-op run. Existing Services notification and retry payload tooling remain separate from native publication and retain their token/OIDC/Tailscale boundaries.

The top-level non-canceling `services-publish-<repository>` queue serializes publishers and table writers. No second push-triggered refresh workflow is introduced. The existing Services connection-test dispatch workflow is retained.

Native versions use `0.1.<full Git commit count>`; Android `versionCode` is the same count. Containers retain their numeric count identity. Releases are stable `v0.1.N`, titled `Agent 0.1.N`, never a separate Windows prerelease. Every manifest binds its source to the actual release tag's resolved commit. Full Git history and a clean committed source tree are required for publication.

`native-selection.mjs` compares each platform to its last **verified publication**, not the preceding push. Initial missing baselines select all platforms. Invalid source/tag/manifest/feed/bytes abort discovery; they never silently grant a skip or choose a lower candidate. Failed or unpublished changes remain selected on later pushes. Divergent history requires operator review. Manual workflow `component` controls force one platform or all; `auto` uses verified baselines.

Native directory changes affect their own platform. Go/server/web/container inputs affect the container. Download-table/release/changelog/selector scripts, their explicit tests, README, docs, and `.gitignore` are control-plane inputs, not binary inputs. Unknown inputs conservatively affect all platforms. Workflow changes use a deliberately restricted extractor: platform jobs are independent; permissions/concurrency are shared; unknown, quoted, duplicate, or unsupported YAML structure fails closed to a rebuild. Adding a new shared workflow key requires extending the extractor and its real-Git regressions first.

## Exact packaging interfaces

The authoritative schema is [NATIVE-CLIENT-CONTRACT.md](NATIVE-CLIENT-CONTRACT.md).

- Android: `bash apps/android/gradlew -p apps/android -PagentCommitCount=N :app:testDebugUnitTest :app:lintDebug :app:assembleRelease`. CI installs SDK/build-tools 36.0.0 and Java 21. The signing step consumes `AGENT_KEYSTORE_BASE64`, `AGENT_KEYSTORE_PASSWORD`, `AGENT_KEY_ALIAS`, and `AGENT_KEY_PASSWORD`, decoding to the task-owned `AGENT_KEYSTORE_PATH` and deleting it with a shell trap. No secret is logged or committed. `package-android` invokes real `apksigner verify --verbose --print-certs` and `aapt dump badging` before writing `Agent-android.apk`, its `.sha256`, and `agent-android-release.json`. Package ID and version must match `com.everyonesoftware.agent` / full-history count.
- Windows: `./scripts/windows-build.ps1 -OutputDirectory dist/windows -Version 0.1.N -Commit SHA`. It must emit artifacts directly into that directory: `AgentWindows-win-x64-Setup.exe`, its `.sha256`, `AgentWindows-0.1.N-win-x64-full.nupkg`, `releases.win-x64.json`, and `agent-windows-release.json`. Packaging owns the manifest; publication validates it rather than replacing it. Only real optional pinned-vpk `RELEASES`, `RELEASES-win-x64`, and `assets.win-x64.json` outputs may accompany it. CI calls `./scripts/test-windows-installer.ps1 -InstallerPath dist/windows/AgentWindows-win-x64-Setup.exe -EvidenceDirectory dist/windows-smoke`; installation/native-window smoke must succeed before artifacts can publish. Windows remains explicitly unsigned.
- Container: existing multiarchitecture build/push and Services capture are preserved. `native-release.mjs container` reads the numeric registry tag back with Docker Buildx and writes `agent-container-release.json` with source SHA, numeric version, image reference, and exact digest. Discovery checks the registry descriptor digest again. A successful build alone is not a verified publication baseline. Anonymous pull access to `ghcr.io/everyonesoftware/agent` is a publication prerequisite: the verification and release jobs intentionally receive no registry write credentials. If the package becomes private, configure isolated read-only registry authentication in both jobs before publishing again.

Release upload never uses `--clobber`. Existing source/title/tag conflicts or differing asset bytes abort publication. Identical existing assets are retained on retry. New assets are downloaded through their exact asset IDs and byte-compared after upload; platform manifests, sizes, checksums, and the Windows full-package feed are revalidated before claiming publication. A new release stays draft until all selected inventories pass, then its stable identity/tag is read back. The published container and Services notification can succeed independently of a later native job failure; do not describe the whole run as unpublished merely because the final native release is blocked.

## Structured change history

`native-changelog.mjs` reads actual Git subjects, descriptions, hashes, and changed paths over the complete verified per-platform source range, including skipped releases. It groups original-source-derived text into Improvements/Fixes, retaining every commit in Commit details. Clearly docs/CI-only and other-native-platform-only changes stay out of the compact summary. Ambiguous relevant changes are retained. Publication adds `agent-<platform>-changelog.json` assets and real change notes; managed download tables are not changelogs. Initial releases have `from: null` and the complete history through the offered commit. Clients comparing arbitrary installed versions must compare those actual source revisions, not simply concatenate the newest release body.

## Read-only checks and previews

```sh
node --test tests/native-*.test.mjs tests/current-downloads.test.mjs tests/browser-launcher.test.mjs tests/browser-ci.test.mjs
node scripts/native-selection.mjs --repository everyonesoftware/agent --force none
node scripts/current-downloads.mjs --repository everyonesoftware/agent --output-directory /absolute/scratch/agent-download-preview
node scripts/native-release.mjs verify --directory dist
# Existing actionlint release gate:
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -shellcheck= -pyflakes= -ignore '^unexpected key "queue" for "concurrency" section[.]' .github/workflows/publish-image.yml
```

Preview never changes GitHub. It writes `inventory.json`, `README.md`, and `release.md` to the explicitly requested output directory. Discovery paginates releases and assets and resolves Android/Windows independently of GitHub Latest. Unchanged platforms retain the original version-specific URLs and source identity; their assets are never copied into a newer tag. No prior native release means honest Not yet published rows, not speculative links. Existing container tags without verified publication manifests are not presented as a newly verified release.

The authorized final CI job invokes:

```sh
node scripts/native-release.mjs publish --repository "$GH_REPO" --directory dist
node scripts/current-downloads.mjs --repository "$GH_REPO" --output-directory dist/current-downloads --publish
```

Both table targets are rendered from the same verified inventory. All prose outside the managed markers is preserved. README writes use the Contents API's current blob SHA and are read back. Release-description writes check for observable edits, serialize automated writers, and read the exact release back. **GitHub release GET/PATCH is not an atomic compare-and-swap**: do not edit a release body manually during refresh; a human edit between its final GET and PATCH can still be overwritten. Observable active concurrent main publishers cause a refresh failure/retry, not an unsafe write. Documentation commits made by `GITHUB_TOKEN` do not trigger recursive push builds.

Fixture tests cover real Git baselines/ranges, bytes, source conflicts, idempotent no-clobber publication, exact CLI argument contracts, managed prose, and external readbacks. Local fixtures and source-contract tests do not establish hosted runners, Windows installation/update behavior, Android device installation, or an actual published release. Those acceptance gates must be reported separately after the authorized run.
