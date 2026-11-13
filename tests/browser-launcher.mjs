// Shared by fixture-only browser suites. Never disables Chromium's sandbox.
import { spawn, spawnSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { appendFileSync, mkdirSync } from 'node:fs';
import { join, resolve } from 'node:path';

const states = new WeakMap();
const diagnosticBytes = 8192;
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const exited = child => child.exitCode !== null || child.signalCode !== null;

function describe(child, reason) {
  const state = states.get(child);
  return new Error('Browser startup ' + reason + '\nExecutable: ' + state.executable + '\nVersion: ' + state.version + '\nExit code: ' + child.exitCode + '; signal: ' + child.signalCode + '\n' + (state.error ? 'Spawn error: ' + state.error.message + '\n' : '') + 'Bounded browser stderr:\n' + state.stderr.toString('utf8'));
}
function record(child, text) {
  const state = states.get(child);
  if (!state.artifact) return;
  try { appendFileSync(state.artifact, text); }
  catch (error) { console.error('Could not write browser diagnostics: ' + error.code); }
}
export function spawnTestBrowser(executable, args, options = {}) {
  const env = options.env || process.env;
  // Windows GUI browsers may treat --version as a normal launch and touch a
  // personal profile. Only use that CLI on platforms that support it; the CI
  // preflight independently verifies the actual CDP version after startup.
  const versionResult = process.platform === 'win32' ? {} : spawnSync(executable, ['--version'], { env, encoding: 'utf8', timeout: 5000, maxBuffer: 4096, windowsHide: true });
  const version = (process.platform === 'win32' ? 'CLI version not queried on Windows; CI verifies CDP version' : versionResult.stdout || versionResult.stderr || versionResult.error?.message || 'unavailable').trim().slice(0, 512);
  console.log('Browser executable: ' + executable + '\nBrowser version: ' + version);
  const child = spawn(executable, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'] });
  const artifact = env.BROWSER_ARTIFACTS ? join(resolve(env.BROWSER_ARTIFACTS), 'browser-' + (child.pid || 'spawn-failed') + '.log') : '';
  states.set(child, { executable, version, artifact, stderr: Buffer.alloc(0), error: null });
  if (artifact) {
    try { mkdirSync(resolve(env.BROWSER_ARTIFACTS), { recursive: true }); }
    catch (error) { console.error('Could not create browser diagnostics directory: ' + error.code); }
  }
  record(child, 'Executable: ' + executable + '\nVersion: ' + version + '\n');
  // Drain output while preserving pipes for callers that also collect their logs.
  child.stdout.resume();
  child.stderr.on('data', chunk => {
    const state = states.get(child);
    state.stderr = Buffer.concat([state.stderr, Buffer.from(chunk)]).subarray(-diagnosticBytes);
  });
  child.on('error', error => { states.get(child).error = error; });
  child.on('exit', (code, signal) => record(child, 'Exit code: ' + code + '; signal: ' + signal + '\nBounded stderr:\n' + states.get(child).stderr.toString('utf8') + '\n'));
  return child;
}

export async function waitForTestBrowser(child, profile, { timeoutMs = 20000 } = {}) {
  const state = states.get(child);
  if (!state) throw new Error('Use spawnTestBrowser before waiting for browser readiness.');
  const deadline = Date.now() + timeoutMs;
  try {
    while (Date.now() < deadline) {
      if (state.error) throw describe(child, 'failed to spawn');
      if (exited(child)) throw describe(child, 'exited before DevTools was ready');
      try {
        const portText = await readFile(join(profile, 'DevToolsActivePort'), 'utf8');
        if (/^\d+\r?\n\/devtools\/browser\//.test(portText)) {
          record(child, 'DevTools readiness confirmed\n');
          return portText;
        }
      } catch (error) {
        // Windows can briefly deny reads while Chromium holds an exclusive file handle.
        if (error.code !== 'ENOENT' && !(process.platform === 'win32' && error.code === 'EBUSY')) throw describe(child, 'could not read readiness file: ' + error.code);
      }
      await pause(40);
    }
    throw describe(child, 'timed out waiting for DevTools after ' + timeoutMs + 'ms');
  } catch (error) {
    record(child, error.message + '\n');
    throw error;
  }
}

export async function stopTestBrowser(child, { timeoutMs = 3000 } = {}) {
  if (!child || !child.pid || exited(child)) return;
  for (const signal of ['SIGTERM', 'SIGKILL']) {
    if (exited(child)) return;
    child.kill(signal);
    const deadline = Date.now() + timeoutMs;
    while (!exited(child) && Date.now() < deadline) await pause(25);
    if (exited(child)) return;
  }
  throw new Error('Test browser process did not stop: ' + child.pid);
}
