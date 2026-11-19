import assert from 'node:assert/strict';
import {test} from 'node:test';
import {createHash} from 'node:crypto';
import * as releaseTools from '../scripts/native-release.mjs';
const commit='a'.repeat(40),certificate='690987d5e9611640779257247f97de327831a01c4cf771ddf4f57dff5da8a5c0';
function fixture(){const apk=Buffer.from('signed APK fixture'),name='Agent-android.apk',digest=createHash('sha256').update(apk).digest('hex');const manifest={schemaVersion:1,platform:'android',versionName:'0.1.7',versionCode:7,commit,apk:{name,size:apk.length,sha256:digest},signingCertificateSha256:certificate};const files=new Map([[name,apk],[name+'.sha256',Buffer.from(`${digest}  ${name}\n`)],['agent-android-release.json',Buffer.from(JSON.stringify(manifest))]]);const release={id:1,tag_name:'v0.1.7',name:'Agent 0.1.7',target_commitish:commit,draft:false,prerelease:false,body:'Keep these notes',assets:[...files].map(([name,bytes],index)=>({id:index+1,name,size:bytes.length,state:'uploaded',browser_download_url:`https://github.com/test/agent/releases/download/v0.1.7/${name}`}))};return {files,release,remote:{repository:'test/agent',list:()=>[release],assets:r=>r.assets,bytes:a=>files.get(a.name),tag:()=>commit,get:()=>release,edit:()=>{}}};}

test('Android packaging requires exactly one dedicated Agent signer',()=>{
 assert.equal(typeof releaseTools.verifiedAndroidSigner,'function','packaging must validate the complete signer count');
 assert.equal(releaseTools.verifiedAndroidSigner('Number of signers: 1\r\nSigner #1 certificate SHA-256 digest: '+certificate),certificate);
 assert.throws(()=>releaseTools.verifiedAndroidSigner('Number of signers: 2\nSigner #1 certificate SHA-256 digest: '+certificate),/signer/);
 assert.throws(()=>releaseTools.verifiedAndroidSigner('Number of signers: 1\nSigner #1 certificate SHA-256 digest: '+'f'.repeat(64)),/signer/);
});
test('publication detects changes to preexisting asset metadata after uploads and byte readbacks',()=>{
 const {files,release,remote}=fixture(),itemFiles=new Map(files);itemFiles.set('agent-android-changelog.json',Buffer.from('{}'));
 remote.upload=(_r,name,bytes)=>{files.set(name,bytes);release.assets.push({id:100,name,size:bytes.length,state:'uploaded',browser_download_url:`https://github.com/test/agent/releases/download/v0.1.7/${name}`});release.assets[0].label='unexpected concurrent edit';};
 assert.throws(()=>releaseTools.publishInventory(remote,{commit,count:7,items:[{platform:'android',files:itemFiles,manifest:releaseTools.verifyPlatform('android',itemFiles)}]}),/preexisting asset/);
});
test('ordinary concurrent downloads do not prevent idempotent release verification',()=>{
 const {files,release,remote}=fixture(),read=remote.bytes;remote.bytes=a=>{a.download_count=(a.download_count??0)+1;return read(a);};
 assert.equal(releaseTools.publishInventory(remote,{commit,count:7,items:[{platform:'android',files,manifest:releaseTools.verifyPlatform('android',files)}]}).tag_name,'v0.1.7');
});
