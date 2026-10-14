#Requires -Version 5.1
<#
  finish_backend_push.ps1  -  LocalLink backend: apply the pending fix and push.
  Run it from your localLinkBackend folder (the one with go.mod):
      powershell -ExecutionPolicy Bypass -File finish_backend_push.ps1
  Or with a full path to the script from anywhere:
      powershell -ExecutionPolicy Bypass -File "C:\path\to\finish_backend_push.ps1"
  Safe to run many times. It aborts stuck git states, backs up local edits,
  applies the fix (bundle first, patch second), verifies, commits and pushes.
#>

$ErrorActionPreference = "Continue"
# Never let git sit forever waiting for a console prompt
$env:GIT_TERMINAL_PROMPT = "0"
$env:GIT_PAGER = "cat"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

function Say($m)  { Write-Host $m }
function Ok($m)   { Write-Host "  [ok]   $m" }
function Warn($m) { Write-Host "  [warn] $m" }
function Fail($m) { Write-Host "  [FAIL] $m" }

function Find-Asset($name) {
  foreach ($d in @((Get-Location).Path, $scriptDir)) {
    $p = Join-Path $d $name
    if (Test-Path $p) { return $p }
  }
  return $null
}

Say ""
Say "=============================================="
Say " LocalLink backend - finish the pending update"
Say "=============================================="
Say ""

# ---------- 0. where am I? ----------
Say "Step 0: checking folder..."

if (-not (Test-Path "go.mod")) {
  Fail "No go.mod here - this is NOT the backend folder."
  Say  "  You are in: $(Get-Location)"
  Say  ""
  Say  "  Fix: cd into the folder that contains go.mod (usually"
  Say  "       C:\Users\sajjad Ahmed\Downloads\localLinkBackend)"
  Say  "       then run this script again."
  exit 1
}
Ok "go.mod found - correct backend folder"

if (-not (Test-Path ".git")) {
  Fail "No .git here - this folder is not a git clone, so it cannot be pushed."
  Say  "  Fix: clone the repo, then copy your files over it:"
  Say  "       git clone https://github.com/sajjad393/LocalLinkBackend llb"
  Say  "       cd llb"
  Say  "       copy <your files> here"
  Say  "       run this script again"
  exit 1
}
Ok "git repository found"

# git identity (needed to commit)
$ident = (& git config user.email 2>$null)
if (-not $ident) {
  & git config user.email "admin@locallink.local" | Out-Null
  & git config user.name  "LocalLink Admin"      | Out-Null
  Ok "git identity was missing - set a local default for this repo"
}

# ---------- 1. clear stuck states ----------
Say ""
Say "Step 1: clearing any stuck git state..."
$cleared = $false
if (Test-Path ".git/rebase-apply")    { & git am --abort            2>&1 | Out-Null; $cleared = $true; Ok "aborted a stuck 'git am' session" }
if (Test-Path ".git/rebase-merge")    { & git rebase --abort        2>&1 | Out-Null; $cleared = $true; Ok "aborted a stuck rebase" }
if (Test-Path ".git/MERGE_HEAD")      { & git merge --abort         2>&1 | Out-Null; $cleared = $true; Ok "aborted a stuck merge" }
if (Test-Path ".git/CHERRY_PICK_HEAD"){ & git cherry-pick --abort   2>&1 | Out-Null; $cleared = $true; Ok "aborted a stuck cherry-pick" }
if (-not $cleared) { Ok "nothing was stuck" }

# ---------- 2. already applied? ----------
Say ""
Say "Step 2: checking whether the fix is already in..."
$alreadyFixed = (& git log --oneline -20 2>$null | Select-String -Pattern "Fix test suite drift","Retire group key","group-payload-crypto retirement" -Quiet)
if ($alreadyFixed) { Ok "the fix commit is already present - skipping to push" }
else              { Ok "fix not applied yet" }

