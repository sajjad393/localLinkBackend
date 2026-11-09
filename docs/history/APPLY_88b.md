# Bundle 88b - hermetic env for TestValidateProductionSecretsRequiresPairingCode
```powershell
cd "C:\Users\sajjad Ahmed\Downloads\localLinkBackend"
Expand-Archive -LiteralPath "$env:USERPROFILE\Downloads\locallink_backend_testfix88b.zip" -DestinationPath . -Force
go test ./...
git add -A; git commit -m "test: pin LOCALLINK_REQUIRE_TLS in prod-secrets test so dev shells with TLS=false do not fail it"; git push origin main
```
