import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import { installationOptions, manifest } from './browser-tools/install.mjs';

const root = new URL('../', import.meta.url);
test('CI browser and installer versions and archive integrity are exact pins', async () => {
  assert.match(manifest.dependencies['@puppeteer/browsers'], /^\d+\.\d+\.\d+$/);
  const options = installationOptions('browser-fixture-cache');
  assert.equal(options.browser, 'chrome');
  assert.equal(options.platform, 'linux');
  assert.match(options.buildId, /^\d+\.\d+\.\d+\.\d+$/);
  assert.match(options.expectedHash, /^[a-f0-9]{64}$/);
  assert.equal(options.installDeps, false);
  assert.equal(installationOptions('browser-fixture-cache', true).installDeps, true);
  const lock = JSON.parse(await readFile(new URL('tests/browser-tools/package-lock.json', root), 'utf8'));
  assert.equal(lock.packages['node_modules/@puppeteer/browsers'].version, manifest.dependencies['@puppeteer/browsers']);
  assert.match(lock.packages['node_modules/@puppeteer/browsers'].integrity, /^sha512-/);
});
test('release CI provisions and checks a controlled browser before app tests without dropping any gate', async () => {
  const workflow = await readFile(new URL('.github/workflows/publish-image.yml', root), 'utf8');
  const verify = workflow.slice(workflow.indexOf('  verify:'), workflow.indexOf('  publish:'));
  assert.match(verify, /runs-on: ubuntu-22\.04/);
  assert.match(verify, /npm ci --prefix tests\/browser-tools --ignore-scripts/);
  assert.match(verify, /node tests\/browser-tools\/install\.mjs/);
  assert.ok(verify.indexOf('node tests/browser-tools/preflight.mjs') < verify.indexOf('node tests/ui.mjs'));
  for (const gate of ['node tests/ui.mjs','node tests/pwa-updates.mjs','node tests/memory-ui.mjs','TestResearchSettingsBrowser','tests/run-inspector-browser.mjs','tests/memory-browser.mjs','tests/research-browser.mjs','tests/pwa-browser.mjs']) assert.ok(verify.includes(gate), 'required real test gate: ' + gate);
  assert.match(verify, /if: failure\(\)/);
  assert.match(verify, /browser-diagnostics/);
  assert.doesNotMatch(verify, /--no-sandbox|continue-on-error/);
});
