import type { Problem } from './api';

export function isProblem(value: unknown): value is Problem {
  return (
    !!value &&
    typeof value === 'object' &&
    typeof (value as Problem).code === 'string' &&
    typeof (value as Problem).message === 'string'
  );
}
export function errorText(value: unknown, secrets: string[] = []): string {
  let text =
    value instanceof Error
      ? value.message
      : typeof value === 'string'
        ? value
        : 'Unbekannter Fehler in der Oberfläche.';
  for (const secret of secrets
    .filter(Boolean)
    .sort((a, b) => b.length - a.length)) {
    text = text.split(secret).join('[geschützt]');
  }
  return text.replace(/[\w.+%-]+@[\w.-]+/g, '[E-Mail-Adresse]').slice(0, 6000);
}

// Wails 2 passes ErrorFormatter strings through new Error(message).
export function desktopError(value: unknown): unknown {
  const text = value instanceof Error ? value.message : value;
  if (typeof text === 'string') {
    try {
      const parsed: unknown = JSON.parse(text);
      if (isProblem(parsed)) return parsed;
    } catch {
      /* Ordinary errors remain readable as text. */
    }
  }
  return value;
}
