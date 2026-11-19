#Requires -Version 7.2
# Execute the real packaging validators without executing the native build.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
$Version = '0.1.123'
[xml]$shell = Get-Content -LiteralPath (Join-Path $root 'apps/windows/Agent.App/Shell.xaml') -Raw
$controls = @{}
foreach ($element in $shell.SelectNodes('//*')) {
    $name = $element.GetAttribute('Name','http://schemas.microsoft.com/winfx/2006/xaml')
    if ($name) {
        if ($controls.ContainsKey($name)) { throw "Duplicate native XAML name: $name" }
        $controls[$name] = $element.LocalName
    }
}
foreach ($source in Get-ChildItem -LiteralPath (Join-Path $root 'apps/windows/Agent.App') -Filter 'MainWindow*.cs') {
    foreach ($lookup in [regex]::Matches((Get-Content -LiteralPath $source.FullName -Raw),'C<(?<type>\w+)>\("(?<name>\w+)"\)')) {
        $name = $lookup.Groups['name'].Value; $type = $lookup.Groups['type'].Value
        if (-not $controls.ContainsKey($name) -or $controls[$name] -cne $type) { throw "Missing/mistyped native XAML control: $name ($type)" }
    }
}
Write-Output 'PASS runtime XAML XML and all typed native control lookups'
$tokens = $null; $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile((Join-Path $root 'scripts/windows-build.ps1'),[ref]$tokens,[ref]$errors)
if ($errors.Count) { throw ($errors -join "`n") }
foreach ($name in @('Assert-Package','Assert-Feed','Artifact')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name },$true)
    if (-not $definition) { throw "Missing production validator: $name" }
    . ([scriptblock]::Create($definition.Extent.Text))
}
function Must-Reject([scriptblock]$Action,[string]$Label) {
    $rejected = $false
    try { & $Action } catch { $rejected = $true }
    if (-not $rejected) { throw "Validator accepted $Label" }
    Write-Output "PASS reject $Label"
}
$base = Join-Path ([IO.Path]::GetTempPath()) ('Agent-pack-contract-' + [guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $base
try {
    $publish = Join-Path $base 'publish'; $null = New-Item -ItemType Directory -Path $publish
    [IO.File]::WriteAllText((Join-Path $publish 'Agent.dll'),'explicit test payload bytes')
    $package = Join-Path $base "AgentWindows-$Version-win-x64-full.nupkg"
    $zip = [IO.Compression.ZipFile]::Open($package,[IO.Compression.ZipArchiveMode]::Create)
    try {
        $entry = $zip.CreateEntry('AgentWindows.nuspec'); $writer = [IO.StreamWriter]::new($entry.Open())
        try { $writer.Write("<package><metadata><id>AgentWindows</id><version>$Version</version></metadata></package>") } finally { $writer.Dispose() }
        $null = [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip,(Join-Path $publish 'Agent.dll'),'lib/app/Agent.dll')
    } finally { $zip.Dispose() }
    Assert-Package $package $publish
    Write-Output 'PASS exact published package bytes'
    $artifact = Artifact $package
    $feed = Join-Path $base 'releases.win-x64.json'
    $asset = @{ PackageId='AgentWindows'; Version=$Version; Type='Full'; FileName=$artifact.name; Size=$artifact.size; SHA256=$artifact.sha256 }
    function Save-Feed { @{Assets=@($asset)} | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $feed }
    Save-Feed; Assert-Feed $feed $package
    Write-Output 'PASS exact feed package identity and SHA256'
    $asset.Version='0.1.122'; Save-Feed; Must-Reject { Assert-Feed $feed $package } 'wrong feed version'
    $asset.Version=$Version; $asset.SHA256=('0'*64); Save-Feed; Must-Reject { Assert-Feed $feed $package } 'wrong feed digest'
    $asset.SHA256=$artifact.sha256; $asset.PackageId='OtherApp'; Save-Feed; Must-Reject { Assert-Feed $feed $package } 'wrong package identity'
    [IO.File]::WriteAllText((Join-Path $publish 'Agent.dll'),'same length different bytes')
    Must-Reject { Assert-Package $package $publish } 'changed packaged payload bytes'
    $zip = [IO.Compression.ZipFile]::Open($package,[IO.Compression.ZipArchiveMode]::Update)
    try { $null=$zip.CreateEntry('../escape') } finally { $zip.Dispose() }
    Must-Reject { Assert-Package $package $publish } 'traversal package entry'
} finally { Remove-Item -LiteralPath $base -Force -Recurse }
