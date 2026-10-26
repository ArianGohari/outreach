export interface Problem {
  id?: string;
  code: string;
  operation?: string;
  message: string;
  details?: string;
  hint?: string;
  step?: string;
  smtpCode?: number;
  time?: string;
  logPath?: string;
  logged: boolean;
  logIssue?: string;
  cancelled?: boolean;
}
export interface Config {
  provider?: 'google' | '';
  googleGrant?: string;
  host: string;
  port: number;
  username: string;
  password: string;
  fromName: string;
  from: string;
  replyTo: string;
  remember: boolean;
}
export interface Contact {
  row: number;
  email: string;
  identity: string;
  fields: Record<string, string>;
}
export interface Report {
  filename: string;
  headers: string[];
  contacts: Contact[];
  issues: { row: number; reason: string }[];
  total: number;
  hasStatus: boolean;
  onlyVerified: boolean;
  previouslyContacted?: number;
}
export interface Request {
  config: Config;
  subject: string;
  body: string;
  schedule: { seconds: number; batchSize: number; batchMinutes: number };
}
export interface Message {
  key: string;
  to: string;
  subject: string;
  body: string;
  messageId: string;
  row: number;
}
export interface Plan {
  example?: boolean;
  id: string;
  request: Request;
  report: Report;
  messages: Message[];
}
export interface Delivery {
  key: string;
  to: string;
  status: string;
  detail: string;
  updated: string;
}
export interface State {
  plan: Plan | null;
  deliveries: Delivery[];
  running: boolean;
  nextAt: string;
  error: string;
  problem?: Problem;
}
export interface JobBridge {
  GetState(): Promise<State>;
  ChooseCSV(verified: boolean): Promise<Report>;
  FilterContacts(verified: boolean): Promise<Report>;
  Preview(req: Request): Promise<Plan>;
  CheckConnection(config: Config): Promise<void>;
  LoadPassword(config: Config): Promise<string>;
  SendTest(req: Request, index: number, to: string): Promise<string>;
  Start(req: Request, token: string): Promise<void>;
  Stop(): Promise<void>;
  ReportClientError(code: string): Promise<Problem>;
}
export interface Draft {
  request: Request;
  report: Report;
}
export interface JobView {
  empty?: boolean;
  id: string;
  name: string;
  draft: Draft;
  state: State;
}
export interface Bridge {
  GoogleConfigured(): Promise<boolean>;
  ImportGoogleClient(): Promise<void>;
  ConnectGoogle(id: string): Promise<Config>;
  DisconnectGoogle(id: string, config: Config): Promise<void>;
  GetWorkspace(): Promise<JobView[]>;
  CreateJob(name: string): Promise<JobView>;
  DiscardEmptyJob(id: string): Promise<void>;
  DeleteJob(id: string): Promise<void>;
  SaveDraft(id: string, name: string, req: Request): Promise<void>;
  GetState(id: string): Promise<State>;
  ChooseCSV(id: string, verified: boolean): Promise<Report>;
  FilterContacts(id: string, verified: boolean): Promise<Report>;
  Preview(id: string, req: Request): Promise<Plan>;
  CheckConnection(id: string, config: Config): Promise<void>;
  LoadPassword(config: Config): Promise<string>;
  SendTest(
    id: string,
    req: Request,
    index: number,
    to: string,
  ): Promise<string>;
  Start(id: string, req: Request, token: string): Promise<void>;
  Stop(id: string): Promise<void>;
  ReportClientError(code: string): Promise<Problem>;
}
export function jobBackend(id: string): JobBridge {
  const api = backend();
  return {
    GetState: () => api.GetState(id),
    ChooseCSV: (v) => api.ChooseCSV(id, v),
    FilterContacts: (v) => api.FilterContacts(id, v),
    Preview: (r) => api.Preview(id, r),
    CheckConnection: (c) => api.CheckConnection(id, c),
    LoadPassword: (c) => api.LoadPassword(c),
    SendTest: (r, i, to) => api.SendTest(id, r, i, to),
    Start: (r, t) => api.Start(id, r, t),
    Stop: () => api.Stop(id),
    ReportClientError: (c) => api.ReportClientError(c),
  };
}
export function jobCounts(job: JobView) {
  const d = job.state.deliveries;
  return {
    total: d.length || job.draft.report.contacts?.length || 0,
    done: d.filter((x) => !['pending', 'sending'].includes(x.status)).length,
    sent: d.filter((x) => x.status === 'sent').length,
    failed: d.filter((x) => x.status === 'failed').length,
    uncertain: d.filter((x) => x.status === 'uncertain').length,
    blocked: d.filter((x) => x.status === 'blocked').length,
  };
}
export function jobStatus(job: JobView): string {
  const c = jobCounts(job);
  if (job.state.running)
    return job.state.nextAt && new Date(job.state.nextAt).getTime() > Date.now()
      ? 'Wartet · Versandpause'
      : 'Läuft';
  if (job.state.problem || job.state.error || c.uncertain || c.failed)
    return 'Pausiert · Fehler prüfen';
  if (job.state.deliveries.length && c.done === c.total) return 'Abgeschlossen';
  if (job.state.plan) return 'Pausiert';
  return 'Entwurf';
}
declare global {
  interface Window {
    go?: { main: { App: Bridge } };
    runtime?: {
      EventsOn(name: string, callback: (problem?: Problem) => void): () => void;
    };
  }
}
export function backend(): Bridge {
  const app = window.go?.main.App;
  if (!app)
    throw new Error(
      'Bitte die Desktop-App öffnen. Im Browser ist kein SMTP-Versand verfügbar.',
    );
  return app;
}
// Preserve the stored schedule format and campaign IDs when changing display units.
export function batchHoursToMinutes(hours: number): number {
  const minutes = hours * 60;
  if (!Number.isFinite(hours) || hours < 0 || hours > 24) {
    throw new Error('Batchpause zwischen 0 und 24 Stunden eingeben.');
  }
  if (Math.abs(minutes - Math.round(minutes)) > 1e-7) {
    throw new Error(
      'Batchpause minutengenau eingeben, z. B. 0,5 oder 1,5 Stunden.',
    );
  }
  return Math.round(minutes);
}
export function canStart(
  busy: boolean,
  running: boolean,
  checked: boolean,
  tested: string,
  plan: Plan | null,
): boolean {
  return (
    !busy &&
    !running &&
    checked &&
    tested.length > 0 &&
    !!plan?.messages.length &&
    !plan.example
  );
}
export const statusLabel: Record<string, string> = {
  pending: 'Ausstehend',
  sending: 'Wird übertragen',
  sent: 'An Versanddienst übergeben',
  failed: 'Fehlgeschlagen',
  uncertain: 'Unklar · gesperrt',
  blocked: 'Doppelversand verhindert',
};
