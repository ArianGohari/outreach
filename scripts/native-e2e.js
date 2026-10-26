(async () => {
  const api = window.go.main.App;
  const assert = (ok, message) => {
    if (!ok) throw new Error(message);
  };
  const visible = (el) => !!el && el.getClientRects().length > 0;
  async function until(check, label) {
    const end = Date.now() + 20000;
    while (Date.now() < end) {
      const result = await check();
      if (result) return result;
      await new Promise((r) => setTimeout(r, 50));
    }
    throw new Error(`Timed out: ${label}`);
  }
  async function click(name) {
    const el = await until(
      () =>
        [...document.querySelectorAll("button")].find(
          (b) =>
            visible(b) &&
            !b.disabled &&
            (b.getAttribute("aria-label") || b.textContent.trim()) === name,
        ),
      name,
    );
    el.click();
  }
  function input(name, value) {
    const label = [...document.querySelectorAll("label")].find(
      (el) =>
        visible(el) && el.querySelector("span")?.textContent.trim() === name,
    );
    const el = label?.querySelector("input,textarea");
    assert(el, `Missing input ${name}`);
    el.value = value;
    el.dispatchEvent(new Event("input", { bubbles: true }));
  }
  const text = () =>
    [...document.querySelectorAll('[role="tabpanel"]')]
      .filter(visible)
      .map((el) => el.textContent)
      .join(" ");
  async function settled(id) {
    return until(async () => {
      const s = await api.GetState(id);
      return !s.running && s.deliveries.length ? s : false;
    }, "worker completion");
  }
  try {
    assert(document.documentElement.lang === "en", "app language must be English");
    await click("Create new job");
    await until(
      () =>
        visible(document.querySelector(".job-editor")) &&
        [...document.querySelectorAll("button")].some(
          (b) => b.textContent.trim() === "Save draft" && !b.disabled,
        ),
      "editor",
    );
    input("Subject", "Hallo {{First Name}} {{Last Name}}");
    input("Text", "Hi {{First Name}} at {{Company Name}}");
    await click("Validate & preview");
    await until(() => text().includes("Hallo Jane Doe"), "Jane Doe preview");
    assert(
      [...document.querySelectorAll("button")].find(
        (b) => visible(b) && b.textContent.trim() === "Send",
      ).disabled,
      "example authorized send",
    );
    input("Subject", "");
    await click("Validate & preview");
    await until(
      () =>
        document
          .querySelector('[role="alert"]')
          ?.textContent.includes("subject is empty"),
      "native error details",
    );
    assert(
      !document
        .querySelector('[role="alert"]')
        .textContent.includes("[object Object]"),
      "structured error corrupted",
    );
    const receipt = await api.ReportClientError("UI_BATCH_RANGE");
    assert(
      receipt.logged && receipt.id,
      "log receipt rejected by native binding",
    );
    input("Subject", "Hallo {{First Name}}");
    input("SMTP server", "127.0.0.1");
    input("Port", String(window.__nativeTest.port));
    input("Username", "sender@example.org");
    input("Password", "synthetic-password");
    input("Pause per email (seconds)", "1");
    input("Pause per batch (hours)", "0");
    await click("Check connection");
    await until(() => text().includes("Connected"), "TLS connection");
    await click("Choose file");
    await until(() => text().includes("synthetic.csv"), "native CSV import");
    const verified = [...document.querySelectorAll("label")]
      .find((el) => visible(el) && el.textContent.trim() === "Verified only")
      ?.querySelector("input");
    assert(verified?.checked, "verification default lost");
    verified.click();
    await until(
      () => text().includes("2 eligible"),
      "filter and CSV persistence",
    );
    await click("Check contacts");
    await click("Validate & preview");
    await until(() => text().includes("Hallo Lena"), "real contact preview");
    input("Test email to", "test@example.org");
    await click("Send test email");
    await until(() => text().includes("Test email accepted by SMTP"), "testmail");
    const workspace = await api.GetWorkspace();
    const id = workspace[0].id;
    await click("Send");
    const state = await settled(id);
    assert(
      state.deliveries.every((d) => d.status === "sent"),
      "delivery state not sent",
    );
    // Retrying an approved plan through the actual native bindings must not send again.
    const req = state.plan.request;
    req.config.password = "synthetic-password";
    await api.CheckConnection(id, req.config);
    const token = await api.SendTest(id, req, 0, "test@example.org");
    await api.Start(id, req, token);
    await settled(id);
    await click("Close tab: New job");
    await click("Delete job: New job");
    await click("Confirm deletion: New job");
    await until(
      async () => (await api.GetWorkspace()).length === 0,
      "delete through UI",
    );
    const next = await api.CreateJob("Duplicate check");
    await api.SaveDraft(next.id, next.name, req);
    await api.ChooseCSV(next.id, false);
    await api.CheckConnection(next.id, req.config);
    const proof = await api.SendTest(next.id, req, 0, "test@example.org");
    await api.Start(next.id, req, proof);
    const blocked = await settled(next.id);
    assert(
      blocked.deliveries.every((d) => d.status === "blocked"),
      "delete removed duplicate protection",
    );
    await api.DeleteJob(next.id);
    window.runtime.EventsEmit("native-test-result", "ok");
  } catch (e) {
    window.runtime.EventsEmit("native-test-result", String(e.stack || e));
  }
})();
