// Focused checks against the shipped embedded UI; browser acceptance lives separately.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
const source = await readFile(new URL('../page.go', import.meta.url), 'utf8');
for (const id of ['memory-card','memory-enabled','memory-list','memory-form','memory-category','memory-content','memory-save','memory-cancel','memory-export','memory-error','memory-retry','run-memory']) {
  assert.ok(source.includes('id="' + id + '"'), 'reachable accessible memory control: ' + id);
}
assert.match(source, /Local storage/);
assert.match(source, /public-web research/);
assert.match(source, /old chats and traces retain/i);
assert.match(source, /No retroactive recall/);
for (const id of ['memory-suggestion-dialog','memory-suggestion-list','memory-suggestion-custom','memory-suggestion-save','memory-suggestion-cancel','memory-suggestion-retry','memory-suggestion-error','memory-suggestion-status']) {
  assert.ok(source.includes('id="' + id + '"'), 'in-chat suggestion control: ' + id);
}
assert.match(source, /message-actions-toggle/);
assert.match(source, /message-action-menu/);
assert.match(source, /Uses your selected model to analyze this message\. Nothing is saved until you confirm\./);
assert.match(source, /\/api\/memory-suggestions/);
assert.match(source, /\/api\/memories\/batch/);
assert.doesNotMatch(source, /remember-message/);
assert.match(source, /if \(memoryBusy\) return/);
assert.match(source, /epoch !== memoryEpoch/);
assert.match(source, /resetMemory\(\)/);
assert.match(source, /\/api\/memory-settings/);
assert.doesNotMatch(source, /\['Skills \/ memory', 'None loaded'\]/);
const render = source.slice(source.indexOf('      function renderRunMemory('), source.indexOf('      function renderRun(run)'));
assert.ok(render.length > 0, 'run snapshot renderer exists');
const output = [];
const target = {replaceChildren(){output.length=0},append(...items){output.push(...items)}};
const sandbox = {runElement:()=>target,runNode:(tag,text)=>({tag,text}),Array};
vm.createContext(sandbox);
vm.runInContext(render + '\nthis.render = renderRunMemory;', sandbox);
const text = () => output.map(x=>x.text).join('\n');
sandbox.render({research:{},context:{}}); assert.match(text(), /Excluded from public-web research/);
sandbox.render({context:{}}); assert.match(text(), /None recorded/);
sandbox.render({context:{memory:{enabled:false,entries:[]}}}); assert.match(text(), /Disabled/);
sandbox.render({context:{memory:{enabled:true,entries:[]}}}); assert.match(text(), /No saved memory sent/);
sandbox.render({context:{memory:{enabled:true,entries:[{category:'user',content:'<img onerror=bad()>',id:'one'}]}}});
assert.ok(text().includes('<img onerror=bad()>'), 'exact snapshot content rendered as text');
// Execute the shipped functions with a tiny DOM, not a reimplementation of the
// state machine. Browser acceptance separately exercises layout/native focus.
class Element {
  constructor(tag = 'div') { this.tagName = tag; this.children = []; this.listeners = {}; this.attrs = {}; this.dataset = {}; this.hidden = false; this.disabled = false; this.checked = false; this.value = ''; this.textContent = ''; this.isConnected = true; }
  append(...nodes) { for (const node of nodes) { this.children.push(node); node.parent = this; } }
  appendChild(node) { this.append(node); }
  replaceChildren(...nodes) { this.children = []; this.append(...nodes); }
  setAttribute(key, value) { this.attrs[key] = value; }
  addEventListener(type, fn) { (this.listeners[type] ||= []).push(fn); }
  async fire(type, extra = {}) { for (const fn of this.listeners[type] || []) await fn({target:this,currentTarget:this,preventDefault(){},...extra}); }
  focus() { doc.activeElement = this; }
  getClientRects() { return this.hidden ? [] : [{}]; }
  showModal() { this.open = true; }
  close() { this.open = false; }
  remove() { this.parent.children = this.parent.children.filter(node => node !== this); this.isConnected = false; }
  contains(node) { return this === node || this.children.some(child => child.contains?.(node)); }
  querySelectorAll(selector) {
    const all = this.children.flatMap(child => [child,...(child.querySelectorAll?.('*') || [])]);
    if (selector === '*') return all;
    return all.filter(child => !child.disabled && (selector.includes('button') && child.tagName === 'button' || selector.includes('textarea') && child.tagName === 'textarea' || selector.includes('input') && child.tagName === 'input'));
  }
}
const elements = new Map();
const el = id => { if (!elements.has(id)) elements.set(id, new Element()); return elements.get(id); };
const doc = {getElementById:el,createElement:tag=>new Element(tag),addEventListener(){},activeElement:null};
const calls = [];
let reply = async () => ({suggestions:[]});
const ui = {
  document:doc, AbortController, setTimeout, clearTimeout, Set, Array,
  input:new Element('textarea'), settingsPanel:{hidden:true},chatApp:{hidden:false},modelSelect:{value:'actual-selected-model'},researchFlow:false,
  messageList:new Element(), renderMessageText(){},navigator:{clipboard:{writeText:async()=>{}}},
  runNode:(tag,text = '',className = '')=>Object.assign(new Element(tag),{textContent:text,className}),
  memoryRequest:async(path,method,body,signal,headers)=>{ calls.push({path,method,body,signal,headers}); return reply(path,method,body,signal,headers); },
  validMemoryData:data=>data && typeof data.enabled === 'boolean' && Array.isArray(data.entries),
};
vm.createContext(ui);
const suggestionCode = source.slice(source.indexOf('      const suggestionEl ='),source.indexOf('      const memoryEl ='));
const addMessageCode = source.slice(source.indexOf('      function addMessage('),source.indexOf('      function setConversationNotice('));
vm.runInContext(suggestionCode + addMessageCode + `
  this.api = {openMemorySuggestions,closeMemorySuggestions,saveMemorySuggestions,refreshMemorySuggestionOutcome,analyzeMemorySuggestions,addMessage,
    showMemorySavedConfirmation,state:()=>memorySuggestionState,busy:()=>memorySuggestionBusy};`,ui);
