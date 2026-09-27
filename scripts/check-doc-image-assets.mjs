// Fail-closed guard for the image formats that trigger the image-size
// advisories accepted by scripts/check-npm-audit.mjs. Docusaurus reads files
// from these roots during its static build, so an extension-only check is not
// enough: a renamed file must not bypass the guard.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

export const SCAN_ROOTS = ["docs", "website"];
export const FORBIDDEN_EXTENSIONS = new Set([".avif", ".heic", ".heif", ".icns", ".jxl"]);

const HEIF_BRANDS = new Set(["avif", "mif1", "msf1", "heic", "heix", "hevc", "hevx"]);

function ascii(buffer, start, end) {
  return buffer.length >= end ? buffer.toString("ascii", start, end) : "";
}

function hasBoxWithBrand(buffer, topLevelType, brand) {
  if (ascii(buffer, 4, 8) !== topLevelType) return false;

  let offset = 0;
  while (offset + 8 <= buffer.length) {
    const size = buffer.readUInt32BE(offset);
    if (size > 0 && (size < 8 || offset + size > buffer.length)) return false;
    if (ascii(buffer, offset + 4, offset + 8) === "ftyp") {
      return ascii(buffer, offset + 8, offset + 12) === brand;
    }
    offset += size > 0 ? size : 8;
  }
  return false;
}

/** Return the risky image signatures found in a file buffer. */
export function detectForbiddenSignatures(buffer) {
  const signatures = [];
  if (ascii(buffer, 0, 4) === "icns") signatures.push("ICNS");
  if (buffer.length >= 2 && buffer[0] === 0xff && buffer[1] === 0x0a) {
    signatures.push("JPEG XL codestream");
  }
  if (hasBoxWithBrand(buffer, "JXL ", "jxl ")) signatures.push("JPEG XL container");

  if (ascii(buffer, 4, 8) === "ftyp") {
    const brand = ascii(buffer, 8, 12);
    if (HEIF_BRANDS.has(brand)) signatures.push(`${brand.toUpperCase()}/HEIF container`);
  }
  return signatures;
}

/**
 * Inspect an array of { path, contents } entries and return human-readable
 * findings. Keeping this pure makes the signature checks unit-testable without
 * creating files in the repository.
 */
export function findForbiddenAssets(files) {
  const findings = [];
  for (const file of files) {
    const extension = path.extname(file.path).toLowerCase();
    if (FORBIDDEN_EXTENSIONS.has(extension)) {
      findings.push(`${file.path}: forbidden image extension ${extension}`);
    }
    for (const signature of detectForbiddenSignatures(file.contents)) {
      findings.push(`${file.path}: forbidden ${signature} signature`);
    }
  }
  return findings;
}

export function listRepositoryFiles(repoRoot = REPO_ROOT, roots = SCAN_ROOTS) {
  const output = execFileSync(
    "git",
    ["ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", ...roots],
    { cwd: repoRoot, encoding: "buffer", maxBuffer: 64 * 1024 * 1024 },
  );
  return output
    .toString("utf8")
    .split("\0")
    .filter(Boolean)
    .filter((relativePath) => {
      try {
        return statSync(path.join(repoRoot, relativePath)).isFile();
      } catch {
        return false;
      }
    });
}

export function scanRepository(repoRoot = REPO_ROOT, roots = SCAN_ROOTS) {
  const missingRoots = roots.filter((root) => {
    const absolute = path.join(repoRoot, root);
    return !existsSync(absolute) || !statSync(absolute).isDirectory();
  });
  if (missingRoots.length > 0) {
    throw new Error(`scan root(s) missing or not directories: ${missingRoots.join(", ")}`);
  }

  const relativePaths = listRepositoryFiles(repoRoot, roots);
  if (relativePaths.length === 0) {
    throw new Error(`no files selected under ${roots.join(" and ")}; refusing a vacuous image guard`);
  }

  const files = relativePaths.map((relativePath) => ({
    path: relativePath,
    contents: readFileSync(path.join(repoRoot, relativePath)),
  }));
  return { files, findings: findForbiddenAssets(files) };
}

export function main() {
  try {
    const { files, findings } = scanRepository();
    if (findings.length > 0) {
      console.error("documentation image input guard failed:");
      for (const finding of findings) console.error(`  ${finding}`);
      process.exitCode = 1;
      return;
    }
    console.log(`documentation image input guard passed (${files.length} files scanned)`);
  } catch (error) {
    console.error(`documentation image input guard failed: ${error.message}`);
    process.exitCode = 1;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main();
}
