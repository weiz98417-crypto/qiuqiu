param(
    [string]$PgRoot = 'C:\Program Files\PostgreSQL\17',
    [string]$Version = 'v0.8.1',
    [switch]$Elevated
)

$ErrorActionPreference = 'Stop'

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

$pgConfig = Join-Path $PgRoot 'bin\pg_config.exe'
if (-not (Test-Path -LiteralPath $pgConfig)) {
    throw "PostgreSQL was not found at $PgRoot"
}

$sourceDir = Join-Path $env:TEMP "qiuqiu-pgvector-$($Version.TrimStart('v'))"
if (-not (Test-Path -LiteralPath $sourceDir)) {
    git clone --depth 1 --branch $Version https://github.com/pgvector/pgvector.git $sourceDir
    if ($LASTEXITCODE -ne 0) {
        throw 'Unable to download pgvector source.'
    }
}

$vswhere = 'C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe'
if (-not (Test-Path -LiteralPath $vswhere)) {
    throw 'Visual Studio Build Tools 2022 with the C++ workload is required.'
}
$visualStudioRoot = & $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
if ([string]::IsNullOrWhiteSpace($visualStudioRoot)) {
    throw 'Visual Studio Build Tools 2022 with the C++ workload is required.'
}

$pgRootForBuild = (& $pgConfig --bindir | Split-Path -Parent).Replace('C:\Program Files', 'C:\PROGRA~1').Replace('PostgreSQL', 'POSTGR~1')
$developerPrompt = Join-Path $visualStudioRoot 'Common7\Tools\VsDevCmd.bat'
$buildCommand = "`"$developerPrompt`" -arch=x64 -host_arch=x64 && set `"PGROOT=$pgRootForBuild`" && cd /d `"$sourceDir`" && nmake /F Makefile.win"
cmd.exe /d /s /c $buildCommand
if ($LASTEXITCODE -ne 0) {
    throw 'pgvector compilation failed.'
}

if (-not $Elevated -and -not (Test-Administrator)) {
    $arguments = @(
        '-NoProfile',
        '-ExecutionPolicy', 'Bypass',
        '-File', "`"$PSCommandPath`"",
        '-PgRoot', "`"$PgRoot`"",
        '-Version', $Version,
        '-Elevated'
    )
    $process = Start-Process powershell.exe -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ArgumentList $arguments
    if ($process.ExitCode -ne 0) {
        throw "Elevated pgvector installation failed with exit code $($process.ExitCode)."
    }
    exit 0
}

$extensionDir = Join-Path $PgRoot 'share\extension'
$libraryDir = Join-Path $PgRoot 'lib'
$headerDir = Join-Path $PgRoot 'include\server\extension\vector'
New-Item -ItemType Directory -Force -Path $headerDir | Out-Null
Copy-Item -LiteralPath (Join-Path $sourceDir 'vector.dll') -Destination $libraryDir -Force
Copy-Item -LiteralPath (Join-Path $sourceDir 'vector.control') -Destination $extensionDir -Force
Get-ChildItem -LiteralPath (Join-Path $sourceDir 'sql') -Filter 'vector--*.sql' |
    Copy-Item -Destination $extensionDir -Force
foreach ($header in 'halfvec.h', 'sparsevec.h', 'vector.h') {
    Copy-Item -LiteralPath (Join-Path $sourceDir "src\$header") -Destination $headerDir -Force
}

Write-Host "pgvector $Version installed into $PgRoot"