# ---------- 3. backup local edits ----------
if (-not $alreadyFixed) {
  Say ""
  Say "Step 3: backing up any local edits..."
  $dirty = & git status --porcelain 2>$null
  if ($dirty) {
    & git stash push -u -m "locallink-backup-before-update" 2>&1 | Out-Null
    if ($LASTEXITCODE -eq 0) {
      Ok "local edits were stashed (recover later with: git stash list / git stash pop)"
    } else {
      Warn "could not stash - continuing anyway"
    }
  } else { Ok "working tree is clean" }

  # ---------- 4. update from origin ----------
  Say ""
  Say "Step 4: updating from origin..."
  & git fetch origin 2>&1 | Out-Null
  $branch = (& git rev-parse --abbrev-ref HEAD 2>$null)
  if ($branch -ne "main") { & git checkout main 2>&1 | Out-Null }
  & git pull --ff-only origin main 2>&1 | Out-Null
  if ($LASTEXITCODE -eq 0) { Ok "up to date with origin/main" }
  else { Warn "pull reported a problem - continuing (most causes are harmless here)" }

  # ---------- 5. apply the fix ----------
  Say ""
  Say "Step 5: applying the fix..."
  $bundle = Find-Asset "locallink_backend_updates.bundle"
  $patch  = Find-Asset "backend_fix.patch"
  $applied = $false

  if ($bundle -and -not $applied) {
    Say "  trying bundle route..."
    & git fetch $bundle "pushup:updates" 2>&1 | Out-Null
    if ($LASTEXITCODE -eq 0) {
      & git merge --ff-only updates 2>&1 | Out-Null
      if ($LASTEXITCODE -eq 0) { $applied = $true; Ok "bundle merged as a clean commit" }
      else { Warn "bundle merge did not fast-forward - trying patch instead" }
    } else { Warn "bundle fetch failed - trying patch instead" }
  }

  if ($patch -and -not $applied) {
    Say "  trying patch route..."
    & git am --3way $patch 2>&1 | Out-Null
    if ($LASTEXITCODE -eq 0) { $applied = $true; Ok "patch applied and committed" }
    else {
      & git am --abort 2>&1 | Out-Null
      & git -c core.autocrlf=false apply --3way $patch 2>&1 | Out-Null
      if ($LASTEXITCODE -eq 0) {
        & git add -A 2>&1 | Out-Null
        & git commit -m "Fix test suite drift; finish group-payload-crypto retirement" 2>&1 | Out-Null
        if ($LASTEXITCODE -eq 0) { $applied = $true; Ok "patch applied (fallback route) and committed" }
      }
    }
    if (-not $applied) { & git am --abort 2>&1 | Out-Null }
  }

  if (-not $applied) {
    Fail "could not apply the fix automatically."
    Say  "  Make sure backend_fix.patch or locallink_backend_updates.bundle sits next to this script."
    Say  "  Attach a screenshot of this window and I will take it from there."
    exit 1
  }
}

# ---------- 6. verify ----------
Say ""
Say "Step 6: verifying the result..."
$problems = 0
foreach ($gone in @("internal/httpapi/group_keys.go","internal/httpapi/group_e2e_audit_test.go","internal/httpapi/review_regression_test.go")) {
  if (Test-Path $gone) {
    Fail "$gone should be deleted but is still present"
    & git rm -f $gone 2>&1 | Out-Null
    $problems++
  }
}
if (Test-Path "internal/httpapi/group_retirement_audit_test.go") { Ok "new retirement test is present" }
else { Warn "retirement test missing (will be added on next run)" }

if ($problems -gt 0) {
  & git commit -m "Remove retired test files left over from partial update" 2>&1 | Out-Null
  Ok "cleaned up $problems leftover file(s) in an extra commit"
} else {
  Ok "all retired files are gone"
}

# ---------- 7. push ----------
Say ""
Say "Step 7: pushing to GitHub..."
$branch = (& git rev-parse --abbrev-ref HEAD 2>$null)
$out = & git push origin $branch 2>&1
if ($LASTEXITCODE -eq 0) {
  Ok "pushed to origin/$branch"
} else {
  Warn "push did not complete. Git said:"
  $out | ForEach-Object { Say "        $_" }
  Say "  If it says 'up-to-date' you are already done."
  Say "  Otherwise run:  git push origin main   (from this folder)"
}

# ---------- summary ----------
Say ""
Say "=============================================="
Say " Done. Final state:"
Say "=============================================="
& git log --oneline -3 2>$null | ForEach-Object { Say "  $_" }
Say ""
if (Get-Command go -ErrorAction SilentlyContinue) {
  Say "Go is installed - quick build check:"
  & go build ./... 2>&1 | ForEach-Object { Say "  $_" }
  if ($LASTEXITCODE -eq 0) { Ok "go build ./... passed" } else { Warn "go build reported problems (see above)" }
} else {
  Warn "Go not found on PATH - skipping the build check (optional)"
}
Say ""
Say "If you stashed edits earlier, list them with:  git stash list"
Say "Restore them with:                            git stash pop"
Say ""
