import assert from 'node:assert/strict';
import http from 'node:http';
import { chromium } from 'playwright';

const BASE_URL = process.env.BASE_URL ?? 'http://localhost:8080';
const UPSTREAM_URL = process.env.UPSTREAM_URL ?? 'http://api:8080';
const PHASE = process.env.PHASE ?? 'before-restart';
const ADMIN_EMAIL = process.env.ADMIN_EMAIL;
const ADMIN_PASSWORD = process.env.ADMIN_PASSWORD;
const ANALYST_EMAIL = process.env.ANALYST_EMAIL;
const VIEWER_EMAIL = process.env.VIEWER_EMAIL;
const USER_PASSWORD = process.env.USER_PASSWORD;
const EXPECTED_VERSION = process.env.EXPECTED_VERSION ?? 'dev';
const EXPECTED_REVISION = process.env.EXPECTED_REVISION ?? '';

for (const [name, value] of Object.entries({ ADMIN_EMAIL, ADMIN_PASSWORD, ANALYST_EMAIL, VIEWER_EMAIL, USER_PASSWORD })) {
  assert.ok(value, `${name} is required`);
}

async function login(page, email, password) {
  await page.goto(`${BASE_URL}/login`);
  await page.locator('#login-email').fill(email);
  await page.locator('#login-password').fill(password);
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/'),
    page.locator('button[type="submit"]').click(),
  ]);
}

async function api(page, method, path, body) {
  return page.evaluate(async ({ method, path, body }) => {
    const csrf = document.cookie.match(/(?:^|; )csrf_token=([^;]*)/)?.[1];
    const headers = { 'Content-Type': 'application/json' };
    if (!['GET', 'HEAD'].includes(method) && csrf) headers['X-CSRF-Token'] = decodeURIComponent(csrf);
    const response = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await response.text();
    let payload = null;
    try { payload = JSON.parse(text); } catch { payload = text; }
    return { status: response.status, contentType: response.headers.get('content-type'), payload };
  }, { method, path: `/api/v1${path}`, body });
}

async function assertAuthenticatedOpenAPI(page) {
  const result = await api(page, 'GET', '/openapi.json');
  assert.equal(result.status, 200);
  assert.match(result.contentType ?? '', /^application\/json/);
  assert.equal(result.payload.openapi, '3.0.3');
  assert.ok(result.payload.paths['/api/v1/admin/users']);
}

async function assertBuildIdentity(page) {
  const result = await api(page, 'GET', '/system/status');
  assert.equal(result.status, 200);
  assert.equal(result.payload.version, EXPECTED_VERSION);
  assert.equal(result.payload.commit, EXPECTED_REVISION);
}

function startLocalhostProxy() {
  const upstream = new URL(UPSTREAM_URL);
  const server = http.createServer((request, response) => {
    const proxy = http.request({
      hostname: upstream.hostname,
      port: upstream.port,
      method: request.method,
      path: request.url,
      headers: { ...request.headers, host: upstream.host },
    }, (upstreamResponse) => {
      response.writeHead(upstreamResponse.statusCode ?? 502, upstreamResponse.headers);
      upstreamResponse.pipe(response);
    });
    proxy.on('error', (error) => {
      response.writeHead(502, { 'content-type': 'text/plain' });
      response.end(error.message);
    });
    request.pipe(proxy);
  });
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(8080, '127.0.0.1', () => resolve(server));
  });
}

