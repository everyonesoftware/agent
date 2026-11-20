import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {test} from 'node:test';

const source=readFileSync(new URL('app/src/main/java/com/everyonesoftware/agent/MainActivity.kt',import.meta.url),'utf8');

test('installed Android launcher uses the real shared Agent artwork',()=>{
 const manifest=readFileSync(new URL('app/src/main/AndroidManifest.xml',import.meta.url),'utf8');
 assert.match(manifest, /android:icon="@drawable\/agent_icon"/);
 assert.match(manifest, /android:roundIcon="@drawable\/agent_icon"/);
 assert.deepEqual(readFileSync(new URL('app/src/main/res/drawable-nodpi/agent_icon.png',import.meta.url)),readFileSync(new URL('../../pwa/icons/icon-512.png',import.meta.url)));
});

test('native in-app brand renders the shared Classic ring at 26dp without tint or duplicate accessibility text',()=>{
 const brand=source.match(/@Composable private fun AgentBrand\([\s\S]*?\n\}/)?.[0] ?? '';
 assert.match(brand,/Image\(painterResource\(R\.drawable\.agent_icon\),contentDescription=null,modifier=Modifier\.size\(26\.dp\)\)/);
 assert.match(brand,/Text\("Agent",style=style\)/);
 assert.doesNotMatch(brand,/\bIcon\(|\btint\s*=/);
});

test('restoring, login and conversation drawer share the native in-app brand',()=>{
 assert.equal((source.match(/AgentBrand\(style=MaterialTheme\.typography\.headlineMedium\)/g) ?? []).length,2);
 assert.equal((source.match(/AgentBrand\(style=MaterialTheme\.typography\.headlineLarge\)/g) ?? []).length,1);
 assert.doesNotMatch(source,/Text\("Agent",(?:Modifier\.padding\(24\.dp\),)?style=MaterialTheme\.typography\.headline/);
});
