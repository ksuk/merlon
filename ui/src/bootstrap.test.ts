import { expect, test, vi } from "vitest"
import { bootstrapApplication } from "./bootstrap"

test("waits for initialization before rendering", async () => {
  let resolveInitialization!: () => void
  const initialization = new Promise<void>((resolve) => {
    resolveInitialization = resolve
  })
  const render = vi.fn()
  const reportError = vi.fn()

  const boot = bootstrapApplication({
    initialize: () => initialization,
    render,
    reportError,
  })

  expect(render).not.toHaveBeenCalled()

  resolveInitialization()
  await boot

  expect(render).toHaveBeenCalledOnce()
  expect(reportError).not.toHaveBeenCalled()
})

test("reports initialization failures without rendering untranslated content", async () => {
  const error = new Error("catalog unavailable")
  const render = vi.fn()
  const reportError = vi.fn()

  await bootstrapApplication({
    initialize: () => Promise.reject(error),
    render,
    reportError,
  })

  expect(reportError).toHaveBeenCalledWith(error)
  expect(render).not.toHaveBeenCalled()
})
