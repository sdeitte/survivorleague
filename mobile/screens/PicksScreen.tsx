import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ActivityIndicator, FlatList, Image, Modal, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import { BrandWordmark } from '../components/BrandWordmark';
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
  const [weekPickerOpen, setWeekPickerOpen] = useState(false);

  const leagueQuery = useQuery({
    queryKey: ['league', leagueId],
    queryFn: () => authFetch((token) => api.getLeague(token, leagueId)),
  });

  const weeksQuery = useQuery({
    queryKey: ['weeks', leagueQuery.data?.season_year],
    queryFn: () => authFetch((token) => api.listWeeks(token, leagueQuery.data!.season_year)),
    enabled: !!leagueQuery.data,
  });

  // Default to the week that's currently occurring schedule-wise for the
  // league's conference — one request to the server (which already knows
  // every week's kickoff range), not a client-side scan across every
  // week's available-teams.
  const currentWeekQuery = useQuery({
    queryKey: ['league', leagueId, 'current-week'],
    queryFn: () => authFetch((token) => api.getCurrentWeek(token, leagueId)),
    enabled: selectedWeekId === null,
    retry: false, // a 404 (no schedule data yet) is an expected, terminal state, not worth retrying
  });

  useEffect(() => {
    if (selectedWeekId === null && currentWeekQuery.data) {
      setSelectedWeekId(currentWeekQuery.data.id);
    }
  }, [selectedWeekId, currentWeekQuery.data]);

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

  const teamLogoById = useMemo(() => {
    const map = new Map<string, string>();
    for (const t of availableTeamsQuery.data?.teams ?? []) {
      if (t.team_logo_url) map.set(t.team_id, t.team_logo_url);
    }
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
  const selectedIndex = weeks.findIndex((w) => w.id === weekId);
  const goToOffset = (offset: number) => {
    if (selectedIndex < 0) return;
    const next = weeks[selectedIndex + offset];
    if (next) setSelectedWeekId(next.id);
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content}>
      <View style={styles.brandRow}>
        <BrandWordmark size={90} />
      </View>

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
        <View style={styles.stepper}>
          <Pressable
            onPress={() => goToOffset(-1)}
            disabled={selectedIndex <= 0}
            style={[styles.stepperArrow, selectedIndex <= 0 && styles.stepperArrowDisabled]}
          >
            <Text style={styles.stepperArrowText}>‹</Text>
          </Pressable>
          <Pressable onPress={() => setWeekPickerOpen(true)} style={styles.stepperLabel}>
            <Text style={styles.stepperLabelText}>{selectedWeek ? `Week ${selectedWeek.week_number}` : 'Select week'}</Text>
          </Pressable>
          <Pressable
            onPress={() => goToOffset(1)}
            disabled={selectedIndex < 0 || selectedIndex >= weeks.length - 1}
            style={[styles.stepperArrow, (selectedIndex < 0 || selectedIndex >= weeks.length - 1) && styles.stepperArrowDisabled]}
          >
            <Text style={styles.stepperArrowText}>›</Text>
          </Pressable>
        </View>
      )}

      <Modal visible={weekPickerOpen} animationType="slide" transparent onRequestClose={() => setWeekPickerOpen(false)}>
        <Pressable style={styles.modalBackdrop} onPress={() => setWeekPickerOpen(false)}>
          <View style={styles.modalSheet}>
            <Text style={styles.modalTitle}>Select a week</Text>
            <FlatList
              data={weeks}
              keyExtractor={(w) => w.id}
              renderItem={({ item }) => (
                <Pressable
                  onPress={() => {
                    setSelectedWeekId(item.id);
                    setWeekPickerOpen(false);
                  }}
                  style={[styles.modalRow, item.id === weekId && styles.modalRowSelected]}
                >
                  <Text style={[styles.modalRowText, item.id === weekId && styles.modalRowTextSelected]}>
                    Week {item.week_number}
                  </Text>
                </Pressable>
              )}
            />
          </View>
        </Pressable>
      </Modal>

      {actionError && (
        <View style={styles.errorBox}>
          <Text style={styles.error}>{actionError}</Text>
        </View>
      )}

      {weekId && (
        <>
          {availableTeamsQuery.data?.current_pick &&
            (() => {
              const pickedTeam = availableTeamsQuery.data!.teams.find((t) => t.is_current_pick);
              return (
                <View style={styles.currentPickCard}>
                  <Text style={styles.currentPickLabel}>
                    Your pick — {selectedWeek ? `Week ${selectedWeek.week_number}` : 'this week'}
                  </Text>
                  {pickedTeam ? (
                    <View style={styles.currentPickRow}>
                      {pickedTeam.team_logo_url && (
                        <View style={styles.currentPickLogoBackdrop}>
                          <Image source={{ uri: pickedTeam.team_logo_url }} style={styles.currentPickLogo} />
                        </View>
                      )}
                      <View style={styles.currentPickTextCol}>
                        <Text style={styles.currentPickTeam}>{pickedTeam.team_name}</Text>
                        <Text style={styles.currentPickMeta}>
                          {pickedTeam.is_home ? 'vs' : '@'} {pickedTeam.opponent_name} ·{' '}
                          {new Date(pickedTeam.kickoff_at).toLocaleString(undefined, {
                            weekday: 'short',
                            month: 'short',
                            day: 'numeric',
                            hour: 'numeric',
                            minute: '2-digit',
                          })}
                        </Text>
                        {pickedTeam.is_locked && <Text style={styles.currentPickLocked}>Locked</Text>}
                      </View>
                    </View>
                  ) : (
                    <Text style={styles.currentPickMeta}>Saved</Text>
                  )}
                </View>
              );
            })()}

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
                  {team.team_logo_url && (
                    <View style={styles.teamRowLogoBackdrop}>
                      <Image source={{ uri: team.team_logo_url }} style={styles.teamRowLogo} />
                    </View>
                  )}
                  <View style={styles.teamRowText}>
                    <Text style={styles.teamName}>
                      {team.team_name}
                      {team.is_current_pick && <Text style={styles.currentPickBadge}>  Your pick</Text>}
                    </Text>
                    <Text style={styles.teamMeta}>
                      {team.is_home ? 'vs' : '@'} {team.opponent_name} ·{' '}
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
                  <Text style={styles.memberName}>{status.team_name || status.display_name}</Text>
                  {status.has_picked ? (
                    status.team_id ? (
                      <View style={styles.pickedTeamRow}>
                        {teamLogoById.get(status.team_id) && (
                          <View style={styles.pickedTeamLogoBackdrop}>
                            <Image source={{ uri: teamLogoById.get(status.team_id) }} style={styles.pickedTeamLogo} />
                          </View>
                        )}
                        <Text style={styles.pickedTeam}>{teamNameById.get(status.team_id) ?? 'Picked'}</Text>
                      </View>
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
  stepper: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 10,
    marginBottom: 4,
  },
  stepperArrow: {
    borderWidth: 1,
    borderColor: '#334155',
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  stepperArrowDisabled: {
    opacity: 0.3,
  },
  stepperArrowText: {
    color: '#cbd5e1',
    fontSize: 16,
  },
  stepperLabel: {
    borderWidth: 1,
    borderColor: '#334155',
    borderRadius: 8,
    paddingHorizontal: 16,
    paddingVertical: 8,
    minWidth: 110,
    alignItems: 'center',
  },
  stepperLabelText: {
    color: '#f1f5f9',
    fontSize: 14,
    fontWeight: '600',
  },
  modalBackdrop: {
    flex: 1,
    backgroundColor: '#00000099',
    justifyContent: 'flex-end',
  },
  modalSheet: {
    backgroundColor: '#1e293b',
    borderTopLeftRadius: 16,
    borderTopRightRadius: 16,
    padding: 16,
    maxHeight: '70%',
  },
  modalTitle: {
    color: '#f1f5f9',
    fontSize: 15,
    fontWeight: '600',
    marginBottom: 8,
  },
  modalRow: {
    paddingVertical: 12,
    paddingHorizontal: 8,
    borderRadius: 8,
  },
  modalRowSelected: {
    backgroundColor: '#0f172a',
  },
  modalRowText: {
    color: '#cbd5e1',
    fontSize: 14,
  },
  modalRowTextSelected: {
    color: '#f1f5f9',
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
  currentPickCard: {
    backgroundColor: '#022c2280',
    borderWidth: 1,
    borderColor: '#065f46',
    borderRadius: 12,
    padding: 16,
    gap: 2,
  },
  currentPickLabel: {
    color: '#34d399',
    fontSize: 12,
    marginBottom: 2,
  },
  currentPickTeam: {
    color: '#f1f5f9',
    fontSize: 18,
    fontWeight: '600',
  },
  currentPickMeta: {
    color: '#cbd5e1',
    fontSize: 13,
  },
  currentPickLocked: {
    color: '#64748b',
    fontSize: 12,
    marginTop: 2,
  },
  currentPickRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  currentPickLogoBackdrop: {
    width: 76,
    height: 76,
    borderRadius: 38,
    backgroundColor: '#ffffff',
    alignItems: 'center',
    justifyContent: 'center',
  },
  currentPickLogo: {
    width: 60,
    height: 60,
    resizeMode: 'contain',
  },
  currentPickTextCol: {
    flex: 1,
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
  teamRowLogoBackdrop: {
    width: 54,
    height: 54,
    borderRadius: 27,
    backgroundColor: '#ffffff',
    alignItems: 'center',
    justifyContent: 'center',
    marginRight: 12,
  },
  teamRowLogo: {
    width: 42,
    height: 42,
    resizeMode: 'contain',
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
  pickedTeamRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  pickedTeamLogoBackdrop: {
    width: 30,
    height: 30,
    borderRadius: 15,
    backgroundColor: '#ffffff',
    alignItems: 'center',
    justifyContent: 'center',
  },
  pickedTeamLogo: {
    width: 22,
    height: 22,
    resizeMode: 'contain',
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
