import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const workflow = readFileSync(
  path.join(__dirname, "..", ".github", "workflows", "security.yml"),
  "utf8",
);

function secretScanJob() {
  const start = workflow.indexOf("  gitleaks:\n");
  const end = workflow.indexOf("\n  govulncheck:", start);
  assert.notEqual(start, -1, "Security workflow must define the gitleaks job");
  assert.notEqual(end, -1, "gitleaks job must end before govulncheck");
  return workflow.slice(start, end);
}

test("secret scan verifies complete reachable history with the pinned scanner", () => {
  const job = secretScanJob();

  assert.match(job, /fetch-depth: 0/);
  assert.match(job, /GITLEAKS_VERSION: "8\.24\.3"/);
  assert.match(
    job,
    /- name: Verify complete reachable history\n\s+run: gitleaks detect --source=\. --redact=100 --exit-code=1 --no-banner --no-color/,
  );

  const fullHistoryStep = job.slice(
    job.indexOf("- name: Verify complete reachable history"),
  );
  assert.doesNotMatch(fullHistoryStep, /--log-opts/);
});
