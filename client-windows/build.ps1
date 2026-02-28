# =============================================================================
# AcaSkill VPN - Windows Build Script
# Run from the client-windows directory on a Windows machine with Go installed
# Requires: Go 1.22+, WireGuard Go library
# =============================================================================

param(
    [string]$Target = "all",  # all | daemon | cli
    [switch]$Install           # install daemon as Windows service after build
)

$ErrorActionPreference = "Stop"

$OutputDir = ".\build"
$DaemonExe = "$OutputDir\acaskill-daemon.exe"
$CLIExe    = "$OutputDir\acaskill-cli.exe"

Write-Host "AcaSkill VPN Build Script" -ForegroundColor Cyan
Write-Host "=========================" -ForegroundColor Cyan

# ── Check prerequisites ───────────────────────────────────────────────────────
Write-Host "`nChecking prerequisites..." -ForegroundColor Yellow

if (-not (Get-Command "go" -ErrorAction SilentlyContinue)) {
    Write-Error "Go is not installed. Download from https://go.dev/dl/"
    exit 1
}

$goVersion = (go version) -replace "go version go", "" -replace " .*", ""
Write-Host "  Go version: $goVersion" -ForegroundColor Green

# ── Create output directory ───────────────────────────────────────────────────
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

# ── Download dependencies ─────────────────────────────────────────────────────
Write-Host "`nDownloading dependencies..." -ForegroundColor Yellow
go mod tidy
if ($LASTEXITCODE -ne 0) { Write-Error "go mod tidy failed"; exit 1 }
Write-Host "  Dependencies ready" -ForegroundColor Green

# ── Build daemon ──────────────────────────────────────────────────────────────
if ($Target -eq "all" -or $Target -eq "daemon") {
    Write-Host "`nBuilding daemon..." -ForegroundColor Yellow
    
    $env:GOOS   = "windows"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"
    
    go build `
        -ldflags="-H windowsgui -s -w -X main.Version=1.0.0" `
        -o $DaemonExe `
        ./cmd/daemon
    
    if ($LASTEXITCODE -ne 0) { Write-Error "Daemon build failed"; exit 1 }
    
    $size = (Get-Item $DaemonExe).Length / 1MB
    Write-Host "  Built: $DaemonExe ($([math]::Round($size, 1)) MB)" -ForegroundColor Green
}

# ── Build CLI ─────────────────────────────────────────────────────────────────
if ($Target -eq "all" -or $Target -eq "cli") {
    Write-Host "`nBuilding CLI..." -ForegroundColor Yellow
    
    $env:GOOS   = "windows"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "0"
    
    go build `
        -ldflags="-s -w" `
        -o $CLIExe `
        ./cmd/cli
    
    if ($LASTEXITCODE -ne 0) { Write-Error "CLI build failed"; exit 1 }
    
    $size = (Get-Item $CLIExe).Length / 1MB
    Write-Host "  Built: $CLIExe ($([math]::Round($size, 1)) MB)" -ForegroundColor Green
}

# ── Install service (optional) ────────────────────────────────────────────────
if ($Install) {
    Write-Host "`nInstalling Windows service..." -ForegroundColor Yellow
    
    # Must run as administrator
    $currentPrincipal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
    if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Error "Installing the service requires Administrator privileges. Run PowerShell as Administrator."
        exit 1
    }
    
    # Copy to Program Files
    $installDir = "C:\Program Files\AcaSkillVPN"
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Copy-Item $DaemonExe "$installDir\acaskill-daemon.exe" -Force
    Copy-Item $CLIExe    "$installDir\acaskill-cli.exe"    -Force
    
    # Install service
    & "$installDir\acaskill-daemon.exe" install
    if ($LASTEXITCODE -ne 0) { Write-Error "Service install failed"; exit 1 }
    
    # Start service
    Start-Service -Name "AcaSkillVPN"
    Write-Host "  Service installed and started" -ForegroundColor Green
    
    # Add CLI to PATH
    $machinePath = [System.Environment]::GetEnvironmentVariable("Path", "Machine")
    if ($machinePath -notlike "*AcaSkillVPN*") {
        [System.Environment]::SetEnvironmentVariable(
            "Path",
            "$machinePath;$installDir",
            "Machine"
        )
        Write-Host "  Added to PATH" -ForegroundColor Green
    }
}

Write-Host "`nBuild complete!" -ForegroundColor Cyan
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Yellow
Write-Host "  1. Make sure WireGuard for Windows is installed: https://www.wireguard.com/install/"
Write-Host "  2. Set your license key: edit C:\ProgramData\AcaSkillVPN\config.json"
Write-Host "  3. Run as service: .\build\acaskill-daemon.exe install"
Write-Host "  4. Test with CLI:  .\build\acaskill-cli.exe status"
