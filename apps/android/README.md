# Agent Android

Native Kotlin/Compose client; protocol and privacy boundaries are in
`../../docs/NATIVE-CLIENT-CONTRACT.md`.

## Local validation

From the repository root, source `/opt/data/tools/android-env.sh`, then run:

```sh
bash apps/android/gradlew -p apps/android :app:testDebugUnitTest :app:lintDebug :app:assembleDebug :app:assembleDebugAndroidTest --console=plain --max-workers=2
node --test apps/android/*.test.mjs
```

Read the JUnit XML under `app/build/test-results/testDebugUnitTest`; compilation
alone is not native execution. Use only an explicitly task-owned emulator serial.
Do not restart the shared adb server or operate on another task's emulator.

For controller-only feedback while another worker owns the Gradle outputs,
use the already resolved pinned Kotlin/JUnit dependencies (no downloads or
Gradle daemon). Source the Android environment for `JAVA_HOME`, then point
`GRADLE_USER_HOME` at the cache populated by Gradle. On this WSL toolchain Java's
home differs from the shell's home, so use the explicit resolved cache:

```sh
export GRADLE_USER_HOME=/opt/data/.gradle
node apps/android/controller-tests.mjs
node apps/android/controller-router-tests.mjs
```

The first command compiles the real controller/API/protocol and runs coroutine
HTTP regressions plus existing API/protocol tests in isolated scratch output.
The second injects a test-only Go file through an overlay and runs two Kotlin
controller tests against the actual Go router/SQLite/password-cookie handlers.
It seeds only isolated fixture accounts, disables live Codex access, and never
changes production routes. `AGENT_CONTROLLER_SOURCE` can select a scratch copy
of a previous controller for red/green comparisons. These checks do not replace
Gradle unit/lint/compile gates or native Compose/emulator execution.


Start the isolated twenty-minute real-router fixture (no live provider accounts):

```sh
AGENT_NATIVE_FIXTURE=1 AGENT_NATIVE_FIXTURE_PORT=19371 go test -run '^TestNativeClientAcceptanceFixture$' -count=1 -v -timeout=25m .
```

Install both debug APKs on the owned emulator, reverse only its port, and run:

```sh
adb -s "$OWNED_SERIAL" reverse tcp:19371 tcp:19371
adb -s "$OWNED_SERIAL" shell am instrument -w -r -e agentFixture http://127.0.0.1:19371/ com.everyonesoftware.agent.test/androidx.test.runner.AndroidJUnitRunner > "$RESULT_LOG" 2>&1
node apps/android/verify-instrumentation.mjs "$RESULT_LOG" 6
```

The full suite requires `agentFixture`; omitting it must not report a false pass.
The validator requires the exact JUnit total and final instrumentation completion,
and rejects failures, crashes, assumption skips and truncated output. adb may exit
zero on a failed test. Keep long instrumentation commands in tracked background
jobs and await their actual result.

`app/src/test/resources/valid-update.apk` is a small, test-only signed archive
containing the debug binary manifest and an uncompressed tamper marker. Its
self-signed test certificate is unrelated to the dedicated production signing
identity. No private key is checked in. `ApkSignatureTest` mutates signed payload
bytes and requires cryptographic rejection even if download metadata were changed.

## Release

Use the dedicated Agent signing environment supplied by the parent/release job.
Never print signing variables or put keystores in this checkout. `assembleRelease`
requires full history, a clean committed tree and the actual commit-count version.
A dirty development checkout cannot produce a publication-ready build; do not
weaken `validateRelease` to bypass that boundary. Signed packaging, native UI
execution, installed-app smoke, end-to-end update installation and published-byte
readback are separate gates.
