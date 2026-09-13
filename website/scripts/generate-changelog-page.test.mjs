import { test } from "node:test";
import assert from "node:assert/strict";

import * as generator from "./generate-changelog-page.mjs";

const CHANGELOG = `# Changelog

## [Unreleased]

Work in progress.

## [0.0.1]

The first stable release.
`;

test("renderPages publishes stable sections without Unreleased", () => {
  assert.equal(typeof generator.renderPages, "function");
  const pages = generator.renderPages(CHANGELOG);

  assert.match(pages.en, /## 0\.0\.1/);
  assert.doesNotMatch(pages.en, /## Unreleased/);
});

test("renderPages is identical for LF and CRLF changelogs", () => {
  assert.deepEqual(
    generator.renderPages(CHANGELOG),
    generator.renderPages(CHANGELOG.replaceAll("\n", "\r\n"))
  );
});

test("renderPages states the strict single stable release channel", () => {
  const pages = generator.renderPages(CHANGELOG);

  assert.match(pages.en, /single release channel/);
  assert.match(pages.en, /`vX\.Y\.Z`/);
  assert.match(pages.en, /pre-release identifiers are rejected/);
  assert.match(pages.ja, /単一のリリースチャネル/);
  assert.match(pages.ja, /プレリリース識別子は拒否/);
});

test("findPageDrift ignores line-ending conversion", () => {
  const pages = generator.renderPages(CHANGELOG);
  const crlfPages = Object.fromEntries(
    Object.entries(pages).map(([locale, page]) => [
      locale,
      page.replaceAll("\n", "\r\n"),
    ])
  );

  assert.deepEqual(generator.findPageDrift(pages, crlfPages), []);
});

test("findPageDrift reports changed generated content", () => {
  const pages = generator.renderPages(CHANGELOG);
  const changed = { ...pages, en: `${pages.en}\nmanual drift\n` };

  assert.deepEqual(generator.findPageDrift(pages, changed), ["en"]);
});

test("findPageDrift reports a missing generated locale", () => {
  const pages = generator.renderPages(CHANGELOG);

  assert.deepEqual(generator.findPageDrift(pages, { en: pages.en }), ["ja"]);
});
