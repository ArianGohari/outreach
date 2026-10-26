<script lang="ts">
  import { onMount, tick } from 'svelte';
  import JobTabs from './JobTabs.svelte';
  import JobEditor from './JobEditor.svelte';
  import ErrorNotice from './ErrorNotice.svelte';
  import {
    backend,
    jobCounts,
    jobStatus,
    type JobView,
    type Problem,
  } from './api';
  import { isProblem, errorText, desktopError } from './errors';
  let jobs = $state<JobView[]>([]);
  let active = $state('overview');
  let tabs = $state<string[]>([]);
  let mounted = $state<string[]>([]);
  let busy = $state(true);
  let error = $state<Problem | null>(null);
  let deleting = $state<string | null>(null);
  let refreshVersion = 0;
  let editors = $state<
    Record<string, { flushDraft: () => Promise<void> } | undefined>
  >({});
  const runningAccounts = $derived(
    jobs
      .filter((j) => j.state.running)
      .map((j) => ({
        id: j.id,
        address:
          j.state.plan?.request.config.from ||
          j.state.plan?.request.config.username ||
          '',
      })),
  );
  const dashboardJobs = $derived(jobs.filter((job) => !job.empty));
  const running = $derived(jobs.filter((j) => j.state.running).length);
  const accounts = $derived(
    jobs.filter((job, index, list) => {
      const address = (
        job.draft.request.config.from || job.draft.request.config.username
      ).toLowerCase();
      return (
        address &&
        list.findIndex(
          (j) =>
            (
              j.draft.request.config.from || j.draft.request.config.username
            ).toLowerCase() === address,
        ) === index
      );
    }),
  );
  function rememberTabs() {
    try {
      window.localStorage.setItem('outreach.openTabs', JSON.stringify(tabs));
    } catch {
      /* Tabs remain usable without browser storage. */
    }
  }
  function openJob(id: string) {
    if (!tabs.includes(id)) tabs = [...tabs, id];
    if (!mounted.includes(id)) mounted = [...mounted, id];
    active = id;
    rememberTabs();
  }
  function hideTab(id: string) {
    const index = tabs.indexOf(id);
    const neighbor = tabs[index + 1] || tabs[index - 1] || 'overview';
    tabs = tabs.filter((x) => x !== id);
    if (active === id) active = neighbor;
    rememberTabs();
  }
  async function showError(value: unknown) {
    value = desktopError(value);
    if (isProblem(value)) {
      if (!value.cancelled) error = value;
      return;
    }
    try {
      error = {
        ...(await backend().ReportClientError('UI_RUNTIME')),
        details: errorText(value),
      };
    } catch {
      error = {
        code: 'UI_RUNTIME',
        message: 'Could not load dashboard',
        details: errorText(value),
        logged: false,
        logIssue: 'Desktop connection unavailable.',
      };
    }
  }
  async function refresh() {
    const version = ++refreshVersion;
    try {
      const data = await backend().GetWorkspace();
      if (version === refreshVersion) jobs = data;
    } catch (e) {
      await showError(e);
    }
  }
  async function create() {
    if (busy) return;
    busy = true;
    error = null;
    try {
      for (const editor of Object.values(editors)) await editor?.flushDraft();
      const job = await backend().CreateJob('New job');
      jobs = [...jobs.filter((j) => j.id !== job.id), job];
      openJob(job.id);
    } catch (e) {
      await showError(e);
    } finally {
      busy = false;
    }
  }
  async function stop(id: string) {
    try {
      await backend().Stop(id);
      await refresh();
    } catch (e) {
      await showError(e);
    }
  }
  function accountRunning(address: string) {
    return jobs.some(
      (j) =>
        j.state.running &&
        (
          j.state.plan?.request.config.from ||
          j.state.plan?.request.config.username ||
          ''
        ).toLowerCase() === address.toLowerCase(),
    );
  }
  async function closeTab(id: string) {
    if (busy) return;
    busy = true;
    try {
      await editors[id]?.flushDraft();
      const current = await backend().GetWorkspace();
      if (current.find((job) => job.id === id)?.empty) {
        await backend().DiscardEmptyJob(id);
        mounted = mounted.filter((x) => x !== id);
        delete editors[id];
        jobs = current.filter((job) => job.id !== id);
      } else jobs = current;
      hideTab(id);
      await refresh();
    } catch (e) {
      await showError(e);
    } finally {
      busy = false;
    }
  }
  async function confirmDeletion(id: string, confirm: boolean) {
    deleting = confirm ? id : null;
    await tick();
    document
      .getElementById((confirm ? 'cancel-delete-' : 'delete-') + id)
      ?.focus();
  }
  async function deleteJob(id: string) {
    if (busy) return;
    busy = true;
    error = null;
    try {
      await editors[id]?.flushDraft();
      await backend().DeleteJob(id);
      ++refreshVersion;
      mounted = mounted.filter((x) => x !== id);
      delete editors[id];
      jobs = jobs.filter((job) => job.id !== id);
      hideTab(id);
      await refresh();
      await tick();
      document.getElementById('tab-overview')?.focus();
    } catch (e) {
      await showError(e);
    } finally {
      deleting = null;
      busy = false;
    }
  }
  onMount(() => {
    let alive = true;
    void (async () => {
      await refresh();
      if (!alive) return;
      try {
        const saved = JSON.parse(
          window.localStorage.getItem('outreach.openTabs') || '[]',
        );
        if (Array.isArray(saved))
          tabs = saved.filter(
            (id: unknown) =>
              typeof id === 'string' && jobs.some((j) => j.id === id),
          );
      } catch {}
      mounted = [...tabs];
      busy = false;
    })();
    const off = window.runtime?.EventsOn('outreach:changed', () => {
      void refresh();
    });
    const offError = window.runtime?.EventsOn('outreach:error', (p) => {
      if (p) void showError(p);
    });
    const onError = (event: ErrorEvent) => {
      void showError(event.error || event.message);
    };
    const onReject = (event: PromiseRejectionEvent) => {
      void showError(event.reason);
    };
    window.addEventListener('error', onError);
    window.addEventListener('unhandledrejection', onReject);
    const timer = setInterval(() => {
      if (running) void refresh();
    }, 1500);
    return () => {
      alive = false;
      off?.();
      offError?.();
      clearInterval(timer);
      window.removeEventListener('error', onError);
      window.removeEventListener('unhandledrejection', onReject);
    };
  });
