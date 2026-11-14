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
 await capture('history-chatgpt-desktop');
 await js("(()=>{const r=document.getElementById('usage-history-range');r.value='period';r.dispatchEvent(new Event('change'))})()");await until("!!document.querySelector('#usage-history-dialog svg')",'Entire-period chart missing');await capture('history-chatgpt-period-desktop');await click('usage-history-close');
 await until("!!document.getElementById('usage-history-open-brave')&&!document.getElementById('usage-history-open-brave').disabled",'Brave history entry point unavailable');await click('usage-history-open-brave');await until("document.getElementById('usage-history-dialog').open&&document.getElementById('usage-history-dialog').textContent.includes('5')",'Brave ledger history did not load');assert.ok(await js("!document.getElementById('usage-history-dialog').innerText.includes('ChatGPT')"),'Brave popup leaked other provider');
 const history=await js("fetch('/api/research/history?range=24h').then(r=>r.json())");assert.equal(history.remaining_count,5);assert.equal(history.remaining_percent,50);assert.ok(history.points.length>=2);assert.equal(history.provider,'brave');
 await capture('history-brave-desktop');await cdp('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');assert.equal(await js('document.documentElement.scrollWidth<=innerWidth'),true,'Mobile page overflows');await capture('history-brave-mobile');await click('usage-history-close');
 // History reads must never create extra searches, inference, or budget events.
 const after=await js("fetch('/api/research/settings').then(r=>r.json())");assert.equal(after.settings.budget.used,5);assert.deepEqual(errors,[],'Runtime exceptions');
 console.log('PASS production Settings, scoped ChatGPT observations and Brave ledger, 24h/week/period options, responsive popup, no runtime exceptions');
}finally{for(const job of pending.values())clearTimeout(job.timer);socket?.close();await stopTestBrowser(browser);await rm(profile,{recursive:true,force:true})}
