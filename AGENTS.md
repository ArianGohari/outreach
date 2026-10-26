# Outreach: Vorgaben für die Weiterentwicklung

Gilt für das gesamte Repository. Vor Änderungen diese Datei und `README.md` lesen. Die Regeln sind die bisher vereinbarten Produktvorgaben; sie dürfen nicht beiläufig durch Refactoring geändert werden.

## Produkt und Umfang

Lokale Desktop-App: Apollo-CSV importieren, Textnachrichten mit Platzhaltern vorbereiten, Vorschau prüfen, Testmail senden, anschließend kontrolliert über SMTP versenden. Nutzung auf einem Rechner; kein Cloud-Backend und keine Synchronisierung zwischen Rechnern.

- Go und Wails 2, Svelte 5 mit TypeScript, Vite, SQLite. Versionen aus `go.mod` und `frontend/pnpm-lock.yaml` verwenden.
- Zielsysteme: macOS auf Apple Silicon (ARM64, deklarierte Mindestversion 13) und Windows 11 Home x64. Kein Linux-Produkt und kein Intel-Mac-Build vereinbart.
- pnpm statt npm verwenden; Version in `frontend/package.json` festlegen. Nur `pnpm-lock.yaml` committen; CI und Wails installieren mit `--frozen-lockfile`.
- Die bisherige Schwarz-Weiß-Palette exakt erhalten, inklusive bestehender Statusfarben. Keine neue Akzentfarbe. Die Bildmarke ersetzt das erste o der Wortmarke; kein separates Icon daneben.
- Geist lokal aus `frontend/public/fonts` laden; Lizenz mitliefern. Farben und Typografie zentral in `frontend/src/style.css`, Markenassets in `frontend/public/brand`. Icons mit `pnpm --dir frontend brand:build` aus SVG erzeugen.
- Bestehendes CSS weiterverwenden. Falls eine CSS-Library nötig wird, ist UnoCSS die vereinbarte Option; nicht vorsorglich installieren.
- Kein Postfachzugriff, kein Einlesen von Antworten oder Bounces, kein Tracking. Ein vorhandenes CSV-Feld zu Bounces auszuwerten ist kein Postfachzugriff.
- SMTP nur mit TLS/STARTTLS. Keine automatische Signatur: Eine Signatur steht nur dann in der Mail, wenn sie im Template eingetragen wurde.
- Google / Workspace nutzt OAuth und die Gmail-API (`gmail.send`, `openid`, `email`), andere Anbieter SMTP. Kein Postfach-Lesezugriff. Einrichtung in `GOOGLE.md`. Offizielle Builds enthalten den gemeinsamen Desktop-OAuth-Client, damit Nutzer direkt „Connect Google“ wählen können. `scripts/configure-google.mjs` erzeugt die ignorierte `internal/googlemail/client_build.go` aus `GOOGLE_OAUTH_CLIENT_JSON`; die originale JSON-Datei nie committen. Nur Desktop-Client-ID und -Secret einbetten, niemals Benutzer-Tokens. Bestehende Grants behalten den zugehörigen OAuth-Client; nicht auf den neuen Build-Client umschreiben.
- Google-Konto aus der bestätigten OAuth-Identität übernehmen. Absender muss diesem Konto entsprechen; Aliase sind vorerst nicht unterstützt. Tokens bleiben im Backend: Refresh-Zugangsdaten im OS-Schlüsselbund, Access-Tokens nur im RAM. SQLite speichert eine nicht geheime Verbindungsreferenz.
- Gmail-Sendeanfragen niemals automatisch wiederholen, auch nicht bei HTTP 401/429/5xx. Fehlende/ungültige Erfolgsbestätigung und 5xx bleiben unklar und gesperrt. Token-Erneuerung vor dem Versand ist erlaubt und verändert keine Versandfreigabe.
- Trennen einer Google-Verbindung mit Jobstarts serialisieren und Freigaben betroffener Jobs löschen. Laufende Jobs blockieren das Trennen. Lokales Trennen widerruft keine Freigabe bei Google.
- Die optionale Antwortadresse ist ein Reply-To-Header. Ohne sie gehen Antworten an den Absender.
- Vorerst keine Developer-ID-Signierung, Notarisierung oder Windows-Code-Signierung. Lokale Ad-hoc-Signierung durch Wails ist erlaubt.

## Versandregeln: nicht abschwächen

