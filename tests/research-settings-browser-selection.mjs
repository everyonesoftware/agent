// Exercise the actual Settings harness's browser selection before launching a browser.
// Simulate installed paths in an isolated child; never modify the parent or host files.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtempSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';

const script = fileURLToPath(new URL('./research-settings-ui.mjs', import.meta.url));
const cases = [
  {name: 'prefers the Chrome used by the other CI suites', paths: ['/usr/bin/google-chrome', '/usr/bin/chromium'], expected: '/usr/bin/google-chrome'},
  {name: 'supports the chromium-browser fallback', paths: ['/usr/bin/chromium-browser'], expected: '/usr/bin/chromium-browser'},
  {name: 'preserves an explicit browser override', paths: ['/usr/bin/google-chrome'], override: '/synthetic/browser-override', expected: '/synthetic/browser-override'},
];
const directory = mkdtempSync(join(process.env.TMPDIR || tmpdir(), 'agent-browser-selection-'));
try {
for (const test of cases) {
  const hook = `
    import assert from 'node:assert/strict';
    import fs from 'node:fs';
    import childProcess from 'node:child_process';
    import {syncBuiltinESMExports} from 'node:module';
    const paths = new Set(${JSON.stringify(test.paths)});
    fs.existsSync = path => paths.has(path);
    childProcess.spawn = executable => {
      assert.equal(executable, ${JSON.stringify(test.expected)}, 'actual Settings browser selection');
      process.exit(0);
    };
    syncBuiltinESMExports();
  `;
  const env = {...process.env, TMPDIR: directory, TEMP: directory, TMP: directory};
  delete env.UI_BROWSER;
  if (test.override) env.UI_BROWSER = test.override;
  const result = spawnSync(process.execPath, ['--import', 'data:text/javascript,' + encodeURIComponent(hook), script], {
    env, encoding: 'utf8', timeout: 10000,
  });
  assert.equal(result.status, 0, test.name + '\n' + (result.stderr || result.error?.message || ''));
  console.log('PASS Settings browser selection ' + test.name);
}
} finally {
  rmSync(directory, {recursive: true, force: true, maxRetries: 10, retryDelay: 150});
}
