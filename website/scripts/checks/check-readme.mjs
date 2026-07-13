import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { REPO_ROOT } from "./lib.mjs";

const REQUIRED_SECTIONS = [
  "License",
  "Architecture",
  "Quick Start",
  "Development",
  "Documentation",
  "Status",
  "Production Warning",
];

export function run() {
  const file = "README.md";
  const abs = path.join(REPO_ROOT, file);
  if (!existsSync(abs)) return [{ file, reason: "README.md is missing" }];
  const body = readFileSync(abs, "utf8");
  const violations = [];
  for (const section of REQUIRED_SECTIONS) {
    if (!new RegExp(`^## ${section.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*$`, "m").test(body)) {
      violations.push({ file, reason: `missing required section: ## ${section}` });
    }
  }
  const links = [...body.matchAll(/\[[^\]]+\]\(([^)#]+)(?:#[^)]+)?\)/g)];
  for (const [, target] of links) {
    if (/^(?:https?:|mailto:)/.test(target)) continue;
    if (!existsSync(path.resolve(REPO_ROOT, target))) {
      violations.push({ file, reason: `broken relative link: ${target}` });
    }
  }
  return violations;
}
