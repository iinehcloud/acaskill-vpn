param(
    [string]$Target = "all",
    [switch]$Install
)

$ErrorActionPreference = "Stop"
$OutputDir = ".\build"

Write-Host "AcaSkill VPN Build Script" -ForegroundColor Cyan
Write-Host "=========================" -ForegroundColor Cyan

Write-Host "`nChecking prerequisites..." -ForegroundColor Yellow
if (-not (Get-Command "go" -ErrorAction SilentlyContinue)) {
    Write-Error "Go is not installed."
    exit 1
}
$goVersion = (go version) -replace "go version go", "" -replace " .*", ""
Write-Host "  Go version: $goVersion" -ForegroundColor Green

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

Write-Host "`nDownloading dependencies..." -ForegroundColor Yellow
go mod tidy
if ($LASTEXITCODE -ne 0) { Write-Error "go mod tidy failed"; exit 1 }
Write-Host "  Dependencies ready" -ForegroundColor Green

$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

if ($Target -eq "all" -or $Target -eq "daemon") {
    Write-Host "`nBuilding daemon..." -ForegroundColor Yellow

    # Console build for debug use (no -H windowsgui)
    go build -ldflags="-s -w" -o "$OutputDir\acaskill-daemon.exe" ./cmd/daemon
    if ($LASTEXITCODE -ne 0) { Write-Error "Daemon build failed"; exit 1 }

    $size = [math]::Round((Get-Item "$OutputDir\acaskill-daemon.exe").Length / 1MB, 1)
    Write-Host "  Built: $OutputDir\acaskill-daemon.exe ($size MB)" -ForegroundColor Green
}

if ($Target -eq "all" -or $Target -eq "cli") {
    Write-Host "`nBuilding CLI..." -ForegroundColor Yellow
    go build -ldflags="-s -w" -o "$OutputDir\acaskill-cli.exe" ./cmd/cli
    if ($LASTEXITCODE -ne 0) { Write-Error "CLI build failed"; exit 1 }

    $size = [math]::Round((Get-Item "$OutputDir\acaskill-cli.exe").Length / 1MB, 1)
    Write-Host "  Built: $OutputDir\acaskill-cli.exe ($size MB)" -ForegroundColor Green
}

if ($Install) {
    $currentPrincipal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
    if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Error "Installing requires Administrator privileges."
        exit 1
    }
    $installDir = "C:\Program Files\AcaSkillVPN"
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Copy-Item "$OutputDir\acaskill-daemon.exe" "$installDir\acaskill-daemon.exe" -Force
    Copy-Item "$OutputDir\acaskill-cli.exe"    "$installDir\acaskill-cli.exe"    -Force
    & "$installDir\acaskill-daemon.exe" install
    Start-Service -Name "AcaSkillVPN"
    Write-Host "  Service installed and started" -ForegroundColor Green
}

Write-Host "`nBuild complete!" -ForegroundColor Cyan
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Yellow
Write-Host "  1. Make sure WireGuard for Windows is installed: https://www.wireguard.com/install/"
Write-Host "  2. Set your license key: edit C:\ProgramData\AcaSkillVPN\config.json"
Write-Host "  3. Run as service: .\build\acaskill-daemon.exe install"
Write-Host "  4. Test with CLI:  .\build\acaskill-cli.exe status"
