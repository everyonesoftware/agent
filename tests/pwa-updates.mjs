// Execute the shipped JavaScript against isolated browser lifecycle fixtures.
import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import vm from 'node:vm';
import { test } from 'node:test';
const root = new URL('../', import.meta.url);
const page = readFileSync(new URL('page.go', root), 'utf8');
const scriptFile = new URL('pwa_updates.go', root);
const source = existsSync(scriptFile) ? readFileSync(scriptFile, 'utf8') : '';
const script = source.slice(source.indexOf('`') + 1, source.lastIndexOf('`'));
class Events {
  listeners = {};
  addEventListener(name, fn) { (this.listeners[name] ||= new Set()).add(fn); }
  removeEventListener(name, fn) { this.listeners[name]?.delete(fn); }
  emit(name) { for (const fn of [...(this.listeners[name] || [])]) fn(); }
}
function fixture(options = {}) {
  const ids = ['pwa-updates','pwa-status','pwa-check','pwa-apply','pwa-later','pwa-version'];
  const elements = Object.fromEntries(ids.map(id => [id, Object.assign(new Events(), {textContent:'',hidden:false,disabled:false,dataset:{},setAttribute(k,v){this[k]=v;}})]));
  const sw = new Events();
  const registration = Object.assign(new Events(), {waiting:null,installing:null,update:async()=>{}});
  let registrations = 0, fetches = 0, reloads = 0, confirmations = 0;
  sw.register = async () => { registrations++; if (options.registrationError) throw Error('registration failed'); return registration; };
  const document = {
    getElementById: id => elements[id],
    querySelector: selector => selector.includes('app-build-id') ? {content:'build-a'} : selector.includes('app-version') ? {content:'42'} : null,
  };
  const context = {document,navigator:{serviceWorker:sw,onLine:true},isSecureContext:true,AbortController,
    setTimeout:(fn,ms)=>setTimeout(fn, options.fastTimers ? 5 : ms),clearTimeout,
    fetch:async()=>{fetches++; return {ok:true,json:async()=>({version:'42',buildId:'build-a'})};},
    location:{reload(){reloads++;}},confirm(){confirmations++;return options.confirm !== false;},
  };
  context.window = context;
  Object.assign(context, options.environment);
  vm.runInNewContext(script, context);
  return {context,sw,registration,elements,get registrations(){return registrations;},get fetches(){return fetches;},get reloads(){return reloads;},get confirmations(){return confirmations;},click(id){elements[id].emit('click');}};
}
const settle = async () => { await new Promise(resolve => setImmediate(resolve)); };

