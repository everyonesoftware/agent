import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { execFileSync } from 'node:child_process';
import { changelog } from '../scripts/native-changelog.mjs';
test('changelog collects complete installed/offered source range including skipped releases; prose excludes only proven unrelated changes',()=>{
 const root=mkdtempSync(join(tmpdir(),'agent-notes-'));
 const git=(...args)=>execFileSync('git',['-C',root,...args],{encoding:'utf8'}).trim();
 const commit=(path,subject)=>{mkdirSync(dirname(join(root,path)),{recursive:true});writeFileSync(join(root,path),subject);git('add','.');git('-c','user.name=Test','-c','user.email=test@example.test','commit','-qm',subject);return git('rev-parse','HEAD');};
 try {git('init','-q');const from=commit('shared','initial');commit('apps/android/a.kt','fix(android): reconnect safely');commit('README.md','docs: instructions');commit('apps/windows/a.cs','feat(windows): title bar');commit('shared-chore','chore: preserve ambiguous shared behavior');const to=commit('shared','[verified] feat: improved sessions');const result=changelog({root,platform:'android',from,to});assert.equal(result.commits.length,5);assert.deepEqual(result.fixes.map(x=>x.text),['reconnect safely']);assert.deepEqual(result.improvements.map(x=>x.text),['preserve ambiguous shared behavior','improved sessions']);assert.equal(result.from,from);assert.equal(result.to,to);assert.equal(result.truncated,false);assert.throws(()=>changelog({root,platform:'android',from:to,to:from}),/ancestor/);}finally{rmSync(root,{recursive:true,force:true});}
});
