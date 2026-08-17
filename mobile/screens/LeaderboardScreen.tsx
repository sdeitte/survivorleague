import { useQuery } from '@tanstack/react-query';
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import * as api from '../api';
import { ApiError, type LeaderboardEntry } from '../api';

// The leaderboard screen — a sorted list with status badges. Sorting is
// entirely the server's job (active first, then eliminated ordered by how
// late they were eliminated); this screen just renders the response order.
export function LeaderboardScreen({ leagueId, onBack }: { leagueId: string; onBack: () => void }) {
  const { authFetch } = useAuth();

  const leaderboardQuery = useQuery({
    queryKey: ['league', leagueId, 'leaderboard'],
    queryFn: () => authFetch((token) => api.getLeaderboard(token, leagueId)),
    refetchInterval: 60_000,
  });

  return (
    <View style={styles.container}>
      <Pressable onPress={onBack}>
        <Text style={styles.backLink}>← League</Text>
      </Pressable>
      <Text style={styles.title}>Leaderboard</Text>

      {leaderboardQuery.isLoading && <ActivityIndicator color="#f1f5f9" />}
      {leaderboardQuery.error && (
        <Text style={styles.error}>
          {leaderboardQuery.error instanceof ApiError ? leaderboardQuery.error.message : 'Could not load the leaderboard.'}
        </Text>
      )}

      <FlatList<LeaderboardEntry>
        data={leaderboardQuery.data ?? []}
        keyExtractor={(item) => item.membership_id}
        renderItem={({ item, index }) => (
          <View style={styles.row}>
            <View style={styles.rowLeft}>
              <Text style={styles.rank}>{index + 1}</Text>
              <View>
                <Text style={styles.name}>{item.display_name}</Text>
                {!item.is_contestant && <Text style={styles.meta}>not playing</Text>}
              </View>
            </View>
            <View style={[styles.badge, item.status === 'active' && styles.badgeActive]}>
              <Text style={[styles.badgeText, item.status === 'active' && styles.badgeTextActive]}>
                {item.status === 'active' ? 'Alive' : 'Eliminated'}
              </Text>
            </View>
          </View>
        )}
        ListEmptyComponent={!leaderboardQuery.isLoading ? <Text style={styles.meta}>No members yet.</Text> : null}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0f172a',
    padding: 24,
    gap: 12,
  },
  backLink: {
    color: '#64748b',
    fontSize: 12,
    textDecorationLine: 'underline',
    marginBottom: 8,
  },
  title: {
    color: '#f1f5f9',
    fontSize: 20,
    fontWeight: '600',
    marginBottom: 8,
  },
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    backgroundColor: '#1e293b',
    borderRadius: 10,
    padding: 14,
    marginBottom: 8,
  },
  rowLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  rank: {
    color: '#64748b',
    fontSize: 12,
    width: 18,
    textAlign: 'right',
  },
  name: {
    color: '#f1f5f9',
    fontSize: 14,
  },
  meta: {
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
  badgeActive: {
    borderColor: '#047857',
  },
  badgeText: {
    color: '#94a3b8',
    fontSize: 11,
  },
  badgeTextActive: {
    color: '#34d399',
  },
  error: {
    color: '#f87171',
    fontSize: 13,
  },
});
