#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { options } from './native-release.mjs';
export function changelog({root='.',platform,from=null,to}) {
 if(!['android','windows','container'].includes(platform)||!/^[a-f0-9]{40}$/.test(to)||(from!==null&&!/^[a-f0-9]{40}$/.test(from)))throw new Error('Invalid changelog source.');
 const git=(...args)=>execFileSync('git',['-C',resolve(root),...args],{encoding:'utf8',maxBuffer:64*1024*1024}).trim();
 if(git('rev-parse','--is-shallow-repository')!=='false')throw new Error('Full history required.');
 if(from) {try{git('merge-base','--is-ancestor',from,to);}catch{throw new Error('Installed source is not an ancestor of offered source.');}}
 const ids=git('rev-list','--reverse',...(from?[`${from}..${to}`]:[to])).split('\n').filter(Boolean);
 const commits=ids.map(commit=>{
   const subject=git('show','-s','--format=%s',commit),description=git('show','-s','--format=%b',commit);
   const paths=git('diff-tree','--root','--no-commit-id','--name-only','-r','-m',commit).split('\n').filter(Boolean);
   const unrelated=path=>path.startsWith('docs/')||/(?:^|\/)README\.md$/.test(path)||path.startsWith('.github/')||/^scripts\/(?:native-selection|native-release|native-changelog|current-downloads)\.mjs$/.test(path)||(platform!=='windows'&&path.startsWith('apps/windows/'))||(platform!=='android'&&path.startsWith('apps/android/'));
   return {commit,subject,description,paths,relevant:!(paths.length&&paths.every(unrelated))};
 });
 const improvements=[],fixes=[];
 for(const c of commits.filter(c=>c.relevant)) {
   const text=c.subject.replace(/^\[verified\]\s*/i,'').replace(/^[a-z]+(?:\([^)]*\))?!?:\s*/i,'');
   (/^(?:\[verified\]\s*)?(?:fix|bugfix)(?:\([^)]*\))?:/i.test(c.subject)?fixes:improvements).push({text,commit:c.commit});
 }
 return {schemaVersion:1,platform,from,to,truncated:false,improvements,fixes,commits};
}
export function main(argv=process.argv.slice(2)) {const o=options(argv,['platform','from','to','output']);const result=changelog({platform:o.platform,from:o.from??null,to:o.to});writeFileSync(o.output,JSON.stringify(result,null,2)+'\n');return result;}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){try{main();}catch(e){console.error(e.message);process.exitCode=1;}}
