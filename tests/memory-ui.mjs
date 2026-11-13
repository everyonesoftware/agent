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
assert.match(source, /className = 'reset remember-message'/);
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
console.log('PASS memory UI controls, privacy copy, guards, and run snapshot states');
