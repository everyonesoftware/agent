#!/usr/bin/env node
import { mkdirSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { client, discover, options } from './native-release.mjs';
const start='<!-- current-downloads:start -->',end='<!-- current-downloads:end -->';
export function renderDownloads(inventory) {
  const row=(key,title,action,status)=>{const i=inventory[key];return i?`| ${title} | ${i.manifest.versionName??i.manifest.version} | [${action}](${i.url}) · [SHA-256](${i.checksum}) | ${status} |`:`| ${title} | — | Not yet published | ${status} |`;};
  const c=inventory.container;
  return ['## Current downloads','','Each platform keeps its independently verified version and original download URL.','','| Platform | Version | Download | Status |','| --- | --- | --- | --- |',
    row('windows','Windows x64','Download installer (.exe)','Unsigned alpha; Windows may display a security warning. Requires your Agent server.'),
    row('android','Android','Download APK','Signed alpha; user-consented installation. Requires your Agent server.'),
    c?`| Container / Web server | ${c.manifest.version} | \`${c.manifest.image}\` · [Publication manifest](${c.url}) | [Deployment instructions](https://github.com/${inventory.repository}/blob/main/README.md#run-locally); requires server and local Agent account. |`:'| Container / Web server | — | [Deployment instructions](#run-locally) | No verified publication manifest yet; existing registry images are not relabeled. Requires server and local Agent account. |',
    '', 'Unchanged platforms are not rebuilt, copied, or relabeled for newer project releases. Native clients connect to your self-hosted server; they do not include a model runtime.'].join('\n');
}
export function replaceDownloads(text,table) {
  const begins=text.split(start).length-1,ends=text.split(end).length-1,block=`${start}\n${table}\n${end}`;
  if(begins||ends) {if(begins!==1||ends!==1||text.indexOf(end)<text.indexOf(start))throw new Error('Malformed download markers.');return text.slice(0,text.indexOf(start))+block+text.slice(text.indexOf(end)+end.length);}
  const heading=/^# [^\n]+\n/.exec(text);return heading?heading[0]+'\n'+block+'\n'+text.slice(heading[0].length):block+'\n\n'+text;
}
export function refresh({remote,inventory=discover(remote),publish=false,currentRunId=process.env.GITHUB_RUN_ID}) {
  // The workflow's global non-canceling queue serializes automated writers.
  // Completed builds are independent; do not wait for queued runs behind this one.
  if(publish) {
    const runs=remote.api('actions/runs?branch=main&status=in_progress&per_page=100',{paginate:true}).flatMap(p=>p.workflow_runs);
    if(runs.some(r=>r.status==='in_progress'&&String(r.id)!==String(currentRunId))) throw new Error('Concurrent publisher active; retry refresh.');
  }
  const table=renderDownloads(inventory),branch=remote.api('').default_branch;
  if(typeof branch!=='string'||!branch)throw new Error('Missing default branch.');
  const readmePath='contents/README.md?ref='+encodeURIComponent(branch),original=remote.api(readmePath);
  if(original.encoding!=='base64'||!original.sha||typeof original.content!=='string')throw new Error('Invalid README blob.');
  const oldReadme=Buffer.from(original.content,'base64').toString(),readme=replaceDownloads(oldReadme,table);
  const target=remote.list().filter(r=>!r.draft&&!r.prerelease&&/^v0\.1\.[1-9][0-9]*$/.test(r.tag_name)).sort((a,b)=>Number(b.tag_name.split('.').at(-1))-Number(a.tag_name.split('.').at(-1)))[0];
  const releaseNotes=target?replaceDownloads(target.body??'',table):null;
  if(publish) {
    if(target) {
      const fresh=remote.api('releases/'+target.id);
      if(fresh.body!==target.body||fresh.tag_name!==target.tag_name||fresh.draft||fresh.prerelease)throw new Error('Release changed; retry.');
      if(releaseNotes!==(target.body??''))remote.api('releases/'+target.id,{method:'PATCH',data:{body:releaseNotes}});
      const actual=remote.api('releases/'+target.id);if(actual.body!==releaseNotes||actual.tag_name!==target.tag_name)throw new Error('Release table readback mismatch.');
    }
    // Contents API provides a real blob-SHA precondition, unlike release PATCH.
    if(readme!==oldReadme)remote.api('contents/README.md',{method:'PUT',data:{message:'docs: refresh current platform downloads',branch,sha:original.sha,content:Buffer.from(readme).toString('base64')}});
    const actual=remote.api(readmePath);if(actual.encoding!=='base64'||Buffer.from(actual.content,'base64').toString()!==readme)throw new Error('README table readback mismatch.');
  }
  return {inventory,table,readme,releaseNotes,tag:target?.tag_name??null,published:publish};
}
export function main(argv=process.argv.slice(2),{makeClient=client}={}) {
  const o=options(argv,['repository','output-directory','publish']);
  if(!o.publish&&!o['output-directory'])throw new Error('Preview requires --output-directory.');
  const result=refresh({remote:makeClient(o.repository),publish:Boolean(o.publish)});
  if(o['output-directory']) {mkdirSync(o['output-directory'],{recursive:true});for(const [name,text] of [['inventory.json',JSON.stringify(result.inventory,null,2)+'\n'],['README.md',result.readme],['release.md',result.releaseNotes??result.table]])writeFileSync(join(o['output-directory'],name),text);}
  console.log(JSON.stringify({tag:result.tag,published:result.published},null,2));return result;
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){try{main();}catch(e){console.error(e.message);process.exitCode=1;}}
