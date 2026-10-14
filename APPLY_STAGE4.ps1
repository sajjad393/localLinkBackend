# =============================================================================
# LocalLink admin - repair the repository and add conversation review
#
# What this script does, in order:
#   1. Copies every file in the payload folder to its real path inside
#      your repository (the new admin console, its backend, and the new
#      owner-only Conversations module).
#   2. Moves the leftover changed-files folder out of the repository. That
#      folder is what made "go test" fail with "api.AdminAnalytics undefined".
#      It is moved one level up so nothing is destroyed.
#   3. Deletes the nine helper files that earlier deliveries left in the
#      repository (readme texts and .bundle/.patch artifacts).
#   4. Clears the LOCALLINK_REQUIRE_TLS variable in this window, which once
#      made a test fail for the wrong reason.
#   5. Builds and tests the result and prints a clear PASS or FAIL report.
# It does not commit and it does not push - those are the final two lines it
# prints for you to run yourself when the report says PASS.
#
# How to run it:
#   1. Extract LocalLink_Admin_Stage4.zip anywhere (right-click, Extract All).
#   2. Open Windows PowerShell inside the extracted folder.
#   3. .\APPLY_STAGE4.ps1
# If your repository is not in Downloads, pass it:
#   .\APPLY_STAGE4.ps1 -Repo "D:\work\localLinkBackend"
# =============================================================================

param(
    [string]$Repo = "$env:USERPROFILE\Downloads\localLinkBackend"
)

$ErrorActionPreference = "Stop"
$failures = New-Object System.Collections.Generic.List[string]

Write-Host ""
Write-Host "LocalLink admin repair + conversation review (stage 4)" -ForegroundColor Cyan
Write-Host ("  PowerShell " + $PSVersionTable.PSVersion.ToString())
Write-Host ""

# ------------------------------------------------------------------ 0. locate
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$payload = Join-Path $scriptDir "payload"
if (-not (Test-Path $payload)) {
    Write-Host "FAIL" -ForegroundColor Red
    Write-Host ("  The payload folder was not found next to this script: " + $payload)
    Write-Host "  Extract the whole zip first, then run the script from inside the extracted folder."
    exit 1
}
$Repo = (Resolve-Path -LiteralPath $Repo -ErrorAction SilentlyContinue).Path
if (-not $Repo) {
    Write-Host "FAIL" -ForegroundColor Red
    Write-Host "  The repository folder does not exist at the expected place:"
    Write-Host ("  " + "$env:USERPROFILE\Downloads\localLinkBackend")
    Write-Host "  Run the script again with -Repo and the real path."
    exit 1
}
if ((-not (Test-Path (Join-Path $Repo "go.mod"))) -or (-not (Test-Path (Join-Path $Repo ".git")))) {
    Write-Host "FAIL" -ForegroundColor Red
    Write-Host ("  This does not look like the localLinkBackend repository: " + $Repo)
    Write-Host "  It should contain go.mod and a .git folder."
    exit 1
}
Write-Host ("  Repository: " + $Repo)

# ---------------------------------------------------------- 1. copy the files
$copied = 0
$mirrors = Get-ChildItem -Recurse -File -Force -LiteralPath $payload
foreach ($file in $mirrors) {
    $relative = $file.FullName.Substring($payload.Length + 1)
    $destination = Join-Path $Repo $relative
    New-Item -ItemType Directory -Force -Path (Split-Path $destination) | Out-Null
    Copy-Item -Force -LiteralPath $file.FullName -Destination $destination
    $copied += 1
}
Write-Host ("  Copied " + $copied + " file(s) into the repository.")
$expected = ($mirrors | Measure-Object).Count
if ($copied -ne $expected) { $failures.Add("copy: expected $expected files, copied $copied") }

# --------------------------------------------- 2. move the mirror folder out
$mirror = Join-Path $Repo "changed-files"
if (Test-Path $mirror) {
    $out = Join-Path (Split-Path $Repo -Parent) "changed-files-out-of-repo"
    if (Test-Path $out) {
        Remove-Item -Recurse -Force -LiteralPath $out
    }
    Move-Item -Force -LiteralPath $mirror -Destination $out
    Write-Host ("  Moved changed-files out of the repository -> " + $out)
} else {
    Write-Host "  No changed-files folder present (nothing to move)."
}

