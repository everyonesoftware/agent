// Real server + SQLite + headless Chromium acceptance. Only model HTTP is a
// loopback fixture; its canned replies are NOT live model inference.
// node tests/memory-browser.mjs <built-agent-executable>
// Optional: UI_BROWSER, UI_SCREENSHOTS, MEMORY_ARTIFACTS, MEMORY_TIMEOUT_MS.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';

assert.equal(process.argv.length, 3, 'Supply a built Agent executable');
const executable = resolve(process.argv[2]);
assert.ok(existsSync(executable), 'Agent executable missing: ' + executable);
const browserPath = process.env.UI_BROWSER || [
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  '/usr/bin/google-chrome', '/usr/bin/chromium', '/usr/bin/chromium-browser',
].find(existsSync);
assert.ok(browserPath, 'Set UI_BROWSER to an installed Chromium executable');
// Never use Windows system temp: all account data and browser profiles are fake.
const scratch = process.env.MEMORY_SCRATCH || join(homedir(), 'AppData', 'Local', 'hermes', 'cache', 'scratch');
await mkdir(scratch, { recursive: true });
const directory = await mkdtemp(join(scratch, 'agent-memory-browser-'));
const artifacts = resolve(process.env.MEMORY_ARTIFACTS || join(directory, 'artifacts'));
const screenshots = resolve(process.env.UI_SCREENSHOTS || join(artifacts, 'screenshots'));
await mkdir(artifacts, { recursive: true });
await mkdir(screenshots, { recursive: true });
const profile = join(directory, 'browser');
const timeout = Number(process.env.MEMORY_TIMEOUT_MS || 20000);
const providerKey = 'synthetic-memory-fixture-key-not-real';
const fact = 'I prefer concise answers with the synthetic codeword cobalt-otter-731.';
const editedFact = 'I prefer detailed answers with the synthetic codeword amber-finch-942.';
const requests = [], runtimeErrors = [], fixtureErrors = [], passes = [];
let server, browser, socket, nextID = 0, serverLog = '', browserLog = '';
const pending = new Map();
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const provider = createServer(async (req, res) => {
  try {
    assert.equal(req.url, '/v1/chat/completions');
    assert.equal(req.headers.authorization, 'Bearer ' + providerKey);
    let body = ''; for await (const chunk of req) body += chunk;
    requests.push(JSON.parse(body));
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ choices: [{ message: { role: 'assistant', content: 'Synthetic fixture response; no live inference was performed.' } }], usage: { prompt_tokens: 41, completion_tokens: 11, total_tokens: 52 } }));
  } catch (error) {
    fixtureErrors.push(String(error)); res.writeHead(500); res.end('{}');
  }
});
async function listen(http) {
  await new Promise((resolve, reject) => { http.once('error', reject); http.listen(0, '127.0.0.1', resolve); });
  return http.address().port;
}
await listen(provider);
const probe = createServer();
const port = await listen(probe);
await new Promise(resolve => probe.close(resolve));
const origin = 'http://127.0.0.1:' + port;
async function until(fn, message, limit = timeout) {
  const deadline = Date.now() + limit;
  while (Date.now() < deadline) {
    const result = await fn();
    if (result) return result;
    await pause(50);
  }
  throw new Error(message);
}
async function stop(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  const exit = new Promise(resolve => child.once('exit', resolve));
  child.kill();
  await Promise.race([exit, pause(3000)]);
  if (child.exitCode === null && child.signalCode === null) child.kill('SIGKILL');
  await Promise.race([exit, pause(3000)]);
  assert.ok(child.exitCode !== null || child.signalCode !== null, 'Child process did not stop: ' + child.pid);
}
async function start() {
  server = spawn(executable, [], {
    env: { ...process.env, PORT: String(port), TZ: 'UTC', APP_DATA_DIR: join(directory, 'data'), SIWC_DATA_DIR: join(directory, 'data'), LLM_BASE_URL: 'http://127.0.0.1:' + provider.address().port + '/v1', LLM_MODEL: 'memory-fixture-model', LLM_API_KEY: providerKey },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  server.stdout.on('data', data => { serverLog += data; });
  server.stderr.on('data', data => { serverLog += data; });
  server.on('error', error => { serverLog += String(error); });
  await until(async () => {
    if (server.exitCode !== null) throw new Error('Agent exited: ' + serverLog);
    try { return (await fetch(origin + '/healthz', { signal: AbortSignal.timeout(1500) })).ok; } catch { return false; }
  }, 'Agent did not start: ' + executable);
}
function cdp(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++nextID;
    const timer = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); }, timeout);
    pending.set(id, { resolve, reject, timer });
    socket.send(JSON.stringify({ id, method, params }));
  });
}
async function js(expression) {
  const result = await cdp('Runtime.evaluate', { expression: '(async () => { return ' + expression + '; })()', awaitPromise: true, returnByValue: true });
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
  return result.result.value;
}
const eventual = (expression, message) => until(() => js(`(() => {try{return (${expression});}catch{return false;}})()`), message);
const click = selector => js(`(() => {const e=document.querySelector(${JSON.stringify(selector)});if(!e)throw Error('Missing control: '+${JSON.stringify(selector)});e.click();})()`);
const api = (path, body, method = body === undefined ? 'GET' : 'POST') => js(`fetch(${JSON.stringify(path)},{method:${JSON.stringify(method)},headers:{'Content-Type':'application/json'},cache:'no-store',${body === undefined ? '' : 'body:JSON.stringify(' + JSON.stringify(body) + '),'}}).then(async r=>{const text=await r.text();let body;try{body=JSON.parse(text)}catch{body=text}return {status:r.status,cache:r.headers.get('Cache-Control'),body}})`);
const pass = message => { passes.push(message); console.log('PASS ' + message); };
async function screenshot(name) {
  await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
  const { data } = await cdp('Page.captureScreenshot', { format: 'png' });
  await writeFile(join(screenshots, name + '.png'), Buffer.from(data, 'base64'));
}
async function setInput(selector, value) {
  await js(`(() => {const e=document.querySelector(${JSON.stringify(selector)});if(!e)throw Error('Missing input '+${JSON.stringify(selector)});e.value=${JSON.stringify(value)};e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));})()`);
}
async function memories() {
  const result = await api('/api/memories');
  assert.equal(result.status, 200, 'GET /api/memories must exist on the real server');
  assert.equal(typeof result.body.enabled, 'boolean');
  assert.ok(Array.isArray(result.body.entries), 'Memory entries must be an array');
  return result.body;
}
async function openMemory() {
  await js(`(() => {const visible=e=>!!e.getClientRects().length;const direct=[...document.querySelectorAll('button,a')].find(e=>visible(e)&&/^memory$/i.test(e.textContent.trim()));if(direct)direct.click();else document.getElementById('settings-button').click();})()`);
  await eventual(`!!document.querySelector('#memory-settings-card,#memory-card,#memory-panel,#memory-section') || [...document.querySelectorAll('h1,h2,h3')].some(e=>/memory/i.test(e.textContent)&&e.getClientRects().length)`, 'Memory management UI is missing');
  await eventual("!document.querySelector('#memory-create').disabled", 'Memory controls did not load');
  await js(`(() => {const e=document.querySelector('#memory-settings-card,#memory-card,#memory-panel,#memory-section')||[...document.querySelectorAll('h1,h2,h3')].find(e=>/memory/i.test(e.textContent));e.scrollIntoView({block:'start'});})()`);
}
async function buttonInMemory(label, content = '') {
  await js(`(() => {const visible=e=>!!e.getClientRects().length;const root=document.querySelector('#memory-settings-card,#memory-card,#memory-panel,#memory-section')||document.getElementById('settings-panel')||document.body;let scope=root;if(${JSON.stringify(content)}){const row=[...root.querySelectorAll('[data-memory-id],.memory-entry,.memory-item,li,article')].find(e=>e.textContent.includes(${JSON.stringify(content)}));if(!row)throw Error('Memory row missing for '+${JSON.stringify(content)});scope=row;}const b=[...scope.querySelectorAll('button,a')].find(e=>visible(e)&&new RegExp(${JSON.stringify(label)},'i').test((e.getAttribute('aria-label')||'')+' '+e.textContent));if(!b)throw Error('Memory action missing: '+${JSON.stringify(label)});b.click();})()`);
}
async function saveUI(content) {
  await eventual("!document.querySelector('#memory-create').disabled", 'Memory controls did not load');
  await click('#memory-create');
  await eventual("!document.querySelector('#memory-form').hidden", 'Memory save form did not open');
  await eventual(`!!document.querySelector('#memory-content,#memory-input,#memory-add-content,textarea[name="memory"],textarea[name="content"]')`, 'Memory content input missing');
  await setInput('#memory-content,#memory-input,#memory-add-content,textarea[name="memory"],textarea[name="content"]', content);
  await js(`(() => {const c=document.querySelector('#memory-category,#memory-add-category,select[name="category"]');if(c){c.value='user';c.dispatchEvent(new Event('change',{bubbles:true}));}})()`);
  await click('#memory-save');
  return until(async () => (await memories()).entries.find(entry => entry.content === content), 'UI save did not persist the exact fact');
}
async function freshChat(prompt, expected, absent = []) {
  await click('#new-chat');
  await eventual("!document.querySelector('#message-input').disabled && !document.querySelector('#send-button').disabled", 'New chat is not ready');
  const before = requests.length;
  await setInput('#message-input', prompt);
  await js("document.querySelector('#chat-form').requestSubmit()");
  await until(() => requests.length > before, 'Chat did not reach the isolated model fixture');
  await eventual("!document.querySelector('#send-button').disabled && !!document.querySelector('.inspect-run')", 'Chat did not finish');
  assert.equal(requests.length, before + 1, 'Normal chat should make one fixture request');
  const wire = requests.at(-1);
  const id = await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1).dataset.runId");
  const trace = await api('/api/runs/' + id);
  assert.equal(trace.status, 200); assert.equal(trace.body.run.state, 'completed');
  assert.deepEqual(trace.body.run.request, wire, 'Inspector must capture the exact actual provider request');
  const memory = trace.body.run.context.memory;
  assert.notEqual(memory, undefined, 'Inspector must explicitly disclose context.memory');
  const entries = Array.isArray(memory) ? memory : memory.entries;
  assert.ok(Array.isArray(entries), 'context.memory must disclose exact selected memory entries');
  assert.deepEqual(entries, expected, 'Inspector memory must exactly match the selected persisted entries');
  const system = wire.messages.filter(message => message.role === 'system').map(message => message.content).join('\n');
  for (const entry of expected) assert.ok(system.includes(entry.content), 'Persisted fact absent from actual system context');
  for (const old of absent) {
    assert.ok(!JSON.stringify(wire).includes(old), 'Old/disabled memory leaked into fresh-chat wire');
    assert.ok(!JSON.stringify(memory).includes(old), 'Old/disabled memory leaked into fresh-chat memory trace');
  }
  assert.ok(!JSON.stringify(trace).includes(providerKey), 'Fixture provider credential leaked into trace');
  await click('.inspect-run[data-run-id="' + id + '"]');
  await eventual(`!document.querySelector('#run-inspector').hidden && document.querySelector('#run-state').textContent === 'Completed'`, 'Run Inspector did not display the completed run');
  await click('#run-tab-context');
  await eventual(`!document.querySelector('#run-view-context').hidden && ${JSON.stringify(expected)}.every(entry => document.querySelector('#run-memory').textContent.includes(entry.content))`, 'Run Inspector did not render the exact saved memory snapshot');
  for (const old of absent) assert.ok(!(await js("document.querySelector('#run-memory').textContent")).includes(old), 'Old/disabled memory leaked into rendered Run Inspector');
  await screenshot('agent-memory-inspector-' + id);
  await click('#run-close');
  await writeFile(join(artifacts, 'run-' + id + '.json'), JSON.stringify({ wire, run: trace.body.run }, null, 2));
  return trace.body.run;
}
let success = false;
try {
  await start();
  browser = spawn(browserPath, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--remote-debugging-port=0', '--user-data-dir=' + profile, 'about:blank'], { stdio: ['ignore', 'pipe', 'pipe'] });
  browser.stdout.on('data', data => { browserLog += data; });
  browser.stderr.on('data', data => { browserLog += data; });
  browser.on('error', error => { browserLog += String(error); });
  const portText = await until(async () => { try { return await readFile(join(profile, 'DevToolsActivePort'), 'utf8'); } catch { return ''; } }, 'Headless browser did not start');
  const targets = await (await fetch('http://127.0.0.1:' + portText.split('\n')[0] + '/json/list')).json();
  socket = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
  socket.onmessage = event => {
    const data = JSON.parse(event.data);
    if (data.method === 'Runtime.exceptionThrown') runtimeErrors.push(data.params.exceptionDetails.exception?.description || data.params.exceptionDetails.text);
    const job = pending.get(data.id); if (!job) return;
    pending.delete(data.id); clearTimeout(job.timer);
    if (data.error) job.reject(new Error(data.error.message)); else job.resolve(data.result);
  };
  await cdp('Runtime.enable'); await cdp('Page.enable');
  await cdp('Page.setLifecycleEventsEnabled', { enabled: true });
  await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
  await cdp('Page.navigate', { url: origin });
  await eventual("!!document.querySelector('#account-form')", 'Login page missing');
  assert.equal((await api('/api/auth/register', { username: 'memory-browser-primary', password: 'synthetic-memory-primary-password' })).status, 201);
  await cdp('Page.reload');
  await eventual("document.querySelector('#model-select')?.value==='memory-fixture-model' && !document.querySelector('#chat-app').hidden", 'Authenticated fixture workspace missing');
  const initial = await memories();
  assert.deepEqual(initial.entries, []);
  pass('Real browser account loads the production memory API against isolated SQLite');
  if (!initial.enabled) assert.equal((await api('/api/memory-settings', { enabled: true }, 'PUT')).status, 200);
  await openMemory();
  const saved = await saveUI(fact);
  assert.equal(saved.category, 'user'); assert.ok(saved.id); assert.ok(Date.parse(saved.updated_at));
  assert.equal(requests.length, 0, 'Explicit memory UI save must not call a model');
  await eventual(`document.body.textContent.includes(${JSON.stringify(fact)})`, 'Saved entry missing from memory UI');
  await screenshot('agent-memory-real-desktop-1440');
  await cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await js("document.querySelector('#memory-card').scrollIntoView({block:'start'})");
  assert.ok(await js('document.documentElement.scrollWidth<=innerWidth'), 'Memory UI overflows the 390px viewport');
  await screenshot('agent-memory-real-phone-390');
  await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
  pass('Memory save uses real UI and persistence without model inference; desktop and phone screenshots captured');
  await freshChat('Give a short greeting for this brand-new conversation.', [saved]);
  pass('Fresh-chat provider wire includes the saved fact; inspector context.memory equals the persisted entry exactly');
  await click('.remember-message');
  await eventual("!document.querySelector('#memory-form').hidden && !document.querySelector('#memory-save').disabled", 'Remember message did not open a ready editable draft');
  assert.equal(await js("document.querySelector('#memory-content').value"), 'Give a short greeting for this brand-new conversation.');
  assert.deepEqual((await memories()).entries, [saved], 'Remember message must not save before explicit confirmation');
  await click('#memory-cancel');
  assert.deepEqual((await memories()).entries, [saved], 'Cancelling Remember draft changed persisted entries');
  await click('#back-to-chat');
  pass('Remember message opens an exact editable draft; opening and cancelling do not save');
  await openMemory();
  await buttonInMemory('edit', fact);
  await eventual(`!!document.querySelector('#memory-edit-content,textarea[data-memory-id],.memory-entry textarea,.memory-item textarea,#memory-content')`, 'Memory edit input missing');
  await setInput('#memory-edit-content,textarea[data-memory-id],.memory-entry textarea,.memory-item textarea,#memory-content', editedFact);
  await click('#memory-save');
  const edited = await until(async () => (await memories()).entries.find(entry => entry.id === saved.id && entry.content === editedFact), 'UI edit did not persist');
  await freshChat('Greet me in a separate conversation after the preference edit.', [edited], [fact]);
  pass('UI edits replace the exact fact in new-chat wire and memory trace, without stale context');
  const exported = await api('/api/memories/export');
  assert.equal(exported.status, 200, 'Memory export endpoint missing');
  assert.ok(JSON.stringify(exported.body).includes(editedFact), 'Export omits the saved fact');
  assert.ok(!JSON.stringify(exported.body).includes(fact), 'Export contains superseded fact');
  pass('Owner memory export includes the current entry and excludes the superseded fact');
  const stranger = await fetch(origin + '/api/auth/register', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: 'memory-browser-stranger', password: 'synthetic-memory-stranger-password' }), signal: AbortSignal.timeout(timeout) });
  assert.equal(stranger.status, 201);
  const cookie = stranger.headers.get('set-cookie').split(';')[0];
  const otherAPI = async (path, method = 'GET', body) => {
    const result = await fetch(origin + path, { method, headers: { Cookie: cookie, 'Content-Type': 'application/json' }, ...(body === undefined ? {} : { body: JSON.stringify(body) }), signal: AbortSignal.timeout(timeout) });
    const text = await result.text(); let data; try { data = JSON.parse(text); } catch { data = text; }
    return { status: result.status, body: data };
  };
  assert.deepEqual((await otherAPI('/api/memories')).body.entries, []);
  for (const method of ['PUT', 'DELETE']) assert.equal((await otherAPI('/api/memories/' + saved.id, method, method === 'PUT' ? { category: 'user', content: 'foreign overwrite' } : undefined)).status, 404);
  assert.ok(!JSON.stringify((await otherAPI('/api/memories/export')).body).includes(editedFact));
  assert.equal((await memories()).entries[0].content, editedFact);
  assert.equal((await fetch(origin + '/api/memories', { signal: AbortSignal.timeout(timeout) })).status, 401);
  pass('Signed-out access is denied and another account cannot read, export, edit, or delete this account’s memory');
  await stop(server); await start();
  await cdp('Page.reload');
  await eventual("document.querySelector('#model-select')?.value==='memory-fixture-model' && !document.querySelector('#chat-app').hidden", 'Account session did not survive actual server restart');
  assert.deepEqual((await memories()).entries, [edited], 'SQLite memory changed across restart');
  await freshChat('Greet me in a fresh conversation after the server restart.', [edited], [fact]);
  pass('Saved memory and exact fresh-chat injection survive an actual server process restart');
  await openMemory();
  await click('#memory-enabled');
  await eventual("!document.querySelector('#memory-enabled').disabled && !document.querySelector('#memory-enabled').checked", 'Disable memory UI did not settle');
  assert.equal((await memories()).enabled, false, 'Disable memory UI was not persisted');
  await freshChat('Greet me in a fresh conversation with memory disabled.', [], [fact, editedFact]);
  assert.deepEqual((await memories()).entries, [edited], 'Disabling memory must not delete saved entries');
  pass('Disabled memory remains stored but is absent from fresh provider requests and context.memory');
  await openMemory();
  await click('#memory-enabled');
  await eventual("!document.querySelector('#memory-enabled').disabled && document.querySelector('#memory-enabled').checked", 'Re-enable memory UI did not settle');
  assert.equal((await memories()).enabled, true, 'Re-enable memory UI was not persisted');
  await cdp('Page.setInterceptFileChooserDialog', { enabled: true });
  // Production delete may use a native confirmation dialog.
  socket.addEventListener('message', event => {
    const data = JSON.parse(event.data);
    if (data.method === 'Page.javascriptDialogOpening') void cdp('Page.handleJavaScriptDialog', { accept: true });
  });
  await buttonInMemory('delete|remove', editedFact);
  await click('[data-memory-confirm="' + edited.id + '"]');
  await until(async () => (await memories()).entries.length === 0, 'UI delete did not remove the persisted entry');
  await freshChat('Greet me in a fresh conversation after deleting the preference.', [], [fact, editedFact]);
  pass('UI deletion removes memory from SQLite, the next fresh-chat wire, and the exact inspector memory context');
  const callsBeforeRemember = requests.length;
  await click('#new-chat');
  await setInput('#message-input', '/remember ' + fact);
  await js("document.querySelector('#chat-form').requestSubmit()");
  const remembered = await until(async () => (await memories()).entries.find(entry => entry.content === fact), '/remember did not save the explicit fact');
  await eventual("!document.querySelector('#send-button').disabled", '/remember did not settle');
  assert.equal(requests.length, callsBeforeRemember, '/remember must invoke the real server memory tool without provider inference');
  const rememberID = await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1)?.dataset.runId");
  assert.ok(rememberID, '/remember must expose its real server tool execution in Run Inspector');
  const rememberTrace = await api('/api/runs/' + rememberID);
  assert.equal(rememberTrace.status, 200);
  assert.equal(rememberTrace.body.run.state, 'completed');
  assert.equal(rememberTrace.body.run.request, undefined, '/remember must not invent a provider request');
  assert.ok(rememberTrace.body.run.events.some(event => event.name === 'memory_save' && event.state === 'completed'), 'Remember trace lacks a completed actual memory-save event');
  await click('.inspect-run[data-run-id="' + rememberID + '"]');
  await eventual("!document.querySelector('#run-inspector').hidden && document.querySelector('#run-events').textContent.includes('Explicit /remember command saved local user memory; no model request sent')", 'Run Inspector does not disclose the actual /remember execution');
  await screenshot('agent-memory-remember-inspector');
  await click('#run-close');
  await writeFile(join(artifacts, 'remember-run.json'), JSON.stringify(rememberTrace.body.run, null, 2));
  await freshChat('Greet me after an explicit remember command in another chat.', [remembered]);
  pass('/remember persists through a real server tool without model inference and supplies memory to the next chat');
  assert.deepEqual(runtimeErrors, []); assert.deepEqual(fixtureErrors, []);
  success = true;
  console.log('PASS real-browser memory acceptance (isolated provider fixture; no live inference)');
} catch (error) {
  console.error('FAIL memory acceptance: ' + (error.stack || error));
  if (socket?.readyState === WebSocket.OPEN) {
    await screenshot('agent-memory-failure').catch(() => {});
    await writeFile(join(artifacts, 'failure-dom.html'), await js('document.documentElement.outerHTML').catch(String));
  }
  process.exitCode = 1;
} finally {
  await writeFile(join(artifacts, 'result.json'), JSON.stringify({ success, passes, runtimeErrors, fixtureErrors, requestCount: requests.length, fixtureOnly: true }, null, 2));
  await writeFile(join(artifacts, 'server.log'), serverLog);
  await writeFile(join(artifacts, 'browser.log'), browserLog);
  if (socket) socket.close();
  for (const job of pending.values()) { clearTimeout(job.timer); job.reject(new Error('Browser closed')); }
  pending.clear();
  try { await stop(browser); } finally {
    try { await stop(server); } finally {
      provider.closeAllConnections();
      await new Promise(resolve => provider.close(resolve));
      // Keep failure evidence / wire traces, never browser cookies or SQLite.
      await rm(profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 150 });
      await rm(join(directory, 'data'), { recursive: true, force: true, maxRetries: 20, retryDelay: 150 });
    }
  }
  console.log('Artifacts: ' + artifacts);
  console.log('Screenshots: ' + screenshots);
}
