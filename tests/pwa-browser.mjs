// Real production-server PWA integration. No request interception or production credentials.
// Build two Agent executables with different -X main.version values, then run:
// node tests/pwa-browser.mjs <first-executable> <second-executable>
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const binaries = process.argv.slice(2).map(p => resolve(p));
assert.equal(binaries.length, 2, 'Supply two built Agent executables');
for (const path of binaries) assert.ok(existsSync(path), 'Executable missing: ' + path);
const browserPath = process.env.UI_BROWSER || [
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  '/usr/bin/google-chrome', '/usr/bin/chromium', '/usr/bin/chromium-browser',
].find(existsSync);
assert.ok(browserPath, 'Set UI_BROWSER to an installed Chromium executable');
const directory = await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'agent-pwa-'));
const profile = join(directory, 'browser');
const probe = createServer();
await new Promise(resolve => probe.listen(0, '127.0.0.1', resolve));
const port = probe.address().port;
await new Promise(resolve => probe.close(resolve));
const origin = 'http://127.0.0.1:' + port;
let server, browser, socket;
let nextID = 0;
const pending = new Map();
const errors = [];
async function until(fn, message, timeout = 20000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await fn();
    if (value) return value;
    await new Promise(resolve => setTimeout(resolve, 60));
  }
  throw new Error(message);
}
async function stop(child) {
  if (!child || child.exitCode !== null) return;
  await new Promise(resolve => { child.once('exit', resolve); child.kill(); });
}
async function start(binary) {
  server = spawn(binary, [], { env: { ...process.env, PORT: String(port), APP_DATA_DIR: join(directory, 'data'), SIWC_DATA_DIR: join(directory, 'data'), MODEL_PROVIDER: 'openai', OPENAI_API_KEY: '', OPENAI_BASE_URL: 'http://127.0.0.1:1' }, stdio: 'ignore' });
  await until(async () => { try { return (await fetch(origin + '/healthz')).ok; } catch { return false; } }, 'Production server did not start');
}
function cdp(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++nextID;
    const timer = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); }, 20000);
    pending.set(id, { resolve, reject, timer });
    socket.send(JSON.stringify({ id, method, params }));
  });
}
async function js(expression) {
  const result = await cdp('Runtime.evaluate', { expression: '(async () => { return ' + expression + '; })()', awaitPromise: true, returnByValue: true });
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
  return result.result.value;
}
const eventual = (expression, message) => until(() => js(`(() => { try { return (${expression}); } catch { return false; } })()`), message);
const clickText = text => js(`Array.from(document.querySelectorAll('button')).find(e=>e.textContent.trim()===${JSON.stringify(text)}).click()`);
async function viewport(width, height) {
  await cdp('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: width < 600 });
  await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
}
async function screenshot(name) {
  if (!process.env.UI_SCREENSHOTS) return;
  const { data } = await cdp('Page.captureScreenshot', { format: 'png' });
  await writeFile(join(process.env.UI_SCREENSHOTS, name + '.png'), Buffer.from(data, 'base64'));
}
function pass(message) { console.log('PASS ' + message); }
try {
  await start(binaries[0]);
  const versionResponse = await fetch(origin + '/version.json');
  assert.equal(versionResponse.status, 200, 'Production update metadata must be served');
  const first = await versionResponse.json();
  assert.ok(first.buildId);
  browser = spawn(browserPath, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--remote-debugging-port=0', '--user-data-dir=' + profile, 'about:blank'], { stdio: 'ignore' });
  const portText = await until(async () => { try { return await readFile(join(profile, 'DevToolsActivePort'), 'utf8'); } catch { return ''; } }, 'Browser did not start');
  const targets = await (await fetch('http://127.0.0.1:' + portText.split('\n')[0] + '/json/list')).json();
  socket = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
  socket.onmessage = event => {
    const data = JSON.parse(event.data);
    if (data.method === 'Runtime.exceptionThrown') errors.push(data.params.exceptionDetails.exception?.description || data.params.exceptionDetails.text);
    const job = pending.get(data.id);
    if (!job) return;
    pending.delete(data.id); clearTimeout(job.timer);
    if (data.error) job.reject(new Error(data.error.message)); else job.resolve(data.result);
  };
  await cdp('Page.enable'); await cdp('Runtime.enable'); await cdp('Network.enable');
  await viewport(1280, 900);
  await cdp('Page.navigate', { url: origin });
  await eventual('navigator.serviceWorker.controller', 'Worker did not take control');
  const manifest = await cdp('Page.getAppManifest');
  assert.equal(manifest.errors.length, 0, JSON.stringify(manifest.errors));
  assert.equal(JSON.parse(manifest.data).display, 'standalone');
  const installability = await cdp('Page.getInstallabilityErrors');
  assert.deepEqual(installability.installabilityErrors, []);
  pass('Production manifest is standalone and Chromium reports no installability errors');
  assert.equal(await js(`(await fetch('/api/auth/register',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'pwa-browser',password:'synthetic-pwa-browser-only'})})).status`), 201);
  await cdp('Page.reload');
  await eventual("document.querySelector('#chat-app') && !document.querySelector('#chat-app').hidden", 'Real account login missing');
  await js("document.querySelector('#settings-button').click()");
  await clickText('Check for updates');
  await eventual("document.querySelector('#settings-panel').textContent.includes('latest version')", 'Current version status missing');
  for (const width of [1280, 390]) {
    await viewport(width, 850);
    assert.ok(await js('document.documentElement.scrollWidth<=innerWidth'), 'Horizontal overflow at ' + width);
    await js("Array.from(document.querySelectorAll('h2')).find(e=>e.textContent==='Web app updates').scrollIntoView({block:'center'})");
    await screenshot('agent-updates-' + width);
  }
  pass('Real Settings panel checks current version at desktop and phone widths');
  const cacheContents = await js(`Promise.all((await caches.keys()).map(async name=>({name,urls:(await (await caches.open(name)).keys()).map(r=>new URL(r.url).pathname)})))`);
  assert.equal(cacheContents.length, 1);
  assert.deepEqual(cacheContents[0].urls, ['/offline.html']);
  // CDP network emulation alone does not reliably affect service-worker targets.
  // Stop the isolated origin too, proving fallback against real connection loss.
  await stop(server);
  await cdp('Network.emulateNetworkConditions', { offline: true, latency: 0, downloadThroughput: 0, uploadThroughput: 0 });
  await clickText('Check for updates');
  await eventual("document.querySelector('#settings-panel').textContent.match(/offline|internet|connection/i)", 'Offline check feedback missing');
  assert.deepEqual(await js(`Promise.all(['GET','POST'].map(async method=>{try{await fetch('/api/conversations',{method});return 'unexpected';}catch{return 'failed';}}))`), ['failed', 'failed']);
  await cdp('Page.navigate', { url: origin + '/' });
  await eventual("document.title==='Agent · offline' && document.querySelector('h1')?.textContent==='Agent is offline'", 'Offline fallback missing');
  assert.ok(await js('!document.querySelector("#chat-app")'), 'Offline fallback must not contain private chat shell');
  await cdp('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
  await cdp('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
  assert.ok(await js("document.activeElement.matches('a') && getComputedStyle(document.activeElement).outlineStyle==='solid'"), 'Offline retry must have visible keyboard focus');
  await screenshot('agent-offline');
  await start(binaries[0]);
  await cdp('Network.emulateNetworkConditions', { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
  // Follow the actual offline retry link. Scheduling a #settings navigation and
  // immediately reloading races Chromium's same-document navigation commit.
  await js("document.querySelector('a[href=\"/\"]').click()");
  await eventual("document.querySelector('#chat-app') && !document.querySelector('#chat-app').hidden", 'Reconnect did not restore the signed-in app');
  await js("document.querySelector('#settings-button').click()");
  await eventual("document.querySelector('#settings-panel') && !document.querySelector('#settings-panel').hidden", 'Reconnect settings missing');
  pass('Only public disconnected page is cached; API GET/POST fail offline and reconnect restores account');
  await js("document.querySelector('#message-input').value='Draft remains while postponing' ");
  await stop(server); await start(binaries[1]);
  const second = await (await fetch(origin + '/version.json')).json();
  assert.notEqual(first.buildId, second.buildId);
  await clickText('Check for updates');
  await eventual("Array.from(document.querySelectorAll('button')).some(e=>e.textContent.trim()==='Update and reload' && !e.hidden && !e.disabled)", 'Update offer missing');
  for (const width of [1280, 390, 320]) {
    await viewport(width, 850);
    await js("document.querySelector('#pwa-updates').scrollIntoView({block:'center'})");
    assert.ok(await js('document.documentElement.scrollWidth<=innerWidth'), 'Available-update controls overflow at ' + width);
    assert.ok(await js("Array.from(document.querySelectorAll('#pwa-updates button')).every(e=>{const r=e.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&r.width>0})"), 'Available-update action clipped at ' + width);
    await screenshot('agent-update-available-' + width);
  }
  await clickText('Later');
  assert.equal(await js("document.querySelector('#message-input').value"), 'Draft remains while postponing');
  assert.ok(await js(`document.querySelector('meta[name="app-build-id"]').content===${JSON.stringify(first.buildId)}`), 'Later unexpectedly reloaded');
  // Explicit discard confirmation is tested without accepting a native browser dialog.
  await js('window.confirm=()=>false');
  await clickText('Update and reload');
  assert.equal(await js("document.querySelector('#message-input').value"), 'Draft remains while postponing');
  await js("document.querySelector('#message-input').value=''");
  await clickText('Update and reload');
  await eventual(`document.querySelector('meta[name="app-build-id"]')?.content===${JSON.stringify(second.buildId)}`, 'Explicit update did not reload new production build');
  await eventual("document.querySelector('#chat-app') && !document.querySelector('#chat-app').hidden", 'Login was not retained after update');
  const finalCaches = await js(`Promise.all((await caches.keys()).map(async name=>({name,urls:(await (await caches.open(name)).keys()).map(r=>new URL(r.url).pathname)})))`);
  assert.equal(finalCaches.length, 1, 'Old offline cache should be removed after activation');
  assert.deepEqual(finalCaches[0].urls, ['/offline.html']);
  pass('Real deployment change offers update; Later and cancelled draft discard preserve page; explicit apply reloads new build');
  assert.deepEqual(errors, [], 'Runtime errors');
  console.log('Production PWA browser integration passed.');
} finally {
  socket?.close();
  for (const job of pending.values()) clearTimeout(job.timer);
  await stop(browser); await stop(server);
  await rm(directory, { recursive: true, force: true, maxRetries: 15, retryDelay: 200 });
}
