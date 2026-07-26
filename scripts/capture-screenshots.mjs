// Capture the demo UI screenshots embedded in README.md.
//
// This runs INSIDE a Microsoft Playwright container, not on the host: the
// repository has no browser toolchain and the development environment has no
// system Chromium, so the browser is borrowed from an image rather than added
// as a project dependency. scripts/capture-screenshots.sh is the entry point —
// it starts that container on the demo stack's compose network, copies this
// file in, runs it, and copies the PNGs back out to docs/img/. Running this
// file directly only works somewhere `playwright` and a browser are installed
// and the demo API is reachable at BASE_URL.
//
// The dataset is deterministic (demogen, seed 20260701 — see docs/demo-tour.md),
// so the same customers, alerts and cases appear in the same places on every
// run. The PNGs are still NOT byte-identical between runs: the UI renders
// relative time labels ("3 days ago"), which drift against the wall clock, and
// chart animations do not land on identical sub-pixel values. Re-run this only
// when the UI itself has changed, and expect a real diff when you do.
//
// Three shots are captured; README.md embeds two of them (the dashboard and
// the customer detail). demo-case.png is captured and committed for future
// documentation use and is not yet referenced anywhere.
//
// No list view is captured, deliberately. Every list page in the UI --
// /customers, /alerts, /cases, /transactions -- currently renders as empty
// regardless of the data behind it: request() in ui/src/lib/api.ts returns the
// API's {"data": [...]} envelope, while the list pages treat that object as
// the array itself, so they all show "0 items". Detail pages return a bare
// object and are unaffected, which is why the customer and case shots below
// are fine. Add a list shot back here once that is fixed; a screenshot of an
// empty table is worse than no screenshot.

import { chromium } from 'playwright';

const BASE_URL = process.env.BASE_URL || 'http://api:8080';
const OUT_DIR = process.env.OUT_DIR || '/out';

// Fixed IDs from the deterministic demo dataset (deploy/seed/demo/STORY_IDS.md).
const SHOTS = [
  { name: 'demo-dashboard.png', path: '/' },
  {
    name: 'demo-customer-cdd.png',
    path: '/customers/61a626c6-ced4-536d-be74-41d6ca874e4d',
  },
  {
    name: 'demo-case.png',
    path: '/cases/3a55610e-d00f-5a34-8bfa-cc9753cbfa06',
  },
];

// Chart animations and post-load data fetches finish well inside this window.
// A fixed settle beats a selector wait here because the pages share no single
// "done" element.
const SETTLE_MS = 1500;

// Every page is loaded twice, and the screenshot is taken after the second
// load. This is not superstition: the UI renders raw i18n keys
// ("nav.dashboard") instead of labels on a page's first load, reproducibly.
// ui/src/main.tsx starts rendering without awaiting initI18n(), the
// translation catalog is a dynamically imported chunk, and nothing re-renders
// when it arrives (addResourceBundle does not, by design), so a first paint
// that beats the chunk keeps the keys on screen. After a reload the chunk is
// in the renderer's memory cache and resolves before the first paint every
// time.
//
// The reloaded state is the honest one to publish -- it is what the UI shows
// on every visit after the first, and what it shows the moment anything
// re-renders -- but the first-load flash is a real defect worth fixing in the
// UI rather than only here.
//
// assertNoRawKeys is the backstop: if this ever stops working, the run fails
// instead of quietly writing a screenshot full of untranslated keys.
async function assertNoRawKeys(page, name) {
  const nav = page.locator('nav').first();
  if ((await nav.count()) === 0) return;
  const text = await nav.innerText();
  if (/\bnav\.[a-z]/.test(text)) {
    throw new Error(
      `${name} shows untranslated i18n keys in the sidebar; refusing to save it`,
    );
  }
}

async function main() {
  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    colorScheme: 'light',
  });

  const failures = [];

  for (const shot of SHOTS) {
    const url = `${BASE_URL}${shot.path}`;
    const out = `${OUT_DIR}/${shot.name}`;
    try {
      const page = await context.newPage();
      await page.goto(url, { waitUntil: 'networkidle' });
      await page.waitForTimeout(SETTLE_MS);
      // See the note above assertNoRawKeys: the second load is the one worth
      // photographing.
      await page.reload({ waitUntil: 'networkidle' });
      await page.waitForTimeout(SETTLE_MS);
      await assertNoRawKeys(page, shot.name);
      await page.screenshot({ path: out, fullPage: false });
      await page.close();
      console.log(`captured ${shot.name} from ${url}`);
    } catch (err) {
      failures.push(`${shot.name} (${url}): ${err.message}`);
      console.error(`FAILED ${shot.name} from ${url}: ${err.message}`);
    }
  }

  await context.close();
  await browser.close();

  if (failures.length > 0) {
    console.error(`\n${failures.length} screenshot(s) failed:`);
    for (const failure of failures) {
      console.error(`  - ${failure}`);
    }
    process.exit(1);
  }

  console.log(`\nAll ${SHOTS.length} screenshots captured to ${OUT_DIR}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
