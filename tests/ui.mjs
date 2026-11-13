// Dependency-free browser regression test for the actual embedded production page.
// Run with Node 22+ and UI_BROWSER pointing to a Chromium executable.
// All API data is confined to this local test server, never production storage.
import assert from 'node:assert/strict';
import { spawnTestBrowser, waitForTestBrowser, stopTestBrowser } from './browser-launcher.mjs';
import { existsSync } from 'node:fs';
import { readFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const browserPath = process.env.UI_BROWSER || [
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  '/usr/bin/google-chrome', '/usr/bin/chromium', '/usr/bin/chromium-browser',
].find(existsSync);
assert.ok(browserPath, 'Set UI_BROWSER to an installed Chromium executable');
const htmlSource = await readFile(join(root, 'page.go'), 'utf8');
const html = htmlSource.slice(htmlSource.indexOf('`') + 1, htmlSource.lastIndexOf('`')).replaceAll('{{APP_VERSION}}', '42').replaceAll('{{APP_BUILD_ID}}', 'ui-fixture');
const pwaSource = await readFile(join(root, 'pwa_updates.go'), 'utf8');
const pwaScript = pwaSource.slice(pwaSource.indexOf('`') + 1, pwaSource.lastIndexOf('`'));
const profile = await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'agent-ui-'));
let authenticated = false;
let authFailure = true;
let listFailure = false;
let loadFailure = false;
let chatFailure = false;
let chatErrorAction = 'manage_usage';
let logoutFailure = false;
let passwordFailure = false;
let chatDelay = 0;
let loadDelay = 0;
let activeProfile = 'p1';
let profileFailure = false;
let disconnectFailure = false;
let connectionStatusFailure = false;
let configuredLocalURL = 'http://127.0.0.1:8182';
let profilesFailure = false;
let modelsFailure = false;
let connectionDelay = 0;
let authorizationFailure = false;
let authorizationDelay = 0;
let modelsDelay = 0;
let remoteRevoked = true;
let runReadFailure = false;
const runs = new Map();
function fixtureRun(id, state = 'completed', messages = [{role:'user',content:'Plan a quiet week.'}]) {
  return {
    id, state, conversation_id:'one', started_at:'2026-11-11T18:00:00-08:00',
    finished_at:state === 'running' ? '' : '2026-11-11T18:00:02.400-08:00', duration_ms:2400,
    provider:'Isolated fixture provider', model:'test-one', retries:0, http_status:state==='failed'?429:200,
    context:{current_date:'2026-11-11', timezone:'America/Los_Angeles', utc_offset:'-08:00', instructions_version:'baseline-v1',
      instructions:'Missing knowledge is not evidence that an event has not happened. No live retrieval is available.',
      messages:[{role:'system',content:'Current date: 2026-11-11. No tools available.'},...messages], omitted_messages:0, tools:[]},
    events:[{name:'Context prepared',detail:'Server-supplied date and baseline instructions.',at:'2026-11-11T18:00:00-08:00',state:'completed'},
      {name:state==='failed'?'Provider failed':state==='running'?'Waiting for response':'Response received',detail:'Fixture event only.',at:'2026-11-11T18:00:02.400-08:00',state:state==='failed'?'failed':state==='running'?'running':'completed'}],
    request:{model:'test-one',messages:[{role:'system',content:'Current date: 2026-11-11.'},...messages]},
    response:state==='running'?undefined:{text:'<script>window.traceExecuted=true</script> <img src=x onerror=alert(1)>'},
    error:state==='failed'?'Fixture provider failure.':undefined,
    request_truncated:false,response_truncated:false,
  };
}
runs.set('history-run', fixtureRun('history-run'));
const providerProfiles = [
  { id: 'p1', label: 'Personal', email: 'personal@example.com', connected: true, plan_usage_enabled: true, oauth_selected: true },
  { id: 'p2', label: 'Other', email: 'other@example.com', connected: true, plan_usage_enabled: true, oauth_selected: true },
];
const calls = [];
let usageAvailable = true, usageDelay = 0, usageMutationDelay = 0, usageRefreshFailure = false, usageStatusFailure = false, usageConnectFailureOnce = false;
const usageRecords = new Map();
const usageStatus = id => structuredClone(usageRecords.get(id) || {profile_id:id,available:usageAvailable,state:usageAvailable?'disconnected':'unavailable',connection_id:'',identity_match:'unverified'});

