import assert from 'node:assert/strict';
import { test } from 'node:test';
import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, rm, readFile, writeFile } from 'node:fs/promises';
import { homedir, tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnTestBrowser, waitForTestBrowser, stopTestBrowser } from './browser-launcher.mjs';

async function fixture(t) {
  const scratch = process.platform === 'win32' ? join(homedir(), 'AppData', 'Local', 'hermes', 'cache', 'scratch') : process.env.RUNNER_TEMP || process.env.TMPDIR || tmpdir();
  await mkdir(scratch, { recursive: true });
  const directory = await mkdtemp(join(scratch, 'agent-browser-unit-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  return directory;
}
function fakeBrowser(profile, code) {
  return spawnTestBrowser(process.execPath, ['-e', code, profile]);
}

test('reports the actual spawn failure promptly instead of a readiness timeout', async t => {
  const profile = await fixture(t);
  const child = spawnTestBrowser(join(profile, 'missing-browser'), []);
  await assert.rejects(waitForTestBrowser(child, profile, { timeoutMs: 2000 }), /spawn.*ENOENT/i);
  await stopTestBrowser(child);
});

test('reports early exit with bounded stderr and exit status', async t => {
  const profile = await fixture(t);
  const child = fakeBrowser(profile, "process.stderr.write('sandbox-startup-fixture\\n');process.exit(23)");
  await assert.rejects(waitForTestBrowser(child, profile), error => /23/.test(error.message) && /sandbox-startup-fixture/.test(error.message));
  await stopTestBrowser(child);
});

test('reports signal termination and cleanup does not await an already emitted exit', async t => {
  const profile = await fixture(t);
  const child = fakeBrowser(profile, 'setInterval(()=>{},1000)');
  const exited = new Promise(resolve => child.once('exit', resolve));
  child.kill('SIGKILL');
  await exited;
  await assert.rejects(waitForTestBrowser(child, profile), /SIGKILL|signal/i);
  await stopTestBrowser(child, { timeoutMs: 100 });
});

test('successful startup reads DevTools readiness and stops the real child', async t => {
  const profile = await fixture(t);
  const child = fakeBrowser(profile, "require('fs').writeFileSync(require('path').join(process.argv[1],'DevToolsActivePort'),'9222\\n/devtools/browser/fixture');setInterval(()=>{},1000)");
  t.after(() => stopTestBrowser(child));
  const portText = await waitForTestBrowser(child, profile);
  assert.equal(portText, '9222\n/devtools/browser/fixture');
  await stopTestBrowser(child);
  assert.ok(child.exitCode !== null || child.signalCode !== null);
});

async function lockReadinessFile(file) {
  const quoted = file.replaceAll("'", "''");
  const locker = spawn('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', "$ErrorActionPreference='Stop'; $f=[System.IO.File]::Open('" + quoted + "',[System.IO.FileMode]::Open,[System.IO.FileAccess]::ReadWrite,[System.IO.FileShare]::None); try { [Console]::WriteLine('LOCKED'); Start-Sleep -Milliseconds 700 } finally { $f.Dispose() }"], { stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true });
  try {
    await new Promise((resolve, reject) => {
      let output = '';
      let stderr = '';
      const timer = setTimeout(() => finish(new Error('Readiness file locker did not become ready within 5000ms: ' + stderr)), 5000);
      const onData = chunk => {
        output = (output + chunk).slice(-1024);
        if (output.includes('LOCKED')) finish();
      };
      const onStderr = chunk => { stderr = (stderr + chunk).slice(-1024); };
      const onError = error => finish(error);
      const onExit = (code, signal) => finish(new Error('Readiness file locker exited before readiness: ' + code + '; ' + signal + '; ' + stderr));
      function finish(error) {
        clearTimeout(timer);
        locker.stdout.off('data', onData);
        locker.stderr.off('data', onStderr);
        locker.off('error', onError);
        locker.off('exit', onExit);
        if (error) reject(error);
        else resolve();
      }
      locker.stdout.on('data', onData);
      locker.stderr.on('data', onStderr);
      locker.once('error', onError);
      locker.once('exit', onExit);
    });
    return locker;
  } catch (error) {
    await stopTestBrowser(locker);
    throw error;
  }
}

for (const expires of [false, true]) {
  test('Windows readiness sharing violation ' + (expires ? 'respects the existing deadline' : 'recovers after the exclusive lock is released'), { skip: process.platform !== 'win32' }, async t => {
    const profile = await fixture(t);
    const file = join(profile, 'DevToolsActivePort');
    const portText = '9222\n/devtools/browser/sharing-fixture';
    await writeFile(file, portText);
    const child = fakeBrowser(profile, 'setInterval(()=>{},1000)');
    let locker;
    try {
      locker = await lockReadinessFile(file);
      // Prove the real Windows sharing violation, rather than mocking readFile.
      await assert.rejects(readFile(file, 'utf8'), { code: 'EBUSY' });
      if (expires) {
        await assert.rejects(waitForTestBrowser(child, profile, { timeoutMs: 100 }), /timed out waiting for DevTools after 100ms/);
      } else {
        assert.equal(await waitForTestBrowser(child, profile, { timeoutMs: 2500 }), portText);
      }
      assert.equal(child.exitCode, null);
      assert.equal(child.signalCode, null);
    } finally {
      await Promise.all([stopTestBrowser(child), stopTestBrowser(locker)]);
    }
  });
}

test('non-transient readiness read errors fail fast', async t => {
  const profile = await fixture(t);
  await mkdir(join(profile, 'DevToolsActivePort'));
  const child = fakeBrowser(profile, 'setInterval(()=>{},1000)');
  try {
    await assert.rejects(waitForTestBrowser(child, profile, { timeoutMs: 2000 }), /could not read readiness file: EISDIR/);
  } finally {
    await stopTestBrowser(child);
  }
});

test('timeout includes bounded diagnostics and leaves cleanup to the caller', async t => {
  const profile = await fixture(t);
  const child = fakeBrowser(profile, "process.stderr.write('x'.repeat(30000)+'TAIL-DIAGNOSTIC');setInterval(()=>{},1000)");
  t.after(() => stopTestBrowser(child));
  await assert.rejects(waitForTestBrowser(child, profile, { timeoutMs: 500 }), error => /timed out/i.test(error.message) && /TAIL-DIAGNOSTIC/.test(error.message) && error.message.length < 12000);
  await stopTestBrowser(child);
});

test('writes startup diagnostics to the explicitly requested artifact directory', async t => {
  const profile = await fixture(t);
  const child = spawnTestBrowser(process.execPath, ['-e', "process.stderr.write('synthetic-browser-artifact');process.exit(9)"], { env: { ...process.env, BROWSER_ARTIFACTS: profile } });
  await assert.rejects(waitForTestBrowser(child, profile), /synthetic-browser-artifact/);
  const log = await readFile(join(profile, 'browser-' + child.pid + '.log'), 'utf8');
  assert.match(log, /synthetic-browser-artifact/);
  assert.match(log, /node|\.exe/i);
  await stopTestBrowser(child);
});
