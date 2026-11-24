import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createHash } from 'node:crypto';
import { verifyPlatform, discover, publishInventory, publishRelease, client, source, loadDirectory, main } from '../scripts/native-release.mjs';
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
function windowsFixture({commit,count}) {
  const installer=Buffer.from('EXE fixture'), pkg=Buffer.from('full package fixture');
  const version=`0.1.${count}`,name=`AgentWindows-${version}-win-x64-full.nupkg`;
  const manifest={schemaVersion:1,platform:'windows',version,versionCode:count,commit,installer:{name:'AgentWindows-win-x64-Setup.exe',size:installer.length,sha256:hash(installer)},package:{name,size:pkg.length,sha256:hash(pkg)}};
  return new Map([[manifest.installer.name,installer],[manifest.installer.name+'.sha256',Buffer.from(`${hash(installer)}  ${manifest.installer.name}\n`)], [name,pkg],['agent-windows-release.json',Buffer.from(JSON.stringify(manifest))],['releases.win-x64.json',Buffer.from(JSON.stringify({Assets:[{PackageId:'AgentWindows',Version:version,Type:'Full',FileName:name,Size:pkg.length,SHA256:hash(pkg)}]}))]]);
}
test('Windows feed must contain the exact full package and matching hashes, not an EXE-only publication',()=>{
  const files=windowsFixture({commit,count:7});
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

function publicationFixture(t) {
  const root=mkdtempSync(join(tmpdir(),'agent-history-'));
  t.after(()=>rmSync(root,{recursive:true,force:true}));
  const git=(...args)=>execFileSync('git',['-C',root,'-c','user.name=Test','-c','user.email=test@example.test',...args],{encoding:'utf8'}).trim();
  git('init','-q','-b','main');
  const add=(path,subject)=>{
    if(path.includes('/'))mkdirSync(join(root,path.slice(0,path.lastIndexOf('/'))),{recursive:true});
    writeFileSync(join(root,path),subject);git('add','.');git('commit','-q','--cleanup=verbatim','-F',path);return git('rev-parse','HEAD');
  };
  const releases=[], data=new Map(), reads=[];
  let nextId=1;
  const remote={repository:'test/agent',list:()=>releases,assets:r=>r.assets,tag:tag=>releases.find(r=>r.tag_name===tag).target_commitish,
    bytes:a=>{reads.push(a.id);return data.get(a.id);},get:id=>releases.find(r=>r.id===id),
    create:r=>{const release={...r,id:nextId++,assets:[]};releases.push(release);return release;},
    edit:(id,patch)=>Object.assign(remote.get(id),patch),
    upload:(r,name,bytes)=>{
      const asset={id:nextId++,name,size:bytes.length,state:'uploaded',browser_download_url:`https://github.com/test/agent/releases/download/${r.tag_name}/${name}`};
      r.assets.push(asset);data.set(asset.id,Buffer.from(bytes));
    }};
  const publish=(files=windowsFixture(source(root)))=>{
    const identity=source(root), manifest=verifyPlatform('windows',files,identity);
    return publishRelease(remote,{root,identity,items:[{platform:'windows',files,manifest}]});
  };
  return {root,git,add,remote,data,reads,publish};
}

const windowsManifest='agent-windows-release.json';
test('published Windows details preserve the first message line without folding continuation text',t=>{
  const {add,data,publish}=publicationFixture(t);
  const sha=add('apps/windows/main.cs','[verified] feat(windows): First line  \ncontinuation not part of the subject\n\nBody\n');
  const release=publish();
  const embedded=JSON.parse(data.get(release.assets.find(a=>a.name===windowsManifest).id)).changelog;
  assert.deepEqual(embedded.commits,[{sha,parents:[],subject:'[verified] feat(windows): First line  '}]);
  assert.deepEqual(embedded.improvements,[{commit:sha,text:'First line'}]);
  const legacy=JSON.parse(data.get(release.assets.find(a=>a.name==='agent-windows-changelog.json').id));
  assert.equal(legacy.commits[0].subject,'[verified] feat(windows): First line continuation not part of the subject');
});

test('published Windows bullets are nonblank single lines without changing raw commit details',t=>{
  const {add,data,publish}=publicationFixture(t);
  const empty=add('apps/windows/main.cs','fix:');
  const blank=add('apps/windows/main.cs','feat: \u0085');
  const fixed=add('apps/windows/main.cs',' [verified] fix(windows)!: Restore\rconnection \r\n');
  const release=publish();
  const embedded=JSON.parse(data.get(release.assets.find(a=>a.name===windowsManifest).id)).changelog;
  assert.deepEqual(embedded.fixes,[{commit:fixed,text:'Restore connection'}]);
  assert.deepEqual(embedded.improvements,[]);
  assert.deepEqual(embedded.commits.map(({sha,subject})=>[sha,subject]),[
    [empty,'fix:'],[blank,'feat: \u0085'],[fixed,' [verified] fix(windows)!: Restore\rconnection \r'],
  ]);
});

test('Windows embedded summaries omit proven web-only changes but keep shared changes and all commit details',t=>{
  const {root,git,add,data,publish}=publicationFixture(t);
  const webOnly=[
    add('page.go','fix(settings): Align browser buttons'),
    add('pwa.go','feat: Serve web manifest'),
    add('pwa_updates.go','fix: Refresh the web app'),
    add('pwa/icône.svg','fix: Update offline artwork'),
    add('tests/pwa-browser.mjs','fix: Check web installation'),
  ];
  git('commit','-q','--allow-empty','-m','fix(web): Refresh browser deployment');
  const scoped=git('rev-parse','HEAD');
  const other=[
    add('apps/android/main.kt','fix: Fix phone layout'),
    add('docs/usage.md','fix: Correct instructions'),
    add('.github/workflows/check.yml','fix: Adjust CI steps'),
    add('Dockerfile','fix: Adjust container packaging'),
  ];
  writeFileSync(join(root,'page.go'),'web change');
  const shared=add('server.go','fix(web): Keep API and web client in sync');
  const windows=add('apps/windows/main.cs','feat: Add keyboard navigation');
  const release=publish();
  const embedded=JSON.parse(data.get(release.assets.find(a=>a.name===windowsManifest).id)).changelog;
  assert.deepEqual(embedded.fixes,[{commit:shared,text:'Keep API and web client in sync'}]);
  assert.deepEqual(embedded.improvements,[{commit:windows,text:'Add keyboard navigation'}]);
  assert.deepEqual(embedded.commits.map(c=>c.sha),[...webOnly,scoped,...other,shared,windows]);
  const legacy=JSON.parse(data.get(release.assets.find(a=>a.name==='agent-windows-changelog.json').id));
  assert.ok(webOnly.every(sha=>legacy.commits.find(c=>c.commit===sha).relevant),'standalone notes keep legacy relevance');
});

test('published Windows manifest carries complete topo-ordered Git history across older releases and merges',t=>{
  const {root,git,add,remote,data,reads,publish}=publicationFixture(t);
  const first=add('shared','feat: start Agent');
  const initial=source(root),initialFiles=windowsFixture(initial);
  const legacy=publishInventory(remote,{...initial,items:[{platform:'windows',files:initialFiles,manifest:verifyPlatform('windows',initialFiles)}]});
  git('branch','side');
  const mainSha=add('apps/windows/main.cs','[verified] fix(windows): Fix main window');
  const skipped=publish();
  const skippedManifest=data.get(skipped.assets.find(a=>a.name===windowsManifest).id);
  git('checkout','-q','side');
  const side=add('apps/android/side.kt','feat(android): Add phone UI');
  const shared=add('common','feat: Add shared behavior');
  git('checkout','-q','main');git('merge','-q','--no-ff','side','-m','Merge feature');
  const offered=source(root),files=windowsFixture(offered),original=verifyPlatform('windows',files);
  const release=publish(files), asset=release.assets.find(a=>a.name===windowsManifest);
  assert.ok(reads.includes(asset.id),'publisher must read back the embedded manifest bytes');
  const bytes=data.get(asset.id), {changelog:embedded,...outer}=JSON.parse(bytes);
  assert.deepEqual(outer,original);
  assert.ok(embedded,'released Windows manifest has no embedded history');
  assert.deepEqual(Object.keys(embedded).sort(),['commits','fixes','improvements','schemaVersion','truncated']);
  assert.equal(embedded.schemaVersion,1);assert.equal(embedded.truncated,false);
  assert.equal(embedded.commits.length,offered.count);
  assert.deepEqual(embedded.commits[0],{sha:first,parents:[],subject:'feat: start Agent'});
  assert.deepEqual(embedded.commits.at(-1),{sha:offered.commit,parents:[mainSha,shared],subject:'Merge feature'});
  const seen=new Set();
  for(const {sha,parents} of embedded.commits) {
    assert.ok(parents.every(parent=>seen.has(parent)));assert.ok(!seen.has(sha));seen.add(sha);
  }
  assert.deepEqual(seen,new Set([first,mainSha,side,shared,offered.commit]));
  assert.equal(embedded.commits.find(c=>c.sha===mainSha).subject,'[verified] fix(windows): Fix main window');
  assert.deepEqual(embedded.fixes,[{commit:mainSha,text:'Fix main window'}]);
  assert.deepEqual(embedded.improvements.map(e=>e.text).sort(),['Add shared behavior','Merge feature','start Agent']);
  const standalone=JSON.parse(data.get(release.assets.find(a=>a.name==='agent-windows-changelog.json').id));
  assert.equal(standalone.from,mainSha);assert.equal(standalone.to,offered.commit);
  assert.deepEqual(data.get(legacy.assets.find(a=>a.name===windowsManifest).id),initialFiles.get(windowsManifest));
  assert.strictEqual(data.get(skipped.assets.find(a=>a.name===windowsManifest).id),skippedManifest);
  assert.equal(publish(windowsFixture(offered)).id,release.id,'published bytes are deterministic on retry');
});

function publishLegacyWindows(remote,identity) {
  const files=windowsFixture(identity);
  return publishInventory(remote,{...identity,items:[{platform:'windows',files,manifest:verifyPlatform('windows',files)}]});
}

for(const [name,subject] of [['encoded JSON', '"'.repeat(525000)],['raw Git output','é'.repeat(525000)]]) {
  test(`Windows ${name} overflow publishes a downloadable manifest with only a truncation marker`,t=>{
    const {root,add,remote,data,publish}=publicationFixture(t);
    add('docs/long.txt',subject);
    publishLegacyWindows(remote,source(root));
    const head=add('shared','feat: Small current change');
    const release=publish(), bytes=data.get(release.assets.find(a=>a.name===windowsManifest).id);
    assert.ok(bytes.length<=1024*1024);
    assert.deepEqual(JSON.parse(bytes).changelog,{schemaVersion:1,truncated:true});
    assert.equal(discover(remote).windows.manifest.commit,head,'the package remains discoverable');
  });
}

test('Windows history over 2048 commits does not publish a partial graph',t=>{
  const {root,add,remote,data,publish}=publicationFixture(t);
  const importInput=Array.from({length:2048},(_,i)=>
    `commit refs/heads/main\ncommitter Test <test@example.test> ${1700000000+i} +0000\ndata 4\nSeed\n\n`).join('');
  execFileSync('git',['-C',root,'fast-import','--quiet'],{input:importInput});
  const prior=source(root);assert.equal(prior.count,2048);
  publishLegacyWindows(remote,prior);
  const head=add('shared','fix: Offer a new package');
  const release=publish(), bytes=data.get(release.assets.find(a=>a.name===windowsManifest).id);
  assert.deepEqual(JSON.parse(bytes).changelog,{schemaVersion:1,truncated:true});
  assert.equal(discover(remote).windows.manifest.commit,head);
});
