import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import assert from "node:assert/strict";
const dir = mkdtempSync(join(tmpdir(), "outreach-native-"));
const exe = resolve(
  process.argv[2] ||
    `build/bin/outreach-native${process.platform === "win32" ? ".exe" : ""}`,
);
const env = { ...process.env, CGO_ENABLED: "1" };
if (process.platform === "darwin") {
  env.CGO_CFLAGS = "-mmacosx-version-min=13.0";
  env.CGO_LDFLAGS =
    "-mmacosx-version-min=13.0 -framework UniformTypeIdentifiers";
}
const build = spawnSync(
  "go",
  [
    "build",
    "-race",
    "-tags",
    "production,integration,nativee2e",
    "-o",
    exe,
    ".",
  ],
  { env, stdio: "inherit" },
);
if (build.error) throw build.error;
assert.equal(build.status, 0, "Native test build failed");
const proc = spawn(exe, [dir], { stdio: "inherit" });
const timer = setTimeout(() => proc.kill("SIGKILL"), 110000);
const status = await new Promise((resolve, reject) => {
  proc.once("error", reject);
  proc.once("exit", resolve);
});
clearTimeout(timer);
assert.equal(
  status,
  0,
  `Native process exited with ${status}; artifacts: ${dir}`,
);
const result = JSON.parse(readFileSync(join(dir, "result.json"), "utf8"));
assert.equal(
  result.result,
  "ok",
  `Native UI failed: ${result.result}; artifacts: ${dir}`,
);
// Three test messages; exactly one outreach per imported contact across retry and deletion.
assert.equal(
  result.messages.length,
  5,
  "unexpected or duplicate SMTP delivery",
);
assert.deepEqual(result.recipients, [
  "<test@example.org>",
  "<lena@example.org>",
  "<max@example.org>",
  "<test@example.org>",
  "<test@example.org>",
]);
for (const [index, name] of [
  [1, "Lena"],
  [2, "Max"],
]) {
  assert.ok(result.messages[index].includes(`Hi ${name} at Example`));
  assert.ok(!result.messages[index].includes("{{"));
}
console.log(`Native WebView + Wails + SQLite + TLS SMTP: passed (${dir})`);
