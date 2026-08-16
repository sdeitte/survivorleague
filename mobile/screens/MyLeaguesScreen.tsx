import { useQuery } from '@tanstack/react-query';
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import * as api from '../api';
import type { League } from '../api';

// "My Leagues" — the main authenticated screen (Phase 2 replaces Phase 1's
// GET-/me-proving HomeScreen with the real landing view): leagues the
// signed-in user belongs to, plus entry points to create one or join one
// by code.
export function MyLeaguesScreen({
  onNavigateToCreate,
  onNavigateToJoin,
  onNavigateToLeague,
  onNavigateToHealth,
}: {
  onNavigateToCreate: () => void;
  onNavigateToJoin: () => void;
  onNavigateToLeague: (leagueId: string) => void;
  onNavigateToHealth: () => void;
}) {
  const { user, logout, authFetch } = useAuth();

  const { data: leagues, error, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['leagues'],
    queryFn: () => authFetch((token) => api.listLeagues(token)),
  });

  return (
    <View style={styles.container}>
      <View style={styles.header}>
        <View>
          <Text style={styles.title}>Survivor League</Text>
          <Text style={styles.subtitle}>Signed in as {user?.display_name}</Text>
        </View>
        <Pressable onPress={() => void logout()}>
          <Text style={styles.link}>Log out</Text>
        </Pressable>
      </View>

      <View style={styles.actionsRow}>
        <Pressable style={[styles.button, styles.actionButton]} onPress={onNavigateToCreate}>
          <Text style={styles.buttonText}>Create a league</Text>
        </Pressable>
        <Pressable style={[styles.buttonOutline, styles.actionButton]} onPress={onNavigateToJoin}>
          <Text style={styles.buttonOutlineText}>Join by code</Text>
        </Pressable>
      </View>

      {isLoading && <ActivityIndicator color="#f1f5f9" />}
      {error && <Text style={styles.error}>Could not load leagues: {(error as Error).message}</Text>}

      <FlatList<League>
        data={leagues ?? []}
        keyExtractor={(item) => item.id}
        refreshing={isFetching}
        onRefresh={() => void refetch()}
        ListEmptyComponent={
          !isLoading ? (
            <Text style={styles.empty}>You're not in any leagues yet. Create one, or join with an invite code.</Text>
          ) : null
        }
        renderItem={({ item }) => (
          <Pressable style={styles.leagueRow} onPress={() => onNavigateToLeague(item.id)}>
            <View>
              <Text style={styles.leagueName}>{item.name}</Text>
              <Text style={styles.leagueMeta}>
                {item.conference} · {item.season_year}
              </Text>
            </View>
            <View style={[styles.badge, item.membership.role === 'commissioner' && styles.badgeCommissioner]}>
              <Text
                style={[
                  styles.badgeText,
                  item.membership.role === 'commissioner' && styles.badgeTextCommissioner,
                ]}
              >
                {item.membership.role}
              </Text>
            </View>
          </Pressable>
        )}
      />

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
    padding: 24,
    gap: 16,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
  },
  title: {
    color: '#f1f5f9',
    fontSize: 22,
    fontWeight: '600',
  },
  subtitle: {
    color: '#94a3b8',
    fontSize: 13,
    marginTop: 2,
  },
  actionsRow: {
    flexDirection: 'row',
    gap: 8,
  },
  actionButton: {
    flex: 1,
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
  buttonOutline: {
    borderRadius: 8,
    paddingVertical: 12,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: '#334155',
  },
  buttonOutlineText: {
    color: '#f1f5f9',
    fontWeight: '600',
  },
  error: {
    color: '#f87171',
    fontSize: 13,
  },
  empty: {
    color: '#94a3b8',
    fontSize: 13,
    padding: 16,
    textAlign: 'center',
  },
  leagueRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    backgroundColor: '#1e293b',
    borderRadius: 10,
    padding: 14,
    marginBottom: 8,
  },
  leagueName: {
    color: '#f1f5f9',
    fontSize: 15,
    fontWeight: '500',
  },
  leagueMeta: {
    color: '#94a3b8',
    fontSize: 12,
    marginTop: 2,
  },
  badge: {
    borderRadius: 999,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderWidth: 1,
    borderColor: '#334155',
  },
  badgeCommissioner: {
    borderColor: '#047857',
  },
  badgeText: {
    color: '#cbd5e1',
    fontSize: 11,
  },
  badgeTextCommissioner: {
    color: '#34d399',
  },
  link: {
    color: '#94a3b8',
    fontSize: 13,
    textAlign: 'center',
    textDecorationLine: 'underline',
  },
});
