// A real sandbox-enabled blank-browser/CDP smoke check, before app assertions.
import assert from 'node:assert/strict';
import { mkdir, mkdtemp, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { spawnTestBrowser, waitForTestBrowser, stopTestBrowser } from '../browser-launcher.mjs';

assert.ok(process.env.UI_BROWSER, 'Preflight requires the explicitly installed UI_BROWSER');
assert.match(process.env.AGENT_TEST_BROWSER_VERSION || '', /^\d+\.\d+\.\d+\.\d+$/, 'Preflight requires the exact expected browser version');
const scratch = process.env.RUNNER_TEMP || process.env.TMPDIR;
assert.ok(scratch, 'Set RUNNER_TEMP or TMPDIR to isolated test storage');
await mkdir(scratch, { recursive: true });
const profile = await mkdtemp(join(scratch, 'agent-browser-preflight-'));
let child;
try {
  child = spawnTestBrowser(process.env.UI_BROWSER, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--remote-debugging-port=0', '--user-data-dir=' + profile, 'about:blank']);
  const portText = await waitForTestBrowser(child, profile);
  const response = await fetch('http://127.0.0.1:' + portText.split('\n')[0].trim() + '/json/version', { signal: AbortSignal.timeout(5000) });
  assert.equal(response.status, 200);
  const version = await response.json();
  assert.equal(version.Browser.split('/').at(-1), process.env.AGENT_TEST_BROWSER_VERSION, 'Actual DevTools browser must match the pinned version');
  assert.ok(version.webSocketDebuggerUrl, 'DevTools WebSocket endpoint missing');
  console.log('PASS sandbox-enabled Chrome for Testing startup and real CDP version: ' + version.Browser);
} finally {
  await stopTestBrowser(child);
  await rm(profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 150 });
}
