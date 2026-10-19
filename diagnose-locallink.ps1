<#
    LocalLink reachability check.

    Read-only by default. Pass -Fix to add the firewall rule and switch the
    active network profile to Private (both need an elevated PowerShell).

    Run:
        powershell -NoProfile -ExecutionPolicy Bypass -File .\diagnose-locallink.ps1
        powershell -NoProfile -ExecutionPolicy Bypass -File .\diagnose-locallink.ps1 -Fix

    Exec policy note: -ExecutionPolicy Bypass above is scoped to this one
    process, exactly like on your machine earlier.
#>

param(
    [switch]$Fix,
    [int]$Port = 8080,
    [string]$TargetIp = '192.168.18.27'
)

$ErrorActionPreference = 'SilentlyContinue'

function Section([string]$title) {
    Write-Host ''
    Write-Host "=== $title ===" -ForegroundColor Cyan
}

function Verdict([string]$text, [string]$level = 'info') {
    $color = switch ($level) { 'bad' { 'Red' } 'warn' { 'Yellow' } 'good' { 'Green' } default { 'Gray' } }
    Write-Host "  -> $text" -ForegroundColor $color
}

# ---------------------------------------------------------------------------
Section '1. IPv4 addresses on this machine'

$allIps = Get-NetIPAddress -AddressFamily IPv4 |
    Where-Object { $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' }

$allIps | Select-Object IPAddress, PrefixLength, InterfaceAlias, @{
    n = 'Virtual'; e = {
        $_.InterfaceAlias -match 'vEthernet|VirtualBox|VMware|Hyper-V|Loopback|Tailscale|ZeroTier|WireGuard|VPN'
    }
} | Format-Table -AutoSize

$realIps = $allIps | Where-Object {
    $_.InterfaceAlias -notmatch 'vEthernet|VirtualBox|VMware|Hyper-V|Loopback|Tailscale|ZeroTier|WireGuard'
}

if ($allIps | Where-Object { $_.IPAddress -eq $TargetIp }) {
    Verdict "$TargetIp IS assigned to this machine." 'good'
} else {
    Verdict "$TargetIp is NOT assigned to this machine. That alone explains the timeout." 'bad'
}

if ($allIps.Count -gt 1) {
    Verdict ("This host has {0} non-loopback IPv4 addresses. Make sure you and the clients use the one on the real adapter: {1}" -f `
            $allIps.Count, (($realIps | ForEach-Object { $_.IPAddress }) -join ', ')) 'warn'
}

# ---------------------------------------------------------------------------
Section '2. Which interface actually has the default route'

$route = Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object RouteMetric | Select-Object -First 1
if ($route) {
    $route | Select-Object DestinationPrefix, NextHop, InterfaceAlias, RouteMetric | Format-Table -AutoSize
    Verdict "Traffic to the outside world (and usually to your phone) leaves via '$($route.InterfaceAlias)'."
    Verdict "If the LAN IP you advertise is on a DIFFERENT adapter, clients on that adapter may be unreachable." 'warn'
} else {
    Verdict 'No default route found.' 'bad'
}

# ---------------------------------------------------------------------------
Section "3. Is anything listening on TCP $Port, and on which address"

$listeners = Get-NetTCPConnection -LocalPort $Port -State Listen
if ($listeners) {
    $listeners | ForEach-Object {
        [pscustomobject]@{
            LocalAddress = $_.LocalAddress
            LocalPort    = $_.LocalPort
            PID          = $_.OwningProcess
            Process      = (Get-Process -Id $_.OwningProcess).ProcessName
            Path         = (Get-Process -Id $_.OwningProcess).Path
        }
    } | Format-Table -AutoSize

    if ($listeners | Where-Object { $_.LocalAddress -eq '0.0.0.0' -or $_.LocalAddress -eq '::' }) {
        Verdict "Listening on all interfaces - binding is correct." 'good'
    } else {
        Verdict ("Bound only to {0}. Clients off this host can never reach it. Bind to 0.0.0.0 (empty host) instead." -f `
                (($listeners | ForEach-Object { $_.LocalAddress }) -join ', ')) 'bad'
    }

    $paths = $listeners | ForEach-Object { (Get-Process -Id $_.OwningProcess).Path } | Where-Object { $_ } | Select-Object -Unique
    if ($paths | Where-Object { $_ -match '\\Temp\\|go-build' }) {
        Verdict "'go run' compiles to a NEW temp exe path on every build, so any firewall rule tied to that path is dead next time. Build a stable exe (see the -Fix notes)." 'bad'
    }
} else {
    Verdict "Nothing is listening on TCP $Port. Start the server first." 'bad'
}

# ---------------------------------------------------------------------------
Section '4. Network category (Public blocks inbound by default)'

Get-NetConnectionProfile | Select-Object Name, InterfaceAlias, NetworkCategory, IPv4Connectivity | Format-Table -AutoSize

$publicProfiles = Get-NetConnectionProfile | Where-Object { $_.NetworkCategory -eq 'Public' }
if ($publicProfiles) {
    Verdict 'At least one network is Public. Windows blocks unsolicited inbound on Public by default.' 'bad'
} else {
    Verdict 'No Public-profile networks. Good.' 'good'
}

# ---------------------------------------------------------------------------
Section "5. Firewall state and rules for port $Port"

$fw = Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction, Notifications
$fw | Format-Table -AutoSize

if ($fw | Where-Object { $_.Name -eq 'Domain' -and $_.Notifications -eq 'True' }) {
    Verdict 'Notification popups are on for Domain; a dismissed prompt silently creates a Block rule.' 'warn'
}