let conversations = [
  { id: 'one', title: 'A quieter week', updated_at: Math.floor(Date.now() / 1000) },
  { id: 'two', title: 'Dinner ideas', updated_at: Math.floor(Date.now() / 1000) - 86400 },
  { id: 'unsafe', title: '<img src=x onerror=alert(1)>', updated_at: 1 },
];
const history = new Map([
  ['one', [{ role: 'user', content: 'Plan a quiet week.' }, { role: 'assistant', content: 'Leave two evenings open.', run_id:'history-run' }]],
  ['two', [{ role: 'user', content: 'Suggest dinner.' }, { role: 'assistant', content: 'Try rice and roasted vegetables.' }]],
  ['unsafe', [{ role: 'assistant', content: '<script>window.unexpectedExecution=true</script>' }]],
]);
const server = createServer(async (req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const call = { path: url.pathname, method: req.method, query: url.search };
  calls.push(call);
  const json = (status, body) => { call.status = status; res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
  if (url.pathname === '/') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(html); return; }
  if (url.pathname === '/pwa-updates.js') { res.writeHead(200, { 'Content-Type': 'text/javascript' }); res.end(pwaScript); return; }
  // This interaction fixture deliberately has no installed worker; real deployment
  // and offline behavior are exercised by pwa-browser.mjs, not emulated here.
  if (url.pathname === '/sw.js') { res.writeHead(404); res.end(); return; }
  if (url.pathname === '/manifest.webmanifest') { json(200, {name:'Agent',start_url:'/',display:'standalone'}); return; }
  if (url.pathname === '/version.json') { json(200, {version:'42',buildId:'ui-fixture'}); return; }
  if (url.pathname === '/favicon.ico') { res.writeHead(204); res.end(); return; }
  if (['/api/auth/login', '/api/auth/register'].includes(url.pathname)) {
    // Drain but never record credentials, even though test values are synthetic.
    for await (const _ of req) {}
    if (authFailure) { json(401, { error: 'Test sign-in rejected. Please try again.' }); return; }
    authenticated = true; json(200, { id: 'demo-user', username: 'Browser Test' }); return;
  }
  if (url.pathname === '/api/auth/me') { json(authenticated ? 200 : 401, authenticated ? { id: 'demo-user', username: 'Browser Test' } : { error: 'Sign in to continue.' }); return; }
  if (!authenticated) { json(401, { error: 'Sign in to continue.' }); return; }
  if (url.pathname === '/api/research/settings') {
    json(200, {settings:{configured:false,provider:'Brave Search',key_suffix:'',revision:'',verified_at:'',last_checked_at:'',status:'not_configured'}}); return;
  }
  if (url.pathname === '/api/auth/password') {
    for await (const _ of req) {}
    json(passwordFailure ? 401 : 200, passwordFailure ? { error: 'Current password is incorrect.' } : { password_changed: true }); return;
  }
  if (url.pathname === '/api/auth/logout') {
    if (logoutFailure) { json(500, { error: 'Test sign-out failure.' }); return; }
    authenticated = false; json(200, {}); return;
  }
  if (url.pathname === '/auth/start') {
    let text = ''; for await (const chunk of req) text += chunk;
    const payload = text ? JSON.parse(text) : {};
    if (req.method !== 'POST') { json(405, { error: 'Use POST.' }); return; }
    call.payload = { keys: Object.keys(payload), expected_active_profile_id: payload.expected_active_profile_id };
    if ('password' in payload) { json(400, { error: 'Authorization must not submit an Agent password.' }); return; }
    await new Promise(resolve => setTimeout(resolve, authorizationDelay));
    if (authorizationFailure) { json(503, { error: 'Test authorization start unavailable.' }); return; }
    json(200, { authorization_url: origin + '/fixture/authorize' + url.search }); return;
  }
  if (url.pathname === '/fixture/authorize') {
    // Emulate only the existing route handoff, never contact the real provider.
    let target = providerProfiles.find(p => p.id === (url.searchParams.get('profile_id') || activeProfile));
    if (url.searchParams.get('mode') === 'add') { target = { id: 'p3', label: 'Added', email: 'added@example.com' }; providerProfiles.push(target); }
    if (target) { Object.assign(target, { connected: true, oauth_selected: true, plan_usage_enabled: true, registration_pending: false }); activeProfile = target.id; }
    res.writeHead(302, { Location: '/' }); res.end(); return;
  }
  if (url.pathname === '/api/auth/status') {
    await new Promise(resolve => setTimeout(resolve, connectionDelay));
    const selected = providerProfiles.find(p => p.id === activeProfile);
    json(connectionStatusFailure ? 503 : 200, connectionStatusFailure ? { error: 'Fixture status unavailable' } : { connected: selected?.connected || false, oauth_selected: selected?.oauth_selected || false, plan_usage_enabled: selected?.plan_usage_enabled || false, local_url: configuredLocalURL }); return;
  }
  if (url.pathname === '/api/auth/profiles') { json(profilesFailure ? 503 : 200, profilesFailure ? { error: 'Fixture profiles unavailable' } : { active_profile_id: activeProfile, profiles: providerProfiles }); return; }
  if (url.pathname === '/api/auth/profiles/active') {
    let text = ''; for await (const chunk of req) text += chunk;
    const payload = JSON.parse(text);
    if (payload.password !== 'synthetic-test-only') { json(401, { error: 'Agent password is incorrect.' }); return; }
    if (profileFailure) { json(500, { error: 'Test profile change failed.' }); return; }
    activeProfile = payload.profile_id;
    json(200, {}); return;
  }
  if (url.pathname === '/api/auth/disconnect') {
    let text = ''; for await (const chunk of req) text += chunk;
    const payload = text ? JSON.parse(text) : {};
    call.payload = { profile_id: payload.profile_id };
    if (payload.password !== 'synthetic-test-only') { json(401, { error: 'Agent password is incorrect.' }); return; }
    if (call.payload.profile_id !== activeProfile) { json(409, { error: 'Active account changed.' }); return; }
    if (!disconnectFailure) { const selected = providerProfiles.find(p => p.id === activeProfile); if (selected) Object.assign(selected, { connected: false, oauth_selected: false, plan_usage_enabled: false }); }
    json(disconnectFailure ? 500 : 200, disconnectFailure ? { error: 'Test disconnect failure.' } : { remote_revocation_confirmed: remoteRevoked }); return;
  }
  if (url.pathname.startsWith('/api/usage/')) {
    const action = url.pathname.split('/').pop();
    let payload = {};
    if (req.method === 'POST') { let text=''; for await (const chunk of req) text+=chunk; payload=JSON.parse(text); }
    const id = payload.profile_id || url.searchParams.get('profile_id');
    call.payload = {...payload}; delete call.payload.password; call.payloadKeys=Object.keys(payload);
    if (action !== 'status') await new Promise(r=>setTimeout(r,usageMutationDelay));
    if (action === 'status' && usageStatusFailure) { json(503,{error:'Fixture usage status unavailable.'}); return; }
    if (action === 'status') { const result=usageStatus(id); await new Promise(r=>setTimeout(r,usageDelay)); json(200,result); return; }
    if (id !== activeProfile) { json(409,{error:'Active account changed.'}); return; }
    const record=usageStatus(id);
    if ((payload.expected_connection_id ?? payload.connection_id) !== record.connection_id) { json(409,{error:'Usage connection changed.'}); return; }
    if (action === 'connect') Object.assign(record,{state:'pending',connection_id:'u-'+id,login:{login_id:'login-'+id,verification_url:'https://auth.openai.com/codex/device',user_code:'TEST-CODE',expires_at:Math.floor(Date.now()/1000)+600}});
    if (action === 'connect' && usageConnectFailureOnce) { usageConnectFailureOnce=false; Object.assign(record,{state:'expired',connection_id:'recovered-'+id}); delete record.login; usageRecords.set(id,record); json(503,{error:'Fixture device start failed after saving connection.'}); return; }
    if (action === 'cancel') { assert.equal(payload.login_id,record.login.login_id); Object.assign(record,{state:'disconnected',connection_id:''}); delete record.login; }
    if (action === 'refresh' && !usageRefreshFailure) record.snapshot = {fetched_at:Math.floor(Date.now()/1000),stale:false,windows:[{id:'custom',label:'Custom quota window',used_percent:32,window_minutes:180,reset_at:1900000000}]};
    if (action === 'refresh' && usageRefreshFailure) { Object.assign(record,{error:'Fixture usage refresh failed.',snapshot:{...record.snapshot,stale:true}}); usageRecords.set(id,record); json(200,record); return; }
    if (action === 'refresh') delete record.error;
    if (action === 'disconnect') {
      if (payload.password !== 'synthetic-test-only') { json(401,{error:'Agent password is incorrect.'}); return; }
      Object.assign(record,{state:'disconnected',connection_id:'',error:'Removed here; remote revocation is not guaranteed.'}); delete record.snapshot; delete record.identity;
    }
    usageRecords.set(id,record); json(200,record); return;
  }
  if (url.pathname === '/api/models') {
    await new Promise(resolve => setTimeout(resolve, modelsDelay));
    json(modelsFailure ? 503 : 200, modelsFailure ? { error: 'Fixture models unavailable' } : { models: [{ id: 'test-one', name: 'Test model one' }, { id: 'test-two', name: 'Test model two' }] }); return;
  }
  if (url.pathname.startsWith('/api/runs/')) {
    const run = runs.get(decodeURIComponent(url.pathname.split('/').pop()));
    json(runReadFailure ? 503 : run ? 200 : 404, runReadFailure ? {error:'Trace read failed.'} : run ? {run} : {error:'Run not found.'}); return;
  }
  if (url.pathname === '/api/conversations') {
    json(listFailure ? 500 : 200, listFailure ? { error: 'Test history unavailable.' } : { conversations }); return;
  }
  if (url.pathname.startsWith('/api/conversations/')) {
    const snapshotFailure = loadFailure;
    await new Promise(resolve => setTimeout(resolve, loadDelay));
    json(snapshotFailure ? 500 : 200, snapshotFailure ? { error: 'Test load failure.' } : { messages: history.get(decodeURIComponent(url.pathname.split('/').pop())) || [] }); return;
  }
  if (url.pathname === '/api/chat') {
    let text = ''; for await (const chunk of req) text += chunk;
    const payload = JSON.parse(text);
    call.payload = payload;
    if (payload.run_id) runs.set(payload.run_id, fixtureRun(payload.run_id, 'running', payload.messages));
    await new Promise(resolve => setTimeout(resolve, chatDelay));
    if (payload.run_id) runs.set(payload.run_id, fixtureRun(payload.run_id, chatFailure ? 'failed' : 'completed', payload.messages));
    if (chatFailure) { json(429, { error: 'Test plan allowance reached.', action: chatErrorAction, run_id:payload.run_id }); return; }
    const id = payload.conversation_id || 'created';
    const message = payload.messages.at(-1);
    const reply = 'A real UI response from the isolated test fixture.\n\n## A formatted reply\n\nKeep **two evenings** open.';
    history.set(id, [...(history.get(id) || []), message, { role: 'assistant', content: reply, run_id:payload.run_id }]);
    if (!conversations.some(item => item.id === id)) conversations.unshift({ id, title: message.content, updated_at: Math.floor(Date.now() / 1000) });
    json(200, { reply, conversation_id: id, run_id:payload.run_id }); return;
  }
  json(404, { error: 'Unexpected test endpoint: ' + url.pathname });
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = 'http://127.0.0.1:' + server.address().port;
const child = spawnTestBrowser(browserPath, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--remote-debugging-port=0', '--user-data-dir=' + profile, 'about:blank'], { stdio: 'ignore' });
let socket;
let nextId = 0;
const pending = new Map();
const runtimeErrors = [];
async function until(fn, message, timeout = 10000) {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    const result = await fn();
    if (result) return result;
    await new Promise(resolve => setTimeout(resolve, 40));
  }
  throw new Error(message);
}
function cdp(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++nextId;
    const timer = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); }, 10000);
    pending.set(id, { resolve, reject, timer });
    socket.send(JSON.stringify({ id, method, params }));
  });
}
async function js(expression) {
  const result = await cdp('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
  return result.result.value;
}
const click = selector => js(`document.querySelector(${JSON.stringify(selector)}).click()`);
const visible = selector => js(`(() => { const e=document.querySelector(${JSON.stringify(selector)}); return !!e && e.getClientRects().length>0; })()`);
const session = title => js(`Array.from(document.querySelectorAll('#conversation-list button')).find(b=>b.textContent.includes(${JSON.stringify(title)}))?.click()`);
const expectEventually = (expression, message) => until(() => js(`(() => { try { return (${expression}); } catch { return false; } })()`), message);
async function confirmChange(password = 'synthetic-test-only') {
  await expectEventually("document.querySelector('#disconnect-dialog').open && !document.querySelector('#confirm-disconnect').disabled", 'Password confirmation did not open');
  await js(`document.querySelector('#connection-password').value=${JSON.stringify(password)}`);
  await click('#confirm-disconnect');
}
async function changeAccount(id) {
  const pendingRegistration = await js(`document.querySelector('#account-select option[value="${id}"]')?.dataset.registrationPending==='true'`);
  await js(`document.querySelector('#account-select').value=${JSON.stringify(id)};document.querySelector('#account-select').dispatchEvent(new Event('change'))`);
  if (!pendingRegistration) await confirmChange();
}
async function viewport(width, height) {
  await cdp('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: width < 600 });
  // Let responsive media-query handlers settle before clicking mobile controls.
  await js('new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))');
}
async function screenshot(name) {
  if (!process.env.UI_SCREENSHOTS) return;
  await js('new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))');
  const { data } = await cdp('Page.captureScreenshot', { format: 'png' });
  await writeFile(join(process.env.UI_SCREENSHOTS, name + '.png'), Buffer.from(data, 'base64'));
}
async function drag(selector, dx, dy) {
  const { x, y } = await js(`(() => { const r=document.querySelector(${JSON.stringify(selector)}).getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2}; })()`);
  await cdp('Input.dispatchMouseEvent', { type: 'mouseMoved', x, y });
  await cdp('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', buttons: 1, clickCount: 1 });
  await cdp('Input.dispatchMouseEvent', { type: 'mouseMoved', x: x+dx, y: y+dy, buttons: 1 });
  await cdp('Input.dispatchMouseEvent', { type: 'mouseReleased', x: x+dx, y: y+dy, button: 'left', clickCount: 1 });
}
function ok(condition, message) { assert.ok(condition, message); console.log('PASS ' + message); }
try {
  const portText = await waitForTestBrowser(child, profile);
  const port = portText.split('\n')[0];
  const tabs = await (await fetch('http://127.0.0.1:' + port + '/json/list')).json();
  socket = new WebSocket(tabs.find(tab => tab.type === 'page').webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
  socket.onmessage = event => {
    const data = JSON.parse(event.data);
    if (data.method === 'Runtime.exceptionThrown') runtimeErrors.push(data.params.exceptionDetails.exception?.description || data.params.exceptionDetails.text);
    if (!data.id) return;
    const job = pending.get(data.id); if (!job) return;
    clearTimeout(job.timer); pending.delete(data.id);
    if (data.error) job.reject(new Error(data.error.message)); else job.resolve(data.result);
  };
  await cdp('Page.enable'); await cdp('Runtime.enable');
  await viewport(1280, 850);
  await cdp('Page.navigate', { url: origin });
  await until(() => visible('#auth-gate'), 'Login missing');
  ok(await js("document.title==='Agent' && document.querySelector('.auth-header .brand').textContent==='e1Agent' && document.querySelector('.auth-header .mark').textContent==='e1'"), 'Login uses the Agent name and e1 mark');
  ok(await js("!document.body.textContent.includes('EveryoneAgent') && document.querySelector('#account-mode-label').textContent==='New to Agent?'"), 'Login copy uses the new display name');
  ok(await js(`['Your personal conversation space', 'Connect your ChatGPT account after signing in.', 'Agent · Your own space'].every(text => !document.querySelector('#auth-gate').textContent.includes(text))`), 'Login omits supplementary header and footer copy');
  ok(await visible('#toggle-password'), 'Quiet entry has password visibility control');
  ok(await js("[document.body, document.documentElement].some(e=>getComputedStyle(e).backgroundColor === 'rgb(32, 35, 34)')"), 'Focus charcoal theme applied');
  await screenshot('agent-login-desktop');
  await click('#toggle-account-mode');
  ok(await js("document.querySelector('#password-input').autocomplete==='new-password' && !document.querySelector('#password-hint').hidden"), 'Account creation preserves password requirements');
  ok(await js("document.querySelector('#auth-intro').textContent==='Create your personal Agent account.'"), 'Create-account mode uses Agent branding');
  await click('#toggle-account-mode'); await click('#toggle-password');
  ok(await js("document.querySelector('#password-input').type==='text'"), 'Password visibility toggles');
  await click('#toggle-password');
  // Generated dummy details belong only to this browser fixture.
  await js("document.querySelector('#username-input').value='ui-test';document.querySelector('#password-input').value='synthetic-test-only';document.querySelector('#account-form').requestSubmit()");
  await expectEventually("document.querySelector('#auth-error').textContent.includes('Test sign-in rejected')", 'Login error was not shown');
  ok(await visible('#auth-gate'), 'Rejected sign-in stays in Quiet entry and shows real API error');
  authFailure = false;
  await click('#toggle-account-mode');
  await js("document.querySelector('#username-input').value='ui-test';document.querySelector('#password-input').value='synthetic-test-only';document.querySelector('#account-form').requestSubmit()");
  await expectEventually("!document.querySelector('#chat-app').hidden && document.querySelectorAll('#conversation-list button').length>=3", 'Signed-in history missing');
  ok(calls.some(call => call.path === '/api/auth/register'), 'Create-account form uses the existing registration API');
  ok(await js("document.querySelector('.sidebar .brand').textContent==='e1Agent' && document.querySelector('#message-input').placeholder==='Message Agent'"), 'Chat sidebar and composer use the approved rebrand');
  await session('A quieter week');
  await expectEventually("document.querySelector('.message.assistant .message-text')?.textContent==='Leave two evenings open.'", 'Conversation layout fixture missing');
  ok(await visible('.inspect-run'), 'Saved assistant messages expose their real run inspection action');
  ok(!(await visible('#run-inspector')), 'Inspector starts closed without taking chat space');
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-inspector')?.textContent.includes('Context prepared')", 'Inspector timeline missing');
  ok(await js("document.querySelector('#run-state').textContent==='Completed'"), 'Inspector shows server-recorded state');
  await click('#run-tab-context');
  ok(await js("document.querySelector('#run-view-context').textContent.includes('2026-11-11') && document.querySelector('#run-view-context').textContent.includes('America/Los_Angeles') && document.querySelector('#run-view-context').textContent.includes('Missing knowledge')"), 'Context tab exposes server date, timezone, and baseline instructions');
  await click('#run-tab-model');
  await click('#run-request summary');
  await click('#run-response summary');
  ok(await js("document.querySelector('#run-request pre').textContent.includes('Plan a quiet week.') && document.querySelector('#run-response pre').textContent.includes('<script>') && !document.querySelector('#run-inspector script, #run-inspector img') && !window.traceExecuted"), 'Request and response disclosures preserve text without interpreting HTML');
  ok(await js("document.querySelector('#run-view-model').textContent.includes('Not reported') && document.querySelector('#run-view-model').textContent.includes('Not available')"), 'Missing token usage and cost are explicit rather than fabricated');
  await click('#run-close');
  runs.set('history-run',{...fixtureRun('history-run'), request_truncated:true, context:{...fixtureRun('history-run').context,omitted_messages:8,capture_omitted_messages:2,truncated:true}});
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-state').textContent==='Completed'", 'Truncated trace missing');
  await click('#run-tab-context');
  ok(await visible('#run-truncation') && await js("document.querySelector('#run-context-values').textContent.includes('Excluded before inference8') && document.querySelector('#run-context-values').textContent.includes('Excluded from capture2')"), 'Context warns about incomplete capture and distinguishes inference omissions from trace-only omissions');
  await click('#run-close');
  runs.set('history-run',fixtureRun('history-run'));
  runs.set('history-run',fixtureRun('history-run','running'));
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-state').textContent==='Running'", 'Running focus fixture missing');
  await viewport(390,844);
  await js("document.querySelector('#run-tab-timeline').focus()");
  for (let i=0;i<2;i++) {
    await cdp('Input.dispatchKeyEvent',{type:'keyDown',key:'Tab',code:'Tab',windowsVirtualKeyCode:9});
    await cdp('Input.dispatchKeyEvent',{type:'keyUp',key:'Tab',code:'Tab',windowsVirtualKeyCode:9});
  }
  ok(await js("document.activeElement.matches('#run-events button')"), 'Timeline actions can be reached with real keyboard navigation');
  runs.get('history-run').events[0].detail = 'Updated by the next server poll.';
  await expectEventually("document.querySelector('#run-events').textContent.includes('Updated by the next server poll.')", 'Running trace did not refresh');
  ok(await js("document.activeElement.matches('#run-events button') && document.querySelector('#run-inspector').contains(document.activeElement)"), 'Polling preserves keyboard focus inside the inspector');
  await click('#run-close');
  runs.set('history-run',fixtureRun('history-run'));
  await viewport(1280,850);
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-state').textContent==='Completed'", 'Trace restore missing');
  await click('#run-tab-model');
  for (const [width,height] of [[1440,900],[1024,600],[760,600],[390,844],[320,640]]) {
    await viewport(width,height);
    ok(await js("(() => {const p=document.querySelector('#run-inspector'),r=p.getBoundingClientRect();return document.documentElement.scrollWidth<=innerWidth && document.documentElement.scrollHeight<=innerHeight && p.scrollWidth<=p.clientWidth && r.right<=innerWidth && r.bottom<=innerHeight && document.querySelector('#run-close').getBoundingClientRect().top>=0;})()"), `Inspector stays bounded at ${width}x${height}`);
    if (width<701) ok(await js("document.querySelector('#run-close').getBoundingClientRect().height>=44 && document.querySelector('#run-tab-model').getBoundingClientRect().height>=44"), 'Phone inspector has accessible touch targets');
    await screenshot('agent-run-inspector-'+width);
  }
  await viewport(760,640);
  ok(await js("document.querySelector('#run-inspector').getAttribute('aria-modal')==='true' && document.querySelector('#session-sidebar').inert && document.querySelector('#chat-thread').inert"), 'Resizing an open inspector from phone to tablet keeps the background inert');
  await viewport(390,844);
  await cdp('Input.dispatchKeyEvent',{type:'keyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
  await cdp('Input.dispatchKeyEvent',{type:'keyUp',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
  ok(!(await visible('#run-inspector')) && await js("document.activeElement.matches('.inspect-run')"), 'Escape closes inspection and restores focus to its run');
  await viewport(1280,850);
  runReadFailure = true;
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-feedback').textContent.includes('could not')", 'Trace read failure not shown');
  runReadFailure = false;
  await click('#run-retry');
  await expectEventually("document.querySelector('#run-state').textContent==='Completed'", 'Trace retry failed');
  await click('#run-close');
  runs.delete('history-run');
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-feedback').textContent.includes('no longer available')", 'Expired trace state missing');
  await click('#run-close');
  runs.set('history-run',fixtureRun('history-run'));
  ok(await visible('#conversation-save-status') && !(await visible('#notice')), 'Saved status moves into the header instead of repeating above the composer');
  ok(await js("document.querySelector('#message-input').getBoundingClientRect().height===42"), 'Composer starts compact while retaining its resize control');
  for (const [width, height] of [[1440,900],[1280,850],[390,844],[320,640]]) {
    await viewport(width,height);
    ok(await js(`(() => {
      const user=document.querySelector('.message.user'), answer=document.querySelector('.message.assistant');
      const text=user.querySelector('.message-text'), box=text.getBoundingClientRect(), thread=answer.getBoundingClientRect();
      const style=getComputedStyle(text);
      return style.backgroundColor==='rgb(44, 57, 48)' && style.borderTopLeftRadius==='12px'
        && getComputedStyle(answer).backgroundColor==='rgba(0, 0, 0, 0)'
        && Math.abs(box.right-thread.right)<2 && box.width<thread.width*.9
        && thread.width<=740 && document.documentElement.scrollWidth<=innerWidth;
    })()`), `Soft green prompts align right while answers stay flat at ${width}px`);
    if (width < 700) await click('#sidebar-toggle');
    ok(await js(`(() => {
      const title=document.querySelector('.session-title'), button=title.closest('button');
      const saved=document.querySelector('#conversation-save-status').getBoundingClientRect();
      const original=title.textContent;
      title.textContent=original.repeat(30);
      const fits=title.getBoundingClientRect().height<=parseFloat(getComputedStyle(title).lineHeight)*2+1;
      title.textContent=original;
      return fits && title.getBoundingClientRect().height>0 && button.title===original && saved.right<=innerWidth;
    })()`), `Long session titles stay within two lines and Saved fits the header at ${width}px`);
    if (width < 700) await click('#sidebar-backdrop');
  }
  await viewport(1280,850);
  const plainHistory = history.get('one');
  const formattedAnswer = 'I’d keep the quiet palette and give the conversation a clearer rhythm.\n\n## Start with the reading experience\n\n- Keep **lines comfortably short**.\n- Render *formatting* and `inline code`.\n\n### Quiet the surrounding interface\n\n1. Use a short title.\n2. Keep the composer compact.\n\n> Prompts and answers should feel distinct.\n\n```js\nconst literal = "<img src=x onerror=alert(1)>";\n' + 'x'.repeat(500) + '\n```\n\n[Safe link](https://example.com/) [Unsafe link](javascript:alert) [Data link](data:text/html,evil)\n\n<script>window.unexpectedExecution=true</script>\n<img src=x onerror="window.unexpectedExecution=true">';
  history.set('one', [{role:'user',content:'Please keep **my wording** literal.\n' + 'longword'.repeat(70)}, {role:'assistant',content:formattedAnswer}]);
  await session('A quieter week');
  await expectEventually("!!document.querySelector('.message.assistant h2')", 'Assistant Markdown was not rendered');
  ok(await js(`(() => {
    const answer=document.querySelector('.message.assistant .message-text');
    return answer.querySelector('h2').textContent==='Start with the reading experience'
      && answer.querySelector('strong').textContent==='lines comfortably short'
      && answer.querySelector('em').textContent==='formatting'
      && answer.querySelectorAll('ul li').length===2 && answer.querySelectorAll('ol li').length===2
      && answer.querySelector('blockquote') && answer.querySelector('pre code').textContent.includes('<img src=x')
      && answer.querySelectorAll('a').length===1 && answer.querySelector('a').href==='https://example.com/'
      && answer.querySelector('a').rel==='noopener noreferrer' && answer.querySelector('a').target==='_blank'
      && !answer.querySelector('script,img,iframe,svg') && !window.unexpectedExecution
      && document.querySelector('.message.user .message-text').textContent.includes('**my wording**')
      && !document.querySelector('.message.user strong');
  })()`), 'Assistant formatting is semantic, user text stays literal, and unsafe HTML/URLs stay inert');
  for (const [width,height] of [[1280,900],[390,844],[320,640]]) {
    await viewport(width,height);
    ok(await js("document.documentElement.scrollWidth<=innerWidth && document.querySelector('#messages').scrollWidth<=document.querySelector('#messages').clientWidth && document.querySelector('.message.user').getBoundingClientRect().left>=0"), `Long prompts and code blocks do not overflow the thread at ${width}px`);
    await js("document.querySelector('#messages').scrollTop=0");
    await screenshot('agent-soft-bubbles-stress-'+width);
  }
  history.set('one', [{role:'user',content:'How could we make this conversation easier to read?'}, {role:'assistant',content:'I’d keep the quiet, dark palette and give the conversation a clearer rhythm. The biggest improvement is **making the answer—not the interface—the focus.**\n\n## Start with the reading experience\n\n- **Keep lines comfortably short.** Use a focused reading column.\n- **Render the formatting.** Headings, bold text, and lists should be easy to scan.\n- **Separate the speakers.** Prompts get a subtle green bubble; answers stay open.\n\n## Quiet the surrounding interface\n\nUse short conversation titles, move the saved status into the header, and keep the composer compact until more space is needed.'}]);
  await viewport(1280,900);
  await session('A quieter week');
  await expectEventually("document.querySelector('.message.assistant strong')?.textContent==='making the answer—not the interface—the focus.'", 'Visual sample missing');
  await js("document.querySelector('#messages').scrollTop=0");
  await screenshot('agent-soft-bubbles-desktop');
  await viewport(390,844);
  await js("document.querySelector('#messages').scrollTop=0");
  await screenshot('agent-soft-bubbles-phone');
  history.set('one', plainHistory);
  await viewport(1280,850);
  await session('A quieter week');
  await expectEventually("document.querySelector('.message.assistant .message-text')?.textContent==='Leave two evenings open.'", 'Plain conversation restore failed');
  await click('#message-input');
  await cdp('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
  await cdp('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
  await js("document.querySelector('#message-input').focus()");
  ok(await js("document.activeElement.id==='message-input' && getComputedStyle(document.activeElement).outlineStyle==='none' && getComputedStyle(document.activeElement).boxShadow==='none'"), 'Focused message textbox has no decorative outline or shadow');
  await cdp('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
  await cdp('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 });
  ok(await js("document.activeElement.matches('button, select') && getComputedStyle(document.activeElement).outlineStyle==='solid'"), 'Other composer controls retain their keyboard focus indicator');
  await js('document.activeElement.blur()');
  ok(await js("document.querySelector('#current-username').textContent==='Browser Test'"), 'Bottom-left profile shows authenticated user');
  ok(await visible('#app-version') && await js("document.querySelector('#app-version').textContent==='Version 42' && document.querySelector('#app-version').getBoundingClientRect().top >= document.querySelector('.account-row').getBoundingClientRect().bottom"), 'Build version is visible underneath the logged-in user');
  ok(!calls.some(c=>c.path.startsWith('/api/usage/')), 'Usage is not loaded during login or ordinary chat');
  await click('.inspect-run');
  await click('#settings-button');
  ok(!(await visible('#run-inspector')), 'Settings closes inspection and stops trace activity');
  await click('#back-to-chat');
  ok(await visible('#settings-button'), 'Gear button is available beside the profile');
  ok(await visible('#sidebar-resize'), 'Desktop sidebar has a resize handle');
  await drag('#sidebar-resize', 100, 0);
  ok(await js("Math.abs(document.querySelector('#session-sidebar').getBoundingClientRect().width-332)<2"), 'Dragging the sidebar edge makes it wider');
  await drag('#sidebar-resize', -200, 0);
  ok(await js("document.querySelector('#session-sidebar').getBoundingClientRect().width===180"), 'Sidebar shrinking stops at a usable minimum');
  await js("document.querySelector('#sidebar-resize').dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}))");
  ok(await js("document.querySelector('#session-sidebar').getBoundingClientRect().width===196"), 'Sidebar supports keyboard resizing');
  await drag('#sidebar-resize', 36, 0);
  await drag('#sidebar-resize', 500, 0);
  ok(await js("document.querySelector('#session-sidebar').getBoundingClientRect().width===480"), 'Sidebar growth leaves room for the conversation');
  await drag('#sidebar-resize', -248, 0);
  ok(await visible('#composer-resize'), 'Composer has a custom resize handle');
  assert.deepEqual(await js(`(() => {
    const handle = document.querySelector('#composer-resize'), icon = handle.querySelector('svg');
    const bounds = handle.getBoundingClientRect(), graphic = icon.getBoundingClientRect();
    return { cursor: getComputedStyle(handle).cursor, width: bounds.width, height: bounds.height,
      iconWidth: graphic.width, iconHeight: graphic.height, color: getComputedStyle(handle).color,
      strokeWidth: icon.getAttribute('stroke-width'), path: icon.querySelector('path').getAttribute('d') };
  })()`), { cursor: 'nesw-resize', width: 44, height: 44, iconWidth: 13, iconHeight: 13,
    color: 'rgb(127, 139, 130)', strokeWidth: '1.25', path: 'M3.5 3.5l9 9M7.5 3.5l5 5M11.5 3.5l1 1' },
  'Composer uses the approved small corner texture with a diagonal cursor and generous grab target');
  ok(await js(`(() => { const r=document.querySelector('#composer-resize').getBoundingClientRect(), t=document.querySelector('#message-input').getBoundingClientRect(); return r.top<=t.top+8 && r.right>=t.right-8 && getComputedStyle(document.querySelector('#message-input')).resize==='none'; })()`), 'Resize grip is at the top-right and replaces the native bottom-right grip');
  const composerHeight = await js("document.querySelector('#message-input').getBoundingClientRect().height");
  await js("document.querySelector('#message-input').value='Keep this draft'");
  await drag('#composer-resize', 0, -100);
  ok(await js("document.querySelector('#message-input').getBoundingClientRect().height") > composerHeight+90, 'Dragging the top-right grip upward expands the text box');
  await drag('#composer-resize', 0, 100);
  ok(await js("document.querySelector('#message-input').getBoundingClientRect().height") === composerHeight, 'Dragging the grip downward shrinks the text box');
  ok(await js("document.querySelector('#message-input').value==='Keep this draft'"), 'Resizing preserves the message draft');
  await js("document.querySelector('#composer-resize').dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowUp',bubbles:true}))");
  ok(await js("document.querySelector('#message-input').getBoundingClientRect().height") === composerHeight+16, 'Message box supports keyboard resizing');
  await screenshot('agent-resize-controls-desktop');
  await js("document.querySelector('#composer-resize').dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowDown',bubbles:true}))");
  await js("document.querySelector('#message-input').value=''");
  ok(await js("document.querySelector('#settings-button').getBoundingClientRect().left >= document.querySelector('#account-menu-toggle').getBoundingClientRect().right"), 'Gear sits to the right of the logged-in user');
  await click('#settings-button');
  ok(await visible('#settings-panel') && !(await visible('#messages')) && !(await visible('#chat-form')), 'Settings replaces the main conversation panel');
  ok(await js("location.hash==='#settings'"), 'Opening Settings updates the browser URL');
  await js('history.back()');
  await expectEventually("location.hash==='' && document.querySelector('#settings-panel').hidden", 'Browser Back did not restore chat');
  await js('history.forward()');
  await expectEventually("location.hash==='#settings' && !document.querySelector('#settings-panel').hidden", 'Browser Forward did not restore Settings');
  await cdp('Page.reload');
  await expectEventually("!document.querySelector('#chat-app')?.hidden && document.querySelector('#settings-button')?.getAttribute('aria-pressed')==='true'", 'Settings URL did not survive reload');
  ok(await visible('#logout-button') && !(await js("!!document.querySelector('#account-menu #logout-button')")), 'Sign out lives only in Settings');
  ok(await js("!!document.querySelector('#settings-panel #account-select') && !!document.querySelector('#settings-panel #connect-chatgpt') && !!document.querySelector('#settings-panel #disconnect-chatgpt')"), 'ChatGPT connection management lives in Settings');
  await expectEventually("document.querySelector('#connection-badge')?.textContent==='Connected'", 'Connected subscription badge missing');
  ok(await js("document.querySelector('#connection-title').textContent==='ChatGPT connection' && !document.querySelector('.settings-card').textContent.includes('This connection is separate') && !document.querySelector('#connection-note')"), 'Option A keeps one concise connection explanation without repetitive notes');
  ok(await js("document.querySelector('.connection-heading #check-chatgpt')?.tagName==='BUTTON' && !!document.querySelector('#check-chatgpt #connection-badge') && !!document.querySelector('#check-chatgpt svg') && !document.querySelector('.provider-actions #check-chatgpt')"), 'Refresh is part of the top-right status pill, not a separate action');
  ok(await js("document.querySelector('#check-chatgpt').getAttribute('aria-label').includes('Refresh') && document.querySelector('#check-chatgpt').title.includes('Refresh')"), 'Status pill announces its refresh action');
  ok(await js("document.querySelector('#connection-status').hidden"), 'Ready local connection does not repeat the selected account or Connected badge');
  ok(await js("document.querySelector('#local-connection-address').hidden"), 'Localhost does not show remote setup address instructions');
  await expectEventually("document.querySelector('#usage-panel') && !document.querySelector('#usage-panel').hidden && !document.querySelector('#usage-connect').disabled", 'Integrated allowance Connect is missing');
  usageMutationDelay=200;
  const usageStartsBefore=calls.filter(c=>c.path==='/api/usage/connect').length;
  await click('#usage-connect'); await click('#usage-connect');
  await expectEventually("document.querySelector('#usage-code').textContent==='TEST-CODE' && !document.querySelector('#usage-cancel').disabled", 'Usage device-code start missing');
  usageMutationDelay=0;
  ok(calls.filter(c=>c.path==='/api/usage/connect').length===usageStartsBefore+1 && !calls.findLast(c=>c.path==='/api/usage/connect').payloadKeys.includes('password'), 'Usage start suppresses duplicates and never submits an Agent password');
  ok(await js("!document.querySelector('#disconnect-dialog').open && document.querySelector('#usage-verification').href==='https://auth.openai.com/codex/device'"), 'Usage starts device-code authorization without an Agent password');
  await click('#usage-cancel');
  await expectEventually("!document.querySelector('#usage-connect').hidden && !document.querySelector('#usage-connect').disabled", 'Usage cancel did not restore Connect');
  await click('#usage-connect');
  await expectEventually("document.querySelector('#usage-code').textContent==='TEST-CODE' && !document.querySelector('#usage-cancel').disabled", 'Second usage start failed');
  const approved = usageRecords.get('p1');
  approved.state='checking';
  await expectEventually("document.querySelector('#usage-badge').textContent==='Checking…' && !document.querySelector('#usage-refresh').getAttribute('aria-busy').includes('true')", 'Usage approval checking phase missing');
  Object.assign(approved,{state:'connected',identity:{email:'personal@example.com',plan_type:'Fixture plan'}}); delete approved.login;
  await expectEventually("document.querySelector('#usage-windows [role=meter]')?.getAttribute('aria-valuenow')==='68'", 'Approval polling did not fetch initial allowance');
  ok(calls.some(c=>c.path==='/api/usage/refresh'), 'Approval explicitly retrieves initial allowance instead of fabricating quota');
  ok(await js("document.querySelector('#usage-panel').closest('.settings-card').querySelector('#connection-title') && !document.querySelector('#usage-panel .connection-heading') && document.querySelector('#usage-updated').textContent.includes('local time')"), 'Allowance is integrated without altering primary heading selectors');
  await js("document.querySelector('#settings-panel').scrollTop=0");
  await screenshot('agent-allowance-integrated-desktop');
  await js("Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async value=>{window.copiedUsageCode=value;}}})");
  for (const [width,height] of [[1280,850],[390,640],[320,640]]) {
    await viewport(width,height);
    ok(await js("(() => { const a=document.querySelector('#disconnect-chatgpt'), b=document.querySelector('#usage-disconnect'); const props=['color','backgroundColor','borderColor','borderWidth','borderRadius','fontSize','fontWeight','padding','minHeight']; return props.every(p=>getComputedStyle(a)[p]===getComputedStyle(b)[p]) && a.getBoundingClientRect().height===b.getBoundingClientRect().height && b.getBoundingClientRect().height>=44 && document.querySelector('#settings-panel').scrollWidth<=document.querySelector('#settings-panel').clientWidth && document.documentElement.scrollWidth<=innerWidth; })()"), `Usage Disconnect matches the conversation control and fits ${width}px`);
    await js("document.querySelector('#usage-panel').scrollIntoView({block:'start'})");
    await screenshot('agent-allowance-connected-'+width);
  }
  await viewport(1280,850);
  usageRefreshFailure=true; usageMutationDelay=200;
  const refreshBefore=calls.filter(c=>c.path==='/api/usage/refresh').length;
  await click('#usage-refresh'); await click('#usage-refresh');
  ok(await js("document.querySelector('#usage-refresh').getAttribute('aria-busy')==='true' && document.querySelector('#usage-refresh svg') && document.querySelector('#usage-disconnect').disabled"), 'Usage refresh pill is busy and blocks duplicate mutations');
  await expectEventually("document.querySelector('#usage-badge').textContent==='Last known' && !document.querySelector('#usage-refresh').disabled", 'Failed usage refresh did not preserve stale quota');
  ok(calls.filter(c=>c.path==='/api/usage/refresh').length===refreshBefore+1 && await js("document.querySelector('#usage-windows [role=meter]').getAttribute('aria-valuenow')==='68' && document.querySelector('#usage-updated').textContent.includes('stale') && document.querySelector('#connection-badge').textContent==='Connected' && !document.querySelector('#send-button').disabled"), 'Failed refresh preserves last-known quota and leaves chat connected');
  usageRefreshFailure=false; usageMutationDelay=0;
  await click('#usage-refresh'); await expectEventually("document.querySelector('#usage-badge').textContent==='Connected' && !document.querySelector('#usage-refresh').disabled", 'Usage refresh recovery failed');
  // account/read can fail while the backend retains the last successful quota.
  const retainedUsage=usageStatus('p1');
  async function loadRetainedUsage(state, stale, identityMatch='unverified') {
    const error=`Fixture retained quota: ${state}/${stale}/${identityMatch}`;
    usageRecords.set('p1',{...structuredClone(retainedUsage),state,identity_match:identityMatch,error,snapshot:{...structuredClone(retainedUsage.snapshot),stale}});
    await click('#check-chatgpt');
    await expectEventually(`document.querySelector('#usage-error').textContent===${JSON.stringify(error)} && document.querySelector('#usage-refresh').getAttribute('aria-busy')==='false'`, 'Retained usage status did not load');
  }
  await loadRetainedUsage('unavailable',true);
  ok(await js("document.querySelector('#usage-windows [role=meter]')?.getAttribute('aria-valuenow')==='68'"), 'Unavailable status retains stale allowance meters');
  ok(await js("document.querySelector('#usage-badge').textContent==='Last known' && document.querySelector('#usage-message').textContent.includes('last-known') && document.querySelector('#usage-windows').classList.contains('usage-stale') && document.querySelector('#usage-windows [role=meter]').getAttribute('aria-label').includes('last known') && document.querySelector('#usage-updated').textContent.includes('stale')"), 'Unavailable retained allowance is explicitly last-known, never live');
  ok(await js("!document.querySelector('#usage-refresh').disabled && !document.querySelector('#usage-connect').hidden && !document.querySelector('#usage-connect').disabled && !document.querySelector('#usage-disconnect').hidden && !document.querySelector('#usage-disconnect').disabled && document.querySelector('#connection-badge').textContent==='Connected' && !document.querySelector('#send-button').disabled"), 'Unavailable retained quota leaves refresh, reconnect, disconnect, and chat usable');
  const retainedStatusBefore=calls.filter(c=>c.path==='/api/usage/status').length;
  await click('#usage-refresh');
  await expectEventually("document.querySelector('#usage-refresh').getAttribute('aria-busy')==='false'", 'Unavailable status refresh did not finish');
  ok(calls.filter(c=>c.path==='/api/usage/status').length===retainedStatusBefore+1 && await js("!!document.querySelector('#usage-windows [role=meter]')"), 'Unavailable refresh retries status and preserves the stale meter');
  await click('#usage-connect');
  await expectEventually("!document.querySelector('#usage-device').hidden && !document.querySelector('#usage-cancel').disabled", 'Unavailable usage reconnect did not start device flow');
  await click('#usage-cancel');
  await expectEventually("!document.querySelector('#usage-connect').disabled", 'Unavailable usage reconnect cancellation did not finish');
  for (const [state,stale,identityMatch] of [['unavailable',true,'different'],['unavailable',false,'unverified'],...['disconnected','expired','pending','checking','unsupported'].map(state=>[state,true,'unverified'])]) {
    await loadRetainedUsage(state,stale,identityMatch);
    ok(await js("document.querySelector('#usage-windows').childElementCount===0"), `Retained quota remains hidden for ${state}/${stale}/${identityMatch}`);
    if (identityMatch==='different') ok(await js("document.querySelector('#usage-badge').textContent==='Review account' && document.querySelector('#usage-message').textContent.includes('Allowance is hidden')"), 'Identity mismatch overrides stale allowance display');
    if (!stale) ok(await js("document.querySelector('#usage-badge').textContent==='Unavailable'"), 'Unavailable non-stale snapshot is not presented as live allowance');
  }
  usageRecords.set('p1',retainedUsage);
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#usage-badge').textContent==='Connected' && !document.querySelector('#usage-refresh').disabled", 'Retained quota fixture did not restore connected usage');
  const usageDisconnectCalls=()=>calls.filter(c=>c.path==='/api/usage/disconnect');
  const beforeUsageDisconnect=usageDisconnectCalls().length;
  await click('#usage-disconnect');
  ok(await js("document.querySelector('#usage-dialog').open && document.activeElement.id==='usage-dialog-cancel' && document.querySelector('#usage-dialog-account').textContent.includes('personal@example.com')"), 'Usage-only named confirmation focuses Cancel');
  await viewport(320,640);
  ok(await js("document.querySelector('#usage-dialog').getBoundingClientRect().right<=innerWidth && document.querySelector('#usage-confirm').getBoundingClientRect().bottom<=innerHeight && document.querySelector('#usage-dialog').scrollWidth<=document.querySelector('#usage-dialog').clientWidth"), 'Usage-only password confirmation fits a 320px phone');
  await screenshot('agent-allowance-confirmation-320');
  await viewport(1280,850);
  await js("document.querySelector('#usage-password').value='discard-this-synthetic-value'"); await click('#usage-dialog-cancel');
  await expectEventually("!document.querySelector('#usage-dialog').open && document.querySelector('#usage-password').value==='' && !document.querySelector('#usage-disconnect').disabled", 'Usage cancel did not clear password');
  await click('#usage-disconnect');
  await cdp('Input.dispatchKeyEvent',{type:'keyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27}); await cdp('Input.dispatchKeyEvent',{type:'keyUp',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
  await expectEventually("!document.querySelector('#usage-dialog').open && !document.querySelector('#usage-disconnect').disabled", 'Usage Escape did not cancel');
  ok(usageDisconnectCalls().length===beforeUsageDisconnect, 'Usage cancel and Escape do not mutate connections');
  await click('#usage-disconnect');
  await js("document.querySelector('#usage-password').value='wrong-synthetic-only';document.querySelector('#usage-confirm-form').requestSubmit()");
  await expectEventually("!document.querySelector('#usage-confirm-error').hidden && !document.querySelector('#usage-confirm').disabled", 'Usage password failure did not allow retry');
  ok(await js("document.querySelector('#usage-password').value==='' && document.querySelector('#usage-dialog').open && document.querySelector('#connection-badge').textContent==='Connected'"), 'Usage password failure clears input without touching SIWC');
  await js("document.querySelector('#usage-password').value='synthetic-test-only';document.querySelector('#usage-confirm-form').requestSubmit()");
  await expectEventually("!document.querySelector('#usage-dialog').open && !document.querySelector('#usage-connect').hidden && !document.querySelector('#usage-connect').disabled", 'Usage disconnect success failed');
  ok(usageDisconnectCalls().at(-1).payload.profile_id==='p1' && usageDisconnectCalls().at(-1).payload.connection_id==='u-p1' && providerProfiles[0].connected && await js("document.querySelector('#usage-error').textContent.includes('remote revocation') && document.querySelector('#connection-badge').textContent==='Connected' && !document.querySelector('#send-button').disabled"), 'Usage removal submits exact IDs, preserves chat, and reports revocation warning');
  usageRecords.delete('p1'); usageAvailable=false;
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#usage-badge').textContent==='Unavailable'", 'Missing dependency state not rendered');
  ok(await js("document.querySelector('#usage-connect').disabled && !document.querySelector('#usage-windows [role=meter]') && document.querySelector('#usage-message').textContent.includes('dependency') && document.querySelector('#connection-badge').textContent==='Connected'"), 'Missing Codex dependency disables Connect without invented quota');
  usageAvailable=true; usageStatusFailure=true;
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#usage-error').textContent.includes('status unavailable')", 'Usage status transport failure missing');
  ok(await js("document.querySelector('#usage-connect').disabled && !document.querySelector('#usage-refresh').disabled && document.querySelector('#connection-badge').textContent==='Connected'"), 'Unavailable usage status offers retry without assuming Connect availability');
  usageStatusFailure=false;
  await click('#usage-refresh'); await expectEventually("!document.querySelector('#usage-connect').disabled", 'Usage status retry did not recover');
  usageConnectFailureOnce=true;
  await click('#usage-connect');
  await expectEventually("document.querySelector('#usage-error').textContent.includes('device start failed') && !document.querySelector('#usage-connect').disabled", 'Usage start failure did not allow retry');
  await click('#usage-connect');
  await expectEventually("!document.querySelector('#usage-device').hidden && !document.querySelector('#usage-cancel').disabled", 'Usage start retry did not recover changed connection ID');
  ok(calls.findLast(c=>c.path==='/api/usage/connect').payload.expected_connection_id==='recovered-p1', 'Mutation failure reloads status before retrying a newly persisted connection ID');
  await click('#usage-cancel'); await expectEventually("!document.querySelector('#usage-connect').disabled", 'Recovery login cancel failed');
  usageRecords.set('p1',{profile_id:'p1',available:true,state:'expired',connection_id:'expired-p1',identity_match:'unverified'});
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#usage-connect').textContent==='Reconnect usage account' && !document.querySelector('#usage-connect').disabled", 'Expired usage reconnect missing');
  await click('#usage-connect'); await expectEventually("!document.querySelector('#usage-device').hidden && !document.querySelector('#usage-cancel').disabled", 'Expired reconnect did not start device flow');
  await click('#usage-copy'); await expectEventually("window.copiedUsageCode==='TEST-CODE' && document.querySelector('#usage-feedback').textContent==='Code copied.'", 'Usage copy feedback missing');
  await js("navigator.clipboard.writeText=async()=>{throw new Error('Fixture clipboard denied')}"); await click('#usage-copy');
  await expectEventually("document.querySelector('#usage-feedback').textContent.includes('manually')", 'Usage copy fallback missing');
  const unsafeLogin=usageRecords.get('p1').login;
  unsafeLogin.verification_url='https://auth.openai.com.evil.invalid/codex/device';
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#usage-panel').hidden && !document.querySelector('#usage-device').hidden && !document.querySelector('#check-chatgpt').disabled && document.querySelector('#usage-refresh').getAttribute('aria-busy')==='false'", 'Unsafe verification fixture did not load');
  ok(await js("document.querySelector('#usage-verification').hidden && !document.querySelector('#usage-verification').hasAttribute('href') && document.querySelector('#usage-code').textContent==='TEST-CODE'"), 'Untrusted verification URLs are never made clickable');
  unsafeLogin.verification_url='https://auth.openai.com/codex/device';
  const pendingUsageCalls=calls.filter(c=>c.path==='/api/usage/status').length;
  await click('#back-to-chat'); await new Promise(r=>setTimeout(r,2250));
  ok(calls.filter(c=>c.path==='/api/usage/status').length===pendingUsageCalls, 'Pending usage polling stops outside Settings');
  const mismatched=usageRecords.get('p1'); Object.assign(mismatched,{state:'connected',identity:{email:'different@example.com',plan_type:'Fixture plan'},identity_match:'different',snapshot:{fetched_at:1900000000,stale:false,windows:[{id:'hidden',label:'Hidden window',used_percent:10,window_minutes:60,reset_at:1900000000}]}}); delete mismatched.login;
  await click('#settings-button');
  await expectEventually("document.querySelector('#usage-badge').textContent==='Review account'", 'Usage identity mismatch missing');
  ok(await js("document.querySelector('#usage-identity').textContent.includes('different@example.com') && !document.querySelector('#usage-windows [role=meter]') && document.querySelector('#usage-message').textContent.includes('Reconnect') && !document.querySelector('#usage-connect').hidden && !document.querySelector('#usage-disconnect').hidden"), 'Different usage identity hides all meters and offers reconnect or disconnect');
  mismatched.identity_match='unverified'; mismatched.identity.email='<img src=x onerror=alert(1)>';
  mismatched.snapshot.windows[0].used_percent=null;
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#usage-windows').textContent.includes('Unavailable')", 'Null quota was treated as full allowance');
  ok(await js("!document.querySelector('#usage-windows [role=meter]') && !document.querySelector('#usage-identity img') && document.querySelector('#usage-identity').textContent.includes('<img')"), 'Invalid percent remains unavailable and usage identities are rendered as text');
  // A delayed status from Personal must never replace Other after a switch.
  usageDelay=900;
  await click('#check-chatgpt');
  await until(()=>calls.some(c=>c.path==='/api/usage/status' && !c.status), 'Delayed usage read did not start');
  usageDelay=0;
  await changeAccount('p2');
  await expectEventually("document.querySelector('#account-select').value==='p2' && !document.querySelector('#check-chatgpt').disabled && !document.querySelector('#usage-connect').disabled", 'Switch with delayed usage read failed');
  await new Promise(r=>setTimeout(r,1000));
  ok(await js("document.querySelector('#usage-identity').hidden && !document.querySelector('#usage-windows [role=meter]') && document.querySelector('#usage-badge').textContent==='Optional'"), 'Delayed allowance response cannot replace another account');
  await changeAccount('p1'); await expectEventually("document.querySelector('#account-select').value==='p1' && !document.querySelector('#check-chatgpt').disabled && !document.querySelector('#usage-disconnect').disabled", 'Restore Personal usage failed');
  await click('#usage-disconnect');
  activeProfile='p2'; await js("window.dispatchEvent(new PageTransitionEvent('pageshow',{persisted:true}))");
  await expectEventually("document.querySelector('#account-select').value==='p2' && !document.querySelector('#check-chatgpt').disabled", 'Background profile refresh failed during usage confirmation');
  await js("document.querySelector('#usage-password').value='synthetic-test-only';document.querySelector('#usage-confirm-form').requestSubmit()");
  await expectEventually("!document.querySelector('#usage-confirm-error').hidden && !document.querySelector('#usage-confirm').disabled", 'Stale usage target not rejected');
  ok(usageDisconnectCalls().at(-1).payload.profile_id==='p1' && usageDisconnectCalls().at(-1).payload.connection_id==='u-p1' && usageDisconnectCalls().at(-1).status===409, 'Usage confirmation keeps immutable profile and connection IDs across background account refresh');
  await click('#usage-dialog-cancel');
  await changeAccount('p1'); await expectEventually("document.querySelector('#account-select').value==='p1' && !document.querySelector('#check-chatgpt').disabled", 'Restore first profile failed');
  const beforePasswordSwitch = calls.filter(call => call.path === '/api/auth/profiles/active').length;
  await js("document.querySelector('#account-select').value='p2';document.querySelector('#account-select').dispatchEvent(new Event('change'))");
  ok(await js("document.querySelector('#disconnect-dialog').open && document.querySelector('#connection-password').type==='password'"), 'Switching a connection asks for the Agent password');
  ok(calls.filter(call => call.path === '/api/auth/profiles/active').length === beforePasswordSwitch, 'Opening password confirmation does not change the account');
  await click('#cancel-disconnect');
  await expectEventually("!document.querySelector('#disconnect-dialog').open && !document.querySelector('#account-select').disabled", 'Canceled connection change did not unlock');
  ok(await js("document.querySelector('#account-select').value==='p1' && document.querySelector('#connection-password').value===''"), 'Cancel restores the active selection and clears the password');
  await js("document.querySelector('#account-select').value='p2';document.querySelector('#account-select').dispatchEvent(new Event('change'))");
  await click('#confirm-disconnect');
  ok(calls.filter(call => call.path === '/api/auth/profiles/active').length === beforePasswordSwitch, 'Empty password is not submitted');
  await confirmChange('wrong-synthetic-only');
  await expectEventually("!document.querySelector('#connection-confirm-error').hidden && !document.querySelector('#confirm-disconnect').disabled", 'Wrong password did not show inline error');
  ok(activeProfile === 'p1' && await js("document.querySelector('#disconnect-dialog').open && document.querySelector('#connection-password').value===''"), 'Wrong password leaves connection unchanged and clears the input for retry');
  await click('#cancel-disconnect');
  await expectEventually("!document.querySelector('#account-select').disabled", 'Wrong-password cancel did not unlock');
  ok(!(await visible('#password-form')), 'Password security fields are collapsed by default');
  await click('#security-toggle');
  ok(await visible('#password-form'), 'Reset password expands the existing form');
  await js("document.querySelector('#current-password').value='synthetic-test-only';document.querySelector('#new-password').value='synthetic-new-only';document.querySelector('#confirm-password').value='different-test-only';document.querySelector('#password-form').requestSubmit()");
  ok(await js("document.querySelector('#password-status').textContent.includes('match')"), 'Password confirmation mismatch is shown');
  ok(!calls.some(call => call.path === '/api/auth/password'), 'Mismatched passwords are not submitted');
  passwordFailure = true;
  await js("document.querySelector('#confirm-password').value='synthetic-new-only';document.querySelector('#password-form').requestSubmit()");
  await expectEventually("document.querySelector('#password-status').textContent.includes('Current password is incorrect.') && !document.querySelector('#password-submit').disabled", 'Password failure not recoverable');
  passwordFailure = false;
  await js("document.querySelector('#password-form').requestSubmit()");
  await expectEventually("document.querySelector('#password-status').textContent.includes('Password updated')", 'Password success missing');
  ok(await js("Array.from(document.querySelectorAll('#password-form input')).every(e=>e.value==='')"), 'Successful password change clears sensitive inputs');
  await click('#security-toggle');
  await js("document.querySelector('#settings-panel').scrollTop=0");
  for (const [width,height] of [[1920,920],[1280,850],[390,640]]) {
    await viewport(width,height);
    ok(await js('document.documentElement.scrollWidth<=innerWidth && document.documentElement.scrollHeight<=innerHeight'), `Settings fits ${width}x${height}`);
    ok(await js(`(() => {
      const panel = document.querySelector('#settings-panel');
      const bounds = panel.getBoundingClientRect();
      const content = document.querySelector('.settings-content').getBoundingClientRect();
      const left = content.left - bounds.left;
      const right = bounds.left + panel.clientWidth - content.right;
      return Math.abs(left - right) <= 1 && content.width <= 600 && panel.scrollWidth <= panel.clientWidth;
    })()`), `Settings content is horizontally centered in the main panel at ${width}x${height}`);
    ok(await visible('#logout-button'), 'Settings sign-out remains reachable');
    await screenshot('agent-settings-' + width);
  }
  await viewport(1280,850);
  await click('#back-to-chat');
  ok(await visible('#messages') && !(await visible('#settings-panel')), 'Back to conversation restores chat');
  ok(await js("location.hash===''"), 'Returning to chat clears the Settings URL');
  await js("document.querySelector('#message-input').value='Unsent draft'");
  await click('#settings-button'); await click('#back-to-chat');
  ok(await js("document.querySelector('#message-input').value==='Unsent draft'"), 'Settings preserves the unsent conversation draft');
  await click('#settings-button'); await session('A quieter week');
  await expectEventually("!document.querySelector('#messages').hidden && document.querySelector('#messages').textContent.includes('Leave two evenings open.')", 'Session selection did not leave Settings');
  await click('#settings-button'); await click('#new-chat');
  ok(!(await visible('#settings-panel')) && await visible('#welcome'), 'New conversation leaves Settings');
  await viewport(390,640); await click('#sidebar-toggle'); await click('#settings-button');
  ok(!(await visible('#sidebar-resize')), 'Sidebar resizing stays hidden on phones');
  ok(await visible('#settings-panel') && !(await visible('#session-sidebar')), 'Phone gear opens Settings and closes sidebar');
  await expectEventually("!document.querySelector('#usage-panel').hidden && !document.querySelector('#usage-connect').disabled", 'Phone usage load unfinished');
  await js("document.querySelector('#settings-panel').scrollTop=document.querySelector('#settings-panel').scrollHeight");
  ok(await js("document.querySelector('#logout-button').getBoundingClientRect().bottom<=innerHeight && document.querySelector('#logout-button').getBoundingClientRect().top>=60"), 'Phone settings scroll exposes the sign-out button');
  await click('#back-to-chat'); await viewport(1280,850);
  ok(!(await js("!!document.querySelector('#conversation-list img')")), 'Conversation titles are text, not injected HTML');
  await session('A quieter week');
  await expectEventually("document.querySelector('#messages').textContent.includes('Leave two evenings open.')", 'Saved conversation did not load');
  ok(await js("document.querySelector('.message.assistant .sender').textContent==='Agent' && document.querySelector('.message.user .sender').firstChild.textContent==='You' && !!document.querySelector('.message.user .sender .message-actions-toggle')"), 'Saved messages display Agent and preserve You beside the compact action menu');
  ok(await js("document.querySelector('#conversation-title').textContent==='A quieter week' && !!document.querySelector('#conversation-list [aria-current=\"true\"]')"), 'Saved-session selection updates title and current row');
  await js("const search=document.querySelector('#conversation-search');search.value='dinner';search.dispatchEvent(new Event('input'))");
  ok(await js("Array.from(document.querySelectorAll('#conversation-list button')).filter(b=>b.getClientRects().length).length===1"), 'Session search filters the sidebar');
  await js("document.querySelector('#conversation-search').value='nothingmatches';document.querySelector('#conversation-search').dispatchEvent(new Event('input'))");
  ok(await js("document.querySelector('#conversation-list').textContent.toLowerCase().includes('no')"), 'Empty search has feedback');
  await js("document.querySelector('#conversation-search').value='';document.querySelector('#conversation-search').dispatchEvent(new Event('input'))");
  await click('#account-menu-toggle');
  ok(await visible('#account-menu'), 'Account menu retains a Settings shortcut');
  await click('#manage-connections');
  await expectEventually("!document.querySelector('#account-select').disabled && !document.querySelector('#check-chatgpt').disabled", 'Settings connection refresh unfinished');
  ok(!(await visible('#account-menu')), 'Settings shortcut closes the old account popup');
  ok(await visible('#account-select'), 'Connected provider account selector remains available');
  profileFailure = true;
  await changeAccount('p2');
  await expectEventually("!document.querySelector('#provider-error').hidden && !document.querySelector('#account-select').disabled", 'Provider change failure not recoverable');
  ok(await visible('#provider-error'), 'Provider selection failure stays next to account controls');
  profileFailure = false;
  await changeAccount('p2');
  await expectEventually("document.querySelector('#account-select').value==='p2' && !document.querySelector('#account-select').disabled", 'Provider selection retry failed');
  ok(activeProfile === 'p2', 'Provider selection retry refreshes the actual selected account');
  disconnectFailure = true;
  const disconnectCalls = () => calls.filter(call => call.path === '/api/auth/disconnect').length;
  const beforeDisconnect = disconnectCalls();
  await click('#disconnect-chatgpt');
  ok(await js("document.querySelector('#disconnect-dialog').open && document.querySelector('#disconnect-account').textContent.includes('Other · other@example.com')"), 'Disconnect confirmation names the active account');
  ok(await js("document.activeElement.id==='cancel-disconnect' && document.querySelector('#account-select').disabled"), 'Confirmation focuses Cancel and locks account switching');
  await click('#cancel-disconnect');
  await expectEventually("!document.querySelector('#disconnect-chatgpt').disabled", 'Cancel did not unlock controls');
  ok(disconnectCalls() === beforeDisconnect, 'Cancel performs no disconnect request');
  await click('#disconnect-chatgpt');
  await cdp('Input.dispatchKeyEvent', {type:'keyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
  await cdp('Input.dispatchKeyEvent', {type:'keyUp',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
  await expectEventually("!document.querySelector('#disconnect-dialog').open && !document.querySelector('#disconnect-chatgpt').disabled", 'Escape did not cancel disconnect');
  ok(disconnectCalls() === beforeDisconnect, 'Escape performs no disconnect request');
  await click('#disconnect-chatgpt');
  activeProfile = 'p1'; // Another browser tab changes the active account after confirmation opens.
  await confirmChange();
  await expectEventually("document.querySelector('#provider-error').textContent.includes('account changed') && document.querySelector('#account-select').value==='p1' && !document.querySelector('#account-select').disabled", 'Stale account confirmation did not recover');
  ok(providerProfiles.every(p => p.connected), 'Stale named confirmation disconnects neither account');
  // A real in-flight chat reauthorization refresh must not retarget an open confirmation.
  disconnectFailure = false;
  chatFailure = true; chatErrorAction = 'reauthorize'; chatDelay = 800;
  const chatsBeforeConfirmation = calls.filter(call => call.path === '/api/chat').length;
  await js("document.querySelector('#message-input').value='Refresh during disconnect confirmation';document.querySelector('#chat-form').requestSubmit()");
  await until(() => calls.filter(call => call.path === '/api/chat').length > chatsBeforeConfirmation, 'Background chat request did not start');
  await click('#disconnect-chatgpt');
  ok(await js("document.querySelector('#disconnect-dialog').open && document.querySelector('#disconnect-account').textContent.includes('Personal · personal@example.com')"), 'Background-refresh confirmation initially names Personal');
  activeProfile = 'p2'; // Another tab changes the server selection before reauthorization refreshes it.
  await expectEventually("document.querySelector('#account-select').value==='p2' && !document.querySelector('#send-button').disabled", 'Chat reauthorization did not refresh the account while confirmation was open');
  ok(await js("document.querySelector('#disconnect-dialog').open && document.querySelector('#disconnect-account').textContent.includes('Personal · personal@example.com')"), 'Background refresh leaves the confirmed account name unchanged');
  chatFailure = false; chatErrorAction = 'manage_usage'; chatDelay = 0;
  const profilesBeforeConfirmation = calls.filter(call => call.path === '/api/auth/profiles').length;
  const disconnectsBeforeConfirmation = disconnectCalls();
  await confirmChange();
  await until(() => disconnectCalls() > disconnectsBeforeConfirmation && calls.findLast(call => call.path === '/api/auth/disconnect').status, 'Background-refresh confirmation was not submitted');
  const refreshedConfirmation = calls.findLast(call => call.path === '/api/auth/disconnect');
  assert.equal(refreshedConfirmation.payload.profile_id, 'p1', 'Background refresh must submit Personal (p1), not the refreshed Other (p2)');
  ok(refreshedConfirmation.status === 409 && providerProfiles.every(p => p.connected), 'Background-refresh stale confirmation returns 409 and leaves both accounts connected');
  await expectEventually("document.querySelector('#provider-error').textContent.includes('account changed') && document.querySelector('#account-select').value==='p2' && !document.querySelector('#account-select').disabled", 'Background-refresh conflict did not recover the Settings UI');
  ok(calls.filter(call => call.path === '/api/auth/profiles').length > profilesBeforeConfirmation, 'Background-refresh conflict reloads the account state');
  // Cancel a new Other confirmation, then reopen Personal: no previous target may leak.
  await click('#disconnect-chatgpt');
  ok(await js("document.querySelector('#disconnect-account').textContent.includes('Other · other@example.com')"), 'Reopened confirmation names the refreshed account');
  await click('#cancel-disconnect');
  await expectEventually("!document.querySelector('#account-select').disabled", 'Reopened confirmation cancel did not unlock account switching');
  await changeAccount('p1');
  await expectEventually("document.querySelector('#account-select').value==='p1' && !document.querySelector('#account-select').disabled", 'Could not select Personal after cancel');
  disconnectFailure = true;
  await click('#disconnect-chatgpt');
  ok(await js("document.querySelector('#disconnect-account').textContent.includes('Personal · personal@example.com')"), 'Cancel and reopen resets the named confirmation target');
  await confirmChange();
  await expectEventually("document.querySelector('#provider-error').textContent.includes('disconnect failure') && !document.querySelector('#account-select').disabled", 'Reopened confirmation failure did not recover');
  ok(calls.findLast(call => call.path === '/api/auth/disconnect').payload.profile_id === 'p1' && providerProfiles.every(p => p.connected), 'Cancel and reopen submits only the newly confirmed account');
  await changeAccount('p2');
  await expectEventually("document.querySelector('#account-select').value==='p2' && !document.querySelector('#account-select').disabled", 'Could not restore second account');
  await click('#disconnect-chatgpt');
  await confirmChange();
  await expectEventually("!document.querySelector('#provider-error').hidden && !document.querySelector('#disconnect-chatgpt').disabled", 'Disconnect failure not recoverable');
  ok(await visible('#disconnect-chatgpt'), 'Failed provider disconnect preserves the retry action');
  disconnectFailure = false;
  remoteRevoked = false;
  await click('#disconnect-chatgpt'); await confirmChange();
  await expectEventually("document.querySelector('#provider-feedback').textContent.includes('remote revocation was not confirmed') && !document.querySelector('#connect-chatgpt').disabled", 'Remote revocation warning missing');
  ok(calls.findLast(call => call.path === '/api/auth/disconnect').payload.profile_id === 'p2', 'Disconnect submits the exact confirmed account');
  ok(await js("document.querySelector('#connection-badge').textContent==='Not connected' && document.querySelector('#disconnect-chatgpt').hidden && !document.querySelector('#connect-chatgpt').hidden"), 'Successful local disconnect removes connected indicators and offers reconnect');
  ok(providerProfiles[0].connected && authenticated, 'Disconnect preserves the other account and Agent sign-in');
  authorizationFailure = true;
  authorizationDelay = 200;
  const startsBeforeFailure = calls.filter(call => call.path === '/auth/start').length;
  await click('#connect-chatgpt');
  ok(await js("!document.querySelector('#disconnect-dialog').open && document.querySelector('#connect-chatgpt').disabled && document.querySelector('#check-chatgpt').disabled"), 'Local authorization starts directly without a password dialog and locks overlapping actions');
  await click('#connect-chatgpt');
  await expectEventually("document.querySelector('#provider-error').textContent.includes('authorization start unavailable') && !document.querySelector('#connect-chatgpt').disabled", 'Authorization failure was not shown inline for retry');
  ok(calls.filter(call => call.path === '/auth/start').length === startsBeforeFailure + 1, 'Double-clicking connect cannot create duplicate authorization starts');
  authorizationFailure = false;
  authorizationDelay = 0;
  await click('#connect-chatgpt');
  await expectEventually("document.querySelector('#chat-app') && !document.querySelector('#chat-app').hidden && !document.querySelector('#check-chatgpt').disabled", 'Reconnection route handoff did not return');
  ok(calls.some(call => call.path === '/auth/start' && call.query === ''), 'Reconnect uses the existing ChatGPT authorization route');
  await click('#settings-button');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Settings refresh unfinished');
  connectionDelay = 750;
  const checksBeforeKeyboard = calls.filter(call => call.path === '/api/auth/status').length;
  await cdp('Page.bringToFront');
  await js("document.querySelector('#check-chatgpt').focus()");
  await cdp('Input.dispatchKeyEvent',{type:'keyDown',key:'Enter',code:'Enter',windowsVirtualKeyCode:13,text:'\r',unmodifiedText:'\r'});
  await cdp('Input.dispatchKeyEvent',{type:'keyUp',key:'Enter',code:'Enter',windowsVirtualKeyCode:13});
  await expectEventually("document.querySelector('#check-chatgpt').disabled", 'Keyboard Enter did not start refreshing the pill');
  ok(await js("document.querySelector('#check-chatgpt').disabled && document.querySelector('#check-chatgpt').textContent==='Checking…' && document.querySelector('#check-chatgpt').getAttribute('aria-busy')==='true' && !!document.querySelector('#check-chatgpt svg') && document.querySelector('#disconnect-chatgpt').disabled"), 'Keyboard refresh shows a busy pill and prevents overlapping account mutations');
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#provider-feedback').textContent==='Connection status refreshed.' && !document.querySelector('#check-chatgpt').disabled", 'Connection check did not finish');
  ok(calls.filter(call => call.path === '/api/auth/status').length === checksBeforeKeyboard + 1, 'Repeated activation while refreshing sends only one status request');
  connectionDelay = 0;
  const statusCallsBeforeRestore = calls.filter(call => call.path === '/api/auth/status').length;
  await js("window.dispatchEvent(new PageTransitionEvent('pageshow',{persisted:true}))");
  await until(() => calls.filter(call => call.path === '/api/auth/status').length > statusCallsBeforeRestore, 'Restored page did not refresh its status');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Restored page did not recheck sign-in and unlock connection controls');
  modelsDelay = 400;
  const modelsBeforeCheck = calls.filter(call=>call.path==='/api/models').length;
  await click('#check-chatgpt');
  await until(()=>calls.filter(call=>call.path==='/api/models').length > modelsBeforeCheck, 'Model refresh did not start');
  ok(await js("document.querySelector('#account-select').disabled && document.querySelector('#disconnect-chatgpt').disabled"), 'Account mutations remain locked through the model-loading phase of a connection check');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Delayed model refresh stuck');
  modelsDelay = 0;
  connectionStatusFailure = true;
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#connection-badge').textContent==='Unavailable' && !document.querySelector('#provider-error').hidden && !document.querySelector('#check-chatgpt').disabled", 'Status failure did not replace success badge');
  connectionStatusFailure = false; profilesFailure = true;
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Profile failure check stuck');
  ok(await js("document.querySelector('#connection-badge').textContent==='Unavailable' && document.querySelector('#account-select').hidden"), 'Profile list failure has no false connected badge or selectable stale account');
  profilesFailure = false; modelsFailure = true;
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Model failure check stuck');
  ok(await js("document.querySelector('#model-select').disabled && document.querySelector('#connection-status').textContent.includes('model list could not be loaded')"), 'Model failure is distinguished from authorization failure');
  modelsFailure = false;
  Object.assign(providerProfiles[1], {connected:false, oauth_selected:true, plan_usage_enabled:false});
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#connection-badge').textContent==='Sign-in required' && !document.querySelector('#check-chatgpt').disabled", 'Expired connection not shown');
  ok(await js("document.querySelector('#connect-chatgpt').textContent.includes('Reconnect')"), 'Expired authorization offers reconnect');
  Object.assign(providerProfiles[1], {connected:true, plan_usage_enabled:false});
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#connection-badge').textContent==='Permission required' && !document.querySelector('#check-chatgpt').disabled", 'Missing plan permission not shown');
  ok(await js("document.querySelector('#connect-chatgpt').textContent.includes('permissions')"), 'Missing plan permission offers permission review');
  Object.assign(providerProfiles[1], {connected:true, plan_usage_enabled:true});
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Connection recovery stuck');
  const savedProfiles = providerProfiles.splice(0);
  activeProfile = '';
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#connection-badge').textContent==='Not connected' && !document.querySelector('#check-chatgpt').disabled", 'Empty subscription state missing');
  ok(await js("document.querySelector('#usage-panel').hidden"), 'Allowance is hidden without an active conversation profile');
  ok(await js("document.querySelector('#account-select').hidden && document.querySelector('#add-chatgpt-account').hidden && !document.querySelector('#connect-chatgpt').hidden"), 'First-time connection offers authorization without a stale selector');
  providerProfiles.push(...savedProfiles); activeProfile = 'p2';
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Restored profiles check stuck');
  activeProfile = '';
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'No-active profile check stuck');
  ok(await js("document.querySelector('#connection-badge').textContent==='Not connected'"), 'Saved accounts without an active account are not falsely labeled as pending authorization');
  activeProfile = 'p2';
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Active profile check stuck');
  await click('#add-chatgpt-account');
  await expectEventually("document.querySelector('#account-select')?.value==='p3' && !document.querySelector('#check-chatgpt').disabled", 'Add-account route handoff failed');
  ok(calls.some(call => call.path === '/auth/start' && call.query === '?mode=add'), 'Add account uses the dedicated authorization mode');
  providerProfiles.push({id:'pending',label:'Incomplete',registration_pending:true,oauth_selected:true}, {id:'pending-two',label:'Other incomplete',registration_pending:true,oauth_selected:true});
  activeProfile = '';
  await click('#settings-button');
  await expectEventually("document.querySelector('#account-select').options.length===6 && !document.querySelector('#account-select').disabled", 'Pending account not loaded');
  ok(await js("document.querySelector('#connection-badge').textContent==='Awaiting sign-in'"), 'Incomplete registrations are explicitly labeled pending');
  const beforeResume = calls.filter(call=>call.path==='/auth/start').length;
  await click('#connect-chatgpt');
  await expectEventually("document.querySelector('#provider-error')?.textContent.includes('Select the incomplete')", 'Ambiguous pending authorization must request account selection');
  ok(calls.filter(call=>call.path==='/auth/start').length === beforeResume, 'Multiple incomplete accounts do not resume an arbitrary registration');
  await changeAccount('pending');
  await expectEventually("document.querySelector('#account-select')?.value==='pending' && !document.querySelector('#check-chatgpt').disabled", 'Pending account resume failed');
  ok(calls.some(call => call.path === '/auth/start' && call.query === '?mode=retry&profile_id=pending'), 'Incomplete account resumes its exact authorization without selecting it prematurely');
  ok(calls.filter(call => call.path === '/auth/start').every(call => call.method === 'POST' && !call.query.includes('password') && !call.payload.keys.includes('password')), 'Every local authorization start uses POST without submitting an Agent password');
  remoteRevoked = true;
  await click('#settings-button');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Settings refresh unfinished');
  await click('#disconnect-chatgpt'); await confirmChange();
  await expectEventually("document.querySelector('#provider-feedback').textContent==='Disconnected from ChatGPT on this host.' && !document.querySelector('#check-chatgpt').disabled", 'Confirmed disconnect success missing');
  ok(await js("!document.querySelector('#provider-feedback').classList.contains('warning')"), 'Confirmed remote revocation shows success rather than a warning');
  await click('#connect-chatgpt');
  await expectEventually("document.querySelector('#account-select')?.value==='pending' && !document.querySelector('#check-chatgpt').disabled", 'Reconnect after confirmed disconnect failed');
  await click('#new-chat');
  ok(await visible('#welcome'), 'New conversation resets the conversation view');
  ok(!(await visible('#conversation-save-status')), 'An unsaved new conversation does not claim to be saved');
  chatDelay = 1500;
  await js("document.querySelector('#message-input').value='A test conversation';document.querySelector('#model-select').value='test-two';document.querySelector('#model-select').dispatchEvent(new Event('change'));document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("document.querySelector('#send-button').disabled", 'Send loading state missing');
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-state').textContent==='Running'", 'Pending model call did not expose its real running trace');
  ok(await js("document.querySelector('#run-inspector').textContent.includes('Waiting for response')"), 'Inspector can monitor an in-flight request');
  await screenshot('agent-run-inspector-running');
  await click('#run-close');
  await click('#new-chat');
  await session('Dinner ideas');
  await expectEventually("document.querySelector('#messages').textContent.includes('A real UI response')", 'Chat response missing');
  await expectEventually("document.querySelector('#conversation-title').textContent==='A test conversation'", 'New chat title did not update');
  ok(await js("document.querySelector('.message.assistant h2')?.textContent==='A formatted reply'"), 'New model replies use the same formatted renderer as saved conversations');
  const chatCall = calls.findLast(call => call.path === '/api/chat');
  ok(chatCall.payload.model === 'test-two' && chatCall.payload.conversation_id === '', 'Composer sends selected model and correct conversation identity');
  ok(typeof chatCall.payload.run_id==='string' && /^[a-f0-9-]{36}$/.test(chatCall.payload.run_id) && chatCall.payload.omitted_messages===0, 'Composer sends a bounded correlation ID and reports context omissions');
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-state').textContent==='Completed'", 'Completed trace unavailable');
  await click('#run-close');
  ok(await js("!document.querySelector('#disconnect-dialog').open"), 'Ordinary model choice and chat do not ask for a connection password');
  ok(await js("!!document.querySelector('#conversation-list [aria-current=\"true\"]')"), 'Newly saved chat is selected in session history');
  await cdp('Page.reload');
  await expectEventually("document.querySelector('#model-select').value==='test-two'", 'Model preference missing after reload');
  await session('A test conversation');
  await expectEventually("document.querySelector('#messages').textContent.includes('A real UI response')", 'Saved messages missing after reload');
  ok(await js("document.querySelector('.message.assistant strong')?.textContent==='two evenings'"), 'Formatting survives loading the saved original Markdown after reload');
  ok(true, 'Model preference and saved conversation survive page reload');
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-state').textContent==='Completed'", 'Trace association did not survive reload');
  await click('#run-close');
  listFailure = true;
  await cdp('Page.reload');
  await expectEventually("document.querySelector('#conversation-list').textContent.includes('Saved conversations could not be loaded.')", 'History failure not shown');
  listFailure = false;
  await js("Array.from(document.querySelectorAll('#conversation-list button')).find(b=>b.textContent==='Retry').click()");
  await expectEventually("document.querySelectorAll('#conversation-list .session').length===4", 'History retry failed');
  ok(true, 'History load failure exposes a working retry');
  loadFailure = true;
  await session('Dinner ideas');
  await expectEventually("document.querySelector('#notice').classList.contains('error')", 'Conversation error missing');
  loadFailure = false;
  await session('Dinner ideas');
  await expectEventually("document.querySelector('#messages').textContent.includes('Try rice')", 'Conversation retry failed');
  ok(true, 'Failed conversation load can be retried');
  loadDelay = 250;
  await session('A quieter week'); await click('#new-chat');
  await expectEventually("document.querySelector('#messages').textContent.includes('Leave two evenings open.')", 'Inflight selection lost');
  ok(!(await visible('#welcome')), 'In-flight conversation load cannot overwrite a new-chat reset');
  loadDelay = 0;
  chatFailure = true; await click('#new-chat');
  await js("document.querySelector('#message-input').value='Failure test';document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("!!document.querySelector('#notice a')", 'Usage action missing');
  ok(await visible('#notice'), 'Request failures remain visible above the composer');
  ok(await js("document.querySelector('#notice a').href==='https://chatgpt.com/settings/usage'"), 'Provider usage errors retain the actionable usage link');
  await click('.inspect-run');
  await expectEventually("document.querySelector('#run-state').textContent==='Failed'", 'Failed request trace unavailable');
  await click('#run-tab-model');
  ok(await js("!document.querySelector('#run-error').hidden && document.querySelector('#run-error').textContent.includes('Fixture provider failure') && !document.querySelector('#run-state').classList.contains('completed')"), 'Failed model calls remain inspectable without a success indicator');
  await screenshot('agent-run-inspector-failed');
  await click('#run-close');
  chatDelay = 0;
  chatFailure = false;
  await session('A test conversation');
  await expectEventually("document.querySelector('#messages').textContent.includes('A real UI response')", 'Saved chat missing');
  await screenshot('agent-chat-desktop');
  // Stress both independently scrolling panes with realistic long content.
  await js("for(let i=0;i<100;i++){const row=document.createElement('button');row.textContent='Additional session '+i;document.querySelector('#conversation-list').append(row)}for(let i=0;i<80;i++){const p=document.createElement('p');p.textContent='Long conversation content '.repeat(8);document.querySelector('#messages').append(p)}");
  for (const [width, height] of [[1280,850],[1024,600],[390,844],[390,640]]) {
    await viewport(width,height);
    ok(await js('document.documentElement.scrollWidth<=innerWidth && document.documentElement.scrollHeight<=innerHeight'), `Workspace has no outer overflow at ${width}x${height}`);
    ok(await js("document.querySelector('#send-button').getBoundingClientRect().bottom<=innerHeight"), `Composer remains visible at ${width}x${height}`);
  }
  await click('#sidebar-toggle');
  ok(await visible('#session-sidebar'), 'Phone menu opens session history');
  ok(await js("document.activeElement.id==='new-chat'"), 'Opening the phone menu focuses navigation, not the search textbox');
  await drag('#conversation-search', 0, 0);
  ok(await js("document.activeElement.id==='conversation-search'"), 'Phone search remains focusable when explicitly selected');
  ok(await visible('#app-version') && await js("document.querySelector('#app-version').getBoundingClientRect().bottom <= innerHeight && document.querySelector('#app-version').getBoundingClientRect().top >= document.querySelector('.account-row').getBoundingClientRect().bottom"), 'Phone sidebar keeps the build version below the user and inside the viewport');
  ok(await js("document.querySelector('#sidebar-toggle').getAttribute('aria-expanded')==='true'"), 'Phone toggle announces expanded state');
  await screenshot('agent-chat-phone-menu');
  await click('#sidebar-backdrop');
  ok(!(await visible('#session-sidebar')), 'Phone backdrop closes session history');
  await click('#sidebar-toggle');
  await cdp('Input.dispatchKeyEvent',{type:'keyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
  await cdp('Input.dispatchKeyEvent',{type:'keyUp',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
  ok(!(await visible('#session-sidebar')), 'Escape closes phone navigation');
  await click('#sidebar-toggle'); await session('Dinner ideas');
  await expectEventually("document.querySelector('#messages').textContent.includes('Try rice')", 'Mobile selection failed');
  ok(!(await visible('#session-sidebar')), 'Choosing a session closes the phone menu');
  await screenshot('agent-chat-phone');
  // An intercepted non-loopback origin exercises remote connection management without external services.
  const remoteCalls = [];
  socket.addEventListener('message', async event => {
    const data = JSON.parse(event.data);
    if (data.method !== 'Fetch.requestPaused') return;
    const {requestId, request} = data.params;
    const remoteURL = new URL(request.url);
    remoteCalls.push({path:remoteURL.pathname,method:request.method});
    try {
      const response = await fetch(origin + remoteURL.pathname + remoteURL.search, {
        method: request.method,
        headers: { 'Content-Type': request.headers['Content-Type'] || 'application/json' },
        body: request.method === 'GET' ? undefined : request.postData,
      });
      const body = Buffer.from(await response.arrayBuffer()).toString('base64');
      await cdp('Fetch.fulfillRequest', {requestId,responseCode:response.status,responseHeaders:[{name:'Content-Type',value:response.headers.get('content-type') || 'text/plain'}],body});
    } catch (error) { runtimeErrors.push(error.message); await cdp('Fetch.failRequest',{requestId,errorReason:'Failed'}); }
  });
  await cdp('Fetch.enable', {patterns:[{urlPattern:'http://agent.invalid/*',requestStage:'Request'}]});
  await viewport(1280,850);
  await cdp('Page.navigate',{url:'http://agent.invalid/'});
  await expectEventually("document.querySelector('#chat-app') && !document.querySelector('#chat-app').hidden && !document.querySelector('#check-chatgpt').disabled", 'Remote workspace did not load');
  await click('#settings-button');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Remote status refresh unfinished');
  ok(await js("document.querySelector('#connect-chatgpt').hidden && document.querySelector('#add-chatgpt-account').hidden && !document.querySelector('#disconnect-chatgpt').hidden && !document.querySelector('#account-select').hidden && !document.querySelector('#remote-connection-help').hidden && !document.querySelector('#remote-connection-help').open && document.querySelector('#connection-status').hidden"), 'Ready remote connection keeps Disconnect visible and setup instructions collapsed without redundant ready copy');
  await expectEventually("!document.querySelector('#usage-panel').hidden && !document.querySelector('#usage-connect').disabled", 'Remote usage Connect unavailable');
  await click('#usage-connect');
  await expectEventually("!document.querySelector('#usage-device').hidden && !document.querySelector('#usage-cancel').disabled", 'Remote usage start did not show device code');
  ok(remoteCalls.some(c=>c.path==='/api/usage/connect' && c.method==='POST') && await js("!document.querySelector('#disconnect-dialog').open"), 'Remote usage device sign-in is allowed without inheriting localhost-only SIWC restrictions');
  await click('#usage-cancel');
  await expectEventually("!document.querySelector('#usage-connect').disabled", 'Remote usage cancel failed');
  await screenshot('agent-panel-remote-connected');
  for (const [width, height] of [[1280,850],[390,640],[320,640]]) {
    await viewport(width, height);
    await js("document.querySelector('#remote-connection-help').open=false; document.querySelector('#settings-panel').scrollTop=0");
    const restingDisconnect = await js("(() => { const r=document.querySelector('#disconnect-chatgpt').getBoundingClientRect(); return {x:r.x,y:r.y}; })()");
    ok(await js("(() => { const a=document.querySelector('#remote-connection-help > summary').getBoundingClientRect(); const b=document.querySelector('#disconnect-chatgpt').getBoundingClientRect(); return Math.abs(a.top-b.top)<1 && a.right<=b.left && a.height>=44; })()"), `Add or reconnect and Disconnect share one accessible row at ${width}px`);
    await click('#remote-connection-help > summary');

    ok(await js(`(() => { const r=document.querySelector('#disconnect-chatgpt').getBoundingClientRect(); return document.querySelector('#remote-connection-help').open && Math.abs(r.x-${restingDisconnect.x})<1 && Math.abs(r.y-${restingDisconnect.y})<1; })()`), `Expanding guidance leaves Disconnect stationary at ${width}px`);
    ok(await js("(() => { const p=document.querySelector('#settings-panel'); const a=document.querySelector('#remote-connection-help-slot').getBoundingClientRect(); const b=document.querySelector('#disconnect-chatgpt').getBoundingClientRect(); return a.top>=b.bottom && a.right>=b.right && p.scrollWidth<=p.clientWidth; })()"), `Expanded guidance spans beneath both actions without overflow at ${width}px`);
    await screenshot('agent-connection-action-row-' + width);
  }
  await viewport(1280,850);
  ok(await js("document.querySelectorAll('#remote-connection-setup ol > li').length===3 && document.querySelector('#remote-connection-setup').textContent.includes('same Agent account') && document.querySelector('#remote-connection-setup').textContent.includes('status pill')"), 'Remote setup provides the three approved steps and points to the refresh pill');
  ok(await js("document.querySelector('#remote-connection-action').textContent.includes('Add account')"), 'Connected setup guidance names the actual available localhost action');
  ok(await js("!document.querySelector('#local-connection-address').hidden && document.querySelector('#local-connection-url').textContent==='http://127.0.0.1:8182'"), 'Remote setup shows the configured callback origin, not the container port or remote host');
  await js("Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async value=>{window.copiedLocalURL=value;}}})");
  await click('#copy-local-connection-url');
  await expectEventually("window.copiedLocalURL==='http://127.0.0.1:8182' && document.querySelector('#provider-feedback').textContent==='Local address copied.'", 'Configured address was not copied');
  await js("navigator.clipboard.writeText=async()=>{throw new Error('Test clipboard rejection')}");
  await click('#copy-local-connection-url');
  await expectEventually("document.querySelector('#provider-feedback').textContent.includes('Select and copy the local address manually.')", 'Clipboard failure must offer manual copying');
  for (const [width, height] of [[1280,850],[390,640]]) {
    await viewport(width,height);
    ok(await js("document.documentElement.scrollWidth<=innerWidth && document.querySelector('#local-connection-address').getBoundingClientRect().right<=innerWidth"), `Configured local address fits ${width}x${height}`);
    await screenshot('agent-configured-local-address-' + width);
  }
  await viewport(1280,850);
  configuredLocalURL = '';
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Invalid config refresh stuck');
  ok(await js("document.querySelector('#local-connection-address').hidden && document.querySelector('#local-connection-url').textContent==='' && !document.querySelector('#local-connection-config-error').hidden"), 'Invalid configuration clears the stale address and shows an actionable warning');
  configuredLocalURL = 'http://127.0.0.1:9090';
  await click('#check-chatgpt');
  await expectEventually("document.querySelector('#local-connection-url').textContent==='http://127.0.0.1:9090' && !document.querySelector('#check-chatgpt').disabled", 'New configured address did not replace the old one');
  connectionStatusFailure = true;
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Status failure refresh stuck');
  ok(await js("document.querySelector('#local-connection-address').hidden && document.querySelector('#local-connection-url').textContent==='' && document.querySelector('#local-connection-config-error').hidden"), 'Status failure does not show a stale address or misreport a configuration error');
  await screenshot('agent-panel-unavailable');
  connectionStatusFailure = false;
  await click('#check-chatgpt');
  await expectEventually("!document.querySelector('#check-chatgpt').disabled", 'Remote status check stuck');
  ok(await js("!document.querySelector('#disconnect-dialog').open"), 'Remote status checks do not require password confirmation');
  await changeAccount('p1');
  await expectEventually("document.querySelector('#account-select').value==='p1' && !document.querySelector('#check-chatgpt').disabled", 'Remote password-confirmed switch did not finish');
  ok(activeProfile === 'p1' && remoteCalls.some(call => call.path === '/api/auth/profiles/active' && call.method === 'POST'), 'Remote password-confirmed switch uses the real UI request path');
  await click('#disconnect-chatgpt');
  await screenshot('agent-connection-confirmation-desktop');
  await viewport(390,640);
  ok(await js("document.querySelector('#disconnect-dialog').getBoundingClientRect().right<=innerWidth && document.querySelector('#confirm-disconnect').getBoundingClientRect().bottom<=innerHeight && document.documentElement.scrollWidth<=innerWidth"), 'Password confirmation fits the phone viewport with actions visible');
  await screenshot('agent-connection-confirmation-phone');
  await confirmChange();
  await expectEventually("document.querySelector('#disconnect-chatgpt').hidden && !document.querySelector('#check-chatgpt').disabled", 'Remote password-confirmed disconnect failed');
  ok(!providerProfiles.find(p => p.id === 'p1').connected && authenticated, 'Remote disconnect changes only the selected connection and preserves Agent sign-in');
  ok(await js("!document.querySelector('#local-connection-address').hidden && document.querySelector('#local-connection-url').textContent==='http://127.0.0.1:9090'"), 'Disconnected remote users retain the configured local setup address');
  ok(await js("!document.querySelector('#remote-connection-setup').hidden && !document.querySelector('#remote-connection-setup').closest('details') && document.querySelector('#provider-actions').hidden"), 'Disconnected remote setup shows the checklist directly without an empty action row');
  for (const [width,height] of [[1280,850],[390,640],[360,640]]) {
    await viewport(width,height);
    ok(await js("document.documentElement.scrollWidth<=innerWidth && document.documentElement.scrollHeight<=innerHeight && document.querySelector('#settings-panel').scrollWidth<=document.querySelector('#settings-panel').clientWidth"), `Remote setup remains inside the settings scroll pane at ${width}x${height}`);
    if (width < 600) ok(await js("document.querySelector('#check-chatgpt').getBoundingClientRect().height>=44"), 'Phone refresh pill retains a 44px touch target');
    await js("document.querySelector('#settings-panel').scrollTop=0");
    await screenshot('agent-panel-remote-setup-' + width);
    await js("document.querySelector('#settings-panel').scrollTop=document.querySelector('#settings-panel').scrollHeight");
    ok(await js("document.querySelector('#logout-button').getBoundingClientRect().bottom<=innerHeight"), 'Settings below the setup checklist remain reachable by scrolling');
  }
  ok(!remoteCalls.some(call => call.path === '/auth/start'), 'Remote management never starts an unsupported authorization flow');
  await screenshot('agent-settings-remote');
  await cdp('Page.navigate',{url:origin});
  await expectEventually("document.querySelector('#chat-app') && !document.querySelector('#chat-app').hidden && !document.querySelector('#check-chatgpt').disabled", 'Return to local workspace failed');
  await cdp('Fetch.disable');
  await viewport(1280,850); logoutFailure = true;
  await click('#settings-button'); await click('#logout-button');
  await expectEventually("!document.querySelector('#chat-app').hidden && !document.querySelector('#logout-button').disabled", 'Failed logout lost workspace');
  ok(authenticated, 'Failed sign-out preserves authenticated session');
  logoutFailure = false; await click('#logout-button');
  await expectEventually("!document.querySelector('#auth-gate').hidden", 'Successful sign-out did not return to login');
  const signedOutUsageCount=calls.filter(c=>c.path.startsWith('/api/usage/')).length;
  await new Promise(r=>setTimeout(r,2100));
  ok(calls.filter(c=>c.path.startsWith('/api/usage/')).length===signedOutUsageCount, 'Sign-out stops all usage polling');
  for (const [width,height] of [[1280,850],[390,844],[390,640]]) {
    await viewport(width,height);
    ok(await js('document.documentElement.scrollWidth<=innerWidth'), `Login fits width ${width}x${height}`);
    ok(await visible('#account-submit'), `Login submit remains reachable at ${width}x${height}`);
  }
  await screenshot('agent-login-phone');
  assert.deepEqual(runtimeErrors, [], 'No unhandled JavaScript errors');
  console.log('PASS all Focus / Quiet entry browser regressions (isolated API fixture)');
} finally {
  if (socket) socket.close();
  await stopTestBrowser(child);
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await rm(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 150 });
}

// Keep task-scoped consent/control coverage in the established UI test command.
await import('./research-ui.mjs');
await import('./research-settings-browser-selection.mjs');
await import('./research-settings-ui.mjs');
