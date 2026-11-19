import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {test} from 'node:test';

test('installed Android launcher uses the real shared Agent artwork',()=>{
 const manifest=readFileSync(new URL('app/src/main/AndroidManifest.xml',import.meta.url),'utf8');
 assert.match(manifest, /android:icon="@drawable\/agent_icon"/);
 assert.match(manifest, /android:roundIcon="@drawable\/agent_icon"/);
 assert.deepEqual(readFileSync(new URL('app/src/main/res/drawable-nodpi/agent_icon.png',import.meta.url)),readFileSync(new URL('../../pwa/icons/icon-512.png',import.meta.url)));
});
