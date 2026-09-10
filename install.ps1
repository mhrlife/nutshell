<#
.SYNOPSIS
    nutshell installer for Windows.

.DESCRIPTION
    Picks the build for this machine, verifies its checksum, drops nutshell.exe
    in %LOCALAPPDATA%\nutshell\bin and adds that directory to your user PATH.

    irm https://github.com/mhrlife/nutshell/releases/latest/download/install.ps1 | iex

.PARAMETER Version
    Install a specific version, e.g. v0.1.0. Defaults to the latest release.

.PARAMETER Dir
    Install directory. Defaults to $env:LOCALAPPDATA\nutshell\bin.

.PARAMETER NoModifyPath
    Leave the user PATH alone.

.PARAMETER Force
    Reinstall even if this version is already present.
#>
[CmdletBinding()]
param(
    [string]$Version = $env:NUTSHELL_VERSION,
    [string]$Dir = $env:NUTSHELL_INSTALL_DIR,
    [switch]$NoModifyPath,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Repo = 'mhrlife/nutshell'
$App = 'nutshell'

function Write-Step($Message) { Write-Host "==> " -ForegroundColor Green -NoNewline; Write-Host $Message }
function Write-Note($Message) { Write-Host $Message -ForegroundColor DarkGray }
function Fail($Message) { Write-Host "error: $Message" -ForegroundColor Red; exit 1 }

if (-not $Dir) { $Dir = Join-Path $env:LOCALAPPDATA "$App\bin" }

# --- what are we running on? ------------------------------------------------

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    'x86'   { Fail 'nutshell has no 32-bit build' }
    default { Fail "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

# --- which version? ---------------------------------------------------------

if (-not $Version) {
    Write-Step 'Looking up the latest release'
    try {
        # /releases/latest redirects to /releases/tag/<tag>; reading the
        # redirect avoids the GitHub API and its rate limit.
        $response = Invoke-WebRequest -Uri "https://github.com/$Repo/releases/latest" `
            -MaximumRedirection 0 -ErrorAction SilentlyContinue -UseBasicParsing
        $location = $response.Headers.Location
    } catch {
        $location = $_.Exception.Response.Headers.Location
    }
    if (-not $location) { Fail 'could not reach github.com' }
    $Version = ($location -split '/')[-1]
}

if (-not $Version.StartsWith('v')) { $Version = "v$Version" }
if ($Version -eq 'vreleases') { Fail "$Repo has no releases yet" }

$exe = Join-Path $Dir "$App.exe"

if (-not $Force -and (Test-Path $exe)) {
    $installed = (& $exe --version 2>$null) -split ' ' | Select-Object -First 1
    if ($installed -eq $Version) {
        Write-Host "$App $Version is already installed at $exe"
        Write-Note 'Pass -Force to reinstall.'
        exit 0
    }
}

# --- download and verify ----------------------------------------------------

$archive = "${App}_$($Version.TrimStart('v'))_windows_$arch.zip"
$baseUrl = "https://github.com/$Repo/releases/download/$Version"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) "nutshell-install-$([guid]::NewGuid())"
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
    Write-Step "Downloading $archive"
    try {
        Invoke-WebRequest -Uri "$baseUrl/$archive" -OutFile "$tmp\$archive" -UseBasicParsing
    } catch {
        Fail "no build for windows/$arch in ${Version}: https://github.com/$Repo/releases"
    }

    try {
        Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile "$tmp\checksums.txt" -UseBasicParsing
        $expected = (Get-Content "$tmp\checksums.txt" |
            Where-Object { ($_ -split '\s+')[-1].TrimStart('*') -eq $archive } |
            ForEach-Object { ($_ -split '\s+')[0] } | Select-Object -First 1)
        if (-not $expected) { Fail "$archive is missing from checksums.txt" }

        $actual = (Get-FileHash "$tmp\$archive" -Algorithm SHA256).Hash
        if ($actual -ne $expected.ToUpper()) {
            Fail "checksum mismatch for $archive - refusing to install"
        }
        Write-Note 'checksum ok'
    } catch [System.Net.WebException] {
        Write-Warning 'could not fetch checksums.txt; skipping verification'
    }

    # --- install ------------------------------------------------------------

    Expand-Archive -Path "$tmp\$archive" -DestinationPath $tmp -Force
    if (-not (Test-Path "$tmp\$App.exe")) { Fail "$archive did not contain $App.exe" }

    New-Item -ItemType Directory -Path $Dir -Force | Out-Null
    Copy-Item "$tmp\$App.exe" $exe -Force

    # SmartScreen blocks binaries carrying the mark of the web; the release
    # builds are unsigned, so clear it.
    Unblock-File -Path $exe -ErrorAction SilentlyContinue
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Step "Installed $App $Version to $exe"

# --- PATH -------------------------------------------------------------------

$onPath = ($env:PATH -split ';') -contains $Dir

if (-not $onPath -and -not $NoModifyPath) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $Dir) {
        $userPath = if ($userPath) { "$userPath;$Dir" } else { $Dir }
        [Environment]::SetEnvironmentVariable('Path', $userPath, 'User')
        Write-Step "Added $Dir to your user PATH"
    }
    $env:PATH = "$env:PATH;$Dir"
}

# --- what's left to do ------------------------------------------------------

Write-Host ''
Write-Host "nutshell $Version"
Write-Host ''

if (-not $onPath) {
    Write-Host "  Open a new terminal so PATH takes effect"
}
if (-not (Get-Command claude -ErrorAction SilentlyContinue)) {
    Write-Host "  Install the agent CLI:   npm i -g @anthropic-ai/claude-code"
}
if (-not $env:OPENROUTER_API_KEY) {
    Write-Host '  Set a key for voice:     $env:OPENROUTER_API_KEY = "sk-or-..."'
    Write-Note '  Without it nutshell still works with typed questions.'
}
Write-Host "  Then, in any project:    nutshell"
Write-Host ''
