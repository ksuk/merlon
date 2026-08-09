import { Component, type ReactNode } from "react"
import { withTranslation, type WithTranslation } from "react-i18next"

interface Props extends WithTranslation {
  children: ReactNode
  /**
   * A value that resets the boundary when it changes. The route path is the
   * natural one: a render failure on one screen must not follow the operator to
   * the next.
   */
  resetKey?: unknown
  /** Rendered instead of the default panel, for a feature-level boundary. */
  fallbackTitleKey?: string
}

interface State {
  error: Error | null
  resetKey: unknown
}

class ErrorBoundaryBase extends Component<Props, State> {
  state: State = { error: null, resetKey: undefined }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  static getDerivedStateFromProps(props: Props, state: State): Partial<State> | null {
    if (props.resetKey !== state.resetKey) {
      // Navigating away clears the failure. Previously the boundary sat outside
      // the route tree and kept its state forever, so one broken screen left
      // the whole application showing an error page until a full reload.
      return { error: null, resetKey: props.resetKey }
    }
    return null
  }

  componentDidCatch(error: Error, info: { componentStack?: string | null }) {
    // The boundary used to swallow the failure entirely: nothing was logged, so
    // a reproducible crash left no trace for anyone to investigate.
    console.error("UI render failure", error, info?.componentStack)
  }

  render() {
    if (this.state.error) {
      const { t, fallbackTitleKey } = this.props
      return (
        <div role="alert" className="flex min-h-[400px] items-center justify-center">
          <div className="max-w-lg text-center">
            <h2 className="mb-2 text-lg font-semibold text-destructive">
              {t(fallbackTitleKey ?? "errorBoundary.title")}
            </h2>
            {/* The thrown message is a developer artifact and may quote
                internal detail. The operator gets a stable explanation and a
                way forward instead. */}
            <p className="mb-4 text-sm text-muted-foreground">{t("errorBoundary.description")}</p>
            <div className="flex justify-center gap-2">
              <button
                onClick={() => this.setState({ error: null })}
                className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90"
              >
                {t("errorBoundary.retry")}
              </button>
              <a
                href="/"
                className="rounded-md border px-4 py-2 text-sm hover:bg-accent"
              >
                {t("errorBoundary.goHome")}
              </a>
            </div>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}

export const ErrorBoundary = withTranslation()(ErrorBoundaryBase)
