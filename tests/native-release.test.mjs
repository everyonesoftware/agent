import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createHash } from 'node:crypto';
import { verifyPlatform, discover, publishInventory, client, source, loadDirectory, main } from '../scripts/native-release.mjs';
import { mkdtempSync, writeFileSync, mkdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';
const commit='a'.repeat(40), hash=b=>createHash('sha256').update(b).digest('hex');
export function androidFixture(count=7) {
  const apk=Buffer.from('signed APK fixture'), name='Agent-android.apk';
  const manifest={schemaVersion:1,platform:'android',versionName:`0.1.${count}`,versionCode:count,commit,apk:{name,size:apk.length,sha256:hash(apk)},signingCertificateSha256:'690987d5e9611640779257247f97de327831a01c4cf771ddf4f57dff5da8a5c0'};
  return new Map([[name,apk],[name+'.sha256',Buffer.from(`${hash(apk)}  ${name}\n`)],['agent-android-release.json',Buffer.from(JSON.stringify(manifest))]]);
}
test('full Android inventory verifies exact source/version/hash/checksum and rejects byte tampering',()=>{
  const files=androidFixture(); const m=verifyPlatform('android',files,{commit,count:7});
  assert.equal(m.versionCode,7);
  const wrongSigner=androidFixture(),wrongManifest=JSON.parse(wrongSigner.get('agent-android-release.json'));wrongManifest.signingCertificateSha256='f'.repeat(64);wrongSigner.set('agent-android-release.json',Buffer.from(JSON.stringify(wrongManifest)));
  assert.throws(()=>verifyPlatform('android',wrongSigner),/certificate/);
  assert.throws(()=>verifyPlatform('android',files,{commit:'c'.repeat(40),count:7}),/source/);
  files.set('Agent-android.apk',Buffer.from('tampered'));
  assert.throws(()=>verifyPlatform('android',files,{commit,count:7}),/bytes/);
});
test('Windows feed must contain the exact full package and matching hashes, not an EXE-only publication',()=>{
  const installer=Buffer.from('EXE fixture'), pkg=Buffer.from('full package fixture');
  const name='AgentWindows-0.1.7-win-x64-full.nupkg';
  const manifest={schemaVersion:1,platform:'windows',version:'0.1.7',versionCode:7,commit,installer:{name:'AgentWindows-win-x64-Setup.exe',size:installer.length,sha256:hash(installer)},package:{name,size:pkg.length,sha256:hash(pkg)}};
  const files=new Map([[manifest.installer.name,installer],[manifest.installer.name+'.sha256',Buffer.from(`${hash(installer)}  ${manifest.installer.name}\n`)], [name,pkg],['agent-windows-release.json',Buffer.from(JSON.stringify(manifest))],['releases.win-x64.json',Buffer.from(JSON.stringify({Assets:[{PackageId:'AgentWindows',Version:'0.1.7',Type:'Full',FileName:name,Size:pkg.length,SHA256:hash(pkg)}]}))]]);
  assert.equal(verifyPlatform('windows',files).version,'0.1.7');
  const feed=JSON.parse(files.get('releases.win-x64.json')); feed.Assets[0].Version='0.1.6'; files.set('releases.win-x64.json',Buffer.from(JSON.stringify(feed)));
  assert.throws(()=>verifyPlatform('windows',files),/feed/);
});
function remoteFixture() {
  const files=androidFixture();
  const release={id:1,tag_name:'v0.1.7',name:'Agent 0.1.7',target_commitish:commit,draft:false,prerelease:false,body:'Real notes',assets:[...files].map(([name,data],i)=>({id:i+1,name,size:data.length,state:'uploaded',browser_download_url:`https://github.com/test/agent/releases/download/v0.1.7/${name}`}))};
  return {files,release,remote:{repository:'test/agent',list:()=>[release],assets:r=>r.assets,bytes:a=>files.get(a.name),tag:()=>commit}};
}
test('discovery downloads bytes, resolves real tags and retains original URL; invalid highest candidate fails closed',()=>{
  const {files,remote}=remoteFixture();
  assert.equal(discover(remote).android.url,'https://github.com/test/agent/releases/download/v0.1.7/Agent-android.apk');
  files.set('Agent-android.apk',Buffer.from('bad'));
  assert.throws(()=>discover(remote),/bytes/);
});
for(const [platform,assets] of [
  ['android',['Agent-android.apk']],
  ['windows',['AgentWindows-win-x64-Setup.exe']],
  ['windows',['releases.win-x64.json']],
  ['windows',['AgentWindows-0.1.8-win-x64-full.nupkg']],
]) test(`discovery rejects newer ${platform} ${assets[0]} without its manifest instead of retaining an older baseline`,()=>{
  const {release,remote}=remoteFixture();
  const newer={...release,id:2,tag_name:'v0.1.8',assets:assets.map((name,i)=>({id:100+i,name}))};
  remote.list=()=>[release,newer];
  assert.throws(()=>discover(remote),/Incomplete\/invalid publication asset/);
});
test('discovery retains the original platform publication when newer releases have no platform assets',()=>{
  const {release,remote}=remoteFixture();
  remote.list=()=>[release,{...release,id:2,tag_name:'v0.1.8',assets:[{name:'notes.txt'}]}];
  assert.equal(discover(remote).android.tag,'v0.1.7');
  assert.equal(discover(remote).windows,null);
});
test('publication is idempotent only for identical existing bytes and never clobbers conflicts',()=>{
  const {files,release,remote}=remoteFixture(); let uploads=0;
  remote.upload=()=>{uploads++;}; remote.get=()=>release; remote.edit=()=>{};
  const item={platform:'android',files,manifest:verifyPlatform('android',files)};
  assert.equal(publishInventory(remote,{commit,count:7,items:[item],notes:'notes'}).tag_name,'v0.1.7');
  assert.equal(uploads,0);
  remote.bytes=a=>a.name==='Agent-android.apk'?Buffer.from('conflicting'):files.get(a.name);
  assert.throws(()=>publishInventory(remote,{commit,count:7,items:[item],notes:'notes'}),/conflict/);
  assert.equal(uploads,0);
});
test('resuming a draft requires exact target identity but does not require an as-yet-unpublished tag ref',()=>{
 const {files,release,remote}=remoteFixture();release.draft=true;let published=false;
 remote.tag=()=>{if(!published)throw new Error('HTTP 404: draft has no tag yet');return commit;};
 remote.get=()=>release;
 remote.edit=(_id,data)=>{assert.equal(data.draft,false);release.draft=false;published=true;};
 assert.equal(publishInventory(remote,{commit,count:7,items:[{platform:'android',files,manifest:verifyPlatform('android',files)}]}).draft,false);
});
test('verifier rejects empty inventories, unknown files, and unsafe optional Windows output',()=>{
 const dir=mkdtempSync(join(tmpdir(),'agent-empty-'));
 try {assert.throws(()=>main(['verify','--directory',dir]),/inventory/);mkdirSync(join(dir,'android'));for(const [name,bytes] of androidFixture())writeFileSync(join(dir,'android',name),bytes);writeFileSync(join(dir,'android','not-a-release.txt'),'unknown');assert.throws(()=>loadDirectory('android',join(dir,'android')),/Unexpected/);}finally{rmSync(dir,{recursive:true,force:true});}
});
test('Git version requires full clean history, including no untracked source',()=>{
 const dir=mkdtempSync(join(tmpdir(),'agent-source-')),git=(...args)=>execFileSync('git',['-C',dir,...args],{encoding:'utf8'}).trim();
 try{git('init','-q');writeFileSync(join(dir,'source'),'one');git('add','.');git('-c','user.name=Test','-c','user.email=test@example.test','commit','-qm','first');assert.deepEqual(source(dir),{commit:git('rev-parse','HEAD'),count:1,version:'0.1.1'});writeFileSync(join(dir,'untracked'),'two');assert.throws(()=>source(dir),/Uncommitted/);}finally{rmSync(dir,{recursive:true,force:true});}
});
test('GitHub command fixture uploads without clobber and paginates reads',()=>{
 const calls=[],remote=client('test/agent',{run:(command,args,input,encoding)=>{calls.push({command,args,input,encoding});if(args[0]==='release')return '';return args.includes('--slurp')?'[[]]':'{}';}});
 assert.deepEqual(remote.list(),[]);remote.upload({tag_name:'v0.1.7'},'Agent-android.apk',Buffer.from('fixture'));
 assert.ok(calls[0].args.includes('--paginate'));assert.ok(calls[0].args.includes('--slurp'));
 assert.equal(calls[1].command,'gh');assert.deepEqual(calls[1].args.slice(0,3),['release','upload','v0.1.7']);assert.ok(!calls[1].args.includes('--clobber'));
});
