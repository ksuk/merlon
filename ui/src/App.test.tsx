import { render, screen } from '@testing-library/react'
import { expect, test, vi, beforeAll, beforeEach } from 'vitest'
import { initI18n } from '@/i18n'
import App from './App'

beforeAll(async () => {
  await initI18n()
})

beforeEach(() => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify({
      customers_by_risk_tier: {},
      total_customers: 0,
      alerts_by_status: {},
      alerts_by_severity: {},
      total_alerts: 0,
      cases_by_status: {},
      total_cases: 0,
      recent_transactions: 0,
    })),
  )
})

test('renders Merlon heading in sidebar', async () => {
  render(<App />)
  expect(await screen.findByText('Merlon')).toBeDefined()
})

test('renders dashboard title', async () => {
  render(<App />)
  const items = await screen.findAllByText('ダッシュボード')
  expect(items.length).toBeGreaterThanOrEqual(1)
})

test('renders sidebar navigation items', async () => {
  render(<App />)
  expect(await screen.findByText('顧客')).toBeDefined()
  const alerts = screen.getAllByText('アラート')
  expect(alerts.length).toBeGreaterThanOrEqual(1)
  expect(screen.getByText('ケース')).toBeDefined()
  expect(screen.getByText('取引')).toBeDefined()
  expect(screen.getByText('監査ログ')).toBeDefined()
  expect(screen.getByText('システム')).toBeDefined()
})
