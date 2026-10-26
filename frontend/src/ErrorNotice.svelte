<script lang="ts">
  import type { Problem } from './api';
  let { problem }: { problem: Problem } = $props();
  let panel: HTMLDivElement;
  $effect(() => {
    const id = problem.id || problem.message;
    if (id && panel) {
      panel.focus({ preventScroll: true });
      panel.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' });
    }
  });
</script>

<div
  class="feedback error error-notice"
  role="alert"
  tabindex="-1"
  bind:this={panel}
>
  <strong>{problem.message}</strong>
  {#if problem.operation}<p>
      {problem.operation}{problem.step
        ? ` · ${problem.step}`
        : ''}{problem.smtpCode ? ` · SMTP ${problem.smtpCode}` : ''}
    </p>{/if}
  {#if problem.details}<p class="error-details">{problem.details}</p>{/if}
  {#if problem.hint}<p><b>Next step:</b> {problem.hint}</p>{/if}
  <details>
    <summary>Diagnostics{problem.id ? ` · Error ID ${problem.id}` : ''}</summary
    >
    <p>Code: {problem.code}</p>
    {#if problem.time}<p>
        Time: {new Date(problem.time).toLocaleString('en-US')}
      </p>{/if}
    {#if problem.logPath}<p>Log: {problem.logPath}</p>{/if}
    <p>
      {problem.logged
        ? 'Error logged locally.'
        : 'Could not log error locally.'}
    </p>
  </details>
  {#if problem.logIssue}<p><b>Logging error:</b> {problem.logIssue}</p>{/if}
</div>

<style>
  .error-notice {
    overflow-wrap: anywhere;
  }
  .error-notice p {
    margin: 8px 0;
  }
  .error-details {
    white-space: pre-wrap;
  }
  .error-notice details {
    color: inherit;
  }
</style>
