// Usage-history regression tests against the actual embedded production page.
// node tests/usage-history-ui.mjs; add --browser for isolated Chromium fixtures.
import assert from 'node:assert/strict';
import { readFile, mkdtemp, rm } from 'node:fs/promises';
import vm from 'node:vm';
import { createServer } from 'node:http';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { spawnTestBrowser, waitForTestBrowser, stopTestBrowser } from './browser-launcher.mjs';
const source = await readFile(new URL('../page.go', import.meta.url), 'utf8');
const html = source.slice(source.indexOf('`') + 1, source.lastIndexOf('`'));
assert.match(html, /id="usage-history-open-chatgpt"[^>]*aria-haspopup="dialog"/, 'Allowance needs a usage-history trigger');
assert.match(html, /id="usage-history-open-brave"[^>]*aria-haspopup="dialog"/, 'Brave usage needs its own history trigger');
assert.match(html, /<dialog[^>]*id="usage-history-dialog"/, 'History must use a native modal');
console.log('PASS provider-scoped native modal entry points');
const modelSource = html.split('// BEGIN usage history model')[1]?.split('// END usage history model')[0];
assert.ok(modelSource, 'Chart needs a testable history model');
const context = vm.createContext({});
vm.runInContext(modelSource + '\nthis.model = usageHistoryModel;', context);
const now = 2000000000;
const points = Array.from({length:61}, (_, i) => ({at:now-3600+i*60,remaining_percent:100-i,reset_at:now+7200}));
const data = {provider:'chatgpt',scope:'weekly',observed_at:now,tracking_started_at:now-86400,period_start:now-86400,reset_at:now+7200,remaining_percent:40,points,stale:false,gap_seconds:180};
let m = context.model(data, '24h', now);
assert.equal(m.segments.length, 1);
assert.equal(m.forecast.end.at, now+2400, 'Projection must stop at depletion before reset');
assert.equal(m.forecast.end.remaining_percent, 0);
assert.deepEqual(Array.from(m.milestones.filter(p=>p.kind==='milestone'), p=>p.remaining_percent), [90,75,50]);
assert.equal(context.model({...data,reset_at:now+600,points:points.map(p=>({...p,reset_at:now+600}))},'24h',now).forecast.end.at,now+600);
assert.equal(context.model({...data,stale:true},'24h',now).forecast,null,'Stale observations cannot support a forecast');
assert.equal(context.model({...data,remaining_percent:null},'24h',now).forecast,null);
assert.equal(context.model({...data,points:points.slice(-4)},'24h',now).forecast,null,'A few minutes are not an hour');
const gapped = points.filter((_,i)=>i<20||i>30);
m=context.model({...data,points:gapped},'24h',now);
assert.equal(m.segments.length,2,'Do not draw invented history through missing samples');
assert.equal(m.forecast,null,'Do not estimate across a recent-hour gap');
const resetPoints=points.map((p,i)=>({...p,reset_at:i<30?now-1800:now+7200,remaining_percent:i<30?100-i:130-i}));
m=context.model({...data,remaining_percent:70,points:resetPoints},'24h',now);
assert.equal(m.segments.length,2,'Reset must split history');assert.equal(m.forecast,null);
const brave={...data,provider:'brave',gap_seconds:0,points:[{at:now-7200,remaining_percent:100},{at:now-1800,remaining_percent:90},{at:now,remaining_percent:80}],remaining_percent:80};
m=context.model(brave,'week',now);assert.equal(m.segments.length,1);assert.equal(m.forecast.rate,20/3600,'Ledger uses the last known hour-start balance, not invented interpolation');
const sameSecond={...brave,points:[{at:now-7200,remaining_percent:100},{at:now-7200,remaining_percent:95},{at:now-1800,remaining_percent:90},{at:now,remaining_percent:80}]};
assert.equal(context.model(sameSecond,'week',now).segments[0][0].remaining_percent,95,'Multiple ledger changes in one second retain the final balance, not the pre-event baseline');
assert.equal(context.model({...brave,remaining_percent:null,points:[]},'period',now).forecast,null);
assert.equal(context.model({...data,period_start:0,tracking_started_at:'<invalid timestamp>'},'period',now).start,now,'Malformed timestamps must never become chart markup');
console.log('PASS observed-only history, gap/reset boundaries, recent-hour forecast and endpoint limits');
const controllerSource=html.split('// BEGIN usage history controller')[1]?.split('// END usage history controller')[0];
assert.ok(controllerSource,'History modal must load scoped server observations');
class Element {
  constructor(id) {this.id=id;this.hidden=false;this.open=false;this.value='';this.textContent='';this.innerHTML='';this.style={};this.dataset={};this.clientWidth=800;this.listeners={};this.classList={toggle(){},remove(){}};}
  addEventListener(type,fn){this.listeners[type]=fn;}
  setAttribute(key,value){this[key]=value;}
  getAttribute(key){return this[key];}
  replaceChildren(){this.children=[];}
  append(...nodes){(this.children ||= []).push(...nodes);}
  querySelectorAll(){return [];}
  showModal(){this.open=true;}
  close(){this.open=false;this.listeners.close?.({});}
  focus(){this.focused=true;}
  getBoundingClientRect(){return {left:0,top:0,width:800,right:800,bottom:400};}
}
const nodes=new Map();const el=id=>{if(!nodes.has(id))nodes.set(id,new Element(id));return nodes.get(id);};
let requests=[],delayed=null,timers=new Map(),timerID=0,metadataRenders=0;
const dom={hidden:false,getElementById:el,createElement:tag=>new Element(tag),addEventListener(){},activeElement:null};
const env=vm.createContext({document:dom,window:{addEventListener(){}},Date,AbortController,URLSearchParams,Number,Math,
  activeProfileID:'p1',settingsPanel:{hidden:false},chatApp:{hidden:false},usageData:null,
  searchBusy:false,searchData:{revision:'saved'},searchEpoch:0,
  acceptSearchSettings(){},renderSearchSettings(){metadataRenders++;},
  setTimeout(fn,ms){const id=++timerID;timers.set(id,{fn,ms});return id;},clearTimeout(id){timers.delete(id);},
  fetch:async(url,options)=>{requests.push({url,options});if(delayed)return delayed;return {ok:true,json:async()=>({...data,observed_at:Math.floor(Date.now()/1000),points:[],windows:[{id:'weekly',label:'Weekly window'},{id:'five',label:'5-hour window'}],window_id:'weekly'})};}
});
vm.runInContext(modelSource+'\n'+controllerSource+'\nthis.openHistory=usageHistoryOpen;this.closeHistory=usageHistoryClose;this.readHistory=usageHistoryRead;',env);
await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
assert.equal(requests[0].url,'/api/usage/history?profile_id=p1&range=24h');
assert.equal(el('usage-history-title').textContent,'ChatGPT usage');
assert.equal(el('usage-history-window').value,'weekly','Use server-selected longest window by default');
assert.equal(el('usage-history-window-label').hidden,false);
assert.match(el('usage-history-scope').textContent,/Weekly window/,'Show the human-readable window label, not an opaque profile scope');
el('usage-history-range').value='week';await el('usage-history-range').listeners.change();
assert.ok(requests.at(-1).url.includes('range=week'));assert.ok(requests.at(-1).url.includes('window_id=weekly'));
assert.equal(requests.every(r=>!r.options.method||r.options.method==='GET'),true,'History never triggers usage refresh mutations or Brave tests');
let resolve;delayed=new Promise(r=>{resolve=r;});const pending=env.readHistory();env.closeHistory();
assert.equal(requests.at(-1).options.signal.aborted,true,'Closing aborts an in-flight read');
resolve({ok:true,json:async()=>({...data,scope:'wrong late data'})});await pending;delayed=null;
assert.notEqual(el('usage-history-scope').textContent,'wrong late data');
env.fetch=async(url,options)=>{requests.push({url,options});return {ok:true,json:async()=>({...brave,remaining_percent:null,points:[],warning:'Agent cap is disabled'})};};
await env.openHistory('brave',el('usage-history-open-brave'));
assert.equal(requests.at(-1).url,'/api/research/history?range=24h');
assert.equal(el('usage-history-window-label').hidden,true,'No cross-provider or Brave window tabs');
assert.match(el('usage-history-message').textContent,/cap.*disabled/i);
assert.equal(el('usage-history-plot').innerHTML,'','Disabled cap has no fake percent chart');
env.closeHistory();assert.equal(el('usage-history-open-brave').focused,true,'Return focus to the provider entry point');
console.log('PASS provider routes, range/window selection, empty cap and stale response cancellation');
const refreshSource=html.split('// BEGIN usage history refresh')[1]?.split('// END usage history refresh')[0];
assert.ok(refreshSource,'Settings needs quiet automatic metadata/history refresh');
vm.runInContext(refreshSource+'\nthis.refreshMetadata=refreshSearchMetadata;this.startMonitor=startUsageMonitor;this.stopMonitor=stopUsageMonitor;',env);
requests=[];metadataRenders=0;
env.fetch=async(url,options)=>{requests.push({url,options});return {ok:true,json:async()=>({settings:{revision:'fresh'}})};};
await env.refreshMetadata();
assert.equal(requests[0].url,'/api/research/settings');assert.equal(metadataRenders,1);
assert.equal(env.searchBusy,false,'Read-only metadata refresh must not disable controls');
env.searchBusy=true;await env.refreshMetadata();assert.equal(requests.length,1,'Do not race an explicit mutation');env.searchBusy=false;
delayed=new Promise(r=>{resolve=r;});env.fetch=async(url,options)=>{requests.push({url,options});return delayed;};
const refreshing=env.refreshMetadata();env.stopMonitor();assert.equal(requests.at(-1).options.signal.aborted,true);
resolve({ok:true,json:async()=>({settings:{revision:'late'}})});await refreshing;
assert.equal(metadataRenders,1,'Late metadata must not render after leaving/logging out');delayed=null;
env.startMonitor();assert.ok([...timers.values()].some(t=>t.ms===30000),'Settings refresh cadence is 30 seconds');env.stopMonitor();
const pollSource=html.match(/function usagePoll\(\) \{[\s\S]*?\n      \}/)[0];
let pollDelay=0;
const pollEnv=vm.createContext({usageVisible:()=>true,usageData:{state:'connected'},usageTimer:null,clearTimeout(){},setTimeout(fn,ms){pollDelay=ms;return 1;}});
vm.runInContext(pollSource+'\nusagePoll();',pollEnv);assert.equal(pollDelay,30000,'Connected quotas should refresh automatically, not only pending sign-ins');
console.log('PASS quiet metadata refresh, 30-second polling and abort/epoch protection');
env.Date=class extends Date{static now(){return now*1000;}};
env.fetch=async()=>({ok:true,json:async()=>({...data,window_id:'weekly',windows:[{id:'weekly',label:'Weekly'}]})});
await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
el('usage-history-dialog').listeners.close({});
assert.equal(el('usage-history-dialog').open,true,'A queued close event from a previous opening must not close the new modal');
assert.doesNotMatch(el('usage-history-note').textContent,/Forecast stops/,'Keep detailed forecast assumptions out of the compact visible footer');
const chartHTML=el('usage-history-plot').innerHTML;
assert.match(chartHTML, /id="usage-history-forecast"[^>]*stroke-dasharray="5 6"/);
assert.equal((chartHTML.match(/class="usage-history-milestone"/g)||[]).length,5,'All 90/75/50/25/10 crossings always visible, without a toggle');
assert.match(chartHTML,/1 hour ago/,'Show a sparse hourly marker in 24h view');assert.doesNotMatch(chartHTML,/10 min/);
const svg=el('usage-history-chart');
const key=k=>svg.onkeydown({key:k,target:svg,preventDefault(){},stopPropagation(){}});
key('Home');assert.match(el('usage-history-tip-text').textContent,/100%/);key('Enter');assert.equal(el('usage-history-unpin').hidden,false);
svg.onpointerleave();assert.equal(el('usage-history-tooltip').hidden,false,'Pinned selection survives pointer leave');
key('Escape');assert.equal(el('usage-history-tooltip').hidden,true);assert.equal(el('usage-history-dialog').open,true);
svg.onpointerdown({button:0,pointerId:1,clientX:400});svg.onpointermove({clientX:500});svg.onpointerup({pointerId:1,clientX:500});assert.equal(el('usage-history-unpin').hidden,false,'Pointer drag release pins a real point');
key('Escape');key('Escape');assert.equal(el('usage-history-dialog').open,false,'A second Escape closes the modal after clearing the chart selection');
assert.ok(html.includes('#usage-history-dialog::backdrop'),'Native modal needs approved dark backdrop');
assert.ok(html.includes('.history-headline > span { margin-left:auto;'),'Reset belongs on the right of current remaining');
console.log('PASS threshold dots, sparse markers, keyboard/pointer pinning and modal presentation');
assert.match(html, /id="usage-history-open-chatgpt"[^>]* disabled/,'History cannot open while the active profile is still being discovered');
const stopSource=html.match(/function stopUsage\([^)]*\) \{[\s\S]*?\n      \}/)[0];
const syncSource=html.match(/function syncUsage\(\) \{[\s\S]*?\n      \}/)[0];
const visibleSource=html.match(/function usageVisible\(\) \{[^\n]*\}/)[0];
let closed=0,statusReads=0;
const usageCalls=[];
const pauseEnv=vm.createContext({usageEpoch:0,usageTimer:null,usageBusy:false,usageProfile:'p1',usageData:{state:'connected'},usagePanel:{hidden:false},usageController:null,historyDialog:{open:true},
  settingsPanel:{hidden:false},chatApp:{hidden:false},document:{hidden:false},
  usageHistoryClose(){closed++;},stopUsageMonitor(){},startUsageMonitor(){},clearTimeout(){},activeProfileID:'p1',historyEl:el,usageRequest(...args){statusReads++;usageCalls.push(args);}});
