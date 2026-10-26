import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import JobEditor from './JobEditor.svelte';
import {
  canStart,
  batchHoursToMinutes,
  type Bridge,
  type Plan,
  type Report,
} from './api';
const report: Report = {
  filename: 'synthetic.csv',
  headers: ['First Name', 'Last Name', 'Company Name', 'Email', 'City'],
  contacts: [
    {
      row: 2,
      email: 'lena@example.com',
      identity: 'one',
      fields: {
        'First Name': 'Lena',
        'Last Name': 'Test',
        'Company Name': 'Example',
        Email: 'lena@example.com',
        City: 'Hamburg',
      },
    },
  ],
  issues: [],
  total: 1,
  hasStatus: false,
  onlyVerified: false,
};
const plan = {
  id: 'one',
  messages: [
    {
      key: 'one',
      to: 'lena@example.com',
      subject: 'Hallo',
      body: 'Plain text',
      row: 2,
      messageId: '<test@example.org>',
    },
  ],
  report,
} as Plan;
let bridge: Bridge;
beforeEach(() => {
  bridge = {
    GoogleConfigured: vi.fn().mockResolvedValue(false),
    ImportGoogleClient: vi.fn().mockResolvedValue(undefined),
    ConnectGoogle: vi.fn(),
    DisconnectGoogle: vi.fn().mockResolvedValue(undefined),
    GetWorkspace: vi.fn().mockResolvedValue([]),
    DiscardEmptyJob: vi.fn().mockResolvedValue(undefined),
    DeleteJob: vi.fn().mockResolvedValue(undefined),
    CreateJob: vi.fn(),
    SaveDraft: vi.fn().mockResolvedValue(undefined),
    GetState: vi.fn().mockResolvedValue({
      plan: null,
      deliveries: [],
      running: false,
      nextAt: '',
      error: '',
    }),
    ChooseCSV: vi.fn().mockResolvedValue(report),
    FilterContacts: vi.fn().mockResolvedValue(report),
    Preview: vi.fn().mockResolvedValue(plan),
    CheckConnection: vi.fn().mockResolvedValue(undefined),
    LoadPassword: vi.fn().mockResolvedValue(''),
    SendTest: vi.fn().mockResolvedValue('proof'),
    Start: vi.fn().mockResolvedValue(undefined),
    ReportClientError: vi.fn().mockResolvedValue({
      code: 'UI_INPUT',
      message: 'Ungültige Eingabe',
      id: 'client-1',
      logged: true,
    }),
    Stop: vi.fn().mockResolvedValue(undefined),
  };
  window.go = { main: { App: bridge } };
});
async function renderEditor() {
  const state = await bridge.GetState('job-1');
  const request = state.plan?.request || {
    config: {
      host: '',
      port: 587,
      username: '',
      password: '',
      from: '',
      fromName: '',
      replyTo: '',
      remember: false,
    },
    subject: '',
    body: '',
    schedule: { seconds: 30, batchSize: 50, batchMinutes: 1440 },
  };
  const rendered = render(JobEditor, {
    job: {
      id: 'job-1',
      name: 'Testjob',
      draft: {
        request,
        report: state.plan?.report || {
          filename: '',
          headers: [],
          contacts: [],
          issues: [],
          total: 0,
          hasStatus: false,
          onlyVerified: true,
        },
      },
      state,
    },
    onChanged: () => {},
  });
  if (state.plan) {
    const edit = await screen.findByRole('button', {
      name: 'Job bearbeiten & fortsetzen',
    });
    await waitFor(() =>
      expect((edit as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.click(edit);
  }
  return rendered;
}
describe('send gate', () => {
  it('requires every prerequisite', () => {
    expect(canStart(false, false, true, 'proof', plan)).toBe(true);
    expect(
      canStart(false, false, true, 'proof', { ...plan, example: true }),
    ).toBe(false);
    for (const args of [
      [true, false, true, 'proof', plan],
      [false, true, true, 'proof', plan],
      [false, false, false, 'proof', plan],
      [false, false, true, '', plan],
      [false, false, true, 'proof', null],
    ] as const) {
      expect(canStart(args[0], args[1], args[2], args[3], args[4])).toBe(false);
    }
  });
});
describe('batch pause units', () => {
  it('converts hours and preserves every previously stored minute value', () => {
    expect(batchHoursToMinutes(1)).toBe(60);
    expect(batchHoursToMinutes(1.5)).toBe(90);
    expect(batchHoursToMinutes(0.5)).toBe(30);
    for (let minutes = 0; minutes <= 1440; minutes++) {
      expect(batchHoursToMinutes(minutes / 60)).toBe(minutes);
    }
    for (const invalid of [NaN, Infinity, -1, 25, 0.001]) {
      expect(() => batchHoursToMinutes(invalid)).toThrow();
    }
  });
  it('restores a saved pause in hours and submits edited hours as the same backend duration', async () => {
    const saved = {
      ...plan,
      request: {
        config: {
          host: 'smtp.example.com',
          port: 587,
          username: 'sender@example.org',
          password: '',
          from: '',
          fromName: '',
          replyTo: '',
          remember: false,
        },
        subject: 'Hallo',
        body: 'Text',
        schedule: { seconds: 30, batchSize: 100, batchMinutes: 61 },
      },
    };
    vi.mocked(bridge.GetState).mockResolvedValue({
      plan: saved,
      deliveries: [],
      running: false,
      nextAt: '',
      error: '',
    });
    await renderEditor();
    const input = screen.getByLabelText(
      'Pause je Batch (Stunden)',
    ) as HTMLInputElement;
    await waitFor(() => expect(Number(input.value)).toBe(61 / 60));
    const preview = screen.getByRole('button', {
      name: 'Nachrichten prüfen & Vorschau',
    });
    await waitFor(() =>
      expect((preview as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.click(preview);
    await waitFor(() => expect(bridge.Preview).toHaveBeenCalled());
    expect(
      vi.mocked(bridge.Preview).mock.calls[0][1].schedule.batchMinutes,
    ).toBe(61);
    await fireEvent.input(input, { target: { value: '1.5' } });
    await waitFor(() =>
      expect((preview as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.click(preview);
    await waitFor(() => expect(bridge.Preview).toHaveBeenCalledTimes(2));
    expect(
      vi.mocked(bridge.Preview).mock.calls[1][1].schedule.batchMinutes,
    ).toBe(90);
  });
});
describe('design workflow', () => {
  it('previews Jane Doe without a list or SMTP but never enables sending', async () => {
    vi.mocked(bridge.Preview).mockResolvedValue({
      ...plan,
      example: true,
      messages: [
        {
          ...plan.messages[0],
          to: 'jane.doe@example.com',
          subject: 'Hallo Jane Doe',
          body: 'Hallo Jane Doe!',
        },
      ],
    });
    await renderEditor();
    const preview = screen.getByRole('button', {
      name: 'Nachrichten prüfen & Vorschau',
    });
    await waitFor(() =>
      expect((preview as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.input(screen.getByLabelText('Betreff'), {
      target: { value: 'Hallo {{First Name}} {{Last Name}}' },
    });
    await fireEvent.input(screen.getByLabelText('Text'), {
      target: { value: 'Hallo {{First Name}} {{Last Name}}!' },
    });
    await fireEvent.click(preview);
    await screen.findByText('Jane Doe · Beispieldaten');
    expect(screen.getByText('Hallo Jane Doe!')).toBeTruthy();
    expect(
      (
        screen.getByRole('button', {
          name: 'Senden',
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      (
        screen.getByRole('button', {
          name: 'Testmail senden',
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(bridge.ChooseCSV).not.toHaveBeenCalled();
    expect(bridge.CheckConnection).not.toHaveBeenCalled();
    expect(bridge.SendTest).not.toHaveBeenCalled();
    expect(bridge.Start).not.toHaveBeenCalled();
  });
  it('starts with no recipient or send authorization', async () => {
    await renderEditor();
    await waitFor(() => expect(bridge.GetState).toHaveBeenCalled());
    expect(
      (screen.getByRole('button', { name: 'Senden' }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(bridge.SendTest).not.toHaveBeenCalled();
    expect(bridge.Start).not.toHaveBeenCalled();
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(5);
    expect(
      (screen.getByLabelText('Pause je Batch (Stunden)') as HTMLInputElement)
        .value,
    ).toBe('24');
  });
  it('shows missing verification and blocks send before test', async () => {
    await renderEditor();
    await waitFor(() =>
      expect(
        (
          screen.getByRole('button', {
            name: 'Datei auswählen',
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    await fireEvent.click(
      screen.getByRole('button', { name: 'Datei auswählen' }),
    );
    await screen.findByText(/Verifizierungsstatus nicht enthalten/);
    expect(
      (screen.getByRole('button', { name: 'Senden' }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      screen.queryByRole('checkbox', { name: 'Ohne Duplikate' }),
    ).toBeNull();
    expect(
      screen.queryByText(
        'Doppelte Kontakte werden automatisch ausgeschlossen.',
      ),
    ).toBeNull();
  });
  it('invalidates successful test when content changes', async () => {
    await renderEditor();
    await waitFor(() =>
      expect(
        (
          screen.getByRole('button', {
            name: 'Verbindung prüfen',
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    await fireEvent.input(screen.getByLabelText('Benutzer'), {
      target: { value: 'sender@example.org' },
    });
    await fireEvent.click(
      screen.getByRole('button', { name: 'Verbindung prüfen' }),
    );
    await screen.findByText('Verbunden · TLS');
    await fireEvent.click(
      screen.getByRole('button', { name: 'Datei auswählen' }),
    );
    await screen.findByText('synthetic.csv');
    await fireEvent.click(
      screen.getByRole('button', { name: 'Kontakte prüfen' }),
    );
    await waitFor(() =>
      expect(
        (
          screen.getByRole('button', {
            name: 'Testmail senden',
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    await fireEvent.click(
      screen.getByRole('button', { name: 'Testmail senden' }),
    );
    await waitFor(() =>
      expect(
        (screen.getByRole('button', { name: 'Senden' }) as HTMLButtonElement)
          .disabled,
      ).toBe(false),
    );
    await fireEvent.input(screen.getByLabelText('Betreff'), {
      target: { value: 'changed' },
    });
    expect(
      (screen.getByRole('button', { name: 'Senden' }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(bridge.Start).not.toHaveBeenCalled();
  });
});

describe('error reporting', () => {
  it('shows native Error-wrapped preview failures with the original log receipt', async () => {
    vi.mocked(bridge.Preview).mockRejectedValue(
      new Error(
        JSON.stringify({
          code: 'OPERATION_FAILED',
          message: 'Nachrichtenvorschau fehlgeschlagen',
          details: 'Zeile 1: Betreff leer, zu lang oder mit Steuerzeichen',
          id: 'preview-123',
          logged: true,
          logPath: '/logs/errors.jsonl',
        }),
      ),
    );
    await renderEditor();
    const button = screen.getByRole('button', {
      name: 'Nachrichten prüfen & Vorschau',
    });
    await waitFor(() =>
      expect((button as HTMLButtonElement).disabled).toBe(false),
    );
    expect(
      (screen.getByLabelText('Pause je Batch (Stunden)') as HTMLInputElement)
        .value,
    ).toBe('24');
    expect(
      (screen.getByLabelText('Batchgröße (max. 100)') as HTMLInputElement)
        .value,
    ).toBe('50');
    await fireEvent.click(button);
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Betreff leer');
    expect(alert.textContent).toContain('preview-123');
    expect(alert.textContent).toContain('Fehler lokal protokolliert.');
    expect(alert.textContent).not.toContain('[object Object]');
    expect(bridge.ReportClientError).not.toHaveBeenCalled();
    expect(bridge.Start).not.toHaveBeenCalled();
  });
  it('shows structured SMTP details, remediation and matching log ID', async () => {
    vi.mocked(bridge.CheckConnection).mockRejectedValue({
      code: 'SMTP_AUTH',
      message: 'SMTP-Anmeldung fehlgeschlagen',
      operation: 'SMTP-Verbindung prüfen',
      details: 'SMTP 535: 5.7.8 Authentication credentials invalid',
      hint: 'App-Passwort prüfen.',
      step: 'AUTH',
      smtpCode: 535,
      id: 'error-123',
      logged: true,
      logPath: '/logs/errors.jsonl',
    });
    await renderEditor();
    const button = screen.getByRole('button', { name: 'Verbindung prüfen' });
    await waitFor(() =>
      expect((button as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.click(button);
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain(
      '5.7.8 Authentication credentials invalid',
    );
    expect(alert.textContent).toContain('App-Passwort prüfen.');
    expect(alert.textContent).toContain('error-123');
    expect(alert.textContent).toContain('/logs/errors.jsonl');
    expect(bridge.ReportClientError).not.toHaveBeenCalled();
    expect(screen.queryByText('Verbunden · TLS')).toBeNull();
  });
  it('keeps an imported list when the file dialog is cancelled without logging', async () => {
    await renderEditor();
    const button = screen.getByRole('button', { name: 'Datei auswählen' });
    await waitFor(() =>
      expect((button as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.click(button);
    await screen.findByText('synthetic.csv');
    expect(screen.queryByLabelText('E-Mail-Spalte')).toBeNull();
    expect(screen.getByRole('columnheader', { name: 'City' })).toBeTruthy();
    expect(screen.getByRole('cell', { name: 'Hamburg' })).toBeTruthy();
    vi.mocked(bridge.ChooseCSV).mockRejectedValue(
      new Error(
        JSON.stringify({
          code: 'CANCELLED',
          message: 'Dateiauswahl abgebrochen',
          cancelled: true,
        }),
      ),
    );
    await fireEvent.click(button);
    await screen.findByText('Dateiauswahl abgebrochen');
    expect(screen.getByText('synthetic.csv')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(bridge.ReportClientError).not.toHaveBeenCalled();
  });
  it('reports local validation and exposes a failed log write', async () => {
    vi.mocked(bridge.ReportClientError).mockResolvedValue({
      code: 'UI_INPUT',
      message: 'Ungültige Eingabe',
      logged: false,
      logIssue: 'disk full',
    });
    await renderEditor();
    const button = screen.getByRole('button', {
      name: 'Nachrichten prüfen & Vorschau',
    });
    await waitFor(() =>
      expect((button as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.input(screen.getByLabelText('Pause je Batch (Stunden)'), {
      target: { value: '25' },
    });
    await fireEvent.click(button);
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Batchpause zwischen 0 und 24 Stunden');
    expect(alert.textContent).toContain('disk full');
    expect(bridge.Preview).not.toHaveBeenCalled();
    expect(bridge.ReportClientError).toHaveBeenCalled();
  });
});

describe('Google account setup', () => {
  it('imports a client, connects the verified account and keeps sending locked', async () => {
    vi.mocked(bridge.ConnectGoogle).mockResolvedValue({
      provider: 'google',
      googleGrant: 'opaque-reference',
      username: 'google@example.org',
      host: '',
      port: 587,
      password: '',
      from: '',
      fromName: '',
      replyTo: '',
      remember: false,
    });
    await renderEditor();
    const provider = screen.getByLabelText('Versand über');
    await waitFor(() =>
      expect((provider as HTMLSelectElement).disabled).toBe(false),
    );
    await fireEvent.change(provider, { target: { value: 'google' } });
    const login = await screen.findByRole('button', {
      name: 'Mit Google verbinden',
    });
    expect((login as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByLabelText('Passwort')).toBeNull();
    const importClient = screen.getByRole('button', {
      name: 'OAuth-JSON auswählen',
    });
    await waitFor(() =>
      expect((importClient as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.click(importClient);
    await waitFor(() =>
      expect((login as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.click(login);
    await screen.findByText('google@example.org');
    expect(bridge.ConnectGoogle).toHaveBeenCalledWith('job-1');
    expect(
      (screen.getByRole('button', { name: 'Senden' }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    await fireEvent.click(
      screen.getByRole('button', { name: 'Verbindung prüfen' }),
    );
    await screen.findByText('Verbunden · Google');
    expect(bridge.CheckConnection).toHaveBeenCalledWith(
      'job-1',
      expect.objectContaining({
        provider: 'google',
        googleGrant: 'opaque-reference',
        password: '',
      }),
    );
    expect(bridge.SendTest).not.toHaveBeenCalled();
    await fireEvent.click(
      screen.getByRole('button', { name: 'Google-Verbindung entfernen' }),
    );
    await waitFor(() =>
      expect(screen.queryByText('google@example.org')).toBeNull(),
    );
    expect(bridge.DisconnectGoogle).toHaveBeenCalledWith(
      'job-1',
      expect.objectContaining({ googleGrant: 'opaque-reference' }),
    );
  });
  it('shows an admin policy refusal without enabling sending', async () => {
    vi.mocked(bridge.GoogleConfigured).mockResolvedValue(true);
    vi.mocked(bridge.ConnectGoogle).mockRejectedValue(
      new Error(
        JSON.stringify({
          code: 'GOOGLE_POLICY',
          message: 'Google blockiert die Anmeldung',
          details: 'Workspace-Richtlinie',
          hint: 'Admin-Freigabe erforderlich.',
          logged: true,
          id: 'google-1',
        }),
      ),
    );
    await renderEditor();
    const provider = screen.getByLabelText('Versand über');
    await waitFor(() =>
      expect((provider as HTMLSelectElement).disabled).toBe(false),
    );
    await fireEvent.change(provider, { target: { value: 'google' } });
    const login = await screen.findByRole('button', {
      name: 'Mit Google verbinden',
    });
    await waitFor(() =>
      expect((login as HTMLButtonElement).disabled).toBe(false),
    );
    await fireEvent.click(login);
    expect((await screen.findByRole('alert')).textContent).toContain(
      'Admin-Freigabe erforderlich',
    );
    expect(
      (
        screen.getByRole('button', {
          name: 'Testmail senden',
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(bridge.Start).not.toHaveBeenCalled();
  });
});