Fehlerhafte Empfänger, falsch gerenderte Nachrichten und Doppelversand sind zentrale Risiken. Diese Prüfungen gehören ins Go-Backend; deaktivierte UI-Buttons allein reichen nicht.

1. CSV-Struktur vollständig prüfen. Ungültige Struktur darf nicht teilweise importiert werden. UTF-8 mit optionalem BOM, Komma als Trennzeichen; die genaue Erkennung und erlaubte Header-Aliase stehen in `csv.go`.
2. Empfänger kommen aus der importierten Backend-Session. Das Frontend darf sie nicht durch eine eigene Liste ersetzen.
3. Vor Versand sämtliche Nachrichten rendern und validieren: Pflichtwerte, unbekannte Tags, Header-Injection, Betreff, MIME-Inhalt und Zieladressen. Vorschau, Testmail und Versand müssen denselben geprüften Inhalt verwenden.
4. SMTP-Prüfung und eine gültige Testmail-Freigabe sind an Job und Einstellungen gebunden. Änderungen machen die Freigabe ungültig. Beispieldaten dürfen niemals Versand freigeben.
5. Empfänger vor jedem SMTP-Versuch dauerhaft in SQLite reservieren. Datenbankfehler verhindern den Versand. Prozess-Lock, Transaktionen, WAL und synchrone Speicherung erhalten.
6. Identische gerenderte Nachrichten an dieselbe erkannte Person/Adresse bleiben über Jobs und Absender hinweg gesperrt. Nicht automatisch wiederholen, auch nicht nach Fehlern, Neustarts oder erneutem Import. Nach unklarer SMTP-Annahme den Eintrag sperren; keine Retry-Schleife ergänzen.
7. Nach einem Versandfehler pausiert der Job. Stoppen verhindert die nächste Übertragung, eine laufende Übertragung darf noch abschließen. Beim Neustart keine automatische Fortsetzung: Verbindung und Testmail erneut prüfen.
8. Job löschen entfernt die Arbeitsansicht, aber keine Versandhistorie oder Duplikatsperren. Laufende oder anderweitig belegte Jobs dürfen nicht gelöscht werden. Das Löschen des migrierten Legacy-Jobs darf ihn beim nächsten Start nicht wiederherstellen.

Der vorhandene Schutz ist lokal und gilt für identischen Nachrichteninhalt. Geänderter Inhalt ist derzeit eine neue Nachricht. Personengleichheit wird über Adresse, Apollo-ID oder LinkedIn-Kennung erkannt, nicht allein über Namen. Verschiedene Adressen ohne gemeinsame Kennung sind nicht zuverlässig zuordenbar. Diese Grenzen nicht als garantierten Schutz für alle Personen oder Rechner darstellen und nicht stillschweigend umdefinieren.

## Jobs und Oberfläche

