import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  children: ReactNode
}

interface State {
  error: Error | null
}

// Phase 9 polish: a top-level render-error safety net. Without this, an
// uncaught exception anywhere in the component tree (a bad API response
// shape, a null-deref in a page that assumed data was present, etc.)
// unmounts the whole React tree and leaves the user staring at a blank
// white page with no way back — the exact "uncaught exception blanks the
// page" failure mode the polish pass is meant to close off. This only
// catches render/lifecycle errors (React's error boundary contract does
// not cover async code, event handlers, etc. — those are handled per-call
// by ApiError catches elsewhere), but it's the last line of defense for
// everything else.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('Unhandled error in component tree:', error, info.componentStack)
  }

  private reset = () => {
    this.setState({ error: null })
    window.location.href = '/'
  }

  render() {
    if (this.state.error) {
      return (
        <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
          <div className="max-w-sm w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg text-center">
            <h1 className="text-xl font-semibold">Something went wrong</h1>
            <p className="text-sm text-slate-400">
              An unexpected error occurred. You can try going back to the home page.
            </p>
            <p className="text-xs text-slate-500 break-words">{this.state.error.message}</p>
            <button
              type="button"
              onClick={this.reset}
              className="w-full rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900"
            >
              Back to home
            </button>
          </div>
        </main>
      )
    }

    return this.props.children
  }
}
