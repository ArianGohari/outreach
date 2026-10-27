<script lang="ts">
  let { headers }: { headers?: string[] } = $props();
  const standardColumns = [
    'First Name',
    'Last Name',
    'Title',
    'Company Name',
    'Email',
    'Contact Owner',
    'Person Linkedin Url',
    'City',
  ];
  const descriptions: Record<string, string> = {
    'First Name': 'First name, e.g. Jane',
    'Last Name': 'Last name, e.g. Doe',
    Title: 'Job title, e.g. Head of Sales',
    'Company Name': 'Company name, e.g. Example Inc.',
    Email: 'Contact address; does not change the recipient.',
    'Contact Owner': 'Contact owner in Apollo',
    'Person Linkedin Url': 'Contact LinkedIn profile URL',
    City: 'Contact city, e.g. London',
    Industry: 'Contact or company industry',
    'Email Status': 'Apollo verification status, e.g. verified',
    'Apollo Contact Id': 'Apollo contact identifier',
    'Email Bounced': 'Apollo bounce status',
  };
  const columns = $derived(headers ?? standardColumns);
</script>

<details class="template-guide">
  <summary>Template tags & guide</summary>
  <div class="guide-content">
    <p>
      <code>{'{{Column Name}}'}</code> inserts a CSV value into the subject or body.
    </p>
    <h3>Available tags</h3>
    <p class="hint">
      {headers ? 'Imported columns.' : 'Standard fields before CSV import.'} Use the
      exact spelling.
    </p>
    <div class="table-wrap">
      <table aria-label="Available template tags">
        <thead><tr><th>Tag</th><th>Value / meaning</th></tr></thead>
        <tbody>
          {#each columns as column}
            <tr>
              <td><code>{'{{' + column + '}}'}</code></td>
              <td
                >{descriptions[column] ??
                  'Value from the matching CSV column'}</td
              >
            </tr>
          {/each}
          {#if columns.includes('Company Name')}
            <tr>
              <td><code>{'{{Company}}'}</code></td>
              <td>Alias for <code>{'{{Company Name}}'}</code>.</td>
            </tr>
          {/if}
        </tbody>
      </table>
    </div>
    <h3>Empty fields and fallbacks</h3>
    <p>
      <code>{'{{Column Name|fallback}}'}</code> uses the fallback when the field is
      empty.
    </p>
    <ul>
      <li>
        <code>{'Hi {{First Name|there}},'}</code> produces "Hi Jane," or "Hi there,"
        if the first name is empty.
      </li>
      <li>
        <code>{'{{Company Name|your company}}'}</code> inserts the company name, or
        "your company" if empty.
      </li>
      <li>
        <code>{'{{Title|}}'}</code> inserts nothing if the title is empty. Surrounding
        text and punctuation stay in place.
      </li>
    </ul>
    <p>
      Empty fields without a fallback block sending. Missing columns are invalid
      even with a fallback.
    </p>
    <h3>Example</h3>
    <div class="guide-example">
      <span class="label">Subject</span>
      <pre>{'Quick question, {{First Name}}'}</pre>
      <span class="label">Body</span>
      <pre>{'Hi {{First Name|there}},\n\nI have a question about {{Company Name|your company}}.\nDo you have time for a quick chat this week?'}</pre>
    </div>
    <h3>Sending</h3>
    <p>
      Preview validates all contacts. Use the arrows to switch recipients. Check
      the test email in your inbox.
    </p>
    <p>
      Templates use plain text. HTML and Markdown stay literal. No conditions,
      loops, nested tags or subject line breaks. Providers may append a footer;
      check the test email.
    </p>
  </div>
</details>
