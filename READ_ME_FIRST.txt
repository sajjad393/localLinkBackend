LocalLink - Browser admin console (stage 3 of 3)

Stage 1 added the admin API endpoints, stage 2 rebuilt the Flutter admin app.
This stage replaces the placeholder pages served at /admin with the full
browser console, and fixes one admin API limit that the console exposed.

Apply to your LocalLinkBackend clone
====================================
Windows PowerShell 5.1: run one line at a time. Do not paste lines with &&.

  cd "$env:USERPROFILE\Downloads\localLinkBackend"
  git pull origin main

  Expand-Archive -Path "$env:USERPROFILE\Downloads\LocalLinkBackend_admin_stage3.zip" -DestinationPath .\stage3 -Force

  git fetch ..\stage3\admin_console_stage3.bundle stage3push:stage3push
  git merge --ff-only stage3push
  git push origin main

If the merge is not a fast-forward (your main has moved), use:

  git fetch ..\stage3\admin_console_stage3.bundle stage3push:stage3push
  git merge stage3push

Only if git refuses entirely, copy the files in place instead:

  Copy-Item -Recurse -Force .\stage3\changed-files\* .

Alternative without a bundle
============================
  git am --3way .\stage3\admin_console_stage3.patch
  git push origin main

Both routes were verified to produce the identical commit tree on top of
17006b5, and the result was built and tested from a clean checkout.

Verify
======
  go build ./...
  go test ./...

Then start the server and open the console:

  $env:LOCALLINK_ENV = "production"
  $env:LOCALLINK_ADMIN_BOOTSTRAP_USERNAME = "owner"
  $env:LOCALLINK_ADMIN_BOOTSTRAP_PASSWORD = "<at least 16 characters>"
  go run ./cmd/server

  https://localhost:<port>/admin

First run creates the administrator from those two variables. Outside
development and test the bootstrap is skipped, so an empty database with no
bootstrap variables signs in as nobody.

What you get at /admin
======================
  13 modules over the endpoints stage 1 added: overview dashboard, users,
  devices, sessions, network, calls, transfers, security, audit, recovery,
  diagnostics, settings, privacy.

  Design tokens with light and dark themes, sidebar and shell, global search,
  notification centre, identity menu, responsive down to a narrow window.

  Charts are drawn from the API's own series. Sections with nothing to report
  say so instead of drawing a shape: the console never invents a trend.

  Icons are inline SVG and text is system fonts, so nothing is fetched from the
  internet and the console works on a LAN with no route out.

  Permission-aware controls, session-expiry handling, skeletons, empty and
  error states, subtle transitions.

Two things worth knowing
========================
1. Admin rate limit. Every admin request shared one ceiling of 30 per minute
   per address. A console refresh reads eight endpoints and polls, so ordinary
   use throttled itself - a browser run showed 429s within seconds. Anonymous
   probes now keep the tight 30/min cap in their own bucket; requests that
   present a credential get a 240/min budget. A throttle is shown to the
   operator, the figures already on screen stay put, polling eases off and
   recovers by itself.

2. Console assets are embedded in the binary, so upgrading the server is all
   that is needed. Unknown file paths under /admin still return 404; only
   extensionless paths fall back to the shell.

For your own checks
===================
  tools/console-render-check.mjs drives the served console in a headless DOM
  against a live server and fails on runtime errors, missing sections, broken
  menus and throttling mishandled. It found defects 2 and the menu behaviour
  that unit tests could not see.

    npm install jsdom
    node tools/console-render-check.mjs

Nothing was pushed: the commit is local to this bundle, ready for you to review
and push.
