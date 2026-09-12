// End-to-end contract for the two documented Docker Demo tours.
// Run through scripts/verify-demo-tour.sh so the browser version and network
// are controlled without adding Playwright to the repository dependencies.

import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const BASE_URL = process.env.BASE_URL || 'http://api:8080';
const STORY = {
  alert: '419d1314-654e-5375-bfb7-9fcea10fcd53',
  alertCompact: '419d1314654e5375bfb79fcea10fcd53',
  transaction: 'b3dbf56d-8e2a-5f1c-86d2-ddf35ce38bfd',
  transactionCompact: 'b3dbf56d8e2a5f1c86d2ddf35ce38bfd',
  customer: '61a626c6-ced4-536d-be74-41d6ca874e4d',
  customerCompact: '61a626c6ced4536dbe7441d6ca874e4d',
  case: '3a55610e-d00f-5a34-8bfa-cc9753cbfa06',
  caseCompact: '3a55610ed00f5a348bfacc9753cbfa06',
};

const MARKER = `demo-tour-${Date.now()}`;

async function open(page, path) {
  await page.goto(`${BASE_URL}${path}`, { waitUntil: 'networkidle' });
  await page.getByRole('img', { name: 'Merlon' }).waitFor({ timeout: 15_000 });
}

async function bodyText(page) {
  return page.locator('body').innerText();
}

async function waitForRun(request, path, expected = 'completed') {
  let last;
  await assert.doesNotReject(async () => {
    await expectPoll(async () => {
      const response = await request.get(`${BASE_URL}${path}`);
      assert.equal(response.ok(), true, `${path} returned ${response.status()}`);
      last = await response.json();
      if (last.status === 'failed' || last.status === 'partial') {
        throw new Error(`${path} ended ${last.status}: ${last.error || 'no error detail'}`);
      }
      return last.status === expected;
    }, 30_000);
  });
  return last;
}

