import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'
import App from './App'

test('renders Merlon heading', () => {
  render(<App />)
  expect(screen.getByText('Merlon')).toBeDefined()
})
