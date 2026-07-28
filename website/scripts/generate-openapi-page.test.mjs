import assert from "node:assert/strict";
import test from "node:test";

import {
  dominantPathPrefix,
  renderOpenApiPage,
} from "./generate-openapi-page.mjs";
import { LOCALES } from "./lib/locales.mjs";

const SPEC = {
  openapi: "3.1.0",
  info: { version: "1.0.0" },
  // What the real document looks like: the mount point lives in the path keys,
  // and servers[0].url is "/" because repeating it there made every effective
  // URL /api/v1/api/v1/...
  servers: [{ url: "/" }],
  paths: {
    "/healthz": { get: {} },
    "/healthz/live": { get: {} },
    "/api/v1/customers": { get: {} },
    "/api/v1/alerts": { get: {}, post: {} },
    "/api/v1/cases": { get: {} },
  },
};

test("the shared mount point of the API paths is the base path", () => {
  assert.equal(dominantPathPrefix(SPEC.paths), "/api/v1");
});

// The probes are mounted at the root on purpose. Demanding a prefix common to
// every path would return "" for exactly the document this describes.
test("root-mounted probes do not erase the base path", () => {
  assert.equal(
    dominantPathPrefix({
      "/healthz": {},
      "/api/v1/alerts": {},
      "/api/v1/cases": {},
    }),
    "/api/v1"
  );
});

test("a document with no paths has no base path", () => {
  assert.equal(dominantPathPrefix({}), "");
  assert.equal(dominantPathPrefix(undefined), "");
});

test("a partial overlap within the largest group stops at the shared segment", () => {
  assert.equal(
    dominantPathPrefix({ "/api/v1/alerts": {}, "/api/v2/alerts": {} }),
    "/api"
  );
});

// The regression this file exists for: servers[0].url was printed verbatim, so
// correcting it in the spec to "/" silently degraded the page to
// "| Base path | `/` |" with nothing asserting the field was still useful.
test("the rendered page shows a usable base path, never bare /", () => {
  for (const locale of Object.keys(LOCALES)) {
    const page = renderOpenApiPage(SPEC, LOCALES[locale]);
    const row = page
      .split("\n")
      .find((line) => line.includes(LOCALES[locale].basePath));
    assert.ok(row, `${locale}: no base path row`);
    assert.match(row, /`\/api\/v1`/, `${locale}: ${row}`);
    assert.doesNotMatch(row, /`\/`/, `${locale}: ${row}`);
  }
});

test("a declared server is used when the paths yield no prefix", () => {
  const spec = {
    ...SPEC,
    servers: [{ url: "https://merlon.example/api" }],
    paths: {},
  };
  const page = renderOpenApiPage(spec, LOCALES.en);
  assert.ok(page.includes("`https://merlon.example/api`"), page);
});
