<script lang="ts">
  const uid = $props.id();
  import { onMount } from 'svelte';
  import TemplateGuide from './TemplateGuide.svelte';
  import ErrorNotice from './ErrorNotice.svelte';
  import JobProgress from './JobProgress.svelte';
  import { isProblem, errorText, desktopError } from './errors';
  import {
    backend as desktopBackend,
    jobBackend,
    type JobView,
    batchHoursToMinutes,
    canStart,
    statusLabel,
    type Problem,
    type Config,
    type Report,
    type Plan,
    type Request,
    type State,
  } from './api';
  let {
    job,
    onChanged,
    accounts = [],
    runningAccounts = [],
  }: {
    job: JobView;
    onChanged: () => void;
    accounts?: JobView[];
    runningAccounts?: { id: string; address: string }[];
  } = $props();
  const backend = () => jobBackend(job.id);
  let jobName = $state('');
  let view = $state<'edit' | 'progress'>('edit');
  let saveStatus = $state('');
  let googleConfigured = $state(false);
  async function selectProvider(provider: string) {
    config = {
      ...config,
      provider: provider === 'google' ? 'google' : '',
      googleGrant: '',
      username: '',
      password: '',
      from: '',
      remember: false,
    };
    invalidate(true);
    if (provider === 'google')
      await action(async () => {
        googleConfigured = await desktopBackend().GoogleConfigured();
      });
  }
  async function googleLogin() {
    await action(async () => {
      const account = await desktopBackend().ConnectGoogle(job.id);
      config = {
        ...config,
        ...account,
        host: '',
        port: 587,
        password: '',
        remember: false,
        from: '',
      };
      testTo = config.username;
      invalidate(true);
    });
  }
  async function googleDisconnect() {
    await action(async () => {
      await desktopBackend().DisconnectGoogle(job.id, { ...config });
      config = { ...config, googleGrant: '', username: '', from: '' };
      invalidate(true);
    });
  }
  let saveTimer: ReturnType<typeof setTimeout> | undefined;
  let dirty = false;
  let initialized = false;
  let saving: Promise<void> = Promise.resolve();
  function scheduleSave() {
    if (!initialized) return;
    dirty = true;
    saveStatus = 'Nicht gespeichert';
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => {
      if (busy) scheduleSave();
      else void saveDraft().catch(showError);
    }, 800);
  }
  export async function flushDraft() {
    if (busy && dirty)
      throw new Error(
        'Für diesen Job läuft noch eine Aktion. Bitte kurz warten.',
      );
    await saveDraft();
    await saving;
  }
  async function saveDraft() {
    clearTimeout(saveTimer);
    if (!dirty) return;
    const req = request(),
      name = jobName,
      version = revision;
    saving = saving
      .catch(() => {})
      .then(() => desktopBackend().SaveDraft(job.id, name, req));
    await saving;
    if (version === revision) {
      dirty = false;
      saveStatus = 'Entwurf gespeichert';
    }
    onChanged();
  }
  async function useAccount(id: string) {
    const source = accounts.find((a) => a.id === id);
    if (!source) return;
    config = { ...source.draft.request.config, password: '' };
    invalidate(true);
    if (config.provider === 'google')
      await action(async () => {
        googleConfigured = await desktopBackend().GoogleConfigured();
      });
    if (config.remember && config.provider !== 'google')
      await action(async () => {
        config.password = await backend().LoadPassword({ ...config });
      });
  }
  let config = $state<Config>({
    host: '',
    port: 587,
    username: '',
    password: '',
    fromName: '',
    from: '',
    replyTo: '',
    remember: false,
  });
  let subject = $state(''),
    body = $state('');
  let seconds = $state(30),
    batchSize = $state(50),
    batchHours = $state(24);
  let report = $state<Report | null>(null),
    plan = $state<Plan | null>(null),
    index = $state(0),
    verified = $state(true);
  let deliveryState = $state<State>({
    plan: null,
    deliveries: [],
    running: false,
    nextAt: '',
    error: '',
  });
  let busy = $state(false),
    error = $state<Problem | null>(null),
    notice = $state(''),
    connected = $state(false),
    contactsChecked = $state(false),
    tested = $state(''),
    testTo = $state('');
  let bodyInput: HTMLTextAreaElement;
  let revision = 0;
  const message = $derived(plan?.messages[index]);
  const sent = $derived(
    deliveryState.deliveries.filter((d) => d.status === 'sent').length,
  );
  const finished = $derived(
    deliveryState.deliveries.filter(
      (d) => !['pending', 'sending'].includes(d.status),
    ).length,
  );
  const locked = $derived(busy || deliveryState.running);
  const accountBusy = $derived(
    runningAccounts.some(
      (a) =>
        a.id !== job.id &&
        a.address.toLowerCase() ===
          (config.from || config.username).toLowerCase(),
    ),
  );
  const ready = $derived(
    !accountBusy &&
      canStart(busy, deliveryState.running, contactsChecked, tested, plan),
  );
  function request(): Request {
    return {
      config: { ...config },
      subject,
      body,
      schedule: {
        seconds: Number(seconds),
        batchSize: Number(batchSize),
        batchMinutes: batchHoursToMinutes(Number(batchHours)),
      },
    };
  }
  function invalidate(smtp = false) {
    revision++;
    tested = '';
    plan = null;
    notice = '';
    if (smtp) connected = false;
    scheduleSave();
  }
  async function showError(value: unknown, category = 'UI_INPUT') {
    value = desktopError(value);
    if (isProblem(value)) {
      if (value.cancelled) {
        notice = value.message;
        return;
      }
      error = value;
      return;
    }
    const details = errorText(value, [
      config.password,
      config.username,
      subject,
      body,
    ]);
    try {
      const inputCodes: Record<string, string> = {
        'Batchpause zwischen 0 und 24 Stunden eingeben.': 'UI_BATCH_RANGE',
        'Batchpause minutengenau eingeben, z. B. 0,5 oder 1,5 Stunden.':
          'UI_BATCH_PRECISION',
        'Keine freigegebenen Kontakte. Filter und Ausschlüsse prüfen.':
          'UI_NO_CONTACTS',
      };
      const logged = await backend().ReportClientError(
        inputCodes[details] || category,
      );
      error = { ...logged, details };
    } catch {
      error = {
        code: category,
        message: 'Aktion fehlgeschlagen',
        details,
        logged: false,
        logIssue:
          'Fehlerprotokoll über die Desktop-Verbindung nicht erreichbar.',
      };
    }
  }
  function readWorkerProblem() {
    if (deliveryState.problem && deliveryState.problem.id !== error?.id)
      error = deliveryState.problem;
  }
  async function action(fn: () => Promise<void>) {
    if (busy) return;
    busy = true;
    error = null;
    notice = '';
    try {
      await saveDraft();
      await fn();
      onChanged();
    } catch (e) {
      await showError(e);
    } finally {
      busy = false;
    }
  }
  async function refresh() {
    try {
      deliveryState = await backend().GetState();
      readWorkerProblem();
    } catch (e) {
      await showError(e);
    }
  }
  async function preview() {
    const current = revision;
    const p = await backend().Preview(request());
    if (current === revision) {
      plan = p;
      index = Math.min(index, p.messages.length - 1);
    }
  }
  async function choose() {
    await action(async () => {
      let imported: Report;
      try {
        imported = await backend().ChooseCSV(verified);
      } catch (e) {
        e = desktopError(e);
        if (!(isProblem(e) && e.cancelled)) {
          invalidate();
          contactsChecked = false;
          report = null;
        }
        throw e;
      }
      invalidate();
      contactsChecked = false;
      report = imported;
      index = 0;
    });
  }
  async function filter() {
    invalidate();
    contactsChecked = false;
    await action(async () => {
      report = await backend().FilterContacts(verified);
      index = 0;
    });
  }
  async function checkContacts() {
    await action(async () => {
      if (!report?.contacts.length)
        throw new Error(
          'Keine freigegebenen Kontakte. Filter und Ausschlüsse prüfen.',
        );
      contactsChecked = true;
    });
  }
  async function connect() {
    await action(async () => {
      connected = false;
      tested = '';
      await backend().CheckConnection({ ...config });
      connected = true;
      tested = '';

      if (!testTo) testTo = config.from || config.username;
    });
  }
  async function test() {
    await action(async () => {
      tested = '';
      await preview();
      tested = await backend().SendTest(request(), index, testTo);
    });
  }
  async function start() {
    await action(async () => {
      await backend().Start(request(), tested);
      await refresh();
      view = 'progress';
    });
  }
  function insert(column: string) {
    const value = `{{${column}}}`;
    const start = bodyInput?.selectionStart ?? body.length;
    const end = bodyInput?.selectionEnd ?? start;
    body = body.slice(0, start) + value + body.slice(end);
    invalidate();
    bodyInput?.focus();
  }
  onMount(() => {
    let alive = true;
    jobName = job.name;
    void action(async () => {
      deliveryState = await backend().GetState();
      readWorkerProblem();
      if (!alive) return;
      const draft = job.draft;
      const r = draft.request;
      config = { ...r.config, password: '' };
      subject = r.subject;
      body = r.body;
      seconds = r.schedule.seconds;
      batchSize = r.schedule.batchSize;
      batchHours = r.schedule.batchMinutes / 60;
      report = draft.report.headers?.length ? draft.report : null;
      verified = draft.report.onlyVerified;
      if (deliveryState.plan) {
        view = 'progress';
        plan =
          deliveryState.plan.request.subject === r.subject &&
          deliveryState.plan.request.body === r.body
            ? deliveryState.plan
            : null;
        contactsChecked = !!report?.contacts.length;
        notice = 'Zum Fortsetzen Verbindung und Testmail erneut prüfen.';
      }
      testTo = config.from || config.username;
      if (config.remember && config.provider !== 'google')
        config.password = await backend().LoadPassword({ ...config });
      if (config.provider === 'google')
        googleConfigured = await desktopBackend().GoogleConfigured();
      initialized = true;
    });
    const off = window.runtime?.EventsOn('outreach:changed', () => {
      void refresh();
    });
    const poll = setInterval(() => {
      if (deliveryState.running) void refresh();
    }, 1500);
    return () => {
      alive = false;
      off?.();
      clearInterval(poll);
      clearTimeout(saveTimer);
    };
  });