async function expectPoll(check, timeout) {
  const deadline = Date.now() + timeout;
  let lastError;
  while (Date.now() < deadline) {
    try {
      if (await check()) return;
    } catch (error) {
      lastError = error;
      if (/ended (failed|partial)/.test(error.message)) throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  if (lastError) throw lastError;
  throw new Error(`condition was not met within ${timeout}ms`);
}

async function pathA(page, request) {
  await open(page, '/');
  const dashboard = await bodyText(page);
  assert.match(dashboard, /1,015|1015/, 'dashboard customer count drifted');
  assert.match(dashboard, /アラート/);
  assert.match(dashboard, /ケース/);

  await open(page, `/alerts/${STORY.alert}`);
  const alert = await bodyText(page);
  assert.match(alert, /tm_rapid_movement/);
  assert.ok(alert.includes(STORY.customerCompact));
  assert.ok(alert.includes(STORY.transactionCompact));

  await open(page, `/transactions/${STORY.transaction}`);
  const transaction = await bodyText(page);
  assert.match(transaction, /MNP-C000002/);
  assert.match(transaction, /tm_rapid_movement/);
  assert.match(transaction, /3,200,000|3200000/);

  await open(page, `/customers/${STORY.customer}`);
  assert.match(await bodyText(page), /スコア評価\s*1/);
  const ruleValue = await page.locator('#cdd-rule-set').inputValue();
  assert.equal(ruleValue, 'funds_transfer', 'CDD selector must submit the stable rule name');
  await page.locator('#score-rationale').fill(`${MARKER}-score`);
  await page.getByRole('button', { name: 'スコアリング', exact: true }).click();
  const scoreResponse = page.waitForResponse((response) =>
    response.url().includes('/api/v1/customers/') && response.url().endsWith('/score') &&
    response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'スコアリングを確定', exact: true }).click();
  assert.equal((await scoreResponse).ok(), true, 'CDD scoring failed');
  await expectPoll(async () => /スコア評価\s*2/.test(await bodyText(page)), 10_000);

  await open(page, `/cases/${STORY.case}`);
  const initialCase = await bodyText(page);
  assert.ok(initialCase.includes(STORY.alertCompact));
  assert.match(initialCase, /STR対象/);
  const note = page.getByPlaceholder('ノートを追加...');
  await note.fill(`${MARKER}-note`);
  const noteResponse = page.waitForResponse((response) =>
    response.url().includes('/api/v1/cases/') && response.url().endsWith('/notes') &&
    response.request().method() === 'POST',
  );
  await note.locator('..').getByRole('button').click();
  assert.equal((await noteResponse).ok(), true, 'case note failed');
  await page.getByText(`${MARKER}-note`, { exact: true }).waitFor();
  const transitionResponse = page.waitForResponse((response) =>
    response.url().includes('/api/v1/cases/') &&
    response.request().method() !== 'GET',
  );
  await page.getByRole('button', { name: 'エスカレーション', exact: true }).click();
  assert.equal((await transitionResponse).ok(), true, 'case transition failed');
  await page.getByText(/エスカレーション.*STR対象/).first().waitFor();

  await open(page, '/reports');
  await page.getByRole('button', { name: new RegExp(`^${STORY.alert.slice(0, 8)}`) }).click();
  await page.locator('textarea').first().fill(`${MARKER}-narrative`);
  await page.locator('input').last().fill('demo-tour-verifier');
  const reportResponse = page.waitForResponse((response) =>
    response.url().endsWith('/api/v1/reports/str') && response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'STRレポート作成', exact: true }).click();
  const createdResponse = await reportResponse;
  assert.equal(createdResponse.ok(), true, 'STR draft creation failed');
  const created = await createdResponse.json();
  assert.ok(created.id, 'STR draft response has no ID');
  await page.getByText(`${MARKER}-narrative`, { exact: true }).waitFor();

  const exportResponse = page.waitForResponse((response) =>
    response.url().includes('/api/v1/reports/str/export?') &&
    response.url().includes(`report_id=${created.id}`) && response.ok(),
  );
  await page.getByRole('button', { name: 'JSON', exact: true }).first().click();
  await exportResponse;

  await page.reload({ waitUntil: 'networkidle' });
  assert.ok((await bodyText(page)).includes(`${MARKER}-narrative`), 'STR draft did not persist');
  await open(page, `/cases/${STORY.case}`);
  const reloadedCase = await bodyText(page);
  assert.ok(reloadedCase.includes(`${MARKER}-note`), 'case note did not persist');
  assert.match(reloadedCase, /エスカレーション/, 'case status did not persist in the UI');
  assert.match(reloadedCase, /STR対象/, 'STR candidate did not persist in the UI');
  const persistedCaseResponse = await request.get(`${BASE_URL}/api/v1/cases/${STORY.case}`);
  assert.equal(persistedCaseResponse.ok(), true, 'persisted case API lookup failed');
  const persistedCase = await persistedCaseResponse.json();
  assert.equal(persistedCase.status, 'escalated', 'case status did not persist');
  assert.equal(persistedCase.str_candidate, true, 'STR candidate did not persist');

  await open(page, '/audit');
  const audit = await bodyText(page);
  for (const expected of ['スコアリング', 'add_note', 'ステータス変更', 'STR作成', 'export_str']) {
    assert.ok(audit.includes(expected), `audit is missing ${expected}`);
  }
  assert.ok(audit.includes(STORY.customerCompact));
  assert.ok(audit.includes(STORY.caseCompact));
  assert.ok(audit.includes(created.id));
  return created.id;
}

async function selectFirstThree(page) {
  const buttons = page.locator('button[aria-pressed]');
  await buttons.first().waitFor({ timeout: 15_000 });
  assert.ok((await buttons.count()) >= 3, 'customer selector has fewer than three entries');
  for (let index = 0; index < 3; index += 1) await buttons.nth(index).click();
}

