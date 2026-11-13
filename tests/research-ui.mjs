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
let authenticated = true;
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
let researchSequence = 0;
const researchTasks = new Map();
let approveFailure = false, stopFailure = false, taskDelay = 0, stopDelay = 0, searchAvailable = true;
let finishChatBeforeControl = false, finishBlockedChat = null;
let approvalDelay = 0;
let approvalAckDelay = 0;

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
  if (url.pathname === '/api/research') { json(200, {available:true,search_available:searchAvailable,search_provider:'Fixture Search',scope:'public_web'}); return; }
  if (url.pathname === '/api/research/tasks') {
    let text=''; for await (const chunk of req) text+=chunk; call.payload=JSON.parse(text);
    const task={id:'task-'+(++researchSequence),status:'pending',expires_at:new Date(Date.now()+60000).toISOString(),search_provider:'Fixture Search',search_available:searchAvailable,scope:'public_web'};
    researchTasks.set(task.id, task); await new Promise(r=>setTimeout(r,taskDelay)); json(200,{task}); return;
  }
  if (url.pathname.startsWith('/api/research/tasks/')) {
    const [,id,action] = url.pathname.match(/tasks\/([^/]+)(?:\/(.*))?$/);
    const task=researchTasks.get(id);
    if (action === 'approve' && approveFailure) { json(409,{error:'Approval expired. Please retry.'}); return; }
    if (action === 'approve') {
      await new Promise(resolve => setTimeout(resolve, approvalDelay));
      if (task.status !== 'pending') { json(409,{error:'Task permission is no longer pending.'}); return; }
    }
    if (['stop','revoke'].includes(action) && finishBlockedChat) {
      task.status = action === 'stop' ? 'cancelled' : 'revoked';
      finishBlockedChat(); finishBlockedChat = null;
    }
    if (['stop','revoke'].includes(action)) await new Promise(r=>setTimeout(r,stopDelay));
    if (['stop','revoke'].includes(action) && stopFailure) { json(503,{error:'Fixture stop unavailable. Try again.'}); return; }
    if (action) task.status={approve:'approved',stop:'cancelled',revoke:'revoked'}[action];
    // Commit approval before delaying its acknowledgment: abort is not rollback.
    if (action === 'approve') await new Promise(resolve => setTimeout(resolve, approvalAckDelay));
    json(200,{task}); return;
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
    if (payload.research_task_id && finishChatBeforeControl) {
      await new Promise(resolve => { finishBlockedChat = () => { json(409,{error:'Research was stopped or its time limit expired.'}); resolve(); }; });
      return;
    }
    await new Promise(resolve => setTimeout(resolve, chatDelay));
    if (payload.run_id) {
      const run = fixtureRun(payload.run_id, chatFailure ? 'failed' : 'completed', payload.messages);
      if (payload.research_task_id) {
        run.research = {task_id:payload.research_task_id,scope:'public_web',consent_basis:'explicit_task_approval',history_policy:'final_user_message_only'};
        run.context.tools = [{type:'function',function:{name:'web_search'}},{type:'function',function:{name:'web_fetch'}}];
        run.model_calls = [
          {index:1,provider:'fixture',model:'test-one',state:'completed',request:{messages:[{role:'user',content:'initial research input'}]},response:{tool_calls:[{Name:'web_search',Arguments:'{"query":"fixture source"}'}]}},
          {index:2,provider:'fixture',model:'test-one',state:'completed',request:{messages:[{role:'tool',content:'retrieved fixture evidence'}]},response:{text:'fixture synthesis'}},
        ];
        run.tool_calls = [{id:'search-1',name:'web_search',state:'completed',consent_basis:'explicit_task_approval',arguments:'{"query":"fixture source"}',result:'<script>window.unsafeResearch=true</script> retrieved fixture evidence',truncated:true}];
        run.capture_omitted_fields = 1;
        run.events.push({name:'consent',detail:'Explicit task approval',at:run.started_at,state:'completed'});
      }
      runs.set(payload.run_id, run);
    }
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
  await expectEventually("!document.querySelector('#chat-app').hidden && !document.querySelector('#model-select').disabled", 'Workspace missing');
  ok(await js("!!document.querySelector('#research-toggle') && !document.querySelector('#research-toggle').checked"), 'Research is an explicit per-task opt-in, off by default');
  ok(!calls.some(c=>c.path.startsWith('/api/research')), 'Normal chat initialization creates no research tasks or approval requests');
  const prompt='<img src=x onerror=window.unsafeResearch=true> Find public sources';
  await js(`document.querySelector('#research-toggle').checked=true;document.querySelector('#message-input').value=${JSON.stringify(prompt)};document.querySelector('#chat-form').requestSubmit()`);
  await expectEventually("!document.querySelector('#research-consent').hidden", 'Inline task consent missing');
  await expectEventually("!document.querySelector('#research-approve').disabled", 'Task preparation missing');
  ok(calls.filter(c=>c.path==='/api/chat').length===0, 'No model request before explicit approval');
  ok(await js("document.querySelector('#research-toggle').disabled && document.querySelector('#model-select').disabled && document.querySelector('#settings-button').disabled"), 'Consent locks task identity and navigation while leaving Cancel usable');
  await click('#settings-button');
  ok(!(await visible('#settings-panel')) && await visible('#research-cancel'), 'Pending consent cannot disappear into Settings');
  ok(await js("document.querySelector('#research-consent').textContent.includes('Fixture Search') && !document.querySelector('#research-consent img') && !window.unsafeResearch"), 'Consent uses actual provider and escaped task preview');
  const prepared=structuredClone(calls.find(c=>c.path==='/api/research/tasks').payload);
  for (const [width,height] of [[1280,850],[390,640],[320,640]]) {
    await viewport(width,height);
    await js("document.querySelector('#research-consent').scrollTop=0");
    ok(await js("document.documentElement.scrollWidth<=innerWidth && document.documentElement.scrollHeight<=innerHeight && document.querySelector('#research-consent').scrollWidth<=document.querySelector('#research-consent').clientWidth && document.querySelector('#research-consent').getBoundingClientRect().top>=0"), `Consent fits the viewport at ${width}px`);
    ok(await js("document.querySelector('#chat-form').hidden || document.querySelector('#chat-form').getBoundingClientRect().bottom<=innerHeight"), `Pending composer is not clipped at ${width}px`);
    await js("document.querySelector('#research-cancel').focus()");
    await cdp('Input.dispatchKeyEvent',{type:'keyDown',key:'Tab',code:'Tab'});
    await cdp('Input.dispatchKeyEvent',{type:'keyUp',key:'Tab',code:'Tab'});
    ok(await js("document.activeElement.getClientRects().length>0 && !document.activeElement.disabled"), `Consent retains a real keyboard path at ${width}px`);
    if(width<600) {
      ok(await js("document.querySelector('#research-approve').getBoundingClientRect().height>=44"), 'Mobile consent actions have 44px touch targets');
      await js("document.querySelector('#research-consent').scrollTop=0");
      ok(await js("['research-approve','research-decline','research-cancel'].every(id=>document.getElementById(id).getBoundingClientRect().bottom<=document.querySelector('#research-consent').getBoundingClientRect().bottom)"), 'All mobile consent choices remain visible without scrolling the disclosure');
    }
    await js("document.querySelector('#research-consent').scrollTop=0");
    await screenshot('agent-research-consent-'+width);
  }
  await viewport(1280,850);
  await js("document.querySelector('#research-approve').focus()");
  await cdp('Input.dispatchKeyEvent',{type:'keyDown',key:'Enter',code:'Enter',windowsVirtualKeyCode:13,text:'\r',unmodifiedText:'\r'});
  await cdp('Input.dispatchKeyEvent',{type:'keyUp',key:'Enter',code:'Enter',windowsVirtualKeyCode:13});
  await expectEventually("!document.querySelector('#send-button').disabled", 'Approved task did not complete');
  const chat=calls.find(c=>c.path==='/api/chat').payload;
  assert.deepEqual(chat,{...prepared,research_task_id:'task-1'});
  ok(await js("document.querySelector('#research-consent').hidden && document.querySelector('#research-scope').hidden && !document.querySelector('#research-toggle').checked"), 'Completed research releases controls and resets opt-in');
  await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1).click()");
  await expectEventually("document.querySelector('#run-state').textContent==='Completed'", 'Research inspector missing');
  await click('#run-tab-context');
  ok(await js("document.querySelector('#run-context-values').textContent.includes('web_search') && !document.querySelector('#run-context-values').textContent.includes('[object Object]')"), 'Research inspector names actual structured tools');
  ok(await visible('#run-truncation'), 'Research capture omissions are disclosed outside the Model call tab');
  await click('#run-tab-model');
  ok(await js("document.querySelector('#run-view-model').textContent.includes('explicit_task_approval') && document.querySelector('#run-request pre').textContent.includes('initial research input') && document.querySelector('#run-request pre').textContent.includes('retrieved fixture evidence') && document.querySelector('#run-response pre').textContent.includes('fixture synthesis') && document.querySelector('#run-response pre').textContent.includes('web_search')"), 'Research inspector exposes consent, every model call, and tool results');
  ok(await js("!window.unsafeResearch && !document.querySelector('#run-view-model script')"), 'Research tool payloads remain inert text');
  await click('#run-close');
  chatDelay=5000;
  await js("document.querySelector('#research-toggle').checked=true;document.querySelector('#message-input').value='Find another public source';document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("!document.querySelector('#research-approve').disabled && !document.querySelector('#research-consent').hidden", 'Second consent missing');
  await click('#research-approve');
  await expectEventually("!document.querySelector('#research-scope').hidden && document.querySelector('#research-scope').dataset.taskId==='task-2'", 'Running task permission controls missing');
  ok(await js("document.querySelector('#research-scope').textContent.includes('Public web only') && !document.querySelector('#research-stop').disabled"), 'Active research has a visible scope and usable Stop');
  for (const [width,height] of [[1280,850],[390,640],[320,640]]) {
    await viewport(width,height);
    ok(await js("document.documentElement.scrollWidth<=innerWidth && document.documentElement.scrollHeight<=innerHeight && document.querySelector('#research-scope').scrollWidth<=document.querySelector('#research-scope').clientWidth && document.querySelector('#research-stop').getBoundingClientRect().bottom<=innerHeight"), `Live permission controls fit ${width}px`);
    await screenshot('agent-research-running-'+width);
  }
  await viewport(1280,850);
  stopFailure=true;
  await click('#research-stop');
  await expectEventually("document.querySelector('#research-scope').textContent.includes('Fixture stop unavailable')", 'Stop failure missing');
  ok(await js("!document.querySelector('#research-stop').disabled && !document.querySelector('#send-button').disabled===false"), 'Rejected Stop stays actionable and leaves execution locked');
  stopFailure=false;
  await click('#research-permissions');
  ok(await visible('#research-permission-details'), 'Permissions reveals task scope and prior-request warning');
  await click('#research-revoke');
  await expectEventually("!document.querySelector('#send-button').disabled && document.querySelector('#research-scope').hidden", 'Revoke did not release running request');
  ok(calls.some(c=>c.path==='/api/research/tasks/task-2/revoke') && await js("document.querySelector('#notice').textContent.toLowerCase().includes('revoked')"), 'Revoked notice follows server acknowledgment');
  chatDelay=0;
  const beforeDecline=calls.filter(c=>c.path==='/api/chat').length;
  await js("document.querySelector('#research-toggle').checked=true;document.querySelector('#message-input').value='Answer this without external requests';document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("!document.querySelector('#research-consent').hidden && !document.querySelector('#research-approve').disabled", 'Decline consent missing');
  const declinedRequest=structuredClone(calls.filter(c=>c.path==='/api/research/tasks').at(-1).payload);
  await click('#research-decline');
  await expectEventually("!document.querySelector('#send-button').disabled", 'Declined answer missing');
  assert.deepEqual(calls.filter(c=>c.path==='/api/chat').at(-1).payload,declinedRequest);
  ok(calls.filter(c=>c.path==='/api/chat').length===beforeDecline+1 && !calls.some(c=>c.path==='/api/research/tasks/task-3/approve'), 'Decline sends exactly one normal chat with the frozen payload and zero research authorization');
  const beforeCancel=calls.filter(c=>c.path==='/api/chat').length;
  taskDelay=300;
  await js("document.querySelector('#research-toggle').checked=true;document.querySelector('#message-input').value='Retain this cancelled draft';document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("!document.querySelector('#research-consent').hidden", 'Cancel consent missing');
  await click('#research-cancel');
  await new Promise(r=>setTimeout(r,400));
  ok(calls.filter(c=>c.path==='/api/chat').length===beforeCancel && await js("document.querySelector('#message-input').value==='Retain this cancelled draft' && !document.querySelector('#send-button').disabled && !document.querySelector('#research-toggle').checked"), 'Cancel during task preparation restores draft and never sends a model request');
  taskDelay=0;
  approveFailure=true;
  await js("document.querySelector('#research-toggle').checked=true;document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("!document.querySelector('#research-approve').disabled", 'Expired consent preparation missing');
  await click('#research-approve');
  await expectEventually("document.querySelector('#research-feedback').textContent.includes('expired')", 'Expired approval error missing');
  const expiredTaskCount=researchSequence;
  await new Promise(r=>setTimeout(r,150));
  ok(researchSequence===expiredTaskCount && calls.filter(c=>c.path==='/api/chat').length===beforeCancel && await visible('#research-retry'), 'Expired approval offers explicit retry without automatic reapproval or a duplicate prompt');
  ok(await js("document.activeElement.id==='research-retry'"), 'Failed approval moves keyboard focus to explicit retry');
  approveFailure=false;
  await click('#research-retry');
  await expectEventually("!document.querySelector('#research-approve').disabled", 'Explicit retry missing');
  await click('#research-cancel');
  await expectEventually("!document.querySelector('#send-button').disabled", 'Retry cancellation missing');
  await js("document.querySelector('#research-toggle').checked=true");
  await click('#new-chat');
  ok(await js("!document.querySelector('#research-toggle').checked"), 'New conversation never inherits an unsubmitted research opt-in');
  await js("document.querySelector('#research-toggle').checked=true");
  await session('A quieter week');
  await expectEventually("document.querySelector('#conversation-title').textContent==='A quieter week' && !document.querySelector('#send-button').disabled", 'Switch conversation failed');
  ok(await js("!document.querySelector('#research-toggle').checked"), 'Selecting a saved conversation resets the research opt-in');
  // Use a held chat response to verify Stop acknowledgment precedes local abort.
  chatDelay=5000;
  finishChatBeforeControl=true;
  await js("document.querySelector('#research-toggle').checked=true;document.querySelector('#message-input').value='Stop this public research task';document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("!document.querySelector('#research-consent').hidden && !document.querySelector('#research-approve').disabled", 'Stop consent missing');
  await click('#research-approve');
  await expectEventually("!document.querySelector('#research-scope').hidden", 'Stop running controls missing');
  stopDelay=300;
  await click('#research-stop');
  ok(await js("document.querySelector('#send-button').disabled && !document.querySelector('#research-scope').hidden && !document.querySelector('#notice').textContent.includes('Research stopped')"), 'Stop waits for server acknowledgment before local abort or a stopped claim');
  await expectEventually("!document.querySelector('#send-button').disabled && document.querySelector('#research-scope').hidden", 'Stop acknowledgment did not close execution controls');
  ok(await js("document.querySelector('#notice').textContent.includes('Research stopped')"), 'Successful server Stop produces a truthful stopped state');
  finishChatBeforeControl=false;
  chatDelay=0;
  searchAvailable=false;
  await js("document.querySelector('#research-toggle').checked=true;document.querySelector('#message-input').value='Read this public page only';document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("!document.querySelector('#research-consent').hidden && !document.querySelector('#research-approve').disabled", 'Retrieval-only consent missing');
  ok(await js("document.querySelector('#research-provider').textContent.includes('retrieval only') && document.querySelector('#research-provider').textContent.includes('not configured') && !document.querySelector('#research-provider').textContent.includes('Fixture Search') && document.querySelector('#research-prompt').textContent==='Read this public page only' && !document.querySelector('#research-prompt').textContent.includes('Plan a quiet week')"), 'Retrieval-only configuration never promises global search, and preview excludes earlier chat');
  await js("document.querySelector('#research-cancel').focus()");
  await cdp('Input.dispatchKeyEvent',{type:'keyDown',key:'Escape',code:'Escape'});
  await cdp('Input.dispatchKeyEvent',{type:'keyUp',key:'Escape',code:'Escape'});
  await expectEventually("document.querySelector('#research-consent').hidden && !document.querySelector('#send-button').disabled", 'Keyboard Escape did not cancel consent');
  chatDelay = 5000; stopDelay = 500;
  await js("document.querySelector('#research-toggle').checked=true;document.querySelector('#message-input').value='Allow a timed-out stop to be retried';document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("!document.querySelector('#research-consent').hidden && !document.querySelector('#research-approve').disabled", 'Timeout task preparation missing');
  await click('#research-approve');
  await expectEventually("!document.querySelector('#research-scope').hidden", 'Timeout task controls missing');
  await js("window.researchOriginalTimer=window.setTimeout;window.setTimeout=function(fn,ms,...args){return window.researchOriginalTimer(fn,ms===10000?40:ms,...args)}");
  await click('#research-stop');
  await until(() => js("document.querySelector('#research-control-feedback').textContent.toLowerCase().includes('timed out') && !document.querySelector('#research-stop').disabled"), 'Unacknowledged Stop must time out and offer a retry', 1500);
  ok(await js("document.querySelector('#send-button').disabled && !document.querySelector('#notice').textContent.includes('Research stopped')"), 'A timed-out Stop never claims cancellation or discards the active task');
  await js("window.setTimeout=window.researchOriginalTimer;delete window.researchOriginalTimer");
  await click('#research-stop');
  await expectEventually("!document.querySelector('#send-button').disabled", 'Stop retry did not complete');
  chatDelay = 0; stopDelay = 0;
  const beforeRacedCancel = calls.filter(call => call.path === '/api/chat').length;
  approvalDelay = 300;
  await js("document.querySelector('#research-toggle').checked=true;document.querySelector('#message-input').value='Cancel while approval is in flight';document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("!document.querySelector('#research-consent').hidden && !document.querySelector('#research-approve').disabled", 'Concurrent approval task missing');
  const racedTaskID = 'task-' + researchSequence;
  await click('#research-approve');
  await click('#research-cancel');
  await expectEventually("document.querySelector('#research-consent').hidden && !document.querySelector('#send-button').disabled", 'Concurrent cancellation did not finish');
  await new Promise(resolve => setTimeout(resolve, 400));
  ok(researchTasks.get(racedTaskID).status === 'revoked' && calls.filter(call => call.path === '/api/chat').length === beforeRacedCancel, 'Cancel during in-flight approval revokes the server grant and sends no model request');
  // A timed-out approval response may already have committed the server grant.
  async function loseApprovalAck(label) {
    approvalDelay = 0; approvalAckDelay = 500;
    await js(`document.querySelector('#research-toggle').checked=true;document.querySelector('#message-input').value=${JSON.stringify(label)};document.querySelector('#chat-form').requestSubmit()`);
    await expectEventually("!document.querySelector('#research-consent').hidden && !document.querySelector('#research-approve').disabled", 'Lost-ack task missing');
    const id = 'task-' + researchSequence;
    await js("window.researchOriginalTimer=window.setTimeout;window.setTimeout=function(fn,ms,...args){return window.researchOriginalTimer(fn,ms===10000?40:ms,...args)}");
    await click('#research-approve');
    await expectEventually("document.querySelector('#research-feedback').textContent.includes('timed out') && !document.querySelector('#research-retry').hidden", 'Lost approval acknowledgment must offer explicit retry');
    await js("window.setTimeout=window.researchOriginalTimer;delete window.researchOriginalTimer");
    assert.equal(researchTasks.get(id).status, 'approved', 'Fixture must commit approval before response timeout');
    return id;
  }
  const beforeLostCancel = calls.filter(call => call.path === '/api/chat').length;
  const lostCancelID = await loseApprovalAck('Keep this draft after lost approval acknowledgment');
  await click('#research-cancel');
  await expectEventually("document.querySelector('#research-consent').hidden && !document.querySelector('#send-button').disabled", 'Lost-ack Cancel did not finish');
  ok(researchTasks.get(lostCancelID).status === 'revoked' && calls.filter(call => call.path === '/api/chat').length === beforeLostCancel, 'Cancel after lost approval acknowledgment revokes the committed grant without model requests');
  for (const action of ['cancel','decline','retry']) {
    for (const failure of ['error','timeout']) {
      const before = calls.length;
      const id = await loseApprovalAck('Lost-ack ' + action + ' ' + failure);
      const count = researchSequence;
      const actionSelector = '#research-' + action;
      stopFailure = failure === 'error'; stopDelay = failure === 'timeout' ? 500 : 0;
      if (failure === 'timeout') await js("window.researchOriginalTimer=window.setTimeout;window.setTimeout=function(fn,ms,...args){return window.researchOriginalTimer(fn,ms===10000?40:ms,...args)}");
      await click(actionSelector);
      const failureText = failure === 'error' ? 'Fixture stop unavailable' : 'timed out';
      await expectEventually(`document.querySelector('#research-feedback').textContent.includes(${JSON.stringify(failureText)}) && ${action === 'retry' ? "!document.querySelector('#research-retry').hidden && !document.querySelector('#research-feedback').textContent.includes('review a new permission')" : "document.querySelector('#research-feedback').textContent.includes('close this permission')"}`, 'Failed revocation must remain visible for ' + action);
      if (failure === 'timeout') await js("window.setTimeout=window.researchOriginalTimer;delete window.researchOriginalTimer");
      ok(await visible('#research-consent') && researchSequence === count && !calls.slice(before).some(call => call.path === '/api/chat'), action + ' keeps the known task/card and sends no model request after revoke ' + failure);
      assert.equal(researchTasks.get(id).status, 'approved', 'Unconfirmed revocation must not discard the approved task');
      stopFailure = false; stopDelay = 200;
      await click(actionSelector);
      await until(() => Promise.resolve(calls.filter(call => call.path === '/api/research/tasks/' + id + '/revoke').length === 2), 'Retry must revoke the original immutable task');
      if (action === 'retry') {
        // Programmatic activation and Escape must not overlap replacement.
        await click('#research-retry'); await click('#research-cancel'); await click('#research-decline');
        await js("document.querySelector('#research-consent').dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))");
        assert.equal(researchSequence, count, 'Replacement must wait for confirmed revocation');
        await expectEventually("!document.querySelector('#research-approve').disabled", 'Retry must prepare a fresh pending card');
        ok(researchTasks.get(id).status === 'revoked' && researchSequence === count + 1 && researchTasks.get('task-' + researchSequence).status === 'pending' && !calls.slice(before).some(call => call.path === '/api/chat'), 'Retry confirms old revocation before one fresh pending task without approval/model reuse');
        stopDelay = 0;
        await click('#research-cancel');
      }
      await expectEventually("document.querySelector('#research-consent').hidden && !document.querySelector('#send-button').disabled", action + ' recovery must finish');
      assert.equal(researchTasks.get(id).status, 'revoked');
      const sent = calls.slice(before).filter(call => call.path === '/api/chat');
      ok(sent.length === (action === 'decline' ? 1 : 0) && sent.every(call => !call.payload.research_task_id), action + ' recovery revokes the old grant and never submits a research model request');
      stopDelay = 0;
    }
  }
  assert.deepEqual(runtimeErrors, []);
  console.log('PASS isolated research consent/control browser regressions (no live provider calls)');
} finally {
  if (socket) socket.close();
  await stopTestBrowser(child);
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await rm(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 150 });
}
