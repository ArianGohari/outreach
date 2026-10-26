import { describe, expect, it } from 'vitest';
import { desktopError, errorText, isProblem } from './errors';
import { backend } from './api';

describe('desktop error boundary', () => {
  it('preserves structured errors and cancellation through the Wails Error wrapper', () => {
    for (const problem of [
      {
        code: 'SMTP_AUTH',
        message: 'Authentication failed',
        logged: true,
        id: 'receipt-1',
        smtpCode: 535,
      },
      {
        code: 'CANCELLED',
        message: 'Cancelled',
        logged: false,
        cancelled: true,
      },
    ]) {
      expect(desktopError(new Error(JSON.stringify(problem)))).toEqual(problem);
      expect(desktopError(JSON.stringify(problem))).toEqual(problem);
      expect(desktopError(problem)).toBe(problem);
    }
  });
  it('keeps ordinary errors readable and rejects malformed problem objects', () => {
    for (const value of [
      null,
      undefined,
      42,
      {},
      { code: 'x' },
      { message: 'x' },
      { code: 1, message: 'x' },
    ]) {
      expect(isProblem(value)).toBe(false);
      expect(desktopError(value)).toBe(value);
    }
    for (const text of [
      'plain error',
      '{broken',
      '{"message":"missing code"}',
    ]) {
      const error = new Error(text);
      expect(desktopError(error)).toBe(error);
      expect(errorText(error)).toBe(text);
    }
    expect(errorText({ details: 'private object' })).not.toContain(
      '[object Object]',
    );
  });
  it('redacts overlapping secrets and email addresses before displaying unexpected errors', () => {
    const text = errorText(
      new Error('secret-long secret person+tag@example.org'),
      ['secret', '', 'secret-long'],
    );
    expect(text).toBe('[geschützt] [geschützt] [E-Mail-Adresse]');
    expect(errorText('x'.repeat(7000))).toHaveLength(6000);
  });
  it('fails clearly when opened outside the desktop app', () => {
    const previous = window.go;
    try {
      delete window.go;
      expect(() => backend()).toThrow('Desktop-App öffnen');
    } finally {
      window.go = previous;
    }
  });
});
