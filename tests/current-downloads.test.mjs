import assert from 'node:assert/strict';
import { test } from 'node:test';
import { renderDownloads, replaceDownloads, refresh, main } from '../scripts/current-downloads.mjs';
test('managed tables retain each publication identity and preserve all unmanaged prose',()=>{
  const inventory={android:{manifest:{versionName:'0.1.7'},url:'https://example.test/v0.1.7/Agent-android.apk',checksum:'https://example.test/checksum'},windows:null,container:null};
  const table=renderDownloads(inventory);
  assert.match(table,/0\.1\.7/);assert.match(table,/Not yet published/);assert.doesNotMatch(table,/v0\.1\.8/);
  const text='# Agent\n\nBefore\n<!-- current-downloads:start -->\nold\n<!-- current-downloads:end -->\nAfter';
  assert.equal(replaceDownloads(text,table).split('<!-- current-downloads:start -->')[0],'# Agent\n\nBefore\n');
  assert.ok(replaceDownloads(text,table).endsWith('\nAfter'));
  assert.throws(()=>replaceDownloads(text+'<!-- current-downloads:end -->',table),/markers/);
});
test('refresh uses blob SHA, detects edits and reads both exact external targets back',()=>{
  let readme='# Agent\n\nProse',body='Changes',sha='blob-sha',puts=0,patches=0;
  const release={id:4,tag_name:'v0.1.7',draft:false,prerelease:false,body};
  const remote={repository:'test/agent',list:()=>[release],api:(path,o={})=>{
    if(path.startsWith('actions/runs'))return [{workflow_runs:[]}];
    if(path==='')return {default_branch:'main'};
    if(path.startsWith('contents/README.md')) {if(o.method==='PUT'){assert.equal(o.data.sha,sha);readme=Buffer.from(o.data.content,'base64').toString(); puts++;}return {encoding:'base64',content:Buffer.from(readme).toString('base64'),sha};}
    if(path==='releases/4') {if(o.method==='PATCH'){body=o.data.body;patches++;}return {...release,body};}
    throw new Error(path);
  }};
  const result=refresh({remote,inventory:{android:null,windows:null,container:null},publish:true});
  assert.equal(puts,1);assert.equal(patches,1);assert.ok(readme.includes(result.table));assert.ok(body.includes(result.table));
  const writes=puts+patches;
  remote.api=()=>{throw new Error('concurrent edit');};
  assert.throws(()=>refresh({remote,inventory:{},publish:true}),/concurrent/);assert.equal(puts+patches,writes);
});
test('exact preview CLI requires output directory and accepts fixture injection without remote writes',()=>{
  assert.throws(()=>main(['--repository','test/agent']),/output-directory/);
});
