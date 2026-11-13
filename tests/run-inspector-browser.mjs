// Real Agent server + SQLite + browser integration. Only the model HTTP service
// is an isolated local fixture; no production accounts, credentials, or inference.
// Build Agent first, then: node tests/run-inspector-browser.mjs <executable>
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { spawnTestBrowser, waitForTestBrowser, stopTestBrowser } from './browser-launcher.mjs';
import { existsSync } from 'node:fs';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
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
const directory = await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'agent-inspector-'));
const profile = join(directory, 'browser');
const providerKey = 'fixture-provider-credential-not-real';
const reply = 'This response came from the isolated provider fixture. The Run Inspector records actual application execution, not live model inference.';
let receivedRequest = null, releaseProvider = null, providerFails = false;
const provider = createServer(async (req, res) => {
  assert.equal(req.url, '/v1/chat/completions');
  assert.equal(req.headers.authorization, 'Bearer ' + providerKey);
  let text = ''; for await (const chunk of req) text += chunk;
  receivedRequest = JSON.parse(text);
  await new Promise(resolve => { releaseProvider = resolve; });
  if (providerFails) {
    res.writeHead(503, {'Content-Type':'application/json'});
    res.end(JSON.stringify({error:{message:'Private upstream error ' + providerKey,type:'server_error',code:'fixture_unavailable'}}));
  } else {
    res.writeHead(200, {'Content-Type':'application/json'});
    res.end(JSON.stringify({choices:[{message:{role:'assistant',content:reply}}],usage:{prompt_tokens:71,completion_tokens:27,total_tokens:98}}));
  }
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
    const result = await fn();
    if (result) return result;
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error(message);
}
async function stop(child) {
  if (!child || child.exitCode !== null) return;
  await new Promise(resolve => { child.once('exit', resolve); child.kill(); });
}
async function start() {
  server = spawn(executable, [], {
    env:{...process.env,PORT:String(port),TZ:'UTC',APP_DATA_DIR:join(directory,'data'),SIWC_DATA_DIR:join(directory,'data'),
      LLM_BASE_URL:'http://127.0.0.1:' + provider.address().port + '/v1',LLM_MODEL:'fixture-model',LLM_API_KEY:providerKey},
    stdio:'ignore',
  });
  await until(async () => { try { return (await fetch(origin + '/healthz')).ok; } catch { return false; } }, 'Agent did not start');
}
function cdp(method, params = {}) {
  return new Promise((resolve,reject) => {
    const id = ++nextID;
    const timer = setTimeout(() => {pending.delete(id); reject(new Error('CDP timeout: ' + method));},20000);
    pending.set(id,{resolve,reject,timer}); socket.send(JSON.stringify({id,method,params}));
  });
}
async function js(expression) {
  const result = await cdp('Runtime.evaluate',{expression:'(async () => { return ' + expression + '; })()',awaitPromise:true,returnByValue:true});
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
  return result.result.value;
}
const eventual = (expression,message) => until(() => js(`(() => {try{return (${expression});}catch{return false;}})()`),message);
const click = selector => js(`document.querySelector(${JSON.stringify(selector)}).click()`);
async function screenshot(name) {
  if (!process.env.UI_SCREENSHOTS) return;
  await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
  const {data} = await cdp('Page.captureScreenshot',{format:'png'});
  await writeFile(join(process.env.UI_SCREENSHOTS,name + '.png'),Buffer.from(data,'base64'));
}
function pass(message) { console.log('PASS ' + message); }
try {
  await start();
  browser = spawnTestBrowser(browserPath,['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--disable-extensions','--remote-debugging-port=0','--user-data-dir=' + profile,'about:blank'],{stdio:'ignore'});
  const portText = await waitForTestBrowser(browser, profile);
  const targets = await (await fetch('http://127.0.0.1:' + portText.split('\n')[0] + '/json/list')).json();
  socket = new WebSocket(targets.find(t=>t.type==='page').webSocketDebuggerUrl);
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject;});
  socket.onmessage = event => {
    const data=JSON.parse(event.data);
    if (data.method==='Runtime.exceptionThrown') runtimeErrors.push(data.params.exceptionDetails.exception?.description || data.params.exceptionDetails.text);
    const job=pending.get(data.id); if (!job) return;
    pending.delete(data.id); clearTimeout(job.timer);
    if (data.error) job.reject(new Error(data.error.message)); else job.resolve(data.result);
  };
  await cdp('Runtime.enable'); await cdp('Page.enable');
  await cdp('Emulation.setDeviceMetricsOverride',{width:1440,height:900,deviceScaleFactor:1,mobile:false});
  await cdp('Page.navigate',{url:origin});
  await eventual("!!document.querySelector('#account-form')",'Login page missing');
  assert.equal(await js(`(await fetch('/api/auth/register',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'inspector-browser',password:'synthetic-inspector-only'})})).status`),201);
  await cdp('Page.reload');
  await eventual("document.querySelector('#model-select')?.value==='fixture-model' && !document.querySelector('#chat-app').hidden",'Authenticated model selection missing');
  await js("(() => {document.querySelector('#message-input').value='Explain the supplied current date and your available capabilities.';document.querySelector('#chat-form').requestSubmit();})()");
  await until(()=>releaseProvider,'Provider request missing');
  await click('.inspect-run');
  await eventual("document.querySelector('#run-state').textContent==='Running'",'Real running trace missing');
  const runID=await js("document.querySelector('.inspect-run').dataset.runId");
  assert.match(runID,/^[a-f0-9-]{36}$/);
  const getRun = () => js(`fetch('/api/runs/'+${JSON.stringify(runID)},{cache:'no-store'}).then(async r=>({status:r.status,cache:r.headers.get('Cache-Control'),data:await r.json()}))`);
  const live=await getRun();
  assert.equal(live.status,200); assert.equal(live.cache,'no-store'); assert.equal(live.data.run.state,'running');
  assert.deepEqual(live.data.run.request,receivedRequest,'Inspector must contain the actual provider request body');
  assert.ok(receivedRequest.messages.some(m=>m.role==='system' && m.content.includes(live.data.run.context.current_date)), 'Trusted date missing from actual model request');
  assert.ok(!JSON.stringify(live.data).includes(providerKey),'Provider credentials leaked into trace');
  await screenshot('agent-inspector-real-running');
  pass('Actual provider request is inspectable while blocked; server context is in the wire payload');
  releaseProvider(); releaseProvider=null;
  await eventual("document.querySelector('#run-state').textContent==='Completed' && !document.querySelector('#send-button').disabled",'Real completed trace missing');
  const completed=await getRun();
  assert.equal(completed.data.run.state,'completed');
  assert.ok(completed.data.run.finished_at); assert.ok(completed.data.run.duration_ms>=0);
  assert.equal(completed.data.run.usage.input_tokens,71); assert.equal(completed.data.run.usage.output_tokens,27); assert.equal(completed.data.run.usage.total_tokens,98);
  const timelineText=await js("document.querySelector('#run-events').textContent");
  assert.ok(timelineText.includes('Context prepared') && timelineText.includes('Model request started') && timelineText.includes('Response received'),'Timeline should use readable event names');
  await click('#run-tab-model');
  assert.ok(await js("document.querySelector('#run-view-model').textContent.includes('71 input') && document.querySelector('#run-view-model').textContent.includes('27 output')"));
  await screenshot('agent-inspector-real-model');
  await click('#run-tab-context');
  await screenshot('agent-inspector-real-context');
  const conversationID=completed.data.run.conversation_id;
  assert.ok(conversationID,'Completed trace missing saved conversation association');
  pass('Completion, timings, provider-reported usage, and saved conversation association are real');
  const stranger=await fetch(origin + '/api/auth/register',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'inspector-stranger',password:'synthetic-stranger-only'})});
  assert.equal(stranger.status,201);
  const forbidden=await fetch(origin + '/api/runs/' + runID,{headers:{Cookie:stranger.headers.get('set-cookie').split(';')[0]}});
  assert.equal(forbidden.status,404);
  assert.equal((await fetch(origin + '/api/runs/' + runID)).status,401);
  pass('Real authentication rejects signed-out and other-user trace reads');
  await stop(server); await start();
  await cdp('Page.reload');
  await eventual("document.querySelector('#conversation-list .session')",'Persistent conversation missing after server restart');
  await click('#conversation-list .session');
  await eventual("!!document.querySelector('.inspect-run')",'Persistent run association missing after restart');
  await click('.inspect-run');
  await eventual("document.querySelector('#run-state').textContent==='Completed'",'Persistent completed trace missing after restart');
  await click('#run-tab-timeline');
  await screenshot('agent-inspector-real-desktop');
  await cdp('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});
  await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
  assert.ok(await js("document.documentElement.scrollWidth<=innerWidth && document.querySelector('#run-inspector').getAttribute('aria-modal')==='true'"));
  await screenshot('agent-inspector-real-phone');
  await click('#run-close');
  await cdp('Emulation.setDeviceMetricsOverride',{width:1440,height:900,deviceScaleFactor:1,mobile:false});
  pass('Trace and message linkage survive an actual server restart and work on phone layout');
  providerFails=true; receivedRequest=null;
  await js("(() => {document.querySelector('#message-input').value='Exercise a provider failure.';document.querySelector('#chat-form').requestSubmit();})()");
  await until(()=>releaseProvider,'Failing provider request missing');
  releaseProvider(); releaseProvider=null;
  await eventual("document.querySelector('#notice').classList.contains('error') && !document.querySelector('#send-button').disabled",'Provider failure missing');
  await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1).click()");
  await eventual("document.querySelector('#run-state').textContent==='Failed'",'Real failure trace missing');
  const failedID=await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1).dataset.runId");
  const failed=await js(`(await fetch('/api/runs/'+${JSON.stringify(failedID)})).json()`);
  assert.equal(failed.run.state,'failed');
  assert.ok(!JSON.stringify(failed).includes(providerKey) && !JSON.stringify(failed).includes('Private upstream error'));
  await click('#run-tab-model'); await screenshot('agent-inspector-real-failed');
  pass('Real upstream failure remains inspectable without persisting private upstream error content');
  await click('#run-close');
  assert.equal(await js(`(await fetch('/api/conversations/'+${JSON.stringify(conversationID)},{method:'DELETE'})).status`),200);
  assert.equal((await getRun()).status,404,'Deleting a conversation must remove its traces');
  assert.deepEqual(runtimeErrors,[]);
  pass('Conversation deletion removes its traces; no browser runtime exceptions');
  console.log('PASS real Run Inspector integration (local provider fixture; no live inference)');
} finally {
  if (releaseProvider) releaseProvider();
  if (socket) socket.close();
  for (const job of pending.values()) {clearTimeout(job.timer);job.reject(new Error('Browser closed'));}
  pending.clear();
  await stopTestBrowser(browser); await stop(server);
  await new Promise(resolve=>provider.close(resolve));
  await rm(directory,{recursive:true,force:true,maxRetries:20,retryDelay:200});
}
