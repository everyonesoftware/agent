// Actual production HTML; isolated synthetic Brave API fixture. No live provider calls.
import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {existsSync} from 'node:fs';
import {readFile,mkdtemp,rm,writeFile,mkdir} from 'node:fs/promises';
import {createServer} from 'node:http';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
const root=new URL('../',import.meta.url);
const source=await readFile(new URL('page.go',root),'utf8');
const html=source.slice(source.indexOf('`')+1,source.lastIndexOf('`')).replaceAll('{{APP_VERSION}}','42').replaceAll('{{APP_BUILD_ID}}','fixture');
const pwa=await readFile(new URL('pwa_updates.go',root),'utf8');
let settings={configured:false,provider:'Brave Search',key_suffix:'',revision:'',verified_at:'',last_checked_at:'',status:'not_configured'};
let mode='',delay=0,auth=true; const calls=[];
const server=createServer(async(req,res)=>{
 const url=new URL(req.url,'http://localhost');
 const reply=(status,data)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(data));};
 if(url.pathname==='/'){res.end(html);return;}
 if(url.pathname==='/pwa-updates.js'){res.writeHead(200,{'Content-Type':'text/javascript'});res.end(pwa.slice(pwa.indexOf('`')+1,pwa.lastIndexOf('`')));return;}
 if(url.pathname.startsWith('/api/research/settings')){
  let payload={}; if(req.method==='POST'){let s='';for await(const c of req)s+=c;payload=JSON.parse(s);}
  calls.push({path:url.pathname,method:req.method,query:url.search,keys:Object.keys(payload),revision:payload.expected_revision});
  const action=url.pathname.split('/').at(-1);
  if(req.method==='GET'){if(mode==='offline'||mode==='lostackoffline'){reply(503,{error:'offline'});return;}reply(200,{settings});return;}
  if(mode==='conflict'){settings={...settings,revision:'external'};reply(409,{error:'changed',settings});return;}
  if(payload.expected_revision!==settings.revision){reply(409,{error:'changed',settings});return;}
  if(mode==='invalid'||mode==='unavailable'){reply(mode==='invalid'?422:503,{error:'Fixture failure',code:mode==='invalid'?'invalid_key':'unavailable',settings});return;}
  if(action==='remove')settings={...settings,configured:false,key_suffix:'',status:'not_configured',revision:settings.revision+'r'};
  else settings={...settings,configured:true,key_suffix:action==='verify'?payload.api_key.slice(-4):settings.key_suffix,revision:settings.revision+'v',status:'connected',verified_at:'2026-11-12T18:00:00Z',last_checked_at:'2026-11-12T18:00:00Z'};
  await new Promise(r=>setTimeout(r,delay));
  if(mode==='lostack'||mode==='lostackoffline'){res.destroy();return;}
  if(mode==='http503ack'){reply(503,{error:'Gateway lost the acknowledgment after the write.'});return;}
  reply(200,{settings});return;
 }
 if(url.pathname==='/api/auth/me'){reply(auth?200:401,auth?{id:'fixture',username:'Test'}:{error:'signed out'});return;}
 if(url.pathname==='/api/auth/logout'){auth=false;reply(200,{});return;}
 if(url.pathname==='/api/auth/status'){reply(200,{connected:true,oauth_selected:true,plan_usage_enabled:true});return;}
 if(url.pathname==='/api/auth/profiles'){reply(200,{active_profile_id:'p1',profiles:[{id:'p1',label:'Fixture',connected:true,oauth_selected:true}]});return;}
 if(url.pathname==='/api/models'){reply(200,{models:[{id:'fixture',name:'Fixture'}]});return;}
 if(url.pathname==='/api/conversations'){reply(200,{conversations:[]});return;}
 if(url.pathname==='/version.json'){reply(200,{version:'42',buildId:'fixture'});return;}
 if(url.pathname==='/api/usage/status'){reply(200,{state:'unavailable',available:false});return;}
 reply(404,{error:'fixture route'});
});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const origin='http://127.0.0.1:'+server.address().port;
const browser=process.env.UI_BROWSER||['C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe','C:/Program Files/Google/Chrome/Application/chrome.exe','/usr/bin/google-chrome','/usr/bin/chromium','/usr/bin/chromium-browser'].find(existsSync);
assert.ok(browser,'Chromium required');
const profile=await mkdtemp(join(process.env.TMPDIR||tmpdir(),'research-settings-'));
const child=spawn(browser,['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--remote-debugging-port=0','--user-data-dir='+profile,'about:blank'],{stdio:'ignore'});
let socket,seq=0;const pending=new Map(),errors=[];
async function until(fn,msg){const deadline=Date.now()+18000;while(Date.now()<deadline){if(await fn())return;await new Promise(r=>setTimeout(r,35));}throw Error(msg);}
function cdp(method,params={}){return new Promise((resolve,reject)=>{const id=++seq;pending.set(id,{resolve,reject});socket.send(JSON.stringify({id,method,params}));});}
async function js(expression){const r=await cdp('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(r.exceptionDetails)throw Error(r.exceptionDetails.exception?.description||r.exceptionDetails.text);return r.result.value;}
const click=id=>js(`document.getElementById('${id}').click()`);
const wait=exp=>until(()=>js(`(()=>{try{return !!(${exp})}catch{return false}})()`),exp);
const fill=()=>js("(()=>{const e=document.querySelector('#research-api-key');e.value='synthetic-fixture-only-1234';e.dispatchEvent(new Event('input',{bubbles:true}));})()");
const settled=()=>wait("!document.querySelector('#research-key-verify').disabled");
const ok=(v,m)=>{assert.ok(v,m);console.log('PASS '+m);};
async function key(key,code=key){const windowsVirtualKeyCode=key==='Escape'?27:key==='Tab'?9:0;await cdp('Input.dispatchKeyEvent',{type:'keyDown',key,code,windowsVirtualKeyCode});await cdp('Input.dispatchKeyEvent',{type:'keyUp',key,code,windowsVirtualKeyCode});}
try{
 let port;await until(async()=>{try{port=(await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0];return true;}catch{return false;}},'browser start');
 const tabs=await(await fetch('http://127.0.0.1:'+port+'/json/list')).json();socket=new WebSocket(tabs.find(t=>t.type==='page').webSocketDebuggerUrl);
 await new Promise(r=>socket.onopen=r);socket.onmessage=e=>{const d=JSON.parse(e.data);if(d.method==='Runtime.exceptionThrown')errors.push(d.params.exceptionDetails.text);const p=pending.get(d.id);if(p){pending.delete(d.id);d.error?p.reject(Error(d.error.message)):p.resolve(d.result);}};
 await cdp('Page.enable');await cdp('Runtime.enable');
 if(process.env.UI_LOCALE)await cdp('Emulation.setLocaleOverride',{locale:process.env.UI_LOCALE});
 await cdp('Page.navigate',{url:origin});
 await wait("!document.querySelector('#chat-app').hidden && !document.querySelector('#model-select').disabled");
 ok(!calls.length,'Ordinary chat does not request research settings');
 await click('settings-button');await wait("document.querySelector('#research-settings-card') && !document.querySelector('#research-key-form').hidden");await settled();
 ok(calls.length===1&&calls[0].method==='GET','Opening Settings reads metadata only');
 ok(await js("document.querySelector('#research-usage-panel')?.hidden === true"),'Usage is hidden until a key is configured');
 ok(await js("Array.from(document.querySelectorAll('.settings-content > .settings-card')).map(e=>e.querySelector('h2').textContent).join('|')==='Web app updates|ChatGPT connection|Web research|Reset password|Sign out'"),'Card order preserves existing settings');
 await fill();await click('research-key-show');ok(await js("document.querySelector('#research-api-key').type==='text' && window.agentUpdateSafety().unsaved"),'Shown candidate remains an unsaved draft');
 for(const width of [1280,390,320]){
  await cdp('Emulation.setDeviceMetricsOverride',{width,height:850,deviceScaleFactor:1,mobile:width<600});
  await js("document.querySelector('#research-settings-card').scrollIntoView({block:'start'})");
  ok(await js("document.documentElement.scrollWidth<=innerWidth && document.querySelector('#research-settings-card').scrollWidth<=document.querySelector('#research-settings-card').clientWidth"),'No overflow at '+width);
  ok(await js("['research-key-show','research-key-verify','research-key-cancel'].every(id=>document.getElementById(id).getBoundingClientRect().height>=44)"),'44px controls at '+width);
  await js("document.querySelector('#research-api-key').focus()");await key('Tab');ok(await js("document.activeElement.id==='research-key-show'"),'Keyboard reaches Show at '+width);
  if(process.env.UI_SCREENSHOTS){await mkdir(process.env.UI_SCREENSHOTS,{recursive:true});const {data}=await cdp('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.UI_SCREENSHOTS,'research-settings-'+width+'.png'),Buffer.from(data,'base64'));}
 }
 mode='invalid';await click('research-key-verify');await settled();ok(await js("!!document.querySelector('#research-key-error').textContent && document.querySelector('#research-api-key').value.length>0"),'Invalid key is actionable and not saved');
 mode='unavailable';await click('research-key-verify');await settled();ok(!settings.configured,'Unavailable verification does not save');
 mode='';await click('research-key-verify');await wait("!document.querySelector('#research-key-saved').hidden && document.querySelector('#research-api-key').value===''");
 ok(await js("document.querySelector('#research-key-suffix').textContent.includes('1234') && !document.querySelector('#research-settings-badge').textContent.match(/^Connected$/)"),'Saved suffix only; verification is not live availability');
 await click('research-key-replace');ok(await js("!document.querySelector('#research-key-form').hidden && document.querySelector('#research-api-key').value===''") ,'Replace opens empty input');
 await fill();mode='invalid';await click('research-key-verify');await settled();ok(settings.configured&&await js("!document.querySelector('#research-key-saved').hidden"),'Failed replacement keeps old saved key visible');
 await click('research-key-cancel');ok(await js("document.querySelector('#research-api-key').value==='' && document.activeElement.id==='research-key-replace'"),'Cancel clears draft and restores focus');
 mode='';const checks=calls.length;await click('research-key-check');await wait("!document.querySelector('#research-key-check').disabled");ok(calls.slice(checks).filter(c=>c.method==='POST').length===1,'Test connection sends exactly one check');
 ok(await js("!document.querySelector('#research-usage-panel').hidden && /No usage snapshot yet/.test(document.querySelector('#research-usage-windows').textContent)"),'Missing headers show unknown usage, never zero');
 const observed='2026-11-12T18:42:00Z',future='2099-12-01T08:00:00Z';
 const quota=(remaining,limit=2000,reset=future)=>({observed_at:observed,windows:[{limit:1,remaining:0,reset_at:'2026-11-12T18:42:01Z',window_seconds:1},{limit,remaining,reset_at:reset,window_seconds:2592000}]});
 async function loadUsage(usage,expected){
  settings={...settings,usage};const before=calls.length;
  await click('back-to-chat');await click('settings-button');await settled();
  await wait("document.querySelector('#research-usage-windows').textContent.includes("+JSON.stringify(expected)+")");
  ok(calls.slice(before).length===1&&calls.at(-1).method==='GET','Usage snapshot reads metadata only');
 }
 await loadUsage(quota(1380),await js('(1380).toLocaleString()'));
 ok(await js("document.querySelector('#research-usage-windows [role=progressbar]').getAttribute('aria-valuenow')==='1380' && document.querySelector('#research-usage-windows').textContent.includes((620).toLocaleString()+' of '+(2000).toLocaleString()+' used')"),'Brave quota renders remaining, used and accessible progress');
 ok(await js("!/running low|No requests remaining/.test(document.querySelector('#research-usage-windows').textContent) && /1-second/.test(document.querySelector('#research-usage-windows').textContent)"),'Burst window is separate and never treated as exhausted monthly quota');
 ok(await js("/Not live/.test(document.querySelector('#research-usage-panel').textContent) && /Snapshot/.test(document.querySelector('#research-usage-panel').textContent) && /Other apps/.test(document.querySelector('#research-usage-panel').textContent)"),'Snapshot age and shared-key caveats are visible');
 ok(await js("(()=>{const a=document.querySelector('#research-usage-dashboard');return a.href==='https://api-dashboard.search.brave.com/' && a.target==='_blank' && a.rel.includes('noopener') && a.referrerPolicy==='no-referrer'})()"),'Dashboard opens safely without a credential or referrer');
 for(const width of [1280,412,390,320]){
  await cdp('Emulation.setDeviceMetricsOverride',{width,height:915,deviceScaleFactor:1,mobile:width<600});
  await js("document.querySelector('#research-usage-panel').scrollIntoView({block:'center'})");
  ok(await js("document.documentElement.scrollWidth<=innerWidth && document.querySelector('#research-settings-card').scrollWidth<=document.querySelector('#research-settings-card').clientWidth && document.querySelector('#research-usage-dashboard').getBoundingClientRect().height>=44"),'Usage layout and dashboard touch target at '+width);
  if(process.env.UI_SCREENSHOTS){const {data}=await cdp('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.UI_SCREENSHOTS,'research-usage-'+width+'.png'),Buffer.from(data,'base64'));}
 }
 await loadUsage(quota(80),'running low');
 await loadUsage(quota(0),'No requests remaining');
 await loadUsage(quota(0,2000,'2026-01-01T00:00:00Z'),'Reset time has passed');
 ok(await js("!/No requests remaining/.test(document.querySelector('#research-usage-windows').textContent)"),'Past reset never claims a live exhausted quota or invents replenishment');
 await loadUsage(quota(0,0),'Unlimited');
 ok(await js("!document.querySelector('#research-usage-windows [role=progressbar]')"),'Unlimited quota has no misleading zero-limit progress bar');
 await loadUsage({observed_at:observed,windows:[{limit:1,remaining:0,reset_at:future,window_seconds:1}]},'No quota snapshot yet');
 await loadUsage({observed_at:observed,windows:[{limit:2000,remaining:-1,reset_at:future,window_seconds:2592000}]},'No usage snapshot yet');
 await loadUsage(null,'No usage snapshot yet');
 await click('research-key-remove');ok(await js("document.querySelector('#research-key-remove-dialog').scrollWidth<=document.querySelector('#research-key-remove-dialog').clientWidth && document.querySelector('#research-key-remove-dialog').getBoundingClientRect().bottom<=innerHeight"),'Remove confirmation fits 320px');await key('Escape');await wait("!document.querySelector('#research-key-remove-dialog').open");ok(settings.configured&&await js("!document.querySelector('#research-key-remove-dialog').open"),'Escape keeps saved key');
 await click('research-key-remove');await click('research-key-keep');ok(settings.configured,'Keep key cancels removal');
 await click('research-key-remove');mode='conflict';await click('research-key-remove-confirm');await wait("!document.querySelector('#research-key-remove-dialog').open");await settled();ok(settings.configured&&await js("/review/i.test(document.querySelector('#research-key-error').textContent)"),'Conflict refreshes metadata and requires review');
 mode='lostack';await click('research-key-replace');await fill();await click('research-key-verify');await wait("document.querySelector('#research-key-form').hidden");ok(settings.configured,'Lost save acknowledgment reconciles metadata without replay');
 mode='offline';await click('back-to-chat');await click('settings-button');await wait("!document.querySelector('#research-settings-retry').hidden");ok(await js("!/verified|connected/i.test(document.querySelector('#research-settings-badge').textContent) && document.querySelector('#research-usage-panel').hidden"),'Metadata failure never shows stale connected badge or usage');
 mode='';await click('research-settings-retry');await wait("!document.querySelector('#research-key-replace').disabled && !document.querySelector('#research-key-saved').hidden");
 // A lost acknowledgment plus disconnected reconciliation must never claim Connected.
 mode='lostackoffline';delay=12500;await click('research-key-replace');await fill();const beforeTimeout=calls.length;
 await click('research-key-verify');await wait("!document.querySelector('#research-settings-retry').hidden");
 ok(await js("/unknown/i.test(document.querySelector('#research-settings-badge').textContent) && /outcome unknown/i.test(document.querySelector('#research-key-error').textContent) && !window.agentUpdateSafety().pending"),'Bounded timeout with failed reconciliation shows unknown outcome');
 ok(calls.slice(beforeTimeout).filter(c=>c.method==='POST').length===1,'Ambiguous mutation is never automatically replayed');
 mode='';delay=0;await click('research-settings-retry');await wait("!document.querySelector('#research-key-replace').disabled && !document.querySelector('#research-key-saved').hidden");
 const beforeReload=calls.length;await cdp('Page.reload');await wait("!document.querySelector('#research-key-saved').hidden && !document.querySelector('#research-key-replace').disabled");
 ok(calls.slice(beforeReload).length===1&&calls.at(-1).method==='GET','Page refresh loads saved metadata without a Brave test');
 await click('research-key-replace');await fill();await click('back-to-chat');ok(await js("document.querySelector('#research-api-key').value===''") ,'Leaving Settings clears candidate');
 await click('settings-button');await wait("!document.querySelector('#research-key-remove').disabled");
 mode='';delay=350;await click('research-key-remove');await click('research-key-remove-confirm');await wait("window.agentUpdateSafety().pending");ok(await js("document.querySelector('#research-key-check').disabled"),'Inflight mutation blocks overlapping controls and reload');
 await wait("!document.querySelector('#research-key-form').hidden && !document.querySelector('#research-key-verify').disabled");ok(!settings.configured&&await js("document.querySelector('#research-usage-panel').hidden && !document.querySelector('#research-usage-windows').textContent"),'Confirmed removal clears usage and updates metadata');delay=0;
 mode='http503ack';await fill();await click('research-key-verify');await wait("!window.agentUpdateSafety().pending");
 ok(settings.configured && await js("!document.querySelector('#research-key-saved').hidden && document.querySelector('#research-api-key').value===''") ,'HTTP 503 without authoritative metadata reconciles a committed save');
 await click('research-key-remove');await click('research-key-remove-confirm');await wait("!window.agentUpdateSafety().pending");
 ok(!settings.configured && await js("!document.querySelector('#research-key-form').hidden && document.querySelector('#research-key-saved').hidden"),'HTTP 503 without authoritative metadata reconciles a committed removal');
 mode='';
 await fill();await click('logout-button');await wait("!document.querySelector('#auth-gate').hidden");ok(await js("document.querySelector('#research-api-key').value===''") ,'Sign out clears candidate');
 ok(await js("!JSON.stringify(localStorage).includes('synthetic-fixture') && !JSON.stringify(sessionStorage).includes('synthetic-fixture') && !location.href.includes('synthetic-fixture')"),'No key in storage or URL');
 ok(calls.every(c=>!c.query)&&errors.length===0,'No credential queries or runtime errors');
 console.log('PASS Brave Search settings browser interactions');
}finally{socket?.close();child.kill();server.closeAllConnections();await new Promise(r=>server.close(r));await new Promise(r=>setTimeout(r,300));await rm(profile,{recursive:true,force:true,maxRetries:8,retryDelay:200});}
