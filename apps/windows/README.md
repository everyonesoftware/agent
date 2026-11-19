# Agent native Windows

Native WinUI 3 runtime-XAML client (`Agent.App`, executable `Agent.exe`), shared protocol/update logic in `Agent.Core`, xUnit tests in `Agent.Core.Tests`. Package identity: `AgentWindows`. No WebView or local model runtime.

## CI interface

```powershell
./scripts/windows-build.ps1 -OutputDirectory dist/windows -Version 0.1.N -Commit <40-character-SHA>
./scripts/test-windows-installer.ps1 -InstallerPath dist/windows/AgentWindows-win-x64-Setup.exe -EvidenceDirectory dist/windows-smoke
```

Build requires native Windows, full clean Git history, .NET 10 SDK and unsigned distribution acknowledgement inherent in this script. Outputs directly in OutputDirectory: `AgentWindows-win-x64-Setup.exe`, `.exe.sha256`, `AgentWindows-0.1.N-win-x64-full.nupkg`, `releases.win-x64.json`, optional pinned-vpk-generated `RELEASES-win-x64`, and `agent-windows-release.json`. Intermediate publish/test files live in a unique system-temporary directory outside OutputDirectory and must not be release attachments. All release payload checks fail closed on missing native WinUI, Windows App Runtime, .NET runtime or merged PRI.

## Local compile gates (not release artifacts)

```sh
source /opt/data/tools/storage-env.sh
export PATH=/opt/data/tools/dotnet:$PATH
export LD_LIBRARY_PATH=/opt/data/tools/icu/usr/lib/x86_64-linux-gnu
node --test apps/windows/validation/*.test.mjs
/opt/data/tools/powershell-7.5.4/pwsh -NoProfile -File apps/windows/validation/packaging-contracts.ps1
 dotnet test apps/windows/Agent.Core.Tests/Agent.Core.Tests.csproj -c Release
 dotnet build apps/windows/Agent.App/Agent.App.csproj -c Release -p:OutputType=Library -p:WindowsAppSDKSelfContained=false -p:PrepareForRunDependsOn=CopyFilesToOutputDirectory
```

Native launch, runtime XAML, PasswordVault, title bar, accessibility, installation, real authenticated end-to-end behavior and installed upgrade are separate acceptance gates. Linux build outputs are never distributable.
