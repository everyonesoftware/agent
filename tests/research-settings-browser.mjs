// Launched by TestResearchSettingsBrowser against the production Agent router.
// Brave and model HTTP calls use isolated upstream fixtures in that Go test.
import assert from 'node:assert/strict';
import { spawnTestBrowser, waitForTestBrowser, stopTestBrowser } from './browser-launcher.mjs';
import { existsSync } from 'node:fs';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

assert.equal(process.argv.length, 3, 'Supply the isolated Go test server origin');
const origin = process.argv[2];
assert.equal(new URL(origin).hostname, '127.0.0.1', 'Only use the isolated loopback fixture server');
const browserPath = process.env.UI_BROWSER || [
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  '/usr/bin/google-chrome', '/usr/bin/chromium', '/usr/bin/chromium-browser',
].find(existsSync);
assert.ok(browserPath, 'Set UI_BROWSER to an installed Chromium browser');
const profile = await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'agent-settings-browser-'));
const keys = { first: 'synthetic-brave-A-ABCD', replacement: 'synthetic-brave-A-WXYZ', rejected: 'synthetic-brave-rejected-NOPE', other: 'synthetic-brave-B-EFGH' };
let browser, socket, nextID = 0;
const pending = new Map(), runtimeErrors = [];
async function until(fn, message, timeout = 20000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await fn();
    if (value) return value;
    await new Promise(resolve => setTimeout(resolve, 40));
  }
  throw new Error(message);
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
const eventual = (expression, message) => until(() => js(`(() => {try{return (${expression});}catch{return false;}})()`), message);
const click = selector => js(`document.querySelector(${JSON.stringify(selector)}).click()`);
const api = (path, body, method = body === undefined ? 'GET' : 'POST') => js(`fetch(${JSON.stringify(path)},{method:${JSON.stringify(method)},headers:{'Content-Type':'application/json'},cache:'no-store',${body === undefined ? '' : 'body:JSON.stringify(' + JSON.stringify(body) + '),'}}).then(async r=>({status:r.status,cache:r.headers.get('Cache-Control'),body:await r.json()}))`);
const pass = message => console.log('PASS ' + message);
function noKeys(value) {
  const encoded = JSON.stringify(value);
  for (const key of Object.values(keys)) assert.ok(!encoded.includes(key), 'A complete fixture search key leaked');
  assert.ok(!encoded.includes('PRIVATE-UPSTREAM-DETAIL'), 'Private upstream error content leaked');
}
async function enterKey(key) {
  await js(`(() => {const input=document.getElementById('research-api-key');input.value=${JSON.stringify(key)};input.dispatchEvent(new Event('input',{bubbles:true}));})()`);
}
async function screenshot(name) {
  if (!process.env.UI_SCREENSHOTS) return;
  await js('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))');
  const { data } = await cdp('Page.captureScreenshot', { format: 'png' });
  await writeFile(join(process.env.UI_SCREENSHOTS, name + '.png'), Buffer.from(data, 'base64'));
}
async function settingsCardInView() {
  await js("(() => {const panel=document.getElementById('settings-panel'),card=document.getElementById('research-settings-card');panel.scrollTop+=card.getBoundingClientRect().top-panel.getBoundingClientRect().top-18;})()");
}
try {
  browser = spawnTestBrowser(browserPath, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--remote-debugging-port=0', '--user-data-dir=' + profile, 'about:blank'], { stdio: 'ignore' });
  const portText = await waitForTestBrowser(browser, profile);
  const targets = await (await fetch('http://127.0.0.1:' + portText.split('\n')[0] + '/json/list')).json();
  socket = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {socket.onopen=resolve;socket.onerror=reject;});
  socket.onmessage = event => {
    const data=JSON.parse(event.data);
    if (data.method==='Runtime.exceptionThrown') runtimeErrors.push(data.params.exceptionDetails.exception?.description || data.params.exceptionDetails.text);
    const job=pending.get(data.id);if(!job)return;
    pending.delete(data.id);clearTimeout(job.timer);
    if(data.error)job.reject(new Error(data.error.message));else job.resolve(data.result);
  };
  await cdp('Runtime.enable'); await cdp('Page.enable');
  if(process.env.UI_LOCALE)await cdp('Emulation.setLocaleOverride',{locale:process.env.UI_LOCALE});
  await cdp('Emulation.setDeviceMetricsOverride', {width:1440,height:1000,deviceScaleFactor:1,mobile:false});
  await cdp('Page.navigate', {url:origin});
  await eventual("!!document.getElementById('account-form')", 'Account form missing');
  assert.equal((await api('/api/auth/register',{username:'settings-primary',password:'synthetic-settings-password'})).status,201);
  await cdp('Page.reload');
  await eventual("!document.getElementById('chat-app').hidden && document.getElementById('model-select').value==='settings-fixture-model'", 'Signed-in workspace missing');
  await click('#settings-button');
  assert.ok(await js("!!document.getElementById('research-settings-card')"), 'Approved Web research settings card is missing');
  assert.ok(await js("document.getElementById('research-key-consent')?.getClientRects().length>0"), 'Task consent must remain visible without expanding help');
  await eventual("!document.getElementById('research-api-key').disabled", 'Key settings did not load');
  const initial=await api('/api/research/settings');
  assert.equal(initial.status,200);assert.equal(initial.cache,'no-store');assert.equal(initial.body.settings.configured,false);
  assert.equal(initial.body.settings.budget?.enabled,false,'A new account tracks searches without enabling a cap');
  assert.equal(initial.body.settings.budget.used,0);
  assert.equal(initial.body.settings.budget.reserved,0);
  assert.equal((await api('/api/research')).body.search_available,false,'A shared environment key must not enable per-user search');
  assert.equal((await api('/__fixture/state')).body.search_calls,0,'Opening Settings must not send a Brave query');
  assert.ok(await js("document.querySelector('#settings-panel .settings-card').id==='pwa-updates'"),'Web app updates must remain first');
  await settingsCardInView(); await screenshot('agent-brave-settings-real-setup-desktop');
  pass('Production Settings loads owner-scoped metadata without a Brave request or environment fallback');

  await enterKey(keys.first); await click('#research-key-verify');
  await eventual("!document.getElementById('research-key-saved').hidden && document.getElementById('research-api-key').value===''") .catch(error=>{throw new Error('Verified key was not saved and cleared: '+error.message);});
  const saved=await api('/api/research/settings');
  assert.equal(saved.body.settings.configured,true);assert.equal(saved.body.settings.key_suffix,'ABCD');assert.ok(saved.body.settings.revision);noKeys(saved);
  assert.equal(saved.body.settings.budget.used,1,'The initial successful verification counts as Agent usage');
  assert.equal(saved.body.settings.budget.reserved,0);
  assert.equal(saved.body.settings.usage.windows[1].limit,2000);
  assert.equal(saved.body.settings.usage.windows[1].remaining,1999);
  assert.equal(saved.body.settings.usage.windows[1].window_seconds,2592000);
  assert.ok(Date.parse(saved.body.settings.usage.observed_at));
  await eventual("document.querySelector('#research-usage-windows').textContent.includes((1999).toLocaleString())",'Verification quota snapshot was not rendered');
  assert.ok(await js("!document.querySelector('#research-usage-panel').hidden && document.querySelector('#research-usage-windows [role=progressbar]').getAttribute('aria-valuenow')==='1999'"));
  assert.equal((await api('/__fixture/state')).body.verify_calls,1,'Explicit save must send exactly one verification request');
  assert.equal((await api('/__fixture/state')).body.model_calls,0,'Key verification must not call the model');
  assert.equal((await api('/api/research')).body.search_available,true);
  pass('Verify & save traverses the real Brave adapter and SQLite, exposes suffix only, and never calls a model');

  const second=await fetch(origin+'/api/auth/register',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'settings-secondary',password:'synthetic-secondary-password'})});
  assert.equal(second.status,201);const secondCookie=second.headers.get('set-cookie').split(';')[0];
  async function otherAPI(path,body) {
    const response=await fetch(origin+path,{method:body===undefined?'GET':'POST',headers:{Cookie:secondCookie,'Content-Type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})});
    return {status:response.status,body:await response.json()};
  }
  const otherInitial=await otherAPI('/api/research/settings');
  assert.equal(otherInitial.body.settings.configured,false);assert.equal((await otherAPI('/api/research')).body.search_available,false);
  const otherSaved=await otherAPI('/api/research/settings/verify',{api_key:keys.other,expected_revision:otherInitial.body.settings.revision});
  assert.equal(otherSaved.status,200);assert.equal(otherSaved.body.settings.key_suffix,'EFGH');noKeys(otherSaved);
  assert.equal(otherSaved.body.settings.usage.windows[1].remaining,1998);
  assert.deepEqual((await api('/api/research/settings')).body.settings.usage,saved.body.settings.usage,'Another owner cannot overwrite this user’s snapshot');
  assert.equal((await api('/api/research/settings')).body.settings.key_suffix,'ABCD');
  await api('/__fixture/reopen',{}); await cdp('Page.reload');
  await eventual("!document.getElementById('research-key-saved').hidden", 'Stored key did not survive a production-router storage reopen');
  assert.equal((await api('/api/research/settings')).body.settings.key_suffix,'ABCD');
  assert.deepEqual((await api('/api/research/settings')).body.settings.usage,saved.body.settings.usage,'Usage survives reopening SQLite');
  assert.equal((await api('/api/research/settings')).body.settings.budget.used,1,'Local request history survives reopening SQLite');
  pass('Credentials are per-user and survive reopening the real persistent database without being returned to the page');

  await click('#research-key-replace');await enterKey(keys.rejected);await click('#research-key-verify');
  await eventual("!document.getElementById('research-key-error').hidden && !document.getElementById('research-key-verify').disabled", 'Rejected replacement did not show an actionable error');
  const afterRejected=await api('/api/research/settings');
  assert.equal(afterRejected.body.settings.key_suffix,'ABCD');assert.equal(afterRejected.body.settings.revision,saved.body.settings.revision);
  assert.deepEqual(afterRejected.body.settings.usage,saved.body.settings.usage,'A rejected replacement must not change the saved key’s usage');
  assert.equal(afterRejected.body.settings.budget.used,1,'Definitively rejected requests do not consume the local successful-request count');
  noKeys(await js("({error:document.getElementById('research-key-error').textContent,feedback:document.getElementById('research-key-feedback').textContent})"));
  await click('#research-key-cancel');
  await eventual("!document.getElementById('research-key-saved').hidden && document.getElementById('research-api-key').value===''") .catch(error=>{throw new Error('Cancel did not restore saved key: '+error.message);});
  pass('Rejected replacement preserves the previous credential and revision; cancellation clears the candidate');

  const approvedPayload={messages:[{role:'user',content:'Research a public fixture topic.'}],model:'settings-fixture-model',conversation_id:'',run_id:'b3333333-3333-4333-8333-333333333333',omitted_messages:0};
  const oldTask=await api('/api/research/tasks',approvedPayload);assert.equal(oldTask.status,201);
  assert.equal((await api('/api/research/tasks/'+oldTask.body.task.id+'/approve',{})).status,200);
  await click('#research-key-replace');await enterKey(keys.replacement);await click('#research-key-verify');
  await eventual("!document.getElementById('research-key-saved').hidden && document.getElementById('research-key-suffix').textContent.includes('WXYZ')", 'Replacement was not reflected in the saved-key summary');
  const replaced=await api('/api/research/settings');assert.notEqual(replaced.body.settings.revision,saved.body.settings.revision);
  assert.equal(replaced.body.settings.budget.used,2,'Key replacement preserves previous usage and counts the replacement verification');
  const oldExecution=await api('/api/chat',{...approvedPayload,research_task_id:oldTask.body.task.id});
  assert.ok(oldExecution.status>=400);assert.equal((await api('/__fixture/state')).body.model_calls,0,'A replaced credential must not reuse an earlier approved grant');
  await click('#research-key-check');
  await eventual("!document.getElementById('research-key-check').disabled", 'Saved-key connection check did not finish');
  assert.equal((await api('/api/research/settings')).body.settings.key_suffix,'WXYZ');
  assert.equal((await api('/__fixture/state')).body.last_suffix,'WXYZ');
  pass('Successful replacement changes the revision and invalidates prior consent; connection checks use the saved owner key');

  for(const width of [1440,412,390,320]) {
    await cdp('Emulation.setDeviceMetricsOverride',{width,height:width<600?844:1000,deviceScaleFactor:1,mobile:width<600});
    await settingsCardInView();
    assert.ok(await js("document.documentElement.scrollWidth<=innerWidth && document.getElementById('research-settings-card').scrollWidth<=document.getElementById('research-settings-card').clientWidth"),'Key settings must fit '+width+'px');
    if(width<600)assert.ok(await js("document.getElementById('research-key-replace').getBoundingClientRect().height>=44"),'Phone key actions need a 44px target');
    await screenshot('agent-brave-settings-real-connected-'+width);
    await js("(() => {const p=document.getElementById('settings-panel'),u=document.getElementById('research-usage-panel');p.scrollTop+=u.getBoundingClientRect().top-p.getBoundingClientRect().top-18;})()");
    assert.ok(await js("document.querySelector('#research-budget-used').getBoundingClientRect().width>0 && document.querySelector('#research-usage-dashboard').getBoundingClientRect().height>=44"),'Local usage and dashboard target must fit '+width+'px');
    await screenshot('agent-brave-settings-real-usage-'+width);
  }
  await click('#back-to-chat');
  await js("(() => {document.getElementById('research-toggle').checked=true;document.getElementById('message-input').value='Research a public topic with the configured search key.';document.getElementById('chat-form').requestSubmit();})()");
  await eventual("!document.getElementById('research-consent').hidden && !document.getElementById('research-approve').disabled", 'Configured research still needs task consent');
  await click('#research-approve');
  await eventual("!document.getElementById('send-button').disabled && document.querySelectorAll('.inspect-run').length>0", 'Configured research did not complete');
  const runID=await js("Array.from(document.querySelectorAll('.inspect-run')).at(-1).dataset.runId");
  const run=await api('/api/runs/'+runID);assert.equal(run.body.run.state,'completed');
  assert.equal(run.body.run.tool_calls[0].name,'web_search');assert.equal(run.body.run.tool_calls[0].state,'completed');noKeys(run);
  assert.equal((await api('/__fixture/state')).body.last_suffix,'WXYZ');
  pass('Saved configuration feeds actual research tool execution only after task approval, with no keys in model requests or inspector records');

  await click('#settings-button');await eventual("!document.getElementById('research-key-saved').hidden",'Saved key settings did not reopen');
  const callsAfterResearch=(await api('/__fixture/state')).body.search_calls;
  const researchUsage=(await api('/api/research/settings')).body.settings.usage;
  assert.equal(researchUsage.windows[1].remaining,2000-callsAfterResearch,'Ordinary approved research must save response usage');
  await eventual("document.querySelector('#research-usage-windows').textContent.includes(("+(2000-callsAfterResearch)+").toLocaleString())",'Research snapshot was not shown on reopening Settings');
  assert.equal((await api('/__fixture/state')).body.search_calls,callsAfterResearch,'Opening Settings must not query Brave to refresh usage');
  pass('Real Brave response headers feed owner-scoped persisted usage through verification, test and approved research without background calls');
  const local=await api('/api/research/settings');
  assert.equal(local.body.settings.budget.used,4,'Verification, replacement, connection check and ordinary research are counted for this owner only');
  assert.equal(local.body.settings.budget.reserved,0);
  // Exercise the approved budget form against the real router, SQLite and Brave adapter.
  await js("(() => {const enabled=document.getElementById('research-budget-enabled');if(!enabled.checked)enabled.click();for(const [id,value] of [['research-budget-maximum','5'],['research-budget-period','month'],['research-budget-timezone','America/Los_Angeles']]){const input=document.getElementById(id);input.value=value;input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));}})()");
  const beforeBudgetSave=(await api('/__fixture/state')).body.search_calls;
  await click('#research-budget-save');
  await eventual("!document.getElementById('research-budget-save').disabled || /saved|enabled/i.test(document.getElementById('research-budget-feedback').textContent)",'Budget save did not settle');
  await eventual("document.getElementById('research-budget-progress').getAttribute('aria-valuemax')==='5'",'Saved cap was not rendered');
  const capped=await api('/api/research/settings');
  assert.equal(capped.body.settings.budget.enabled,true);assert.equal(capped.body.settings.budget.maximum,5);
  assert.equal(capped.body.settings.budget.timezone,'America/Los_Angeles');
  assert.equal((await api('/__fixture/state')).body.search_calls,beforeBudgetSave,'Saving a cap sends no Brave request');
  for(const width of [1440,390,320]) {
    await cdp('Emulation.setDeviceMetricsOverride',{width,height:width<600?875:1000,deviceScaleFactor:1,mobile:width<600});
    await js("(() => {const p=document.getElementById('settings-panel'),u=document.getElementById('research-usage-panel');p.scrollTop+=u.getBoundingClientRect().top-p.getBoundingClientRect().top-18;})()");
    assert.ok(await js("document.documentElement.scrollWidth<=innerWidth && document.getElementById('research-budget-progress').getBoundingClientRect().width>0 && document.getElementById('research-budget-save').getBoundingClientRect().height>=44"),'Used-progress and cap controls fit '+width+'px');
    await screenshot('agent-brave-budget-fixture-'+width);
    if(width===390){await js("document.getElementById('research-budget-save').scrollIntoView({block:'center'})");await screenshot('agent-brave-budget-fixture-phone-controls');}
  }
  assert.equal((await api('/api/research/settings/budget',{expected_revision:capped.body.settings.revision,expected_budget_revision:'stale',enabled:true,maximum:6,period:'month',timezone:'UTC'})).status,409,'Stale cap writes require explicit review');
  await click('#research-key-check');await eventual("!document.getElementById('research-key-check').disabled",'Final available request did not settle');
  assert.equal((await api('/api/research/settings')).body.settings.budget.used,5);
  assert.ok(await js("document.getElementById('research-budget-progress').getAttribute('aria-valuenow')==='5' && !document.getElementById('research-budget-notice').hidden"),'Progress fills toward used requests and explains exhaustion');
  const atCap=(await api('/__fixture/state')).body.search_calls;
  await click('#research-key-check');await eventual("!document.getElementById('research-key-check').disabled",'Cap rejection did not settle');
  assert.equal((await api('/__fixture/state')).body.search_calls,atCap,'An exhausted cap stops the request before Brave');
  assert.ok(await js("/limit|cap/i.test(document.getElementById('research-key-error').textContent) && !/Brave rate limit/i.test(document.getElementById('research-key-error').textContent)"),'Local exhaustion is not mislabeled as a Brave rate limit');
  const currentCap=await api('/api/research/settings');
  assert.equal(currentCap.body.settings.status,'connected','Local budget exhaustion does not mark a valid credential invalid');
  const disable=await api('/api/research/settings/budget',{expected_revision:currentCap.body.settings.revision,expected_budget_revision:currentCap.body.settings.budget.revision,enabled:false,maximum:5,period:'month',timezone:'America/Los_Angeles'});
  assert.equal(disable.status,200);assert.equal(disable.body.settings.budget.used,5,'Disabling a cap retains usage');
  await click('#back-to-chat');await click('#settings-button');await eventual("!document.getElementById('research-key-check').disabled",'Settings did not reload');
  assert.ok(await js("document.getElementById('research-budget-progress').hidden && document.getElementById('research-budget-used').textContent.includes((5).toLocaleString())"),'Cap-disabled usage is visible without a misleading progress denominator');
  assert.equal((await otherAPI('/api/research/settings')).body.settings.budget.used,1,'Another owner has an independent budget');
  pass('Real budget form saves without queries, preserves history and enforces the cap before Brave dispatch');
  // The extended scenarios intentionally use the bounded connection-check budget.
  // Reopen the isolated application before testing upstream rejection; keep the
  // real verification rate limiter intact rather than weakening it for this test.
  await api('/__fixture/reopen',{});await cdp('Page.reload');
  await eventual("!document.getElementById('research-key-check').disabled",'Settings did not return after reopening the fixture');
  assert.equal((await api('/api/research/settings')).body.settings.budget.used,5,'Usage still survives reopening after cap enforcement');
  await api('/__fixture/reject-saved?enabled=1',{});await click('#research-key-check');
  await eventual("/invalid|unavailable|attention/i.test(document.getElementById('research-settings-badge').textContent)",'A rejected saved key must not retain a passing verification indicator');
  const failedCheck=await api('/api/research/settings');assert.equal(failedCheck.body.settings.configured,true);assert.equal(failedCheck.body.settings.key_suffix,'WXYZ');noKeys(failedCheck);
  assert.equal(failedCheck.body.settings.status,'invalid');
  await api('/__fixture/reject-saved?enabled=0',{});
  await click('#research-key-remove');await eventual("document.getElementById('research-key-remove-dialog').open",'Named removal confirmation missing');
  await click('#research-key-keep');assert.equal((await api('/api/research/settings')).body.settings.configured,true);
  await click('#research-key-remove');await click('#research-key-remove-confirm');
  await eventual("document.getElementById('research-key-saved').hidden && !document.getElementById('research-api-key').disabled",'Local removal did not restore setup');
  const removed=await api('/api/research/settings');assert.equal(removed.body.settings.configured,false);assert.ok(removed.body.settings.revision);
  assert.ok(!removed.body.settings.usage,'Removal clears persisted usage');
  assert.ok(await js("!document.querySelector('#research-usage-panel').hidden && !document.querySelector('#research-usage-windows').textContent"),'Removal clears credential quota but retains the account usage panel');
  assert.equal(removed.body.settings.budget.used,5,'Removing a key does not erase local request history');
  assert.equal((await api('/api/research')).body.search_available,false);
  const beforeStale=(await api('/__fixture/state')).body.search_calls;
  const stale=await api('/api/research/settings/verify',{api_key:keys.first,expected_revision:replaced.body.settings.revision});
  assert.equal(stale.status,409,'A stale key write must not resurrect removed configuration');
  assert.equal((await api('/__fixture/state')).body.search_calls,beforeStale,'Stale writes must fail before external verification');
  assert.equal((await otherAPI('/api/research/settings')).body.settings.key_suffix,'EFGH');assert.equal((await otherAPI('/api/research')).body.search_available,true);
  noKeys(removed);noKeys(stale);
  const storage=await js("({local:Object.values(localStorage),session:Object.values(sessionStorage)})");noKeys(storage);
  assert.deepEqual(runtimeErrors,[]);
  pass('Check errors are sanitized, removal is confirmed and isolated, tombstones reject stale writes, and browser storage has no key');
  console.log('PASS production Brave settings integration (real app/SQLite; isolated Brave and model HTTP fixtures)');
} finally {
  if(socket)socket.close();
  for(const job of pending.values()){clearTimeout(job.timer);job.reject(new Error('Browser closed'));}pending.clear();
  await stopTestBrowser(browser);
  await rm(profile,{recursive:true,force:true,maxRetries:20,retryDelay:200});
}
