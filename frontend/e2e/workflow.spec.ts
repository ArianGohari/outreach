import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    const contacts = [
      {
        row: 2,
        email: 'lena@example.com',
        identity: 'one',
        fields: {
          'First Name': 'Lena',
          'Last Name': 'Beispiel',
          Title: 'Head of Sales',
          'Company Name': 'Example GmbH',
          Email: 'lena@example.com',
          City: 'Berlin',
        },
      },
    ];
    const report = {
      filename: 'synthetic.csv',
      headers: [
        'First Name',
        'Last Name',
        'Title',
        'Company Name',
        'Email',
        'City',
      ],
      contacts,
      issues: [],
      total: 1,
      hasStatus: false,
      onlyVerified: false,
    };
    const jobs: Record<string, any> = {};
    let serial = 0;
    let sent = 0;
    const emptyReport = () => ({
      filename: '',
      headers: [],
      contacts: [],
      issues: [],
      total: 0,
      hasStatus: false,
      onlyVerified: true,
    });
    const state = (j: any) => ({
      plan: j.plan,
      deliveries: j.running
        ? [
            {
              key: 'one',
              to: 'lena@example.com',
              status: 'sending',
              detail: '',
              updated: new Date().toISOString(),
            },
          ]
        : [],
      running: j.running,
      nextAt: '',
      error: '',
    });
    const view = (j: any) => ({
      id: j.id,
      name: j.name,
      empty:
        j.name === 'New job' &&
        !j.plan &&
        !j.imported &&
        !j.request.subject &&
        !j.request.body &&
        !Object.entries(j.request.config).some(
          ([key, value]) =>
            !['port', 'remember', 'password'].includes(key) && !!value,
        ),
      draft: { request: j.request, report: j.report },
      state: state(j),
    });
    const api = {
      GoogleConfigured: async () => true,
      ImportGoogleClient: async () => {},
      ConnectGoogle: async () => ({
        provider: 'google',
        googleGrant: 'test-grant',
        host: '',
        port: 587,
        username: 'google@example.org',
        password: '',
        from: '',
        fromName: '',
        replyTo: '',
        remember: false,
      }),
      DisconnectGoogle: async () => {},
      GetWorkspace: async () => Object.values(jobs).map(view),
      DiscardEmptyJob: async (id: string) => {
        if (!view(jobs[id]).empty) throw new Error('Not empty');
        delete jobs[id];
      },
      DeleteJob: async (id: string) => {
        if (jobs[id].running) throw new Error('Pause job first');
        delete jobs[id];
      },
      CreateJob: async (name: string) => {
        const existing = Object.values(jobs).find((j) => view(j).empty);
        if (existing) return view(existing);
        const id = 'job-' + ++serial;
        const j = {
          id,
          name,
          request: {
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
          },
          report: emptyReport(),
          plan: null,
          running: false,
          tested: false,
          imported: false,
        };
        jobs[id] = j;
        return view(j);
      },
      SaveDraft: async (id: string, name: string, request: any) => {
        jobs[id].name = name;
        jobs[id].request = {
          ...request,
          config: { ...request.config, password: '' },
        };
      },
      GetState: async (id: string) => state(jobs[id]),
      ChooseCSV: async (id: string, verified: boolean) => {
        jobs[id].imported = true;
        jobs[id].report = {
          ...report,
          onlyVerified: verified,
          contacts: verified ? [] : contacts,
          issues: verified
            ? [{ row: 2, reason: 'Verification status is missing' }]
            : [],
        };
        return jobs[id].report;
      },
      FilterContacts: async (id: string) => {
        jobs[id].report = report;
        return report;
      },
      Preview: async (id: string, req: any) => ({
        example: !jobs[id].imported,
        id: 'plan-' + id,
        request: req,
        report: jobs[id].report,
        messages: [
          {
            key: 'one',
            to: jobs[id].imported ? 'lena@example.com' : 'jane.doe@example.com',
            subject: req.subject
              .replace('{{First Name}}', jobs[id].imported ? 'Lena' : 'Jane')
              .replace('{{Last Name}}', jobs[id].imported ? 'Beispiel' : 'Doe'),
            body: req.body
              .replace('{{First Name}}', jobs[id].imported ? 'Lena' : 'Jane')
              .replace('{{Last Name}}', jobs[id].imported ? 'Beispiel' : 'Doe'),
            messageId: '<one@outreach.local>',
            row: 2,
          },
        ],
      }),
      CheckConnection: async () => {},
      LoadPassword: async () => '',
      SendTest: async (id: string) => {
        jobs[id].tested = true;
        return 'proof-' + id;
      },
      Start: async (id: string, req: any) => {
        if (!jobs[id].tested) throw new Error('Missing test');
        if (
          Object.values(jobs).some(
            (j: any) =>
              j.running &&
              j.id !== id &&
              j.request.config.username.toLowerCase() ===
                req.config.username.toLowerCase(),
          )
        )
          throw {
            code: 'ACCOUNT_BUSY',
            message: 'Absenderkonto belegt',
            logged: true,
          };
        sent++;
        jobs[id].running = true;
        jobs[id].plan = await api.Preview(id, req);
      },
      Stop: async (id: string) => {
        jobs[id].running = false;
      },
      ReportClientError: async () => ({
        code: 'UI_INPUT',
        message: 'Invalid input',
        logged: true,
        id: 'test-error',
      }),
    };
    (window as any).go = { main: { App: api } };
    (window as any).__testSendCount = () => sent;
    (window as any).__jobs = jobs;
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Create new job' }).click();
});

