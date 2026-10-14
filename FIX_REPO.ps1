# FIX_REPO.ps1 - applies the two admin stages to the LocalLink backend repo.
#
# Windows PowerShell 5.1. Run it with:
#   powershell -ExecutionPolicy Bypass -File .\FIX_REPO.ps1
#
# It finds the repository, moves anything left lying around by the earlier zip
# extractions out of the way, applies the three commits, and then builds and
# tests. Nothing is pushed; the last line tells you the push command.

[CmdletBinding()]
param(
    [string]$RepoPath = "",
    [string]$BundlePath = "",
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"

function Say($message) { Write-Host "  $message" }
function Step($message) { Write-Host ""; Write-Host $message -ForegroundColor Cyan }
function Stop-Here($message) {
    Write-Host ""
    Write-Host "STOPPED: $message" -ForegroundColor Red
    Write-Host ""
    exit 1
}

Step "1. Checking the tools"
if (-not (Get-Command git -ErrorAction SilentlyContinue)) { Stop-Here "git is not on your PATH." }
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { Stop-Here "go is not on your PATH." }
Say "git and go are available"

# Variables left over in this window make the test suite fail: the production
# tests deliberately reject LOCALLINK_REQUIRE_TLS=false, for example.
$names = @("LOCALLINK_ENV", "LOCALLINK_REQUIRE_TLS", "LOCALLINK_ADDR", "LOCALLINK_DB",
           "LOCALLINK_FILES_DIR", "LOCALLINK_TLS_CERT", "LOCALLINK_TLS_KEY",
           "LOCALLINK_ADMIN_TOKEN", "LOCALLINK_ADMIN_BOOTSTRAP_USERNAME",
           "LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD", "LOCALLINK_PAIRING_CODE")
foreach ($name in $names) {
    if (Test-Path "Env:$name") {
        Remove-Item "Env:$name" -Force
        Say "cleared `$env:$name for this run"
    }
}

Step "2. Locating the repository"
if ($RepoPath -eq "") {
    $candidates = @((Get-Location).Path, (Join-Path $env:USERPROFILE "Downloads\localLinkBackend"))
    foreach ($candidate in $candidates) {
        if ((Test-Path (Join-Path $candidate ".git")) -and (Test-Path (Join-Path $candidate "go.mod"))) {
            $RepoPath = $candidate
            break
        }
    }
}
if ($RepoPath -eq "") {
    Stop-Here "could not find the repository. Run this again with -RepoPath 'C:\Users\...\Downloads\localLinkBackend'"
}
if (-not (Test-Path (Join-Path $RepoPath "go.mod"))) { Stop-Here "$RepoPath does not look like the backend repository (no go.mod)." }
Say "repository: $RepoPath"

Step "3. Locating the fix-up bundle"
if ($BundlePath -eq "") {
    $roots = @($PSScriptRoot, (Get-Location).Path, (Join-Path $env:USERPROFILE "Downloads"))
    foreach ($root in $roots) {
        if ($root -eq "" -or -not (Test-Path $root)) { continue }
        $found = Get-ChildItem -Path $root -Filter "locallink_backend_fixup2.bundle" -Recurse -Depth 3 -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($found) { $BundlePath = $found.FullName; break }
    }
}
if ($BundlePath -eq "") {
    Stop-Here @"
could not find locallink_backend_fixup2.bundle.
The zip must be extracted somewhere OUTSIDE the repository, for example:
  Expand-Archive -Path "$env:USERPROFILE\Downloads\LocalLinkBackend_fixup_v2.zip" -DestinationPath "$env:USERPROFILE\Downloads\fixup2" -Force
Then run this script again.
"@
}
Say "bundle:    $BundlePath"

Step "4. Making sure the working tree is clean"
$status = & git -C $RepoPath status --porcelain
if ($LASTEXITCODE -ne 0) { Stop-Here "git status failed." }
if ($status) {
    Say "uncommitted changes found, saving them to a stash"
    & git -C $RepoPath stash push --include-untracked -m "autosave before fix-up"
    if ($LASTEXITCODE -ne 0) { Stop-Here "could not stash the working tree. Commit or discard your changes, then run this again." }
    Say "saved. Bring them back later with: git stash pop"
    Say "note: if you do pop it, delete any changed-files folder it restores, or go build will fail again"
} else {
    Say "working tree is clean"
}

Step "5. Updating main"
& git -C $RepoPath pull --ff-only origin main
if ($LASTEXITCODE -ne 0) { Stop-Here "git pull failed. Check 'git status' and try again." }
Say "main is up to date"

Step "6. Applying the three commits"
& git -C $RepoPath fetch $BundlePath fixall:fixall
if ($LASTEXITCODE -ne 0) { Stop-Here "could not read the bundle. Is the file readable and complete?" }
& git -C $RepoPath merge --ff-only fixall
if ($LASTEXITCODE -ne 0) {
    Write-Host ""
    Write-Host "The merge was refused, which means main has commits the bundle does not know about." -ForegroundColor Yellow
    Write-Host "Either finish with:  git -C `"$RepoPath`" merge fixall   (and resolve if asked)" -ForegroundColor Yellow
    Write-Host "or apply the patch instead:" -ForegroundColor Yellow
    Write-Host "  cd `"$RepoPath`"" -ForegroundColor Yellow
    Write-Host "  git am --3way '<folder you unzipped to>\locallink_backend_fixup2.patch'" -ForegroundColor Yellow
    exit 1
}

Step "7. Moving leftovers out of the repository"
$leftovers = @("changed-files", "READ_ME_FIRST.txt", "START_HERE.txt", "FIX_REPO.ps1",
               "admin_console_stage3.bundle", "admin_console_stage3.patch",
               "locallink_backend_fixup.bundle", "locallink_backend_fixup.patch",
               "locallink_backend_fixup2.bundle", "locallink_backend_fixup2.patch",
               "backend_fix.patch", "finish_backend_push.ps1", "locallink_backend_updates.bundle")
$parked = Join-Path (Split-Path $RepoPath -Parent) ("_cleaned_from_repo_" + (Get-Date -Format "yyyyMMdd_HHmmss"))
$moved = 0
foreach ($name in $leftovers) {
    $path = Join-Path $RepoPath $name
    if (Test-Path $path) {
        if (-not (Test-Path $parked)) { New-Item -ItemType Directory -Path $parked | Out-Null }
        Move-Item -Path $path -Destination (Join-Path $parked $name) -Force
        Say "moved $name out of the repository"
        $moved++
    }
}
if ($moved -eq 0) { Say "nothing left lying around" }
if ($moved -gt 0) { Say "they are parked in: $parked  (delete that folder whenever you like)" }

Step "8. Building and testing"
Push-Location $RepoPath
& go build ./...
if ($LASTEXITCODE -ne 0) { Pop-Location; Stop-Here "go build failed. Send me the output above." }
Say "go build ./...  OK"
if (-not $SkipTests) {
    & go test ./... -count=1
    if ($LASTEXITCODE -ne 0) { Pop-Location; Stop-Here "go test failed. Send me the output above." }
    Say "go test ./...  OK"
}
Pop-Location

Step "9. Result"
& git -C $RepoPath log --oneline -3
Say ""
Say "The console files are in place:"
foreach ($file in @("internal\httpapi\adminweb\views.js", "internal\httpapi\adminweb\charts.js", "internal\httpapi\admin_overview.go")) {
    $size = (Get-Item (Join-Path $RepoPath $file)).Length
    Say ("  {0}  {1} bytes" -f $file, $size)
}

Write-Host ""
Write-Host "Done. Nothing has been pushed yet. To publish it:" -ForegroundColor Green
Write-Host "  cd `"$RepoPath`"" -ForegroundColor Green
Write-Host "  git push origin main" -ForegroundColor Green
Write-Host ""
Write-Host "To look at the new console, start the server in its own window (a clean one," -ForegroundColor Green
Write-Host "so it does not inherit the variables this script cleared):" -ForegroundColor Green
Write-Host "  `$env:LOCALLINK_ENV = `"production`"" -ForegroundColor Green
Write-Host "  `$env:LOCALLINK_ADMIN_BOOTSTRAP_USERNAME = `"owner`"" -ForegroundColor Green
Write-Host "  `$env:LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD = `"<16 or more characters>`"" -ForegroundColor Green
Write-Host "  go run .\cmd\server" -ForegroundColor Green
Write-Host "  then open  https://localhost:8443/admin" -ForegroundColor Green