</script>

<div class="job-editor">
  <header>
    <h1>{jobName}</h1>
  </header>
  {#if error}<ErrorNotice problem={error} />{/if}
  {#if notice}<div class="feedback" role="status">{notice}</div>{/if}
  {#if view === 'progress'}
    <JobProgress
      state={deliveryState}
      {busy}
      onEdit={() => (view = 'edit')}
      onStop={() =>
        action(async () => {
          await backend().Stop();
          await refresh();
          notice = 'Wird nach der laufenden Übertragung gestoppt.';
        })}
    />
  {/if}
  <div hidden={view !== 'edit'}>
    <section aria-labelledby={uid + '-sender-heading'}>
      <h2 id={uid + '-sender-heading'}><b>01</b>Job & Absender</h2>
      <label class="job-name"
        ><span>Jobname</span><input
          bind:value={jobName}
          maxlength="100"
          disabled={locked}
          oninput={() => {
            revision++;
            scheduleSave();
          }}
        /></label
      >
      {#if accounts.some((a) => a.id !== job.id && (a.draft.request.config.from || a.draft.request.config.username))}
        <label class="account-choice"
          ><span>Absender-Konto übernehmen</span><select
            disabled={locked}
            onchange={(e) => void useAccount(e.currentTarget.value)}
          >
            <option value="">Eigenes Konto konfigurieren</option>
            {#each accounts.filter((a) => a.id !== job.id && (a.draft.request.config.from || a.draft.request.config.username)) as account}
              <option value={account.id}
                >{account.draft.request.config.from ||
                  account.draft.request.config.username}</option
              >
            {/each}
          </select></label
        >
      {/if}
      <label class="account-choice"
        ><span>Versand über</span><select
          value={config.provider || ''}
          disabled={locked}
          onchange={(e) => void selectProvider(e.currentTarget.value)}
        >
          <option value="">SMTP</option><option value="google"
            >Google / Workspace</option
          >
        </select></label
      >
      {#if config.provider === 'google'}
        <div class="actions">
          <button
            class="plain"
            disabled={locked}
            onclick={() =>
              action(async () => {
                await desktopBackend().ImportGoogleClient();
                googleConfigured = true;
              })}
            >{googleConfigured
              ? 'OAuth-JSON ändern'
              : 'OAuth-JSON auswählen'}</button
          >
          <button disabled={locked || !googleConfigured} onclick={googleLogin}
            >Mit Google verbinden</button
          >
          {#if config.googleGrant}<button
              class="plain"
              disabled={locked}
              onclick={googleDisconnect}>Google-Verbindung entfernen</button
            >{/if}
        </div>
        <p class="hint">
          {config.googleGrant
            ? config.username
            : 'Google-Anmeldung im Browser. Nur E-Mails senden.'}
        </p>
        {#if busy}<p class="hint">
            Google-Anmeldung gegebenenfalls im Browser abschließen.
          </p>{/if}
      {/if}
      <fieldset disabled={locked}>
        <div class="grid">
          {#if config.provider !== 'google'}
            <label class="four"
              ><span>SMTP-Server</span><input
                bind:value={config.host}
                oninput={() => invalidate(true)}
                placeholder="smtp.example.com"
                autocomplete="off"
                spellcheck="false"
              /></label
            >
            <label class="two"
              ><span>Port</span><input
                type="number"
                min="1"
                max="65535"
                bind:value={config.port}
                oninput={() => invalidate(true)}
              /></label
            >
            <label class="three"
              ><span>Benutzer</span><input
                bind:value={config.username}
                oninput={() => invalidate(true)}
                autocomplete="username"
                spellcheck="false"
              /></label
            >
            <label class="three"
              ><span>Passwort</span><input
                type="password"
                bind:value={config.password}
                oninput={() => invalidate(true)}
                autocomplete="current-password"
              /></label
            >
          {/if}
          <label class="three"
            ><span>Absendername</span><input
              bind:value={config.fromName}
              oninput={() => invalidate(true)}
            /></label
          >
          {#if config.provider !== 'google'}<label class="three"
              ><span>Absenderadresse</span><input
                type="email"
                bind:value={config.from}
                oninput={() => invalidate(true)}
                placeholder="Leer = Benutzer"
              /></label
            >
          {/if}
          <label class="full"
            ><span>Antwortadresse (optional)</span><input
              type="email"
              bind:value={config.replyTo}
              oninput={() => invalidate(true)}
              placeholder="Leer = Absenderadresse"
            /></label
          >
        </div>
        {#if config.provider !== 'google'}<label class="check remember"
            ><input
              type="checkbox"
              bind:checked={config.remember}
              onchange={() => invalidate(true)}
            />Passwort im Betriebssystem-Schlüsselbund speichern</label
          >{/if}
      </fieldset>
      <div class="actions">
        <button disabled={locked} onclick={connect}>Verbindung prüfen</button
        >{#if connected}<span class="ok small"
            >{config.provider === 'google'
              ? 'Verbunden · Google'
              : 'Verbunden · TLS'}</span
          >{/if}
      </div>
      <div class="actions">
        <button
          class="plain"
          disabled={locked}
          onclick={() => action(saveDraft)}>Entwurf speichern</button
        ><span class="hint">{saveStatus}</span>
      </div>
      {#if config.provider !== 'google'}<p class="hint">
          Port 465: TLS · andere Ports: STARTTLS.
        </p>{/if}
    </section>
    <section aria-labelledby={uid + '-contacts-heading'}>
      <h2 id={uid + '-contacts-heading'}><b>02</b>Kontakte</h2>
      <div class="grid">
        <div class="full">
          <div class="label">Apollo-CSV</div>
          <div class="file">
            <button disabled={locked} onclick={choose}>Datei auswählen</button
            ><span>{report?.filename || 'Keine Datei ausgewählt'}</span>
          </div>
        </div>
        <div class="three checks">
          <label class="check"
            ><input
              type="checkbox"
              bind:checked={verified}
              disabled={locked || !report}
              onchange={filter}
            />Nur verifizierte</label
          >
        </div>
      </div>
      {#if report && !report.hasStatus}<p class="hint warning">
          Verifizierungsstatus nicht enthalten. {verified
            ? 'Zum Import „Nur verifizierte“ deaktivieren.'
            : 'Ohne Verifizierungsnachweis.'}
        </p>{/if}
      <div class="actions">
        <button
          disabled={locked || !report?.contacts.length}
          onclick={checkContacts}>Kontakte prüfen</button
        >{#if report}<span class="small" class:ok={contactsChecked}
            >{contactsChecked ? 'Geprüft · ' : ''}{report.contacts.length} verwendbar
            · {report.headers.length} Spalten ·
            {report.issues.length} ausgeschlossen</span
          >{/if}
      </div>
      {#if report?.previouslyContacted}<p class="hint warning">
          {report.previouslyContacted} Kontakte bereits angeschrieben. Identische
          Nachrichten bleiben jobübergreifend gesperrt.
        </p>{/if}
      {#if report}
        <div class="table-wrap">
          <table>
            <thead
              ><tr
                >{#each report.headers as h}<th>{h}</th>{/each}</tr
              ></thead
            ><tbody
              >{#each report.contacts.slice(0, 5) as contact}<tr
                  >{#each report.headers as h}<td>{contact.fields[h] || '—'}</td
                    >{/each}</tr
                >{/each}</tbody
            >
          </table>
        </div>
        {#if report.issues.length}<details>
            <summary
              >{report.issues.length} ausgeschlossene Einträge prüfen</summary
            >
            <ul>
              {#each report.issues as issue}<li>
                  Zeile {issue.row}: {issue.reason}
                </li>{/each}
            </ul>
          </details>{/if}
      {/if}
    </section>
    <section aria-labelledby={uid + '-message-heading'}>
      <h2 id={uid + '-message-heading'}><b>03</b>Nachricht</h2>
      <fieldset disabled={locked}>
        <div class="grid">
          <label class="full"
            ><span>Betreff</span><input
              bind:value={subject}
              oninput={() => invalidate()}
            /></label
          >
          <label class="full"
            ><span>Text</span><textarea
              bind:this={bodyInput}
              bind:value={body}
              oninput={() => invalidate()}></textarea></label
          >
        </div>
        <div class="tokens">
          {#each report?.headers || [] as column}<button
              class="token"
              onclick={() => insert(column)}>{'{{' + column + '}}'}</button
            >{/each}
        </div>
      </fieldset>
      <TemplateGuide headers={report?.headers} />
      <div class="actions">
        <button disabled={locked} onclick={() => action(preview)}
          >Nachrichten prüfen & Vorschau</button
        >{#if plan}<span class="small" class:ok={!plan.example}
            >{plan.example
              ? 'Beispielvorschau'
              : `Alle ${plan.messages.length} Nachrichten geprüft`}</span
          >{/if}
      </div>
      <div class="preview">
        <div class="preview-heading">
          <span class="label">Vorschau</span>
          {#if plan?.example}<span class="small muted"
              >Jane Doe · Beispieldaten</span
            >{:else}<div>
              <button
                class="plain"
                aria-label="Vorheriger Kontakt"
                disabled={!plan || index <= 0}
                onclick={() => index--}>←</button
              ><span class="small"
                >{plan ? index + 1 : 0} / {plan?.messages.length || 0}</span
              ><button
                class="plain"
                aria-label="Nächster Kontakt"
                disabled={!plan || index >= plan.messages.length - 1}
                onclick={() => index++}>→</button
              >
            </div>{/if}
        </div>
        {#if message}<dl>
            <dt>An</dt>
            <dd>{message.to}</dd>
            <dt>Betreff</dt>
            <dd>{message.subject}</dd>
          </dl>
          <p class="message">{message.body}</p>{:else}<p class="muted">
            Ohne CSV: Vorschau mit Jane Doe.
          </p>{/if}
      </div>
    </section>
    <section aria-labelledby={uid + '-test-heading'}>
      <h2 id={uid + '-test-heading'}><b>04</b>Testmail</h2>
      <div class="grid">
        <label class="four"
          ><span>Testmail an</span><input
            type="email"
            bind:value={testTo}
            disabled={locked}
            oninput={() => (tested = '')}
          /></label
        >
      </div>
      <p class="hint">
        Testmail nutzt den Vorschaukontakt. Vor dem Versand im Posteingang
        prüfen.
      </p>
      <div class="actions">
        <button
          disabled={locked || !connected || !contactsChecked || !testTo}
          onclick={test}>Testmail senden</button
        >{#if tested}<span class="ok small"
            >Testmail an {config.provider === 'google' ? 'Google' : 'SMTP'} übergeben
            · Posteingang prüfen</span
          >{/if}
      </div>
    </section>
    <section aria-labelledby={uid + '-send-heading'}>
      <h2 id={uid + '-send-heading'}><b>05</b>Versand</h2>
      <fieldset disabled={locked}>
        <div class="grid">
          <label class="two"
            ><span>Pause je E-Mail (Sek.)</span><input
              type="number"
              min="1"
              max="86400"
              bind:value={seconds}
              oninput={() => invalidate()}
            /></label
          >
          <label class="two"
            ><span>Batchgröße (max. 100)</span><input
              type="number"
              min="1"
              max="100"
              bind:value={batchSize}
              oninput={() => invalidate()}
            /></label
          >
          <label class="two"
            ><span>Pause je Batch (Stunden)</span><input
              type="number"
              min="0"
              max="24"
              step="any"
              bind:value={batchHours}
              oninput={() => invalidate()}
            /></label
          >
        </div>
      </fieldset>
      {#if report?.contacts.length}<p class="hint">
          {Math.ceil(report.contacts.length / Math.max(1, batchSize))} Batches
        </p>{/if}
      <p class="hint">Versand nur bei geöffneter App und wachem Rechner.</p>
      {#if accountBusy}<p class="hint warning">
          Absender belegt. Anderen Job zuerst pausieren oder beenden lassen.
        </p>{/if}
      <div class="actions">
        <button class="primary" disabled={!ready} onclick={start}
          >{deliveryState.plan?.id === plan?.id &&
          deliveryState.deliveries.length
            ? 'Fortsetzen'
            : 'Senden'}</button
        ><button
          class="plain"
          disabled={!deliveryState.running}
          onclick={() =>
            action(async () => {
              await backend().Stop();
              notice = 'Wird nach der laufenden Übertragung gestoppt.';
            })}>Stoppen</button
        >{#if busy}<span class="small muted" role="status">Wird geprüft …</span
          >{/if}
      </div>
      {#if deliveryState.error && !deliveryState.problem}<p
          class="error"
          role="alert"
        >
          {deliveryState.error}
        </p>{/if}
      {#if deliveryState.deliveries.length}<div class="progress">
          <div class="progress-heading">
            <span class="number"
              >{finished} / {deliveryState.deliveries.length}</span
            ><span class="small"
              >{deliveryState.running
                ? 'Versand aktiv'
                : 'Versand pausiert / beendet'} ·
              <span class="ok">{sent} übergeben</span></span
            >
          </div>
          <progress
            value={finished}
            max={deliveryState.deliveries.length}
            aria-label="Versandfortschritt"
          ></progress>
          {#if deliveryState.running && deliveryState.nextAt}<p class="hint">
              Nächste Freigabe frühestens {new Date(
                deliveryState.nextAt,
              ).toLocaleTimeString('de-DE')}.
            </p>{/if}
          <div class="log">
            {#each deliveryState.deliveries
              .filter((d) => d.status !== 'pending')
              .slice(-100)
              .reverse() as d}<div class="log-row">
                <span class="muted"
                  >{d.updated
                    ? new Date(d.updated).toLocaleTimeString('de-DE')
                    : ''}</span
                ><span>{d.to}</span><span
                  title={d.detail}
                  class:ok={d.status === 'sent'}
                  class:error={['failed', 'uncertain', 'blocked'].includes(
                    d.status,
                  )}>{statusLabel[d.status]}</span
                >
                {#if d.detail && ['failed', 'uncertain', 'blocked'].includes(d.status)}
                  <details class="delivery-error">
                    <summary>Fehlerdetails</summary>
                    <p>{d.detail}</p>
                  </details>
                {/if}
              </div>{/each}
          </div>
        </div>{/if}
    </section>
  </div>
</div>

<style>
  .job-name,
  .account-choice {
    margin-bottom: 24px;
  }
  .delivery-error {
    grid-column: 1 / -1;
    overflow-wrap: anywhere;
  }
  .delivery-error p {
    white-space: pre-wrap;
  }
</style>