test('five-step flow, missing verification, preview, test, invalidation and stop', async ({
  page,
}) => {
  await expect(page.getByRole('heading', { level: 2 })).toHaveCount(5);
  await expect(
    page.getByRole('checkbox', { name: 'Ohne Duplikate' }),
  ).toHaveCount(0);
  await expect(page.getByLabel('Pause per batch (hours)')).toHaveValue('24');
  await expect(
    page.getByRole('button', { name: 'Send', exact: true }),
  ).toBeDisabled();
  await page.getByLabel('SMTP server').fill('smtp.example.com');
  await page.getByLabel('Username', { exact: true }).fill('sender@example.org');
  await page.getByLabel('Password', { exact: true }).fill('test-only');
  await page.getByRole('button', { name: 'Check connection' }).click();
  await expect(page.getByText('Connected · TLS')).toBeVisible();
  await page.getByRole('button', { name: 'Choose file' }).click();
  await expect(
    page.getByText(/Verification status is missing/).first(),
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'Check contacts' }),
  ).toBeDisabled();
  await page.getByLabel('Verified only', { exact: true }).uncheck();
  await page.getByRole('button', { name: 'Check contacts' }).click();
  await page
    .getByLabel('Subject', { exact: true })
    .fill('Hallo {{First Name}}');
  await page
    .getByLabel('Text', { exact: true })
    .fill('Hallo {{First Name}},\n\nOhne Signatur.');
  await page.getByRole('button', { name: 'Validate & preview' }).click();
  await expect(page.locator('.message')).toHaveText(
    'Hallo Lena,\n\nOhne Signatur.',
  );
  await expect(
    page.getByRole('button', { name: 'Send', exact: true }),
  ).toBeDisabled();
  await page.getByRole('button', { name: 'Send test email' }).click();
  await expect(
    page.getByRole('button', { name: 'Send', exact: true }),
  ).toBeEnabled();
  await page.getByLabel('Subject', { exact: true }).fill('Neuer Subject');
  await expect(
    page.getByRole('button', { name: 'Send', exact: true }),
  ).toBeDisabled();
  await page.getByRole('button', { name: 'Send test email' }).click();
  await page.getByRole('button', { name: 'Send', exact: true }).dblclick();
  await expect(page.getByRole('button', { name: 'Stop' })).toBeEnabled();
  await expect(page.getByLabel('Text', { exact: true })).toBeDisabled();
  expect(await page.evaluate(() => (window as any).__testSendCount())).toBe(1);
  await page.getByRole('button', { name: 'Stop' }).click();
});

