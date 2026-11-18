// Usage-history regression tests against the actual embedded production page.
// node tests/usage-history-ui.mjs; add --browser for isolated Chromium fixtures.
import assert from 'node:assert/strict';
import { readFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import vm from 'node:vm';
import { createServer } from 'node:http';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { spawnTestBrowser, waitForTestBrowser, stopTestBrowser } from './browser-launcher.mjs';
import './usage-history-forecast.mjs';
const source = await readFile(new URL('../page.go', import.meta.url), 'utf8');
const html = source.slice(source.indexOf('`') + 1, source.lastIndexOf('`'));
assert.match(html, /id="usage-history-open-chatgpt"[^>]*aria-haspopup="dialog"/, 'Allowance needs a usage-history trigger');
assert.match(html, /id="usage-history-open-brave"[^>]*aria-haspopup="dialog"/, 'Brave usage needs its own history trigger');
assert.match(html, /<dialog[^>]*id="usage-history-dialog"/, 'History must use a native modal');
const historyMarkup=html.match(/<dialog id="usage-history-dialog"[\s\S]*?<\/dialog>/)[0];
assert.match(historyMarkup, /<details id="usage-history-details"[\s\S]*?<summary[^>]*aria-label="Forecast details"[\s\S]*?id="usage-history-rate"[\s\S]*?id="usage-history-note"[\s\S]*?<\/details>/, 'Forecast explanation and caveat belong in on-demand details');
assert.doesNotMatch(historyMarkup.match(/<header[\s\S]*?<\/header>/)[0], /usage-history-scope/, 'Remove the redundant provider/window subtitle from the header');
assert.doesNotMatch(historyMarkup, /<small>current remaining<\/small>/, 'The remaining value needs no redundant label');
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
assert.equal(context.model({...data,points:points.slice(-4)},'24h',now).forecast.seconds,180,'Use even a few minutes of available history, with its actual duration');
const gapped = points.filter((_,i)=>i<20||i>30);
m=context.model({...data,points:gapped},'24h',now);
assert.equal(m.segments.length,2,'Do not draw invented history through missing samples');
assert.equal(m.forecast.seconds,48*60,'Combine covered intervals on both sides, excluding the missing 12 minutes');
assert.equal(m.forecast.consumed,48,'Do not attribute the gap decrease to observed consumption');
assert.equal(m.displaySegments.length,1,'Connect readings less than an hour apart without changing forecast coverage');
assert.equal(m.gapConnectors.length,0);
const sparsePoints=[{at:now-7500,remaining_percent:80,reset_at:data.reset_at},{at:now-7200,remaining_percent:79,reset_at:data.reset_at},{at:now-3600,remaining_percent:70,reset_at:data.reset_at},{at:now,remaining_percent:60,reset_at:data.reset_at}];
const sparse=context.model({...data,remaining_percent:60,points:sparsePoints},'24h',now);
assert.equal(sparse.displaySegments.length,3,'An hour or more separates solid display segments');
assert.equal(sparse.displaySegments[0].length,2,'Five-minute readings connect solidly');
assert.equal(sparse.gapConnectors.length,2,'Connect long gaps with explicit dotted bridges');
assert.equal(sparse.forecast,null,'Display bridges must not supply forecast evidence');
assert.equal(context.model({...data,points:[sparsePoints[0],{...sparsePoints[1],at:now-3901}]},'24h',now).displaySegments.length,1,'A 3599-second gap remains solid');
const resetPoints=points.map((p,i)=>({...p,reset_at:i<30?now-1800:now+7200,remaining_percent:i<30?100-i:130-i}));
m=context.model({...data,remaining_percent:70,points:resetPoints},'24h',now);
assert.equal(m.segments.length,2,'Reset must split history');assert.equal(m.forecast.seconds,59*60,'Combine consumption measured separately within both reset periods');
assert.equal(m.displaySegments.length,2,'Display must still break at resets');
assert.equal(m.gapConnectors.length,0,'Never bridge a reset or balance increase');
const brave={...data,provider:'brave',gap_seconds:0,points:[{at:now-7200,remaining_percent:100},{at:now-1800,remaining_percent:90},{at:now,remaining_percent:80}],remaining_percent:80};
m=context.model(brave,'week',now);assert.equal(m.segments.length,1);assert.equal(m.forecastHourly.rate,20/3600,'Ledger uses the last known hour-start balance, not invented interpolation');
const sameSecond={...brave,points:[{at:now-7200,remaining_percent:100},{at:now-7200,remaining_percent:95},{at:now-1800,remaining_percent:90},{at:now,remaining_percent:80}]};
assert.equal(context.model(sameSecond,'week',now).segments[0][0].remaining_percent,95,'Multiple ledger changes in one second retain the final balance, not the pre-event baseline');
assert.equal(context.model({...brave,remaining_percent:null,points:[]},'period',now).forecast,null);
assert.equal(context.model({...data,period_start:0,tracking_started_at:'<invalid timestamp>'},'period',now).start,now,'Malformed timestamps must never become chart markup');
const dailyPoints = Array.from({length:1442}, (_,i) => ({at:now-86460+i*60,remaining_percent:i<=1381?64:64-(i-1381)/5,reset_at:now+604800}));
const dailyData = {...data,reset_at:now+604800,remaining_percent:52,points:dailyPoints};
let daily = context.model(dailyData,'24h',now);
assert.ok(daily.forecast,'A full day of recorded history supports the recent average');
assert.equal(daily.forecastHourly.rate,12/3600,'Hourly pace remains a separate detail');
assert.equal(daily.forecast.rate,12/86400,'Recent average includes idle hours');
assert.equal(daily.forecast.end.at,now+52/(12/86400),'Recent-average projection stops at depletion');
assert.equal(context.model({...dailyData,points:dailyPoints.slice(-61)},'24h',now).forecast.seconds,3600,'Short history is labeled with its measured duration, not a full day');
assert.equal(context.model({...dailyData,stale:true},'24h',now).forecast,null,'Stale readings disable the forecast');
assert.equal(context.model({...dailyData,points:dailyPoints.filter((_,i)=>i<100||i>110)},'24h',now).forecast.seconds,1428*60,'Missing coverage is excluded without discarding earlier observed idle time');
assert.equal(context.model({...dailyData,points:dailyPoints.map((p,i)=>({...p,reset_at:i<100?now-80000:p.reset_at}))},'24h',now).forecast.seconds,1439*60,'A reset excludes its crossing interval, not the previous recorded period');
const idleDaily=context.model({...dailyData,remaining_percent:52,points:dailyPoints.map(p=>({...p,remaining_percent:52}))},'24h',now);
assert.equal(idleDaily.forecast.end.at,dailyData.reset_at,'No daily consumption projects a flat balance until reset');
assert.equal(idleDaily.forecast.end.remaining_percent,52);
const emptyDaily=context.model({...dailyData,remaining_percent:0,points:dailyPoints.map(p=>({...p,remaining_percent:0}))},'24h',now);
assert.equal(emptyDaily.forecast.end.at,now,'An already depleted balance must not project remaining time until reset');
const idleHourData={...dailyData,remaining_percent:52,points:dailyPoints.map((p,i)=>({...p,remaining_percent:i<1381?64-(12*i/1381):52}))};
const idleHour=context.model(idleHourData,'24h',now);
assert.ok(idleHour.forecastHourly,'A fully observed idle hour supports an hourly detail');
assert.equal(idleHour.forecastHourly.rate,0);
assert.equal(idleHour.forecastHourly.end.at,idleHourData.reset_at);
assert.equal(idleHour.forecastHourly.end.remaining_percent,52);
assert.ok(idleHour.forecast.rate>0,'Earlier daily usage remains visible when the recent hour is idle');
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
  contains(node){return node===this;}
  showModal(){this.open=true;}
  close(){this.open=false;this.listeners.close?.({});}
  focus(){this.focused=true;}
  getBoundingClientRect(){return {left:0,top:0,width:800,height:400,right:800,bottom:400};}
}
const nodes=new Map();const el=id=>{if(!nodes.has(id))nodes.set(id,new Element(id));return nodes.get(id);};
let requests=[],delayed=null,timers=new Map(),timerID=0,metadataRenders=0;
const documentListeners={};
const dom={hidden:false,getElementById:el,createElement:tag=>new Element(tag),addEventListener(type,fn){documentListeners[type]=fn;},activeElement:null};
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
env.startMonitor();assert.ok([...timers.values()].some(t=>t.ms===60000),'Settings refresh cadence is one minute');env.stopMonitor();
// The open chart still refreshes on the same cadence with Settings closed.
env.settingsPanel.hidden=true;
env.fetch=async(url,options)=>{requests.push({url,options});return {ok:true,json:async()=>({...data,window_id:'weekly',windows:[{id:'weekly',label:'Weekly'}]})};};
await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
env.startMonitor();
const historyTick=[...timers.values()].find(t=>t.ms===60000);
assert.ok(historyTick,'Chart refresh cadence is one minute even with Settings closed');
requests=[];historyTick.fn();
await new Promise(resolve=>setImmediate(resolve));
assert.ok(requests.some(r=>r.url.startsWith('/api/usage/history?')),'Minute tick reads newly recorded chart points');
assert.ok([...timers.values()].some(t=>t.ms===60000),'Chart keeps polling each minute');
env.stopMonitor();env.closeHistory();env.settingsPanel.hidden=false;
const pollSource=html.match(/function usagePoll\(\) \{[\s\S]*?\n      \}/)[0];
let pollDelay=0;
const pollEnv=vm.createContext({usageVisible:()=>true,usageData:{state:'connected'},usageTimer:null,clearTimeout(){},setTimeout(fn,ms){pollDelay=ms;return 1;}});
vm.runInContext(pollSource+'\nusagePoll();',pollEnv);assert.equal(pollDelay,60000,'Connected quotas refresh once per minute');
pollEnv.usageData.state='pending';vm.runInContext('usagePoll();',pollEnv);
assert.equal(pollDelay,2000,'Sign-in approval checks remain responsive');
console.log('PASS quiet metadata/history refresh, one-minute polling with Settings open or closed and abort/epoch protection');
env.Date=class extends Date{static now(){return now*1000;}};
env.fetch=async()=>({ok:true,json:async()=>({...data,window_id:'weekly',windows:[{id:'weekly',label:'Weekly'}]})});
await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
el('usage-history-dialog').listeners.close({});
assert.equal(el('usage-history-dialog').open,true,'A queued close event from a previous opening must not close the new modal');
assert.doesNotMatch(el('usage-history-note').textContent,/Forecast stops/,'Keep detailed forecast assumptions out of the compact visible footer');
const chartHTML=el('usage-history-plot').innerHTML;
assert.match(el('usage-history-estimate').textContent,/May run out in about/,'Familiar shows the recent-average outcome, not an hourly headline');
assert.match(el('usage-history-basis').textContent,/Forecast:.*1 hour.*covered/,'Label forecast coverage, not all stored history');
assert.match(el('usage-history-budget').textContent,/Daily budget: 480% \/ day/,'Show the remaining allowance divided by time until reset');
assert.match(el('usage-history-average').textContent,/Short-term pace: 1,440% \/ day/);
assert.doesNotMatch(historyMarkup,/>1h pace<|>24h pace</,'Only the recent-average series appears in the legend');
assert.match(chartHTML,/class="usage-history-recorded"[^>]*d="[^"]* C /,'Production rendering uses a smooth curve for sampled readings');
const recordedPath=chartHTML.match(/class="usage-history-recorded" d="([^"]+)"/)[1];
assert.ok((recordedPath.match(/ C /g)||[]).length<30,'Production rendering uses time-smoothed knots rather than every minute reading');
assert.match(el('usage-history-note').textContent,/time-smoothed.*exact readings/,'Explain the visual smoothing without relabeling exact inspection readings');
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
env.fetch=async()=>({ok:true,json:async()=>dailyData});
await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
assert.doesNotMatch(el('usage-history-plot').innerHTML,/id="usage-history-forecast-daily"/,'The familiar chart has one forecast, not competing hourly and daily lines');
assert.match(el('usage-history-estimate').textContent,/May run out/);
assert.match(el('usage-history-rate').textContent,/idle time.*1 day/,'Explain that the average includes idle time and its actual duration');
assert.match(el('usage-history-hourly-rate').textContent,/Last hour: 12%/,'Hourly pace moves into details');
el('usage-history-chart').onkeydown({key:'End',target:el('usage-history-chart'),preventDefault(){},stopPropagation(){}});
assert.match(el('usage-history-tip-text').textContent,/Recent average/,'The single projection can be inspected and identified');
env.closeHistory();
env.fetch=async()=>({ok:true,json:async()=>idleHourData});
await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
assert.match(el('usage-history-hourly-rate').textContent,/Last hour: no usage recorded/,'Idle hourly details do not imply the whole forecast is flat');
const dailyPath=el('usage-history-plot').innerHTML.match(/id="usage-history-forecast" d="([^"]+)"/)[1];
const [,dailyX,dailyY]=dailyPath.match(/L ([\d.]+) ([\d.]+)/);
el('usage-history-chart').onpointerdown({button:0,pointerId:2,clientX:Number(dailyX),clientY:Number(dailyY)*400/330});
el('usage-history-chart').onpointerup({pointerId:2,clientX:Number(dailyX),clientY:Number(dailyY)*400/330});
assert.match(el('usage-history-tip-text').textContent,/Recent average.*pinned/,'Pointer can pin the average projection during an idle hour');
env.closeHistory();
assert.ok(html.includes('#usage-history-dialog::backdrop'),'Native modal needs approved dark backdrop');
assert.ok(html.includes('.history-headline > span { margin-left:auto;'),'Reset belongs on the right of current remaining');
console.log('PASS threshold dots, sparse markers, keyboard/pointer pinning and modal presentation');
for (const age of [61,120,180,181]) {
  env.Date=class extends Date{static now(){return (now+age)*1000;}};
  env.fetch=async()=>({ok:true,json:async()=>({...data,stale:age>180})});
  await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
  assert.match(el('usage-history-message').textContent,/last checked.*ago/i,'Explain the age of a delayed reading');
  if(age<=180) {
    assert.match(el('usage-history-estimate').textContent,/May run out/);
    assert.match(el('usage-history-budget').textContent,/Daily budget:/);
    assert.match(el('usage-history-message').textContent,/delayed/i);
  } else {
    assert.equal(el('usage-history-estimate').textContent,'Forecast unavailable');
    assert.match(el('usage-history-basis').textContent,/out of date/i,'Show the actual reason without requiring details');
    assert.equal(el('usage-history-budget').textContent,'Daily budget unavailable');
  }
  env.closeHistory();
}
env.Date=class extends Date{static now(){return now*1000;}};
console.log('PASS delayed updates remain useful and expired readings explain their unavailability');
env.fetch=async()=>({ok:true,json:async()=>({...brave,remaining_count:80})});
await env.openHistory('brave',el('usage-history-open-brave'));
assert.match(el('usage-history-plot').innerHTML,/class="usage-history-recorded"[^>]*d="[^"]* C /,'Brave uses the same time-smoothed curve as ChatGPT');
assert.doesNotMatch(el('usage-history-plot').innerHTML,/class="usage-history-recorded"[^>]*d="[^"]* H /,'Brave display is no longer a step line');
assert.match(el('usage-history-note').textContent,/Agent cap.*time-smoothed.*exact readings/,'Brave discloses display smoothing too');
assert.equal(el('usage-history-value').textContent,'80 searches left','Smoothing cannot alter the exact search count');
key('Home');key('ArrowRight');
assert.match(el('usage-history-tip-text').textContent,/90% remaining.*Recorded/,'Inspection retains the original ledger event balance');
assert.match(el('usage-history-hourly-rate').textContent,/Last hour: 20%/,'Ledger forecast math remains unchanged');
env.closeHistory();
console.log('PASS Brave smoothing with exact search counts, ledger inspection and forecasts');
env.fetch=async()=>({ok:true,json:async()=>({...data,remaining_percent:60,points:sparsePoints})});
await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
const sparseHTML=el('usage-history-plot').innerHTML;
assert.equal((sparseHTML.match(/class="usage-history-gap"/g)||[]).length,2,'Render dotted connectors across hour-long gaps');
assert.match(sparseHTML,/class="usage-history-gap"[^>]*stroke-dasharray="1 5"[^>]*stroke-linecap="round"/,'Missing-history bridges are dotted, distinct from the forecast');
assert.doesNotMatch(sparseHTML,/<circle[^>]*r="3"/,'Do not clutter sparse history with standalone filled sample circles');
assert.match(el('usage-history-note').textContent,/Dotted.*hour/i,'Explain long-gap connectors in details');
env.closeHistory();
console.log('PASS sparse display connections without sample dots or invented forecast evidence');
assert.match(html, /id="usage-history-open-chatgpt"[^>]* disabled/,'History cannot open while the active profile is still being discovered');
const stopSource=html.match(/function stopUsage\([^)]*\) \{[\s\S]*?\n      \}/)[0];
const syncSource=html.match(/function syncUsage\(\) \{[\s\S]*?\n      \}/)[0];
const visibleSource=html.match(/function usageVisible\(\) \{[^\n]*\}/)[0];
let closed=0,statusReads=0;
const usageCalls=[];
const pauseEnv=vm.createContext({usageEpoch:0,usageTimer:null,usageBusy:false,usageProfile:'p1',usageData:{state:'connected'},usagePanel:{hidden:false},usageController:null,historyDialog:{open:true},
  settingsPanel:{hidden:false},chatApp:{hidden:false},document:{hidden:false},deviceLogin:null,
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
env.fetch=async()=>({ok:true,json:async()=>({...data,sessions:[{start_at:now-3600,end_at:now,last_used_at:now,used_percent:60,current:true,estimated:true}]})});
await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
assert.match(el('usage-history-plot').innerHTML,/class="usage-history-session current"/,'Draw estimated sessions behind the recorded line');
assert.equal(el('usage-history-session-legend').hidden,false);
const sessionRect=el('usage-history-plot').innerHTML.match(/class="usage-history-session current"[^>]*x="([\d.]+)"[^>]*width="([\d.]+)"/);
const sessionX=Number(sessionRect[1])+Number(sessionRect[2])/2;
el('usage-history-chart').onpointerdown({button:0,pointerId:7,clientX:sessionX,clientY:180});
el('usage-history-chart').onpointerup({pointerId:7,clientX:sessionX,clientY:180});
assert.match(el('usage-history-tip-text').textContent,/Current session.*estimated[\s\S]*60% used/,'Cursor popup includes estimated session usage, not a footer caption');
assert.ok(el('usage-history-tooltip').style.top,'Position the popup near the inspected point');
const inspectedChart=el('usage-history-chart');inspectedChart.onkeydown({key:'Escape',target:inspectedChart,preventDefault(){},stopPropagation(){}});
const exactSampleX=Number(sessionRect[1])+Number(sessionRect[2])*40/60, exactSampleY=(293-60*232/100)*400/330;
inspectedChart.onpointermove({clientX:exactSampleX,clientY:exactSampleY,target:{classList:{contains:()=>true}}});
assert.match(el('usage-history-tip-text').textContent,/60% remaining.*Recorded/,'A raw reading near the cursor wins over the full-height session band');
assert.match(el('usage-history-tip-text').textContent,/Current session.*estimated/,'Exact inspection still includes session context');
inspectedChart.onpointerdown({button:0,pointerId:8,clientX:sessionX,clientY:100});inspectedChart.onpointerup({pointerId:8,clientX:sessionX,clientY:100});
env.fetch=async()=>({ok:true,json:async()=>({...data,stale:true,sessions:[{start_at:now-3600,end_at:now,last_used_at:now,used_percent:60,current:true,estimated:true}]})});
await env.readHistory();
assert.doesNotMatch(el('usage-history-tip-text').textContent,/Current session/,'A pinned session must reconcile freshness on refresh');
el('usage-history-details').open=true;
documentListeners.pointerdown({target:el('usage-history-value')});
assert.equal(el('usage-history-details').open,false,'Outside pointer interaction dismisses information');
assert.equal(el('usage-history-tooltip').hidden,true,'Outside pointer interaction dismisses the pinned chart popup');
env.closeHistory();
env.fetch=async()=>({ok:true,json:async()=>data});await env.openHistory('chatgpt',el('usage-history-open-chatgpt'));
const forecastChart=el('usage-history-chart');forecastChart.onkeydown({key:'End',target:forecastChart,preventDefault(){},stopPropagation(){}});forecastChart.onkeydown({key:'Enter',target:forecastChart,preventDefault(){},stopPropagation(){}});
env.fetch=async()=>({ok:true,json:async()=>({...data,stale:true})});await env.readHistory();
assert.equal(el('usage-history-tooltip').hidden,true,'A stale forecast must also dismiss its pinned projected point');
env.closeHistory();
console.log('PASS estimated sessions in floating inspection and outside disclosure dismissal');
// Native dialog, SVG pointer capture and real keyboard focus are not emulated by
// the VM. --browser exercises the exact production controller/CSS in isolation;
// the production-router suite separately covers auth, storage and ledger reads.
if (process.argv.includes('--browser')) {
  const style=html.match(/<style>([\s\S]*?)<\/style>/)[1];
  const dialog=html.match(/<dialog id="usage-history-dialog"[\s\S]*?<\/dialog>/)[0];
  const buttons=['chatgpt','brave'].map(p=>html.match(new RegExp('<button id="usage-history-open-'+p+'"[^>]*>[\\s\\S]*?<\\/button>'))[0].replace(' disabled','')).join('');
  const page='<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><style>'+style+'</style>'+buttons+dialog+'<script>let activeProfileID="p1";const settingsPanel={hidden:false},chatApp={hidden:false};'+modelSource+controllerSource+'</script>';
  const calls=[];let capDisabled=false,sparseHistory=false,dailyHistory=false,readingDelay=0,gapHistory=false,sessionHistory=false;
  const server=createServer((req,res)=>{
    if(req.url==='/'){res.writeHead(200,{'Content-Type':'text/html'});res.end(page);return;}
    calls.push(req.url);
    const t=Math.floor(Date.now()/1000),url=new URL(req.url,'http://127.0.0.1');
    const result={...data,scope:'ChatGPT · weekly allowance',observed_at:t,tracking_started_at:t-86400,period_start:t-86400,reset_at:t+7200,points:points.map(p=>({...p,at:p.at-now+t,reset_at:t+7200})),window_id:'weekly',windows:[{id:'weekly',label:'Weekly allowance'},{id:'five',label:'5-hour allowance'}]};
    if(dailyHistory)Object.assign(result,{remaining_percent:dailyData.remaining_percent,reset_at:t+604800,points:(dailyHistory==='idle'?idleHourData.points:dailyPoints).map(p=>({...p,at:p.at-now+t,reset_at:t+604800}))});
    if(dailyHistory==='stairs')Object.assign(result,{remaining_percent:29,reset_at:t+241920,points:Array.from({length:481},(_,i)=>({at:t-28800+i*60,remaining_percent:37-Math.floor(i/60),reset_at:t+241920}))});
    if(url.pathname==='/api/research/history')Object.assign(result,{provider:'brave',scope:'Brave Search · Agent cap',gap_seconds:0,remaining_count:80,remaining_percent:80,points:[{at:t-7200,remaining_percent:100},{at:t-1800,remaining_percent:90},{at:t,remaining_percent:80}],...(capDisabled?{remaining_percent:null,points:[],warning:'Agent cap is disabled'}:{})});
    if(url.searchParams.get('window_id')==='five')Object.assign(result,{scope:'ChatGPT · 5-hour allowance',window_id:'five'});
    if(sparseHistory)result.points=result.points.slice(-4);
    if(gapHistory)Object.assign(result,{remaining_percent:60,points:sparsePoints.map(p=>({...p,at:p.at-now+t,reset_at:t+7200}))});
    if(sessionHistory && result.provider==='chatgpt')result.sessions=[{start_at:t-3600,end_at:t,last_used_at:t,used_percent:60,current:true,estimated:true}];
    if(readingDelay && result.provider==='chatgpt')Object.assign(result,{observed_at:t-readingDelay,stale:readingDelay>180,points:result.points.map(p=>({...p,at:p.at-readingDelay}))});
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
    await cdp('Runtime.enable');await cdp('Page.enable');await cdp('Emulation.setDeviceMetricsOverride',{width:1280,height:844,deviceScaleFactor:1,mobile:false});await cdp('Page.navigate',{url:'http://127.0.0.1:'+server.address().port});
    await until("!!document.getElementById('usage-history-open-chatgpt')");
    await js("document.getElementById('usage-history-open-chatgpt').click()");
    await until("!!document.getElementById('usage-history-forecast')");
    gapHistory=true;
    await js('usageHistoryRead()');
    assert.equal(await js("document.querySelectorAll('.usage-history-gap').length"),2,'Chromium draws dotted hour-gap connectors');
    assert.match(await js("document.querySelector('.usage-history-recorded').getAttribute('d')"),/ C /,'Chromium connects five-minute readings solidly');
    assert.equal(await js("document.querySelectorAll('#usage-history-chart circle[r=\"3\"]').length"),0,'Chromium removes isolated sample dots');
    assert.equal(await js("!!document.getElementById('usage-history-forecast')"),false,'Display connectors cannot create forecast evidence');
    gapHistory=false;
    await js('usageHistoryRead()');
    console.log('PASS Chromium solid nearby readings, dotted hour gaps and no isolated sample circles');
    for(const age of [90,181,0]) {
      readingDelay=age;
      await js('usageHistoryRead()');
      if(age===181) {
        assert.equal(await js("document.getElementById('usage-history-estimate').textContent"),'Forecast unavailable');
        assert.match(await js("document.getElementById('usage-history-basis').textContent"),/out of date/);
        assert.equal(await js("!!document.getElementById('usage-history-forecast')"),false);
      } else {
        assert.equal(await js("!!document.getElementById('usage-history-forecast')"),true,'Delayed polling retains the real rendered forecast, and a fresh poll restores it');
        assert.match(await js("document.getElementById('usage-history-budget').textContent"),/Daily budget:/);
        assert.match(await js("document.getElementById('usage-history-message').textContent"),age ? /last checked.*ago.*delayed/ : /^$/);
      }
    }
    console.log('PASS Chromium delayed-poll grace, stale explanation and fresh-reading recovery');
    assert.equal(await js("document.activeElement.id"),'usage-history-close');
    assert.equal(await js("document.querySelectorAll('.usage-history-milestone').length"),5);
    await js("document.getElementById('usage-history-chart').focus()");
    const press=async key=>{const event={key,code:key,windowsVirtualKeyCode:{Enter:13,Escape:27,Home:36,End:35}[key]||0};await cdp('Input.dispatchKeyEvent',{type:'keyDown',...event,...(key==='Enter'?{text:'\r',unmodifiedText:'\r'}:{})});await cdp('Input.dispatchKeyEvent',{type:'keyUp',...event});};
    await press('Home');await press('Enter');assert.equal(await js("document.getElementById('usage-history-unpin').hidden"),false);
    await press('Escape');assert.equal(await js("document.getElementById('usage-history-tooltip').hidden"),true);assert.equal(await js("document.getElementById('usage-history-dialog').open"),true);
    const coords=await js("(()=>{const r=document.getElementById('usage-history-chart').getBoundingClientRect();return {x:r.left+r.width*.5,y:r.top+r.height*.5};})()");
    await cdp('Input.dispatchMouseEvent',{type:'mouseMoved',...coords});await cdp('Input.dispatchMouseEvent',{type:'mousePressed',...coords,button:'left',clickCount:1});await cdp('Input.dispatchMouseEvent',{type:'mouseMoved',x:coords.x+20,y:coords.y,button:'left',buttons:1});await cdp('Input.dispatchMouseEvent',{type:'mouseReleased',x:coords.x+20,y:coords.y,button:'left',clickCount:1});
    assert.equal(await js("document.getElementById('usage-history-unpin').hidden"),false);
    await js("document.getElementById('usage-history-unpin').click()");
    assert.equal(await js("document.getElementById('usage-history-details').open"),false,'Details start collapsed');
    assert.equal(await js("document.getElementById('usage-history-dialog').innerText.includes('current remaining')"),false);
    const chartHeight=await js("document.getElementById('usage-history-chart').getBoundingClientRect().height");
    const detailPoint=await js("(()=>{const r=document.getElementById('usage-history-details-toggle').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()");
    await cdp('Input.dispatchMouseEvent',{type:'mousePressed',...detailPoint,button:'left',clickCount:1});
    await cdp('Input.dispatchMouseEvent',{type:'mouseReleased',...detailPoint,button:'left',clickCount:1});
    assert.equal(await js("document.getElementById('usage-history-details').open"),true,'Information control opens real details: '+JSON.stringify(await js("(()=>{const p=document.getElementById('usage-history-details-toggle'),r=p.getBoundingClientRect();return {point:{x:r.x,y:r.y,width:r.width,height:r.height},viewport:[innerWidth,innerHeight],hit:document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)?.outerHTML};})()")));
    assert.equal(await js("document.getElementById('usage-history-chart').getBoundingClientRect().height"),chartHeight,'Opening details never resizes the plot');
    assert.equal(await js("document.getElementById('usage-history-scope').getClientRects().length"),0,'Removed subtitle stays absent even when details are open');
    await press('Escape');
    assert.equal(await js("document.getElementById('usage-history-details').open"),false,'Escape dismisses details before the dialog');
    assert.equal(await js("document.getElementById('usage-history-dialog').open"),true);
    assert.equal(await js('document.activeElement.id'),'usage-history-details-toggle','Dismissal restores focus to the information control');
    await press('Enter');
    await until("document.getElementById('usage-history-details').open");
    await js("(()=>{const w=document.getElementById('usage-history-window');w.value='five';w.dispatchEvent(new Event('change'));})()");await until("document.getElementById('usage-history-scope').textContent.includes('5-hour')");
    await js("(()=>{const r=document.getElementById('usage-history-range');r.value='week';r.dispatchEvent(new Event('change'));})()");await until("!!document.getElementById('usage-history-chart')");assert.ok(calls.some(c=>c.includes('range=week')&&c.includes('window_id=five')));
    await js("document.getElementById('usage-history-details').open=false");
    dailyHistory=true;
    await js("document.getElementById('usage-history-close').click();document.getElementById('usage-history-open-chatgpt').click()");
    await until("!!document.getElementById('usage-history-forecast')");
    assert.equal(await js("document.querySelectorAll('#usage-history-forecast-daily').length"),0);
    assert.equal(await js("document.getElementById('usage-history-forecast').getAttribute('stroke-dasharray')"),'5 6');
    await js("document.getElementById('usage-history-chart').focus()");await press('End');await press('Enter');
    assert.match(await js("document.getElementById('usage-history-tip-text').textContent"),/Recent average.*pinned/,'Real keyboard inspection identifies the recent-average projection');
    await press('Escape');
    for(const [width,height] of [[1280,844],[900,600],[390,844],[320,640]]){
      await cdp('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile:width<500});
      await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
      assert.equal(await js("(()=>{const d=document.getElementById('usage-history-dialog'),r=d.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&d.scrollWidth<=d.clientWidth;})()"),true,'Modal must fit width '+width);
      assert.equal(await js("(()=>{const r=document.getElementById('usage-history-range').getBoundingClientRect(),c=document.getElementById('usage-history-close').getBoundingClientRect();return r.right<=c.left&&Math.abs(r.top-c.top)<8;})()"),true,'Range stays inline just before Close');
      if(width<=600)assert.equal(await js("(()=>{const r=document.getElementById('usage-history-range'),s=getComputedStyle(r),ctx=document.createElement('canvas').getContext('2d');ctx.font=s.font;const textWidth=Math.max(...[...r.options].map(o=>ctx.measureText(o.text).width));return r.clientWidth>=textWidth+parseFloat(s.paddingLeft)+parseFloat(s.paddingRight)+24;})()"),true,'Every range label fits with padding and room for the native arrow at '+width);
      assert.equal(await js("(()=>{const t=document.getElementById('usage-history-title').getBoundingClientRect(),r=document.getElementById('usage-history-range').getBoundingClientRect();return t.right<=r.left&&Math.abs((t.top+t.bottom-r.top-r.bottom)/2)<2;})()"),true,'Title and range share one row without overlap at '+width);
      assert.equal(await js("(()=>{const d=document.getElementById('usage-history-dialog');return d.scrollHeight<=d.clientHeight;})()"),true,'Compact dialog has no internal scrolling at '+width+'×'+height+' '+JSON.stringify(await js("(()=>{const d=document.getElementById('usage-history-dialog');return {scroll:d.scrollHeight,client:d.clientHeight,children:[...d.children].map(e=>({id:e.id,tag:e.tagName,height:e.getBoundingClientRect().height,top:e.getBoundingClientRect().top}))};})()")));
      assert.equal(await js("document.getElementById('usage-history-chart').viewBox.baseVal.height"),width<500?310:330,'Original chart height is preserved');
      await js("document.getElementById('usage-history-details-toggle').click()");
      assert.equal(await js("(()=>{const d=document.getElementById('usage-history-dialog'),p=document.querySelector('.history-details-body').getBoundingClientRect(),r=d.getBoundingClientRect();return p.left>=r.left&&p.right<=r.right&&p.top>=r.top&&p.bottom<=r.bottom&&d.scrollHeight<=d.clientHeight;})()"),true,'Open details stay inside the dialog without adding scrolling');
      await js("document.getElementById('usage-history-details-toggle').click()");
      if(process.env.UI_SCREENSHOTS){const {data}=await cdp('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.UI_SCREENSHOTS,'history-daily-'+width+'.png'),Buffer.from(data,'base64'));}
    }
    dailyHistory='stairs';
    await js("document.getElementById('usage-history-close').click();document.getElementById('usage-history-open-chatgpt').click()");
    await until("document.getElementById('usage-history-value').textContent==='29% left' && !!document.getElementById('usage-history-forecast')");
    assert.ok(await js("(document.querySelector('.usage-history-recorded').getAttribute('d').match(/ C /g)||[]).length<110"),'Real browser renders a reduced trend for quantized minute readings');
    await js("document.getElementById('usage-history-chart').focus()");await press('End');await press('ArrowLeft');await press('ArrowLeft');await press('Enter');
    assert.match(await js("document.getElementById('usage-history-tip-text').textContent"),/29% remaining.*Recorded.*pinned/,'The latest exact reading remains inspectable after smoothing');
    await press('Escape');
    await cdp('Emulation.setDeviceMetricsOverride',{width:1280,height:844,deviceScaleFactor:1,mobile:false});
    await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
    if(process.env.UI_SCREENSHOTS){const {data}=await cdp('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.UI_SCREENSHOTS,'history-smoothed-stairs.png'),Buffer.from(data,'base64'));}
    dailyHistory='idle';
    await js("document.getElementById('usage-history-close').click();document.getElementById('usage-history-open-chatgpt').click()");
    await until("!!document.getElementById('usage-history-forecast')");
    const endpoint=async id=>js("(()=>{const p=document.getElementById("+JSON.stringify(id)+"),v=p.getPointAtLength(p.getTotalLength()),s=new DOMPoint(v.x,v.y).matrixTransform(p.getScreenCTM());return {x:s.x,y:s.y};})()");
    const dailyEnd=await endpoint('usage-history-forecast');
    assert.match(await js("document.getElementById('usage-history-hourly-rate').textContent"),/Last hour: no usage recorded/);
    await cdp('Input.dispatchMouseEvent',{type:'mousePressed',...dailyEnd,button:'left',clickCount:1});
    await cdp('Input.dispatchMouseEvent',{type:'mouseReleased',...dailyEnd,button:'left',clickCount:1});
    assert.match(await js("document.getElementById('usage-history-tip-text').textContent"),/Recent average.*pinned/,'Mouse can select the average endpoint');
    await cdp('Emulation.setTouchEmulationEnabled',{enabled:true});
    for(const [point,label] of [[dailyEnd,/Recent average.*pinned/]]){
      await cdp('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[point]});
      await cdp('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});
      assert.match(await js("document.getElementById('usage-history-tip-text').textContent"),label,'Touch pins the average projection');
    }
    await cdp('Emulation.setTouchEmulationEnabled',{enabled:false});
    await js("document.getElementById('usage-history-close').click()");assert.equal(await js('document.activeElement.id'),'usage-history-open-chatgpt');dailyHistory=false;
    sparseHistory=true;await js("document.getElementById('usage-history-open-chatgpt').click()");await until("!!document.getElementById('usage-history-forecast')");
    assert.match(await js("document.getElementById('usage-history-basis').textContent"),/Forecast:.*3 min.*covered/,'Sparse history still gives an estimate with its measured duration');
    assert.equal(await js("document.getElementById('usage-history-dialog').innerText.includes('Needs a complete hour')"),false,'Hourly explanation is hidden until requested');
    await js("document.getElementById('usage-history-details-toggle').click()");
    assert.equal(await js("document.getElementById('usage-history-dialog').innerText.includes('Needs a complete hour')"),true,'The unavailable hourly detail does not prevent a recent-average forecast');
    await js("document.getElementById('usage-history-close').click()");sparseHistory=false;
    await js("document.getElementById('usage-history-open-brave').click()");
    await until("document.getElementById('usage-history-value').textContent==='80 searches left' && !!document.getElementById('usage-history-forecast')");
    assert.match(await js("document.querySelector('.usage-history-recorded').getAttribute('d')"),/ C /,'Native Brave chart uses smoothed history');
    assert.doesNotMatch(await js("document.querySelector('.usage-history-recorded').getAttribute('d')"),/ [HV] /,'Native Brave chart no longer renders steps');
    assert.match(await js("document.getElementById('usage-history-note').textContent"),/Agent cap.*time-smoothed.*exact readings/);
    assert.match(await js("document.getElementById('usage-history-hourly-rate').textContent"),/Last hour: 20%/);
    await js("document.getElementById('usage-history-chart').focus()");await press('Home');await press('Enter');
    assert.match(await js("document.getElementById('usage-history-tip-text').textContent"),/100% remaining.*Recorded.*pinned/,'Keyboard still inspects the exact ledger baseline');
    await press('Escape');
    const threshold=await js("(()=>{const r=document.querySelector('.usage-history-milestone').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()");
    await cdp('Input.dispatchMouseEvent',{type:'mousePressed',...threshold,button:'left',clickCount:1});
    await cdp('Input.dispatchMouseEvent',{type:'mouseReleased',...threshold,button:'left',clickCount:1});
    assert.match(await js("document.getElementById('usage-history-tip-text').textContent"),/90% remaining.*Recorded.*pinned/,'Mouse retains the actual Brave balance at the threshold event');
    await press('Escape');
    for(const width of [1280,390]) {
      await cdp('Emulation.setDeviceMetricsOverride',{width,height:844,deviceScaleFactor:1,mobile:width<500});
      await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
      await js("document.getElementById('usage-history-details-toggle').click()");
      assert.equal(await js("(()=>{const d=document.getElementById('usage-history-dialog'),r=d.getBoundingClientRect(),p=document.querySelector('.history-details-body').getBoundingClientRect();return d.scrollHeight<=d.clientHeight&&d.scrollWidth<=d.clientWidth&&p.top>=r.top&&p.bottom<=r.bottom;})()"),true,'Brave smoothing details fit desktop and narrow layouts');
      await js("document.getElementById('usage-history-details-toggle').click()");
      if(process.env.UI_SCREENSHOTS){const {data}=await cdp('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.UI_SCREENSHOTS,'history-brave-smoothed-'+width+'.png'),Buffer.from(data,'base64'));}
    }
    await js("document.getElementById('usage-history-close').click()");
    assert.equal(await js('document.activeElement.id'),'usage-history-open-brave');
    sessionHistory=true;
    await cdp('Emulation.setDeviceMetricsOverride',{width:1280,height:844,deviceScaleFactor:1,mobile:false});
    await js("document.getElementById('usage-history-open-chatgpt').click()");await until("!!document.querySelector('.usage-history-session.current')");
    const sessionPoint=await js("(()=>{const r=document.querySelector('.usage-history-session.current').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()");
    await cdp('Input.dispatchMouseEvent',{type:'mouseMoved',...sessionPoint});
    assert.match(await js("document.getElementById('usage-history-tip-text').textContent"),/Current session.*estimated[\s\S]*60% used/,'Real hover shows session details in the cursor popup');
    await cdp('Input.dispatchMouseEvent',{type:'mousePressed',...sessionPoint,button:'left',clickCount:1});await cdp('Input.dispatchMouseEvent',{type:'mouseReleased',...sessionPoint,button:'left',clickCount:1});
    assert.equal(await js("document.getElementById('usage-history-unpin').hidden"),false,'Click pins session details');
    assert.equal(await js("(()=>{const t=document.getElementById('usage-history-tooltip').getBoundingClientRect(),c=document.getElementById('usage-history-chart').getBoundingClientRect();return t.left>=c.left&&t.right<=c.right&&t.top>=c.top&&t.bottom<=c.bottom})()"),true,'Session popup is contained within the chart');
    if(process.env.UI_SCREENSHOTS){const {data}=await cdp('Page.captureScreenshot',{format:'png'});await writeFile(join(process.env.UI_SCREENSHOTS,'history-session-popup-desktop.png'),Buffer.from(data,'base64'));}
    const outsidePoint=await js("(()=>{const r=document.getElementById('usage-history-value').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()");
    await cdp('Input.dispatchMouseEvent',{type:'mousePressed',...outsidePoint,button:'left',clickCount:1});await cdp('Input.dispatchMouseEvent',{type:'mouseReleased',...outsidePoint,button:'left',clickCount:1});
    assert.equal(await js("document.getElementById('usage-history-tooltip').hidden"),true,'Outside mouse click dismisses pinned session popup');
    await js("document.querySelector('.usage-history-session.current').focus()");await press('Enter');assert.equal(await js("document.getElementById('usage-history-unpin').hidden"),false,'Keyboard pins a session');await press('Escape');await press('Escape');
    assert.equal(await js("document.getElementById('usage-history-dialog').open"),false,'Second Escape from session focus closes the dialog');
    assert.equal(await js("document.activeElement.id"),'usage-history-open-chatgpt');
    await js("document.getElementById('usage-history-open-chatgpt').click()");await until("!!document.querySelector('.usage-history-session.current')");
    await js("document.getElementById('usage-history-details-toggle').click()");
    await cdp('Input.dispatchMouseEvent',{type:'mousePressed',...outsidePoint,button:'left',clickCount:1});await cdp('Input.dispatchMouseEvent',{type:'mouseReleased',...outsidePoint,button:'left',clickCount:1});
    assert.equal(await js("document.getElementById('usage-history-details').open"),false,'Outside click dismisses information without closing usage history');
    await cdp('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await cdp('Emulation.setTouchEmulationEnabled',{enabled:true});
    await js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))');
    const sessionTouch=await js("(()=>{const r=document.querySelector('.usage-history-session.current').getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2};})()");
    await cdp('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[sessionTouch]});await cdp('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});
    assert.match(await js("document.getElementById('usage-history-tip-text').textContent"),/Current session.*estimated[\s\S]*60% used/,'Real touch pins session details');
    assert.equal(await js("document.getElementById('usage-history-unpin').hidden"),false);
    assert.equal(await js("(()=>{const t=document.getElementById('usage-history-tooltip').getBoundingClientRect(),c=document.getElementById('usage-history-chart').getBoundingClientRect();return t.left>=c.left&&t.right<=c.right&&t.top>=c.top&&t.bottom<=c.bottom})()"),true,'Touch session popup fits the narrow chart');
    await cdp('Emulation.setTouchEmulationEnabled',{enabled:false});await js("document.getElementById('usage-history-close').click()");sessionHistory=false;
    console.log('PASS Chromium session hover, mouse/touch pinning, keyboard, popup bounds and outside disclosure dismissal');
    capDisabled=true;await js("document.getElementById('usage-history-open-brave').click()");await until("document.getElementById('usage-history-message').textContent.includes('disabled')");
    assert.equal(await js("document.getElementById('usage-history-plot').childElementCount"),0);assert.equal(await js("document.getElementById('usage-history-window-label').hidden"),true);
    assert.equal(await js("document.getElementById('usage-history-details').open"),false,'Reopening a provider resets the details disclosure');
    await js("document.getElementById('usage-history-details-toggle').click()");
    assert.equal(await js("(()=>{const p=document.querySelector('.history-details-body').getBoundingClientRect(),d=document.getElementById('usage-history-dialog').getBoundingClientRect();return p.top>=d.top&&p.bottom<=d.bottom;})()"),true,'Details remain readable even without a chart');
    assert.deepEqual(errors,[]);assert.ok(calls.every(c=>!c.includes('/check')&&!c.includes('/refresh')));
    console.log('PASS native Chromium focus, chart inspection, keyboard details, scoped windows, sparse/empty history and scroll-free 1280/900/390/320px layout');
  } finally {for(const job of jobs.values())clearTimeout(job.timer);socket?.close();await stopTestBrowser(browser);await rm(profile,{recursive:true,force:true,maxRetries:3,retryDelay:100});await new Promise(r=>server.close(r));}
}
