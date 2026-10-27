# Google / Workspace

Select **Google / Workspace**, then **Connect Google**. Sign in through your browser and allow sending emails. Official releases include Outreach’s Google client; no JSON import is needed.

Each account signs in separately. Jobs can reuse a connection through **Use existing account**. Check the connection, preview and send a test email before starting outreach. The sender must match the Google account; aliases are not supported.

Outreach requests `gmail.send`, `openid` and `email`. It cannot read your mailbox. Refresh credentials stay in Keychain / Windows Credential Manager; access tokens stay in memory. Existing connections retain their original OAuth client after an update.

**Remove Google connection** removes local access and clears affected sending approvals. Stop running jobs first. To revoke Google consent too, remove Outreach from your Google Account’s connections.

Google messages include matching text and HTML versions so Workspace can append a [formatted organization footer](https://knowledge.workspace.google.com/admin/gmail/advanced/add-a-standard-footer-to-outgoing-emails). Outreach does not fetch account signatures. Provider footers appear in the received test email, not the local preview.

## Build configuration

1. Enable **Gmail API** in Google Cloud and configure Google Auth Platform. In **External / Testing**, add each sender as a test user.
2. Create a **Desktop app** OAuth client and download its JSON.
3. For local builds, run:

```sh
node scripts/configure-google.mjs /absolute/path/to/desktop-client.json --required
```

Then build normally. The generated `internal/googlemail/client_build.go` is ignored by Git. It contains only the desktop client ID and client secret; these identify the public desktop app and are extractable from distributed binaries. Never supply or bundle account tokens or service-account credentials.

GitHub builds read the repository Actions secret **GOOGLE_OAUTH_CLIENT_JSON**. Version-tag builds fail if it is missing or invalid. Pull-request builds receive no client configuration. Developers can build without it for SMTP work; Google sign-in then requires a previously configured local client or the command above.

## Authorization check without sending

```sh
go run ./cmd/google-check /absolute/path/to/desktop-client.json
```

This explicitly uses the supplied client, opens the browser and checks sending permission. It never sends mail or opens the outreach database. Its temporary account credential is removed afterwards; the developer client stays in the local keyring.

## Limits

- Google’s External / Testing mode expires Gmail authorizations after seven days. Reconnect, or configure production publishing and any required Google verification.
- Workspace policy may require administrator approval. Outreach cannot bypass it. Some policy errors appear only in the browser.
- Missing API activation, denied permission, expired consent or rate limits stop sending with an error. Uncertain results remain locked; message POSTs are never retried automatically.

References: [desktop OAuth](https://developers.google.com/identity/protocols/oauth2/native-app), [Gmail scopes](https://developers.google.com/workspace/gmail/api/auth/scopes), [testing mode](https://support.google.com/cloud/answer/15549945), [Workspace access controls](https://support.google.com/a/answer/7281227).
