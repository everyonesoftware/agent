import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';
import { affected } from '../scripts/native-selection.mjs';

test('all generated Classic ring assets match the approved shared geometry', () => {
 const result = execFileSync(process.execPath, [fileURLToPath(new URL('../scripts/generate-branding.mjs', import.meta.url)), '--check'], { encoding: 'utf8' });
 assert.match(result, /Verified Classic ring SVG, web\/PWA, Android and Windows artwork/);
});

test('shared artwork and its generator select every consuming platform', () => {
 for (const path of ['pwa/icons/agent.svg', 'pwa/icons/icon-512.png', 'scripts/generate-branding.mjs']) {
  assert.deepEqual(affected([path]), ['android', 'windows', 'container'], path);
 }
});
