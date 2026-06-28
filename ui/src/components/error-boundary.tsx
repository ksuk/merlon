import { Component, type ReactNode } from "react"

interface Props {
  children: ReactNode
}

interface State {
  error: Error | null
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  render() {
    if (this.state.error) {
      return (
        <div className="flex min-h-[400px] items-center justify-center">
          <div className="text-center">
            <h2 className="mb-2 text-lg font-semibold text-destructive">エラーが発生しました</h2>
            <p className="mb-4 text-sm text-muted-foreground">{this.state.error.message}</p>
            <button
              onClick={() => this.setState({ error: null })}
              className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90"
            >
              再試行
            </button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}
