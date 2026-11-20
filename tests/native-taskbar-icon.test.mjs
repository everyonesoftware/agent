import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { affected } from '../scripts/native-selection.mjs';
const smoke = readFileSync(new URL('../scripts/test-windows-installer.ps1', import.meta.url), 'utf8');

test('Windows acceptance inspects actual taskbar identity and icon separately from the title-bar screenshot', () => {
 for (const marker of ['Save-TaskbarEvidence', 'SHGetPropertyStoreForWindow', 'ReadTaskbarProperty', 'WM_GETICON', 'taskbar-properties.json', 'taskbar-native-icon.png', 'taskbar.png', 'Shell_TrayWnd', 'taskbarVerified']) assert.ok(smoke.includes(marker), marker);
 assert.ok(smoke.indexOf('    Save-TaskbarEvidence $window') > smoke.indexOf('    Save-WindowScreenshot $window'));
 assert.ok(smoke.indexOf('    Save-TaskbarEvidence $window') < smoke.indexOf('    $summary.windowVerified = $true'));
});

test('taskbar-only changes keep Android and container inputs unchanged', () => {
 for (const file of ['apps/windows/Agent.App/TaskbarIcon.cs', 'apps/windows/generate-icons.mjs', 'scripts/windows-build.ps1', 'scripts/test-windows-installer.ps1']) assert.deepEqual(affected([file]), ['windows']);
});
