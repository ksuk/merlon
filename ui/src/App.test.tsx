import { render, screen } from '@testing-library/react'
import { expect, test, vi, beforeEach } from 'vitest'
import App from './App'

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

test('renders Merlon heading in sidebar', () => {
  render(<App />)
  expect(screen.getByText('Merlon')).toBeDefined()
})

test('renders dashboard title', async () => {
  render(<App />)
  expect(await screen.findByText('ダッシュボード')).toBeDefined()
})

test('renders sidebar navigation items', () => {
  render(<App />)
  expect(screen.getByText('顧客')).toBeDefined()
  expect(screen.getByText('アラート')).toBeDefined()
  expect(screen.getByText('ケース')).toBeDefined()
  expect(screen.getByText('取引')).toBeDefined()
  expect(screen.getByText('監査ログ')).toBeDefined()
  expect(screen.getByText('システム')).toBeDefined()
})
