<script lang="ts">
  import { tick } from 'svelte';
  import { jobCounts, jobStatus, type JobView } from './api';
  let {
    jobs,
    tabs,
    active,
    busy,
    onSelect,
    onClose,
    onCreate,
  }: {
    jobs: JobView[];
    tabs: string[];
    active: string;
    busy: boolean;
    onSelect: (id: string) => void;
    onClose: (id: string) => void | Promise<void>;
    onCreate: () => void;
  } = $props();
  let strip: HTMLDivElement;
  const running = $derived(jobs.filter((j) => j.state.running).length);
  $effect(() => {
    const id = active;
    void tick().then(() => {
      const selected = strip?.querySelector<HTMLElement>(`[id="tab-${id}"]`);
      if (!selected || !strip) return;
      const bounds = selected.parentElement!.getBoundingClientRect();
      const viewport = strip.getBoundingClientRect();
      if (bounds.left < viewport.left)
        strip.scrollLeft -= viewport.left - bounds.left;
      else if (bounds.right > viewport.right)
        strip.scrollLeft += bounds.right - viewport.right;
    });
  });
  async function close(id: string) {
    await onClose(id);
    await tick();
    strip
      ?.querySelector<HTMLElement>('[aria-selected="true"]')
      ?.focus({ preventScroll: true });
  }
  function keys(event: KeyboardEvent) {
    const buttons = Array.from(
      strip.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
    );
    const current = buttons.indexOf(event.target as HTMLButtonElement);
    if (current < 0) return;
    if (event.key === 'Delete' && active !== 'overview') {
      event.preventDefault();
      void close(active);
      return;
    }
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    const next =
      event.key === 'Home'
        ? 0
        : event.key === 'End'
          ? buttons.length - 1
          : (current + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) %
            buttons.length;
    event.preventDefault();
    buttons[next].focus({ preventScroll: true });
    buttons[next].click();
  }
</script>

