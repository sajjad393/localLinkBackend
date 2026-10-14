LocalLink backend - fix-up: the two admin stages on top of your pushed main

WHAT WENT WRONG
===============
Your pushed commit "arina 0.2" (ac737dc) added the stage 3 ZIP CONTENTS as
ordinary files instead of applying them. On main today:

  - READ_ME_FIRST.txt, admin_console_stage3.bundle, admin_console_stage3.patch
    and changed-files/ sit in the repository as source files.
  - changed-files/internal/httpapi is picked up by go build ./... as a Go
    package. It has no API type in scope, so the build fails with
    "undefined: API" before it ever reaches the real package.
  - internal/httpapi/adminweb still holds the old placeholder console
    (app.js 9,953 bytes instead of 42,394).
  - None of stage 1 is present: no admin_overview.go, no procstats, and
    cmd/server/main.go registers no /api/v1/admin/analytics|health|
    security-summary|activity route. The console needs all of them.

This fix-up replaces that commit with what it was meant to be.

WHAT YOU GET
============
Three commits on top of ac737dc:
  1. the stage 1 backend (analytics rollups, live health, security summary,
     activity feed, metrics sampler)
  2. the stage 3 browser console (go:embed assets, endpoint guards, the
     rate-limit split and the render-check tool)
  3. removal of the README, bundle, patch and changed-files committed by
     mistake, which is what un-breaks go build ./...

Verified before packaging: go build ./... and go test ./... green (5/5
packages) on a clone of your repository with these three commits applied, and
every console file byte-identical to the tested build.

APPLY
=====
Windows PowerShell 5.1: one command per line, no &&.

  cd "$env:USERPROFILE\Downloads\localLinkBackend"
  git status
  git pull origin main

Check git status is clean first. Then:

  Expand-Archive -Path "$env:USERPROFILE\Downloads\LocalLinkBackend_admin_fixup.zip" -DestinationPath .\fixup -Force
  git fetch ..\fixup\locallink_backend_fixup.bundle allstages:fixup
  git merge --ff-only fixup
  go build ./...
  go test ./...
  git push origin main

Your main is ac737dc, which is what the bundle is built on, so the merge is a
fast-forward: the three commits land directly on top.

If the merge refuses, you have local commits on main. Push or park them first,
or fall back to:

  git am --3way .\fixup\locallink_backend_fixup.patch

FALLBACK: copy the files instead
================================
Only if git will not cooperate:

  Copy-Item -Recurse -Force .\fixup\changed-files\* .
  git rm -r --cached changed-files admin_console_stage3.bundle admin_console_stage3.patch READ_ME_FIRST.txt
  Remove-Item -Recurse -Force changed-files, admin_console_stage3.bundle, admin_console_stage3.patch, READ_ME_FIRST.txt
  go build ./...

The removal lines are not optional: leaving changed-files/ in place keeps
go build ./... failing.

AFTER APPLYING
==============
The console files are embedded in the server binary, so the only thing needed
to see the new one is a rebuild. To drive it in a headless browser:

  Expand-Archive -Path "$env:USERPROFILE\Downloads\LocalLinkBackend_admin_stage3.zip" -DestinationPath .\stage3 -Force
  npm install jsdom
  node .\stage3\changed-files\tools\console-render-check.mjs

It expects a running server and a bootstrap administrator:

  $env:LOCALLINK_ENV = "production"
  $env:LOCALLINK_ADMIN_BOOTSTRAP_USERNAME = "owner"
  $env:LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD = "<16+ characters>"

Outside development and test the bootstrap is skipped, so without those two
variables a fresh database has no administrator and /admin signs in as nobody.

Two more things worth knowing, both unchanged by this fix-up:

  - go build ./... fails on main right now for the reason above; it passes once
    commit 3 lands. Nothing else in the tree was touched.
  - data/locallink.db is tracked in the repository. It was written to when the
    server ran, so every start-up shows it as modified. Adding data/*.db to
    .gitignore, or keeping the file out of the repository, saves that churn.

Nothing was pushed by me: the three commits are local to this bundle.
