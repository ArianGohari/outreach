# Outreach

Lokale Desktop-App mit Go, Wails 2, Svelte 5 und SQLite. Die aktuelle Oberfläche in `frontend/src` ist maßgeblich. Dashboard und Job-Tabs folgen der neueren Designvorlage; ältere Details aus `Outreach.html` werden nicht wieder eingeführt. Versand über SMTP mit TLS/STARTTLS; keine externen Trackingdienste und kein Postfachzugriff.

## Downloads und Mitarbeit

Fertige Versionen stehen nach der ersten Veröffentlichung unter [GitHub Releases](https://github.com/ArianGohari/outreach/releases) bei **Assets**: `Outreach-macos-arm64.zip` und `Outreach-windows-x64.zip`. Für Downloads aus einem privaten Repository benötigen Teammates Zugriff auf das Repository. Die automatisch angezeigten "Source code"-Archive enthalten nur Quellcode.

Für die Weiterentwicklung zuerst [AGENTS.md](AGENTS.md) lesen. Dort stehen die bisherigen Produktentscheidungen, Versandregeln, Designvorgaben und Testpflichten. Diese README beschreibt Einrichtung und Bedienung.

## Lokal starten

Voraussetzungen: Go ab der Version in `go.mod`, Node.js 24 oder neuer und die [Wails-Systemvoraussetzungen](https://wails.io/docs/gettingstarted/installation/). Auf macOS werden die Xcode Command Line Tools benötigt.

```sh
make setup
make dev
```

```sh
make test
make build-mac
```

Unter Windows kann man dieselben Schritte in PowerShell ohne Make ausführen. Go, Node.js und die Wails-Voraussetzungen müssen installiert sein; für `go test -race` wird ein kompatibler C-Compiler benötigt.

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
cd frontend
npm ci
npm run build
npm test
npx playwright install chromium webkit
npm run test:e2e
cd ..
go test -race ./...
go vet ./...
wails dev
# Oder den Windows-Build erzeugen:
wails build -platform windows/amd64
```

Go bettet `frontend/dist` ein. Deshalb vor dem ersten Go-Test das Frontend bauen und Go-Tests niemals parallel zu einem Vite-Build starten.

Die macOS-App liegt anschließend unter `build/bin/Outreach.app`. Der erste Build verwendet kein Developer-ID-Zertifikat und keine Notarisierung. Das Buildwerkzeug darf lokal ad hoc signieren. `make build-windows` erzeugt eine Windows-x64-EXE; deren Ausführung muss separat unter Windows 11 geprüft werden. Eine erfolgreiche Cross-Kompilierung ist kein Windows-Laufzeittest.

## Jobs und Konten

Die Übersicht zeigt eingerichtete Jobs, ihre Absender, Fortschritte und Status. Das Plus in der Tab-Leiste öffnet einen vorhandenen unberührten Entwurf oder legt einen neuen an. Unberührte Entwürfe erscheinen nur als Tab und werden beim Schließen sowie beim App-Start bereinigt. Namen, geänderte Einstellungen, CSV-Daten und Versandhistorie verhindern das Verwerfen. Jeder Job hat eigene SMTP-Einstellungen, CSV-Daten, Vorlage, Vorschau, Testfreigabe und Versandwarteschlange. Bereits eingerichtete Absender können beim Erstellen weiterer Jobs übernommen werden; das Passwort wird erneut eingegeben oder aus dem Betriebssystem-Schlüsselbund geladen.

Jobs verschiedener Absenderadressen können parallel laufen. Pro Absenderadresse darf nur ein Job gleichzeitig aktiv sein, einschließlich Versand- und Batchpausen. Die Prüfung erfolgt im Backend auch bei gleichzeitigen Startversuchen; Groß-/Kleinschreibung der Adresse umgeht diese Sperre nicht. Weitere Jobs desselben Absenders werden nach Freigabe des Kontos manuell gestartet.

Offene Jobs erscheinen in einer horizontal scrollbaren Tab-Leiste mit Statusanzeige und fest erreichbarem Plus-Button. Pfeiltasten sowie Home/End wechseln Tabs; Delete schließt den ausgewählten Job-Tab. Beim Schließen wird der benachbarte Tab aktiv. Ein Tabwechsel erhält Eingaben und Freigaben des jeweiligen Jobs. Bei eingerichteten Jobs schließt der Tab nur die Ansicht; der Job bleibt im Dashboard sichtbar und läuft gegebenenfalls weiter. „Löschen“ im Dashboard entfernt nach Bestätigung den Job und seinen Tab. Laufende Jobs müssen zuerst pausiert werden. Versandpläne, Versandhistorie und Duplikatsperren bleiben gespeichert; gelöschte Jobs werden nicht fortgesetzt. Pausieren wirkt nur auf den gewählten Job. Beim Beenden der App werden alle Jobs gestoppt, wobei laufende SMTP-Übertragungen abgeschlossen werden.

Entwürfe werden nach einer kurzen Eingabepause lokal gespeichert; „Entwurf speichern“ speichert sofort. Die Anzeige bestätigt den Speichervorgang bzw. meldet Fehler. Passwörter werden nicht in Entwürfen gespeichert. Nach einem App-Neustart laufen Jobs nicht automatisch weiter: Verbindung und Testmail müssen erneut geprüft werden. Die bisherige Einzel-Job-Versandhistorie wird einmalig als „Bisheriger Versand“ übernommen, einschließlich gesperrter Einträge und ausstehender Pausen. Alte Reservierungen werden auf den gemeinsamen Duplikatschutz übertragen.

## Ablauf pro Job

1. SMTP-Verbindung prüfen. Port 465 verwendet direkt TLS, andere Ports zwingend STARTTLS. Unterstützt wird SMTP AUTH PLAIN über TLS, kein OAuth-Login. Manche Anbieter benötigen dafür ein App-Passwort.
2. CSV auswählen und Kontakte prüfen. Nur UTF-8, optional mit BOM, und Komma als Trennzeichen werden akzeptiert. Fehlerhafte CSV-Strukturen blockieren den vollständigen Import.
3. Betreff und Text schreiben und alle Nachrichten prüfen. Platzhalter: `{{First Name}}`, `{{Company Name}}` oder `{{Company}}`; Ersatzwerte: `{{Title|Team}}`. Unbekannte Spalten und fehlende Werte ohne Ersatz blockieren die gesamte Sendung. Es werden nur Textmails erzeugt. Die App ergänzt keine Signatur.
4. Testmail an eine ausdrücklich angegebene Adresse senden und den Inhalt im Posteingang prüfen.
5. Batchgröße (Standard: 50) und Batchpause in Stunden einstellen (Standard: 24 Stunden; z. B. auch 0,5 oder 1,5 Stunden), dann Senden bestätigen. Stoppen beendet den Ablauf vor dem nächsten Empfänger; eine laufende SMTP-Übertragung kann noch abschließen. Nach App-Neustart Verbindung und Testmail erneut prüfen und mit identischen Einstellungen fortsetzen.

Der mit dem Nutzer abgestimmte CSV-Aufbau:

```text
First Name,Last Name,Title,Company Name,Email,Contact Owner,Person Linkedin Url,City
```

Eine Spalte `Email Status` fehlt in diesem Format. Der standardmäßig aktive Filter „Nur verifizierte“ lässt daher keine Kontakte durch, bis er ausdrücklich deaktiviert wird. Doppelte Kontakte werden immer automatisch ausgeschlossen; dafür gibt es keine Checkbox. Personengleichheit wird anhand derselben Adresse, Apollo-Kontakt-ID oder LinkedIn-Personenkennung erkannt, einschließlich transitiver Verknüpfungen. Identische Namen allein werden nicht zusammengeführt.

## Template-Sprache und Vorschau

Die aufklappbare „Template-Tags & Anleitung“ im Bereich Nachricht listet jede Spalte der geladenen CSV auf. Vor dem Import zeigt sie das vereinbarte Apollo-Format. Mit „Nachrichten prüfen & Vorschau“ lässt sich das Template schon ohne CSV und ohne SMTP-Zugangsdaten ansehen. Dabei wird Jane Doe als eindeutig gekennzeichneter Beispielkontakt verwendet. Beispieldaten können weder Testmails noch Outreach-Versand freigeben und werden nicht in die Versanddatenbank übernommen. Nach dem Import verwendet die Vorschau die echten Kontaktwerte.

| Tag | Inhalt |
|---|---|
| `{{First Name}}` | Vorname, im Beispiel Jane |
| `{{Last Name}}` | Nachname, im Beispiel Doe |
| `{{Title}}` | Position / Berufsbezeichnung |
| `{{Company Name}}` | Firmenname |
| `{{Company}}` | Kurzform für `{{Company Name}}` |
| `{{Email}}` | Kontaktadresse als Text; ändert nicht den Versandempfänger |
| `{{Contact Owner}}` | Zuständige Person oder Kennung aus Apollo, nicht der SMTP-Absender |
| `{{Person Linkedin Url}}` | LinkedIn-Profiladresse |
| `{{City}}` | Ort |

Zusätzliche importierte Spalten können ebenfalls verwendet werden, etwa `{{Industry}}`, wenn die CSV tatsächlich eine Spalte „Industry“ enthält. Schreibweise wie in der Tag-Liste übernehmen. Tags funktionieren im Betreff und im Nachrichtentext.

`{{First Name|zusammen}}` setzt bei leerem Vornamen „zusammen“ ein. `Hallo {{First Name|zusammen}},` ergibt also „Hallo Jane,“ oder „Hallo zusammen,“. Ein leeres Feld ohne Ersatz blockiert die Versandfreigabe. Eine vollständig fehlende Spalte ist auch mit Ersatzwert ungültig. `{{Title|}}` erlaubt einen leeren Ersatz; umgebende Wörter und Satzzeichen bleiben erhalten.

```text
Betreff: Kurze Frage, {{First Name}}

Hallo {{First Name|zusammen}},

ich habe eine Frage zu {{Company Name|deinem Unternehmen}}.
Hast du diese Woche Zeit für einen kurzen Austausch?
```

Es gibt keine Bedingungen, Schleifen oder verschachtelten Tags. HTML und Markdown werden nicht als Formatierung interpretiert. Signaturen werden nur versendet, wenn sie im Template stehen. Zeilenumbrüche sind im Nachrichtentext erlaubt, im Betreff nicht.

## Versand und Speicherung

Ein Versandplan enthält die fertig gerenderten Nachrichten. Vorschau, Testmail und Outreach verwenden diesen Inhalt; technisch notwendige Zeilenenden werden vorab normalisiert. Jeder Empfänger wird in SQLite reserviert, bevor SMTP angesprochen wird. Datenbankfehler verhindern den Versand. Ein Dateilock verhindert, dass zwei App-Prozesse dieselbe Datenbank verwenden.

Bei fehlender SMTP-Bestätigung oder einem Absturz während der Übertragung wird der Eintrag als unklar gesperrt. Es gibt keine automatischen Wiederholungen. Eine SMTP-Annahme bedeutet keine bestätigte Zustellung. Nach dem ersten Versandfehler pausiert die gesamte Verarbeitung; weitere ausstehende Kontakte können manuell fortgesetzt werden. Fehlerhafte und unklare Einträge selbst werden nicht wiederholt.

Zusätzlich verhindern gespeicherte Reservierungen, dass dieselbe Nachricht erneut an dieselbe erkannte Person oder Adresse gesendet wird, auch bei erneutem Import, verschiedenen Jobs und verschiedenen Absendern. Ein geänderter Nachrichteninhalt ist eine neue Nachricht. Ohne gemeinsame Personenkennung lassen sich verschiedene Adressen derselben Person nicht sicher zuordnen. Löschen oder Zurücksetzen der Datenbank entfernt diesen lokalen Schutz; die Versandhistorie daher nicht durch eine ältere Kopie ersetzen.

Die Datenbank liegt unter dem Betriebssystem-Konfigurationsverzeichnis in `Outreach/outreach.db`, auf macOS normalerweise `~/Library/Application Support/Outreach/`. SMTP-Passwörter werden niemals in SQLite gespeichert. Optional werden sie im macOS-Schlüsselbund bzw. Windows Credential Manager abgelegt, andernfalls nur für die laufende App-Sitzung gehalten.

## Tests

```sh
go test -race -cover ./internal/outreach
go test ./internal/outreach -run '^$' -fuzz FuzzCSVNeverPanics -fuzztime 5s
cd frontend
npm test
npm run test:e2e
```

Go-Tests prüfen CSV-Struktur und Duplikate, Platzhalter, Freigaben, MIME-Inhalt, Empfänger, SQLite-Reservierungen, konkurrierende Starts, Stoppen, Batchpausen und Abstürze. Zusätzliche Job-Tests prüfen parallele Worker, Kontosperren, getrennte Testfreigaben, gemeinsame Duplikatsperren sowie die Migration und Wiederherstellung bestehender Daten. SMTP-Integrationstests starten ausschließlich lokale Server auf `127.0.0.1`, mit temporären TLS-Zertifikaten. Sie simulieren verlorene Bestätigungen, abgelehnte Empfänger und fehlendes TLS. Testdateien enthalten erfundene Kontakte mit `example.com`-Adressen. `Master.csv` wird nicht in das Repository übernommen.

Frontend-Tests prüfen die fünf Schritte und die Versandfreigabe. Browser-Tests nutzen eine ausdrücklich simulierte Wails-Schnittstelle und können keine echten Mails senden. Vor Browser-Tests einmal `npx playwright install chromium webkit` ausführen.

Die GitHub-Actions-Konfiguration führt Tests und Builds auf macOS- und Windows-Runnern aus. Nach einem Versions-Tag veröffentlicht sie die geprüften Build-Artefakte als GitHub Release; Ablauf unten. Noch separat zu prüfen: Installation auf einem sauberen Windows-11-Home-Rechner, dortige WebView2-Verfügbarkeit und Credential Manager, echte Hardware-/Ruhezustandsfälle sowie ältere macOS-Versionen. Bis diese Prüfungen vorliegen, ist dies ein lokaler erster Build.

## Fehlerdiagnose

Bei einem Fehler zeigt die Oberfläche die Aktion, die konkrete Ursache und einen nächsten Schritt. SMTP-Fehler enthalten Server, Port, Protokollschritt und den Antwortcode samt Servermeldung, soweit vorhanden. Unter „Diagnosedaten“ stehen Fehler-ID, Zeitpunkt und Protokollpfad. Dieselbe ID wird im lokalen Protokoll gespeichert. Fehler während des Versands sind zusätzlich beim betroffenen Eintrag aufklappbar. Abgebrochene Dialoge werden nicht als Fehler behandelt.

Das strukturierte JSONL-Protokoll liegt auf macOS unter `~/Library/Application Support/Outreach/logs/errors.jsonl`, auf Windows unter `%AppData%\Outreach\logs\errors.jsonl`. Es rotiert bei 2 MiB mit maximal drei Sicherungsdateien. Passwörter, SMTP-Authentifizierungsdaten und bekannte Nachrichteninhalte werden aus Fehlertexten entfernt; E-Mail-Adressen werden maskiert. Für interne Desktop-Fehler werden eine Meldungskennung und die Quellstelle protokolliert, da rohe Wails-Meldungen komplette Formularwerte enthalten können. Bekannte Eingabefehler werden mit genauer Ursache protokolliert; unbekannte Oberflächenfehler nur mit ihrer Kategorie. Die Oberfläche zeigt zusätzlich die bereinigte Fehlermeldung.

Kann das Protokoll nicht geschrieben werden, zeigt die Fehleranzeige dies ausdrücklich. Ist schon beim Start kein Protokoll verfügbar, wird der Versand nicht freigegeben. Fehlerberichte sind lokale Dateien und werden nicht automatisch hochgeladen.


## GitHub einrichten und Releases erstellen

Der Workflow steht in [.github/workflows/check.yml](.github/workflows/check.yml). Normale Pushes und Pull Requests führen Tests und Builds aus. Die ZIP-Dateien stehen im jeweiligen Actions-Lauf 14 Tage lang als Artefakte bereit. Tags im Format `vX.Y.Z` lösen zusätzlich ein GitHub Release aus. Dieses erhält beide ZIP-Dateien und `SHA256SUMS.txt`, sobald beide Plattform-Jobs erfolgreich waren.

Einmalige Einrichtung:

1. Repository unter dem gewünschten Account oder der Organisation anlegen und die Sichtbarkeit festlegen. Für die Zusammenarbeit am privaten Projekt die Teammates einladen.
2. Quellcode einschließlich `AGENTS.md`, Lockfiles, `build/`-Vorlagen und `.github/` hochladen. `build/bin/`, `frontend/dist/`, `node_modules`, Logs, echte Kontaktlisten, Datenbanken und Zugangsdaten bleiben lokal. Die `.gitignore` deckt diese generierten Dateien, `Master.csv` und übliche lokale Geheimnisdateien ab; andere echte CSV-Exports ebenfalls nicht hinzufügen.
3. GitHub Actions im Repository zulassen. Der Workflow verwendet `GITHUB_TOKEN`; ein persönlicher Token oder SMTP-Geheimnisse sind nicht nötig. Nur der Release-Job erhält `contents: write`.
4. Den ersten Actions-Lauf abwarten. Als Team Branch-Schutz für `main` mit Review und den beiden Checks `Desktop (macos-arm64)` und `Desktop (windows-x64)` einrichten.

Für ein bereits auf GitHub angelegtes, leeres Repository (URL ersetzen):

```sh
git add .
git diff --cached --stat
git commit -m "Initial Outreach app"
git remote add origin https://github.com/ArianGohari/outreach.git
git push -u origin main
```

Im lokalen Projekt ist Git bereits initialisiert. In einem neuen Export ohne `.git` vorher `git init -b main` ausführen. Bei einem bestehenden Repository mit Inhalt zuerst klonen und die Änderungen dort integrieren, keinen Force-Push verwenden. Vor dem ersten Commit die vorgemerkten Dateien prüfen. Eine Open-Source-Lizenz wird erst nach einer ausdrücklichen Teamentscheidung ergänzt.

Für ein Release die gewünschte Version in `wails.json` unter `info.productVersion` eintragen, committen und nach `main` pushen. Tag und App-Version müssen übereinstimmen; andernfalls stoppt der Workflow. Für die aktuelle Version `0.1.0`:

```sh
git tag -a v0.1.0 -m "Outreach 0.1.0"
git push origin v0.1.0
```

Danach unter **Actions** den Lauf verfolgen und unter **Releases** herunterladen. Keine zweite Release-Seite manuell anlegen: Der Workflow erstellt sie samt Downloads. Bereits veröffentlichte Tags nicht verschieben; für Änderungen eine neue Version verwenden. Wenn ein Release-Lauf fehlschlägt, zuerst die Logs prüfen; ein bestehendes Release wird nicht automatisch überschrieben.

macOS wird auf einem ARM64-Runner gebaut und mit `ditto` vor dem Artefakt-Upload als ZIP verpackt, damit das App-Bundle seine Dateirechte behält. Windows wird auf einem x64-Windows-Runner gebaut und als ZIP mit EXE ausgeliefert. Es wird derzeit kein Installer erzeugt.

Downloads haben weiterhin keine Developer-ID-/Windows-Code-Signatur und keine Notarisierung. Daher sind Hinweise von Gatekeeper bzw. Windows möglich; für macOS beschreibt [Apple das Öffnen heruntergeladener Apps](https://support.apple.com/en-us/102445). Windows benötigt die [WebView2 Runtime](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution). SHA-256-Prüfsummen erkennen abweichende Dateien, ersetzen aber keine Herausgebersignatur.

Vor einem Einsatz mit echten Empfängern auf den Zielsystemen prüfen: Start einer heruntergeladenen Version, CSV-Import, Jane-Doe-Vorschau, Fehlerdarstellung, parallele Jobs, Neustart und Wiederaufnahme, Job-Löschen sowie Schlüsselbundzugriff. SMTP-Tests zunächst nur mit eigenen Testkontakten. CI bestätigt keine fehlerfreie Installation auf Windows 11 Home oder jeder unterstützten macOS-Version.

Referenzen für den Workflow: [GitHub-Runner](https://docs.github.com/en/actions/reference/runners/github-hosted-runners), [GitHub-Release-CLI](https://cli.github.com/manual/gh_release_create).
