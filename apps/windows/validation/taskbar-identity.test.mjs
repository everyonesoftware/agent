import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const read = name => fs.readFileSync(new URL(name, import.meta.url), 'utf8');

test('generator emits an immutable full SHA256 taskbar ICO with exact original bytes', () => {
    const out = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-taskbar-'));
    try {
        const generator = fileURLToPath(new URL('../generate-icons.mjs', import.meta.url));
        execFileSync(process.execPath, [generator, out]);
        const original = fs.readFileSync(path.join(out, 'Agent.ico'));
        const filename = `Agent-${createHash('sha256').update(original).digest('hex')}.ico`;
        assert.ok(fs.existsSync(path.join(out, filename)), 'missing content-addressed taskbar ICO');
        assert.deepEqual(fs.readFileSync(path.join(out, filename)), original);
        assert.deepEqual(fs.readFileSync(new URL(`../Agent.App/Assets/${filename}`, import.meta.url)), original);
        execFileSync(process.execPath, [generator, out]);
        assert.deepEqual(fs.readdirSync(out).filter(n => /^Agent-[0-9a-f]{64}\.ico$/.test(n)), [filename]);
        assert.deepEqual(fs.readFileSync(path.join(out, filename)), original);
    } finally { fs.rmSync(out, {recursive: true, force: true}); }
});

test('running HWND receives installed taskbar identity before activation and clears before close', () => {
    const program = read('../Agent.App/Program.cs');
    assert.match(program, /RunningTaskbarIdentity\.Attach\(window\);\s*window\.Activate\(\)/, 'running taskbar hookup is missing before Activate');
    const helper = read('../Agent.App/RunningTaskbarIdentity.cs');
    assert.match(helper, /VelopackLocator\.Current/);
    assert.match(helper, /string\.IsNullOrWhiteSpace\(locator\.AppUserModelId\)/);
    assert.match(helper, /locator\.CurrentlyInstalledVersion is null/);
    assert.match(helper, /Path\.Combine\(locator\.RootAppDir,"Agent\.exe"\)/);
    assert.match(helper, /WindowNative\.GetWindowHandle\(window\)/);
    assert.match(helper, /SHA256\.HashData/);
    assert.match(helper, /Convert\.ToHexStringLower/);
    assert.match(helper, /File\.Exists\(iconPath\)/);
    assert.match(helper, /SetString\(RelaunchCommand,.*launcher/);
    assert.match(helper, /SetString\(RelaunchIconResource,iconPath\+",0"\)/);
    assert.match(helper, /SetString\(RelaunchDisplayNameResource,"Agent"\)/);
    assert.match(helper, /SetString\(AppUserModelId,locator\.AppUserModelId\)/);
    assert.ok(helper.indexOf('SetString(AppUserModelId,') > helper.indexOf('SetString(RelaunchDisplayNameResource,'), 'relaunch properties must precede ID');
    assert.match(helper, /AppWindow\.Closing\+=.*Dispose\(\)/);
    assert.match(helper, /catch\(Exception exception\)/);
    assert.match(helper, /RecordFailure\(exception\)/);
    assert.match(helper, /Trace\.TraceError/);
    assert.match(helper, /taskbar-identity-error\.txt/);
    assert.doesNotMatch(helper, /SetCurrentProcessExplicitAppUserModelID|SetAppUserModelId|Registry|ShellLink|Process\.Start/);
    assert.match(helper, /SHGetPropertyStoreForWindow/);
    assert.match(helper, /886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99/i);
    assert.match(helper, /9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3/i);
    for (const [key, pid] of [['RelaunchCommand',2],['RelaunchIconResource',3],['RelaunchDisplayNameResource',4],['AppUserModelId',5]]) {
        assert.match(helper, new RegExp(`${key}=new\\(${pid}\\)`));
    }
    assert.match(helper, /StructLayout\(LayoutKind\.Explicit,Size=24\)/);
    assert.match(helper, /FieldOffset\(0\)\] public ushort/);
    assert.match(helper, /FieldOffset\(8\)\] public IntPtr/);
    assert.match(helper, /VarType=31/);
    assert.match(helper, /Marshal\.StringToCoTaskMemUni/);
    assert.match(helper, /finally\s*\{Marshal\.FreeCoTaskMem/);
    const slots = [...helper.matchAll(/\[PreserveSig\] int (\w+)\(/g)].map(m => m[1]);
    assert.deepEqual(slots, ['GetCount','GetAt','GetValue','SetValue','Commit'], 'IPropertyStore vtable ABI');
    assert.match(helper, /SetValue\(ref PropertyKey key,ref PropVariant value\)/);
    assert.match(helper, /PropVariant empty=default/);
    assert.match(helper, /foreach\(var property in new\[\]\{AppUserModelId,RelaunchCommand,RelaunchIconResource,RelaunchDisplayNameResource\}\)/);
    assert.match(helper, /SetValue\(ref key,ref empty\)/);
    assert.match(helper, /finally\s*\{\s*try\{Marshal\.ReleaseComObject\(releasing\);\}\s*catch\(Exception exception\)\{RecordFailure\(exception\);\}/, 'COM release failure must remain cosmetic');
    assert.doesNotMatch(helper, /\.Commit\(/, 'HWND SetValue applies immediately');
});

test('content-addressed ICOs are removed from implicit items and copied exactly once', () => {
    const project = read('../Agent.App/Agent.App.csproj');
    assert.match(project, /<Content Remove="[^"]*Assets\/Agent-\*\.ico/);
    assert.match(project, /<None Remove="[^"]*Assets\/Agent-\*\.ico/);
    assert.match(project, /<Content Include="Assets\/Agent-\*\.ico" CopyToOutputDirectory="PreserveNewest" CopyToPublishDirectory="PreserveNewest"\/>/);
});
