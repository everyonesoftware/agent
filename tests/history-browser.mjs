// Production-router/SQLite integration launched by TestUsageHistoryProductionBrowser.
// All account and provider observations are isolated fixtures; no live services.
import assert from 'node:assert/strict';
import {mkdtemp, rm, writeFile} from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {spawnTestBrowser,waitForTestBrowser,stopTestBrowser} from './browser-launcher.mjs';
const origin=process.argv[2];assert.equal(new URL(origin).hostname,'127.0.0.1');
const profile=await mkdtemp(join(process.env.TMPDIR||tmpdir(),'agent-history-browser-'));
let browser,socket,id=0;const pending=new Map(),errors=[];
function cdp(method,params={}){return new Promise((resolve,reject)=>{const n=++id,timer=setTimeout(()=>{pending.delete(n);reject(new Error('CDP timeout '+method))},20000);pending.set(n,{resolve,reject,timer});socket.send(JSON.stringify({id:n,method,params}))})}
async function js(expression){const out=await cdp('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(out.exceptionDetails)throw new Error(out.exceptionDetails.exception?.description||out.exceptionDetails.text);return out.result.value}
async function until(expression,message){const end=Date.now()+20000;while(Date.now()<end){if(await js(`(()=>{try{return !!(${expression})}catch{return false}})()`))return;await new Promise(r=>setTimeout(r,50))}throw new Error(message)}
const click=id=>js(`document.getElementById(${JSON.stringify(id)}).click()`);
async function capture(name){if(!process.env.UI_SCREENSHOTS)return;const {data}=await cdp('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.UI_SCREENSHOTS,name+'.png'),Buffer.from(data,'base64'))}
try{
 browser=spawnTestBrowser(process.env.UI_BROWSER,['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--disable-extensions','--remote-debugging-port=0','--user-data-dir='+profile,'about:blank'],{stdio:'ignore'});
 const ready=await waitForTestBrowser(browser,profile);const targets=await(await fetch('http://127.0.0.1:'+ready.split('\n')[0]+'/json/list')).json();socket=new WebSocket(targets.find(t=>t.type==='page').webSocketDebuggerUrl);await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject});socket.onmessage=e=>{const v=JSON.parse(e.data);if(v.method==='Runtime.exceptionThrown')errors.push(v.params.exceptionDetails.exception?.description||v.params.exceptionDetails.text);const job=pending.get(v.id);if(!job)return;pending.delete(v.id);clearTimeout(job.timer);if(v.error)job.reject(new Error(v.error.message));else job.resolve(v.result)};
 await cdp('Runtime.enable');await cdp('Page.enable');await cdp('Emulation.setDeviceMetricsOverride',{width:1280,height:1000,deviceScaleFactor:1,mobile:false});await cdp('Page.navigate',{url:origin});await until("!!document.getElementById('account-form')",'Login form missing');
 const login=await js("fetch('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'confirmation-user',password:'correct-horse-battery'})}).then(r=>r.status)");assert.equal(login,200);await cdp('Page.reload');await until("!document.getElementById('chat-app').hidden",'Signed-in page missing');await click('settings-button');
 await until("!!document.getElementById('usage-history-open-chatgpt')",'ChatGPT history entry point missing');await until("!document.getElementById('usage-history-open-chatgpt').disabled",'ChatGPT history entry point unavailable');
 await capture('history-settings-desktop');await click('usage-history-open-chatgpt');await until("document.getElementById('usage-history-dialog').open",'ChatGPT popup did not open');await until("document.getElementById('usage-history-dialog').textContent.includes('68%')",'Real quota observations were not rendered');
 assert.ok(await js("!document.getElementById('usage-history-dialog').innerText.includes('Brave')"),'ChatGPT popup leaked other provider');assert.equal(await js("document.getElementById('usage-history-range').value"),'24h');assert.deepEqual(await js("[...document.getElementById('usage-history-range').options].map(o=>o.value)"),['24h','week','period']);
 await until("!!document.getElementById('usage-history-forecast')",'Available stored observations did not produce a forecast');
 assert.match(await js("document.getElementById('usage-history-basis').textContent"),/Forecast:.*1 hour.*covered/);
 assert.match(await js("document.getElementById('usage-history-budget').textContent"),/^Daily budget:/);
 assert.match(await js("document.querySelector('.usage-history-recorded').getAttribute('d')"),/ C /,'Provider history is smoothed in the production page');
 assert.equal(await js("document.querySelectorAll('#usage-history-forecast-daily').length"),0,'Only one recent-average forecast is shown');
 const quotaHistory=await js("fetch('/api/usage/history?profile_id='+encodeURIComponent(document.getElementById('account-select').value)+'&range=24h').then(r=>r.json())");
 assert.ok(quotaHistory.sessions?.length,'The real scoped endpoint derives session evidence from SQLite');
 assert.ok(quotaHistory.forecast_points.every(p=>p.window_minutes>0),'Forecast evidence carries the quota definition');
 assert.ok(Math.abs(quotaHistory.sessions.at(-1).used_percent-12)<1e-8,'Session usage matches the actual normalized observations');
 assert.equal(quotaHistory.sessions.at(-1).estimated,true);
 await until("!!document.querySelector('.usage-history-session.current')",'Production session band missing');
 const sessionPoint=await js("(()=>{const r=document.querySelector('.usage-history-session.current').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()");
 await cdp('Input.dispatchMouseEvent',{type:'mouseMoved',...sessionPoint});
 assert.match(await js("document.getElementById('usage-history-tip-text').textContent"),/Current session.*estimated[\s\S]*12% used/,'Production cursor popup uses persisted session evidence');
 assert.equal(await js("document.querySelectorAll('.session-caption').length"),0,'Session details have no below-chart captions');
 await capture('history-chatgpt-desktop');
 await cdp('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
 assert.equal(await js("(()=>{const t=document.getElementById('usage-history-title').getBoundingClientRect(),r=document.getElementById('usage-history-range').getBoundingClientRect(),d=document.getElementById('usage-history-dialog');return t.right<=r.left&&Math.abs((t.top+t.bottom-r.top-r.bottom)/2)<2&&d.scrollHeight<=d.clientHeight;})()"),true,'Production phone header is inline and dialog fits without scrolling');
 await capture('history-chatgpt-mobile');await cdp('Emulation.setTouchEmulationEnabled',{enabled:true});
 const detailPoint=await js("(()=>{const r=document.getElementById('usage-history-details-toggle').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()");
 const tapDetails=async()=>{await cdp('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[detailPoint]});await cdp('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});};
 await tapDetails();await until("document.getElementById('usage-history-details').open",'Touch did not open forecast details');
 assert.equal(await js("(()=>{const d=document.getElementById('usage-history-dialog'),p=document.querySelector('.history-details-body').getBoundingClientRect(),r=d.getBoundingClientRect();return p.top>=r.top&&p.bottom<=r.bottom&&d.scrollHeight<=d.clientHeight;})()"),true,'Details fit above the phone footer without changing dialog height');
 await capture('history-chatgpt-mobile-details');
 const outsideDetails=await js("(()=>{const r=document.getElementById('usage-history-value').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()");
 await cdp('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[outsideDetails]});await cdp('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});await until("!document.getElementById('usage-history-details').open",'Outside touch did not close forecast details');
 await cdp('Emulation.setTouchEmulationEnabled',{enabled:false});await cdp('Emulation.setDeviceMetricsOverride',{width:1280,height:1000,deviceScaleFactor:1,mobile:false});await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
 await js("(()=>{const r=document.getElementById('usage-history-range');r.value='period';r.dispatchEvent(new Event('change'))})()");await until("!!document.querySelector('#usage-history-dialog svg')",'Entire-period chart missing');await capture('history-chatgpt-period-desktop');await click('usage-history-close');
 await until("!!document.getElementById('usage-history-open-brave')&&!document.getElementById('usage-history-open-brave').disabled",'Brave history entry point unavailable');await click('usage-history-open-brave');await until("document.getElementById('usage-history-dialog').open&&document.getElementById('usage-history-dialog').textContent.includes('5')",'Brave ledger history did not load');assert.ok(await js("!document.getElementById('usage-history-dialog').innerText.includes('ChatGPT')"),'Brave popup leaked other provider');
 const history=await js("fetch('/api/research/history?range=24h').then(r=>r.json())");assert.equal(history.remaining_count,5);assert.equal(history.remaining_percent,50);assert.ok(history.points.length>=2);assert.equal(history.provider,'brave');
 assert.ok(history.forecast_points.length>=2,'The ledger supplies exact cutoff balances separately from chart buckets');
 await until("!!document.querySelector('.usage-history-recorded')",'Brave smoothed chart missing');
 const bravePath=await js("document.querySelector('.usage-history-recorded').getAttribute('d')");
 assert.match(bravePath,/ C /,'Brave ledger history uses a smoothed display curve');
 assert.doesNotMatch(bravePath,/ [HV] /,'Brave display no longer uses the old step line');
 assert.match(await js("document.getElementById('usage-history-value').textContent"),/^5 searches left$/,'The smoothed chart retains the exact ledger balance');
 assert.match(await js("document.getElementById('usage-history-note').textContent"),/Agent cap.*time-smoothed.*exact readings/,'Brave discloses display-only smoothing');
 await capture('history-brave-desktop');await cdp('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');assert.equal(await js('document.documentElement.scrollWidth<=innerWidth'),true,'Mobile page overflows');await capture('history-brave-mobile');await click('usage-history-close');
 // History reads must never create extra searches, inference, or budget events.
 const after=await js("fetch('/api/research/settings').then(r=>r.json())");assert.equal(after.settings.budget.used,5);assert.deepEqual(errors,[],'Runtime exceptions');
 console.log('PASS production Settings, scoped ChatGPT observations and Brave ledger, 24h/week/period options, responsive popup, no runtime exceptions');
}finally{for(const job of pending.values())clearTimeout(job.timer);socket?.close();await stopTestBrowser(browser);await rm(profile,{recursive:true,force:true})}
