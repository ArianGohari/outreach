import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, existsSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { configureGoogle, renderGoogleClient } from './configure-google.mjs';

const valid = JSON.stringify({ installed: { client_id: 'synthetic.apps.googleusercontent.com', client_secret: 'synthetic-only', auth_uri: 'https://untrusted.example.org', refresh_token: 'never-embed-me' } });
test('embeds only the desktop app identity', () => {
  const result = renderGoogleClient(valid);
  assert.match(result, /synthetic.apps.googleusercontent.com/);
  assert.match(result, /Secret: "synthetic-only"/);
  assert.ok(!result.includes('never-embed-me'));
  assert.ok(!result.includes('untrusted.example.org'));
});
test('rejects malformed, web, oversized and injectable configuration without exposing it', () => {
  for (const raw of ['{', 'null', '{}', '{"web":{}}', 'x'.repeat(65537),
    JSON.stringify({installed:{client_id:'wrong',client_secret:'PRIVATE'}}),
    JSON.stringify({installed:{client_id:'synthetic.apps.googleusercontent.com',client_secret:'PRIVATE"\n'}}),
  ]) {
    assert.throws(() => renderGoogleClient(raw), error => !error.message.includes('PRIVATE'));
  }
});
test('release configuration is required and failures remove stale generated files', () => {
  const dir = mkdtempSync(join(tmpdir(), 'outreach-google-build-'));
  const output = join(dir, 'client_build.go');
  try {
    assert.equal(configureGoogle(valid, output, true), true);
    assert.equal(readFileSync(output, 'utf8'), renderGoogleClient(valid));
    assert.throws(() => configureGoogle('{', output, true));
    assert.equal(existsSync(output), false);
    writeFileSync(output, 'stale');
    assert.throws(() => configureGoogle(undefined, output, true));
    assert.equal(existsSync(output), false);
    assert.equal(configureGoogle(undefined, output, false), false);
  } finally { rmSync(dir, {recursive: true, force: true}); }
});
