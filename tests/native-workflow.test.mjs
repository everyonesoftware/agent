import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';
const workflow=readFileSync(new URL('../.github/workflows/publish-image.yml',import.meta.url),'utf8');
test('single push workflow uses mandatory shared verify, one job/platform, skipped-compatible release and unconditional table repair',()=>{
 const jobs=[...workflow.matchAll(/^  ([a-z_]+):\n(?=    (?:name|outputs|needs|runs-on|if):)/gm)].map(m=>m[1]);
 for(const name of ['verify','publish','android','windows','release','services_notify'])assert.equal(jobs.filter(j=>j===name).length,1,name);
 assert.match(workflow,/android: \$\{\{ steps\.selection\.outputs\.android \}\}/);
 assert.match(workflow,/if: needs\.verify\.outputs\.container == 'true'/);
 const final=workflow.slice(workflow.indexOf('  release:'),workflow.indexOf('  services_notify:'));
 assert.match(final,/needs: \[verify, android, windows, publish\]/);
 for(const p of ['android','windows','publish'])assert.ok(final.includes(`needs.${p}.result == 'skipped'`));
 assert.ok(final.indexOf('native-release.mjs publish')<final.indexOf('current-downloads.mjs'));
 assert.match(final,/actions: read/);
 assert.match(final,/--output-directory dist\/current-downloads --publish/);
 assert.match(workflow,/test-windows-installer\.ps1/);
 assert.match(workflow,/AGENT_KEYSTORE_BASE64/);
 assert.match(workflow,/native-release\.mjs package-android/);
});
test('notification remains a separate container-only retained-payload security boundary',()=>{
 const notify=workflow.slice(workflow.indexOf('  services_notify:'));
 assert.match(notify,/needs: publish/);assert.match(notify,/needs\.publish\.result == 'success'/);assert.match(notify,/id-token: write/);assert.match(notify,/digest-mismatch: error/);assert.match(notify,/publish-notification\.mjs send/);assert.doesNotMatch(notify,/contents: write|packages: write/);
});