const settle = async () => { for (let i=0; i<20; i++) await new Promise(resolve=>setImmediate(resolve)); };
const origin = new Element('button');
const bubble = ui.api.addMessage('user','Only clicked message');
assert.equal(calls.length,0,'rendering a message never invokes analysis or memory writes');
const sender = bubble.children[0];
assert.equal(sender.children[0].className,'message-actions-toggle','dots beside You, not a footer');
assert.equal(bubble.children.length,2,'no extra message footer before saving');
reply = async () => ({suggestions:[{content:'First fact',already_saved:false},{content:'Existing fact',already_saved:true}]});
ui.api.openMemorySuggestions('Only clicked message',origin,bubble);
el('memory-suggestion-custom').value = 'Custom draft during loading';
await settle();
assert.deepEqual(JSON.parse(JSON.stringify(calls[0].body)),{message:'Only clicked message',model:'actual-selected-model'});
assert.equal(calls.length,1,'analysis is the only request before explicit Save');
assert.equal(el('memory-suggestion-custom').value,'Custom draft during loading');
el('memory-suggestion-custom').value = '';
await el('memory-suggestion-custom').fire('input');
assert.equal(el('memory-suggestion-save').disabled,true,'empty selection and custom draft cannot be submitted');
el('memory-suggestion-custom').value = 'Custom draft during loading';
await el('memory-suggestion-custom').fire('input');
assert.equal(el('memory-suggestion-save').disabled,false,'typing a custom fact enables explicit save');
assert.equal(ui.api.state().rows[0].select.checked,false);
assert.equal(ui.api.state().rows[1].select.disabled,true);
assert.equal(ui.api.state().rows[1].label.textContent,'Already saved');
ui.api.state().rows[0].select.checked = true;
ui.api.state().rows[0].text.value = 'Edited fact';
const saved = [{id:'new-one',category:'user',content:'Edited fact',updated_at:'2026-11-13T00:00:00.000000001Z'},{id:'existing-custom',category:'user',content:'Custom draft during loading',updated_at:'2026-11-13T00:00:00Z'}];
reply = async (path,method,body) => {
  if (method === 'POST') {
    assert.deepEqual(JSON.parse(JSON.stringify(body)),{entries:saved.map(({category,content})=>({category,content}))});
    return {entries:saved,created_ids:['new-one']};
  }
  return {enabled:false,entries:saved};
};
const saving = ui.api.saveMemorySuggestions();
await ui.api.saveMemorySuggestions();
await saving;
assert.equal(calls.filter(call=>call.path === '/api/memories/batch').length,1,'double submission is prevented');
assert.equal(calls.at(-1).method,'GET','success requires exact readback');
assert.equal(el('memory-suggestion-dialog').open,false);
const confirmation = bubble.children.at(-1), undo = confirmation.children[1];
assert.ok('memoryUndo' in undo.dataset);
let present = [...saved];
reply = async (path,method) => { if (method === 'DELETE') { assert.equal(path,'/api/memories/new-one'); present = present.filter(entry=>entry.id !== 'new-one'); return {}; } return {enabled:false,entries:present}; };
await undo.fire('click');
assert.equal(present.length,1,'Undo preserves previously saved entries');
assert.match(confirmation.children[0].textContent,/Save undone/);
assert.equal(calls.at(-1).method,'GET','Undo verifies absence');