- Die gesamte App ist auf Englisch: Oberfläche, Hilfetexte, eigene Dialoge, Validierungsfehler und Diagnosen. Keine Sprachauswahl nötig. Vorhandene Benutzereingaben und Versandhistorie nicht übersetzen oder verändern.
- Mehrere Jobs und Absender; höchstens ein gleichzeitig laufender Job je normalisierter Absenderadresse. Das gilt auch während Batchpausen. Andere Absender dürfen parallel laufen.
- Jobs derselben Adresse dürfen vorbereitet werden; sie starten nach Freigabe manuell.
- Dashboard und Job-Tabs. Neue Jobs über das Plus in der Tab-Leiste, kein zusätzlicher "Neuer Job"-Button.
- Einen eingerichteten Tab schließen beendet oder löscht den Job nicht. Unberührte leere Entwürfe nicht im Dashboard anzeigen; beim Schließen bzw. Start bereinigen. Keine Benutzereingaben dabei verwerfen.
- Löschen im Dashboard braucht eine kurze Bestätigung. Fehler beim Speichern oder Löschen sichtbar melden; keinen Erfolg vortäuschen.
- Neue Jobs: 30 Sekunden zwischen Mails, Batchgröße 50, Batchpause 24 Stunden. Bestehende Einstellungen erhalten. UI in Stunden, gespeicherte Werte weiterhin in Minuten; Plan-IDs dürfen durch Einheitenwechsel nicht verändert werden.
- Der aktuelle Code in `frontend/src` ist die Designreferenz. `Outreach.html` ist ein historischer, lokal ignorierter Entwurf (bei Bedarf in der Git-Historie). Daraus keine alten Checkboxen, Hinweise oder Layouts wieder übernehmen.
- Kontakte immer deduplizieren, keine "Ohne Duplikate"-Checkbox. "Nur verifizierte" bleibt ein echter Filter. Fehlt der Verifizierungsstatus, muss er für den Import ausdrücklich deaktiviert werden.
- Kein festes Dropdown "E-Mail-Spalte"; Apollo `Email` ist die Empfängerspalte. Die Kontaktvorschau zeigt alle importierten Spalten.
- UI-Texte auf ihren Nutzen prüfen. Überflüssige Einleitungen entfernen; hilfreiche Hinweise kürzen. Aussagekräftige Fehler, Versandgrenzen und die Template-Anleitung erhalten.
- Fenster beim Start an die logische Bildschirmgröße anpassen; normale Fenstergröße beim bestätigten Beenden in `meta.window_size` speichern. Maximierte/Vollbild-Größen nicht als normale Größe speichern. Dashboard und Kontakte bis 1120 px Inhaltsbreite, Formularabschnitte bis 760 px.
- Im Job-Editor Tab-Leiste, Titel, Hinweise/Fehler und Formular-/Fortschrittsabschnitte seitlich ausrichten (max. 760 px). Nur die Kontakttabelle darf breiter sein. Inline-Status neben Buttons ohne Block-Hinweis-Margins darstellen.
- Tastaturbedienung, Fokus, Labels und schmale Fenster erhalten. Kein Layout darf horizontal aus dem Fenster laufen; breite Tabellen dürfen intern scrollen.

## CSV und Template-Sprache

Vereinbarter Beispielexport:

```csv
First Name,Last Name,Title,Company Name,Email,Contact Owner,Person Linkedin Url,City
```

`TestContacts.csv` enthält nur Kopfzeilen für manuelle Tests. Reale Apollo-Exports wie `Master.csv` gehören nicht ins Repository.

- Tags: `{{Spaltenname}}`, Ersatzwerte: `{{Spaltenname|Ersatz}}`. `{{Company}}` ist ein Alias für `{{Company Name}}`.
- Leere Werte ohne Ersatz blockieren Versand. Eine fehlende Spalte bleibt auch mit Ersatzwert ungültig. Keine Schleifen oder Bedingungen hinzufügen, ohne den Umfang abzustimmen.
- Alle verfügbaren Tags in der aufklappbaren Anleitung auflisten, einschließlich zusätzlicher importierter Spalten.
- Vorschau ohne CSV und SMTP muss mit Jane Doe funktionieren. Betreff und Text müssen trotzdem gültig sein. Beispieldaten deutlich kennzeichnen und vom Versand ausschließen.
- Plain Text; HTML und Markdown werden nicht als Formatierung gerendert. Betreff ohne Zeilenumbrüche.

## Fehler und vertrauliche Daten

- Fehler mit Aktion, genauer Ursache und geeignetem nächsten Schritt anzeigen. Wenn vorhanden SMTP-Schritt, Antwortcode und Fehler-ID zeigen.
- Strukturierte lokale JSONL-Logs mit Rotation. Passwörter, Auth-Daten, Nachrichteninhalte und Kontaktadressen nicht ungeschützt protokollieren. Auch rohe Wails-Aufrufe können Formulardaten enthalten.
- Logs und UI müssen dieselbe Fehler-ID verwenden. Ein fehlgeschlagener Log-Schreibvorgang wird ausdrücklich angezeigt.
- Wails 2 wandelt Callback-Fehler in JavaScript `Error` um. `App.formatError` überträgt deshalb JSON als String; `desktopError` stellt das strukturierte Problem wieder her. Direkte Objekte ergeben sonst `[object Object]`.
- `ReportClientError` gibt `diagnostics.Problem` als Wert zurück. `*Problem` implementiert `error` und würde von Wails als fehlgeschlagener Aufruf behandelt.
- SMTP-Passwörter nur im Arbeitsspeicher oder auf Wunsch im OS-Schlüsselbund. Nicht in SQLite, Git, Testsnapshots oder Release-Dateien speichern.
- Keine echten Mails aus Agentenläufen, Tests oder CI versenden. Tests verwenden ausschließlich künstliche Kontakte und lokale SMTP-Server auf `127.0.0.1`.
- Keine Benutzerdatenbank löschen, zurücksetzen oder durch eine ältere Kopie ersetzen. Das würde den Doppelversandschutz entfernen.

