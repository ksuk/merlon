import { Component, type ReactNode } from "react"
import { withTranslation, type WithTranslation } from "react-i18next"

interface Props extends WithTranslation {
  children: ReactNode
}

interface State {
  error: Error | null
}

class ErrorBoundaryBase extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  render() {
    if (this.state.error) {
      const { t } = this.props
      return (
        <div className="flex min-h-[400px] items-center justify-center">
          <div className="text-center">
            <h2 className="mb-2 text-lg font-semibold text-destructive">{t("errorBoundary.title")}</h2>
            <p className="mb-4 text-sm text-muted-foreground">{this.state.error.message}</p>
            <button
              onClick={() => this.setState({ error: null })}
              className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90"
            >
              {t("errorBoundary.retry")}
            </button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}

export const ErrorBoundary = withTranslation()(ErrorBoundaryBase)