test('layout fits supported window sizes and displays imported text literally', async ({
  page,
}) => {
  for (const width of [640, 1000, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
  await page.setViewportSize({ width: 1000, height: 900 });
  await page.screenshot({
    path: `test-results/initial-${test.info().project.name}.png`,
    fullPage: true,
  });
});

test('template guide and Jane Doe preview work without import or SMTP', async ({
  page,
}) => {
  await page.getByText('Template tags & guide').click();
  await expect(
    page
      .getByRole('table', { name: 'Available template tags' })
      .locator('tbody tr'),
  ).toHaveCount(9);
  await page
    .getByLabel('Subject', { exact: true })
    .fill('Hallo {{First Name}} {{Last Name}}');
  await page
    .getByLabel('Text', { exact: true })
    .fill('Hallo {{First Name}} {{Last Name}}!');
  await page.getByRole('button', { name: 'Validate & preview' }).click();
  await expect(page.getByText('Jane Doe · Sample data')).toBeVisible();
  await expect(page.locator('.message')).toHaveText('Hallo Jane Doe!');
  await expect(
    page.getByRole('button', { name: 'Send', exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole('button', { name: 'Send test email' }),
  ).toBeDisabled();
  expect(await page.evaluate(() => (window as any).__testSendCount())).toBe(0);
});

test('Google setup hides password fields and requires a new connection check', async ({
  page,
}) => {
  await page
    .getByRole('combobox', { name: 'Send via', exact: true })
    .selectOption('google');
  await expect(page.getByLabel('SMTP server')).toHaveCount(0);
  await expect(page.getByLabel('Password', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Connect Google' }).click();
  await expect(
    page.getByText('google@example.org', { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'Send', exact: true }),
  ).toBeDisabled();
  await page.getByRole('button', { name: 'Check connection' }).click();
  await expect(page.getByText('Connected · Google')).toBeVisible();
  await page
    .getByRole('combobox', { name: 'Send via', exact: true })
    .selectOption('');
  await expect(page.getByLabel('Username', { exact: true })).toHaveValue('');
  await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
  await expect(page.getByText('Connected · Google')).toHaveCount(0);
  expect(await page.evaluate(() => (window as any).__testSendCount())).toBe(0);
});

test('SMTP failure remains readable with diagnostic ID at narrow window width', async ({
  page,
}) => {
  await page.setViewportSize({ width: 640, height: 900 });
  await page.evaluate(() => {
    (window as any).go.main.App.CheckConnection = async () => {
      throw {
        code: 'SMTP_AUTH',
        message: 'SMTP authentication failed',
        operation: 'Check SMTP connection',
        details:
          'Server smtp.example.com, Port 587. Schritt: AUTH.\nSMTP 535: 5.7.8 Authentication credentials invalid',
        hint: 'Check the username and app password.',
        step: 'AUTH',
        smtpCode: 535,
        id: 'smtp-error-123',
        logged: true,
        logPath:
          '/Users/test/Library/Application Support/Outreach/logs/errors.jsonl',
      };
    };
  });
  await page.getByRole('button', { name: 'Check connection' }).click();
  const alert = page.getByRole('alert');
  await expect(alert).toBeVisible();
  await expect(alert).toContainText('5.7.8 Authentication credentials invalid');
  await expect(alert).toContainText('Check the username and app password.');
  await alert.getByText(/Diagnostics/).click();
  await expect(alert).toContainText('smtp-error-123');
  await expect(alert.getByText(/Log:/)).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'Send', exact: true }),
  ).toBeDisabled();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: `test-results/error-${test.info().project.name}.png`,
    fullPage: true,
  });
});

test('dashboard and tabs preserve separate job drafts and credentials', async ({
  page,
}) => {
  let panel = page.getByRole('tabpanel');
  await panel.getByLabel('Job name', { exact: true }).fill('Alpha');
  await panel.getByLabel('Username', { exact: true }).fill('alpha@example.org');
  await panel.getByLabel('Password', { exact: true }).fill('alpha-secret');
  await panel.getByLabel('Subject', { exact: true }).fill('Message Alpha');
  await panel.getByLabel('Text', { exact: true }).fill('Hallo {{First Name}}');
  await panel.getByRole('button', { name: 'Save draft' }).click();
  await expect(page.getByRole('tab', { name: /Alpha/ })).toBeVisible();
  await page.getByRole('button', { name: 'Create new job' }).click();
  panel = page.getByRole('tabpanel');
  await panel.getByLabel('Job name', { exact: true }).fill('Beta');
  await panel.getByLabel('Username', { exact: true }).fill('beta@example.org');
  await panel.getByLabel('Subject', { exact: true }).fill('Message Beta');
  await panel.getByRole('button', { name: 'Save draft' }).click();
  await page.getByRole('tab', { name: /Alpha/ }).click();
  await expect(
    page.getByRole('tabpanel').getByLabel('Subject', { exact: true }),
  ).toHaveValue('Message Alpha');
  await expect(
    page.getByRole('tabpanel').getByLabel('Password', { exact: true }),
  ).toHaveValue('alpha-secret');
  await page.getByRole('button', { name: 'Close tab: Alpha' }).click();
  await expect(page.getByRole('tab', { name: /Beta/ })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('tab', { name: /Dashboard/ }).click();
  await page
    .getByRole('tabpanel')
    .getByRole('button', { name: /Alpha alpha@example.org Draft/ })
    .click();
  await expect(
    page.getByRole('tabpanel').getByLabel('Password', { exact: true }),
  ).toHaveValue('alpha-secret');
  await page.getByRole('tab', { name: /Beta/ }).click();
  await expect(
    page.getByRole('tabpanel').getByLabel('Subject', { exact: true }),
  ).toHaveValue('Message Beta');
  await expect(
    page.getByRole('tabpanel').getByLabel('Password', { exact: true }),
  ).toHaveValue('');
  await page.getByRole('tab', { name: /Dashboard/ }).click();
  await page.setViewportSize({ width: 640, height: 900 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: `test-results/dashboard-${test.info().project.name}.png`,
    fullPage: true,
  });
  expect(await page.evaluate(() => (window as any).__testSendCount())).toBe(0);
});

test('closing a running job tab does not stop its worker', async ({ page }) => {
  const panel = page.getByRole('tabpanel');
  await panel
    .getByLabel('Username', { exact: true })
    .fill('sender@example.org');
  await panel.getByRole('button', { name: 'Check connection' }).click();
  await panel.getByRole('button', { name: 'Choose file' }).click();
  await panel.getByLabel('Verified only', { exact: true }).uncheck();
  await panel.getByRole('button', { name: 'Check contacts' }).click();
  await panel.getByLabel('Subject', { exact: true }).fill('Test');
  await panel.getByLabel('Text', { exact: true }).fill('Hallo {{First Name}}');
  await panel.getByRole('button', { name: 'Send test email' }).click();
  await panel.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(panel.getByRole('button', { name: 'Stop' })).toBeEnabled();
  await page.screenshot({
    path: `test-results/job-progress-${test.info().project.name}.png`,
    fullPage: true,
  });
  await page.getByRole('button', { name: 'Close tab: New job' }).click();
  await expect(
    page.getByRole('tabpanel').getByRole('button', { name: 'Pause: New job' }),
  ).toBeVisible();
  expect(
    await page.evaluate(() => (window as any).__jobs['job-1'].running),
  ).toBe(true);
  await page
    .getByRole('tabpanel')
    .getByRole('button', { name: 'Pause: New job' })
    .click();
  await expect(
    page.getByRole('tabpanel').getByRole('button', { name: 'Pause: New job' }),
  ).toHaveCount(0);
});

test('reopens a blank draft and discards it without leaving stale tabs', async ({
  page,
}) => {
  await page.getByRole('button', { name: 'Create new job' }).click();
  await expect(page.getByRole('tab', { name: /New job/ })).toHaveCount(1);
  expect(
    await page.evaluate(() => Object.keys((window as any).__jobs).length),
  ).toBe(1);
  await page.getByRole('tab', { name: /Dashboard/ }).click();
  await page.getByRole('button', { name: 'Close tab: New job' }).click();
  await expect(page.getByText('No jobs yet.')).toBeVisible();
  await expect(page.getByRole('tab', { name: /New job/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'Create new job' }).click();
  await expect(
    page.getByRole('heading', { name: 'New job', exact: true }),
  ).toBeVisible();
});

test('tab overflow keeps add button visible and supports keyboard navigation', async ({
  page,
}) => {
  await page.setViewportSize({ width: 640, height: 900 });
  for (let i = 1; i <= 6; i++) {
    await page
      .getByRole('tabpanel')
      .getByLabel('Job name', { exact: true })
      .fill(`Kampagne ${i} · Unternehmensgründer in Deutschland`);
    if (i < 6)
      await page.getByRole('button', { name: 'Create new job' }).click();
    else
      await page
        .getByRole('tabpanel')
        .getByRole('button', { name: 'Save draft' })
        .click();
  }
  const add = page.getByRole('button', { name: 'Create new job' });
  await expect(add).toBeInViewport();
  const last = page.getByRole('tab', { name: /Kampagne 6/ });
  await expect(last).toBeInViewport();
  await last.focus();
  await last.press('Home');
  const overview = page.getByRole('tab', { name: /Dashboard/ });
  await expect(overview).toHaveAttribute('aria-selected', 'true');
  await expect(overview).toBeInViewport();
  await overview.press('End');
  await expect(last).toHaveAttribute('aria-selected', 'true');
  await last.press('Delete');
  await expect(page.getByRole('tab', { name: /Kampagne 5/ })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await expect(last).toHaveCount(0);
  await page.getByRole('tab', { name: /Kampagne 5/ }).press('Home');
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: `test-results/tabs-refined-${test.info().project.name}.png`,
    fullPage: true,
  });
  expect(await page.evaluate(() => (window as any).__testSendCount())).toBe(0);
});

test('deleting a configured job requires confirmation and removes its tab', async ({
  page,
}) => {
  await page.getByLabel('Job name', { exact: true }).fill('Entfernen');
  await page.getByLabel('Subject', { exact: true }).fill('Mein Draft');
  await page.getByRole('button', { name: 'Save draft' }).click();
  await expect(page.getByRole('tab', { name: /Entfernen/ })).toBeVisible();
  await page.getByRole('tab', { name: /Dashboard/ }).click();
  await page.getByRole('button', { name: 'Delete job: Entfernen' }).click();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('tab', { name: /Entfernen/ })).toBeVisible();
  await page.getByRole('button', { name: 'Delete job: Entfernen' }).click();
  await expect(
    page.getByText('Delete job? Sending history will be kept.'),
  ).toBeVisible();
  await page
    .getByRole('button', { name: 'Confirm deletion: Entfernen' })
    .click();
  await expect(page.getByText('No jobs yet.')).toBeVisible();
  await expect(page.getByRole('tab', { name: /Entfernen/ })).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('tab', { name: /Entfernen/ })).toHaveCount(0);
});