// Execute real Undo with another tab editing after its preliminary GET.
for (const sameContent of [false,true]) {
  const target = {...saved[0]}, raceBubble = new Element();
  ui.api.showMemorySavedConfirmation(raceBubble,[target],1);
  const row = raceBubble.children[0], button = row.children[1];
  let persisted = {...target}, deleted = false;
  reply = async (path,method,body,signal,headers) => {
    if (method === 'GET') {
      const snapshot = {...persisted};
      persisted = {...persisted,content:sameContent ? target.content : 'Edited in another tab',updated_at:'2026-11-13T00:00:01Z'};
      return {enabled:true,entries:[snapshot]};
    }
    assert.equal(method,'DELETE');
    assert.equal(path,'/api/memories/' + target.id);
    assert.equal(body,undefined);
    assert.ok(signal instanceof AbortSignal);
    assert.equal(headers?.['If-Match'],JSON.stringify(target.updated_at),'DELETE pins the original save revision');
    if (headers['If-Match'] !== JSON.stringify(persisted.updated_at)) throw new Error('Memory entry changed. Refresh before trying to undo.');
    deleted = true;
    return {};
  };
  await button.fire('click');
  assert.equal(calls.at(-1).method,'DELETE','the edit interleaves after the pre-delete GET');
  assert.equal(calls.at(-1).body,undefined);
  assert.ok(calls.at(-1).signal instanceof AbortSignal);
  assert.equal(calls.at(-1).headers?.['If-Match'],JSON.stringify(target.updated_at));
  assert.equal(deleted,false,'server revision conflict preserves the later edit, including same-content ABA');
  assert.equal(persisted.updated_at,'2026-11-13T00:00:01Z');
  assert.doesNotMatch(row.children[0].textContent,/Save undone/);
  assert.match(row.children[0].textContent,/fact changed/);
  assert.equal(button.textContent,'Refresh memory');
}
// Even identical content observed with a newer revision is not an Undo target.
{
  const target = {...saved[0]}, raceBubble = new Element();
  ui.api.showMemorySavedConfirmation(raceBubble,[target],1);
  const before = calls.length;
  reply = async () => ({enabled:true,entries:[{...target,updated_at:'2026-11-13T00:00:02Z'}]});
  await raceBubble.children[0].children[1].fire('click');
  assert.equal(calls.slice(before).filter(call=>call.method === 'DELETE').length,0,'precheck includes the original revision, not only category/content');
  assert.doesNotMatch(raceBubble.children[0].children[0].textContent,/Save undone/);
}