async function runBeforeRestart(browser) {
  const context = await browser.newContext();
  const page = await context.newPage();

  const anonymousOpenAPI = await page.request.get(`${BASE_URL}/api/v1/openapi.json`);
  assert.equal(anonymousOpenAPI.status(), 401, 'OpenAPI must require authentication in Standard mode');

  await page.goto(`${BASE_URL}/setup`);
  await page.locator('#setup-email').fill(ADMIN_EMAIL);
  await page.locator('#setup-password').fill(ADMIN_PASSWORD);
  await Promise.all([
    page.waitForURL((url) => url.pathname === '/login'),
    page.locator('button[type="submit"]').click(),
  ]);
  await login(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  await assertAuthenticatedOpenAPI(page);
  await assertBuildIdentity(page);

  for (const [email, role] of [[ANALYST_EMAIL, 'analyst'], [VIEWER_EMAIL, 'viewer']]) {
    const created = await api(page, 'POST', '/admin/users', { email, password: USER_PASSWORD, role });
    assert.equal(created.status, 201, `create ${role}`);
  }

  await page.goto(`${BASE_URL}/users`);
  const adminEmail = page.getByText(ADMIN_EMAIL, { exact: true });
  await adminEmail.waitFor();
  const adminCard = adminEmail.locator('xpath=ancestor::div[contains(concat(" ", normalize-space(@class), " "), " p-4 ") and .//form][1]');
  const roleSelect = adminCard.getByRole('combobox').nth(0);
  const statusSelect = adminCard.getByRole('combobox').nth(1);
  const save = adminCard.getByRole('button', { name: new RegExp(ADMIN_EMAIL) }).first();

  await roleSelect.selectOption('viewer');
  let responsePromise = page.waitForResponse((response) => response.url().includes('/api/v1/admin/users/') && response.request().method() === 'PATCH');
  await save.click();
  let response = await responsePromise;
  assert.equal(response.status(), 409, 'last Admin demotion must be rejected');
  assert.equal((await response.json()).error_code, 'conflict');
  await adminCard.getByRole('alert').waitFor();

  await roleSelect.selectOption('admin');
  await statusSelect.selectOption('inactive');
  responsePromise = page.waitForResponse((candidate) => candidate.url().includes('/api/v1/admin/users/') && candidate.request().method() === 'PATCH');
  await save.click();
  response = await responsePromise;
  assert.equal(response.status(), 409, 'last Admin deactivation must be rejected');
  assert.equal((await response.json()).error_code, 'conflict');

  const users = await api(page, 'GET', '/admin/users');
  assert.equal(users.status, 200);
  const admin = users.payload.find((user) => user.email === ADMIN_EMAIL);
  assert.deepEqual({ role: admin.role, active: admin.active }, { role: 'admin', active: true });

  const analystContext = await browser.newContext();
  const analystPage = await analystContext.newPage();
  await login(analystPage, ANALYST_EMAIL, USER_PASSWORD);
  assert.equal((await api(analystPage, 'GET', '/auth/me')).payload.role, 'analyst');
  await analystContext.close();

  const viewerContext = await browser.newContext();
  const viewerPage = await viewerContext.newPage();
  await login(viewerPage, VIEWER_EMAIL, USER_PASSWORD);
  assert.equal((await api(viewerPage, 'GET', '/auth/me')).payload.role, 'viewer');
  const denied = await api(viewerPage, 'POST', '/admin/users', {
    email: 'forbidden@example.invalid', password: USER_PASSWORD, role: 'viewer',
  });
  assert.equal(denied.status, 403, 'Viewer mutation must be forbidden');
  await viewerContext.close();
  await context.close();
}

async function runAfterRestart(browser) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await login(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  await assertAuthenticatedOpenAPI(page);
  await assertBuildIdentity(page);
  const users = await api(page, 'GET', '/admin/users');
  assert.equal(users.status, 200);
  for (const expected of [ADMIN_EMAIL, ANALYST_EMAIL, VIEWER_EMAIL]) {
    assert.ok(users.payload.some((user) => user.email === expected), `${expected} persisted after restart`);
  }
  await context.close();
}

// Session cookies are Secure. Chromium treats localhost as a trustworthy
// development origin, so proxy the Compose service locally inside this
// verifier container rather than weakening the application's cookie policy.
const proxy = await startLocalhostProxy();
const browser = await chromium.launch({ headless: true });
try {
  if (PHASE === 'before-restart') await runBeforeRestart(browser);
  else if (PHASE === 'after-restart') await runAfterRestart(browser);
  else assert.fail(`unknown phase: ${PHASE}`);
  console.log(`Standard acceptance ${PHASE}: PASS`);
} finally {
  await browser.close();
  await new Promise((resolve, reject) => proxy.close((error) => error ? reject(error) : resolve()));
}