# ----------------------------------------------------------- 3. delete strays
$strays = @(
    "FIX_REPO.ps1",
    "READ_ME_FIRST.txt",
    "START_HERE.txt",
    "admin_console_stage3.bundle",
    "admin_console_stage3.patch",
    "locallink_backend_fixup.bundle",
    "locallink_backend_fixup.patch",
    "locallink_backend_fixup2.bundle",
    "locallink_backend_fixup2.patch"
)
$removed = 0
foreach ($name in $strays) {
    $target = Join-Path $Repo $name
    if (Test-Path $target) {
        Remove-Item -Force -LiteralPath $target
        $removed += 1
    }
}
Write-Host ("  Removed " + $removed + " leftover delivery artifact(s).")

# ----------------------------------------------------- 4. clear the env trap
Remove-Item Env:LOCALLINK_REQUIRE_TLS -ErrorAction SilentlyContinue

# ----------------------------------------------------------------- 5. verify
Push-Location $Repo

Write-Host ""
Write-Host "  Console assets in internal\httpapi\adminweb:" -ForegroundColor Cyan
Get-ChildItem (Join-Path $Repo "internal\httpapi\adminweb") |
    Format-Table Name, Length -AutoSize | Out-String -Width 200 | Write-Host
$assetCount = (Get-ChildItem (Join-Path $Repo "internal\httpapi\adminweb") -File).Count
if ($assetCount -ne 6) { $failures.Add("console assets: expected 6 files, found $assetCount") }

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    $failures.Add("go is not on PATH in this window - install Go or open a window where it is")
    Write-Host "  Go toolchain not found on PATH." -ForegroundColor Yellow
} else {
    Write-Host "  go build ./..." -ForegroundColor Cyan
    & go build ./... 2>&1 | ForEach-Object { Write-Host ("    " + $_) }
    if ($LASTEXITCODE -eq 0) {
        Write-Host "    build OK (no output above is a pass)" -ForegroundColor Green
    } else {
        $failures.Add("go build ./... exited with code $LASTEXITCODE")
    }

    Write-Host ""
    Write-Host "  go test ./..." -ForegroundColor Cyan
    & go test ./... 2>&1 | ForEach-Object { Write-Host ("    " + $_) }
    if ($LASTEXITCODE -eq 0) {
        Write-Host "    tests OK" -ForegroundColor Green
    } else {
        $failures.Add("go test ./... exited with code $LASTEXITCODE")
    }
}

Pop-Location

# ----------------------------------------------------------------- 6. report
Write-Host ""
if ($failures.Count -eq 0) {
    Write-Host "PASS - the repository now holds the finished, tested build." -ForegroundColor Green
    Write-Host ""
    Write-Host "To publish it, run these two lines inside the repository:"
    Write-Host ""
    Write-Host '    git add -A'
    Write-Host '    git commit -m "Admin console: conversation review for the owner (stage 4)"'
    Write-Host '    git push origin main'
    Write-Host ""
    Write-Host "The commit will include any changes to data\locallink.db from your runs;"
    Write-Host "that is expected on this repository and has no effect on the fix."
    Write-Host ""
    Write-Host "Then stop the old server before restarting it, or it will keep the old panel:"
    Write-Host ""
    Write-Host "    netstat -ano | findstr :8090"
    Write-Host "    Stop-Process -Id <PID from that line> -Force"
    Write-Host ""
    Write-Host "Quick check on the new panel: the sidebar has 13 items, including a new"
    Write-Host "Conversations module (speech-bubble icon) under Trust & Compliance."
    exit 0
} else {
    Write-Host "FAIL - copy this window's output and send it:" -ForegroundColor Red
    foreach ($problem in $failures) { Write-Host ("  - " + $problem) -ForegroundColor Red }
    exit 1
}