</script>

<main class="workspace">
  <JobTabs
    {jobs}
    {tabs}
    {active}
    {busy}
    onSelect={(id) => (active = id)}
    onClose={closeTab}
    onCreate={create}
  />
  {#if error}<ErrorNotice problem={error} />{/if}
  <div
    role="tabpanel"
    id="panel-overview"
    aria-labelledby="tab-overview"
    hidden={active !== 'overview'}
  >
    <header>
      <h1>
        <img
          class="wordmark"
          src="/brand/wordmark.svg"
          alt="Outreach"
          width="152"
          height="40"
        />
      </h1>
    </header>
    <section aria-labelledby="jobs-heading">
      <h2 id="jobs-heading"><b>01</b>Jobs</h2>
      {#if busy && !dashboardJobs.length}<p class="muted" role="status">
          Loading jobs...
        </p>
      {:else if !dashboardJobs.length}<p class="muted">No jobs yet.</p>
      {:else}<div class="dashboard-list">
          {#each dashboardJobs as job (job.id)}
            {@const counts = jobCounts(job)}
            <div class="job-row">
              <button class="dashboard-row" onclick={() => openJob(job.id)}>
                <span
                  ><span>{job.name}</span><small
                    >{job.draft.request.config.from ||
                      job.draft.request.config.username ||
                      'Sender not configured'}</small
                  ></span
                >
                <small
                  class:error={!!job.state.error ||
                    !!counts.uncertain ||
                    !!counts.failed}
                  >{jobStatus(
                    job,
                  )}{#if job.state.running && job.state.nextAt && new Date(job.state.nextAt).getTime() > Date.now()}<span
                      class="next-time"
                      >until {new Date(job.state.nextAt).toLocaleTimeString(
                        'en-US',
                      )}</span
                    >{/if}</small
                >
                <span class="row-progress"
                  >{counts.done} / {counts.total}<progress
                    value={counts.done}
                    max={Math.max(counts.total, 1)}
                    aria-label={'Progress ' + job.name}
                  ></progress></span
                >
              </button>
              <div class="row-actions">
                {#if deleting === job.id}
                  <span class="small"
                    >Delete job? Sending history will be kept.</span
                  >
                  <button
                    class="plain"
                    id={'cancel-delete-' + job.id}
                    disabled={busy}
                    onclick={() => confirmDeletion(job.id, false)}
                    >Cancel</button
                  >
                  <button
                    class="plain delete"
                    disabled={busy || job.state.running}
                    aria-label={'Confirm deletion: ' + job.name}
                    onclick={() => deleteJob(job.id)}>Delete</button
                  >
                {:else}
                  {#if job.state.running}<button
                      class="plain"
                      aria-label={'Pause: ' + job.name}
                      onclick={() => stop(job.id)}>Pause</button
                    >{/if}
                  <button
                    class="plain delete"
                    id={'delete-' + job.id}
                    disabled={busy || job.state.running}
                    aria-label={'Delete job: ' + job.name}
                    title={job.state.running ? 'Pause job first' : 'Delete job'}
                    onclick={() => confirmDeletion(job.id, true)}>Delete</button
                  >
                {/if}
              </div>
            </div>
          {/each}
        </div>{/if}
    </section>
    {#if accounts.length}
      <section aria-labelledby="accounts-heading">
        <h2 id="accounts-heading"><b>02</b>Accounts</h2>
        <div class="dashboard-list">
          {#each accounts as job (job.id)}
            {@const config = job.draft.request.config}
            {@const address = config.from || config.username}
            <button
              class="dashboard-row account-row"
              onclick={() => openJob(job.id)}
              ><span
                >{config.fromName || address}<small
                  >{address} · {config.provider === 'google'
                    ? 'Google / Workspace'
                    : config.host || 'SMTP server missing'}</small
                ></span
              ><small
                >{accountRunning(address)
                  ? 'Busy · Job running'
                  : 'Available'}</small
              ><span class="small">Open →</span></button
            >
          {/each}
        </div>
      </section>
    {/if}
  </div>
  {#each mounted as id (id)}
    {@const job = jobs.find((j) => j.id === id)}
    {#if job}<div
        role="tabpanel"
        id={'panel-' + id}
        aria-labelledby={'tab-' + id}
        hidden={active !== id}
      >
        <JobEditor
          bind:this={editors[id]}
          {job}
          {accounts}
          {runningAccounts}
          onChanged={() => void refresh()}
        />
      </div>{/if}
  {/each}
</main>

<style>
  .wordmark {
    display: block;
    max-width: 100%;
    object-fit: contain;
    object-position: left;
  }
  .workspace {
    padding-top: 0;
  }

  .dashboard-list {
    border-top: 1px solid #e6e6e6;
  }
  .job-row {
    border-bottom: 1px solid #e6e6e6;
    position: relative;
  }
  .dashboard-row {
    width: 100%;
    border: 0;
    padding: 16px 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 190px) 100px;
    gap: 16px;
    align-items: center;
    text-align: left;
    font-weight: 400;
  }
  .dashboard-row small {
    display: block;
    color: #6b6b6b;
    font-size: 12px;
    line-height: 18px;
    overflow-wrap: anywhere;
  }
  .dashboard-row small.error {
    color: #b3261e;
  }
  .dashboard-row > span {
    overflow-wrap: anywhere;
  }
  .row-progress {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .row-progress progress {
    margin: 8px 0 0;
    height: 1px;
  }
  .row-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
    margin: -4px 0 8px;
  }
  .row-actions button {
    font-size: 12px;
    min-height: 32px;
  }
  .delete:not(:disabled) {
    color: #b3261e;
  }
  .next-time {
    display: block;
  }
  .account-row {
    border-bottom: 1px solid #e6e6e6;
  }
  .account-row > span:last-child {
    text-align: right;
  }

  @media (max-width: 680px) {
    .dashboard-row {
      grid-template-columns: minmax(0, 1fr) 130px 72px;
      gap: 12px;
    }
  }
</style>