vm.runInContext(visibleSource+'\n'+stopSource+'\n'+syncSource+'\nstopUsage(true);',pauseEnv);
assert.equal(closed,0,'Hiding the document should pause reads, not close its native modal');
assert.equal(pauseEnv.usageData.state,'connected','Keep cached quota on backgrounding');
vm.runInContext('syncUsage();',pauseEnv);assert.equal(statusReads,1,'Foreground reloads status via normal TTL path');
console.log('PASS discovery gate and cached foreground/background lifecycle');
// Exercise Settings visibility changes while the same profile remains cached.
const cachedUsage=pauseEnv.usageData;
usageCalls.length=0;
vm.runInContext('settingsPanel.hidden=true; syncUsage();',pauseEnv);
assert.equal(pauseEnv.usagePanel.hidden,true,'Closing Settings hides the quota panel');
assert.equal(pauseEnv.usageProfile,'p1','Hidden sync retains the selected usage profile');
assert.equal(usageCalls.length,0,'Closing Settings must not start a request');
vm.runInContext('settingsPanel.hidden=false; syncUsage();',pauseEnv);
assert.equal(pauseEnv.usagePanel.hidden,false,'Reopening Settings for the same profile restores the quota panel');
assert.equal(!pauseEnv.settingsPanel.hidden&&!pauseEnv.usagePanel.hidden&&!el('usage-history-open-chatgpt').hidden,true,'Reopening Settings restores the quota history entry point');
assert.equal(pauseEnv.usageData,cachedUsage,'Reopening retains cached quota instead of restarting login');
assert.deepEqual(usageCalls,[['status',undefined,true]],'Reopening issues exactly one quiet status read, with no login restart or duplicate request');
console.log('PASS same-profile Settings close/reopen restores quota and history without duplicate reads or login restart');
const budgetSource=html.match(/function renderSearchBudget\(known\) \{[\s\S]*?\n      \}/)[0];
const budget={enabled:true,maximum:1000,period:'month',timezone:'UTC',revision:'saved',used:10,reserved:0,start_at:'2026-11-01T00:00:00Z',reset_at:'2026-12-01T00:00:00Z',tracking_started_at:'2026-11-01T00:00:00Z'};
let dirtyWrites=0;
for(const key of ['maximum','period','timezone'])Object.defineProperty(el('research-budget-'+key),'value',{configurable:true,get(){return key==='maximum'?'1234':key==='period'?'day':'America/Los_Angeles';},set(){dirtyWrites++;}});
el('research-budget-progress').firstElementChild={style:{}};
const budgetEnv=vm.createContext({searchData:{budget},searchBusy:false,searchBudgetDraft:{enabled:false,maximum:'1234',period:'day',timezone:'America/Los_Angeles'},searchBudgetBase:{},searchBudgetRevision:'saved',searchBudgetMessage:'',searchBudgetFields:['enabled','maximum','period','timezone'],searchBudgetDirty:()=>true,searchEl:el,searchBudgetTime:()=>'',searchBudgetValues:()=>budget});
vm.runInContext(budgetSource+'\nrenderSearchBudget(true);',budgetEnv);
assert.equal(dirtyWrites,0,'Metadata refresh must not rewrite dirty cap fields, including focused timezone text');
console.log('PASS cap drafts are not rewritten by automatic metadata rendering');
// Native dialog, SVG pointer capture and real keyboard focus are not emulated by
// the VM. --browser exercises the exact production controller/CSS in isolation;
// the production-router suite separately covers auth, storage and ledger reads.
if (process.argv.includes('--browser')) {
  const style=html.match(/<style>([\s\S]*?)<\/style>/)[1];
  const dialog=html.match(/<dialog id="usage-history-dialog"[\s\S]*?<\/dialog>/)[0];
  const buttons=['chatgpt','brave'].map(p=>html.match(new RegExp('<button id="usage-history-open-'+p+'"[^>]*>[\\s\\S]*?<\\/button>'))[0].replace(' disabled','')).join('');
  const page='<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><style>'+style+'</style>'+buttons+dialog+'<script>let activeProfileID="p1";const settingsPanel={hidden:false},chatApp={hidden:false};'+modelSource+controllerSource+'</script>';
  const calls=[];let capDisabled=false;
  const server=createServer((req,res)=>{
    if(req.url==='/'){res.writeHead(200,{'Content-Type':'text/html'});res.end(page);return;}
    calls.push(req.url);
    const t=Math.floor(Date.now()/1000),url=new URL(req.url,'http://127.0.0.1');
    const result={...data,scope:'ChatGPT · weekly allowance',observed_at:t,tracking_started_at:t-86400,period_start:t-86400,reset_at:t+7200,points:points.map(p=>({...p,at:p.at-now+t,reset_at:t+7200})),window_id:'weekly',windows:[{id:'weekly',label:'Weekly allowance'},{id:'five',label:'5-hour allowance'}]};
    if(url.pathname==='/api/research/history')Object.assign(result,{provider:'brave',scope:'Brave Search · Agent cap',gap_seconds:0,remaining_count:40,...(capDisabled?{remaining_percent:null,points:[],warning:'Agent cap is disabled'}:{})});
    if(url.searchParams.get('window_id')==='five')Object.assign(result,{scope:'ChatGPT · 5-hour allowance',window_id:'five'});
    res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(result));
  });
  await new Promise(r=>server.listen(0,'127.0.0.1',r));
  const profile=await mkdtemp(join(process.env.TMPDIR||tmpdir(),'agent-usage-ui-'));
  let browser,socket,id=0;const jobs=new Map(),errors=[];
  const cdp=(method,params={})=>new Promise((resolve,reject)=>{const n=++id,timer=setTimeout(()=>{jobs.delete(n);reject(new Error('CDP timeout '+method));},20000);jobs.set(n,{resolve,reject,timer});socket.send(JSON.stringify({id:n,method,params}));});
  const js=async expression=>{const out=await cdp('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(out.exceptionDetails)throw new Error(out.exceptionDetails.exception?.description||out.exceptionDetails.text);return out.result.value;};
  const until=async expression=>{const end=Date.now()+10000;while(Date.now()<end){if(await js(expression))return;await new Promise(r=>setTimeout(r,30));}throw new Error('Browser assertion timed out: '+expression);};
  try {
    browser=spawnTestBrowser(process.env.UI_BROWSER,['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--disable-extensions','--remote-debugging-port=0','--user-data-dir='+profile,'about:blank']);
    const ready=await waitForTestBrowser(browser,profile),targets=await(await fetch('http://127.0.0.1:'+ready.split('\n')[0]+'/json/list')).json();
    socket=new WebSocket(targets.find(t=>t.type==='page').webSocketDebuggerUrl);await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject;});
    socket.onmessage=e=>{const msg=JSON.parse(e.data);if(msg.method==='Runtime.exceptionThrown')errors.push(msg.params.exceptionDetails.text);const job=jobs.get(msg.id);if(!job)return;jobs.delete(msg.id);clearTimeout(job.timer);if(msg.error)job.reject(new Error(msg.error.message));else job.resolve(msg.result);};
    await cdp('Runtime.enable');await cdp('Page.enable');await cdp('Page.navigate',{url:'http://127.0.0.1:'+server.address().port});
    await until("!!document.getElementById('usage-history-open-chatgpt')");
    await js("document.getElementById('usage-history-open-chatgpt').click()");
    await until("!!document.getElementById('usage-history-forecast')");
    assert.equal(await js("document.activeElement.id"),'usage-history-close');
    assert.equal(await js("document.querySelectorAll('.usage-history-milestone').length"),5);
    await js("document.getElementById('usage-history-chart').focus()");
    const press=key=>cdp('Input.dispatchKeyEvent',{type:'keyDown',key,code:key});
    await press('Home');await press('Enter');assert.equal(await js("document.getElementById('usage-history-unpin').hidden"),false);
    await press('Escape');assert.equal(await js("document.getElementById('usage-history-tooltip').hidden"),true);assert.equal(await js("document.getElementById('usage-history-dialog').open"),true);
    const coords=await js("(()=>{const r=document.getElementById('usage-history-chart').getBoundingClientRect();return {x:r.left+r.width*.5,y:r.top+r.height*.5};})()");
    await cdp('Input.dispatchMouseEvent',{type:'mouseMoved',...coords});await cdp('Input.dispatchMouseEvent',{type:'mousePressed',...coords,button:'left',clickCount:1});await cdp('Input.dispatchMouseEvent',{type:'mouseMoved',x:coords.x+20,y:coords.y,button:'left',buttons:1});await cdp('Input.dispatchMouseEvent',{type:'mouseReleased',x:coords.x+20,y:coords.y,button:'left',clickCount:1});
    assert.equal(await js("document.getElementById('usage-history-unpin').hidden"),false);
    await js("(()=>{const w=document.getElementById('usage-history-window');w.value='five';w.dispatchEvent(new Event('change'));})()");await until("document.getElementById('usage-history-scope').textContent.includes('5-hour')");
    await js("(()=>{const r=document.getElementById('usage-history-range');r.value='week';r.dispatchEvent(new Event('change'));})()");await until("!!document.getElementById('usage-history-chart')");assert.ok(calls.some(c=>c.includes('range=week')&&c.includes('window_id=five')));
    for(const width of [1280,390,320]){
      await cdp('Emulation.setDeviceMetricsOverride',{width,height:844,deviceScaleFactor:1,mobile:width<500});
      await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
      assert.equal(await js("(()=>{const d=document.getElementById('usage-history-dialog'),r=d.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&d.scrollWidth<=d.clientWidth;})()"),true,'Modal must fit width '+width);
      assert.equal(await js("(()=>{const r=document.getElementById('usage-history-range').getBoundingClientRect(),c=document.getElementById('usage-history-close').getBoundingClientRect();return r.right<=c.left&&Math.abs(r.top-c.top)<8;})()"),true,'Range stays inline just before Close');
    }
    await js("document.getElementById('usage-history-close').click()");assert.equal(await js('document.activeElement.id'),'usage-history-open-chatgpt');
    capDisabled=true;await js("document.getElementById('usage-history-open-brave').click()");await until("document.getElementById('usage-history-message').textContent.includes('disabled')");
    assert.equal(await js("document.getElementById('usage-history-plot').childElementCount"),0);assert.equal(await js("document.getElementById('usage-history-window-label').hidden"),true);
    assert.deepEqual(errors,[]);assert.ok(calls.every(c=>!c.includes('/check')&&!c.includes('/refresh')));
    console.log('PASS native Chromium focus, threshold/pointer/keyboard interaction, scoped windows, empty cap and 1280/390/320px layout');
  } finally {for(const job of jobs.values())clearTimeout(job.timer);socket?.close();await stopTestBrowser(browser);await rm(profile,{recursive:true,force:true});await new Promise(r=>server.close(r));}
}
