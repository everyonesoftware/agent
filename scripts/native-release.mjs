#!/usr/bin/env node
import { spawnSync, execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync, readdirSync, mkdirSync, existsSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { isDeepStrictEqual } from 'node:util';
import { changelog, windowsPublicationHistory } from './native-changelog.mjs';
export const sha256 = b=>createHash('sha256').update(b).digest('hex');
const sha=/^[a-f0-9]{40}$/, digest=/^[a-f0-9]{64}$/;
const names={android:'agent-android-release.json',windows:'agent-windows-release.json',container:'agent-container-release.json'};
const manifestLimit=1024*1024;
export function verifiedAndroidSigner(output) {
  const count=/^Number of signers:\s*(\d+)\s*$/m.exec(output)?.[1];
  const certificates=[...output.matchAll(/^Signer #\d+ certificate SHA-256 digest: ([a-fA-F0-9]{64})\s*$/gm)];
  if(count!=='1'||certificates.length!==1||certificates[0][1].toLowerCase()!=='690987d5e9611640779257247f97de327831a01c4cf771ddf4f57dff5da8a5c0') throw new Error('APK must have exactly one dedicated Agent signer.');
  return certificates[0][1].toLowerCase();
}
export function options(argv,allowed) {
  const o={}; for(let i=0;i<argv.length;i++) { const key=argv[i].replace(/^--/,''); if(!argv[i].startsWith('--')||!allowed.includes(key)||Object.hasOwn(o,key)) throw new Error('Invalid option: '+argv[i]); if(key==='publish') o[key]=true; else { const value=argv[++i]; if(!value||value.startsWith('--')) throw new Error('Missing option value.'); o[key]=value; } } return o;
}
export function invoke(command,args,input,encoding='utf8') {
  const r=spawnSync(command,args,{input,encoding,shell:false,timeout:300000,maxBuffer:1024*1024*1024});
  if(r.error||r.status!==0) throw new Error(`${command} failed: ${r.error?.message||r.stderr||r.status}`); return r.stdout;
}
export function source(root='.') {
  const git=(...args)=>invoke('git',['-C',resolve(root),...args]).trim();
  if(git('rev-parse','--is-shallow-repository')!=='false') throw new Error('Full Git history required.');
  if(git('status','--porcelain')) throw new Error('Uncommitted tracked source; publication refused.');
  const commit=git('rev-parse','HEAD'),count=Number(git('rev-list','--count','HEAD'));
  if(!sha.test(commit)||!Number.isSafeInteger(count)||count<1) throw new Error('Invalid source identity.');
  return {commit,count,version:`0.1.${count}`};
}
function json(files,name) { if(!files.has(name)||files.get(name).length>manifestLimit) throw new Error('Missing/oversized manifest: '+name); return JSON.parse(files.get(name)); }
export function verifyPlatform(platform,files,expected={}) {
  const m=json(files,names[platform]);
  if(m.schemaVersion!==1||m.platform!==platform||!sha.test(m.commit)||!Number.isSafeInteger(m.versionCode)||m.versionCode<1) throw new Error('Invalid publication source/schema.');
  const version=`0.1.${m.versionCode}`;
  if((platform==='android'?m.versionName:m.version)!==(platform==='container'?String(m.versionCode):version) || (expected.commit&&expected.commit!==m.commit)||(expected.count&&expected.count!==m.versionCode)) throw new Error('Publication source/version mismatch.');
  if(platform==='container') {
    if(m.version!==String(m.versionCode)||m.image!==`ghcr.io/everyonesoftware/agent:${m.versionCode}`||!/^sha256:[a-f0-9]{64}$/.test(m.digest)) throw new Error('Invalid container identity.');
    return m;
  }
  const check=(entry,name)=>{
    if(!entry||entry.name!==name||!digest.test(entry.sha256)||!Number.isSafeInteger(entry.size)||entry.size<=0) throw new Error('Invalid artifact metadata.');
    const bytes=files.get(name); if(!bytes||bytes.length!==entry.size||sha256(bytes)!==entry.sha256) throw new Error('Artifact bytes mismatch: '+name);
  };
  const installer=platform==='android'?'Agent-android.apk':'AgentWindows-win-x64-Setup.exe';
  check(platform==='android'?m.apk:m.installer,installer);
  const entry=platform==='android'?m.apk:m.installer;
  if(files.get(installer+'.sha256')?.toString()!==`${entry.sha256}  ${installer}\n`) throw new Error('Checksum mismatch.');
  if(platform==='android') { if(!digest.test(m.signingCertificateSha256)||m.signingCertificateSha256!=='690987d5e9611640779257247f97de327831a01c4cf771ddf4f57dff5da8a5c0') throw new Error('Invalid signing certificate.'); }
  else {
    const name=`AgentWindows-${version}-win-x64-full.nupkg`; check(m.package,name);
    const feed=json(files,'releases.win-x64.json');
    if(!Array.isArray(feed.Assets)||feed.Assets.length!==1) throw new Error('Windows feed must describe exactly one full package.');
    const a=feed.Assets[0], bytes=files.get(name);
    if(a.PackageId!=='AgentWindows'||a.Version!==version||a.Type!=='Full'||a.FileName!==name||a.Size!==bytes.length||(!a.SHA256&&!a.SHA1)) throw new Error('Windows feed identity mismatch.');
    for(const [key,algo] of [['SHA256','sha256'],['SHA1','sha1']]) if(a[key]&&a[key].toLowerCase()!==createHash(algo).update(bytes).digest('hex')) throw new Error('Windows feed hash mismatch.');
  }
  return m;
}
function required(platform,count) {
  if(platform==='container') return [names.container];
  if(platform==='android') return ['Agent-android.apk','Agent-android.apk.sha256',names.android];
  return ['AgentWindows-win-x64-Setup.exe','AgentWindows-win-x64-Setup.exe.sha256',`AgentWindows-0.1.${count}-win-x64-full.nupkg`,'releases.win-x64.json',names.windows];
}
export function client(repository,{run=invoke}={}) {
  if(!/^[A-Za-z0-9_-][A-Za-z0-9_.-]*\/[A-Za-z0-9_-][A-Za-z0-9_.-]*$/.test(repository??'')||repository.split('/').some(s=>s==='.'||s==='..')) throw new Error('Invalid repository.');
  const api=(path,{method,data,paginate=false,binary=false}={})=>{
    const args=['api',`repos/${repository}${path?'/'+path:''}`];
    if(paginate) args.push('--paginate','--slurp');
    if(binary) args.push('-H','Accept: application/octet-stream');
    if(method) args.push('--method',method,'--input','-');
    const result=run('gh',args,data?JSON.stringify(data):undefined,binary?null:'utf8');
    return binary?Buffer.from(result):JSON.parse(result);
  };
  return {repository,api,list:()=>api('releases?per_page=100',{paginate:true}).flat(),assets:r=>api(`releases/${r.id}/assets?per_page=100`,{paginate:true}).flat(),bytes:a=>api(`releases/assets/${a.id}`,{binary:true}),
    tag:tag=>{let o=api('git/ref/tags/'+encodeURIComponent(tag)).object; const seen=new Set(); while(o?.type==='tag') {if(!sha.test(o.sha)||seen.has(o.sha)||seen.size>=16) throw new Error('Invalid tag chain.');seen.add(o.sha);o=api('git/tags/'+o.sha).object;} if(o?.type!=='commit'||!sha.test(o.sha)) throw new Error('Invalid release tag.');return o.sha;},
    get:id=>api('releases/'+id),create:data=>api('releases',{method:'POST',data}),edit:(id,data)=>api('releases/'+id,{method:'PATCH',data}),
    upload:(release,name,bytes)=>{const dir=mkdtempSync(join(tmpdir(),'agent-upload-'));try {const path=join(dir,name);writeFileSync(path,bytes);run('gh',['release','upload',release.tag_name,path,'--repo',repository]);}finally{rmSync(dir,{recursive:true,force:true});}},
    verifyContainer:m=>{const remote=JSON.parse(run('docker',['buildx','imagetools','inspect',m.image,'--format','{{json .Manifest}}'])); if(remote.digest!==m.digest) throw new Error('Registry digest readback mismatch.');},
  };
}
export function discover(remote) {
  const result={repository:remote.repository,android:null,windows:null,container:null};
  const releases=remote.list().filter(r=>!r.draft&&!r.prerelease&&/^v0\.1\.[1-9][0-9]*$/.test(r.tag_name)).sort((a,b)=>Number(b.tag_name.split('.').at(-1))-Number(a.tag_name.split('.').at(-1)));
  for(const r of releases) {
    const count=Number(r.tag_name.split('.').at(-1)); if(!Number.isSafeInteger(count)) throw new Error('Unsafe release version.');
    const assets=remote.assets(r);
    for(const platform of ['android','windows','container']) {
      const hasPlatformAssets=assets.some(a=>a.name===names[platform] || (platform==='android' && /^Agent-android\.apk(?:\.sha256)?$/.test(a.name)) || (platform==='windows' && (/^AgentWindows-/.test(a.name) || ['releases.win-x64.json','assets.win-x64.json','RELEASES','RELEASES-win-x64'].includes(a.name))));
      if(result[platform]||!hasPlatformAssets) continue;
      if(!assets.some(a=>a.name===names[platform])) throw new Error('Incomplete/invalid publication asset: '+names[platform]);
      const files=new Map();
      for(const name of required(platform,count)) {
        const match=assets.filter(a=>a.name===name);
        if(match.length!==1||match[0].state!=='uploaded'||!Number.isSafeInteger(match[0].id)||match[0].id<=0||match[0].size<=0||match[0].browser_download_url!==`https://github.com/${remote.repository}/releases/download/${r.tag_name}/${name}`) throw new Error('Incomplete/invalid publication asset: '+name);
        const bytes=remote.bytes(match[0]); if(bytes.length!==match[0].size) throw new Error('Asset bytes size readback mismatch.'); files.set(name,bytes);
      }
      const manifest=verifyPlatform(platform,files,{count});
      if(r.target_commitish!==manifest.commit||remote.tag(r.tag_name)!==manifest.commit) throw new Error('Release tag/source mismatch.');
      if(platform==='container') remote.verifyContainer(manifest);
      const name=platform==='android'?manifest.apk.name:platform==='windows'?manifest.installer.name:names.container;
      result[platform]={manifest,tag:r.tag_name,releaseId:r.id,url:`https://github.com/${remote.repository}/releases/download/${r.tag_name}/${name}`,checksum:`https://github.com/${remote.repository}/releases/download/${r.tag_name}/${name}.sha256`};
    }
  }
  return result;
}
export function publishInventory(remote,{commit,count,items,notes=''}) {
  if(!sha.test(commit)||!Number.isSafeInteger(count)||count<1||!items.length) throw new Error('Invalid publication request.');
  const tag=`v0.1.${count}`, title=`Agent 0.1.${count}`;
  for(const i of items) verifyPlatform(i.platform,i.files,{commit,count});
  let r=remote.list().find(r=>r.tag_name===tag);
  if(r) {
    let tagCommit;
    try { tagCommit=remote.tag(tag); } catch(error) { if(!r.draft||!/HTTP 404/.test(error.message))throw error; }
    if(r.target_commitish!==commit||r.name!==title||r.prerelease||(tagCommit&&tagCommit!==commit)) throw new Error('Existing release source identity conflict.');
  }
  else {r=remote.create({tag_name:tag,target_commitish:commit,name:title,body:notes,draft:true,prerelease:false,make_latest:'false'});if(r.target_commitish!==commit||r.tag_name!==tag) throw new Error('Release creation readback mismatch.');}
  const stableAsset = asset => Object.fromEntries(Object.entries(asset).filter(([key])=>key!=='download_count'));
  const preexisting = new Map(remote.assets(r).map(asset=>[asset.id,structuredClone(stableAsset(asset))]));
  for(const item of items) for(const [name,bytes] of item.files) {
    const current=remote.assets(r).filter(a=>a.name===name);
    if(current.length>1) throw new Error('Duplicate existing asset conflict.');
    if(current.length) {if(!remote.bytes(current[0]).equals(bytes)) throw new Error('Existing asset bytes conflict: '+name);}
    else remote.upload(r,name,bytes);
    const readback=remote.assets(r).filter(a=>a.name===name);
    if(readback.length!==1||!remote.bytes(readback[0]).equals(bytes)) throw new Error('Publication bytes readback mismatch: '+name);
  }
  for(const item of items) if(item.platform==='container') remote.verifyContainer(item.manifest);
  // Users may download while we verify. All other preexisting asset fields,
  // including IDs, labels, state and hashes, must remain unchanged.
  const after = remote.assets(r);
  for(const [id,original] of preexisting) {
    const matching=after.filter(asset=>asset.id===id);
    if(matching.length!==1||!isDeepStrictEqual(stableAsset(matching[0]),original)) throw new Error('A preexisting asset changed during publication.');
  }
  if(r.draft) remote.edit(r.id,{draft:false,prerelease:false,make_latest:'true'});
  const verified=remote.get(r.id); if(verified.draft||verified.prerelease||verified.target_commitish!==commit||remote.tag(tag)!==commit) throw new Error('Stable release readback mismatch.');
  return verified;
}
export function loadDirectory(platform,directory,expected={}) {
  const files=new Map(readdirSync(directory,{withFileTypes:true}).filter(f=>f.isFile()).map(f=>[f.name,readFileSync(join(directory,f.name))]));
  const manifest=verifyPlatform(platform,files,expected);
  const allowed=new Set([...required(platform,manifest.versionCode),`agent-${platform}-changelog.json`,...(platform==='windows'?['RELEASES','RELEASES-win-x64','assets.win-x64.json']:[])]);
  for(const name of files.keys()) if(!allowed.has(name)) throw new Error('Unexpected publication file: '+name);
  return {platform,files,manifest};
}
export function publishRelease(remote,{root='.',identity,items}) {
  const prior=discover({...remote,list:()=>remote.list().filter(r=>r.tag_name!==`v${identity.version}`)}), sections=[];
  for(const item of items) {
    if(item.platform==='windows') {
      const manifest=verifyPlatform('windows',item.files,identity);
      manifest.changelog=windowsPublicationHistory({root,to:manifest.commit,count:manifest.versionCode,maxBytes:manifestLimit});
      const encode=()=>Buffer.from(JSON.stringify(manifest)+'\n');
      let bytes=encode();
      if(bytes.length>manifestLimit) {
        manifest.changelog={schemaVersion:1,truncated:true};
        bytes=encode();
      }
      item.files.set(names.windows,bytes);
    }
    const notes=changelog({root,platform:item.platform,from:prior[item.platform]?.manifest.commit??null,to:identity.commit});
    item.files.set(`agent-${item.platform}-changelog.json`,Buffer.from(JSON.stringify(notes,null,2)+'\n'));
    const groups=[['Improvements',notes.improvements],['Fixes',notes.fixes]].filter(([,entries])=>entries.length).map(([title,entries])=>`### ${title}\n\n`+entries.map(e=>`- ${e.text} (${e.commit.slice(0,7)})`).join('\n'));
    sections.push(`## ${item.platform==='android'?'Android':item.platform==='windows'?'Windows':'Container'}\n\n`+groups.join('\n\n'));
  }
  return publishInventory(remote,{...identity,items,notes:sections.join('\n\n')});
}
export function main(argv=process.argv.slice(2)) {
  const command=argv.shift(),o=options(argv,['repository','directory','apk','apksigner','aapt','count','commit','digest']);
  if(command==='verify') {let count=0;for(const platform of ['android','windows','container']) {const path=join(o.directory,platform);if(existsSync(path)) {loadDirectory(platform,path);count++;}}if(!count)throw new Error('No platform inventory found.');return;}
  if(command==='package-android') {
    const identity=source(); const signer=invoke(o.apksigner,['verify','--verbose','--print-certs',o.apk]);
    const cert=verifiedAndroidSigner(signer);
    const badging=invoke(o.aapt,['dump','badging',o.apk]);
    if(!badging.includes(`package: name='com.everyonesoftware.agent' versionCode='${identity.count}' versionName='${identity.version}'`)) throw new Error('APK package/version mismatch.');
    const bytes=readFileSync(o.apk),name='Agent-android.apk'; mkdirSync(o.directory,{recursive:true});
    writeFileSync(join(o.directory,name),bytes); writeFileSync(join(o.directory,name+'.sha256'),`${sha256(bytes)}  ${name}\n`);
    writeFileSync(join(o.directory,names.android),JSON.stringify({schemaVersion:1,platform:'android',versionName:identity.version,versionCode:identity.count,commit:identity.commit,apk:{name,size:bytes.length,sha256:sha256(bytes)},signingCertificateSha256:cert},null,2)+'\n');
    return loadDirectory('android',o.directory,identity);
  }
  if(command==='container') {
    if(!sha.test(o.commit)||!/^sha256:[a-f0-9]{64}$/.test(o.digest??'')) throw new Error('Invalid container source/digest.');
    const m={schemaVersion:1,platform:'container',version:String(o.count),versionCode:Number(o.count),commit:o.commit,image:`ghcr.io/everyonesoftware/agent:${o.count}`,digest:o.digest};
    // Container keeps its numeric version identity, independently of native tags.
    mkdirSync(o.directory,{recursive:true});writeFileSync(join(o.directory,names.container),JSON.stringify(m,null,2)+'\n');client(o.repository).verifyContainer(m);return;
  }
  if(command==='publish') {
    const identity=source(),remote=client(o.repository),items=[];
    for(const platform of ['android','windows','container']) if(existsSync(join(o.directory,platform))) items.push(loadDirectory(platform,join(o.directory,platform),identity));
    return publishRelease(remote,{identity,items});
  }
  throw new Error('Expected package-android, verify, container or publish.');
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href) {try{main();}catch(e){console.error(e.message);process.exitCode=1;}}
