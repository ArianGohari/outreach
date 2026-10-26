<script lang="ts">
  let { headers }: { headers?: string[] } = $props();
  const standardColumns = [
    'First Name',
    'Last Name',
    'Title',
    'Company Name',
    'Email',
    'Contact Owner',
    'Person Linkedin Url',
    'City',
  ];
  const descriptions: Record<string, string> = {
    'First Name': 'Vorname, z. B. Jane',
    'Last Name': 'Nachname, z. B. Doe',
    Title: 'Position / Berufsbezeichnung, z. B. Head of Sales',
    'Company Name': 'Firmenname, z. B. Beispiel GmbH',
    Email: 'Kontaktadresse; ändert nicht den Versandempfänger.',
    'Contact Owner': 'Zuständige Person in Apollo.',
    'Person Linkedin Url': 'Link zum LinkedIn-Profil des Kontakts',
    City: 'Ort des Kontakts, z. B. Berlin',
    Industry: 'Branche des Kontakts / Unternehmens',
    'Email Status': 'Verifizierungsstatus aus Apollo, z. B. verified',
    'Apollo Contact Id': 'Kontaktkennung aus Apollo',
    'Email Bounced': 'Bounce-Angabe aus Apollo',
  };
  const columns = $derived(headers ?? standardColumns);
</script>

<details class="template-guide">
  <summary>Template-Tags & Anleitung</summary>
  <div class="guide-content">
    <p>
      <code>{'{{Spaltenname}}'}</code> fügt den CSV-Wert in Betreff oder Text ein.
    </p>
    <h3>Verfügbare Tags</h3>
    <p class="hint">
      {headers ? 'Importierte Spalten.' : 'Standardfelder vor dem CSV-Import.'} Schreibweise
      genau übernehmen.
    </p>
    <div class="table-wrap">
      <table aria-label="Verfügbare Template-Tags">
        <thead><tr><th>Tag</th><th>Wert / Bedeutung</th></tr></thead>
        <tbody>
          {#each columns as column}
            <tr
              ><td><code>{'{{' + column + '}}'}</code></td><td
                >{descriptions[column] ??
                  'Wert aus der gleichnamigen CSV-Spalte'}</td
              ></tr
            >
          {/each}
          {#if columns.includes('Company Name')}
            <tr
              ><td><code>{'{{Company}}'}</code></td><td
                >Alias für <code>{'{{Company Name}}'}</code>.</td
              ></tr
            >
          {/if}
        </tbody>
      </table>
    </div>
    <h3>Leere Felder und Ersatzwerte</h3>
    <p>
      <code>{'{{Spaltenname|Ersatztext}}'}</code> verwendet den Ersatztext, wenn das
      Feld leer ist.
    </p>
    <ul>
      <li>
        <code>{'Hallo {{First Name|zusammen}},'}</code> ergibt „Hallo Jane,“ oder
        bei leerem Vornamen „Hallo zusammen,“.
      </li>
      <li>
        <code>{'{{Company Name|deinem Unternehmen}}'}</code> setzt den Firmennamen
        ein, sonst „deinem Unternehmen“.
      </li>
      <li>
        <code>{'{{Title|}}'}</code> setzt bei leerer Position nichts ein. Umgebender
        Text und Satzzeichen bleiben stehen.
      </li>
    </ul>
    <p>
      Leere Felder ohne Ersatz blockieren den Versand. Fehlende Spalten sind
      auch mit Ersatzwert ungültig.
    </p>
    <h3>Beispiel</h3>
    <div class="guide-example">
      <span class="label">Betreff</span>
      <pre>{'Kurze Frage, {{First Name}}'}</pre>
      <span class="label">Text</span>
      <pre>{'Hallo {{First Name|zusammen}},\n\nich habe eine Frage zu {{Company Name|deinem Unternehmen}}.\nHast du diese Woche Zeit für einen kurzen Austausch?'}</pre>
    </div>
    <h3>Versand</h3>
    <p>
      Die Vorschau prüft alle Kontakte. Mit den Pfeilen wechselst du den
      Empfänger. Testmail im Posteingang prüfen.
    </p>
    <p>
      Nur Text: keine HTML-/Markdown-Formatierung, Bedingungen, Schleifen oder
      verschachtelten Tags. Signatur selbst einfügen. Keine Zeilenumbrüche im
      Betreff.
    </p>
  </div>
</details>
