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
  <h2 id={uid + '-job-progress-heading'}><b>01</b>Progress</h2>
  <div class="progress-heading">
    <span class="number">{finished} / {state.deliveries.length}</span><span
      class="small">{state.running ? 'Job running' : 'Paused / Finished'}</span
    >
  </div>
  <progress
    value={finished}
    max={Math.max(state.deliveries.length, 1)}
    aria-label="Sending progress"
  ></progress>
  <div class="counts">
    {#each [{ n: counts.sent, label: 'Accepted', kind: 'ok' }, { n: counts.pending, label: 'Pending', kind: '' }, { n: counts.failed, label: 'Failed', kind: 'error' }, { n: counts.blocked, label: 'Duplicates blocked', kind: '' }, { n: counts.uncertain, label: 'Uncertain', kind: 'error' }] as item}
      <div>
        <span class={'number ' + item.kind}>{item.n}</span><span class="hint"
          >{item.label}</span
        >
      </div>
    {/each}
  </div>
  {#if state.running && state.nextAt}<p class="hint">
      Next send no earlier than {new Date(state.nextAt).toLocaleString(
        'en-US',
      )}.
    </p>{/if}
  <div class="actions">
    <button disabled={busy || !state.running} onclick={onStop}>Stop</button>
    <button class="plain" disabled={busy || state.running} onclick={onEdit}
      >Edit & resume job</button
    >
  </div>
  {#if counts.uncertain}<p class="hint warning">
      {counts.uncertain} uncertain: may already have been sent. Sending again is blocked.
    </p>{/if}
</section>
<section aria-labelledby={uid + '-job-log-heading'}>
  <h2 id={uid + '-job-log-heading'}><b>02</b>Log</h2>
  {#if !state.deliveries.some((d) => d.status !== 'pending')}<p class="hint">
      No transfers yet.
    </p>{/if}
  <div class="log">
    {#each state.deliveries
      .filter((d) => d.status !== 'pending')
      .slice(-100)
      .reverse() as d}
      <div class="log-row">
        <span class="muted"
          >{d.updated
            ? new Date(d.updated).toLocaleTimeString('en-US')
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
    <h2 id={uid + '-job-message-heading'}><b>03</b>Message</h2>
    <dl>
      <dt>Sender</dt>
      <dd>
        {state.plan.request.config.from || state.plan.request.config.username}
      </dd>
      <dt>Subject</dt>
      <dd>{state.plan.request.subject}</dd>
    </dl>
    <p class="message">{state.plan.request.body}</p>
    <p class="hint schedule">
      {state.plan.request.schedule.seconds} sec. pause · Batch size {state.plan
        .request.schedule.batchSize} · {state.plan.request.schedule
        .batchMinutes / 60} hours between batches
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
