import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  Image,
  Modal,
  Pressable,
  Share,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  View,
} from 'react-native';
import { useAuth } from '../auth/AuthContext';
import { BrandWordmark } from '../components/BrandWordmark';
import { ChatSection } from '../components/ChatSection';
import { getConferenceLogoUrl } from '../leagues/conferenceLogos';
import * as api from '../api';
import { ApiError, type Member } from '../api';

export function LeagueDetailScreen({
  leagueId,
  onBack,
  onNavigateToPicks,
  onNavigateToLeaderboard,
}: {
  leagueId: string;
  onBack: () => void;
  onNavigateToPicks: (leagueId: string) => void;
  onNavigateToLeaderboard: (leagueId: string) => void;
}) {
  const { authFetch } = useAuth();
  const queryClient = useQueryClient();
  const [actionError, setActionError] = useState<string | null>(null);
  const [closeModalVisible, setCloseModalVisible] = useState(false);
  const [closeConfirmText, setCloseConfirmText] = useState('');
  const [inviteRows, setInviteRows] = useState<{ name: string; email: string }[]>([{ name: '', email: '' }]);
  const [inviteResults, setInviteResults] = useState<api.InviteSendResult[] | null>(null);
  const [teamNameDraft, setTeamNameDraft] = useState('');

  const leagueQuery = useQuery({
    queryKey: ['league', leagueId],
    queryFn: () => authFetch((token) => api.getLeague(token, leagueId)),
  });
  const membersQuery = useQuery({
    queryKey: ['league', leagueId, 'members'],
    queryFn: () => authFetch((token) => api.listMembers(token, leagueId)),
  });
  // 404 (no week has finalized yet) is expected/terminal, not worth
  // retrying — same treatment web's identical query gives its 404 case.
  const recapQuery = useQuery({
    queryKey: ['league', leagueId, 'recap'],
    queryFn: () => authFetch((token) => api.getLatestRecap(token, leagueId)),
    retry: false,
  });
  const noRecapYet = recapQuery.error instanceof ApiError && recapQuery.error.status === 404;

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

  const buyBackMutation = useMutation({
    mutationFn: (membershipId: string) => authFetch((token) => api.buyBackMember(token, leagueId, membershipId)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'members'] });
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'leaderboard'] });
    },
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to buy back member.'),
  });

  const closeMutation = useMutation({
    mutationFn: () => authFetch((token) => api.closeLeague(token, leagueId, closeConfirmText)),
    onSuccess: (updated) => {
      queryClient.setQueryData(['league', leagueId], updated);
      setCloseModalVisible(false);
      setCloseConfirmText('');
    },
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to close league.'),
  });

  const setTeamNameMutation = useMutation({
    mutationFn: (teamName: string) => authFetch((token) => api.updateTeamName(token, leagueId, teamName)),
    onSuccess: (updated) => queryClient.setQueryData(['league', leagueId], updated),
  });

  const updateInviteRow = (index: number, field: 'name' | 'email', value: string) => {
    setInviteRows((rows) => rows.map((row, i) => (i === index ? { ...row, [field]: value } : row)));
  };
  const addInviteRow = () => setInviteRows((rows) => [...rows, { name: '', email: '' }]);
  const removeInviteRow = (index: number) => setInviteRows((rows) => rows.filter((_, i) => i !== index));

  const sendInvitesMutation = useMutation({
    mutationFn: () =>
      authFetch((token) => api.sendInvites(token, leagueId, inviteRows.filter((row) => row.email.trim() !== ''))),
    onSuccess: (results) => {
      setInviteResults(results);
      if (results.every((r) => r.sent)) {
        setInviteRows([{ name: '', email: '' }]);
      }
    },
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to send invites.'),
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

  const confirmBuyBack = (member: Member) => {
    Alert.alert(
      'Buy back this member?',
      `${member.display_name} will be reinstated as an active contestant. This is a one-time lifeline per member — it cannot be undone or used again for them, even if they're eliminated again later. Their previously-used teams stay locked.`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Buy back',
          onPress: () => buyBackMutation.mutate(member.membership_id),
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
  const isClosed = league.status === 'closed';
  const closePhrase = `I want to close ${league.name}`;
  // Mirrors the backend's isLeagueJoinable — already covers "closed", so
  // this alone gates the invite code / invite-by-email UI. Defaults to
  // true while the invite query is still loading so these cards don't
  // flash hidden then shown on first render.
  const inviteJoinable = inviteQuery.data?.joinable ?? true;

  return (
    <View style={styles.container}>
      <View style={styles.brandRow}>
        <BrandWordmark size={90} />
      </View>

      <Pressable onPress={onBack}>
        <Text style={styles.backLink}>← My Leagues</Text>
      </Pressable>

      <FlatList<Member>
        data={membersQuery.data ?? []}
        keyExtractor={(item) => item.membership_id}
        ListHeaderComponent={
          <>
            {isClosed && (
              <View style={styles.closedBanner}>
                <Text style={styles.closedBannerTitle}>This league is closed</Text>
                <Text style={styles.closedBannerSubtitle}>
                  No new picks, joins, or changes can be made. The league and its history are still here to look
                  back on.
                </Text>
              </View>
            )}

            <View style={styles.actionsRow}>
              {isClosed ? (
                <View style={[styles.pickButtonDisabled, styles.actionButton]}>
                  <Text style={styles.pickButtonDisabledText}>Make your pick</Text>
                </View>
              ) : (
                <Pressable style={[styles.pickButton, styles.actionButton]} onPress={() => onNavigateToPicks(leagueId)}>
                  <Text style={styles.pickButtonText}>Make your pick</Text>
                </Pressable>
              )}
              <Pressable style={[styles.buttonOutline, styles.actionButton]} onPress={() => onNavigateToLeaderboard(leagueId)}>
                <Text style={styles.buttonOutlineText}>Leaderboard</Text>
              </Pressable>
            </View>

            <View style={styles.card}>
              <View style={styles.rowBetween}>
                <Text style={styles.leagueName}>{league.name}</Text>
                <View style={styles.badgeRow}>
                  {isClosed && (
                    <View style={styles.badgeClosed}>
                      <Text style={styles.badgeTextClosed}>closed</Text>
                    </View>
                  )}
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
              </View>
              <View style={styles.leagueMetaRow}>
                {getConferenceLogoUrl(league.conference) && (
                  <Image source={{ uri: getConferenceLogoUrl(league.conference) }} style={styles.conferenceLogoSmall} />
                )}
                <Text style={styles.leagueMeta}>
                  {league.conference} · {league.season_year}
                </Text>
              </View>

              {isCommissioner && (
                <View style={styles.switchRow}>
                  <Text style={styles.switchLabel}>Playing as a contestant</Text>
                  <Switch
                    value={league.membership.is_contestant}
                    disabled={toggleContestantMutation.isPending || isClosed}
                    onValueChange={(v) => toggleContestantMutation.mutate(v)}
                  />
                </View>
              )}
            </View>

            {isCommissioner && inviteJoinable && (
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

            {isCommissioner && inviteJoinable && (
              <View style={styles.card}>
                <Text style={styles.sectionTitle}>Invite by email</Text>
                <Text style={styles.inviteHint}>Add names and emails, then send everyone your invite link at once.</Text>
                {inviteRows.map((row, i) => (
                  <View key={i} style={styles.inviteRowInput}>
                    <TextInput
                      value={row.name}
                      onChangeText={(v) => updateInviteRow(i, 'name', v)}
                      placeholder="Name (optional)"
                      placeholderTextColor="#475569"
                      style={[styles.inviteInput, styles.inviteNameInput]}
                    />
                    <TextInput
                      value={row.email}
                      onChangeText={(v) => updateInviteRow(i, 'email', v)}
                      placeholder="email@example.com"
                      placeholderTextColor="#475569"
                      autoCapitalize="none"
                      autoCorrect={false}
                      keyboardType="email-address"
                      style={[styles.inviteInput, styles.inviteEmailInput]}
                    />
                    <Pressable
                      onPress={() => removeInviteRow(i)}
                      disabled={inviteRows.length === 1}
                      style={styles.inviteRemoveButton}
                    >
                      <Text style={[styles.inviteRemoveText, inviteRows.length === 1 && styles.buttonDisabled]}>✕</Text>
                    </Pressable>
                  </View>
                ))}
                <View style={styles.rowBetween}>
                  <Pressable onPress={addInviteRow}>
                    <Text style={styles.link}>+ Add another</Text>
                  </Pressable>
                  <Pressable
                    style={[
                      styles.pickButton,
                      (sendInvitesMutation.isPending || inviteRows.every((row) => row.email.trim() === '')) &&
                        styles.buttonDisabled,
                    ]}
                    disabled={sendInvitesMutation.isPending || inviteRows.every((row) => row.email.trim() === '')}
                    onPress={() => sendInvitesMutation.mutate()}
                  >
                    <Text style={styles.pickButtonText}>
                      {sendInvitesMutation.isPending ? 'Sending…' : 'Send invites'}
                    </Text>
                  </Pressable>
                </View>
                {inviteResults && (
                  <View style={styles.inviteResults}>
                    {inviteResults.map((r, i) => (
                      <Text key={r.email + i} style={r.sent ? styles.inviteResultSent : styles.inviteResultFailed}>
                        {r.email} — {r.sent ? 'sent' : r.error}
                      </Text>
                    ))}
                  </View>
                )}
              </View>
            )}

            {actionError && <Text style={styles.error}>{actionError}</Text>}

            <ChatSection leagueId={leagueId} isCommissioner={isCommissioner} />

            {recapQuery.data && !noRecapYet && (
              <View style={styles.card}>
                <Text style={styles.sectionTitle}>This week's recap</Text>
                <Text style={styles.recapBody}>{recapQuery.data.body}</Text>
              </View>
            )}

            <Text style={styles.sectionTitle}>Members</Text>
            {membersQuery.isLoading && <ActivityIndicator color="#f1f5f9" />}
            {membersQuery.error && (
              <Text style={styles.error}>
                {membersQuery.error instanceof ApiError ? membersQuery.error.message : 'Could not load members.'}
              </Text>
            )}
          </>
        }
        renderItem={({ item }) => (
          <View style={styles.memberRow}>
            <View>
              <Text style={styles.memberName}>{item.team_name || item.display_name}</Text>
              <Text style={styles.memberMeta}>
                {item.role}
                {!item.is_contestant && ' · not playing'}
                {item.status === 'eliminated' && ' · eliminated'}
              </Text>
            </View>
            <View style={styles.memberActions}>
              {isCommissioner && !isClosed && item.status === 'eliminated' && (
                item.bought_back ? (
                  <Text style={styles.buyBackUsedText}>Buy-back already used</Text>
                ) : (
                  <Pressable onPress={() => confirmBuyBack(item)}>
                    <Text style={styles.buyBackLink}>Buy back</Text>
                  </Pressable>
                )
              )}
              {isCommissioner && !isClosed && item.role !== 'commissioner' && (
                <Pressable onPress={() => confirmRemove(item)}>
                  <Text style={styles.removeLink}>Remove</Text>
                </Pressable>
              )}
            </View>
          </View>
        )}
        ListFooterComponent={
          isCommissioner && !isClosed ? (
            <View style={styles.dangerZone}>
              <Text style={styles.dangerZoneTitle}>Danger zone</Text>
              <Text style={styles.dangerZoneSubtitle}>
                Closing this league locks it for everyone — no more picks, joins, or changes. This can't be undone
                by you, though nothing is deleted.
              </Text>
              <Pressable style={styles.dangerZoneButton} onPress={() => setCloseModalVisible(true)}>
                <Text style={styles.dangerZoneButtonText}>Close league</Text>
              </Pressable>
            </View>
          ) : null
        }
      />

      <Modal
        visible={closeModalVisible}
        transparent
        animationType="fade"
        onRequestClose={() => setCloseModalVisible(false)}
      >
        <View style={styles.modalOverlay}>
          <View style={styles.modalCard}>
            <Text style={styles.modalTitle}>Close {league.name}?</Text>
            <Text style={styles.modalDescription}>
              Every member will be locked out — no more picks, joins, or league changes. The league and its full
              history stay saved, but this can't be undone by you. To confirm, type "{closePhrase}" below.
            </Text>
            <TextInput
              value={closeConfirmText}
              onChangeText={setCloseConfirmText}
              placeholder={closePhrase}
              placeholderTextColor="#475569"
              autoCorrect={false}
              autoCapitalize="none"
              contextMenuHidden
              style={styles.modalInput}
            />
            {closeMutation.error && (
              <Text style={styles.error}>
                {closeMutation.error instanceof ApiError ? closeMutation.error.message : 'Failed to close league.'}
              </Text>
            )}
            <View style={styles.modalButtonRow}>
              <Pressable
                style={styles.modalCancelButton}
                onPress={() => {
                  setCloseModalVisible(false);
                  setCloseConfirmText('');
                }}
              >
                <Text style={styles.modalCancelButtonText}>Cancel</Text>
              </Pressable>
              <Pressable
                style={[
                  styles.modalConfirmButton,
                  (closeConfirmText !== closePhrase || closeMutation.isPending) && styles.buttonDisabled,
                ]}
                disabled={closeConfirmText !== closePhrase || closeMutation.isPending}
                onPress={() => closeMutation.mutate()}
              >
                <Text style={styles.modalConfirmButtonText}>
                  {closeMutation.isPending ? 'Closing…' : 'Close league'}
                </Text>
              </Pressable>
            </View>
          </View>
        </View>
      </Modal>

      {/* One-time backfill prompt: only pre-existing memberships from
          before team names were required can ever have a blank one — a
          new join/create always sets one up front, so this naturally
          stops firing once every membership has a name. Non-dismissable
          (no onRequestClose) since there's nothing sensible to fall back
          to — every league now requires a team name. */}
      <Modal visible={!league.membership.team_name} transparent animationType="fade">
        <View style={styles.modalOverlay}>
          <View style={styles.modalCard}>
            <Text style={styles.modalTitle}>Set your team name</Text>
            <Text style={styles.modalDescription}>
              Give your squad in {league.name} a name — it'll show up on the leaderboard, chat, and picks instead
              of your player name.
            </Text>
            <TextInput
              value={teamNameDraft}
              onChangeText={setTeamNameDraft}
              placeholder="Team name"
              placeholderTextColor="#475569"
              maxLength={60}
              style={styles.modalInput}
            />
            {setTeamNameMutation.error && (
              <Text style={styles.error}>
                {setTeamNameMutation.error instanceof ApiError
                  ? setTeamNameMutation.error.message
                  : 'Failed to save team name.'}
              </Text>
            )}
            <Pressable
              style={[
                styles.modalConfirmButtonWide,
                (!teamNameDraft.trim() || setTeamNameMutation.isPending) && styles.buttonDisabled,
              ]}
              disabled={!teamNameDraft.trim() || setTeamNameMutation.isPending}
              onPress={() => setTeamNameMutation.mutate(teamNameDraft)}
            >
              <Text style={styles.modalConfirmButtonText}>
                {setTeamNameMutation.isPending ? 'Saving…' : 'Save team name'}
              </Text>
            </Pressable>
          </View>
        </View>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  brandRow: {
    alignItems: 'center',
  },
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
  actionsRow: {
    flexDirection: 'row',
    gap: 8,
    marginBottom: 12,
  },
  actionButton: {
    flex: 1,
    marginBottom: 0,
    alignItems: 'center',
  },
  pickButton: {
    backgroundColor: '#059669',
    borderRadius: 10,
    paddingVertical: 12,
    alignItems: 'center',
    marginBottom: 12,
  },
  pickButtonText: {
    color: '#ffffff',
    fontSize: 14,
    fontWeight: '600',
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
  leagueMetaRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  conferenceLogoSmall: {
    width: 32,
    height: 32,
    resizeMode: 'contain',
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
  recapBody: {
    color: '#cbd5e1',
    fontSize: 13,
    lineHeight: 18,
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
  memberActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  removeLink: {
    color: '#f87171',
    fontSize: 12,
    textDecorationLine: 'underline',
  },
  buyBackLink: {
    color: '#34d399',
    fontSize: 12,
    textDecorationLine: 'underline',
  },
  buyBackUsedText: {
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
  closedBanner: {
    borderWidth: 1,
    borderColor: '#92400e',
    backgroundColor: '#451a03',
    borderRadius: 10,
    padding: 12,
    marginBottom: 12,
    gap: 4,
  },
  closedBannerTitle: {
    color: '#fcd34d',
    fontSize: 13,
    fontWeight: '600',
  },
  closedBannerSubtitle: {
    color: '#fbbf24',
    fontSize: 12,
  },
  pickButtonDisabled: {
    backgroundColor: '#1e293b',
    borderRadius: 10,
    paddingVertical: 12,
    alignItems: 'center',
    marginBottom: 12,
  },
  pickButtonDisabledText: {
    color: '#64748b',
    fontSize: 14,
    fontWeight: '600',
  },
  badgeRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  badgeClosed: {
    borderRadius: 999,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderWidth: 1,
    borderColor: '#b45309',
  },
  badgeTextClosed: {
    color: '#fbbf24',
    fontSize: 11,
  },
  dangerZone: {
    borderWidth: 1,
    borderColor: '#7f1d1d',
    backgroundColor: 'rgba(69, 10, 10, 0.3)',
    borderRadius: 12,
    padding: 16,
    gap: 6,
    marginTop: 4,
  },
  dangerZoneTitle: {
    color: '#fca5a5',
    fontSize: 13,
    fontWeight: '600',
  },
  dangerZoneSubtitle: {
    color: '#f87171',
    fontSize: 12,
  },
  dangerZoneButton: {
    alignSelf: 'flex-start',
    borderWidth: 1,
    borderColor: '#991b1b',
    borderRadius: 8,
    paddingVertical: 8,
    paddingHorizontal: 14,
    marginTop: 4,
  },
  dangerZoneButtonText: {
    color: '#fca5a5',
    fontSize: 13,
    fontWeight: '600',
  },
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0, 0, 0, 0.6)',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  modalCard: {
    width: '100%',
    maxWidth: 400,
    backgroundColor: '#1e293b',
    borderRadius: 14,
    borderWidth: 1,
    borderColor: '#7f1d1d',
    padding: 20,
    gap: 12,
  },
  modalTitle: {
    color: '#f1f5f9',
    fontSize: 17,
    fontWeight: '600',
  },
  modalDescription: {
    color: '#94a3b8',
    fontSize: 13,
    lineHeight: 18,
  },
  modalInput: {
    borderWidth: 1,
    borderColor: '#334155',
    backgroundColor: '#0f172a',
    borderRadius: 8,
    paddingVertical: 10,
    paddingHorizontal: 12,
    color: '#f1f5f9',
    fontSize: 14,
  },
  modalButtonRow: {
    flexDirection: 'row',
    gap: 8,
  },
  modalCancelButton: {
    flex: 1,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: '#334155',
    paddingVertical: 10,
    alignItems: 'center',
  },
  modalCancelButtonText: {
    color: '#f1f5f9',
    fontSize: 14,
    fontWeight: '600',
  },
  modalConfirmButton: {
    flex: 1,
    borderRadius: 8,
    backgroundColor: '#dc2626',
    paddingVertical: 10,
    alignItems: 'center',
  },
  modalConfirmButtonWide: {
    borderRadius: 8,
    backgroundColor: '#059669',
    paddingVertical: 10,
    alignItems: 'center',
  },
  modalConfirmButtonText: {
    color: '#ffffff',
    fontSize: 14,
    fontWeight: '600',
  },
  buttonDisabled: {
    opacity: 0.4,
  },
  inviteHint: {
    color: '#64748b',
    fontSize: 12,
    marginBottom: 8,
  },
  inviteRowInput: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginBottom: 8,
  },
  inviteInput: {
    borderWidth: 1,
    borderColor: '#334155',
    backgroundColor: '#0f172a',
    borderRadius: 8,
    paddingVertical: 8,
    paddingHorizontal: 10,
    color: '#f1f5f9',
    fontSize: 13,
  },
  inviteNameInput: {
    flex: 2,
  },
  inviteEmailInput: {
    flex: 3,
  },
  inviteRemoveButton: {
    paddingHorizontal: 6,
    paddingVertical: 6,
  },
  inviteRemoveText: {
    color: '#f87171',
    fontSize: 14,
  },
  inviteResults: {
    marginTop: 8,
    paddingTop: 8,
    borderTopWidth: 1,
    borderTopColor: '#334155',
    gap: 2,
  },
  inviteResultSent: {
    color: '#34d399',
    fontSize: 12,
  },
  inviteResultFailed: {
    color: '#f87171',
    fontSize: 12,
  },
});
