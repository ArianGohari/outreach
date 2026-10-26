<script lang="ts">
  const uid = $props.id();
  import { statusLabel, type State } from './api';
  let {
    state: jobState,
    busy,
    onStop,
    onEdit,
  }: {
    state: State;
    busy: boolean;
    onStop: () => void;
    onEdit: () => void;
  } = $props();
  let search = $state('');
  let status = $state('');
  let page = $state(0);
  const pageSize = 50;
  const counts = $derived({
    sent: jobState.deliveries.filter((d) => d.status === 'sent').length,
    pending: jobState.deliveries.filter((d) => d.status === 'pending').length,
    sending: jobState.deliveries.filter((d) => d.status === 'sending').length,
    failed: jobState.deliveries.filter((d) => d.status === 'failed').length,
    blocked: jobState.deliveries.filter((d) => d.status === 'blocked').length,
    uncertain: jobState.deliveries.filter((d) => d.status === 'uncertain')
      .length,
  });
  const finished = $derived(
    counts.sent + counts.failed + counts.blocked + counts.uncertain,
  );
  const waiting = $derived(
    jobState.running &&
      !counts.sending &&
      !!jobState.nextAt &&
      new Date(jobState.nextAt).getTime() > Date.now(),
  );
  const summary = $derived(
    jobState.running
      ? counts.sending
        ? 'Sending'
        : waiting
          ? 'Waiting · Sending pause'
          : 'Starting'
      : jobState.problem || jobState.error || counts.failed || counts.uncertain
        ? 'Paused · Check errors'
        : jobState.deliveries.length && finished === jobState.deliveries.length
          ? 'Completed'
          : jobState.plan
            ? 'Paused'
            : 'Not started',
  );
  const filtered = $derived(
    jobState.deliveries.filter(
      (d) =>
        (!status || d.status === status) &&
        d.to.toLowerCase().includes(search.trim().toLowerCase()),
    ),
  );
  const lastPage = $derived(
    Math.max(0, Math.ceil(filtered.length / pageSize) - 1),
  );
  const currentPage = $derived(Math.min(page, lastPage));
  const visible = $derived(
    filtered.slice(currentPage * pageSize, (currentPage + 1) * pageSize),
  );
</script>

