import '@testing-library/jest-dom'
// toHaveNoViolations, so an accessibility failure names the rule it broke
// rather than reading as an opaque boolean.
import 'vitest-axe/extend-expect'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// Keep every jsdom render isolated when Vitest runs several files in one
// worker (including the vmThreads fallback used in constrained containers).
// Testing Library's auto-cleanup is not guaranteed when the runner's globals
// are injected rather than imported, so register it explicitly here.
afterEach(() => {
  cleanup()
})
