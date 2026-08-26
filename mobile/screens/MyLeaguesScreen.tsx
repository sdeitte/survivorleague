import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ActivityIndicator, FlatList, Image, Pressable, StyleSheet, Text, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import { BrandWordmark } from '../components/BrandWordmark';
import { getConferenceLogoUrl } from '../leagues/conferenceLogos';
import * as api from '../api';
import type { League } from '../api';
import { ApiError } from '../api';

// Shown when the signed-in user's /me response has email_verified_at:
// null. There's no real email delivery to click through in this
// environment (and no deep-linking infra either — see
// ResetPasswordScreen's doc comment for the same limitation applied to
// reset tokens), so a resend button is the full extent of this UI — see
// api/internal/auth/password_reset.go for the backend side.
function VerifyEmailBanner() {
  const { authFetch } = useAuth();
  const [status, setStatus] = useState<'idle' | 'sending' | 'sent' | 'error'>('idle');
  const [error, setError] = useState<string | null>(null);

  const onResend = async () => {
    setStatus('sending');
    setError(null);
    try {
      await authFetch((token) => api.resendVerification(token));
      setStatus('sent');
    } catch (err) {
      setStatus('error');
      setError(err instanceof ApiError ? err.message : 'Failed to send verification email.');
    }
  };

  return (
    <View style={styles.verifyBanner}>
      <Text style={styles.verifyTitle}>Verify your email address</Text>
      <Text style={styles.verifySubtitle}>
        {status === 'sent'
          ? 'Verification email sent — check your inbox.'
          : "We sent a verification link when you signed up. Didn't get it?"}
      </Text>
      {error && <Text style={styles.error}>{error}</Text>}
      <Pressable
        style={[styles.verifyButton, (status === 'sending' || status === 'sent') && styles.buttonDisabled]}
        onPress={() => void onResend()}
        disabled={status === 'sending' || status === 'sent'}
      >
        <Text style={styles.verifyButtonText}>
          {status === 'sending' ? 'Sending…' : status === 'sent' ? 'Sent' : 'Resend email'}
        </Text>
      </Pressable>
    </View>
  );
}

// "My Leagues" — the main authenticated screen (Phase 2 replaces Phase 1's
// GET-/me-proving HomeScreen with the real landing view): leagues the
// signed-in user belongs to, plus entry points to create one or join one
// by code.
export function MyLeaguesScreen({
  onNavigateToCreate,
  onNavigateToJoin,
  onNavigateToLeague,
  onNavigateToHealth,
  onNavigateToNotificationPreferences,
  onNavigateToSettings,
  onNavigateToAdmin,
}: {
  onNavigateToCreate: () => void;
  onNavigateToJoin: () => void;
  onNavigateToLeague: (leagueId: string) => void;
  onNavigateToHealth: () => void;
  onNavigateToNotificationPreferences: () => void;
  onNavigateToSettings: () => void;
  /** Present only for a signed-in site admin (see App.tsx). */
  onNavigateToAdmin?: () => void;
}) {
  const { user, logout, authFetch } = useAuth();

  const { data: leagues, error, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['leagues'],
    queryFn: () => authFetch((token) => api.listLeagues(token)),
  });

  return (
    <View style={styles.container}>
      <View style={styles.brandRow}>
        <BrandWordmark size={138} />
      </View>
      <View style={styles.header}>
        <Text style={styles.subtitle}>Signed in as {user?.display_name}</Text>
        <View style={styles.headerLinks}>
          <Pressable onPress={onNavigateToSettings}>
            <Text style={styles.link}>Settings</Text>
          </Pressable>
          <Pressable onPress={() => void logout()}>
            <Text style={styles.link}>Log out</Text>
          </Pressable>
        </View>
      </View>

      {user && user.email_verified_at === null && <VerifyEmailBanner />}

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
          <Pressable
            style={[styles.leagueRow, item.status === 'closed' && styles.leagueRowClosed]}
            onPress={() => onNavigateToLeague(item.id)}
          >
            <View style={styles.leagueRowLeft}>
              {getConferenceLogoUrl(item.conference) && (
                <Image source={{ uri: getConferenceLogoUrl(item.conference) }} style={styles.conferenceLogo} />
              )}
              <View style={styles.leagueRowTextCol}>
                <Text style={styles.leagueName}>{item.name}</Text>
                <Text style={styles.leagueMeta}>
                  {item.conference} · {item.season_year}
                </Text>
              </View>
            </View>
            <View style={styles.badgeRow}>
              {item.status === 'closed' && (
                <View style={styles.badgeClosed}>
                  <Text style={styles.badgeTextClosed}>closed</Text>
                </View>
              )}
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
            </View>
          </Pressable>
        )}
      />

      <Pressable onPress={onNavigateToNotificationPreferences}>
        <Text style={styles.link}>Notification preferences</Text>
      </Pressable>
      <Pressable onPress={onNavigateToHealth}>
        <Text style={styles.link}>API health check</Text>
      </Pressable>
      {onNavigateToAdmin && (
        <Pressable onPress={onNavigateToAdmin}>
          <Text style={styles.linkAdmin}>Site admin</Text>
        </Pressable>
      )}
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
  brandRow: {
    alignItems: 'center',
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  headerLinks: {
    flexDirection: 'row',
    gap: 12,
  },
  subtitle: {
    color: '#94a3b8',
    fontSize: 13,
    marginTop: 2,
  },
  verifyBanner: {
    borderWidth: 1,
    borderColor: '#92400e',
    backgroundColor: '#451a03',
    borderRadius: 10,
    padding: 12,
    gap: 6,
  },
  verifyTitle: {
    color: '#fcd34d',
    fontSize: 13,
    fontWeight: '600',
  },
  verifySubtitle: {
    color: '#fbbf24',
    fontSize: 12,
  },
  verifyButton: {
    alignSelf: 'flex-start',
    borderWidth: 1,
    borderColor: '#b45309',
    borderRadius: 8,
    paddingVertical: 6,
    paddingHorizontal: 12,
    marginTop: 2,
  },
  verifyButtonText: {
    color: '#fcd34d',
    fontSize: 12,
    fontWeight: '600',
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  actionsRow: {
    flexDirection: 'row',
    gap: 8,
  },
  actionButton: {
    flex: 1,
  },
  button: {
    backgroundColor: '#059669',
    borderRadius: 8,
    paddingVertical: 12,
    alignItems: 'center',
  },
  buttonText: {
    color: '#ffffff',
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
  leagueRowLeft: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    flexShrink: 1,
  },
  leagueRowTextCol: {
    flexShrink: 1,
  },
  conferenceLogo: {
    width: 50,
    height: 50,
    resizeMode: 'contain',
  },
  leagueRowClosed: {
    opacity: 0.6,
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
  linkAdmin: {
    color: '#34d399',
    fontSize: 13,
    textAlign: 'center',
    textDecorationLine: 'underline',
  },
});
