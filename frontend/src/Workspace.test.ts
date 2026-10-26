import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import App from './App.svelte';
import { jobStatus, jobCounts, type JobView, type Bridge } from './api';
const job = (id: string, name: string, address: string): JobView => ({
  id,
  name,
  draft: {
    request: {
      config: {
        host: 'smtp.example.org',
        port: 587,
        username: address,
        password: '',
        from: '',
        fromName: '',
        replyTo: '',
        remember: false,
      },
      subject: 'Subject ' + name,
      body: 'Hi {{First Name}}',
      schedule: { seconds: 30, batchSize: 50, batchMinutes: 1440 },
    },
    report: {
      filename: '',
      headers: [],
      contacts: [],
      issues: [],
      total: 0,
      hasStatus: false,
      onlyVerified: true,
    },
  },
  state: { plan: null, deliveries: [], running: false, nextAt: '', error: '' },
});
let api: Bridge;
let jobs: JobView[];
beforeEach(() => {
  const storage = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
    clear: () => storage.clear(),
  });
  jobs = [
    job('a', 'Alpha', 'a@example.org'),
    job('b', 'Beta', 'b@example.org'),
  ];
  api = {
    GetWorkspace: vi.fn(async () => structuredClone(jobs)),
    DiscardEmptyJob: vi.fn().mockResolvedValue(undefined),
    DeleteJob: vi.fn(async (id) => {
      jobs = jobs.filter((j) => j.id !== id);
    }),
    CreateJob: vi.fn(async (name) => {
      const j = job('new', name, '');
      jobs.push(j);
      return j;
    }),
    SaveDraft: vi.fn(async (id, name, request) => {
      const j = jobs.find((j) => j.id === id)!;
      j.name = name;
      j.draft.request = structuredClone(request);
      j.draft.request.config.password = '';
    }),
    GetState: vi.fn(async (id) =>
      structuredClone(jobs.find((j) => j.id === id)!.state),
    ),
    ChooseCSV: vi.fn(),
    FilterContacts: vi.fn(),
    Preview: vi.fn(),
    CheckConnection: vi.fn(),
    LoadPassword: vi.fn(),
    SendTest: vi.fn(),
    Start: vi.fn(),
    Stop: vi.fn(),
    ReportClientError: vi.fn(async () => ({
      code: 'UI_RUNTIME',
      message: 'Error',
      logged: true,
    })),
  };
  window.go = { main: { App: api } };
});
afterEach(() => vi.unstubAllGlobals());
describe('workspace', () => {
  it('confirms deletion, removes the open tab and keeps other jobs', async () => {
    render(App);
    await fireEvent.click(
      await screen.findByRole('button', {
        name: /Alpha.*a@example.org.*Entwurf/,
      }),
    );
    await fireEvent.click(screen.getByRole('tab', { name: /Übersicht/ }));
    await fireEvent.click(
      screen.getByRole('button', { name: 'Job löschen: Alpha' }),
    );
    expect(api.DeleteJob).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Abbrechen' }));
    expect(api.DeleteJob).not.toHaveBeenCalled();
    await fireEvent.click(
      screen.getByRole('button', { name: 'Job löschen: Alpha' }),
    );
    await fireEvent.click(
      screen.getByRole('button', { name: 'Endgültig löschen: Alpha' }),
    );
    await waitFor(() => expect(api.DeleteJob).toHaveBeenCalledWith('a'));
    await waitFor(() =>
      expect(screen.queryByRole('tab', { name: /Alpha/ })).toBeNull(),
    );
    expect(jobs.map((j) => j.id)).toEqual(['b']);
    expect(
      JSON.parse(localStorage.getItem('outreach.openTabs')!),
    ).not.toContain('a');
    expect(api.Stop).not.toHaveBeenCalled();
  });
  it('blocks running-job deletion and keeps a job when deletion fails', async () => {
    jobs[1].state.running = true;
    vi.mocked(api.DeleteJob).mockRejectedValue(
      new Error(
        JSON.stringify({
          code: 'DATABASE_FULL',
          message: 'Datenbank voll',
          logged: true,
          id: 'delete-1',
        }),
      ),
    );
    render(App);
    const button = await screen.findByRole('button', {
      name: 'Job löschen: Alpha',
    });
    expect(
      (
        screen.getByRole('button', {
          name: 'Job löschen: Beta',
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    await fireEvent.click(button);
    await fireEvent.click(
      screen.getByRole('button', { name: 'Endgültig löschen: Alpha' }),
    );
    expect((await screen.findByRole('alert')).textContent).toContain(
      'Datenbank voll',
    );
    expect(jobs).toHaveLength(2);
    expect(
      screen.getByRole('button', { name: /Alpha.*a@example.org.*Entwurf/ }),
    ).toBeTruthy();
  });
  it('opens existing drafts without importing, connecting or sending', async () => {
    render(App);
    await fireEvent.click(
      await screen.findByRole('button', {
        name: /Alpha.*a@example.org.*Entwurf/,
      }),
    );
    await waitFor(() =>
      expect((screen.getByLabelText('Betreff') as HTMLInputElement).value).toBe(
        'Subject Alpha',
      ),
    );
    expect(api.GetState).toHaveBeenCalledWith('a');
    expect(api.CheckConnection).not.toHaveBeenCalled();
    expect(api.SendTest).not.toHaveBeenCalled();
    expect(api.Start).not.toHaveBeenCalled();
    await fireEvent.click(
      screen.getByRole('button', { name: 'Tab schließen: Alpha' }),
    );
    await waitFor(() =>
      expect(
        screen
          .getByRole('tab', { name: /Übersicht/ })
          .getAttribute('aria-selected'),
      ).toBe('true'),
    );
    expect(api.Stop).not.toHaveBeenCalled();
    expect(api.DiscardEmptyJob).not.toHaveBeenCalled();
  });
  it('hides untouched drafts from the dashboard and offers creation only in the tab bar', async () => {
    jobs = ['a', 'b', 'c'].map((id) => ({
      ...job(id, 'Neuer Job', ''),
      empty: true,
    }));
    render(App);
    await screen.findByText('Keine Jobs.');
    expect(screen.queryByRole('button', { name: 'Neuer Job' })).toBeNull();
    expect(screen.queryByRole('tab', { name: /Neuer Job/ })).toBeNull();
    expect(
      screen.getAllByRole('button', { name: 'Neuen Job anlegen' }),
    ).toHaveLength(1);
  });
  it('keeps a tab open when saving its edits fails during close', async () => {
    render(App);
    await fireEvent.click(
      await screen.findByRole('button', {
        name: /Alpha.*a@example.org.*Entwurf/,
      }),
    );
    const save = await screen.findByRole('button', {
      name: 'Entwurf speichern',
    });
    await waitFor(() =>
      expect((save as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.input(screen.getByLabelText('Jobname'), {
      target: { value: 'Nicht verlieren' },
    });
    vi.mocked(api.SaveDraft).mockRejectedValue({
      code: 'DATABASE_FULL',
      message: 'Datenbank voll',
      logged: true,
    });
    await fireEvent.click(
      screen.getByRole('button', { name: 'Tab schließen: Alpha' }),
    );
    await waitFor(() => expect(api.SaveDraft).toHaveBeenCalled());
    await waitFor(() =>
      expect(
        screen
          .getAllByRole('alert')
          .some((alert) => alert.textContent?.includes('Datenbank voll')),
      ).toBe(true),
    );
    expect((screen.getByLabelText('Jobname') as HTMLInputElement).value).toBe(
      'Nicht verlieren',
    );
    expect(
      screen.getByRole('tab', { name: /Alpha/ }).getAttribute('aria-selected'),
    ).toBe('true');
    expect(api.DiscardEmptyJob).not.toHaveBeenCalled();
  });
  it('names and saves only the selected job and exposes save errors', async () => {
    render(App);
    await fireEvent.click(
      await screen.findByRole('button', {
        name: /Beta.*b@example.org.*Entwurf/,
      }),
    );
    const save = await screen.findByRole('button', {
      name: 'Entwurf speichern',
    });
    await waitFor(() =>
      expect((save as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.input(screen.getByLabelText('Jobname'), {
      target: { value: 'Beta geändert' },
    });
    vi.mocked(api.SaveDraft).mockRejectedValueOnce({
      code: 'DATABASE_FULL',
      message: 'Datenbank voll',
      logged: true,
      id: 'disk-1',
    });
    await fireEvent.click(save);
    expect((await screen.findByRole('alert')).textContent).toContain(
      'Datenbank voll',
    );
    expect(api.SaveDraft).toHaveBeenCalledWith(
      'b',
      'Beta geändert',
      expect.any(Object),
    );
    expect(jobs[0].name).toBe('Alpha');
  });
  it('shows an empty dashboard and creates a draft with no send authorization', async () => {
    jobs = [];
    render(App);
    await screen.findByText('Keine Jobs.');
    await fireEvent.click(
      screen.getByRole('button', { name: 'Neuen Job anlegen' }),
    );
    await screen.findByRole('tab', { name: /Neuer Job/ });
    expect(
      (
        screen.getByRole('button', {
          name: 'Senden',
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(api.CreateJob).toHaveBeenCalledTimes(1);
  });
  it('keeps uncertain and failed jobs visibly distinct from completed jobs', () => {
    const j = job('a', 'Alpha', 'a@example.org');
    j.state.deliveries = [
      {
        key: 'one',
        to: 'x@example.org',
        status: 'uncertain',
        detail: 'lost ACK',
        updated: '',
      },
    ];
    expect(jobStatus(j)).toBe('Pausiert · Fehler prüfen');
    expect(jobCounts(j).uncertain).toBe(1);
    j.state.deliveries[0].status = 'sent';
    expect(jobStatus(j)).toBe('Abgeschlossen');
    j.state.running = true;
    j.state.nextAt = new Date(Date.now() + 60000).toISOString();
    expect(jobStatus(j)).toBe('Wartet · Versandpause');
  });
});
