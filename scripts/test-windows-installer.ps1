#Requires -Version 7.2
<#
.SYNOPSIS
Fail-closed native Windows install / visible-window / uninstall acceptance gate.
.DESCRIPTION
Run only on a disposable, unlocked interactive Windows account with no existing Agent
installation/profile. Requires PowerShell 7.2+. EvidenceDirectory must be empty/new.
No package extraction, process-only success, or headless fallback is allowed.
Pinned primary references for velopack/1.2.161 (not latest-version CLI assumptions):
https://github.com/velopack/velopack/blob/1.2.161/src/bins/src/setup.rs
https://github.com/velopack/velopack/blob/1.2.161/src/bins/src/update.rs
https://github.com/velopack/velopack/blob/1.2.161/src/bins/src/commands/install.rs
https://github.com/velopack/velopack/blob/1.2.161/src/bins/src/commands/uninstall.rs
https://github.com/velopack/velopack/blob/1.2.161/src/bins/src/windows/registry.rs
Setup --silent suppresses first launch; launch the installed current\Agent.exe ourselves.
Uninstall returns success even when some removals fail; independently verify removal.
A scheduled self-delete can leave Update.exe briefly; poll, never manually delete to pass.
.EXAMPLE
pwsh -File scripts/test-windows-installer.ps1 -InstallerPath dist/windows-build/releases/AgentWindows-win-x64-Setup.exe -EvidenceDirectory dist/windows-installer-smoke
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$InstallerPath,
    [Parameter(Mandatory = $true)][string]$EvidenceDirectory,
    [ValidateRange(10, 300)][int]$WindowTimeoutSeconds = 60,
    [ValidateRange(30, 600)][int]$OperationTimeoutSeconds = 180,
    [ValidateRange(10, 180)][int]$RemovalTimeoutSeconds = 60
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$started = [DateTime]::UtcNow
$evidence = [IO.Path]::GetFullPath($EvidenceDirectory)
if ((Test-Path -LiteralPath $evidence) -and (@(Get-ChildItem -LiteralPath $evidence -Force).Count -gt 0)) {
    throw 'EvidenceDirectory must be empty: refusing to overwrite retained diagnostics.'
}
$null = New-Item -ItemType Directory -Path $evidence -Force
$summary = [ordered]@{
    schemaVersion = 1; status = 'failed'; startedUtc = $started.ToString('o')
    installer = $InstallerPath; installerSha256 = $null; installRoot = $null
    evidenceDirectory = $evidence; installVerified = $false; windowVerified = $false
    screenshotSha256 = $null; taskbarVerified = $false; uninstallVerified = $false; cleanupRemovedInstallRoot = $false
    cleanupErrors = @(); failure = $null
}
$installRoot = $null
$appProfile = $null
$mutationStarted = $false
$transcriptStarted = $false
$nativeReady = $false
$window = $null
$registryPath = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\AgentWindows'

