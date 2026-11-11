// Dependency-free browser regression test for the actual embedded production page.
// Run with Node 22+ and UI_BROWSER pointing to a Chromium executable.
// All API data is confined to this local test server, never production storage.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
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
const html = htmlSource.slice(htmlSource.indexOf('`') + 1, htmlSource.lastIndexOf('`'));
const profile = await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'agent-ui-'));
let authenticated = false;
let authFailure = true;
let listFailure = false;
let loadFailure = false;
let chatFailure = false;
let logoutFailure = false;
let chatDelay = 0;
let loadDelay = 0;
let activeProfile = 'p1';
let profileFailure = false;
let disconnectFailure = false;
const calls = [];
let conversations = [
  { id: 'one', title: 'A quieter week', updated_at: Math.floor(Date.now() / 1000) },
  { id: 'two', title: 'Dinner ideas', updated_at: Math.floor(Date.now() / 1000) - 86400 },
  { id: 'unsafe', title: '<img src=x onerror=alert(1)>', updated_at: 1 },
];
const history = new Map([
  ['one', [{ role: 'user', content: 'Plan a quiet week.' }, { role: 'assistant', content: 'Leave two evenings open.' }]],
  ['two', [{ role: 'user', content: 'Suggest dinner.' }, { role: 'assistant', content: 'Try rice and roasted vegetables.' }]],
  ['unsafe', [{ role: 'assistant', content: '<script>window.unexpectedExecution=true</script>' }]],
]);
const server = createServer(async (req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const call = { path: url.pathname, method: req.method };
  calls.push(call);
  const json = (status, body) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
  if (url.pathname === '/') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(html); return; }
  if (url.pathname === '/favicon.ico') { res.writeHead(204); res.end(); return; }
  if (['/api/auth/login', '/api/auth/register'].includes(url.pathname)) {
    // Drain but never record credentials, even though test values are synthetic.
    for await (const _ of req) {}
    if (authFailure) { json(401, { error: 'Test sign-in rejected. Please try again.' }); return; }
    authenticated = true; json(200, { id: 'demo-user', username: 'Browser Test' }); return;
  }
  if (url.pathname === '/api/auth/me') { json(authenticated ? 200 : 401, authenticated ? { id: 'demo-user', username: 'Browser Test' } : { error: 'Sign in to continue.' }); return; }
  if (!authenticated) { json(401, { error: 'Sign in to continue.' }); return; }
  if (url.pathname === '/api/auth/logout') {
    if (logoutFailure) { json(500, { error: 'Test sign-out failure.' }); return; }
    authenticated = false; json(200, {}); return;
  }
  if (url.pathname === '/api/auth/status') { json(200, { connected: true, oauth_selected: true, plan_usage_enabled: true }); return; }
  if (url.pathname === '/api/auth/profiles') { json(200, { active_profile_id: activeProfile, profiles: [{ id: 'p1', label: 'Personal', plan_usage_enabled: true, oauth_selected: true }, { id: 'p2', label: 'Other', plan_usage_enabled: true, oauth_selected: true }] }); return; }
  if (url.pathname === '/api/auth/profiles/active') {
    let text = ''; for await (const chunk of req) text += chunk;
    if (profileFailure) { json(500, { error: 'Test profile change failed.' }); return; }
    activeProfile = JSON.parse(text).profile_id;
    json(200, {}); return;
  }
  if (url.pathname === '/api/auth/disconnect') { json(disconnectFailure ? 500 : 200, disconnectFailure ? { error: 'Test disconnect failure.' } : { remote_revocation_confirmed: true }); return; }
  if (url.pathname === '/api/models') { json(200, { models: [{ id: 'test-one', name: 'Test model one' }, { id: 'test-two', name: 'Test model two' }] }); return; }
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
    await new Promise(resolve => setTimeout(resolve, chatDelay));
    if (chatFailure) { json(429, { error: 'Test plan allowance reached.', action: 'manage_usage' }); return; }
    const id = payload.conversation_id || 'created';
    const message = payload.messages.at(-1);
    const reply = 'A real UI response from the isolated test fixture.';
    history.set(id, [...(history.get(id) || []), message, { role: 'assistant', content: reply }]);
    if (!conversations.some(item => item.id === id)) conversations.unshift({ id, title: message.content, updated_at: Math.floor(Date.now() / 1000) });
    json(200, { reply, conversation_id: id }); return;
  }
  json(404, { error: 'Unexpected test endpoint: ' + url.pathname });
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = 'http://127.0.0.1:' + server.address().port;
const child = spawn(browserPath, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--remote-debugging-port=0', '--user-data-dir=' + profile, 'about:blank'], { stdio: 'ignore' });
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
const expectEventually = (expression, message) => until(() => js(expression), message);
async function viewport(width, height) {
  await cdp('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: width < 600 });
}
async function screenshot(name) {
  if (!process.env.UI_SCREENSHOTS) return;
  const { data } = await cdp('Page.captureScreenshot', { format: 'png' });
  await writeFile(join(process.env.UI_SCREENSHOTS, name + '.png'), Buffer.from(data, 'base64'));
}
function ok(condition, message) { assert.ok(condition, message); console.log('PASS ' + message); }
try {
  const portText = await until(async () => { try { return await readFile(join(profile, 'DevToolsActivePort'), 'utf8'); } catch { return ''; } }, 'Browser did not start');
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
  ok(await js("document.querySelector('#current-username').textContent==='Browser Test'"), 'Bottom-left profile shows authenticated user');
  ok(!(await js("!!document.querySelector('#conversation-list img')")), 'Conversation titles are text, not injected HTML');
  await session('A quieter week');
  await expectEventually("document.querySelector('#messages').textContent.includes('Leave two evenings open.')", 'Saved conversation did not load');
  ok(await js("document.querySelector('.message.assistant .sender').textContent==='Agent' && document.querySelector('.message.user .sender').textContent==='You'"), 'Saved messages display Agent without changing the user label');
  ok(await js("document.querySelector('#conversation-title').textContent==='A quieter week' && !!document.querySelector('#conversation-list [aria-current=\"true\"]')"), 'Saved-session selection updates title and current row');
  await js("const search=document.querySelector('#conversation-search');search.value='dinner';search.dispatchEvent(new Event('input'))");
  ok(await js("Array.from(document.querySelectorAll('#conversation-list button')).filter(b=>b.getClientRects().length).length===1"), 'Session search filters the sidebar');
  await js("document.querySelector('#conversation-search').value='nothingmatches';document.querySelector('#conversation-search').dispatchEvent(new Event('input'))");
  ok(await js("document.querySelector('#conversation-list').textContent.toLowerCase().includes('no')"), 'Empty search has feedback');
  await js("document.querySelector('#conversation-search').value='';document.querySelector('#conversation-search').dispatchEvent(new Event('input'))");
  await click('#account-menu-toggle');
  ok(await visible('#account-menu'), 'Account menu exposes preserved provider controls');
  ok(await visible('#account-select'), 'Connected provider account selector remains available');
  profileFailure = true;
  await js("document.querySelector('#account-select').value='p2';document.querySelector('#account-select').dispatchEvent(new Event('change'))");
  await expectEventually("!document.querySelector('#provider-error').hidden && !document.querySelector('#account-select').disabled", 'Provider change failure not recoverable');
  ok(await visible('#provider-error'), 'Provider selection failure stays next to account controls');
  profileFailure = false;
  await js("document.querySelector('#account-select').value='p2';document.querySelector('#account-select').dispatchEvent(new Event('change'))");
  await expectEventually("document.querySelector('#account-select').value==='p2' && !document.querySelector('#account-select').disabled", 'Provider selection retry failed');
  ok(activeProfile === 'p2', 'Provider selection retry refreshes the actual selected account');
  disconnectFailure = true;
  await click('#disconnect-chatgpt');
  await expectEventually("!document.querySelector('#provider-error').hidden && !document.querySelector('#disconnect-chatgpt').disabled", 'Disconnect failure not recoverable');
  ok(await visible('#disconnect-chatgpt'), 'Failed provider disconnect preserves the retry action');
  disconnectFailure = false;
  await click('#account-menu-toggle');
  await click('#new-chat');
  ok(await visible('#welcome'), 'New conversation resets the conversation view');
  chatDelay = 250;
  await js("document.querySelector('#message-input').value='A test conversation';document.querySelector('#model-select').value='test-two';document.querySelector('#model-select').dispatchEvent(new Event('change'));document.querySelector('#chat-form').requestSubmit()");
  await expectEventually("document.querySelector('#send-button').disabled", 'Send loading state missing');
  await click('#new-chat');
  await session('Dinner ideas');
  await expectEventually("document.querySelector('#messages').textContent.includes('A real UI response')", 'Chat response missing');
  await expectEventually("document.querySelector('#conversation-title').textContent==='A test conversation'", 'New chat title did not update');
  const chatCall = calls.findLast(call => call.path === '/api/chat');
  ok(chatCall.payload.model === 'test-two' && chatCall.payload.conversation_id === '', 'Composer sends selected model and correct conversation identity');
  ok(await js("!!document.querySelector('#conversation-list [aria-current=\"true\"]')"), 'Newly saved chat is selected in session history');
  await cdp('Page.reload');
  await expectEventually("document.querySelector('#model-select').value==='test-two'", 'Model preference missing after reload');
  await session('A test conversation');
  await expectEventually("document.querySelector('#messages').textContent.includes('A real UI response')", 'Saved messages missing after reload');
  ok(true, 'Model preference and saved conversation survive page reload');
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
  ok(await js("document.querySelector('#notice a').href==='https://chatgpt.com/settings/usage'"), 'Provider usage errors retain the actionable usage link');
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
  await viewport(1280,850); logoutFailure = true;
  await click('#account-menu-toggle'); await click('#logout-button');
  await expectEventually("!document.querySelector('#chat-app').hidden && !document.querySelector('#logout-button').disabled", 'Failed logout lost workspace');
  ok(authenticated, 'Failed sign-out preserves authenticated session');
  logoutFailure = false; await click('#logout-button');
  await expectEventually("!document.querySelector('#auth-gate').hidden", 'Successful sign-out did not return to login');
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
  child.kill();
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await new Promise(resolve => child.exitCode !== null ? resolve() : child.once('exit', resolve));
  await rm(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 150 });
}
