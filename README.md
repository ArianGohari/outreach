# Outreach

Desktop-App für personalisierten E-Mail-Outreach aus Apollo-CSV-Dateien. Mit Nachrichtenvorlagen, Vorschau, Testmail und parallelen Jobs über mehrere SMTP-Konten. Gebaut mit Go, Wails und Svelte.

**[macOS ARM herunterladen](https://github.com/ArianGohari/outreach/releases/latest/download/Outreach-macos-arm64.zip)** · **[Windows x64 herunterladen](https://github.com/ArianGohari/outreach/releases/latest/download/Outreach-windows-x64.zip)**

ZIP entpacken und die App starten. Die Builds sind nicht mit einem Herausgeberzertifikat signiert; beim ersten Öffnen können Sicherheitsmeldungen erscheinen. Windows benötigt WebView2.

## Entwickeln

Voraussetzungen: Go laut [go.mod](go.mod), Node.js 24+ und die [Wails-Systemvoraussetzungen](https://wails.io/docs/gettingstarted/installation/).

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
npm --prefix frontend ci
wails dev
```

Produktvorgaben, Architektur und Versandregeln stehen in [AGENTS.md](AGENTS.md). Die aktuelle Oberfläche im Code ist die Designreferenz.

## Prüfen und bauen

```sh
npm --prefix frontend run build
npm --prefix frontend test
go test -race ./...
go vet ./...
```

Browser-Tests und weitere Testregeln: [AGENTS.md](AGENTS.md#arbeitsweise-und-tests). Auf Windows braucht der Go-Race-Test einen C-Compiler. Tests versenden keine echten Mails.

```sh
make build-mac
make build-windows
```

Auf Windows ohne Make: `wails build -platform windows/amd64`. Ergebnisse liegen in `build/bin/`. Builds nacheinander ausführen.

## Releases

`info.productVersion` in `wails.json` aktualisieren, committen und pushen. Danach den passenden Tag `vX.Y.Z` pushen. [GitHub Actions](.github/workflows/check.yml) testet beide Plattformen und veröffentlicht ZIP-Dateien mit Prüfsummen unter [Releases](https://github.com/ArianGohari/outreach/releases).

Der Duplikatschutz beruht auf der lokalen Versandhistorie. Die Datenbank nicht löschen oder zurücksetzen. Manuelle Tests auf den Zielsystemen bleiben zusätzlich zu CI erforderlich.
