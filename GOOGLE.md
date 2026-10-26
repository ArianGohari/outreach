# Google / Workspace

Google accounts use the Gmail API with `gmail.send`, plus `openid` and `email` to identify the signed-in sender. Outreach does not request mailbox-reading access. Other providers keep using SMTP.

## Setup

1. Enable **Gmail API** in your Google Cloud project.
2. Configure Google Auth Platform. For initial testing, use **External / Testing** and add each sender as a test user.
3. Create an OAuth client of type **Desktop app** and download its JSON.
4. In a job, select **Google / Workspace**, choose **OAuth-JSON auswählen**, then **Mit Google verbinden**.
5. Complete consent in your system browser. Back in Outreach, check the connection, import contacts, preview and send a test email before starting outreach.

Each account signs in separately. Jobs can reuse a connected account through **Absender-Konto übernehmen**. The sender must match the Google account; aliases are not supported yet. Reply-To remains optional.

The OAuth client and refresh credentials are stored in Keychain / Windows Credential Manager. Access tokens stay in memory. SQLite contains only the account address and an opaque connection reference. Client JSON and tokens must not be committed or included in releases.

**Google-Verbindung entfernen** deletes the local credential and clears approval for jobs sharing that connection. Running jobs must stop first. This does not revoke consent at Google; remove Outreach from your Google Account's connections to revoke that consent too.

## First login check without sending

From the repository, run:

```sh
go run ./cmd/google-check /absolute/path/to/desktop-client.json
```

This imports the client into the OS keyring, opens the browser and verifies the account and granted send scope. It never opens the outreach database or sends mail. Its temporary account credential is removed afterwards; the client setup remains available to the app. Success proves authorization, not message delivery or Gmail API availability.

## Limits and failures

- In Google's external Testing mode, authorizations and refresh tokens for Gmail access expire after seven days. Reconnect, or configure production publishing and any required Google verification.
- Workspace policy can require administrator approval even for sending only. Outreach cannot bypass that policy. Some policy errors appear only in the browser; copy the displayed error if the app times out.
- Missing Gmail API activation, denied permission, expired consent and rate limits stop sending with an error. HTTP 5xx, a lost response or an invalid success response leave the recipient uncertain and locked.
- Access tokens are refreshed before a send when needed. Message POSTs are never automatically retried, including after HTTP 401 or 429. Existing cross-job duplicate protection applies to SMTP and Google alike.
- A released build contains no OAuth client credentials. Each installation imports its own desktop-client JSON.

References: [desktop OAuth](https://developers.google.com/identity/protocols/oauth2/native-app), [Gmail scopes](https://developers.google.com/workspace/gmail/api/auth/scopes), [testing mode](https://support.google.com/cloud/answer/15549945), [Workspace access controls](https://support.google.com/a/answer/7281227).
