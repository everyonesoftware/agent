#Requires -Version 7.2
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$OutputDirectory,
    [Parameter(Mandatory)][ValidatePattern('^0\.1\.[1-9][0-9]*$')][string]$Version,
    [Parameter(Mandatory)][ValidatePattern('^[a-f0-9]{40}$')][string]$Commit
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
function Invoke-Checked([string]$Command, [string[]]$Arguments) {
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Command exited $LASTEXITCODE" }
}
function Git-Value([string[]]$Arguments) {
    $value = & git -C $root @Arguments
    if ($LASTEXITCODE -ne 0) { throw 'Git source verification failed' }
    return ($value -join "`n").Trim()
}
function Assert-SourceIdentity {
    if ((Git-Value @('rev-parse','--is-shallow-repository')) -ne 'false') { throw 'Full Git history required' }
    if ((Git-Value @('rev-parse','HEAD')) -cne $Commit) { throw 'Source SHA mismatch' }
    $count = [int](Git-Value @('rev-list','--count','HEAD'))
    if ($count -lt 1 -or $count -gt 65534 -or $Version -cne "0.1.$count") { throw 'Source commit-count version mismatch or assembly version overflow' }
    if (Git-Value @('status','--porcelain','--untracked-files=all')) { throw 'Release sources must be committed and clean' }
}
function Assert-Payload([string]$Directory) {
    $files = @(Get-ChildItem -LiteralPath $Directory -Recurse -Force)
    foreach ($file in $files) { if ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Payload must not contain links' } }
    foreach ($name in @('Agent.exe','Agent.dll','Agent.deps.json','Agent.runtimeconfig.json','coreclr.dll','hostfxr.dll','Microsoft.UI.Xaml.dll','Microsoft.WindowsAppRuntime.dll','resources.pri','Assets/Agent.ico','Assets/Agent.png')) {
        $file = Get-Item -LiteralPath (Join-Path $Directory $name)
        if ($file.PSIsContainer -or $file.Length -le 0) { throw "Missing/empty self-contained payload: $name" }
    }
    $exe = [IO.File]::ReadAllBytes((Join-Path $Directory 'Agent.exe'))
    if ($exe.Length -lt 64 -or $exe[0] -ne 0x4d -or $exe[1] -ne 0x5a) { throw 'Agent.exe is not PE' }
    $offset = [BitConverter]::ToInt32($exe,60)
    if ($offset -lt 64 -or $offset -gt $exe.Length-6 -or [BitConverter]::ToUInt32($exe,$offset) -ne 0x4550 -or [BitConverter]::ToUInt16($exe,$offset+4) -ne 0x8664) { throw 'Agent.exe is not Windows x64' }
    $runtime = (Get-Content -LiteralPath (Join-Path $Directory 'Agent.runtimeconfig.json') -Raw | ConvertFrom-Json -AsHashtable).runtimeOptions
    if ($runtime.ContainsKey('framework') -or $runtime.ContainsKey('frameworks') -or -not $runtime.includedFrameworks) { throw 'Framework-dependent runtime is not distributable' }
    $deps = Get-Content -LiteralPath (Join-Path $Directory 'Agent.deps.json') -Raw | ConvertFrom-Json -AsHashtable
    if ($deps.runtimeTarget.name -notlike '*win-x64' -or -not $deps.libraries.ContainsKey("Agent/$Version")) { throw 'Payload dependency identity mismatch' }
    $info = [Diagnostics.FileVersionInfo]::GetVersionInfo((Join-Path $Directory 'Agent.dll'))
    if ($info.ProductVersion -cne "$Version+$Commit") { throw 'Emitted informational version/source mismatch' }
    foreach ($name in @('Agent.ico','Agent.png')) {
        if ((Get-FileHash -LiteralPath (Join-Path $Directory "Assets/$name")).Hash -ne (Get-FileHash -LiteralPath (Join-Path $root "apps/windows/Agent.App/Assets/$name")).Hash) { throw 'Published brand bytes mismatch' }
    }
}
function Assert-Package([string]$PackagePath, [string]$PublishDirectory) {
    $zip = [IO.Compression.ZipFile]::OpenRead($PackagePath)
    try {
        $entries = [Collections.Generic.Dictionary[string,object]]::new([StringComparer]::OrdinalIgnoreCase)
        foreach ($entry in $zip.Entries) {
            $name = $entry.FullName
            if ($name.Contains('\') -or $name.StartsWith('/') -or $name.Contains(':') -or @($name.Split('/') | Where-Object { $_ -in @('.','..') }).Count -or (($entry.ExternalAttributes -shr 16) -band 0xF000) -eq 0xA000) { throw 'Unsafe package member' }
            if (-not $entries.TryAdd($name,$entry)) { throw 'Case-ambiguous package members' }
        }
        $nuspecs = @($zip.Entries | Where-Object { $_.FullName -match '^[^/]+\.nuspec$' })
        if ($nuspecs.Count -ne 1) { throw 'Ambiguous package manifest' }
        $reader = [IO.StreamReader]::new($nuspecs[0].Open())
        try { [xml]$manifest = $reader.ReadToEnd() } finally { $reader.Dispose() }
        $id = $manifest.SelectSingleNode("//*[local-name()='metadata']/*[local-name()='id']").InnerText
        $v = $manifest.SelectSingleNode("//*[local-name()='metadata']/*[local-name()='version']").InnerText
        if ($id -cne 'AgentWindows' -or $v -cne $Version) { throw 'Package manifest identity mismatch' }
        foreach ($file in Get-ChildItem -LiteralPath $PublishDirectory -Recurse -File) {
            $relative = [IO.Path]::GetRelativePath($PublishDirectory,$file.FullName).Replace('\','/')
            $key = "lib/app/$relative"
            if (-not $entries.ContainsKey($key)) { throw "Published file missing from nupkg: $relative" }
            $entry = $entries[$key]
            if ($entry.Length -ne $file.Length) { throw "Packaged byte length mismatch: $relative" }
            $stream = $entry.Open()
            try { $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($stream)) } finally { $stream.Dispose() }
            if ($hash -ne (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash) { throw "Packaged hash mismatch: $relative" }
        }
    } finally { $zip.Dispose() }
}
function Artifact([string]$Path) {
    $file = Get-Item -LiteralPath $Path
    if ($file.Length -le 0) { throw 'Empty release artifact' }
    return [ordered]@{ name=$file.Name; size=$file.Length; sha256=(Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }
}
function Assert-Feed([string]$FeedPath, [string]$PackagePath) {
    $feed = Get-Content -LiteralPath $FeedPath -Raw | ConvertFrom-Json
    $assets = @($feed.Assets)
    if ($assets.Count -ne 1) { throw 'Feed must contain exactly one full package' }
    $a = $assets[0]; $file = Get-Item -LiteralPath $PackagePath
    if ($a.PackageId -cne 'AgentWindows' -or $a.Version -cne $Version -or $a.Type -cne 'Full' -or $a.FileName -cne $file.Name -or $a.Size -ne $file.Length) { throw 'Feed identity mismatch' }
    $verified = $false
    foreach ($algorithm in @('SHA256','SHA1')) {
        $property = $a.PSObject.Properties[$algorithm]
        if ($property -and $property.Value) {
            if ($property.Value -ine (Get-FileHash -LiteralPath $PackagePath -Algorithm $algorithm).Hash) { throw 'Feed package hash mismatch' }
            $verified = $true
        }
    }
    if (-not $verified) { throw 'Feed has no package digest' }
}
if (-not $IsWindows) { throw 'Native Windows is required; Linux compile output cannot be released' }
Assert-SourceIdentity
$output = [IO.Path]::GetFullPath($OutputDirectory)
if ((Test-Path -LiteralPath $output) -and @(Get-ChildItem -LiteralPath $output -Force).Count) { throw 'OutputDirectory must be new/empty' }
# Intermediates never enter the recursively uploaded release asset directory.
$work = Join-Path ([IO.Path]::GetTempPath()) ('AgentWindows-build-' + [guid]::NewGuid().ToString('N'))
$publish = Join-Path $work 'publish'; $packed = Join-Path $work 'packed'
$null = New-Item -ItemType Directory -Path $work
$previousIcon = $env:VPK_ICON
try {
    Push-Location (Join-Path $root 'apps/windows')
    try {
        Invoke-Checked 'node' @('generate-icons.mjs')
        Assert-SourceIdentity
        Invoke-Checked 'dotnet' @('tool','restore','--tool-manifest','.config/dotnet-tools.json')
        $toolVersion = & dotnet tool run vpk -- --help
        if ($LASTEXITCODE -ne 0 -or ($toolVersion -join '') -notmatch '1\.2\.161') { throw 'Pinned vpk 1.2.161 required' }
        Invoke-Checked 'dotnet' @('test','Agent.Core.Tests/Agent.Core.Tests.csproj','-c','Release','--results-directory',(Join-Path $work 'tests'))
        $contracts = @(Get-ChildItem -LiteralPath 'validation' -Filter '*.test.mjs' | ForEach-Object FullName)
        Invoke-Checked 'node' (@('--test') + $contracts)
        Invoke-Checked (Join-Path $PSHOME 'pwsh.exe') @('-NoProfile','-File','validation/packaging-contracts.ps1')
        Invoke-Checked 'dotnet' @('publish','Agent.App/Agent.App.csproj','-c','Release','-r','win-x64','--self-contained','true','-p:WindowsAppSDKSelfContained=true','-p:AppxGeneratePriEnabled=true','-p:ContinuousIntegrationBuild=true',"-p:Version=$Version","-p:InformationalVersion=$Version+$Commit",'-o',$publish)
        Assert-Payload $publish
        $env:VPK_ICON = Join-Path $publish 'Assets/Agent.ico'
        Invoke-Checked 'dotnet' @('tool','run','vpk','--','[win]','pack','--packId','AgentWindows','--packVersion',$Version,'--packDir',$publish,'--mainExe','Agent.exe','--packTitle','Agent','--packAuthors','Everyone Software','--runtime','win-x64','--channel','win-x64','--framework','vcredist143-x64','--noPortable','--noDefaultExclude','--exclude','(?!)','--delta','None','--shortcuts','StartMenuRoot','--outputDir',$packed,'--yes','--skip-updates')
    } finally { Pop-Location }
    Assert-SourceIdentity
    $packageName = "AgentWindows-$Version-win-x64-full.nupkg"
    $packagePath = Join-Path $packed $packageName
    Assert-Package $packagePath $publish
    Assert-Feed (Join-Path $packed 'releases.win-x64.json') $packagePath
    $installer = Artifact (Join-Path $packed 'AgentWindows-win-x64-Setup.exe')
    $package = Artifact $packagePath
    $null = New-Item -ItemType Directory -Path $output -Force
    foreach ($name in @($installer.name,$package.name,'releases.win-x64.json')) { Copy-Item -LiteralPath (Join-Path $packed $name) -Destination (Join-Path $output $name) }
    if (Test-Path -LiteralPath (Join-Path $packed 'RELEASES-win-x64')) { Copy-Item -LiteralPath (Join-Path $packed 'RELEASES-win-x64') -Destination $output }
    [IO.File]::WriteAllText((Join-Path $output ($installer.name+'.sha256')),($installer.sha256+'  '+$installer.name+"`n"))
    $manifest = [ordered]@{schemaVersion=1;platform='windows';version=$Version;versionCode=[int]$Version.Split('.')[2];commit=$Commit;installer=$installer;package=$package}
    [IO.File]::WriteAllText((Join-Path $output 'agent-windows-release.json'),(($manifest | ConvertTo-Json -Depth 5)+"`n"))
    foreach ($artifact in @($installer,$package)) {
        $copied = Artifact (Join-Path $output $artifact.name)
        if ($copied.size -ne $artifact.size -or $copied.sha256 -cne $artifact.sha256) { throw 'Staged artifact byte mismatch' }
    }
    Write-Output "Unsigned Agent $Version packaged. Native installed-window smoke is still required."
} finally {
    $env:VPK_ICON = $previousIcon
    # Retain failed build diagnostics outside the publication inventory.
    Write-Output "Build diagnostics: $work"
}
