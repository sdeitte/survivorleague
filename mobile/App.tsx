import { StatusBar } from 'expo-status-bar';
import { StyleSheet, Text, View, Pressable, ActivityIndicator } from 'react-native';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { API_BASE_URL, fetchHealth } from './api';

const queryClient = new QueryClient();

function HealthCheckScreen() {
  const { data, error, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['health'],
    queryFn: fetchHealth,
    retry: false,
  });

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Survivor League</Text>
      <Text style={styles.subtitle}>
        Phase 0 scaffold — this screen proves the mobile client can reach the
        Go API at {API_BASE_URL}.
      </Text>

      <View style={styles.card}>
        {isLoading && <ActivityIndicator />}
        {error ? (
          <Text style={styles.error}>
            Could not reach the API: {(error as Error).message}
          </Text>
        ) : null}
        {data ? (
          <View>
            <Text style={data.status === 'ok' ? styles.ok : styles.error}>
              status: {data.status}
            </Text>
            <Text style={data.db === 'ok' ? styles.ok : styles.error}>
              db: {data.db}
            </Text>
            {data.error ? <Text style={styles.error}>error: {data.error}</Text> : null}
          </View>
        ) : null}
      </View>

      <Pressable style={styles.button} onPress={() => refetch()} disabled={isFetching}>
        <Text style={styles.buttonText}>{isFetching ? 'Refreshing…' : 'Refresh'}</Text>
      </Pressable>

      <StatusBar style="auto" />
    </View>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <HealthCheckScreen />
    </QueryClientProvider>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0f172a',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
    gap: 16,
  },
  title: {
    color: '#f1f5f9',
    fontSize: 22,
    fontWeight: '600',
  },
  subtitle: {
    color: '#94a3b8',
    fontSize: 13,
    textAlign: 'center',
  },
  card: {
    backgroundColor: '#1e293b',
    borderRadius: 12,
    padding: 16,
    width: '100%',
    minHeight: 60,
    justifyContent: 'center',
  },
  ok: {
    color: '#34d399',
    fontSize: 14,
  },
  error: {
    color: '#f87171',
    fontSize: 14,
  },
  button: {
    backgroundColor: '#f1f5f9',
    borderRadius: 8,
    paddingVertical: 10,
    paddingHorizontal: 16,
  },
  buttonText: {
    color: '#0f172a',
    fontWeight: '600',
  },
});
