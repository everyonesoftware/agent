// Standalone controller/JVM checks: no Gradle daemon or shared build outputs.
// Uses the project's already resolved, pinned dependencies; fails if missing.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const root=path.dirname(fileURLToPath(import.meta.url));
const cache=path.join(process.env.GRADLE_USER_HOME || path.join(os.homedir(),'.gradle'),'caches/modules-2/files-2.1');
function jar(group,name,version) {
 const dir=path.join(cache,group,name,version);
 for(const hash of fs.readdirSync(dir)) { const p=path.join(dir,hash,`${name}-${version}.jar`); if(fs.existsSync(p)) return p; }
 throw new Error(`Resolve ${group}:${name}:${version} with the Android build first`);
}
const specs=[['org.jetbrains.kotlin','kotlin-stdlib','2.2.21'],['org.jetbrains','annotations','23.0.0'],['org.jetbrains.kotlinx','kotlinx-coroutines-core-jvm','1.10.2'],['org.jetbrains.kotlinx','kotlinx-coroutines-test-jvm','1.10.2'],['org.jetbrains.kotlinx','kotlinx-serialization-core-jvm','1.9.0'],['org.jetbrains.kotlinx','kotlinx-serialization-json-jvm','1.9.0'],['com.squareup.okhttp3','okhttp','4.12.0'],['com.squareup.okhttp3','mockwebserver','4.12.0'],['com.squareup.okio','okio-jvm','3.6.0'],['junit','junit','4.13.2'],['org.hamcrest','hamcrest-core','1.3']];
const cp=specs.map(s=>jar(...s)).join(path.delimiter);
const compiler=[jar('org.jetbrains.kotlin','kotlin-reflect','2.2.0'),jar('org.jetbrains.kotlin','kotlin-compiler-embeddable','2.2.21'),jar('org.jetbrains.kotlin','kotlin-script-runtime','2.2.21'),...cp.split(path.delimiter)].join(path.delimiter);
const out=fs.mkdtempSync(path.join(process.env.TMPDIR || os.tmpdir(),'android-controller-'));
const java=process.env.JAVA_HOME ? path.join(process.env.JAVA_HOME,'bin/java') : 'java';
const src='app/src/main/java/com/everyonesoftware/agent/';
const tests='app/src/test/java/com/everyonesoftware/agent/';
const classes=process.argv.slice(2).length ? process.argv.slice(2) : ['AgentControllerTest','AgentApiTest','ChatProtocolTest'];
function run(args) { const r=spawnSync(java,args,{cwd:root,stdio:'inherit',timeout:120000}); if(r.error) throw r.error; if(r.status!==0) throw Object.assign(new Error('Controller test process failed'),{status:r.status || 1}); }
try {
 run(['-cp',compiler,'org.jetbrains.kotlin.cli.jvm.K2JVMCompiler','-no-stdlib','-no-reflect','-jvm-target','17','-classpath',cp,'-d',out,...['AgentApi.kt','ChatProtocol.kt','ResetCredits.kt'].map(f=>src+f),process.env.AGENT_CONTROLLER_SOURCE || src+'AgentController.kt',...classes.map(c=>(c==='AgentControllerRouterTest' ? 'fixtures/' : tests)+c+'.kt')]);
 run(['-cp',out+path.delimiter+cp,'org.junit.runner.JUnitCore',...classes.map(c=>'com.everyonesoftware.agent.'+c)]);
} catch(error) { process.exitCode=error.status || 1; if(!error.status) console.error(error); }
finally { fs.rmSync(out,{recursive:true,force:true}); }
