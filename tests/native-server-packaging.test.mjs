import {readFile} from 'node:fs/promises';
import assert from 'node:assert/strict';
import {test} from 'node:test';

test('native clients and their build output never enter the server image context',async()=>{
 const patterns=(await readFile(new URL('../.dockerignore',import.meta.url),'utf8')).split(/\r?\n/).map(v=>v.trim());
 for(const excluded of ['apps','dist','**/.gradle','**/bin','**/obj','*.jks','*.keystore'])assert.ok(patterns.includes(excluded),'Docker context must exclude '+excluded);
 for(const required of ['*.go','go.mod','go.sum','pwa','scripts'])assert.ok(!patterns.includes(required),'server build input remains available: '+required);
});