test('installable metadata and Settings update controls are embedded in the real page', () => {
  for(const value of ['name="app-build-id" content="{{APP_BUILD_ID}}"','name="app-version" content="{{APP_VERSION}}"','rel="manifest" href="/manifest.webmanifest"','name="theme-color"','rel="apple-touch-icon" href="/icons/apple-touch-icon.png"','name="apple-mobile-web-app-capable" content="yes"','src="/pwa-updates.js" defer','Web app updates','id="pwa-check"','id="pwa-apply"','id="pwa-later"']) assert.ok(page.includes(value), value);
});
test('Web app updates is the first settings card', () => {
  const settings = page.slice(page.indexOf('<section class="settings-panel"'), page.indexOf('<div class="chat-layout"'));
  const cards = [...settings.matchAll(/<section class="settings-card"[^>]*aria-labelledby="([^"]+)"/g)].map(match => match[1]);
  assert.deepEqual(cards, ['pwa-title', 'connection-title', 'research-settings-title', 'memory-title', 'password-title', 'signout-title']);
});
test('checks compare build IDs even when the version is unchanged, and Later never reloads', async () => {
  const f=fixture(); await settle(); assert.equal(f.registrations,1);
  f.click('pwa-check'); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'current');
  f.context.fetch=async()=>({ok:true,json:async()=>({version:'42',buildId:'build-b'})});
  f.click('pwa-check'); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'available');
  assert.equal(f.elements['pwa-apply'].hidden,false);
  f.click('pwa-later');
  assert.match(f.elements['pwa-status'].textContent,/postponed/i);
  f.sw.emit('controllerchange'); assert.equal(f.reloads,0);
});
test('failed startup is contained, retry recovers, offline and malformed metadata never claim current', async () => {
  const f=fixture({registrationError:true}); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'error');
  f.sw.register=async()=>f.registration;
  f.context.navigator.onLine=false; f.click('pwa-check'); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'offline');
  f.context.navigator.onLine=true;
  f.context.fetch=async()=>({ok:true,json:async()=>({version:'42'})});
  f.click('pwa-check'); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'error');
  f.context.fetch=async()=>({ok:true,json:async()=>({version:'42',buildId:'build-a'})});
  f.click('pwa-check'); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'current');
});
test('explicit apply alone activates waiting worker and reloads once', async () => {
  const f=fixture(); await settle(); let messages=0;
  f.registration.waiting={postMessage(message){assert.equal(message.type,'SKIP_WAITING');messages++;}};
  f.sw.emit('controllerchange'); assert.equal(f.reloads,0);
  f.click('pwa-check'); await settle();
  f.context.agentUpdateSafety=()=>({pending:true,unsaved:false});
  f.click('pwa-apply'); assert.equal(messages,0); assert.match(f.elements['pwa-status'].textContent,/request/i);
  f.context.agentUpdateSafety=()=>({pending:false,unsaved:true});
  f.context.confirm=()=>false; f.click('pwa-apply'); assert.equal(messages,0);
  f.context.confirm=()=>true; f.click('pwa-apply'); f.click('pwa-apply'); assert.equal(messages,1);
  f.sw.emit('controllerchange'); f.sw.emit('controllerchange'); assert.equal(f.reloads,1);
});
test('metadata-only update reload is guarded, and missing safety bridge fails closed', async () => {
  const f=fixture(); await settle();
  f.context.fetch=async()=>({ok:true,json:async()=>({version:'43',buildId:'build-b'})});
  f.click('pwa-check'); await settle(); f.click('pwa-apply'); assert.equal(f.reloads,0);
  f.context.agentUpdateSafety=()=>({pending:false,unsaved:false});
  f.click('pwa-apply'); assert.equal(f.reloads,1);
});
test('checks coalesce, time out, ignore stale responses and permit explicit retry', async () => {
  const f=fixture({fastTimers:true}); await settle(); let finish;
  f.context.fetch=()=>new Promise(resolve=>{finish=resolve;});
  f.click('pwa-check'); f.click('pwa-check'); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'checking');
  await new Promise(resolve=>setTimeout(resolve,25));
  assert.equal(f.elements['pwa-updates'].dataset.state,'error');
  f.context.fetch=async()=>({ok:true,json:async()=>({version:'42',buildId:'build-a'})});
  f.click('pwa-check'); await settle();
  finish({ok:true,json:async()=>({version:'43',buildId:'build-b'})}); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'current');
});
test('installing workers finish before current, asynchronous waiting updates surface without reload', async () => {
  const f=fixture(); await settle();
  const worker=Object.assign(new Events(),{state:'installing'}); f.registration.installing=worker;
  f.registration.emit('updatefound');
  f.click('pwa-check'); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'checking');
  worker.state='installed'; f.registration.waiting={postMessage(){}}; worker.emit('statechange'); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'available'); assert.equal(f.reloads,0);
});
test('apply timeout disarms late controllerchange and checks revalidate', async () => {
  const f=fixture({fastTimers:true}); await settle();
  f.registration.waiting={postMessage(){}}; f.click('pwa-check'); await settle();
  f.context.agentUpdateSafety=()=>({pending:false,unsaved:false}); f.click('pwa-apply');
  await new Promise(resolve=>setTimeout(resolve,25));
  assert.equal(f.elements['pwa-updates'].dataset.state,'error');
  f.sw.emit('controllerchange'); assert.equal(f.reloads,0);
});
test('synchronous registration failure is contained at background startup', async () => {
  const sw=new Events(); sw.register=()=>{throw Error('browser denied registration');};
  const f=fixture({environment:{navigator:{serviceWorker:sw,onLine:true}}}); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'error');
});
test('new unsaved changes or active requests during activation are rechecked before reload', async () => {
  const f=fixture(); await settle(); f.registration.waiting={postMessage(){}};
  f.click('pwa-check'); await settle();
  let safety={pending:false,unsaved:false,revision:0}; f.context.agentUpdateSafety=()=>safety;
  f.click('pwa-apply');
  safety={pending:false,unsaved:true,revision:0}; f.context.confirm=()=>false;
  f.sw.emit('controllerchange'); assert.equal(f.reloads,0);
  assert.equal(f.elements['pwa-check'].disabled,false,'cancelled reload must unlock controls');
  f.click('pwa-apply'); assert.equal(f.reloads,0);
  f.context.confirm=()=>true; f.click('pwa-apply'); safety.pending=true;
  f.sw.emit('controllerchange'); assert.equal(f.reloads,0);
});
test('real page safety bridge reads live chat, request, password and dialog state without persistence', () => {
  const match=page.match(/      \/\/ BEGIN update reload safety([\s\S]*?)      \/\/ END update reload safety/);
  assert.ok(match,'page must expose its actual closure state');
  const budgetFields=page.match(/      const searchBudgetFields = ([^;]+);/);
  const budgetDirty=page.match(/      function searchBudgetDirty\(\) \{[\s\S]*?\n      \}/);
  assert.ok(budgetFields && budgetDirty,'include the real budget draft check rather than a stub');
  const context={window:{},document:Object.assign(new Events(),{querySelectorAll:selector=>{context.passwordSelector=selector;return context.passwords;},querySelector:()=>context.dialog}),input:{value:''},searchKey:{value:''},passwords:[],dialog:null,
    busy:false,loadingConversation:false,passwordBusy:false,authBusy:false,connectionBusy:false,connectionSubmitting:false,usageBusy:false,usageSubmitting:false,searchBusy:false,searchBudgetDraft:null,searchBudgetBase:null,memoryBusy:false,memorySuggestionBusy:false,memorySuggestionState:null,memoryForm:{hidden:true},memoryEl:id=>{assert.equal(id,'form');return context.memoryForm;}};
  vm.runInNewContext('const searchBudgetFields = '+budgetFields[1]+';\n'+budgetDirty[0]+'\n'+match[1],context);
  const safety=()=>context.window.agentUpdateSafety();
  assert.equal(safety().pending,false); assert.equal(safety().unsaved,false);
  assert.match(context.passwordSelector,/autocomplete/, 'revealed password fields must remain guarded');
  for(const name of ['busy','loadingConversation','passwordBusy','authBusy','connectionBusy','connectionSubmitting','usageBusy','usageSubmitting','searchBusy','memoryBusy','memorySuggestionBusy']) {context[name]=true; assert.equal(safety().pending,true,name); context[name]=false;}
  context.memorySuggestionState={custom:'private suggested memory draft'};
  assert.equal(safety().unsaved,true,'suggestion review guards reload');
  assert.ok(!JSON.stringify(safety()).includes(context.memorySuggestionState.custom),'suggestion drafts never leave safety bridge');
  context.memorySuggestionState=null;
  context.memoryForm={hidden:false,value:'private memory draft'};
  assert.equal(safety().unsaved,true,'open memory draft guards reload');
  assert.ok(!JSON.stringify(safety()).includes(context.memoryForm.value),'memory draft never leaves safety bridge');
  context.memoryForm.hidden=true;
  assert.equal(safety().unsaved,false,'closed memory draft releases reload guard');
  context.input.value='private draft'; assert.equal(safety().unsaved,true); context.input.value='';
  context.searchKey.value='synthetic-brave-key'; assert.equal(safety().unsaved,true); assert.ok(!JSON.stringify(safety()).includes(context.searchKey.value)); context.searchKey.value='';
  context.searchBudgetBase={enabled:false,maximum:'1000',period:'month',timezone:'UTC'};
  context.searchBudgetDraft={...context.searchBudgetBase};
  assert.equal(safety().unsaved,false,'saved cap values are not a draft');
  for(const [field,value] of Object.entries({enabled:true,maximum:'500',period:'day',timezone:'America/Los_Angeles'})) {
    context.searchBudgetDraft={...context.searchBudgetBase,[field]:value};
    assert.equal(safety().unsaved,true,'unsaved budget '+field+' guards reload');
    assert.ok(!JSON.stringify(safety()).includes('America/Los_Angeles'),'safety returns flags, not budget values');
  }
  context.searchBudgetDraft=null;context.searchBudgetBase=null;
  context.passwords=[{value:'private password'}]; assert.equal(safety().unsaved,true); context.passwords=[];
  context.dialog={open:true}; assert.equal(safety().unsaved,true);
  const revision=safety().revision; context.document.emit('input'); assert.notEqual(safety().revision,revision);
});
test('insecure contexts show an unsupported note without registering', async () => {
  const f=fixture({environment:{isSecureContext:false}}); await settle();
  assert.equal(f.elements['pwa-updates'].dataset.state,'unsupported');
  assert.match(f.elements['pwa-status'].textContent,/HTTPS.*localhost/);
  assert.equal(f.registrations,0);
});
