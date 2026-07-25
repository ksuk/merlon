// Gate for `npm audit` that fails on any high or critical advisory unless it is
// listed in scripts/npm-audit-exceptions.json with a rationale and an expiry.
//
// The plain `npm audit --audit-level=high` it replaces had no way to record a
// decision, so an advisory with no forward fix left only two options: leave the
// gate red until upstream ships one, or drop the gate. Both lose the record of
// why the risk was accepted, which is the part an audit needs.
//
// Fail-Alert: anything unlisted fails, an expired exception fails, an exception
// that no longer matches a reported advisory fails, and an accepted advisory
// that reaches a dependent it was not accepted for fails. The last one matters
// because a rationale is only valid for the paths it was written about -- a
// build-time tool being exposed is not the same risk as the shipped bundle
// being exposed, even for an identical advisory.
//
// Usage: node scripts/check-npm-audit.mjs <workspace-dir> [more dirs...]

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const EXCEPTIONS_FILE = path.join(REPO_ROOT, "scripts", "npm-audit-exceptions.json");
const BLOCKING = new Set(["high", "critical"]);

function auditReport(dir) {
  try {
    return JSON.parse(
      execFileSync("npm", ["audit", "--json"], {
        cwd: path.resolve(REPO_ROOT, dir),
        encoding: "utf8",
        maxBuffer: 64 * 1024 * 1024,
      }),
    );
  } catch (error) {
    // npm audit exits non-zero whenever it finds anything, so a non-zero exit
    // carrying parseable output is the normal path, not a failure.
    if (!error.stdout) throw error;
    return JSON.parse(error.stdout);
  }
}

function blockingAdvisories(dir) {
  const report = auditReport(dir);
  const found = [];
  for (const [name, vulnerability] of Object.entries(report.vulnerabilities ?? {})) {
    for (const via of vulnerability.via ?? []) {
      // A string in `via` means the package is only affected through another
      // one; the advisory itself is recorded on that other package.
      if (typeof via !== "object" || !BLOCKING.has(via.severity)) continue;
      const advisory = via.url?.match(/GHSA-[a-z0-9-]+/i)?.[0];
      if (!advisory) continue;
      found.push({
        advisory,
        package: name,
        severity: via.severity,
        title: via.title,
        dependents: [...(vulnerability.effects ?? [])].sort(),
      });
    }
  }
  return found;
}

function main() {
  const workspaces = process.argv.slice(2);
  if (workspaces.length === 0) {
    console.error("usage: node scripts/check-npm-audit.mjs <workspace-dir> [more dirs...]");
    process.exit(2);
  }

  const exceptions = JSON.parse(readFileSync(EXCEPTIONS_FILE, "utf8")).exceptions ?? [];
  const today = new Date().toISOString().slice(0, 10);
  const failures = [];
  const matched = new Set();

  for (const workspace of workspaces) {
    for (const finding of blockingAdvisories(workspace)) {
      const exception = exceptions.find(
        (candidate) =>
          candidate.workspace === workspace &&
          candidate.advisory === finding.advisory &&
          candidate.package === finding.package,
      );

      if (!exception) {
        failures.push(
          `${workspace}: unreviewed ${finding.severity} advisory ${finding.advisory} in ${finding.package} -- ${finding.title}`,
        );
        continue;
      }

      matched.add(exception);

      if (exception.expires < today) {
        failures.push(
          `${workspace}: exception for ${finding.advisory} in ${finding.package} expired on ${exception.expires}; re-assess it`,
        );
        continue;
      }

      const approved = new Set(exception.dependents ?? []);
      const unapproved = finding.dependents.filter((dependent) => !approved.has(dependent));
      if (unapproved.length > 0) {
        failures.push(
          `${workspace}: ${finding.advisory} in ${finding.package} now also reaches ${unapproved.join(", ")}, which the exception does not cover; re-assess it`,
        );
        continue;
      }

      console.log(
        `${workspace}: accepted ${finding.advisory} in ${finding.package} until ${exception.expires} (${exception.reason})`,
      );
    }
  }

  for (const exception of exceptions) {
    if (!matched.has(exception)) {
      failures.push(
        `stale exception: ${exception.advisory} in ${exception.package} (${exception.workspace}) no longer matches a reported advisory; remove it`,
      );
    }
  }

  if (failures.length > 0) {
    for (const failure of failures) console.error(failure);
    process.exit(1);
  }

  console.log(`npm audit gate passed for: ${workspaces.join(", ")}`);
}

main();