function Write-Json($Value, [string]$Name) {
    ConvertTo-Json -InputObject $Value -Depth 12 | Set-Content -LiteralPath (Join-Path $evidence $Name) -Encoding utf8
}
function Get-OwnedProcesses {
    if (-not $installRoot) { return }
    $prefix = $installRoot.TrimEnd('\') + '\'
    Get-CimInstance Win32_Process | Where-Object {
        $_.ExecutablePath -and $_.ExecutablePath.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)
    }
}
function Stop-OwnedProcesses {
    foreach ($owned in @(Get-OwnedProcesses)) {
        # Verify the current PID still names the same executable before terminating it.
        $current = Get-Process -Id $owned.ProcessId -ErrorAction SilentlyContinue
        if ($current -and $current.Path -eq $owned.ExecutablePath) {
            $current.Kill($true)
            if (-not $current.WaitForExit(10000)) { throw "Could not stop owned PID $($owned.ProcessId)" }
        }
    }
}
function Invoke-BoundedProcess([string]$Executable, [string[]]$Arguments, [string]$Label) {
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $Executable
    $info.WorkingDirectory = $evidence # never hold the install directory open during deletion
    $info.UseShellExecute = $false
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    foreach ($argument in $Arguments) { $info.ArgumentList.Add($argument) }
    Write-Json @{ executable = $Executable; arguments = $Arguments } "$Label-command.json"
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $info
    try {
        if (-not $process.Start()) { throw "Could not start $Label" }
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit($OperationTimeoutSeconds * 1000)) {
            $process.Kill($true)
            $null = $process.WaitForExit(10000)
            throw "$Label exceeded ${OperationTimeoutSeconds}s; process tree terminated"
        }
        if (-not $stdout.Wait(5000) -or -not $stderr.Wait(5000)) { throw "$Label log streams did not close" }
        $stdout.Result | Set-Content -LiteralPath (Join-Path $evidence "$Label-stdout.txt")
        $stderr.Result | Set-Content -LiteralPath (Join-Path $evidence "$Label-stderr.txt")
        Write-Json @{ exitCode = $process.ExitCode; exitedUtc = [DateTime]::UtcNow.ToString('o') } "$Label-result.json"
        if ($process.ExitCode -ne 0) { throw "$Label exited $($process.ExitCode)" }
    } finally {
        if ($process.Id -and -not $process.HasExited) {
            $process.Kill($true)
            $null = $process.WaitForExit(10000)
        }
        $process.Dispose()
    }
}
function Get-StartMenuLinks {
    $roots = @([Environment]::GetFolderPath('StartMenu'), [Environment]::GetFolderPath('CommonStartMenu'),
        [Environment]::GetFolderPath('DesktopDirectory'), [Environment]::GetFolderPath('CommonDesktopDirectory'))
    $shell = New-Object -ComObject WScript.Shell
    try {
        foreach ($root in $roots | Select-Object -Unique) {
            if (-not $root -or -not (Test-Path -LiteralPath $root)) { continue }
            foreach ($file in Get-ChildItem -LiteralPath $root -Filter '*.lnk' -Recurse -File) {
                $link = $shell.CreateShortcut($file.FullName)
                try {
                    if ($file.BaseName -in @('Agent', 'AgentWindows') -or
                        ($installRoot -and $link.TargetPath.StartsWith($installRoot + '\', [StringComparison]::OrdinalIgnoreCase))) {
                        [pscustomobject]@{ path = $file.FullName; target = $link.TargetPath; arguments = $link.Arguments }
                    }
                } finally { $null = [Runtime.InteropServices.Marshal]::FinalReleaseComObject($link) }
            }
        }
    } finally { $null = [Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell) }
}
function Save-TaskbarEvidence($Candidate) {
    # A custom title-bar Image does not establish the icon Explorer uses.
    $assets = Join-Path $installRoot 'current\Assets'
    $icon = Join-Path $assets 'Agent.ico'
    $hash = (Get-FileHash -LiteralPath $icon -Algorithm SHA256).Hash.ToLowerInvariant()
    $taskbarIcon = Join-Path $assets "Agent-$hash.ico"
    if ((Get-FileHash -LiteralPath $taskbarIcon -Algorithm SHA256).Hash.ToLowerInvariant() -cne $hash) {
        throw 'Installed content-addressed taskbar icon differs from Agent artwork'
    }
    $properties = [ordered]@{
        appId = [AgentSmoke.Native]::ReadTaskbarProperty($Candidate.Handle, 5)
        relaunchCommand = [AgentSmoke.Native]::ReadTaskbarProperty($Candidate.Handle, 2)
        iconResource = [AgentSmoke.Native]::ReadTaskbarProperty($Candidate.Handle, 3)
        displayName = [AgentSmoke.Native]::ReadTaskbarProperty($Candidate.Handle, 4)
    }
    Write-Json $properties 'taskbar-properties.json'
    if ($properties.appId -cne 'velopack.AgentWindows' -or $properties.displayName -cne 'Agent' -or
        $properties.iconResource -cne "$taskbarIcon,0" -or
        $properties.relaunchCommand -cne ('"' + (Join-Path $installRoot 'Agent.exe') + '"')) {
        throw 'Running window taskbar identity/icon does not match the installed Agent application'
    }
    [UIntPtr]$result = [UIntPtr]::Zero
    # WM_GETICON ICON_BIG is used for the running application, with documented class fallback.
    $sent = [AgentSmoke.Native]::SendMessageTimeout($Candidate.Handle, 0x007F, [UIntPtr]::new(1), [IntPtr]::Zero,
        [AgentSmoke.Native]::SMTO_ABORTIFHUNG, 1000, [ref]$result)
    if ($sent -eq [IntPtr]::Zero) { throw 'Running window did not answer WM_GETICON' }
    $handle = [IntPtr]::new([long]$result.ToUInt64())
    if ($handle -eq [IntPtr]::Zero) { $handle = [AgentSmoke.Native]::GetClassLongPtr($Candidate.Handle, -14) }
    if ($handle -eq [IntPtr]::Zero) { throw 'Running window has no native application icon' }
    $nativeIcon = [Drawing.Icon]::FromHandle($handle) # borrowed HICON; do not call DestroyIcon
    $actual = $nativeIcon.ToBitmap()
    $expectedIcon = [Drawing.Icon]::new($taskbarIcon, $actual.Size)
    $expected = $expectedIcon.ToBitmap()
    try {
        $actual.Save((Join-Path $evidence 'taskbar-native-icon.png'), [Drawing.Imaging.ImageFormat]::Png)
        if ($actual.Size -ne $expected.Size) { throw 'Running native icon dimensions differ from Agent artwork' }
        for ($y = 0; $y -lt $actual.Height; $y++) {
            for ($x = 0; $x -lt $actual.Width; $x++) {
                if ($actual.GetPixel($x, $y).ToArgb() -ne $expected.GetPixel($x, $y).ToArgb()) {
                    throw "Running native icon pixels differ from Agent artwork at $x,$y"
                }
            }
        }
    } finally { $expected.Dispose(); $expectedIcon.Dispose(); $actual.Dispose(); $nativeIcon.Dispose() }
    $taskbar = [AgentSmoke.Native]::FindWindow('Shell_TrayWnd', $null)
    $rect = [AgentSmoke.Native+RECT]::new()
    if ($taskbar -eq [IntPtr]::Zero -or -not [AgentSmoke.Native]::IsWindowVisible($taskbar) -or
        -not [AgentSmoke.Native]::GetWindowRect($taskbar, [ref]$rect)) { throw 'No visible Explorer taskbar for actual taskbar acceptance' }
    $width = $rect.Right - $rect.Left; $height = $rect.Bottom - $rect.Top
    $bounds = [Windows.Forms.SystemInformation]::VirtualScreen
    if ($width -le 0 -or $height -le 0 -or $rect.Left -lt $bounds.Left -or $rect.Top -lt $bounds.Top -or
        $rect.Right -gt $bounds.Right -or $rect.Bottom -gt $bounds.Bottom) { throw 'Taskbar capture bounds are invalid or offscreen' }
    $bitmap = [Drawing.Bitmap]::new($width, $height)
    $graphics = [Drawing.Graphics]::FromImage($bitmap)
    try {
        $graphics.CopyFromScreen($rect.Left, $rect.Top, 0, 0, $bitmap.Size, [Drawing.CopyPixelOperation]::SourceCopy)
        $bitmap.Save((Join-Path $evidence 'taskbar.png'), [Drawing.Imaging.ImageFormat]::Png)
    } finally { $graphics.Dispose(); $bitmap.Dispose() }
    $summary.taskbarVerified = $true
}
function Get-RemainingInstallation {
    $files = @()
    if (Test-Path -LiteralPath $installRoot) {
        $files = @(Get-ChildItem -LiteralPath $installRoot -File -Recurse -Force | Select-Object -ExpandProperty FullName)
    }
    [pscustomobject]@{ files = $files; registryPresent = (Test-Path -LiteralPath $registryPath)
        shortcuts = @(Get-StartMenuLinks); processes = @(Get-OwnedProcesses | Select-Object ProcessId, ExecutablePath) }
}
function Save-WindowScreenshot($Candidate) {
    # Refuse a background/occluded window: capture pixels from a foreground visible window.
    $null = [AgentSmoke.Native]::ShowWindow($Candidate.Handle, 3) # maximize to fit the runner display
    $null = [AgentSmoke.Native]::SetForegroundWindow($Candidate.Handle)
    Start-Sleep -Milliseconds 750
    if ([AgentSmoke.Native]::GetForegroundWindow() -ne $Candidate.Handle) { throw 'Cannot bring Agent window to foreground for screenshot' }
    $rect = [AgentSmoke.Native+RECT]::new()
    # GetWindowRect includes invisible resize borders outside the display when maximized.
    # DWMWA_EXTENDED_FRAME_BOUNDS (9) gives the actual visible physical-pixel frame.
    if ([AgentSmoke.Native]::DwmGetWindowAttribute($Candidate.Handle, 9, [ref]$rect, [Runtime.InteropServices.Marshal]::SizeOf($rect)) -ne 0) {
        throw 'Cannot read visible screenshot bounds from DWM'
    }
    $bounds = [Windows.Forms.SystemInformation]::VirtualScreen
    $width = $rect.Right - $rect.Left; $height = $rect.Bottom - $rect.Top
    if ($width -lt 100 -or $height -lt 100 -or $rect.Left -lt $bounds.Left -or $rect.Top -lt $bounds.Top -or
        $rect.Right -gt $bounds.Right -or $rect.Bottom -gt $bounds.Bottom) { throw 'Agent window has invalid/offscreen capture bounds' }
    # Require realized native content, not merely a responsive empty top-level HWND.
    Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
    $automation = [System.Windows.Automation.AutomationElement]::FromHandle($Candidate.Handle)
    $realized = @()
    foreach ($label in @('Connect to your Agent server','Server address','Username','Sign in')) {
        $condition = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::NameProperty,$label)
        $elements = $automation.FindAll([System.Windows.Automation.TreeScope]::Descendants,$condition)
        $matched = $false
        foreach ($element in $elements) {
            $current = $element.Current; $box = $current.BoundingRectangle
            if (-not $current.IsOffscreen -and $box.Width -gt 0 -and $box.Height -gt 0 -and $box.Left -ge $rect.Left -and $box.Top -ge $rect.Top -and $box.Right -le $rect.Right -and $box.Bottom -le $rect.Bottom) {
                $realized += @{ name=$label; left=$box.Left; top=$box.Top; width=$box.Width; height=$box.Height }
                $matched = $true; break
            }
        }
        if (-not $matched) { throw "Missing realized native sign-in control: $label" }
    }
    Write-Json $realized 'rendered-controls.json'
    $bitmap = [Drawing.Bitmap]::new($width, $height)
    $graphics = [Drawing.Graphics]::FromImage($bitmap)
    try {
        $graphics.CopyFromScreen($rect.Left, $rect.Top, 0, 0, $bitmap.Size)
        $colors = [Collections.Generic.HashSet[int]]::new()
        for ($x = 0; $x -lt $width; $x += 17) {
            for ($y = 0; $y -lt $height; $y += 17) { $null = $colors.Add($bitmap.GetPixel($x, $y).ToArgb()) }
        }
        $bitmap.Save((Join-Path $evidence 'window.png'), [Drawing.Imaging.ImageFormat]::Png)
        if ($colors.Count -lt 8) { throw 'Screenshot is blank or has insufficient pixel variation; desktop capture not proven' }
        Write-Json @{ width = $width; height = $height; sampledColors = $colors.Count; foregroundHandle = $Candidate.Handle.ToInt64() } 'screenshot.json'
    } finally { $graphics.Dispose(); $bitmap.Dispose() }
}

try {
    Start-Transcript -LiteralPath (Join-Path $evidence 'transcript.log') -Force | Out-Null
    $transcriptStarted = $true
    if (-not $IsWindows) { throw 'Native Windows is required; WSL/Linux is not a desktop acceptance environment' }
    $installer = (Resolve-Path -LiteralPath $InstallerPath).Path
    if (-not (Test-Path -LiteralPath $installer -PathType Leaf) -or [IO.Path]::GetExtension($installer) -ine '.exe') {
        throw 'InstallerPath must identify the actual provided Setup.exe file'
    }
    $summary.installer = $installer
    $summary.installerSha256 = (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash
    $installRoot = Join-Path ([IO.Path]::GetTempPath()) ('AgentWindows-smoke-' + [guid]::NewGuid().ToString('N'))
    $summary.installRoot = $installRoot
    $appProfile = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'EveryoneSoftware\Agent'
    if (Test-Path -LiteralPath $registryPath) { throw 'Existing AgentWindows registration: use a disposable account' }
    if (Test-Path -LiteralPath $appProfile) { throw 'Existing Agent profile: refusing to alter user data' }
    if (Test-Path -LiteralPath (Join-Path $env:LOCALAPPDATA 'AgentWindows')) { throw 'Existing default AgentWindows installation' }
    if (@(Get-StartMenuLinks).Count) { throw 'Existing Start menu shortcut: refusing to overwrite Agent links' }
    if (-not [Environment]::UserInteractive -or (Get-Process -Id $PID).SessionId -eq 0) {
        throw 'An interactive non-session-0 Windows desktop is required (hosted runner capability is not assumed)'
    }
    Add-Type -AssemblyName System.Windows.Forms, System.Drawing
    Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Collections.Generic;
using System.Runtime.InteropServices;
namespace AgentSmoke {
    public class Window {
        public IntPtr Handle; public string Title; public int Pid;
        public bool Responsive; public int Width; public int Height;
    }
    public static class Native {
        public const uint SMTO_ABORTIFHUNG = 0x0002;
        public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);
        [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
        [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc callback, IntPtr extra);
        [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint processId);
        [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hWnd);
        [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr hWnd, StringBuilder text, int max);
        [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
        [DllImport("dwmapi.dll")] public static extern int DwmGetWindowAttribute(IntPtr hWnd, int attribute, out RECT rect, int size);
        [DllImport("user32.dll")] public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint msg, UIntPtr wParam, IntPtr lParam, uint flags, uint timeout, out UIntPtr result);
        [DllImport("user32.dll")] public static extern IntPtr OpenInputDesktop(uint flags, bool inherit, uint access);
        [DllImport("user32.dll")] public static extern bool SwitchDesktop(IntPtr desktop);
        [DllImport("user32.dll")] public static extern bool CloseDesktop(IntPtr desktop);
        [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
        [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
        [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int command);
        [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr hWnd, uint msg, IntPtr wParam, IntPtr lParam);
        [DllImport("user32.dll")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);
        [DllImport("user32.dll", EntryPoint="FindWindowW", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string className, string title);
        [DllImport("user32.dll", EntryPoint="GetClassLongPtrW")] public static extern IntPtr GetClassLongPtr(IntPtr window, int index);
        [StructLayout(LayoutKind.Sequential)] public struct PROPERTYKEY { public Guid Format; public uint Id; }
        [StructLayout(LayoutKind.Explicit, Size=24)] public struct PROPVARIANT {
            [FieldOffset(0)] public ushort Type; [FieldOffset(8)] public IntPtr Pointer;
        }
        [ComImport, Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
        private interface IPropertyStore {
            [PreserveSig] int GetCount(out uint count);
            [PreserveSig] int GetAt(uint index, out PROPERTYKEY key);
            [PreserveSig] int GetValue(ref PROPERTYKEY key, out PROPVARIANT value);
            [PreserveSig] int SetValue(ref PROPERTYKEY key, ref PROPVARIANT value);
            [PreserveSig] int Commit();
        }
        [DllImport("shell32.dll", ExactSpelling=true)]
        private static extern int SHGetPropertyStoreForWindow(IntPtr window, ref Guid iid, [MarshalAs(UnmanagedType.Interface)] out IPropertyStore store);
        [DllImport("ole32.dll", ExactSpelling=true)] private static extern int PropVariantClear(ref PROPVARIANT value);
        public static string ReadTaskbarProperty(IntPtr window, uint id) {
            var iid = typeof(IPropertyStore).GUID;
            IPropertyStore store;
            Marshal.ThrowExceptionForHR(SHGetPropertyStoreForWindow(window, ref iid, out store));
            try {
                var key = new PROPERTYKEY { Format = new Guid("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3"), Id = id };
                PROPVARIANT value;
                Marshal.ThrowExceptionForHR(store.GetValue(ref key, out value));
                try {
                    if (value.Type != 31) throw new InvalidOperationException("Missing LPWSTR taskbar property " + id);
                    return Marshal.PtrToStringUni(value.Pointer) ?? "";
                } finally { PropVariantClear(ref value); }
            } finally { Marshal.FinalReleaseComObject(store); }
        }
        public static Window[] Find(int pid) {
            var found = new List<Window>();
            EnumWindows((h, ignored) => {
                uint owner; GetWindowThreadProcessId(h, out owner);
                if (owner != pid || !IsWindowVisible(h)) return true;
                var text = new StringBuilder(1024); GetWindowText(h, text, text.Capacity);
                RECT rect; if (!GetWindowRect(h, out rect)) return true;
                UIntPtr result;
                bool responsive = SendMessageTimeout(h, 0 /* WM_NULL */, UIntPtr.Zero, IntPtr.Zero, SMTO_ABORTIFHUNG, 1000, out result) != IntPtr.Zero;
                found.Add(new Window { Handle=h, Title=text.ToString(), Pid=pid, Responsive=responsive, Width=rect.Right-rect.Left, Height=rect.Bottom-rect.Top });
                return true;
            }, IntPtr.Zero);
            return found.ToArray();
        }
    }
}
'@
    $nativeReady = $true
    $null = [AgentSmoke.Native]::SetThreadDpiAwarenessContext([IntPtr]::new(-4))
    $desktop = [AgentSmoke.Native]::OpenInputDesktop(0, $false, 0x0100)
    if ($desktop -eq [IntPtr]::Zero) { throw 'No accessible input desktop (locked/headless runner)' }
    try { if (-not [AgentSmoke.Native]::SwitchDesktop($desktop)) { throw 'Input desktop is not usable/unlocked' } }
    finally { $null = [AgentSmoke.Native]::CloseDesktop($desktop) }
    Write-Json @{ machine = $env:COMPUTERNAME; userInteractive = [Environment]::UserInteractive
        sessionId = (Get-Process -Id $PID).SessionId; os = [Environment]::OSVersion.VersionString
        powershell = $PSVersionTable.PSVersion.ToString(); runnerImage = $env:ImageOS } 'environment.json'

    $mutationStarted = $true # cleanup even when Setup fails or times out partway through
    Invoke-BoundedProcess $installer @('--silent', '--installto', $installRoot, '--log', (Join-Path $evidence 'install.log')) 'install'
    $appExe = Join-Path $installRoot 'current\Agent.exe'
    $updateExe = Join-Path $installRoot 'Update.exe'
    foreach ($relative in @('Update.exe', 'current\Agent.exe', 'current\Agent.dll', 'current\Agent.runtimeconfig.json',
        'current\Microsoft.UI.Xaml.dll', 'current\Microsoft.WindowsAppRuntime.dll', 'current\coreclr.dll', 'current\hostfxr.dll', 'current\resources.pri')) {
        $file = Get-Item -LiteralPath (Join-Path $installRoot $relative)
        if ($file.Length -le 0) { throw "Empty installed file: $relative" }
    }
    Write-Json @(Get-ChildItem -LiteralPath $installRoot -Recurse -File | Select-Object FullName, Length) 'installed-files.json'
    if (-not (Test-Path -LiteralPath $registryPath)) { throw 'Installer did not register AgentWindows' }
    $registration = Get-ItemProperty -LiteralPath $registryPath
    if ($registration.InstallLocation.TrimEnd('\') -ine $installRoot.TrimEnd('\')) { throw 'Installer registered a different install location' }
    # Registry provider metadata includes recursive PSDrive/PSProvider object graphs.
    # Serializing that live object can hang despite the bounded installer process.
    Write-Json ($registration | Select-Object DisplayName, DisplayVersion, InstallLocation, UninstallString) 'registration.json'
    $links = @(Get-StartMenuLinks)
    Write-Json $links 'installed-shortcuts.json'
    if (-not @($links | Where-Object { $_.target.StartsWith($installRoot + '\', [StringComparison]::OrdinalIgnoreCase) }).Count) {
        throw 'No installed Agent Start menu shortcut targets the isolated installation'
    }
    $summary.installVerified = $true
    $previousDiagnostics = $env:AGENT_STARTUP_DIAGNOSTICS
    try {
        $env:AGENT_STARTUP_DIAGNOSTICS = $evidence
        $app = Start-Process -FilePath $appExe -WorkingDirectory (Split-Path $appExe) -PassThru `
            -RedirectStandardOutput (Join-Path $evidence 'app-stdout.txt') -RedirectStandardError (Join-Path $evidence 'app-stderr.txt')
    } finally { $env:AGENT_STARTUP_DIAGNOSTICS = $previousDiagnostics }
    $appPid = $app.Id
    $deadline = [DateTime]::UtcNow.AddSeconds($WindowTimeoutSeconds)
    $stableSamples = 0
    $lastHandle = [IntPtr]::Zero
    $observations = [Collections.Generic.List[object]]::new()
    while ([DateTime]::UtcNow -lt $deadline) {
        $ownedApp = @(Get-OwnedProcesses | Where-Object { $_.ProcessId -eq $appPid -and $_.ExecutablePath -ieq $appExe })
        if (-not $ownedApp.Count) {
            $app.Refresh()
            $exitCode = if ($app.HasExited) { $app.ExitCode } else { $null }
            Write-Json @{ pid = $appPid; hasExited = $app.HasExited; exitCode = $exitCode; executable = $appExe } 'app-exit.json'
            throw "Installed Agent.exe unavailable before responsive-window acceptance (exit code: $exitCode)"
        }
        $candidates = @([AgentSmoke.Native]::Find($appPid))
        $candidate = $null
        foreach ($item in $candidates) {
            $title = $item.Title
            $observations.Add(@{ utc = [DateTime]::UtcNow.ToString('o'); pid = $appPid; title = $title
                handle = $item.Handle.ToInt64(); responsive = $item.Responsive; width = $item.Width; height = $item.Height })
            if ($title -eq 'Agent' -and $item.Responsive -and $item.Width -ge 100 -and $item.Height -ge 100) { $candidate = $item }
        }
        if ($candidate) {
            if ($candidate.Handle -eq $lastHandle) { $stableSamples++ } else { $stableSamples = 1 }
            $lastHandle = $candidate.Handle
            if ($stableSamples -ge 3) { $window = $candidate; break }
        } else { $stableSamples = 0; $lastHandle = [IntPtr]::Zero }
        Start-Sleep -Milliseconds 1000
    }
    Write-Json @($observations.ToArray()) 'window-observations.json'
    if (-not $window) { throw "No real responsive visible Agent title/handle within ${WindowTimeoutSeconds}s" }
    Write-Json @{ pid = $appPid; executable = $appExe; title = $window.Title; handle = $window.Handle.ToInt64()
        responsiveSamples = $stableSamples; width = $window.Width; height = $window.Height } 'window.json'
    Save-WindowScreenshot $window
    Save-TaskbarEvidence $window
    $summary.screenshotSha256 = (Get-FileHash -LiteralPath (Join-Path $evidence 'window.png') -Algorithm SHA256).Hash
    # Re-probe after capture, not just a window that once existed.
    if (-not @([AgentSmoke.Native]::Find($appPid) | Where-Object { $_.Handle -eq $window.Handle -and $_.Responsive }).Count) {
        throw 'Agent window stopped responding during screenshot capture'
    }
    $summary.windowVerified = $true
    $null = [AgentSmoke.Native]::PostMessage($window.Handle, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero) # WM_CLOSE
    $closeDeadline = [DateTime]::UtcNow.AddSeconds(15)
    while (@(Get-OwnedProcesses).Count -and [DateTime]::UtcNow -lt $closeDeadline) { Start-Sleep -Milliseconds 250 }
    if (@(Get-OwnedProcesses).Count) { throw 'Agent did not exit after WM_CLOSE (forced termination is cleanup only)' }
    Invoke-BoundedProcess $updateExe @('--uninstall', '--silent', '--log', (Join-Path $evidence 'uninstall.log')) 'uninstall'
    $removeDeadline = [DateTime]::UtcNow.AddSeconds($RemovalTimeoutSeconds)
    do {
        $remaining = Get-RemainingInstallation
        if (-not $remaining.files.Count -and -not $remaining.registryPresent -and -not $remaining.shortcuts.Count -and -not $remaining.processes.Count) { break }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $removeDeadline)
    Write-Json $remaining 'uninstallRemaining.json'
    if ($remaining.files.Count -or $remaining.registryPresent -or $remaining.shortcuts.Count -or $remaining.processes.Count) {
        throw 'Remaining installed payload, registry, shortcuts, or processes after silent uninstall'
    }
    $summary.uninstallVerified = $true
    $summary.status = 'passed'
} catch {
    $summary.status = 'failed'
    $summary.failure = $_.ToString()
    ($_ | Format-List * -Force | Out-String) | Set-Content -LiteralPath (Join-Path $evidence 'failure.txt')
} finally {
    if ($IsWindows) {
        # Windows Error Reporting can publish events shortly after the failing process exits.
        if ($summary.installVerified -and -not $summary.windowVerified) { Start-Sleep -Seconds 5 }
        try {
            Write-Json @(Get-WinEvent -FilterHashtable @{ LogName = 'Application'; StartTime = $started.ToLocalTime() } -ErrorAction Stop |
                Where-Object { $_.ProviderName -in @('Application Error', '.NET Runtime', 'Windows Error Reporting') } |
                Select-Object TimeCreated, Id, ProviderName, Message) 'events.json'
        } catch { $_.ToString() | Set-Content -LiteralPath (Join-Path $evidence 'events-unavailable.txt') }
    }
    if ($mutationStarted) {
        try {
            Write-Json @(Get-OwnedProcesses | Select-Object ProcessId, ExecutablePath, CommandLine) 'cleanup-processes.json'
            if (Test-Path -LiteralPath $installRoot) {
                Write-Json @(Get-ChildItem -LiteralPath $installRoot -Recurse -File -Force | Select-Object FullName, Length) 'cleanup-files.json'
                foreach ($log in Get-ChildItem -LiteralPath $installRoot -Recurse -File -Filter '*.log') {
                    $relative = [IO.Path]::GetRelativePath($installRoot, $log.FullName).Replace('\', '_')
                    Copy-Item -LiteralPath $log.FullName -Destination (Join-Path $evidence "retained-$relative")
                }
            }
        } catch { $summary.cleanupErrors += "Diagnostics: $_" }
        try { Stop-OwnedProcesses } catch { $summary.cleanupErrors += "Stop: $_" }
        # Always attempt supported uninstall on partial installation/failure, BEFORE fallback removal.
        if ($summary.status -ne 'passed' -and (Test-Path -LiteralPath (Join-Path $installRoot 'Update.exe'))) {
            try {
                Invoke-BoundedProcess (Join-Path $installRoot 'Update.exe') @('--uninstall', '--silent', '--log', (Join-Path $evidence 'cleanup-uninstall.log')) 'cleanup-uninstall'
            } catch { $summary.cleanupErrors += "Uninstall: $_" }
        }
        try { Stop-OwnedProcesses } catch { $summary.cleanupErrors += "Stop after uninstall: $_" }
        # These are only newly created test-owned links/profile/key, never existing user state.
        try {
            foreach ($link in @(Get-StartMenuLinks)) {
                if ($link.target.StartsWith($installRoot + '\', [StringComparison]::OrdinalIgnoreCase)) { Remove-Item -LiteralPath $link.path -Force }
            }
            if (Test-Path -LiteralPath $registryPath) {
                $entry = Get-ItemProperty -LiteralPath $registryPath
                if ($entry.InstallLocation.TrimEnd('\') -ieq $installRoot.TrimEnd('\')) { Remove-Item -LiteralPath $registryPath -Recurse -Force }
                else { throw 'Refusing cleanup of registry entry not owned by isolated install' }
            }
            if (Test-Path -LiteralPath $appProfile) { Remove-Item -LiteralPath $appProfile -Recurse -Force }
        } catch { $summary.cleanupErrors += "User state: $_" }
        try {
            # Bounded retry allows Velopack's delayed Update.exe self-delete to finish first.
            $cleanupDeadline = [DateTime]::UtcNow.AddSeconds(15)
            do {
                try {
                    if (Test-Path -LiteralPath $installRoot) { Remove-Item -LiteralPath $installRoot -Recurse -Force }
                    break
                } catch { if ([DateTime]::UtcNow -ge $cleanupDeadline) { throw }; Start-Sleep -Milliseconds 500 }
            } while ($true)
            $summary.cleanupRemovedInstallRoot = -not (Test-Path -LiteralPath $installRoot)
            if (-not $summary.cleanupRemovedInstallRoot) { throw 'Isolated install directory survived cleanup' }
        } catch { $summary.cleanupErrors += "Directory: $_" }
    }
    if ($summary.cleanupErrors.Count) { $summary.status = 'failed' }
    $summary.finishedUtc = [DateTime]::UtcNow.ToString('o')
    if ($transcriptStarted) {
        try { Stop-Transcript | Out-Null } catch { $summary.cleanupErrors += "Transcript: $_"; $summary.status = 'failed' }
    }
    Write-Json $summary 'summary.json'
}
$summary | ConvertTo-Json -Depth 12 | Write-Output
if ($summary.status -ne 'passed') { exit 1 }
exit 0
