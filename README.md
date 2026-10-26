# Outreach

Desktop app for personalized email outreach from Apollo CSV exports. Templates, previews, test emails and parallel jobs across SMTP accounts. Built with Go, Wails and Svelte.

**[Download for macOS ARM](https://github.com/ArianGohari/outreach/releases/latest/download/Outreach-macos-arm64.zip)** · **[Download for Windows x64](https://github.com/ArianGohari/outreach/releases/latest/download/Outreach-windows-x64.zip)**

Unzip and launch. Builds are not publisher-signed or notarized; security prompts may appear. Windows requires WebView2.

## Develop

Requires Go as specified in [go.mod](go.mod), Node.js 24+ and the [Wails prerequisites](https://wails.io/docs/gettingstarted/installation/).

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
npm --prefix frontend ci
wails dev
```

Read [AGENTS.md](AGENTS.md) for architecture, product decisions and sending safeguards. The current UI code is the design reference.

## Test and build

```sh
npm --prefix frontend run build
npm --prefix frontend test
go test -race -tags integration ./...
go vet -tags integration ./...
```

[Test coverage and native checks](TESTING.md). Go race tests require a C compiler. Tests send no external emails.

Build with `make build-mac` or `make build-windows`. On Windows without Make, use `wails build -platform windows/amd64`. Output: `build/bin/`. Run builds sequentially.

## Release

Update `info.productVersion` in `wails.json`, commit and push the matching `vX.Y.Z` tag. [GitHub Actions](.github/workflows/check.yml) tests both platforms and publishes ZIPs and checksums to [Releases](https://github.com/ArianGohari/outreach/releases).

Keep the local database: it preserves sending history and duplicate protection. CI does not replace manual testing on the target systems.
