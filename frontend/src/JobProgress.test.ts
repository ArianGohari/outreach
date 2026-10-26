import { describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import JobProgress from './JobProgress.svelte';
import type { Delivery, Plan, State } from './api';

function delivery(status: string, key = status): Delivery {
  return {
    key,
    to: `${key}@example.org`,
    status,
    detail: '',
    updated: '2026-10-26T10:00:00Z',
  };
}
function state(deliveries: Delivery[] = []): State {
  return {
    running: false,
    nextAt: '',
    error: '',
    deliveries,
    plan: {
      id: 'plan',
      messages: [],
      request: {
        config: { username: 'sender@example.org' },
        subject: 'Hello {{First Name}}',
        body: 'Synthetic template',
        schedule: { seconds: 30, batchSize: 50, batchMinutes: 1440 },
      },
      report: { issues: [] },
    } as unknown as Plan,
  };
}
function show(value: State) {
  return render(JobProgress, {
    state: value,
    busy: false,
    onStop: vi.fn(),
    onEdit: vi.fn(),
  });
}

describe('recipient results', () => {
  it('shows every outcome and separates unattempted, active and accepted messages', () => {
    const value = state(
      ['sent', 'pending', 'sending', 'failed', 'uncertain', 'blocked'].map(
        (s) => delivery(s),
      ),
    );
    value.deliveries[3].detail =
      'SMTP RCPT TO: 550 recipient rejected · Error ID synthetic-1';
    value.deliveries[4].detail =
      'Connection lost after DATA; acknowledgement missing';
    show(value);
    const table = screen.getByRole('table');
    for (const d of value.deliveries) {
      const row = screen.getByText(d.to).closest('tr')!;
      expect(row.textContent).toContain(
        {
          sent: 'Accepted by provider',
          pending: 'Not attempted',
          sending: 'Sending',
          failed: 'Failed',
          uncertain: 'Uncertain · Locked',
          blocked: 'Duplicate prevented',
        }[d.status],
      );
    }
    expect(
      within(table).getByText(value.deliveries[3].detail).closest('details'),
    ).toBeNull();
    expect(within(table).getByText(value.deliveries[4].detail)).toBeTruthy();
    expect(screen.getByText(/4 \/ 6/).textContent).toContain('processed');
    expect(screen.getByText(/inbox delivery is not confirmed/)).toBeTruthy();
    const counters = screen
      .getByText('Accepted', { exact: true })
      .closest('.counts')!;
    expect(counters.children).toHaveLength(6);
    for (const count of counters.children)
      expect(count.textContent).toMatch(/^1/);
  });

  it('makes more than 100 records reachable and resets pagination when filtering', async () => {
    const value = state(
      Array.from({ length: 121 }, (_, i) =>
        delivery(i === 120 ? 'failed' : 'pending', `person-${i}`),
      ),
    );
    show(value);
    expect(
      screen.getByRole('status', { name: 'Recipient range' }).textContent,
    ).toBe('1–50 of 121');
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(screen.getByText('person-120@example.org')).toBeTruthy();
    expect(
      screen.getByRole('status', { name: 'Recipient range' }).textContent,
    ).toBe('101–121 of 121');
    expect(
      (screen.getByRole('button', { name: 'Next' }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    await fireEvent.change(screen.getByLabelText('Status'), {
      target: { value: 'failed' },
    });
    expect(
      screen.getByRole('status', { name: 'Recipient range' }).textContent,
    ).toBe('1–1 of 1 matching · 121 total');
    expect(screen.getAllByRole('row')).toHaveLength(2);
    await fireEvent.change(screen.getByLabelText('Status'), {
      target: { value: '' },
    });
    await fireEvent.input(screen.getByLabelText('Find recipient'), {
      target: { value: ' PERSON-0@EXAMPLE.ORG ' },
    });
    expect(screen.getByText('person-0@example.org')).toBeTruthy();
    await fireEvent.input(screen.getByLabelText('Find recipient'), {
      target: { value: 'absent' },
    });
    expect(screen.getByText('No matching recipients.')).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('updates results without hiding pending recipients or retaining an out-of-range page', async () => {
    const value = state(
      Array.from({ length: 51 }, (_, i) => delivery('pending', `person-${i}`)),
    );
    const component = show(value);
    await fireEvent.change(screen.getByLabelText('Status'), {
      target: { value: 'pending' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    const next = structuredClone(value);
    next.deliveries[50].status = 'sent';
    await component.rerender({ state: next });
    expect(
      screen.getByRole('status', { name: 'Recipient range' }).textContent,
    ).toBe('1–50 of 50 matching · 51 total');
    expect(screen.getByText('person-0@example.org')).toBeTruthy();
  });

  it('keeps persisted failure details visible after the component is reopened', () => {
    const value = state([delivery('failed'), delivery('pending')]);
    value.deliveries[0].detail =
      'Authentication rejected · Error ID persisted-1';
    const first = show(value);
    first.unmount();
    show(structuredClone(value));
    expect(screen.getByText(value.deliveries[0].detail)).toBeTruthy();
    expect(screen.getByRole('status', { name: 'Job status' }).textContent).toBe(
      'Paused · Check errors',
    );
    expect(screen.getByText('pending@example.org')).toBeTruthy();
  });

  it('distinguishes excluded CSV rows from sending results', async () => {
    const value = state([delivery('sent')]);
    value.plan!.report.issues = [{ row: 3, reason: 'Missing email address' }];
    show(value);
    expect(screen.getByText('1 CSV rows excluded before sending')).toBeTruthy();
    expect(screen.getByText('Row 3: Missing email address')).toBeTruthy();
    expect(screen.getAllByRole('row')).toHaveLength(2);
  });
});

describe('job status', () => {
  it.each([
    ['Not started', { plan: null }],
    ['Paused', {}],
    ['Starting', { running: true }],
    [
      'Waiting · Sending pause',
      { running: true, nextAt: '2099-01-01T00:00:00Z' },
    ],
    [
      'Sending',
      {
        running: true,
        deliveries: [delivery('sending')],
        nextAt: '2099-01-01T00:00:00Z',
      },
    ],
    ['Completed', { deliveries: [delivery('sent'), delivery('blocked')] }],
    ['Paused', { deliveries: [delivery('sent'), delivery('pending')] }],
    ['Paused · Check errors', { deliveries: [delivery('failed')] }],
    ['Paused · Check errors', { deliveries: [delivery('uncertain')] }],
    ['Paused · Check errors', { error: 'Database unavailable' }],
  ] as [string, Partial<State>][])('shows %s for %j', (label, patch) => {
    show({ ...state(), ...patch });
    expect(screen.getByRole('status', { name: 'Job status' }).textContent).toBe(
      label,
    );
    expect(!!screen.queryByText(/Next send no earlier/)).toBe(
      label === 'Waiting · Sending pause',
    );
  });
});