## Einstieg in den Code

| Bereich | Dateien |
|---|---|
| Desktop-Bindings und Fehlertransport | `app.go`, `desktop.go`, `main.go`, `framework_logger.go` |
| Jobs, Sperren, Entwürfe, Löschen | `internal/outreach/jobs.go` |
| Import und Personenabgleich | `internal/outreach/csv.go`, `history.go` |
| Templates und Beispielkontakt | `internal/outreach/template.go`, `example.go` |
| Freigaben und SMTP-Worker | `internal/outreach/session.go`, `engine.go`, `smtp.go` |
| Google OAuth, Token-Erneuerung und Gmail-Versand | `google.go`, `internal/googlemail/` |
| Persistenz und Reservierungen | `internal/outreach/store.go` |
| Diagnosedaten und Schwärzung | `internal/diagnostics/` |
| Dashboard, Tabs, Job-Editor | `frontend/src/App.svelte`, `JobTabs.svelte`, `JobEditor.svelte` |
| Bridge und Fehlermeldungen | `frontend/src/api.ts`, `errors.ts`, `ErrorNotice.svelte` |

## Arbeitsweise und Tests

Die vereinbarte Arbeitsweise entspricht `/ponytail`: die kleinste saubere Lösung, vorhandenen Code und Standardbibliotheken nutzen, keine spekulativen Abstraktionen oder Abhängigkeiten. Die Regeln stehen hier vollständig; private Skills auf dem Rechner des ursprünglichen Entwicklers werden nicht vorausgesetzt. Validierung, Fehlerbehandlung und Tests werden dadurch nicht gekürzt.

Für jeden geänderten Verarbeitungsschritt passende Unit-Tests ergänzen oder aktualisieren. Bei Sicherheitsregeln auch Fehlfälle, konkurrierende Aktionen und Wiederherstellung prüfen. Ein Bugfix braucht einen Regressionstest, der den ursprünglichen Fehler erkennen würde. Frontend-Mocks ersetzen keine Backend-Tests.

```sh
make setup
make test
cd frontend
pnpm exec playwright install chromium webkit
pnpm run test:e2e
```

`make test` baut zuerst das Frontend, dann folgen Vitest, `go test -race -tags integration ./...` und `go vet -tags integration ./...`. Go bettet `frontend/dist` ein: Niemals Go-Kompilierung und einen Vite-Build gleichzeitig ausführen, der diesen Ordner ersetzt. Wails-Builds ebenfalls nacheinander ausführen, weil sie Dateien generieren.

`make test-native` ergänzt den echten OS-Schlüsselbund und die native Wails-WebView mit isolierter SQLite-Datenbank und lokalem TLS-SMTP. Die Testprogramme mit `integration,nativee2e` niemals als Release verteilen. [TESTING.md](TESTING.md) enthält die Testmatrix, einzelne Befehle und die noch manuell zu prüfenden Betriebssystemfälle. Der Prozessabsturz-Test beendet ausschließlich seinen eigenen Test-Unterprozess.

Auf Windows ohne Make die Befehle aus `README.md` verwenden. Für Versand-, Store- und Job-Änderungen Go-Race-Tests ausführen. Für UI-Änderungen Svelte/TypeScript-Check, Vitest und die betroffenen Playwright-Flows auf Chromium und WebKit prüfen. Keine tatsächliche Zustellung oder Windows-11-Kompatibilität aus Browser-Mocks ableiten.

## Builds und Veröffentlichung

`make build-mac` erzeugt `build/bin/Outreach.app`, `make build-windows` die x64-EXE. Die GitHub-Actions-Konfiguration in `.github/workflows/check.yml` baut auf separaten macOS- und Windows-Runnern und veröffentlicht bei passenden Versionstags Downloads. Ablauf in `README.md`.

Vor Übergabe Änderungen, ausgeführte Tests und offene Grenzen nennen. Ein grüner CI-Build ersetzt den manuellen Test auf Windows 11 Home, WebView2, Credential Manager oder einer sauberen macOS-Installation nicht. Keine "läuft überall perfekt"-Zusage ohne diese Prüfungen. Signierung bleibt bis zu einer neuen Entscheidung aus.
