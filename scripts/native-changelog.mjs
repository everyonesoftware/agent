#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { options } from './native-release.mjs';
const sha=/^[a-f0-9]{40}$/;
const gitAt=root=>(...args)=>execFileSync('git',['-C',resolve(root),...args],{encoding:'utf8',maxBuffer:64*1024*1024}).trim();
const pathsFor=(git,commit)=>git('diff-tree','--root','--no-commit-id','--name-only','-r','-m',commit).split('\n').filter(Boolean);
const unrelatedPath=(platform,path)=>path.startsWith('docs/')||/(?:^|\/)README\.md$/.test(path)||path.startsWith('.github/')||/^scripts\/(?:native-selection|native-release|native-changelog|current-downloads)\.mjs$/.test(path)||(platform!=='windows'&&path.startsWith('apps/windows/'))||(platform!=='android'&&path.startsWith('apps/android/'));
function isRelevant(platform,paths) {
 return !(paths.length&&paths.every(path=>unrelatedPath(platform,path)));
}
const nonWindowsFiles=new Set([
 'page.go','page_test.go','pwa.go','pwa_test.go','pwa_updates.go',
 'tests/pwa-browser.mjs','tests/pwa-updates.mjs',
 'Dockerfile','.dockerignore','compose.yaml',
]);
function isWindowsRelevant(git,{sha,subject}) {
 const paths=git('diff-tree','--root','--no-commit-id','--name-only','--no-renames','-r','-m','-z',sha).split('\0').filter(Boolean);
 // A scope is only useful without path evidence. A web-labelled commit can also change the shared API.
 if(!paths.length)return !/^\s*(?:\[verified\]\s*)?[a-z]+\((?:web|pwa|browser|android|container|docs|ci)\)!?:/i.test(subject);
 return paths.some(path=>!unrelatedPath('windows',path)&&!nonWindowsFiles.has(path)&&!path.startsWith('pwa/'));
}
function groups(commits) {
 const improvements=[],fixes=[];
 for(const {commit,subject} of commits) {
   const text=subject.replace(/^\[verified\]\s*/i,'').replace(/^[a-z]+(?:\([^)]*\))?!?:\s*/i,'');
   (/^(?:\[verified\]\s*)?(?:fix|bugfix)(?:\([^)]*\))?:/i.test(subject)?fixes:improvements).push({text,commit});
 }
 return {improvements,fixes};
}
function windowsGroups(commits) {
 const improvements=[],fixes=[];
 // Include NEL, which .NET treats as whitespace but JavaScript's \s does not.
 const trim=text=>text.replace(/^[\s\u0085]+|(?<![\s\u0085])[\s\u0085]+$/g,'');
 for(const {sha:commit,subject} of commits) {
   const line=trim(subject.replace(/[\r\n]+/g,' ')).replace(/^\[verified\][\s\u0085]*/i,'');
   const prefix=/^([a-z]+)(?:\([^)]*\))?!?:/i.exec(line);
   const text=trim(line.slice(prefix?.[0].length??0));
   if(text)(/^(?:fix|bugfix)$/i.test(prefix?.[1]??'')?fixes:improvements).push({text,commit});
 }
 return {improvements,fixes};
}
export function changelog({root='.',platform,from=null,to}) {
 if(!['android','windows','container'].includes(platform)||!sha.test(to)||(from!==null&&!sha.test(from)))throw new Error('Invalid changelog source.');
 const git=gitAt(root);
 if(git('rev-parse','--is-shallow-repository')!=='false')throw new Error('Full history required.');
 if(from) {try{git('merge-base','--is-ancestor',from,to);}catch{throw new Error('Installed source is not an ancestor of offered source.');}}
 const ids=git('rev-list','--reverse',...(from?[`${from}..${to}`]:[to])).split('\n').filter(Boolean);
 const commits=ids.map(commit=>{
   const subject=git('show','-s','--format=%s',commit),description=git('show','-s','--format=%b',commit);
   const paths=pathsFor(git,commit);
   return {commit,subject,description,paths,relevant:isRelevant(platform,paths)};
 });
 return {schemaVersion:1,platform,from,to,truncated:false,...groups(commits.filter(c=>c.relevant)),commits};
}

export function windowsPublicationHistory({root='.',to,count,maxBytes}) {
 const git=gitAt(root), unavailable={schemaVersion:1,truncated:true};
 if(!sha.test(to)||!Number.isSafeInteger(count)||count<1)throw new Error('Invalid Windows changelog source.');
 if(git('rev-parse','--is-shallow-repository')!=='false')throw new Error('Full history required.');
 if(Number(git('rev-list','--count',to))!==count)throw new Error('Windows changelog count does not match publication.');
 if(count>2048)return unavailable;
 let raw;
 try {
   // %s folds the whole subject paragraph; %B preserves the first line verbatim.
   // Bound the command independently: message bodies don't count toward the manifest budget.
   raw=execFileSync('git',['-C',resolve(root),'log','--topo-order','--reverse','--encoding=UTF-8','-z','--format=%H%x00%P%x00%B',to],{encoding:'utf8',maxBuffer:64*1024*1024});
 } catch(error) {
   if(error.code==='ENOBUFS')return unavailable;
   throw error;
 }
 const fields=raw.split('\0');
 if(fields.pop()!==''||fields.length!==count*3)throw new Error('Incomplete Windows changelog history.');
 const seen=new Set(), commits=[];
 let metadataBytes=0;
 for(let i=0;i<fields.length;i+=3) {
   const [commit,parentList,message]=[fields[i],fields[i+1],fields[i+2]];
   const subject=message.split('\n',1)[0];
   // JSON necessarily costs more than the equivalent NUL-delimited metadata.
   metadataBytes+=commit.length+parentList.length+Buffer.byteLength(subject)+3;
   if(metadataBytes>maxBytes)return unavailable;
   const parents=parentList?parentList.split(' '):[];
   if(!sha.test(commit)||seen.has(commit)||!parents.every(parent=>seen.has(parent)))throw new Error('Invalid Windows changelog topology.');
   seen.add(commit);commits.push({sha:commit,parents,subject});
 }
 if(commits.at(-1).sha!==to)throw new Error('Windows changelog head does not match publication.');
 const relevant=commits.filter(commit=>isWindowsRelevant(git,commit));
 return {schemaVersion:1,truncated:false,commits,...windowsGroups(relevant)};
}
export function main(argv=process.argv.slice(2)) {const o=options(argv,['platform','from','to','output']);const result=changelog({platform:o.platform,from:o.from??null,to:o.to});writeFileSync(o.output,JSON.stringify(result,null,2)+'\n');return result;}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){try{main();}catch(e){console.error(e.message);process.exitCode=1;}}
