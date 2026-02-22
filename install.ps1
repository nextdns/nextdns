param(
    [ValidateSet("install", "upgrade", "uninstall", "configure")]
    [string]$Command
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Write-Info {
    param([string]$Message)
    Write-Host "INFO: $Message"
}

function Write-WarnMsg {
    param([string]$Message)
    Write-Host "WARN: $Message" -ForegroundColor Yellow
}

function Write-ErrorMsg {
    param([string]$Message)
    Write-Host "ERROR: $Message" -ForegroundColor Red
}

function Get-IsAdmin {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Ensure-Admin {
    param([string]$RunCommand)

    if (Get-IsAdmin) {
        return
    }
    Write-WarnMsg "Administrator privileges are required. Re-launching elevated PowerShell..."
    $args = @(
        "-NoProfile"
        "-ExecutionPolicy", "Bypass"
        "-File", "`"$PSCommandPath`""
    )
    if ($RunCommand) {
        $args += "`"$RunCommand`""
    }
    Start-Process -FilePath "powershell.exe" -Verb RunAs -ArgumentList ($args -join " ")
    exit 0
}

function Get-GoArch {
    switch ($env:PROCESSOR_ARCHITECTURE) {
        "AMD64" { return "amd64" }
        "x86" { return "386" }
        default {
            throw "Unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)"
        }
    }
}

function Get-InstallDir {
    return Join-Path ${env:ProgramFiles} "NextDNS"
}

function Get-NextDNSBin {
    return Join-Path (Get-InstallDir) "nextdns.exe"
}

function Ensure-PathContainsInstallDir {
    $installDir = Get-InstallDir
    $machinePath = [Environment]::GetEnvironmentVariable("Path", "Machine")
    if (-not $machinePath) {
        $machinePath = ""
    }
    $parts = $machinePath.Split(";", [System.StringSplitOptions]::RemoveEmptyEntries)
    $exists = $false
    foreach ($part in $parts) {
        if ($part.Trim().ToLowerInvariant() -eq $installDir.ToLowerInvariant()) {
            $exists = $true
            break
        }
    }
    if (-not $exists) {
        $newPath = if ($machinePath) { "$machinePath;$installDir" } else { $installDir }
        [Environment]::SetEnvironmentVariable("Path", $newPath, "Machine")
        Write-Info "Added $installDir to machine PATH."
    }
    # Refresh current session PATH from persisted values.
    $env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")
}

function Get-LatestRelease {
    if ($env:NEXTDNS_VERSION) {
        return $env:NEXTDNS_VERSION
    }
    try {
        $latest = Invoke-RestMethod -Headers @{ "User-Agent" = "nextdns-install-ps1" } -Uri "https://api.github.com/repos/nextdns/nextdns/releases/latest"
        return ($latest.tag_name -replace "^v", "")
    }
    catch {
        throw "Cannot retrieve latest version from GitHub: $($_.Exception.Message)"
    }
}

function Get-CurrentRelease {
    param([string]$BinPath)
    if (-not (Test-Path -LiteralPath $BinPath)) {
        return ""
    }
    try {
        $out = & $BinPath version 2>$null
        if ($LASTEXITCODE -ne 0 -or -not $out) {
            return ""
        }
        $parts = ($out -join " ").Trim().Split(" ", [System.StringSplitOptions]::RemoveEmptyEntries)
        if ($parts.Length -eq 0) {
            return ""
        }
        return $parts[$parts.Length - 1]
    }
    catch {
        return ""
    }
}

function Get-ReleaseUrl {
    param(
        [string]$Release,
        [string]$GoArch
    )
    if ($Release -match "/") {
        $split = $Release.Split("/", 2)
        $branch = $split[0]
        $hash = $split[1]
        return "https://snapshot.nextdns.io/$branch/nextdns-$hash`_windows_$GoArch.zip"
    }
    return "https://github.com/nextdns/nextdns/releases/download/v$Release/nextdns_$Release`_windows_$GoArch.zip"
}

function Install-Binary {
    param(
        [string]$Release,
        [string]$GoArch
    )
    $url = Get-ReleaseUrl -Release $Release -GoArch $GoArch
    $tmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("nextdns-install-" + [Guid]::NewGuid().ToString("N"))
    $zipPath = Join-Path $tmpRoot "nextdns.zip"
    $extractDir = Join-Path $tmpRoot "extract"
    New-Item -ItemType Directory -Path $extractDir -Force | Out-Null

    try {
        Write-Info "Downloading $url"
        Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing
        Expand-Archive -LiteralPath $zipPath -DestinationPath $extractDir -Force
        $srcBin = Join-Path $extractDir "nextdns.exe"
        if (-not (Test-Path -LiteralPath $srcBin)) {
            throw "Archive did not contain nextdns.exe"
        }
        $installDir = Get-InstallDir
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        Copy-Item -LiteralPath $srcBin -Destination (Get-NextDNSBin) -Force
    }
    finally {
        Remove-Item -LiteralPath $tmpRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}

function Configure-NextDNS {
    param([string]$BinPath)
    $profile = $env:NEXTDNS_PROFILE
    if (-not $profile) {
        $profile = Read-Host "NextDNS Profile ID (6 chars)"
    }
    while ($profile -notmatch "^[0-9a-f]{6}$") {
        Write-ErrorMsg "Invalid profile ID. Expected 6 lowercase alphanumeric chars (example: 123abc)."
        $profile = Read-Host "NextDNS Profile ID (6 chars)"
    }
    & $BinPath install "-profile=$profile"
    if ($LASTEXITCODE -ne 0) {
        throw "nextdns install failed with code $LASTEXITCODE"
    }
}

function Install-NextDNS {
    param(
        [string]$Release,
        [string]$GoArch,
        [string]$BinPath
    )
    if (Test-Path -LiteralPath $BinPath) {
        Write-Info "Already installed."
        return
    }
    Write-Info "Installing NextDNS..."
    Install-Binary -Release $Release -GoArch $GoArch
    Ensure-PathContainsInstallDir
    Configure-NextDNS -BinPath $BinPath
}

function Upgrade-NextDNS {
    param(
        [string]$Release,
        [string]$GoArch,
        [string]$BinPath
    )
    if (-not (Test-Path -LiteralPath $BinPath)) {
        Write-WarnMsg "Not installed. Running install instead."
        Install-NextDNS -Release $Release -GoArch $GoArch -BinPath $BinPath
        return
    }
    Write-Info "Upgrading NextDNS..."
    & $BinPath uninstall
    Install-Binary -Release $Release -GoArch $GoArch
    Ensure-PathContainsInstallDir
    & $BinPath install
}

function Uninstall-NextDNS {
    param([string]$BinPath)
    if (-not (Test-Path -LiteralPath $BinPath)) {
        Write-Info "Not installed."
        return
    }
    Write-Info "Uninstalling NextDNS..."
    & $BinPath uninstall
    Remove-Item -LiteralPath $BinPath -Force -ErrorAction SilentlyContinue
}

function Show-PostInstall {
    Write-Host ""
    Write-Host "Congratulations! NextDNS is now installed."
    Write-Host ""
    Write-Host "Useful commands:"
    Write-Host "  nextdns start"
    Write-Host "  nextdns stop"
    Write-Host "  nextdns restart"
    Write-Host "  nextdns log"
    Write-Host "  nextdns help"
    Write-Host ""
    Write-Host "Binary location:"
    Write-Host "  $(Get-NextDNSBin)"
    Write-Host ""
}

function Main {
    param([string]$RunCommand)

    Ensure-Admin -RunCommand $RunCommand

    $goArch = Get-GoArch
    $release = Get-LatestRelease
    $bin = Get-NextDNSBin
    $current = Get-CurrentRelease -BinPath $bin

    Write-Info "GOARCH: $goArch"
    Write-Info "NEXTDNS_BIN: $bin"
    Write-Info "INSTALL_RELEASE: $release"
    if ($current) {
        Write-Info "CURRENT_RELEASE: $current"
    }

    if ($RunCommand) {
        switch ($RunCommand.ToLowerInvariant()) {
            "install" {
                Install-NextDNS -Release $release -GoArch $goArch -BinPath $bin
                Show-PostInstall
                return
            }
            "upgrade" {
                Upgrade-NextDNS -Release $release -GoArch $goArch -BinPath $bin
                return
            }
            "uninstall" {
                Uninstall-NextDNS -BinPath $bin
                return
            }
            "configure" {
                if (-not (Test-Path -LiteralPath $bin)) {
                    throw "NextDNS is not installed."
                }
                Configure-NextDNS -BinPath $bin
                return
            }
            default {
                throw "Unknown command: $RunCommand. Use: install, upgrade, uninstall, configure"
            }
        }
    }

    while ($true) {
        $current = Get-CurrentRelease -BinPath $bin
        Write-Host ""
        if (-not $current) {
            Write-Host "1) Install NextDNS"
            Write-Host "2) Exit"
            $choice = Read-Host "Choice"
            switch ($choice) {
                "1" {
                    Install-NextDNS -Release $release -GoArch $goArch -BinPath $bin
                    Show-PostInstall
                }
                "" { Install-NextDNS -Release $release -GoArch $goArch -BinPath $bin; Show-PostInstall }
                "2" { return }
                default { Write-WarnMsg "Invalid choice." }
            }
        }
        elseif ($current -ne $release) {
            Write-Host "1) Upgrade NextDNS from $current to $release"
            Write-Host "2) Configure NextDNS"
            Write-Host "3) Remove NextDNS"
            Write-Host "4) Exit"
            $choice = Read-Host "Choice"
            switch ($choice) {
                "1" { Upgrade-NextDNS -Release $release -GoArch $goArch -BinPath $bin }
                "2" { Configure-NextDNS -BinPath $bin }
                "3" { Uninstall-NextDNS -BinPath $bin }
                "4" { return }
                "" { Upgrade-NextDNS -Release $release -GoArch $goArch -BinPath $bin }
                default { Write-WarnMsg "Invalid choice." }
            }
        }
        else {
            Write-Host "1) Configure NextDNS"
            Write-Host "2) Remove NextDNS"
            Write-Host "3) Exit"
            $choice = Read-Host "Choice"
            switch ($choice) {
                "1" { Configure-NextDNS -BinPath $bin }
                "2" { Uninstall-NextDNS -BinPath $bin }
                "3" { return }
                "" { Configure-NextDNS -BinPath $bin }
                default { Write-WarnMsg "Invalid choice." }
            }
        }
    }
}

Main -RunCommand $Command
