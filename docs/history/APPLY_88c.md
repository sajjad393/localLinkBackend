# Bundle 88c - register endpoint accepts hardware_fp (fixes 400 "invalid request body" on new account creation)

cause: decodeJSON uses DisallowUnknownFields; bundle 88 app started sending hardware_fp on register too, but only the LOGIN struct got the field.

```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
Expand-Archive -LiteralPath "$env:USERPROFILE\Downloads\locallink_backend_fixes88c.zip" -DestinationPath . -Force -Verbose
go build ./...
go test ./...
git add -A; git commit -m "fix register: accept and persist hardware_fp on account creation (unknown-field decoder rejected new app payload with 400)"; git push origin main
```
restart the server, then retry Create account on both phones.
