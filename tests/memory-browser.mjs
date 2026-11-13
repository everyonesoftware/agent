// Real server + SQLite + headless Chromium acceptance. Only model HTTP is a
// loopback fixture; its canned replies are NOT live model inference.
// node tests/memory-browser.mjs <built-agent-executable>
// Optional: UI_BROWSER, UI_SCREENSHOTS, MEMORY_ARTIFACTS, MEMORY_TIMEOUT_MS.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { spawnTestBrowser, waitForTestBrowser, stopTestBrowser } from './browser-launcher.mjs';
import { existsSync } from 'node:fs';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
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
let suggestionReply = null, suggestionDelay = 0;
let pauseUndoDelete = false, pausedUndoDelete = null;
let server, browser, socket, nextID = 0, serverLog = '', browserLog = '';
const pending = new Map();
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const provider = createServer(async (req, res) => {
  try {
    assert.equal(req.url, '/v1/chat/completions');
    assert.equal(req.headers.authorization, 'Bearer ' + providerKey);
    let body = ''; for await (const chunk of req) body += chunk;
    requests.push(JSON.parse(body));
    const content = suggestionReply ?? 'Synthetic fixture response; no live inference was performed.';
    const delay = suggestionDelay;
    if (delay) await pause(delay);
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ choices: [{ message: { role: 'assistant', content } }], usage: { prompt_tokens: 41, completion_tokens: 11, total_tokens: 52 } }));
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
  browser = spawnTestBrowser(browserPath, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--remote-debugging-port=0', '--user-data-dir=' + profile, 'about:blank'], { stdio: ['ignore', 'pipe', 'pipe'] });
  browser.stdout.on('data', data => { browserLog += data; });
  browser.stderr.on('data', data => { browserLog += data; });
  browser.on('error', error => { browserLog += String(error); });
  const portText = await waitForTestBrowser(browser, profile);
  const targets = await (await fetch('http://127.0.0.1:' + portText.split('\n')[0] + '/json/list')).json();
  socket = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
  socket.onmessage = event => {
    const data = JSON.parse(event.data);
    if (data.method === 'Runtime.exceptionThrown') runtimeErrors.push(data.params.exceptionDetails.exception?.description || data.params.exceptionDetails.text);
    if (data.method === 'Fetch.requestPaused') {
      if (pauseUndoDelete && data.params.request.method === 'DELETE') pausedUndoDelete = data.params;
      else void cdp('Fetch.continueRequest', { requestId: data.params.requestId }).catch(error => runtimeErrors.push(String(error)));
    }
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
  const sourceMessage = 'I prefer quiet outdoor activities and don’t drink alcohol. This weekend I’m visiting my sister. ' + fact;
  await freshChat(sourceMessage, [saved]);
  pass('Fresh-chat provider wire includes the saved fact; inspector context.memory equals the persisted entry exactly');
  const suggestedFacts = ['I prefer quiet outdoor activities.', 'I don’t drink alcohol.', fact];
  const callsBeforeMenu = requests.length;
  await click('.message-actions-toggle');
  assert.equal(requests.length, callsBeforeMenu, 'Opening message actions must not invoke inference');
  assert.ok(await js("(() => {const b=document.querySelector('.message-actions-toggle'),s=b.closest('.sender');return !!s && Math.abs(b.getBoundingClientRect().top-s.getBoundingClientRect().top)<44;})()"), 'Action belongs beside You, not in a footer');
  suggestionReply = JSON.stringify({ suggestions: suggestedFacts });
  await click('[data-suggest-memory]');
  await eventual("document.querySelectorAll('.memory-suggestion-select').length===3", 'Suggestion checklist did not load');
  assert.ok(await js("document.querySelector('#settings-panel').hidden"), 'Suggestions must not navigate to Settings');
  assert.ok(await js("[...document.querySelectorAll('.memory-suggestion-select')].every(e=>!e.checked)"), 'Suggestions must start unchecked');
  assert.equal(await js("document.querySelectorAll('.memory-suggestion-select:disabled').length"), 1, 'Existing memory must be marked and disabled');
  assert.ok(await js("document.querySelector('#memory-suggestion-save').disabled"), 'Empty selection cannot be saved');
  assert.deepEqual((await memories()).entries, [saved], 'Analysis must not save memory');
  const analysisWire = requests.at(-1);
  assert.equal(analysisWire.messages.length, 2, 'Analysis must contain only server instructions and the selected message');
  assert.deepEqual(analysisWire.messages[1], { role: 'user', content: sourceMessage });
  assert.ok(!analysisWire.messages[0].content.includes(saved.content), 'Saved memories must not enter analysis instructions');
  await screenshot('agent-memory-suggestions-desktop');
  await click('#memory-suggestion-cancel');
  assert.deepEqual((await memories()).entries, [saved], 'Cancelling suggestions must not save');
  assert.equal(await js("document.activeElement.classList.contains('message-actions-toggle')"), true, 'Cancel restores message action focus');
  pass('Opt-in analysis sends only the selected message; unchecked checklist, duplicates and cancellation preserve memory');

  for (const width of [390, 320]) {
    await cdp('Emulation.setDeviceMetricsOverride', { width, height: 875, deviceScaleFactor: 1, mobile: true });
    await click('.message-actions-toggle'); await click('[data-suggest-memory]');
    await eventual("document.querySelectorAll('.memory-suggestion-select').length===3", 'Phone suggestions did not load');
    assert.ok(await js('document.documentElement.scrollWidth<=innerWidth'), 'Suggestion sheet overflows ' + width);
    assert.ok(await js("(() => {const r=document.querySelector('#memory-suggestion-save').getBoundingClientRect();return r.bottom<=innerHeight && r.right<=innerWidth && r.height>=44;})()"), 'Phone save button must be visible and touch sized');
    await screenshot('agent-memory-suggestions-phone-' + width);
    await cdp('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 });
    await cdp('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27 });
    await eventual("!document.querySelector('#memory-suggestion-dialog').open", 'Escape did not close phone sheet');
  }
  await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
  await click('.message-actions-toggle'); await click('[data-suggest-memory]');
  await eventual("document.querySelectorAll('.memory-suggestion-select').length===3", 'Checklist did not reload');
  const selectedFact = 'I prefer quiet trails and gardens.';
  const customFact = 'I prefer weekend activities close to home.';
  await setInput('.memory-suggestion-text', selectedFact);
  await click('.memory-suggestion-select:not(:disabled)');
  await setInput('#memory-suggestion-custom', customFact);
  const callsBeforeSave = requests.length;
  await click('#memory-suggestion-save');
  await eventual("!document.querySelector('#memory-suggestion-dialog').open && !!document.querySelector('[data-memory-undo]')", 'Selected memory save did not settle');
  const afterSelection = (await memories()).entries;
  assert.deepEqual(afterSelection.map(e => e.content).sort(), [fact, selectedFact, customFact].sort(), 'Only selected edited text and custom fact should persist');
  assert.equal(requests.length, callsBeforeSave, 'Explicit selection save must not invoke inference');
  await screenshot('agent-memory-suggestions-saved');
  await click('[data-memory-undo]');
  await until(async () => JSON.stringify((await memories()).entries) === JSON.stringify([saved]), 'Undo must remove only newly created memories');
  await eventual("!document.querySelector('[data-memory-undo]')", 'Undo confirmation did not finish verification');
  pass('Desktop and narrow phone flows work; selected edits and custom text save explicitly, with verified Undo');

  suggestionReply = JSON.stringify({ suggestions: [] });
  await click('.message-actions-toggle'); await click('[data-suggest-memory]');
  await eventual("/lasting/i.test(document.querySelector('#memory-suggestion-status').textContent)", 'Empty analysis did not finish');
  await setInput('#memory-suggestion-custom', fact.toUpperCase());
  await click('#memory-suggestion-save');
  await eventual("!document.querySelector('#memory-suggestion-dialog').open", 'A differently cased existing fact must verify without a spurious unknown outcome');
  assert.deepEqual((await memories()).entries, [saved], 'Normalized duplicates must not create a new memory');
  assert.ok(await js("document.querySelector('.message.user').textContent.includes('Already remembered')"), 'Duplicate save must not claim a new memory was created');

  // Interleave a real second-tab-equivalent edit AFTER Undo's GET, BEFORE DELETE.
  suggestionReply = JSON.stringify({ suggestions: [suggestedFacts[0]] });
  await click('.message-actions-toggle'); await click('[data-suggest-memory]');
  await eventual("document.querySelectorAll('.memory-suggestion-select').length===1", 'Race-case suggestion did not load');
  await click('.memory-suggestion-select'); await click('#memory-suggestion-save');
  await eventual("!document.querySelector('#memory-suggestion-dialog').open && !!document.querySelector('[data-memory-undo]')", 'Race-case memory save did not finish');
  const raceEntry = (await memories()).entries.find(entry => entry.content === suggestedFacts[0]);
  assert.ok(raceEntry);
  pauseUndoDelete = true;
  await cdp('Fetch.enable', { patterns: [{ urlPattern: origin + '/api/memories/*', requestStage: 'Request' }] });
  await click('[data-memory-undo]');
  await until(() => pausedUndoDelete, 'Undo DELETE did not reach the interception point');
  const raceEdit = 'This preference was edited in another tab.';
  const raceUpdate = await api('/api/memories/' + raceEntry.id, { category: 'user', content: raceEdit }, 'PUT');
  assert.equal(raceUpdate.status, 200);
  const deleteHeaders = pausedUndoDelete.request.headers;
  await cdp('Fetch.continueRequest', { requestId: pausedUndoDelete.requestId });
  await eventual("!document.querySelector('[data-memory-undo]')?.disabled", 'Conditional Undo did not settle');
  await cdp('Fetch.disable'); pauseUndoDelete = false; pausedUndoDelete = null;
  assert.ok((await memories()).entries.some(entry => entry.id === raceEntry.id && entry.content === raceEdit), 'Undo must not delete an edit committed between its GET and DELETE');
  const revisionHeader = Object.entries(deleteHeaders).find(([name]) => name.toLowerCase() === 'if-match')?.[1];
  assert.equal(revisionHeader, JSON.stringify(raceEntry.updated_at), 'Undo must send the original revision, not a fresh snapshot revision');
  assert.ok(await js("/changed|unknown|refresh/i.test(document.querySelector('[data-memory-undo]').parentElement.textContent)"), 'Changed memory must produce a conflict, not a false undone claim');
  assert.equal((await api('/api/memories/' + raceEntry.id, undefined, 'DELETE')).status, 200);
  assert.deepEqual((await memories()).entries, [saved]);
  pass('Server-side Undo revision check preserves an edit interleaved after the initial read');

  suggestionReply = JSON.stringify({ suggestions: [] });
  await click('.message-actions-toggle'); await click('[data-suggest-memory]');
  await eventual("/lasting|durable|clear/i.test(document.querySelector('#memory-suggestion-status').textContent)", 'Empty suggestion result needs meaningful guidance');
  assert.equal(await js("document.querySelectorAll('.memory-suggestion-select').length"), 0);
  await setInput('#memory-suggestion-custom', 'Keep this draft through retry.');
  await click('#memory-suggestion-cancel');
  suggestionReply = 'invalid JSON fixture result';
  await click('.message-actions-toggle'); await click('[data-suggest-memory]');
  await eventual("!!document.querySelector('#memory-suggestion-error').textContent", 'Invalid provider reply should show an inline error');
  await setInput('#memory-suggestion-custom', 'Keep this draft through retry.');
  suggestionReply = JSON.stringify({ suggestions: [] });
  await click('#memory-suggestion-retry');
  await eventual("!document.querySelector('#memory-suggestion-retry').getClientRects().length", 'Analysis retry did not recover');
  assert.equal(await js("document.querySelector('#memory-suggestion-custom').value"), 'Keep this draft through retry.');
  await click('#memory-suggestion-cancel');
  assert.deepEqual((await memories()).entries, [saved]);
  suggestionReply = JSON.stringify({ suggestions: suggestedFacts }); suggestionDelay = 600;
  const callsBeforeDelayedAnalysis = requests.length;
  await click('.message-actions-toggle'); await click('[data-suggest-memory]');
  await until(() => requests.length > callsBeforeDelayedAnalysis, 'Delayed suggestion request missing');
  await click('#new-chat');
  await pause(800);
  assert.equal(await js("document.querySelector('#memory-suggestion-dialog').open"), false, 'Stale analysis cannot reopen a dialog after navigation');
  suggestionReply = null; suggestionDelay = 0;
  assert.deepEqual((await memories()).entries, [saved]);
  pass('Empty results and malformed-model errors remain editable; retry preserves custom text and stale analysis cannot save or reopen');
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
  try { await stopTestBrowser(browser); } finally {
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