<section aria-labelledby={uid + '-job-progress-heading'}>
  <h2 id={uid + '-job-progress-heading'}><b>01</b>Progress</h2>
  <div class="progress-heading">
    <span class="number"
      >{finished} / {jobState.deliveries.length}
      <span class="small muted">processed</span></span
    >
    <span class="small" role="status" aria-label="Job status">{summary}</span>
  </div>
  <progress
    value={finished}
    max={Math.max(jobState.deliveries.length, 1)}
    aria-label="Sending progress"
  ></progress>
  <div class="counts">
    {#each [{ n: counts.sent, label: 'Accepted', kind: 'ok' }, { n: counts.pending, label: 'Not attempted', kind: '' }, { n: counts.sending, label: 'Sending', kind: '' }, { n: counts.failed, label: 'Failed', kind: 'error' }, { n: counts.uncertain, label: 'Uncertain', kind: 'error' }, { n: counts.blocked, label: 'Duplicates blocked', kind: '' }] as item}
      <div>
        <span class={'number ' + item.kind}>{item.n}</span><span class="hint"
          >{item.label}</span
        >
      </div>
    {/each}
  </div>
  {#if waiting}<p class="hint">
      Next send no earlier than {new Date(jobState.nextAt).toLocaleString(
        'en-US',
      )}.
    </p>{/if}
  {#if jobState.error && !jobState.problem}<p class="error" role="alert">
      {jobState.error}
    </p>{/if}
  {#if counts.failed || counts.uncertain}<p class="hint warning">
      Check the affected recipients below. Attempted messages cannot be retried.
    </p>{/if}
  {#if counts.uncertain}<p class="hint warning">
      Uncertain messages may already have been sent. Sending again is blocked.
    </p>{/if}
  <div class="actions">
    <button disabled={busy || !jobState.running} onclick={onStop}>Stop</button>
    <button class="plain" disabled={busy || jobState.running} onclick={onEdit}
      >Edit job</button
    >
  </div>
</section>
<section aria-labelledby={uid + '-job-recipients-heading'}>
  <h2 id={uid + '-job-recipients-heading'}><b>02</b>Recipients</h2>
  <p class="hint">
    Current sending plan. Accepted means the provider accepted the email; inbox
    delivery is not confirmed.
  </p>
  <div class="filters">
    <label
      ><span>Find recipient</span><input
        type="search"
        bind:value={search}
        oninput={() => (page = 0)}
        placeholder="Email address"
      /></label
    >
    <label
      ><span>Status</span><select
        bind:value={status}
        onchange={() => (page = 0)}
      >
        <option value="">All statuses</option>
        {#each Object.entries(statusLabel) as [value, label]}<option {value}
            >{label}</option
          >{/each}
      </select></label
    >
  </div>
  {#if !jobState.deliveries.length}
    <p class="hint">No outreach has started for this job.</p>
  {:else if !filtered.length}
    <p class="hint">No matching recipients.</p>
  {:else}
    <!-- svelte-ignore a11y_no_noninteractive_tabindex (Keyboard access to horizontally scrollable results.) -->
    <div
      class="table-wrap"
      tabindex="0"
      role="region"
      aria-label="Recipient results"
    >
      <table>
        <thead
          ><tr
            ><th scope="col">Recipient</th><th scope="col">Status & details</th
            ><th scope="col">Updated</th></tr
          ></thead
        >
        <tbody>
          {#each visible as d (d.key)}
            <tr>
              <td>{d.to}</td>
              <td>
                <span
                  class:ok={d.status === 'sent'}
                  class:error={['failed', 'uncertain'].includes(d.status)}
                  >{statusLabel[d.status] || 'Unknown status'}</span
                >
                {#if d.detail && d.status !== 'sent'}<p
                    class="recipient-detail"
                  >
                    {d.detail}
                  </p>{/if}
                {#if d.status === 'pending'}<p class="recipient-detail muted">
                    No sending attempt yet.
                  </p>{/if}
                {#if d.status === 'blocked'}<p class="recipient-detail muted">
                    No new sending attempt. A matching message is already
                    recorded.
                  </p>{/if}
              </td>
              <td class="muted"
                >{d.updated
                  ? new Date(d.updated).toLocaleString('en-US')
                  : '—'}</td
              >
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <div class="pagination">
      <span class="small muted" role="status" aria-label="Recipient range"
        >{currentPage * pageSize + 1}–{Math.min(
          (currentPage + 1) * pageSize,
          filtered.length,
        )} of {filtered.length}{status || search.trim()
          ? ` matching · ${jobState.deliveries.length} total`
          : ''}</span
      >
      {#if lastPage > 0}<div class="actions">
          <button
            class="plain"
            disabled={currentPage === 0}
            onclick={() => (page = currentPage - 1)}>Previous</button
          >
          <button
            class="plain"
            disabled={currentPage === lastPage}
            onclick={() => (page = currentPage + 1)}>Next</button
          >
        </div>{/if}
    </div>
  {/if}
  {#if jobState.plan?.report.issues.length}<details>
      <summary
        >{jobState.plan.report.issues.length} CSV rows excluded before sending</summary
      >
      {#each jobState.plan.report.issues as issue}<p class="hint">
          Row {issue.row}: {issue.reason}
        </p>{/each}
    </details>{/if}
</section>
{#if jobState.plan}
  <section aria-labelledby={uid + '-job-message-heading'}>
    <h2 id={uid + '-job-message-heading'}><b>03</b>Message</h2>
    <dl>
      <dt>Sender</dt>
      <dd>
        {jobState.plan.request.config.from ||
          jobState.plan.request.config.username}
      </dd>
      <dt>Subject template</dt>
      <dd>{jobState.plan.request.subject}</dd>
    </dl>
    <p class="message">{jobState.plan.request.body}</p>
    <p class="hint schedule">
      {jobState.plan.request.schedule.seconds} sec. pause · Batch size {jobState
        .plan.request.schedule.batchSize} · {jobState.plan.request.schedule
        .batchMinutes / 60} hours between batches
    </p>
  </section>
{/if}

<style>
  .counts {
    display: grid;
    grid-template-columns: repeat(6, minmax(0, 1fr));
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
  .filters {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px;
    margin-top: 24px;
  }
  table {
    table-layout: fixed;
    min-width: 480px;
  }
  th:first-child {
    width: 32%;
  }
  th:last-child {
    width: 22%;
  }
  td {
    vertical-align: top;
  }
  .recipient-detail {
    margin: 6px 0 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .pagination {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
    margin-top: 16px;
  }
  .pagination .actions {
    margin: 0;
  }
  .schedule {
    margin-top: 24px;
  }
  @media (max-width: 680px) {
    .counts {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .filters {
      grid-template-columns: 1fr;
    }
    .pagination {
      flex-wrap: wrap;
    }
  }
</style>
