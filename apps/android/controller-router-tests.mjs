// Exercise the Android controller against the actual Go router without adding
// a production route, editing the server, or using Gradle/shared build outputs.
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
const android=path.dirname(fileURLToPath(import.meta.url));
const root=path.resolve(android,'../..');
const scratch=fs.mkdtempSync(path.join(process.env.TMPDIR || os.tmpdir(),'android-router-'));
try {
 const overlay=path.join(scratch,'overlay.json');
 fs.writeFileSync(overlay,JSON.stringify({Replace:{[path.join(root,'android_controller_fixture_test.go')]:path.join(android,'fixtures/controller_router_test.go')}}));
 const result=spawnSync('go',['test','-overlay='+overlay,'-tags=android_controller_fixture','-run','^TestAndroidControllerRealRouter$','-count=1','-timeout=2m','-v','.'],{cwd:root,stdio:'inherit',timeout:150000});
 if(result.error) throw result.error;
 process.exitCode=result.status || (result.signal ? 1 : 0);
} finally { fs.rmSync(scratch,{recursive:true,force:true}); }
