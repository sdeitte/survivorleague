import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ActivityIndicator, Alert, Image, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useAuth } from '../auth/AuthContext';
import { BrandWordmark } from '../components/BrandWordmark';
import * as api from '../api';
import { ApiError, type LeaderboardEntry, type Member, type MembershipWeekPick } from '../api';

// The leaderboard screen — a sorted list with status badges, each row
// expandable into that member's full-season pick history. Sorting is
// entirely the server's job (active first, then eliminated ordered by how
// late they were eliminated); this screen just renders the response order.
//
// Also the commissioner's member-management surface on mobile (buy
// back / remove) — LeagueDetailScreen used to own a separate members
// list for this, but that duplicated the leaderboard's own roster. The
// membersQuery here exists solely to get each entry's `role` (to hide
// "Remove" on the commissioner's own row) and to drive the mutations;
// leaderboardQuery remains the source of truth for ranking/display.
export function LeaderboardScreen({ leagueId, onBack }: { leagueId: string; onBack: () => void }) {
  const { authFetch } = useAuth();
  const queryClient = useQueryClient();
  const [expandedMembershipId, setExpandedMembershipId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const leagueQuery = useQuery({
    queryKey: ['league', leagueId],
    queryFn: () => authFetch((token) => api.getLeague(token, leagueId)),
  });
  const isCommissioner = leagueQuery.data?.membership.role === 'commissioner';
  const isClosed = leagueQuery.data?.status === 'closed';

  const leaderboardQuery = useQuery({
    queryKey: ['league', leagueId, 'leaderboard'],
    queryFn: () => authFetch((token) => api.getLeaderboard(token, leagueId)),
    refetchInterval: 60_000,
  });

  const membersQuery = useQuery({
    queryKey: ['league', leagueId, 'members'],
    queryFn: () => authFetch((token) => api.listMembers(token, leagueId)),
    enabled: isCommissioner,
  });
  const membersByMembershipId = new Map((membersQuery.data ?? []).map((m) => [m.membership_id, m]));

  const removeMemberMutation = useMutation({
    mutationFn: (membershipId: string) => authFetch((token) => api.removeMember(token, leagueId, membershipId)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'members'] });
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'leaderboard'] });
    },
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to remove member.'),
  });

  const buyBackMutation = useMutation({
    mutationFn: (membershipId: string) => authFetch((token) => api.buyBackMember(token, leagueId, membershipId)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'members'] });
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'leaderboard'] });
    },
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to buy back member.'),
  });

  const confirmRemove = (member: Member) => {
    Alert.alert(
      'Remove member?',
      `${member.display_name} will lose access to this league. They can rejoin later with the invite code.`,
      [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Remove', style: 'destructive', onPress: () => removeMemberMutation.mutate(member.membership_id) },
      ],
    );
  };

  const confirmBuyBack = (member: Member) => {
    Alert.alert(
      'Buy back this member?',
      `${member.display_name} will be reinstated as an active contestant. This is a one-time lifeline per member — it cannot be undone or used again for them, even if they're eliminated again later. Their previously-used teams stay locked.`,
      [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Buy back', onPress: () => buyBackMutation.mutate(member.membership_id) },
      ],
    );
  };

  return (
    <SafeAreaView style={styles.container} edges={['top']}>
    <ScrollView contentContainerStyle={styles.content}>
      <View style={styles.brandRow}>
        <BrandWordmark size={90} />
      </View>

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
      {actionError && <Text style={styles.error}>{actionError}</Text>}

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
          member={membersByMembershipId.get(entry.membership_id)}
          isCommissioner={!!isCommissioner}
          isClosed={!!isClosed}
          onRemove={confirmRemove}
          onBuyBack={confirmBuyBack}
        />
      ))}
    </ScrollView>
    </SafeAreaView>
  );
}

function LeaderboardRow({
  leagueId,
  entry,
  rank,
  expanded,
  onToggle,
  member,
  isCommissioner,
  isClosed,
  onRemove,
  onBuyBack,
}: {
  leagueId: string;
  entry: LeaderboardEntry;
  rank: number;
  expanded: boolean;
  onToggle: () => void;
  member?: Member;
  isCommissioner: boolean;
  isClosed: boolean;
  onRemove: (member: Member) => void;
  onBuyBack: (member: Member) => void;
}) {
  const { authFetch } = useAuth();

  const picksQuery = useQuery({
    queryKey: ['league', leagueId, 'members', entry.membership_id, 'picks'],
    queryFn: () => authFetch((token) => api.listMembershipPicks(token, leagueId, entry.membership_id)),
    enabled: expanded,
  });

  const canBuyBack = isCommissioner && !isClosed && entry.status === 'eliminated' && !entry.bought_back;
  const canRemove = isCommissioner && !isClosed && member && member.role !== 'commissioner';

  return (
    <View style={styles.row}>
      <Pressable style={styles.rowHeader} onPress={onToggle}>
        <View style={styles.rowLeft}>
          <Text style={styles.rank}>{rank}</Text>
          <View>
            <Text style={styles.name}>{entry.team_name || entry.display_name}</Text>
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

      {(canBuyBack || canRemove) && (
        <View style={styles.commissionerActions}>
          {canBuyBack && (
            <Pressable onPress={() => onBuyBack(member!)}>
              <Text style={styles.buyBackLink}>Buy back</Text>
            </Pressable>
          )}
          {canRemove && (
            <Pressable onPress={() => onRemove(member!)}>
              <Text style={styles.removeLink}>Remove</Text>
            </Pressable>
          )}
        </View>
      )}

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
          <View style={styles.pickedTeamRow}>
            {pick.team_logo_url && (
              <View style={styles.pickedTeamLogoBackdrop}>
                <Image source={{ uri: pick.team_logo_url }} style={styles.pickedTeamLogo} />
              </View>
            )}
            <Text style={styles.pickedTeam}>
              {pick.team_name} <Text style={styles.opponentText}>{pick.is_home ? 'vs' : '@'} {pick.opponent_name}</Text>
            </Text>
          </View>
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
  brandRow: {
    alignItems: 'center',
    marginBottom: 12,
  },
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
  commissionerActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingHorizontal: 14,
    paddingBottom: 12,
  },
  buyBackLink: {
    color: '#34d399',
    fontSize: 12,
    textDecorationLine: 'underline',
  },
  removeLink: {
    color: '#f87171',
    fontSize: 12,
    textDecorationLine: 'underline',
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
  pickedTeamRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  pickedTeamLogoBackdrop: {
    width: 22,
    height: 22,
    borderRadius: 11,
    backgroundColor: '#ffffff',
    alignItems: 'center',
    justifyContent: 'center',
  },
  pickedTeamLogo: {
    width: 16,
    height: 16,
    resizeMode: 'contain',
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
