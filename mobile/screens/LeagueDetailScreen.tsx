import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ActivityIndicator, Alert, FlatList, Pressable, Share, StyleSheet, Switch, Text, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import * as api from '../api';
import { ApiError, type Member } from '../api';

export function LeagueDetailScreen({ leagueId, onBack }: { leagueId: string; onBack: () => void }) {
  const { authFetch } = useAuth();
  const queryClient = useQueryClient();
  const [actionError, setActionError] = useState<string | null>(null);

  const leagueQuery = useQuery({
    queryKey: ['league', leagueId],
    queryFn: () => authFetch((token) => api.getLeague(token, leagueId)),
  });
  const membersQuery = useQuery({
    queryKey: ['league', leagueId, 'members'],
    queryFn: () => authFetch((token) => api.listMembers(token, leagueId)),
  });

  const isCommissioner = leagueQuery.data?.membership.role === 'commissioner';

  const inviteQuery = useQuery({
    queryKey: ['league', leagueId, 'invite'],
    queryFn: () => authFetch((token) => api.getInviteCode(token, leagueId)),
    enabled: isCommissioner,
  });

  const regenerateMutation = useMutation({
    mutationFn: () => authFetch((token) => api.regenerateInviteCode(token, leagueId)),
    onSuccess: (data) => queryClient.setQueryData(['league', leagueId, 'invite'], data),
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to regenerate invite code.'),
  });

  const toggleContestantMutation = useMutation({
    mutationFn: (isContestant: boolean) =>
      authFetch((token) => api.updateLeague(token, leagueId, { commissioner_is_contestant: isContestant })),
    onSuccess: (league) => queryClient.setQueryData(['league', leagueId], league),
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to update league.'),
  });

  const removeMemberMutation = useMutation({
    mutationFn: (membershipId: string) => authFetch((token) => api.removeMember(token, leagueId, membershipId)),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'members'] }),
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to remove member.'),
  });

  const confirmRemove = (member: Member) => {
    Alert.alert(
      'Remove member?',
      `${member.display_name} will lose access to this league. They can rejoin later with the invite code.`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Remove',
          style: 'destructive',
          onPress: () => removeMemberMutation.mutate(member.membership_id),
        },
      ],
    );
  };

  const shareInviteCode = async () => {
    if (!inviteQuery.data || !leagueQuery.data) return;
    try {
      await Share.share({
        message: `Join my Survivor League league "${leagueQuery.data.name}" with invite code: ${inviteQuery.data.invite_code}`,
      });
    } catch {
      // User dismissed the share sheet, or it's unavailable — non-fatal,
      // the code is still visible on screen.
    }
  };

  if (leagueQuery.isLoading) {
    return (
      <View style={styles.loadingContainer}>
        <ActivityIndicator color="#f1f5f9" />
      </View>
    );
  }

  if (leagueQuery.error || !leagueQuery.data) {
    return (
      <View style={styles.loadingContainer}>
        <Text style={styles.error}>
          {leagueQuery.error instanceof ApiError ? leagueQuery.error.message : 'Could not load this league.'}
        </Text>
        <Pressable onPress={onBack}>
          <Text style={styles.link}>Back to My Leagues</Text>
        </Pressable>
      </View>
    );
  }

  const league = leagueQuery.data;

  return (
    <View style={styles.container}>
      <Pressable onPress={onBack}>
        <Text style={styles.backLink}>← My Leagues</Text>
      </Pressable>

      <FlatList<Member>
        data={membersQuery.data ?? []}
        keyExtractor={(item) => item.membership_id}
        ListHeaderComponent={
          <>
            <View style={styles.card}>
              <View style={styles.rowBetween}>
                <Text style={styles.leagueName}>{league.name}</Text>
                <View style={[styles.badge, league.membership.role === 'commissioner' && styles.badgeCommissioner]}>
                  <Text
                    style={[
                      styles.badgeText,
                      league.membership.role === 'commissioner' && styles.badgeTextCommissioner,
                    ]}
                  >
                    {league.membership.role}
                  </Text>
                </View>
              </View>
              <Text style={styles.leagueMeta}>
                {league.conference} · {league.season_year}
              </Text>

              {isCommissioner && (
                <View style={styles.switchRow}>
                  <Text style={styles.switchLabel}>Playing as a contestant</Text>
                  <Switch
                    value={league.membership.is_contestant}
                    disabled={toggleContestantMutation.isPending}
                    onValueChange={(v) => toggleContestantMutation.mutate(v)}
                  />
                </View>
              )}
            </View>

            {isCommissioner && (
              <View style={styles.card}>
                <Text style={styles.sectionTitle}>Invite code</Text>
                {inviteQuery.isLoading && <ActivityIndicator color="#f1f5f9" />}
                {inviteQuery.data && (
                  <View style={styles.inviteRow}>
                    <Text style={styles.inviteCode}>{inviteQuery.data.invite_code}</Text>
                    <Pressable style={styles.buttonOutline} onPress={() => void shareInviteCode()}>
                      <Text style={styles.buttonOutlineText}>Share</Text>
                    </Pressable>
                  </View>
                )}
                <Pressable onPress={() => regenerateMutation.mutate()} disabled={regenerateMutation.isPending}>
                  <Text style={styles.link}>
                    {regenerateMutation.isPending ? 'Regenerating…' : 'Regenerate code (invalidates the old one)'}
                  </Text>
                </Pressable>
              </View>
            )}

            {actionError && <Text style={styles.error}>{actionError}</Text>}

            <Text style={styles.sectionTitle}>Members</Text>
          </>
        }
        renderItem={({ item }) => (
          <View style={styles.memberRow}>
            <View>
              <Text style={styles.memberName}>{item.display_name}</Text>
              <Text style={styles.memberMeta}>
                {item.role}
                {!item.is_contestant && ' · not playing'}
                {item.status === 'eliminated' && ' · eliminated'}
              </Text>
            </View>
            {isCommissioner && item.role !== 'commissioner' && (
              <Pressable onPress={() => confirmRemove(item)}>
                <Text style={styles.removeLink}>Remove</Text>
              </Pressable>
            )}
          </View>
        )}
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
  loadingContainer: {
    flex: 1,
    backgroundColor: '#0f172a',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 12,
  },
  backLink: {
    color: '#64748b',
    fontSize: 12,
    textDecorationLine: 'underline',
    marginBottom: 8,
  },
  card: {
    backgroundColor: '#1e293b',
    borderRadius: 12,
    padding: 16,
    gap: 4,
    marginBottom: 12,
  },
  rowBetween: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  leagueName: {
    color: '#f1f5f9',
    fontSize: 18,
    fontWeight: '600',
  },
  leagueMeta: {
    color: '#94a3b8',
    fontSize: 13,
  },
  switchRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginTop: 8,
  },
  switchLabel: {
    color: '#cbd5e1',
    fontSize: 13,
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
  sectionTitle: {
    color: '#f1f5f9',
    fontSize: 14,
    fontWeight: '600',
    marginBottom: 8,
  },
  inviteRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  inviteCode: {
    flex: 1,
    backgroundColor: '#0f172a',
    color: '#f1f5f9',
    fontSize: 18,
    letterSpacing: 3,
    textAlign: 'center',
    paddingVertical: 10,
    borderRadius: 8,
  },
  buttonOutline: {
    borderRadius: 8,
    paddingVertical: 10,
    paddingHorizontal: 14,
    borderWidth: 1,
    borderColor: '#334155',
  },
  buttonOutlineText: {
    color: '#f1f5f9',
    fontWeight: '600',
    fontSize: 13,
  },
  memberRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    backgroundColor: '#1e293b',
    borderRadius: 10,
    padding: 14,
    marginBottom: 8,
  },
  memberName: {
    color: '#f1f5f9',
    fontSize: 14,
  },
  memberMeta: {
    color: '#94a3b8',
    fontSize: 12,
    marginTop: 2,
  },
  removeLink: {
    color: '#f87171',
    fontSize: 12,
    textDecorationLine: 'underline',
  },
  error: {
    color: '#f87171',
    fontSize: 13,
  },
  link: {
    color: '#94a3b8',
    fontSize: 13,
    textDecorationLine: 'underline',
  },
});
