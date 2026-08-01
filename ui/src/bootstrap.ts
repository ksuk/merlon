export interface BootstrapDependencies {
  initialize: () => Promise<unknown>
  render: () => void
  reportError: (error: unknown) => void
}

export async function bootstrapApplication({
  initialize,
  render,
  reportError,
}: BootstrapDependencies): Promise<void> {
  try {
    await initialize()
  } catch (error) {
    reportError(error)
    return
  }

  render()
}
