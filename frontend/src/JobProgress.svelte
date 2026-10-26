<script lang="ts">
  const uid = $props.id();
  import { statusLabel, type State } from './api';
  let {
    state,
    busy,
    onStop,
    onEdit,
  }: { state: State; busy: boolean; onStop: () => void; onEdit: () => void } =
    $props();
  const counts = $derived({
    sent: state.deliveries.filter((d) => d.status === 'sent').length,
    pending: state.deliveries.filter((d) =>
      ['pending', 'sending'].includes(d.status),
    ).length,
    failed: state.deliveries.filter((d) => d.status === 'failed').length,
    blocked: state.deliveries.filter((d) => d.status === 'blocked').length,
    uncertain: state.deliveries.filter((d) => d.status === 'uncertain').length,
  });
  const finished = $derived(state.deliveries.length - counts.pending);
</script>

<section aria-labelledby={uid + '-job-progress-heading'}>
  <h2 id={uid + '-job-progress-heading'}><b>01</b>Fortschritt</h2>
  <div class="progress-heading">
    <span class="number">{finished} / {state.deliveries.length}</span><span
      class="small">{state.running ? 'Job läuft' : 'Pausiert / beendet'}</span
    >
  </div>
  <progress
    value={finished}
    max={Math.max(state.deliveries.length, 1)}
    aria-label="Versandfortschritt"
  ></progress>
  <div class="counts">
    {#each [{ n: counts.sent, label: 'Übergeben', kind: 'ok' }, { n: counts.pending, label: 'Offen', kind: '' }, { n: counts.failed, label: 'Fehlgeschlagen', kind: 'error' }, { n: counts.blocked, label: 'Duplikate gesperrt', kind: '' }, { n: counts.uncertain, label: 'Unklar', kind: 'error' }] as item}
      <div>
        <span class={'number ' + item.kind}>{item.n}</span><span class="hint"
          >{item.label}</span
        >
      </div>
    {/each}
  </div>
  {#if state.running && state.nextAt}<p class="hint">
      Nächste Freigabe frühestens {new Date(state.nextAt).toLocaleString(
        'de-DE',
      )}.
    </p>{/if}
  <div class="actions">
    <button disabled={busy || !state.running} onclick={onStop}>Stoppen</button>
    <button class="plain" disabled={busy || state.running} onclick={onEdit}
      >Job bearbeiten & fortsetzen</button
    >
  </div>
  {#if counts.uncertain}<p class="hint warning">
      {counts.uncertain} unklar: möglicherweise bereits übertragen. Erneuter Versand
      gesperrt.
    </p>{/if}
</section>
<section aria-labelledby={uid + '-job-log-heading'}>
  <h2 id={uid + '-job-log-heading'}><b>02</b>Protokoll</h2>
  {#if !state.deliveries.some((d) => d.status !== 'pending')}<p class="hint">
      Noch keine Übertragungen.
    </p>{/if}
  <div class="log">
    {#each state.deliveries
      .filter((d) => d.status !== 'pending')
      .slice(-100)
      .reverse() as d}
      <div class="log-row">
        <span class="muted"
          >{d.updated
            ? new Date(d.updated).toLocaleTimeString('de-DE')
            : ''}</span
        ><span>{d.to}</span><span
          class:ok={d.status === 'sent'}
          class:error={['failed', 'uncertain'].includes(d.status)}
          >{statusLabel[d.status]}</span
        >
        {#if d.detail && d.status !== 'sent'}<details>
            <summary>Details</summary>
            <p>{d.detail}</p>
          </details>{/if}
      </div>
    {/each}
  </div>
</section>
{#if state.plan}
  <section aria-labelledby={uid + '-job-message-heading'}>
    <h2 id={uid + '-job-message-heading'}><b>03</b>Nachricht</h2>
    <dl>
      <dt>Absender</dt>
      <dd>
        {state.plan.request.config.from || state.plan.request.config.username}
      </dd>
      <dt>Betreff</dt>
      <dd>{state.plan.request.subject}</dd>
    </dl>
    <p class="message">{state.plan.request.body}</p>
    <p class="hint schedule">
      {state.plan.request.schedule.seconds} Sek. Pause · Batches à {state.plan
        .request.schedule.batchSize} · {state.plan.request.schedule
        .batchMinutes / 60} Stunden Batchpause
    </p>
  </section>
{/if}

<style>
  .counts {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 16px;
    margin: 24px 0;
  }
  .counts > div {
    display: flex;
    flex-direction: column;
  }
  .counts .number {
    font-size: 24px;
  }
  .log-row details {
    grid-column: 1/-1;
    margin-top: 0;
  }
  .log-row details p {
    white-space: pre-wrap;
  }
  .schedule {
    margin-top: 24px;
  }
  @media (max-width: 680px) {
    .counts {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }
</style>
