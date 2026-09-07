import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  detectForbiddenSignatures,
  findForbiddenAssets,
  scanRepository,
} from "./check-doc-image-assets.mjs";

const SCRIPT = path.join(path.dirname(fileURLToPath(import.meta.url)), "check-doc-image-assets.mjs");

function box(type, size = 16) {
  const result = Buffer.alloc(size);
  result.writeUInt32BE(size, 0);
  result.write(type, 4, "ascii");
  return result;
}

test("safe image files do not trigger the guard", () => {
  assert.deepEqual(findForbiddenAssets([{ path: "docs/img/logo.png", contents: Buffer.from("PNG") }]), []);
});

test("forbidden extensions are matched case-insensitively", () => {
  const findings = findForbiddenAssets([
    { path: "website/static/icon.AVIF", contents: Buffer.from("not an image") },
    { path: "website/static/icon.JxL", contents: Buffer.from("not an image") },
  ]);
  assert.equal(findings.length, 2);
  assert.match(findings[0], /forbidden image extension \.avif/);
  assert.match(findings[1], /forbidden image extension \.jxl/);
});

test("renamed ICNS and JPEG XL codestreams are rejected", () => {
  const findings = findForbiddenAssets([
    { path: "docs/img/renamed.png", contents: Buffer.from("icns\0\0\0\0") },
    { path: "docs/img/renamed.webp", contents: Buffer.from([0xff, 0x0a, 0x01]) },
  ]);
  assert.equal(findings.length, 2);
  assert.match(findings[0], /ICNS signature/);
  assert.match(findings[1], /JPEG XL codestream signature/);
});

test("HEIF and JPEG XL container signatures are rejected", () => {
  const jxl = Buffer.concat([box("JXL ", 16), box("ftyp", 16)]);
  jxl.write("jxl ", 24, "ascii");
  const heif = box("ftyp", 16);
  heif.write("heic", 8, "ascii");
  const findings = findForbiddenAssets([
    { path: "website/static/renamed.png", contents: jxl },
    { path: "website/static/renamed.jpg", contents: heif },
  ]);
  assert.deepEqual(detectForbiddenSignatures(jxl), ["JPEG XL container"]);
  assert.deepEqual(detectForbiddenSignatures(heif), ["HEIC/HEIF container"]);
  assert.equal(findings.length, 2);
});

test("zero-sized ISO BMFF boxes use the parser's eight-byte step", () => {
  const jxlSignature = box("JXL ", 8);
  jxlSignature.writeUInt32BE(0, 0);
  const jxlType = box("ftyp", 16);
  jxlType.write("jxl ", 8, "ascii");

  const heif = box("ftyp", 16);
  heif.writeUInt32BE(0, 0);
  heif.write("avif", 8, "ascii");

  assert.deepEqual(detectForbiddenSignatures(Buffer.concat([jxlSignature, jxlType])), [
    "JPEG XL container",
  ]);
  assert.deepEqual(detectForbiddenSignatures(heif), ["AVIF/HEIF container"]);
});

test("the real repository scan has roots and a non-vacuous file set", () => {
  const result = scanRepository();
  assert.ok(result.files.length > 0);
  assert.deepEqual(result.findings, []);
});

test("the CLI reports a successful guarded scan", () => {
  const output = execFileSync(process.execPath, [SCRIPT], { encoding: "utf8" });
  assert.match(output, /documentation image input guard passed \(\d+ files scanned\)/);
});
