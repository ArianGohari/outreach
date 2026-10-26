# Testing

Tests use synthetic contacts, temporary databases and loopback SMTP servers. No external mail is sent. The OS credential test creates and deletes its own uniquely named entry.

## Run

```sh
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend run build
pnpm --dir frontend test
go test -race -tags integration '-coverpkg=./...' '-coverprofile=coverage.out' ./...
go vet -tags integration ./...
go test ./internal/outreach -run '^$' -fuzz '^FuzzCSVNeverPanics$' -fuzztime=10s
node scripts/native-e2e.mjs
```

Run browser tests from `frontend`: `pnpm exec playwright install chromium webkit`, then `pnpm run test:e2e`.

For the real Keychain / Windows Credential Manager test, set `OUTREACH_OS_TESTS=1` and run `go test -tags integration -run TestOperatingSystemCredentialRoundTrip .`. In PowerShell, set it with `$env:OUTREACH_OS_TESTS='1'`.

`make test` runs the build, component tests and Go tests. `make test-native` also runs the credential and native WebView tests. Native tests require an interactive desktop and open a temporary test window. Build the frontend before compiling Go; do not run those builds concurrently.

## Coverage

| Feature / failure | Automated checks |
|---|---|
| Apollo structure, aliases, encoding, malformed rows, size limits | CSV unit tests and fuzzing |
| Verification filter, person matching, transitive duplicates | CSV and history tests |
| Tags, fallbacks, missing fields, injection, Unicode, plain text | Template and MIME tests |
| Jane Doe preview; preview, test and delivery use the same content | Session, component, browser and native tests |
| Approval invalidation, foreign job/token, example data cannot authorize sending | Session, jobs, desktop and UI tests |
| TLS, STARTTLS, certificate rejection, authentication, SMTP rejection, lost acknowledgement | Local SMTP integration tests |
| Durable reservations, repeat starts/imports, sender changes, parallel jobs | Store, engine and jobs tests with the Go race detector |
| Crash after SMTP DATA but before acknowledgement | Child process is forcibly killed; reopen SQLite and verify no recipient is sent twice |
| Batch limits, pauses, stop, restart, elapsed deadlines and clock changes | Engine tests with a controlled clock |
| One active job per sender; other senders run independently | Concurrent jobs tests |
| Draft persistence, secret removal, legacy migration, empty drafts | Jobs and desktop integration tests |
| Delete jobs while retaining history and duplicate protection | Jobs, UI and native tests |
| Dashboard, tabs, account reuse, defaults, hours conversion, keyboard and narrow layouts | Component tests and Chromium/WebKit browser tests |
| Credentials, cancellation, confirmation, close while sending | Desktop unit tests; actual OS credential round trip |
| Error details, Wails serialization, log receipts, redaction, rotation and write failures | Diagnostics, desktop, component and native tests |
| Actual Svelte → Wails → SQLite → TLS SMTP flow | Native WKWebView / WebView2 test, including errors, retry and deletion |
| Google OAuth, PKCE/state, account identity, denied consent and secret storage | Local OAuth endpoint tests and desktop/UI tests |
| Google refresh, revoked grants, API errors, lost acknowledgement and no retries | Concurrent token tests and local Gmail API tests |
| Google sends and deletion retain global duplicate protection | Jobs → SQLite → local Gmail API integration test |

The native test builds a separate executable with `integration,nativee2e` tags and the race detector. It uses the production window options and bindings. File-picker and confirmation results are scripted; the WebView, bridge, file reads, validation, database, worker and SMTP transport are real. Test hooks and the SMTP fixture are excluded from release builds.

CI runs the suites, native app test, credential test and release build on macOS ARM and Windows x64. Browser tests mock the backend; they are not evidence of SMTP delivery. Coverage percentages identify unexecuted statements, not a guarantee that every possible failure is tested.

## Target-device checks

These still require manual testing of a downloaded build on a clean macOS ARM machine and **Windows 11 Home**. The Windows CI runner uses Windows Server.

- Extract and launch the ZIP, including unsigned-app prompts and WebView2 installation.
- Select and cancel files using the actual OS picker; confirm and cancel sending and quitting.
- Save, reload and remove a synthetic credential; check behavior when the credential store is locked or access is denied.
- Sleep/wake, network loss and resume during a batch pause; verify pending contacts and durable duplicate protection using a local SMTP sink.
- Relaunch after forced termination and a normal OS shutdown; verify drafts, history and manual reapproval.

Do not mark these checks as passed from a green CI run. Never use a real contact list or delete the user's database for testing.

Google's actual consent screen, Workspace policy and production delivery need separate verification. The opt-in `google-check` command in [GOOGLE.md](GOOGLE.md) checks real authorization without sending mail; automated suites never contact Google.
