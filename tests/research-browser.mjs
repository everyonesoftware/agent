// Production Agent server + SQLite + Chromium. Model HTTP is an isolated fixture;
// this test makes no live inference and uses no real keys. Public requests are
// disabled unless RESEARCH_WEB_LIVE=1 explicitly enables example.com retrieval.
// Build Agent first, then: node tests/research-browser.mjs <executable>
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

assert.equal(process.argv.length, 3, 'Supply a built Agent executable');
const executable = resolve(process.argv[2]);
assert.ok(existsSync(executable), 'Agent executable missing');
const browserPath = process.env.UI_BROWSER || [
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  '/usr/bin/google-chrome', '/usr/bin/chromium', '/usr/bin/chromium-browser',
].find(existsSync);
assert.ok(browserPath, 'Set UI_BROWSER to an installed Chromium executable');
const directory = await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'agent-research-'));
const profile = join(directory, 'browser');
const reply = 'Isolated research fixture response, not live model inference.';
const providerRequests = [];
let blockProvider = false, releaseProvider, liveToolMode = false;
const provider = createServer(async (req, res) => {
  assert.equal(req.url, '/v1/chat/completions');
  let text = ''; for await (const chunk of req) text += chunk;
  const payload = JSON.parse(text);
  providerRequests.push(payload);
  if (liveToolMode && payload.messages.at(-1).role === 'user') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({choices:[{message:{role:'assistant',content:null,tool_calls:[{id:'live-public-page',type:'function',function:{name:'web_fetch',arguments:'{"url":"https://example.com/"}'}}]},finish_reason:'tool_calls'}]}));
    return;
  }
  if (blockProvider) await new Promise(resolve => { releaseProvider = resolve; });
  if (res.destroyed) return;
  res.writeHead(200, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({ choices: [{ message: { role: 'assistant', content: reply }, finish_reason: 'stop' }], usage: { prompt_tokens: 20, completion_tokens: 10, total_tokens: 30 } }));
});
await new Promise(resolve => provider.listen(0, '127.0.0.1', resolve));
const probe = createServer();
await new Promise(resolve => probe.listen(0, '127.0.0.1', resolve));
const port = probe.address().port;
await new Promise(resolve => probe.close(resolve));
const origin = 'http://127.0.0.1:' + port;
let server, browser, socket, nextID = 0;
const pending = new Map(), runtimeErrors = [];
async function until(fn, message, timeout = 20000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await fn()) return;
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error(message);
}
async function stop(child) {
  if (!child || child.exitCode !== null) return;
  await new Promise(resolve => { child.once('exit', resolve); child.kill(); });
}
function cdp(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++nextID;
    const timer = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); }, 20000);
    pending.set(id, { resolve, reject, timer }); socket.send(JSON.stringify({ id, method, params }));
  });
}
async function js(expression) {
  const result = await cdp('Runtime.evaluate', { expression: '(async () => { return ' + expression + '; })()', awaitPromise: true, returnByValue: true });
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
  return result.result.value;
}
const eventual = (expression, message) => until(() => js(`(() => { try { return (${expression}); } catch { return false; } })()`), message);
const click = selector => js(`document.querySelector(${JSON.stringify(selector)}).click()`);
async function approve() {
  await eventual("!document.querySelector('#research-approve').disabled", 'Task approval did not become ready');
  await click('#research-approve');
}
const api = (path, body, method = 'POST') => js(`fetch(${JSON.stringify(path)}, {method:${JSON.stringify(method)},headers:{'Content-Type':'application/json'},${method === 'GET' ? '' : 'body:JSON.stringify(' + JSON.stringify(body) + '),'}cache:'no-store'}).then(async r=>({status:r.status,cache:r.headers.get('Cache-Control'),body:await r.json()}))`);
async function screenshot(name) {
  if (!process.env.UI_SCREENSHOTS) return;
  await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
  const { data } = await cdp('Page.captureScreenshot', { format: 'png' });
  await writeFile(join(process.env.UI_SCREENSHOTS, name + '.png'), Buffer.from(data, 'base64'));
}
async function compose(content, research = true) {
  await eventual("!document.querySelector('#send-button').disabled", 'Composer did not become ready');
  await js(`(() => { document.querySelector('#message-input').value=${JSON.stringify(content)}; const toggle=document.querySelector('#research-toggle'); toggle.checked=${research}; toggle.dispatchEvent(new Event('change',{bubbles:true})); document.querySelector('#chat-form').requestSubmit(); })()`);
}
const pass = message => console.log('PASS ' + message);
try {
  server = spawn(executable, [], {
    env: { ...process.env, PORT: String(port), TZ: 'UTC', APP_DATA_DIR: join(directory, 'data'), SIWC_DATA_DIR: join(directory, 'data'),
      LLM_BASE_URL: 'http://127.0.0.1:' + provider.address().port + '/v1', LLM_MODEL: 'fixture-model', LLM_API_KEY: '', BRAVE_SEARCH_API_KEY: '' },
    stdio: 'ignore',
  });
  await until(async () => { try { return (await fetch(origin + '/healthz')).ok; } catch { return false; } }, 'Agent did not start');
  browser = spawn(browserPath, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--remote-debugging-port=0', '--user-data-dir=' + profile, 'about:blank'], { stdio: 'ignore' });
  let portText;
  await until(async () => { try { portText = await readFile(join(profile, 'DevToolsActivePort'), 'utf8'); return true; } catch { return false; } }, 'Browser did not start');
  const targets = await (await fetch('http://127.0.0.1:' + portText.split('\n')[0] + '/json/list')).json();
  socket = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
  socket.onmessage = event => {
    const data = JSON.parse(event.data);
    if (data.method === 'Runtime.exceptionThrown') runtimeErrors.push(data.params.exceptionDetails.exception?.description || data.params.exceptionDetails.text);
    const job = pending.get(data.id); if (!job) return;
    pending.delete(data.id); clearTimeout(job.timer);
    if (data.error) job.reject(new Error(data.error.message)); else job.resolve(data.result);
  };
  await cdp('Runtime.enable'); await cdp('Page.enable');
  await cdp('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
  await cdp('Page.navigate', { url: origin });
  await eventual("!!document.querySelector('#account-form')", 'Login page missing');
  assert.equal((await api('/api/auth/register', { username: 'research-browser', password: 'synthetic-research-only' })).status, 201);
  await cdp('Page.reload');
  await eventual("document.querySelector('#model-select')?.value==='fixture-model' && !document.querySelector('#chat-app').hidden", 'Authenticated model selection missing');
  assert.ok(await js("!!document.querySelector('#research-toggle')"), 'Research requires an explicit opt-in control');
  assert.equal(await js("document.querySelector('#research-toggle').checked"), false);
  const config = await api('/api/research', null, 'GET');
  assert.equal(config.status, 200); assert.equal(config.cache, 'no-store');
  assert.equal(config.body.search_available, false, 'Fixture does not configure a search key');
  pass('Explicit research opt-in defaults off; unconfigured search is reported honestly');

  await compose('Read https://example.com and explain its purpose.');
  await eventual("!document.querySelector('#research-consent').hidden", 'Inline task consent missing');
  assert.equal(providerRequests.length, 0, 'No model or web request before approval');
  assert.match(await js("document.querySelector('#research-consent').textContent"), /earlier|previous|history/i);
  await screenshot('agent-research-consent-desktop');
  await cdp('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
  assert.ok(await js("document.documentElement.scrollWidth<=innerWidth"), 'Phone consent overflows horizontally');
  assert.ok(await js("['research-approve','research-decline','research-cancel'].every(id=>{const b=document.getElementById(id).getBoundingClientRect();return b.width>0 && b.left>=0 && b.right<=innerWidth;})"));
  await screenshot('agent-research-consent-phone');
  await click('#research-cancel');
  await eventual("document.querySelector('#research-consent').hidden && !document.querySelector('#send-button').disabled", 'Cancel did not return to composer');
  assert.equal(providerRequests.length, 0); assert.match(await js("document.querySelector('#message-input').value"), /example\.com/);
  pass('Desktop/phone consent fits; cancellation preserves the task and makes zero model requests');

  await compose('Answer this without external research.');
  await eventual("!document.querySelector('#research-consent').hidden", 'Second task consent missing');
  await click('#research-decline');
  await eventual("!document.querySelector('#send-button').disabled && document.querySelector('#messages').textContent.includes('Isolated research fixture response')", 'Declined research did not fall back to normal chat');
  assert.equal(providerRequests.length, 1); assert.ok(!providerRequests[0].tools?.length, 'Declining must not provide model tools');
  pass('Answer without browsing uses the normal no-tool provider path');

  await compose('Only research the public URL https://example.com.');
  await eventual("!document.querySelector('#research-consent').hidden", 'Approval card missing');
  await approve();
  await eventual("!document.querySelector('#send-button').disabled && document.querySelector('#research-scope').hidden", 'Approved research did not finish');
  assert.equal(providerRequests.length, 2);
  assert.ok(providerRequests[1].tools?.some(t => t.function?.name === 'web_fetch'), 'Approved task must expose a real public retrieval tool');
  assert.ok(!providerRequests[1].tools?.some(t => t.function?.name === 'web_search'), 'Unconfigured search tool must not be advertised');
  assert.ok(!JSON.stringify(providerRequests[1]).includes('Answer this without external research.'), 'Earlier chat must not enter research context');
  const lastID = await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1).dataset.runId");
  const trace = await api('/api/runs/' + lastID, null, 'GET');
  assert.equal(trace.status, 200); assert.equal(trace.body.run.state, 'completed');
  assert.match(JSON.stringify(trace.body.run.events), /consent|permission|approv/i, 'Trace must explain why research was permitted');
  await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1).click()");
  await eventual("!document.querySelector('#run-inspector').hidden", 'Research trace did not open');
  await screenshot('agent-research-trace-phone');
  await click('#run-close');
  pass('Approved research uses actual tool schemas, excludes history, and records consent in its persisted trace');

  // Exercise approval matching and ownership through the production HTTP router.
  const immutable = { messages: [{ role: 'user', content: 'Public task https://example.com' }], model: 'fixture-model', conversation_id: '', run_id: 'a2222222-2222-4222-8222-222222222222', omitted_messages: 0 };
  const created = await api('/api/research/tasks', immutable);
  assert.equal(created.status, 201);
  const taskID = created.body.task.id;
  const beforeSecurity = providerRequests.length;
  const unapproved = await api('/api/chat', { ...immutable, research_task_id: taskID });
  assert.ok(unapproved.status >= 400); assert.equal(providerRequests.length, beforeSecurity);
  const stranger = await fetch(origin + '/api/auth/register', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: 'research-stranger', password: 'synthetic-stranger-only' }) });
  assert.equal(stranger.status, 201);
  const cookie = stranger.headers.get('set-cookie').split(';')[0];
  for (const [suffix, method] of [['', 'GET'], ['/approve', 'POST'], ['/stop', 'POST'], ['/revoke', 'POST']]) {
    const foreign = await fetch(origin + '/api/research/tasks/' + taskID + suffix, { method, headers: { Cookie: cookie, 'Content-Type': 'application/json' }, ...(method === 'POST' ? { body: '{}' } : {}) });
    assert.equal(foreign.status, 404, 'Foreign task access must look missing: ' + suffix);
  }
  assert.equal((await api('/api/research/tasks/' + taskID + '/approve', {})).status, 200);
  const changed = await api('/api/chat', { ...immutable, messages: [{ role: 'user', content: 'Changed task must not reuse consent' }], research_task_id: taskID });
  assert.ok(changed.status >= 400); assert.equal(providerRequests.length, beforeSecurity);
  const authorized = await api('/api/chat', { ...immutable, research_task_id: taskID });
  assert.equal(authorized.status, 200); assert.equal(providerRequests.length, beforeSecurity + 1);
  const replay = await api('/api/chat', { ...immutable, research_task_id: taskID });
  assert.ok(replay.status >= 400); assert.equal(providerRequests.length, beforeSecurity + 1);
  pass('Real router enforces approval, immutable task binding, single-use grants and user isolation');

  // Drop an acknowledgment in this browser only, after the REAL server commits
  // approval. Read back the actual grant to verify recovery, not fixture state.
  for (const action of ['cancel', 'decline', 'retry']) {
    const beforeLostAck = providerRequests.length;
    await js(`(() => {
      const original = window.fetch;
      window.researchLostAck = {original,created:[],taskID:'',dropApproval:true,failRevoke:true};
      window.fetch = async function(input, init) {
        const state = window.researchLostAck;
        const path = new URL(typeof input === 'string' ? input : input.url, location.href).pathname;
        if (path.endsWith('/revoke') && state.failRevoke) {
          state.failRevoke = false;
          return new Response(JSON.stringify({error:'Fixture revocation unavailable; retry.'}),{status:503,headers:{'Content-Type':'application/json'}});
        }
        const response = await original.call(this,input,init);
        if (path === '/api/research/tasks' && response.ok) state.created.push((await response.clone().json()).task.id);
        if (path.endsWith('/approve') && response.ok && state.dropApproval) {
          state.dropApproval = false;
          state.taskID = (await response.clone().json()).task.id;
          throw new TypeError('Fixture lost approval acknowledgment after server commit.');
        }
        return response;
      };
    })()`);
    await compose('Recover a lost approval acknowledgment using ' + action + ' https://example.com/.');
    await eventual("!document.querySelector('#research-consent').hidden", 'Lost-ack consent missing');
    await approve();
    await eventual("!document.querySelector('#research-retry').hidden && !window.researchLostAck.dropApproval", 'Lost-ack recovery did not appear');
    const lostID = await js('window.researchLostAck.taskID');
    assert.equal((await api('/api/research/tasks/' + lostID, null, 'GET')).body.task.status, 'approved', 'The simulated lost acknowledgment must follow a real committed approval');
    const target = '#research-' + action;
    await click(target);
    await eventual("!window.researchLostAck.failRevoke || document.querySelector('#research-consent').hidden || window.researchLostAck.created.length>1", 'Recovery action did not settle');
    assert.equal(await js("document.querySelector('#research-consent').hidden"), false, 'Failed revocation must keep the consent card open');
    assert.equal(await js('window.researchLostAck.created.length'), 1, 'Failed revocation must not replace the grant');
    assert.equal(providerRequests.length, beforeLostAck, 'Failed revocation must not start model work');
    assert.equal((await api('/api/research/tasks/' + lostID, null, 'GET')).body.task.status, 'approved');
    await eventual("document.querySelector('#research-feedback').textContent.includes('Fixture revocation unavailable')", 'Failed revocation must remain actionable');
    await eventual(`!document.querySelector(${JSON.stringify(target)}).disabled`, 'Recovery action remained locked');
    await click(target);
    if (action === 'retry') {
      await eventual("window.researchLostAck.created.length===2 && !document.querySelector('#research-approve').disabled", 'Explicit retry did not prepare a fresh pending task');
      assert.equal((await api('/api/research/tasks/' + lostID, null, 'GET')).body.task.status, 'revoked', 'Retry must revoke the old grant before replacing it');
      const replacementID = await js('window.researchLostAck.created[1]');
      assert.equal((await api('/api/research/tasks/' + replacementID, null, 'GET')).body.task.status, 'pending');
      await click('#research-cancel');
    }
    await eventual("document.querySelector('#research-consent').hidden && !document.querySelector('#send-button').disabled", 'Lost-ack recovery did not return to chat');
    assert.equal((await api('/api/research/tasks/' + lostID, null, 'GET')).body.task.status, 'revoked');
    assert.equal(providerRequests.length, beforeLostAck + (action === 'decline' ? 1 : 0));
    if (action === 'decline') assert.ok(!providerRequests.at(-1).tools?.length, 'Lost-ack decline must remain ordinary no-tool chat');
    await js('(() => {window.fetch=window.researchLostAck.original;delete window.researchLostAck;})()');
  }
  pass('Lost approval acknowledgments retain real grants until confirmed revocation; failed Cancel/decline/Retry remains recoverable');

  if (process.env.RESEARCH_WEB_LIVE === '1') {
    const beforeLive = providerRequests.length;
    liveToolMode = true;
    await compose('Read the public page https://example.com/ and explain its purpose.');
    await eventual("!document.querySelector('#research-consent').hidden", 'Live retrieval task consent missing');
    await approve();
    await eventual("!document.querySelector('#send-button').disabled && document.querySelector('#research-scope').hidden", 'Live public-page retrieval did not finish');
    liveToolMode = false;
    assert.equal(providerRequests.length, beforeLive + 2, 'A tool call must be followed by model synthesis');
    const continuation = providerRequests.at(-1).messages.at(-1);
    assert.equal(continuation.role, 'tool');
    const page = JSON.parse(continuation.content);
    assert.equal(page.url, 'https://example.com/');
    assert.match(page.title, /Example Domain/); assert.match(page.content, /documentation/);
    const liveID = await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1).dataset.runId");
    const liveTrace = await api('/api/runs/' + liveID, null, 'GET');
    assert.equal(liveTrace.body.run.tool_calls.length, 1);
    assert.equal(liveTrace.body.run.tool_calls[0].state, 'completed');
    assert.equal(liveTrace.body.run.model_calls.length, 2);
    await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1).click()");
    await eventual("document.querySelector('#run-state').textContent==='Completed'", 'Live retrieval trace missing');
    await click('#run-tab-model');
    assert.match(await js("document.querySelector('#run-response pre').textContent"), /Example Domain/);
    await screenshot('agent-research-public-page-trace-phone');
    await click('#run-close');
    pass('Opt-in live HTTPS page flows through real server tool execution, provider continuation and persisted inspector (model remains a fixture)');
  }

  for (const action of ['stop', 'revoke']) {
    blockProvider = true; releaseProvider = null;
    await compose('Blocked fixture task for ' + action + ' https://example.com.');
    await eventual("!document.querySelector('#research-consent').hidden", 'Blocked task consent missing');
    await approve();
    await until(() => !!releaseProvider, 'Provider was not entered after approval');
    await eventual("!document.querySelector('#research-scope').hidden", 'Active task permissions missing');
    const activeID = await js("document.querySelector('#research-scope').dataset.taskId");
    assert.ok(activeID, 'Running controls must identify their task');
    await click('#research-permissions');
    await eventual("!document.querySelector('#research-permission-details').hidden", 'Permissions disclosure missing');
    if (action === 'stop') await screenshot('agent-research-running-phone');
    const count = providerRequests.length;
    await click(action === 'stop' ? '#research-stop' : '#research-revoke');
    await eventual("!document.querySelector('#send-button').disabled", 'Stop/revoke did not release composer');
    const task = await api('/api/research/tasks/' + activeID, null, 'GET');
    assert.equal(task.body.task.status, action === 'stop' ? 'cancelled' : 'revoked');
    releaseProvider(); releaseProvider = null; blockProvider = false;
    assert.equal(providerRequests.length, count, 'Stopped/revoked tasks must not make another model request');
    assert.match(await js("document.querySelector('#notice').textContent"), /stopp|revok|cancel/i);
  }
  pass('Stop and revoke cancel real blocked server requests and terminalize their permissions');
  assert.deepEqual(runtimeErrors, []);
  console.log('PASS production research consent integration (isolated model fixture; no live inference/search)');
} finally {
  if (releaseProvider) releaseProvider();
  if (socket) socket.close();
  for (const job of pending.values()) { clearTimeout(job.timer); job.reject(new Error('Browser closed')); }
  pending.clear();
  await stop(browser); await stop(server);
  provider.closeAllConnections(); await new Promise(resolve => provider.close(resolve));
  await rm(directory, { recursive: true, force: true, maxRetries: 20, retryDelay: 200 });
}
