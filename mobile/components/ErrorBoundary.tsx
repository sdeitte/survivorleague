import { Component, type ErrorInfo, type ReactNode } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

// Phase 9 polish: mirrors web/src/components/ErrorBoundary.tsx — a
// top-level render-error safety net so an uncaught exception anywhere in
// the screen tree shows a recoverable "something went wrong" screen
// instead of crashing the app outright (or, in a release build, going
// fully unresponsive with no way back to a known-good screen). Only
// catches render/lifecycle errors, same React error-boundary contract
// caveat as the web version.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('Unhandled error in component tree:', error, info.componentStack);
  }

  private reset = () => {
    this.setState({ error: null });
  };

  render() {
    if (this.state.error) {
      return (
        <View style={styles.container}>
          <View style={styles.card}>
            <Text style={styles.title}>Something went wrong</Text>
            <Text style={styles.subtitle}>An unexpected error occurred.</Text>
            <Text style={styles.message}>{this.state.error.message}</Text>
            <Pressable style={styles.button} onPress={this.reset}>
              <Text style={styles.buttonText}>Try again</Text>
            </Pressable>
          </View>
        </View>
      );
    }

    return this.props.children;
  }
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0f172a',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  card: {
    width: '100%',
    maxWidth: 360,
    backgroundColor: '#1e293b',
    borderRadius: 12,
    padding: 20,
    gap: 8,
    alignItems: 'center',
  },
  title: {
    color: '#f1f5f9',
    fontSize: 18,
    fontWeight: '600',
  },
  subtitle: {
    color: '#94a3b8',
    fontSize: 13,
    textAlign: 'center',
  },
  message: {
    color: '#64748b',
    fontSize: 12,
    textAlign: 'center',
  },
  button: {
    marginTop: 8,
    backgroundColor: '#f1f5f9',
    borderRadius: 8,
    paddingVertical: 10,
    paddingHorizontal: 20,
  },
  buttonText: {
    color: '#0f172a',
    fontWeight: '600',
  },
});
