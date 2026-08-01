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
// Four shots are captured. README.md embeds two of them (the dashboard and the
// customer detail); demo-case.png and demo-customers.png are captured and
// committed for documentation use and are not referenced from README yet.
//
// demo-customers.png doubles as a check on the API list envelope: every list
// route answers with {"data": [...], "pagination": {...}}, and a client that
// treats that object as the array renders an empty table with the data sitting
// right there. That is not hypothetical -- it is why no list shot was captured
// until ui/src/lib/api.ts was corrected. A blank table here means the envelope
// handling has regressed, not that the demo failed to seed.

import { chromium } from 'playwright';

const BASE_URL = process.env.BASE_URL || 'http://api:8080';
const OUT_DIR = process.env.OUT_DIR || '/out';

// Fixed IDs from the deterministic demo dataset (deploy/seed/demo/STORY_IDS.md).
const SHOTS = [
  { name: 'demo-dashboard.png', path: '/' },
  {
    name: 'demo-customers.png',
    path: '/customers',
    // The sidebar makes even an empty customer page large enough to pass the
    // PNG byte-size check. Require a real data row so this image remains an
    // executable regression check for the paginated list envelope.
    requiredSelector: 'tbody a[href^="/customers/"]',
  },
  {
    name: 'demo-customer-cdd.png',
    path: '/customers/61a626c6-ced4-536d-be74-41d6ca874e4d',
  },
  {
    name: 'demo-case.png',
    path: '/cases/3a55610e-d00f-5a34-8bfa-cc9753cbfa06',
    contrastSelector: '.bg-destructive',
  },
];

// Chart animations and post-load data fetches finish well inside this window.
// A fixed settle beats a selector wait here because the pages share no single
// "done" element.
const SETTLE_MS = 1500;

// Each page is loaded exactly once. assertNoRawKeys makes the capture an
// executable regression check for first-load i18n initialization: a raw key
// must fail the run instead of being hidden by a cache-warming reload.
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

async function assertBrandLogo(page, name) {
  try {
    await page.getByRole('img', { name: 'Merlon' }).first().waitFor({
      state: 'visible',
      timeout: 10_000,
    });
  } catch {
    throw new Error(`${name} does not show the Merlon brand logo`);
  }
}

function relativeLuminance([red, green, blue]) {
  const linear = [red, green, blue].map((channel) => {
    const value = channel / 255;
    return value <= 0.04045
      ? value / 12.92
      : ((value + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2];
}

function contrastRatio(foreground, background) {
  const lighter = Math.max(
    relativeLuminance(foreground),
    relativeLuminance(background),
  );
  const darker = Math.min(
    relativeLuminance(foreground),
    relativeLuminance(background),
  );
  return (lighter + 0.05) / (darker + 0.05);
}

async function assertContrast(page, selector, name) {
  const target = page.locator(selector).first();
  await target.waitFor({ state: 'visible', timeout: 10_000 });
  const colors = await target.evaluate((element) => {
    const style = getComputedStyle(element);
    const canvas = document.createElement('canvas');
    canvas.width = 1;
    canvas.height = 1;
    const context = canvas.getContext('2d', { willReadFrequently: true });
    if (!context) throw new Error('could not create a color sampling context');

    function sample(color) {
      context.clearRect(0, 0, 1, 1);
      context.fillStyle = color;
      context.fillRect(0, 0, 1, 1);
      return Array.from(context.getImageData(0, 0, 1, 1).data.slice(0, 3));
    }

    return {
      foreground: sample(style.color),
      background: sample(style.backgroundColor),
    };
  });
  const ratio = contrastRatio(colors.foreground, colors.background);
  if (ratio < 4.5) {
    throw new Error(
      `${name} has ${ratio.toFixed(2)}:1 foreground contrast for ${selector}; expected at least 4.5:1`,
    );
  }
  console.log(
    `verified ${name} foreground contrast for ${selector}: ${ratio.toFixed(2)}:1`,
  );
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
      await assertNoRawKeys(page, shot.name);
      await assertBrandLogo(page, shot.name);
      if (shot.requiredSelector) {
        await page.locator(shot.requiredSelector).first().waitFor({
          state: 'visible',
          timeout: 10_000,
        });
      }
      if (shot.contrastSelector) {
        await assertContrast(page, shot.contrastSelector, shot.name);
      }
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