<nav class="job-tabs" aria-label="Offene Ansichten">
  <div class="tab-track">
    <div
      class="tab-list"
      role="tablist"
      tabindex="-1"
      aria-label="Jobs"
      bind:this={strip}
      onkeydown={keys}
    >
      <div class="tab overview" class:active={active === 'overview'}>
        <button
          class="tab-button"
          role="tab"
          id="tab-overview"
          aria-controls="panel-overview"
          aria-selected={active === 'overview'}
          tabindex={active === 'overview' ? 0 : -1}
          onclick={() => onSelect('overview')}
        >
          <span class="tab-title"
            ><svg
              aria-hidden="true"
              width="14"
              height="14"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.25"
              ><rect x="2" y="2" width="4.5" height="4.5" rx="1" /><rect
                x="9.5"
                y="2"
                width="4.5"
                height="4.5"
                rx="1"
              /><rect x="2" y="9.5" width="4.5" height="4.5" rx="1" /><rect
                x="9.5"
                y="9.5"
                width="4.5"
                height="4.5"
                rx="1"
              /></svg
            >Übersicht</span
          >
          <small
            >{running
              ? `${running} ${running === 1 ? 'Job läuft' : 'Jobs laufen'}`
              : `${jobs.filter((j) => !j.empty).length} Jobs`}</small
          >
        </button>
      </div>
      {#each tabs as id (id)}
        {@const job = jobs.find((j) => j.id === id)}
        {#if job}
          {@const counts = jobCounts(job)}
          {@const failed =
            !!job.state.error || counts.failed > 0 || counts.uncertain > 0}
          <div class="tab" class:active={active === id}>
            <button
              class="tab-button"
              role="tab"
              id={'tab-' + id}
              aria-controls={'panel-' + id}
              aria-selected={active === id}
              tabindex={active === id ? 0 : -1}
              title={job.name + ' · ' + jobStatus(job)}
              onclick={() => onSelect(id)}
            >
              <span class="tab-title">{job.name}</span>
              <small
                ><span
                  class="status-dot"
                  class:running={job.state.running}
                  class:failed
                  aria-hidden="true"
                ></span><span class="tab-status"
                  >{job.state.running
                    ? `${counts.done} / ${counts.total} · ${jobStatus(job)}`
                    : jobStatus(job)}</span
                ></small
              >
            </button>
            <button
              class="close-tab"
              tabindex={active === id ? 0 : -1}
              aria-label={'Tab schließen: ' + job.name}
              title={job.empty
                ? 'Leeren Entwurf schließen'
                : 'Tab schließen · Job bleibt erhalten'}
              onclick={() => void close(id)}
              ><svg
                aria-hidden="true"
                width="12"
                height="12"
                viewBox="0 0 16 16"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"><path d="m4 4 8 8M12 4l-8 8" /></svg
              ></button
            >
          </div>
        {/if}
      {/each}
    </div>
    <div class="add-slot">
      <button
        class="new-tab"
        aria-label="Neuen Job anlegen"
        title="Neuer Job"
        disabled={busy}
        onclick={onCreate}
        ><svg
          aria-hidden="true"
          width="18"
          height="18"
          viewBox="0 0 20 20"
          fill="none"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"><path d="M10 4v12M4 10h12" /></svg
        ></button
      >
    </div>
  </div>
</nav>

<style>
  .job-tabs {
    position: sticky;
    top: 0;
    z-index: 20;
    padding: 16px 0;
    margin: 0 0 36px;
    background: rgba(255, 255, 255, 0.96);
    backdrop-filter: blur(12px);
  }
  .tab-track {
    display: flex;
    min-width: 0;
    align-items: stretch;
    padding: 5px;
    gap: 6px;
    background: #f4f4f3;
    border: 1px solid #e9e9e7;
    border-radius: 14px;
  }
  .tab-list {
    display: flex;
    gap: 5px;
    min-width: 0;
    flex: 1;
    overflow-x: auto;
    scrollbar-width: thin;
    scrollbar-color: #d1d1ce transparent;
  }
  .tab {
    display: flex;
    flex: 0 0 192px;
    min-width: 0;
    align-items: center;
    border: 1px solid transparent;
    border-radius: 9px;
    transition:
      background 120ms ease,
      border-color 120ms ease;
  }
  .tab.overview {
    flex-basis: 140px;
  }
  .tab:hover {
    background: #eaeae8;
  }
  .tab.active {
    background: #fff;
    border-color: #dededb;
    box-shadow: 0 1px 3px #00000008;
  }
  .tab-button {
    border: 0;
    background: transparent;
    padding: 8px 12px;
    min-height: 50px;
    min-width: 0;
    flex: 1;
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 3px;
    text-align: left;
    border-radius: 8px;
    color: #656563;
  }
  .tab-button:hover:not(:disabled) {
    background: transparent;
    color: #171716;
  }
  .active .tab-button {
    color: #171716;
  }
  .tab-title {
    display: flex;
    align-items: center;
    gap: 8px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 13px;
    line-height: 18px;
    font-weight: 500;
  }
  .tab:not(.overview) .tab-title {
    display: block;
  }
  .tab-title svg {
    flex-shrink: 0;
  }
  small {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    max-width: 100%;
    font-size: 11px;
    line-height: 15px;
    font-weight: 400;
    color: #777773;
  }
  .tab-status {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .status-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: #b6b6b1;
    flex-shrink: 0;
  }
  .status-dot.running {
    background: #218552;
  }
  .status-dot.failed {
    background: #b3261e;
  }
  .close-tab {
    width: 28px;
    min-height: 28px;
    padding: 0;
    margin-right: 5px;
    flex-shrink: 0;
    border: 0;
    border-radius: 6px;
    color: #969691;
    display: grid;
    place-items: center;
  }
  .close-tab:hover:not(:disabled) {
    background: #e9e9e6;
    color: #262624;
  }
  .add-slot {
    display: flex;
    align-items: center;
    padding-left: 5px;
    border-left: 1px solid #e2e2df;
    flex-shrink: 0;
  }
  .new-tab {
    width: 38px;
    min-height: 42px;
    padding: 0;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 8px;
    color: #62625e;
  }
  .new-tab:hover:not(:disabled) {
    background: #e6e6e3;
    color: #171716;
  }
  button:focus-visible {
    outline: 2px solid #50504b;
    outline-offset: -2px;
  }
  @media (prefers-reduced-motion: reduce) {
    .tab {
      transition: none;
    }
  }
  @media (max-width: 680px) {
    .job-tabs {
      padding: 12px 0;
      margin-bottom: 28px;
    }
    .tab {
      flex-basis: 172px;
    }
    .tab.overview {
      flex-basis: 132px;
    }
  }
</style>
