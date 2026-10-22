# Bundle 88 (backend) — devices dedupe + server-backed contacts

Base: backend `375debf`. Files: internal/httpapi/{auth.go, api.go, restoration.go, contacts.go (new), reinstall_dedupe_contacts_test.go (new)}, internal/store/store.go, cmd/server/main.go.

## Apply (Linux server)
```bash
cd ~/LocalLinkBackend          # your backend repo
cp -r internal internal.bak88 && cp -r cmd cmd.bak88
unzip -o ~/Downloads/locallink_backend_fixes88_2026-10-22.zip -d .
go build ./...
go test ./...
# restart the server process (DB migration is automatic on boot):
#  - adds devices.hardware_fp column + index
#  - creates account_contacts table
sudo systemctl restart locallink   # or however your server process is run
```

## One-time cleanup of CURRENT duplicates
The two stale rows ("Sajjad phone" offline since Oct 15, and "Sumaira phone")
are orphans whose app credentials are gone forever. Safe cleanup: open the
admin panel > Devices > **Revoke** those two. History stays restorable
(restore queries join on user_id, retained even when revoked). Do NOT revoke
"My Android Phone (*)" rows — those are the current devices.

Suggested commit:
fix devices: bind reinstalled logins to the same hardware fingerprint instead of duplicating device rows; server-backed account contacts (CRUD + restore) and tests