// Exercise the shipped request helper too: extra headers cannot drop body/signal.
{
  const requests = [];
  const requestUI = {fetch:async(path,options)=>{ requests.push({path,...options}); return {ok:true,json:async()=>({})}; }};
  vm.createContext(requestUI);
  vm.runInContext(source.slice(source.indexOf('      async function memoryRequest('),source.indexOf('      function validMemoryData(')) + '\nthis.request = memoryRequest;',requestUI);
  const signal = new AbortController().signal;
  await requestUI.request('/api/memories/new-one','DELETE',undefined,signal,{'If-Match':JSON.stringify(saved[0].updated_at)});
  assert.equal(requests[0].headers['If-Match'],JSON.stringify(saved[0].updated_at));
  assert.equal(requests[0].signal,signal);
  assert.equal(requests[0].body,undefined);
  await requestUI.request('/api/memories','POST',{content:'Keep body'},signal,{'X-Test':'extra'});
  assert.equal(requests[1].headers['Content-Type'],'application/json');
  assert.equal(requests[1].headers['X-Test'],'extra');
  assert.equal(requests[1].body,JSON.stringify({content:'Keep body'}));
  await requestUI.request('/api/memories/new-one','DELETE',undefined,signal);
  assert.equal(requests[2].headers['If-Match'],undefined,'legacy Settings DELETE stays unconditional');
}

reply = async () => { throw new Error('Network unavailable'); };
ui.api.openMemorySuggestions('Error case',origin,bubble);
el('memory-suggestion-custom').value = 'Draft survives retry'; await settle();
assert.equal(el('memory-suggestion-retry').hidden,false);
assert.equal(el('memory-suggestion-save').disabled,false,'custom can save after analysis error');
reply = async () => ({suggestions:[]});
await ui.api.analyzeMemorySuggestions();
assert.match(el('memory-suggestion-status').textContent,/No lasting facts/);
assert.equal(el('memory-suggestion-custom').value,'Draft survives retry');
reply = async () => { throw new Error('Write outcome unknown'); };
await ui.api.saveMemorySuggestions();
assert.equal(el('memory-suggestion-save').disabled,true);
assert.equal(el('memory-suggestion-retry').textContent,'Refresh saved memory');
const writes = calls.filter(call=>call.method === 'POST').length;
await ui.api.saveMemorySuggestions();
assert.equal(calls.filter(call=>call.method === 'POST').length,writes,'unknown outcome is never replayed');
// Cancel/reopen must not bypass the required verification read.
ui.api.closeMemorySuggestions();
reply = async () => ({suggestions:[]});
ui.api.openMemorySuggestions('Another message',origin,bubble); await settle();
assert.equal(el('memory-suggestion-save').disabled,true,'unknown outcomes require refresh even after closing');
reply = async () => ({enabled:false,entries:[]});
await ui.api.refreshMemorySuggestionOutcome();
assert.equal(ui.api.state().refreshRequired,false);
assert.equal(el('memory-suggestion-save').disabled,true,'refresh does not enable an empty selection');
el('memory-suggestion-custom').value = 'A new explicit fact';
await el('memory-suggestion-custom').fire('input');
assert.equal(el('memory-suggestion-save').disabled,false);
ui.api.closeMemorySuggestions();

let resolveStale;
reply = () => new Promise(resolve=>{resolveStale=resolve;});
ui.api.openMemorySuggestions('Stale message',origin,bubble);
const staleSignal = calls.at(-1).signal;
ui.api.closeMemorySuggestions();
assert.equal(staleSignal.aborted,true);
resolveStale({suggestions:[{content:'Must not appear',already_saved:false}]}); await settle();
assert.equal(ui.api.state(),null);
assert.equal(el('memory-suggestion-list').children.length,0,'late responses cannot repopulate closed UI');
const beforeResearch = calls.length; ui.researchFlow = true;
ui.api.openMemorySuggestions('Research message',origin,bubble);
assert.equal(calls.length,beforeResearch,'research never analyzes saved memories');
assert.equal(el('memory-suggestion-dialog').open,false);
for (const script of source.matchAll(/<script(?:\s[^>]*)?>([\s\S]*?)<\/script>/g)) new vm.Script(script[1]);
console.log('PASS memory UI: explicit scoped inference, editable opt-in suggestions, custom drafts, verified batch save/Undo, ambiguity guards, cancellation, research isolation, and embedded syntax');