async function pathB(page, request) {
  await open(page, '/rules/rapid_movement');
  assert.match(await bodyText(page), /rapid_movement/);
  const ruleExportPromise = page.waitForResponse((response) =>
    response.url().includes('/api/v1/rules/rapid_movement/export?format=json') && response.ok(),
  );
  await page.getByRole('link', { name: 'JSON', exact: true }).click();
  const ruleExport = await ruleExportPromise;
  assert.match(ruleExport.headers()['content-type'] || '', /application\/json/);
  assert.match(await ruleExport.text(), /rapid_movement/);

  await open(page, `/customers/${STORY.customer}`);
  assert.match(await bodyText(page), /スコア評価\s*2/);
  await page.locator('#score-rationale').fill(`${MARKER}-path-b-score`);
  await page.getByRole('button', { name: 'スコアリング', exact: true }).click();
  const scoreResponse = page.waitForResponse((response) =>
    response.url().includes('/api/v1/customers/') && response.url().endsWith('/score') &&
    response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'スコアリングを確定', exact: true }).click();
  assert.equal((await scoreResponse).ok(), true, 'Path B CDD scoring failed');
  await expectPoll(async () => /スコア評価\s*3/.test(await bodyText(page)), 10_000);

  await open(page, '/backtest');
  await page.locator('#backtest-rationale').fill(`${MARKER}-backtest`);
  await page.locator('#backtest-candidate').fill('rapid_movement');
  await selectFirstThree(page);
  await page.getByRole('button', { name: '実行前にプレビュー', exact: true }).click();
  await page.getByText('顧客: 3件', { exact: true }).waitFor();
  await page.getByRole('button', { name: 'バックテスト実行', exact: true }).click();
  const backtestText = page.getByText(/ジョブ .*（/).first();
  await backtestText.waitFor();
  const backtestID = (await backtestText.innerText()).match(/ジョブ ([0-9a-f-]+)/)?.[1];
  assert.ok(backtestID, 'backtest job ID was not rendered');
  const backtest = await waitForRun(request, `/api/v1/backtests/${backtestID}`);
  assert.equal(backtest.progress, 1);

  await open(page, '/batch');
  await page.locator('#batch-rationale').fill(`${MARKER}-batch`);
  await selectFirstThree(page);
  await page.getByRole('button', { name: '一括スコアリング', exact: true }).click();
  await page.getByText('3件の顧客に一括スコアリングを実行します。', { exact: true }).waitFor();
  await page.getByRole('button', { name: '確認して永続実行を開始', exact: true }).click();
  const batchText = page.getByText(/実行 [0-9a-f]+:/).first();
  await batchText.waitFor();
  const batchID = (await batchText.innerText()).match(/実行 ([0-9a-f]+)/)?.[1];
  assert.ok(batchID, 'batch run ID was not rendered');
  const batch = await waitForRun(request, `/api/v1/batch/runs/${batchID}`);
  assert.equal(batch.result_counts.succeeded, 3);
  assert.equal(batch.result_counts.failed, 0);
  assert.equal(batch.processed_customer_ids.length, 3);

  await open(page, '/audit');
  await page.getByText(backtest.id, { exact: true }).first().waitFor();
  await page.getByText(batch.id, { exact: true }).first().waitFor();
  const audit = await bodyText(page);
  assert.ok(audit.includes(backtest.id));
  assert.ok(audit.includes(batch.id));

  await open(page, '/system');
  const system = await bodyText(page);
  for (const component of ['Go API', 'PostgreSQL', 'Go Engine']) {
    assert.ok(system.includes(component), `system page is missing ${component}`);
  }
  const openapiResponse = await request.get(`${BASE_URL}/api/v1/openapi.json`);
  assert.equal(openapiResponse.ok(), true);
  const contract = await openapiResponse.json();
  assert.equal(contract.openapi, '3.0.3');
  assert.ok(contract.paths['/api/v1/batch/runs']);
  assert.ok(contract.paths['/api/v1/customers/{id}/cdd-rule-sets']);
}

async function main() {
  const browser = await chromium.launch();
  const context = await browser.newContext({ locale: 'ja-JP' });
  const page = await context.newPage();
  try {
    const reportID = await pathA(page, context.request);
    await pathB(page, context.request);
    console.log(`verified documented demo tours; persisted STR draft ${reportID}`);
  } finally {
    await context.close();
    await browser.close();
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
