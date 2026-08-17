import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import * as api from '../api';
import { ApiError, type LeaderboardEntry, type MembershipWeekPick } from '../api';

// The leaderboard screen — a sorted list with status badges, each row
// expandable into that member's full-season pick history. Sorting is
// entirely the server's job (active first, then eliminated ordered by how
// late they were eliminated); this screen just renders the response order.
export function LeaderboardScreen({ leagueId, onBack }: { leagueId: string; onBack: () => void }) {
  const { authFetch } = useAuth();
  const [expandedMembershipId, setExpandedMembershipId] = useState<string | null>(null);

  const leaderboardQuery = useQuery({
    queryKey: ['league', leagueId, 'leaderboard'],
    queryFn: () => authFetch((token) => api.getLeaderboard(token, leagueId)),
    refetchInterval: 60_000,
  });

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content}>
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
      {!leaderboardQuery.isLoading && leaderboardQuery.data?.length === 0 && (
        <Text style={styles.meta}>No members yet.</Text>
      )}

      {leaderboardQuery.data?.map((entry, index) => (
        <LeaderboardRow
          key={entry.membership_id}
          leagueId={leagueId}
          entry={entry}
          rank={index + 1}
          expanded={expandedMembershipId === entry.membership_id}
          onToggle={() =>
            setExpandedMembershipId((cur) => (cur === entry.membership_id ? null : entry.membership_id))
          }
        />
      ))}
    </ScrollView>
  );
}

function LeaderboardRow({
  leagueId,
  entry,
  rank,
  expanded,
  onToggle,
}: {
  leagueId: string;
  entry: LeaderboardEntry;
  rank: number;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { authFetch } = useAuth();

  const picksQuery = useQuery({
    queryKey: ['league', leagueId, 'members', entry.membership_id, 'picks'],
    queryFn: () => authFetch((token) => api.listMembershipPicks(token, leagueId, entry.membership_id)),
    enabled: expanded,
  });

  return (
    <View style={styles.row}>
      <Pressable style={styles.rowHeader} onPress={onToggle}>
        <View style={styles.rowLeft}>
          <Text style={styles.rank}>{rank}</Text>
          <View>
            <Text style={styles.name}>{entry.display_name}</Text>
            {!entry.is_contestant && <Text style={styles.meta}>not playing</Text>}
          </View>
        </View>
        <View style={styles.rowRight}>
          {entry.bought_back && (
            <View style={styles.boughtBackBadge}>
              <Text style={styles.boughtBackText}>bought back</Text>
            </View>
          )}
          <View style={[styles.badge, entry.status === 'active' && styles.badgeActive]}>
            <Text style={[styles.badgeText, entry.status === 'active' && styles.badgeTextActive]}>
              {entry.status === 'active' ? 'Alive' : 'Eliminated'}
            </Text>
          </View>
          <Text style={styles.chevron}>{expanded ? '▲' : '▼'}</Text>
        </View>
      </Pressable>

      {expanded && (
        <View style={styles.picksContainer}>
          {picksQuery.isLoading && <ActivityIndicator color="#f1f5f9" />}
          {picksQuery.error && (
            <Text style={styles.error}>
              {picksQuery.error instanceof ApiError ? picksQuery.error.message : "Could not load this member's picks."}
            </Text>
          )}
          {picksQuery.data?.length === 0 && <Text style={styles.meta}>No schedule data yet.</Text>}
          {picksQuery.data?.map((wp) => (
            <WeekPickRow key={wp.week_number} pick={wp} />
          ))}
        </View>
      )}
    </View>
  );
}

function WeekPickRow({ pick }: { pick: MembershipWeekPick }) {
  const revealed = pick.has_picked && !!pick.team_name;

  return (
    <View style={styles.weekRow}>
      <Text style={styles.weekLabel}>Wk {pick.week_number}</Text>
      <View style={styles.weekPickText}>
        {!pick.has_picked ? (
          <Text style={styles.notPicked}>Not picked</Text>
        ) : revealed ? (
          <Text style={styles.pickedTeam}>
            {pick.team_name} <Text style={styles.opponentText}>{pick.is_home ? 'vs' : '@'} {pick.opponent_name}</Text>
          </Text>
        ) : (
          <Text style={styles.pickedHidden}>Picked (hidden until kickoff)</Text>
        )}
      </View>
      <ResultBadge pick={pick} revealed={revealed} />
    </View>
  );
}

function ResultBadge({ pick, revealed }: { pick: MembershipWeekPick; revealed: boolean }) {
  if (!pick.has_picked) return null;
  if (!revealed || !pick.result || pick.result === 'pending') {
    return <Text style={styles.resultPending}>{pick.is_locked ? 'In progress' : 'Pending'}</Text>;
  }
  if (pick.result === 'win') return <Text style={styles.resultWin}>Won</Text>;
  if (pick.result === 'loss') return <Text style={styles.resultLoss}>Lost</Text>;
  return <Text style={styles.resultPending}>Void</Text>;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0f172a',
  },
  content: {
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
    backgroundColor: '#1e293b',
    borderRadius: 10,
    overflow: 'hidden',
  },
  rowHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: 14,
  },
  rowLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  rowRight: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
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
  chevron: {
    color: '#64748b',
    fontSize: 11,
  },
  boughtBackBadge: {
    borderRadius: 999,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderWidth: 1,
    borderColor: '#92400e',
  },
  boughtBackText: {
    color: '#f59e0b',
    fontSize: 11,
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
  picksContainer: {
    borderTopWidth: 1,
    borderTopColor: '#0f172a',
    padding: 8,
    gap: 1,
  },
  weekRow: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: '#0f172a99',
    paddingHorizontal: 10,
    paddingVertical: 8,
    borderRadius: 6,
    marginBottom: 2,
  },
  weekLabel: {
    color: '#64748b',
    fontSize: 11,
    width: 48,
  },
  weekPickText: {
    flex: 1,
  },
  pickedTeam: {
    color: '#e2e8f0',
    fontSize: 12,
  },
  opponentText: {
    color: '#64748b',
  },
  pickedHidden: {
    color: '#94a3b8',
    fontSize: 12,
  },
  notPicked: {
    color: '#475569',
    fontSize: 12,
  },
  resultWin: {
    color: '#34d399',
    fontSize: 11,
    marginLeft: 8,
  },
  resultLoss: {
    color: '#f87171',
    fontSize: 11,
    marginLeft: 8,
  },
  resultPending: {
    color: '#64748b',
    fontSize: 11,
    marginLeft: 8,
  },
  error: {
    color: '#f87171',
    fontSize: 13,
  },
});
