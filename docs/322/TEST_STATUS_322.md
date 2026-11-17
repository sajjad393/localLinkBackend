# Test results — Bundle 322 (final, before the zip)

Run in this sandbox against the exact tree that is inside the zip.

| Suite | Command | Result |
|---|---|---|
| App (whole project) | `flutter test` | **3425 passed, 0 failed** (12 m 06 s), exit 0 |
| Backend (whole project) | `go test ./...` | **7 / 7 packages ok**, exit 0 |
| Backend vet | `go vet ./...` | clean |
| 322 app: files card | `flutter test test/bundle322_files_test.dart` | 8 / 8 |
| 322 app: card lifecycle | `flutter test test/bundle322_card_test.dart` | 9 / 9 |
| 322 backend: version rule | `go test ./internal/httpapi -run TestVersionRule322` | 3 / 3 |
| 322 backend: web page | `go test ./internal/httpapi -run "TestAdminWeb(Smoke322\|CSS322\|322StateLine)"` | 3 / 3 |
| 322 web smoke (real views.js in node) | `node internal/httpapi/testdata/appversion_322_smoke.js` | `SMOKE_OK 322 appversion: 6 server states` |
| 322 real-page overlap audit | `python3 tools/real_page_audit.py` | **PASS 84 / FAIL 0** (6 states x 7 widths x 2 themes) |
| 322 other panel pages | `python3 tools/audit_all_views.py` | **PASS 66 / FAIL 0** (11 pages x 3 widths x 2 themes) |
| Untouched 317-321 suites | inside the full app run | all pass, no test edited |

Version after the bump: app `3.0.113+184`, backend 322.
Patches verified: `git apply --check` on untouched clones of `faf9080` (app) and `54909a9` (backend).
