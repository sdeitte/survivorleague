import { useState } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';

// The main authenticated screen. `user` here came from GET /me (either at
// login/register or during the app-launch secure-storage bootstrap in
// AuthContext), and the Refresh button re-issues that same GET /me call —
// today's proof that stored tokens, the Authorization header, and the
// requireAuth middleware all round-trip correctly. Replaces Phase 0's
// /health-fetching placeholder screen.
export function HomeScreen({ onNavigateToHealth }: { onNavigateToHealth: () => void }) {
  const { user, logout, refreshProfile } = useAuth();
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onRefresh = async () => {
    setIsRefreshing(true);
    setError(null);
    try {
      await refreshProfile();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to refresh profile');
    } finally {
      setIsRefreshing(false);
    }
  };

  return (
    <View style={styles.container}>
      <View style={styles.header}>
        <Text style={styles.title}>Survivor League</Text>
        <Pressable onPress={() => void logout()}>
          <Text style={styles.link}>Log out</Text>
        </Pressable>
      </View>

      <Text style={styles.subtitle}>
        Signed in as {user?.display_name}. This card is a live GET /me call, proving the stored
        access/refresh tokens and requireAuth middleware round-trip correctly.
      </Text>

      <View style={styles.card}>
        {user ? (
          <View style={styles.grid}>
            <Text style={styles.label}>id</Text>
            <Text style={styles.value} numberOfLines={1}>
              {user.id}
            </Text>
            <Text style={styles.label}>email</Text>
            <Text style={styles.value}>{user.email}</Text>
            <Text style={styles.label}>display_name</Text>
            <Text style={styles.value}>{user.display_name}</Text>
            <Text style={styles.label}>is_site_admin</Text>
            <Text style={[styles.value, user.is_site_admin && styles.ok]}>
              {String(user.is_site_admin)}
            </Text>
          </View>
        ) : (
          <Text style={styles.value}>No profile loaded.</Text>
        )}
        {error && <Text style={styles.error}>{error}</Text>}
      </View>

      <Pressable style={styles.button} onPress={() => void onRefresh()} disabled={isRefreshing}>
        {isRefreshing ? <ActivityIndicator color="#0f172a" /> : <Text style={styles.buttonText}>Refresh</Text>}
      </Pressable>

      <Pressable onPress={onNavigateToHealth}>
        <Text style={styles.link}>API health check</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0f172a',
    justifyContent: 'center',
    padding: 24,
    gap: 16,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  title: {
    color: '#f1f5f9',
    fontSize: 22,
    fontWeight: '600',
  },
  subtitle: {
    color: '#94a3b8',
    fontSize: 13,
  },
  card: {
    backgroundColor: '#1e293b',
    borderRadius: 12,
    padding: 16,
    gap: 8,
  },
  grid: {
    gap: 4,
  },
  label: {
    color: '#94a3b8',
    fontSize: 12,
  },
  value: {
    color: '#f1f5f9',
    fontSize: 14,
    marginBottom: 4,
  },
  ok: {
    color: '#34d399',
  },
  error: {
    color: '#f87171',
    fontSize: 13,
  },
  button: {
    backgroundColor: '#f1f5f9',
    borderRadius: 8,
    paddingVertical: 12,
    alignItems: 'center',
  },
  buttonText: {
    color: '#0f172a',
    fontWeight: '600',
  },
  link: {
    color: '#94a3b8',
    fontSize: 13,
    textDecorationLine: 'underline',
  },
});
