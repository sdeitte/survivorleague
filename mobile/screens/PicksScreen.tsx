import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import * as api from '../api';
import { ApiError, type AvailableTeam, type Pick, type Week } from '../api';

// The weekly picks screen — this was the old app's most complex screen too
// (its Pick.js was 639 lines). Mirrors web/src/routes/PicksPage.tsx's data
// flow exactly (same endpoints, same server-computed lock/used flags, same
// privacy rule for the league-picks list) with React Native components in
// place of the web's Tailwind markup.
export function PicksScreen({ leagueId, onBack }: { leagueId: string; onBack: () => void }) {
  const { authFetch } = useAuth();
  const queryClient = useQueryClient();
  const [selectedWeekId, setSelectedWeekId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const leagueQuery = useQuery({
    queryKey: ['league', leagueId],
    queryFn: () => authFetch((token) => api.getLeague(token, leagueId)),
  });

  const weeksQuery = useQuery({
    queryKey: ['weeks', leagueQuery.data?.season_year],
    queryFn: () => authFetch((token) => api.listWeeks(token, leagueQuery.data!.season_year)),
    enabled: !!leagueQuery.data,
  });

  // Default to the "current" week: the earliest week with at least one
  // not-yet-kicked-off game in the league's conference. Falls back to the
  // last week if the season's fully underway, or the first if there's no
  // schedule data at all yet.
  const currentWeekDetectQuery = useQuery({
    queryKey: ['league', leagueId, 'current-week-detect', weeksQuery.data?.map((w) => w.id).join(',')],
    queryFn: async () => {
      const weeks = weeksQuery.data!;
      const now = Date.now();
      for (const week of weeks) {
        const res = await authFetch((token) => api.getAvailableTeams(token, leagueId, week.id)).catch(() => ({
          teams: [] as AvailableTeam[],
        }));
        if (res.teams.some((t) => new Date(t.kickoff_at).getTime() > now)) {
          return week.id;
        }
      }
      return weeks.length > 0 ? weeks[weeks.length - 1].id : null;
    },
    enabled: !!weeksQuery.data && weeksQuery.data.length > 0 && selectedWeekId === null,
  });

  useEffect(() => {
    if (selectedWeekId === null && currentWeekDetectQuery.data) {
      setSelectedWeekId(currentWeekDetectQuery.data);
    }
  }, [selectedWeekId, currentWeekDetectQuery.data]);

  const weekId = selectedWeekId ?? undefined;

  const availableTeamsQuery = useQuery({
    queryKey: ['league', leagueId, 'weeks', weekId, 'available-teams'],
    queryFn: () => authFetch((token) => api.getAvailableTeams(token, leagueId, weekId!)),
    enabled: !!weekId,
  });

  // Every other week's own pick, used only to render "already used in
  // Week N" — available-teams tells us THAT a team is used elsewhere, not
  // WHICH week.
  const allPicksQuery = useQuery({
    queryKey: ['league', leagueId, 'all-my-picks', weeksQuery.data?.map((w) => w.id).join(',')],
    queryFn: () =>
      Promise.all(
        (weeksQuery.data ?? []).map((week) =>
          authFetch((token) => api.getMyPick(token, leagueId, week.id)).then((pick) => ({ week, pick })),
        ),
      ),
    enabled: !!weeksQuery.data && weeksQuery.data.length > 0,
  });

  const usedTeamWeekNumbers = useMemo(() => {
    const map = new Map<string, number>();
    for (const { week, pick } of allPicksQuery.data ?? []) {
      if (pick && week.id !== weekId) map.set(pick.team_id, week.week_number);
    }
    return map;
  }, [allPicksQuery.data, weekId]);

  const teamNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const t of availableTeamsQuery.data?.teams ?? []) map.set(t.team_id, t.team_name);
    return map;
  }, [availableTeamsQuery.data]);

  const hasOwnPick = !!availableTeamsQuery.data?.current_pick;
  const anyGameLockedThisWeek = availableTeamsQuery.data?.teams.some((t) => t.is_locked) ?? false;

  const weekPicksQuery = useQuery({
    queryKey: ['league', leagueId, 'weeks', weekId, 'picks'],
    queryFn: () => authFetch((token) => api.listWeekPicks(token, leagueId, weekId!)),
    enabled: !!weekId && (hasOwnPick || anyGameLockedThisWeek),
  });

  const pickMutation = useMutation({
    mutationFn: (team: AvailableTeam) =>
      authFetch((token) => api.upsertMyPick(token, leagueId, weekId!, { game_id: team.game_id, team_id: team.team_id })),
    onSuccess: (pick: Pick) => {
      setActionError(null);
      queryClient.setQueryData(
        ['league', leagueId, 'weeks', weekId, 'available-teams'],
        (old: typeof availableTeamsQuery.data) =>
          old && {
            ...old,
            current_pick: pick,
            teams: old.teams.map((t) => ({ ...t, is_current_pick: t.team_id === pick.team_id })),
          },
      );
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'weeks', weekId, 'picks'] });
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'all-my-picks'] });
    },
    onError: (err) => {
      if (err instanceof ApiError) {
        if (err.status === 409) {
          setActionError(
            err.message.toLowerCase().includes('used')
              ? "You've already used that team in a different week."
              : 'Your pick for this week is already locked — its game has started.',
          );
        } else {
          setActionError(err.message);
        }
      } else {
        setActionError('Failed to save your pick.');
      }
    },
  });

  if (leagueQuery.isLoading || weeksQuery.isLoading) {
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
  const weeks: Week[] = weeksQuery.data ?? [];
  const selectedWeek = weeks.find((w) => w.id === weekId);

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content}>
      <Pressable onPress={onBack}>
        <Text style={styles.backLink}>← {league.name}</Text>
      </Pressable>

      <Text style={styles.title}>Picks</Text>
      <Text style={styles.subtitle}>
        {league.conference} · {league.season_year}
      </Text>

      {weeks.length === 0 ? (
        <Text style={styles.subtitle}>No schedule data yet — check back once weeks are synced.</Text>
      ) : (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.weekRow}>
          {weeks.map((week) => (
            <Pressable
              key={week.id}
              onPress={() => setSelectedWeekId(week.id)}
              style={[styles.weekButton, week.id === weekId && styles.weekButtonSelected]}
            >
              <Text style={[styles.weekButtonText, week.id === weekId && styles.weekButtonTextSelected]}>
                Wk {week.week_number}
              </Text>
            </Pressable>
          ))}
        </ScrollView>
      )}

      {actionError && (
        <View style={styles.errorBox}>
          <Text style={styles.error}>{actionError}</Text>
        </View>
      )}

      {weekId && (
        <>
          <View style={styles.card}>
            <Text style={styles.sectionTitle}>
              {selectedWeek ? `Week ${selectedWeek.week_number}` : 'This week'}'s teams
            </Text>
            {availableTeamsQuery.isLoading && <ActivityIndicator color="#f1f5f9" />}
            {availableTeamsQuery.error && <Text style={styles.error}>Could not load teams for this week.</Text>}
            {availableTeamsQuery.data?.teams.length === 0 && (
              <Text style={styles.subtitle}>No {league.conference} games scheduled this week.</Text>
            )}
            {availableTeamsQuery.data?.teams.map((team) => {
              const disabled = team.is_used_elsewhere || (team.is_locked && !team.is_current_pick);
              const usedWeek = usedTeamWeekNumbers.get(team.team_id);
              let reason: string | null = null;
              if (team.is_used_elsewhere) {
                reason = usedWeek ? `Already used in Week ${usedWeek}` : 'Already used in a different week';
              } else if (team.is_locked && !team.is_current_pick) {
                reason = 'Game already started';
              }
              return (
                <Pressable
                  key={team.team_id}
                  disabled={disabled || pickMutation.isPending}
                  onPress={() => pickMutation.mutate(team)}
                  style={[
                    styles.teamRow,
                    team.is_current_pick && styles.teamRowSelected,
                    disabled && !team.is_current_pick && styles.teamRowDisabled,
                  ]}
                >
                  <View style={styles.teamRowText}>
                    <Text style={styles.teamName}>
                      {team.team_name}
                      {team.is_current_pick && <Text style={styles.currentPickBadge}>  Your pick</Text>}
                    </Text>
                    <Text style={styles.teamMeta}>
                      vs {team.opponent_name} ·{' '}
                      {new Date(team.kickoff_at).toLocaleString(undefined, {
                        weekday: 'short',
                        month: 'short',
                        day: 'numeric',
                        hour: 'numeric',
                        minute: '2-digit',
                      })}
                    </Text>
                    {reason && <Text style={styles.reasonText}>{reason}</Text>}
                  </View>
                  {team.is_locked && <Text style={styles.lockedBadge}>Locked</Text>}
                </Pressable>
              );
            })}
          </View>

          {(hasOwnPick || anyGameLockedThisWeek) && (
            <View style={styles.card}>
              <Text style={styles.sectionTitle}>League picks</Text>
              {weekPicksQuery.isLoading && <ActivityIndicator color="#f1f5f9" />}
              {weekPicksQuery.error && (
                <Text style={styles.error}>
                  {weekPicksQuery.error instanceof ApiError
                    ? weekPicksQuery.error.message
                    : 'Could not load the league picks for this week.'}
                </Text>
              )}
              {weekPicksQuery.data?.map((status) => (
                <View key={status.membership_id} style={styles.pickStatusRow}>
                  <Text style={styles.memberName}>{status.display_name}</Text>
                  {status.has_picked ? (
                    status.team_id ? (
                      <Text style={styles.pickedTeam}>{teamNameById.get(status.team_id) ?? 'Picked'}</Text>
                    ) : (
                      <Text style={styles.pickedHidden}>Picked (hidden until kickoff)</Text>
                    )
                  ) : (
                    <Text style={styles.notPicked}>Not yet picked</Text>
                  )}
                </View>
              ))}
            </View>
          )}
        </>
      )}
    </ScrollView>
  );
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
    marginBottom: 4,
  },
  title: {
    color: '#f1f5f9',
    fontSize: 20,
    fontWeight: '600',
  },
  subtitle: {
    color: '#94a3b8',
    fontSize: 13,
    marginBottom: 4,
  },
  weekRow: {
    flexDirection: 'row',
    marginBottom: 4,
  },
  weekButton: {
    borderWidth: 1,
    borderColor: '#334155',
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 8,
    marginRight: 6,
  },
  weekButtonSelected: {
    backgroundColor: '#f1f5f9',
    borderColor: '#f1f5f9',
  },
  weekButtonText: {
    color: '#cbd5e1',
    fontSize: 13,
  },
  weekButtonTextSelected: {
    color: '#0f172a',
    fontWeight: '600',
  },
  errorBox: {
    borderWidth: 1,
    borderColor: '#7f1d1d',
    backgroundColor: '#450a0a66',
    borderRadius: 8,
    padding: 10,
  },
  card: {
    backgroundColor: '#1e293b',
    borderRadius: 12,
    padding: 4,
    gap: 2,
  },
  sectionTitle: {
    color: '#f1f5f9',
    fontSize: 14,
    fontWeight: '600',
    padding: 12,
    paddingBottom: 4,
  },
  teamRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: 12,
    borderTopWidth: 1,
    borderTopColor: '#0f172a',
  },
  teamRowSelected: {
    backgroundColor: '#022c22',
  },
  teamRowDisabled: {
    opacity: 0.4,
  },
  teamRowText: {
    flex: 1,
  },
  teamName: {
    color: '#f1f5f9',
    fontSize: 14,
    fontWeight: '500',
  },
  currentPickBadge: {
    color: '#34d399',
    fontSize: 12,
  },
  teamMeta: {
    color: '#94a3b8',
    fontSize: 12,
    marginTop: 2,
  },
  reasonText: {
    color: '#f59e0b',
    fontSize: 12,
    marginTop: 2,
  },
  lockedBadge: {
    color: '#64748b',
    fontSize: 11,
    marginLeft: 8,
  },
  pickStatusRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: 12,
    borderTopWidth: 1,
    borderTopColor: '#0f172a',
  },
  memberName: {
    color: '#f1f5f9',
    fontSize: 14,
  },
  pickedTeam: {
    color: '#34d399',
    fontSize: 12,
  },
  pickedHidden: {
    color: '#94a3b8',
    fontSize: 12,
  },
  notPicked: {
    color: '#64748b',
    fontSize: 12,
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