$portRules = Get-NetFirewallRule -Direction Inbound -Enabled True |
    Where-Object { $_.DisplayName -match 'LocalLink' }

if ($portRules) {
    $portRules | Select-Object DisplayName, Action, Profile, Enabled | Format-Table -AutoSize
    if ($portRules | Where-Object { $_.Action -eq 'Block' }) {
        Verdict 'There is a BLOCK rule matching LocalLink. Remove it: Get-NetFirewallRule -DisplayName "LocalLink*" | Remove-NetFirewallRule' 'bad'
    }
} else {
    Verdict "No inbound rule referencing LocalLink found - inbound $Port is probably being dropped." 'bad'
}

# Block rules on the port itself matter more than allow rules
$blocking = Get-NetFirewallRule -Direction Inbound -Enabled True -Action Block |
    Where-Object { $_.DisplayName -match 'go|server|locallink' }
if ($blocking) {
    $blocking | Select-Object DisplayName, Profile | Format-Table -AutoSize
    Verdict 'Inbound BLOCK rules exist for go/server processes.' 'bad'
}

# ---------------------------------------------------------------------------
Section '6. Same-machine reachability (the decisive test)'

$curl = Join-Path $env:SystemRoot 'System32\curl.exe'

function Test-Http([string]$url, [string]$label) {
    if (Test-Path $curl) {
        $out = & $curl -sS -m 5 -o NUL -w '%{http_code}' $url 2>&1
        if ($LASTEXITCODE -eq 0 -and $out -match '^\d{3}$') {
            Verdict ("{0} -> HTTP {1}" -f $label, $out) ($(if ($out -match '^[45]') { 'warn' } else { 'good' }))
        } else {
            Verdict ("{0} -> FAILED ({1})" -f $label, ($out -join ' ').Trim()) 'bad'
        }
    }
}

Test-Http "http://127.0.0.1:$Port/admin" 'loopback 127.0.0.1'
foreach ($ip in ($allIps | ForEach-Object { $_.IPAddress })) {
    Test-Http "http://${ip}:$Port/admin" "own IPv4 $ip"
}

$tcp = Test-NetConnection -ComputerName $TargetIp -Port $Port -InformationLevel Quiet -WarningAction SilentlyContinue
if ($tcp) {
    Verdict "TCP connect to ${TargetIp}:$Port from this machine: OK" 'good'
} else {
    Verdict "TCP connect to ${TargetIp}:$Port from this machine: FAILED" 'bad'
}

# ---------------------------------------------------------------------------
Section '7. Proxy / VPN hijacking the route'

$proxy = netsh winhttp show proxy 2>&1 | Out-String
Write-Host $proxy.Trim()
if ($proxy -match '(?i)proxy server') {
    Verdict 'A WinHTTP proxy is configured; Chrome may follow the system proxy and never hit the LAN.' 'warn'
}

Get-NetRoute -DestinationPrefix "192.168.18.0/24" | Select-Object DestinationPrefix, NextHop, InterfaceAlias, RouteMetric | Format-Table -AutoSize

$vpnIfaces = Get-NetAdapter | Where-Object {
    $_.Status -eq 'Up' -and $_.InterfaceDescription -match 'VPN|TAP|WireGuard|Tailscale|ZeroTier|WARP|GlobalProtect'
}
if ($vpnIfaces) {
    $vpnIfaces | Select-Object Name, InterfaceDescription | Format-Table -AutoSize
    Verdict 'A VPN-style adapter is up. If it captures 192.168.18.0/24, the LAN traffic goes into the tunnel and dies.' 'warn'
}

# ---------------------------------------------------------------------------
if ($Fix) {
    Section 'APPLYING FIXES'

    $elevated = ([Security.Principal.WindowsPrincipal] `
            [Security.Principal.WindowsIdentity]::GetCurrent()
    ).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

    if (-not $elevated) {
        Verdict 'Not elevated. Re-run this script from an Administrator PowerShell to apply fixes.' 'bad'
    } else {
        $existing = Get-NetFirewallRule -DisplayName 'LocalLink dev (TCP)' -ErrorAction SilentlyContinue
        if (-not $existing) {
            New-NetFirewallRule `
                -DisplayName 'LocalLink dev (TCP)' `
                -Direction Inbound `
                -Action Allow `
                -Protocol TCP `
                -LocalPort $Port `
                -Profile Private, Domain `
                -Description 'Allow LocalLink backend during development. Port-based, so it survives go run temp exe paths.' | Out-Null
            Verdict "Added inbound allow rule for TCP $Port (Private + Domain)." 'good'
        } else {
            Verdict "Inbound rule for TCP $Port already exists." 'good'
        }

        Get-NetConnectionProfile | Where-Object { $_.NetworkCategory -eq 'Public' } | ForEach-Object {
            Set-NetConnectionProfile -InterfaceIndex $_.InterfaceIndex -NetworkCategory Private
            Verdict "Set network '$($_.Name)' to Private." 'good'
        }
    }

    Write-Host ''
    Write-Host 'Then stop using "go run" for testing the network:' -ForegroundColor Yellow
    Write-Host '    go build -o .\locallink-server.exe .\cmd\server'
    Write-Host '    .\locallink-server.exe'
    Write-Host '  (a fixed exe path keeps any per-program firewall rule valid)'
}

Write-Host ''
Write-Host 'Done.' -ForegroundColor Cyan
