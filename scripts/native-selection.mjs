#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
export const platforms = ['android','windows','container'];
const controls = new Set(['scripts/native-selection.mjs','scripts/native-release.mjs','scripts/current-downloads.mjs','scripts/native-changelog.mjs']);
export function affected(paths) {
  const selected = new Set();
  for(const path of paths) {
    if(path.startsWith('docs/') || /(?:^|\/)(?:README|AGENTS|RELEASING|VALIDATION)\.md$/.test(path) || controls.has(path) || /^tests\/native-.*\.test\.mjs$/.test(path) || path === 'tests/current-downloads.test.mjs' || path === '.gitignore') continue;
    if(path.startsWith('apps/android/')) selected.add('android');
    else if(path.startsWith('apps/windows/') || /^scripts\/(?:windows-build|test-windows-installer)\.ps1$/.test(path)) selected.add('windows');
    else if(path.endsWith('.go') || ['go.mod','go.sum','Dockerfile','.dockerignore','docker-compose.yml','compose.yml','compose.yaml','scripts/install-codex.sh'].includes(path) || path.startsWith('assets/') || path.startsWith('tests/') || path.startsWith('scripts/publish-notification')) selected.add('container');
    else platforms.forEach(p=>selected.add(p));
  }
  return platforms.filter(p=>selected.has(p));
}
// Only the supported, unambiguous workflow shape can establish an unchanged input.
// New shared YAML keys fail closed until deliberately incorporated here.
export function workflowInputs(text) {
  const sections = {}; let key;
  for(const line of text.split('\n')) {
    if(!line.trim() || /^\s*#/.test(line)) continue;
    if(!/^\s/.test(line)) {
      const m = /^(name|on|permissions|concurrency|jobs):(?:\s.*)?$/.exec(line);
      if(!m || Object.hasOwn(sections,m[1])) throw new Error('Unsupported workflow layout; select all platforms.');
      key = m[1]; sections[key]=line+'\n';
    } else { if(!key) throw new Error('Invalid workflow layout.'); sections[key]+=line+'\n'; }
  }
  if(!sections.jobs) throw new Error('Missing jobs.');
  const jobs = {}; let job;
  for(const line of sections.jobs.split('\n').slice(1)) {
    if(!line.trim()) continue;
    if(/^  \S/.test(line)) {
      const m = /^  (verify|publish|android|windows|release|services_notify):$/.exec(line);
      if(!m || Object.hasOwn(jobs,m[1])) throw new Error('Unsupported workflow job.');
      job=m[1]; jobs[job]='';
    } else { if(!job || !/^    /.test(line)) throw new Error('Unsupported jobs layout.'); jobs[job]+=line+'\n'; }
  }
  // Only this explicit output/producer contract permits ignoring later browser
  // gates. Include checkout, job-level settings and everything before the version
  // is calculated; unfamiliar wiring fails closed rather than guessing YAML.
  const verify=jobs.verify??'';
  const producer=/^      - name: Calculate commit-count version\n(?:(?!      - ).*\n)*/m.exec(verify);
  if(!/^      version: \$\{\{ steps\.version\.outputs\.value \}\}\n/m.test(verify) || !producer || !/^        id: version$/m.test(producer[0]) || (verify.match(/^        id: version$/gm)??[]).length!==1) throw new Error('Unsupported shared version contract.');
  const versionInputs=verify.slice(0,producer.index+producer[0].length);
  if(/^    \S/m.test(verify.slice(versionInputs.length))) throw new Error('Unsupported late verify job settings.');
  const shared = ['permissions','concurrency'].map(k=>sections[k]??'').join('')+versionInputs;
  return Object.fromEntries(platforms.map(p=>[p,shared+(jobs[p==='container'?'publish':p]??'')]));
}
export function decide({root='.',platform,baseline,force=false}) {
  if(!platforms.includes(platform)) throw new Error('Invalid platform.');
  const git = (...args)=>execFileSync('git',['-C',resolve(root),...args],{encoding:'utf8'}).trim();
  if(git('rev-parse','--is-shallow-repository')!=='false') throw new Error('Full Git history required.');
  const head=git('rev-parse','HEAD');
  if(!/^[a-f0-9]{40}$/.test(head) || (baseline!==null && !/^[a-f0-9]{40}$/.test(baseline))) throw new Error('Invalid baseline/source SHA.');
  if(force || baseline===null) return {build:true,baseline,head,reason:force?'forced':'no verified publication'};
  try { git('merge-base','--is-ancestor',baseline,head); } catch { throw new Error('Publication baseline is not an ancestor of HEAD.'); }
  const paths=git('diff','--name-only','-z','--no-renames',baseline,head,'--').split('\0').filter(Boolean);
  let build=affected(paths.filter(p=>p!=='.github/workflows/publish-image.yml')).includes(platform);
  if(paths.includes('.github/workflows/publish-image.yml')) {
    try { build ||= workflowInputs(git('show',baseline+':.github/workflows/publish-image.yml'))[platform]!==workflowInputs(git('show',head+':.github/workflows/publish-image.yml'))[platform]; }
    catch { build=true; }
  }
  return {build,baseline,head,paths,reason:build?'inputs changed since verified publication':'unchanged'};
}
export async function main(argv=process.argv.slice(2)) {
  const { options, client, discover }=await import('./native-release.mjs');
  const o=options(argv,['repository','github-output','force']);
  if(!['none','all',...platforms].includes(o.force??'none')) throw new Error('Invalid force platform.');
  const inventory=discover(client(o.repository));
  const result=Object.fromEntries(platforms.map(platform=>[platform,decide({platform,baseline:inventory[platform]?.manifest.commit??null,force:o.force==='all'||o.force===platform})]));
  if(o['github-output']) appendFileSync(o['github-output'],platforms.map(p=>`${p}=${result[p].build}\n`).join(''));
  console.log(JSON.stringify(result,null,2)); return result;
}
if(process.argv[1] && import.meta.url===pathToFileURL(resolve(process.argv[1])).href) main().catch(e=>{console.error(e.message);process.exitCode=1;});
