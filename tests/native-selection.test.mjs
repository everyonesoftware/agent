import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { execFileSync } from 'node:child_process';
import { decide, affected, workflowInputs } from '../scripts/native-selection.mjs';

test('real Git publication baselines retain Android after README/control-plane edits and retry unpublished Windows changes', () => {
  const root = mkdtempSync(join(tmpdir(), 'agent-select-'));
  const git = (...args) => execFileSync('git', ['-C', root, ...args], { encoding:'utf8' }).trim();
  const commit = (path, value) => { mkdirSync(dirname(join(root,path)), {recursive:true}); writeFileSync(join(root,path),value); git('add','.'); git('-c','user.name=Test','-c','user.email=test@example.test','commit','-qm',path); return git('rev-parse','HEAD'); };
  try {
    git('init','-q'); const baseline = commit('apps/android/app.kt','one');
    commit('apps/windows/app.cs','unpublished'); commit('scripts/current-downloads.mjs','control'); commit('README.md','docs');
    assert.equal(decide({root,platform:'android',baseline}).build,false);
    assert.equal(decide({root,platform:'windows',baseline}).build,true);
    assert.equal(decide({root,platform:'container',baseline}).build,false);
    assert.equal(decide({root,platform:'android',baseline:null}).build,true);
    commit('scripts/unknown.mjs','unknown');
    assert.equal(decide({root,platform:'android',baseline}).build,true);
    assert.deepEqual(affected(['server.go']),['container']);
  } finally { rmSync(root,{recursive:true,force:true}); }
});
const releaseWorkflow=readFileSync(new URL('../.github/workflows/publish-image.yml',import.meta.url),'utf8');
for(const [name,from,to,build] of [
  ['version producer','git rev-list --count HEAD','git rev-list --first-parent --count HEAD',true],
  ['version output wiring','version: ${{ steps.version.outputs.value }}','version: ${{ steps.version.outputs.other }}',true],
  ['version producer output key','value=$(git rev-list','other=$(git rev-list',true],
  ['late shared verify environment','  publish:\n','    env:\n      VERSION_OFFSET: 1\n\n  publish:\n',true],
  ['ordinary browser gate','run: node tests/ui.mjs','run: node tests/ui.mjs --browser',false],
]) test(`real Git workflow ${name} change ${build?'selects all platforms':'retains unchanged publications'}`,()=>{
  const root=mkdtempSync(join(tmpdir(),'agent-select-workflow-'));
  const git=(...args)=>execFileSync('git',['-C',root,...args],{encoding:'utf8'}).trim();
  const commit=value=>{writeFileSync(join(root,'.github/workflows/publish-image.yml'),value);git('add','.');git('-c','user.name=Test','-c','user.email=test@example.test','commit','-qm','workflow');return git('rev-parse','HEAD');};
  try {
    git('init','-q');mkdirSync(join(root,'.github/workflows'),{recursive:true});
    const baseline=commit(releaseWorkflow),changed=releaseWorkflow.replace(from,to);
    assert.notEqual(changed,releaseWorkflow,'probe must change the actual workflow');
    commit(changed);
    for(const platform of ['android','windows','container']) assert.equal(decide({root,platform,baseline}).build,build,platform);
  } finally {rmSync(root,{recursive:true,force:true});}
});
test('workflow extraction rejects unsupported/duplicate/quoted shared layouts instead of permitting skips',()=>{
  const valid = releaseWorkflow;
  assert.equal(workflowInputs(valid).android.includes('ubuntu-24.04'),true);
  for(const extra of ['env:\n  INPUT: shared\n','"env":\n  INPUT: shared\n','defaults: {}\n','permissions: {}\n']) assert.throws(()=>workflowInputs(extra+valid));
});
