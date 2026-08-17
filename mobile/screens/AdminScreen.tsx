import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ActivityIndicator, Alert, FlatList, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import * as api from '../api';
import { ApiError, type AdminLeague, type AdminUser, type AuditLogEntry, type SyncRun } from '../api';

// A minimal-but-real site-admin screen for mobile — per the plan, admin
// tooling is web's job first; this mirrors web's feature set (leagues,
// users with disable/enable, sync runs + trigger, single-game resync by
// id, audit log) with a simpler tab-switch UI and no fancy game picker
// (enter a game id directly, rather than web's season/week browser).
// Client-side gating on user.is_site_admin happens one level up (App.tsx
// only reaches this screen for a signed-in site admin) — the real
// enforcement is server-side (requireSiteAdmin on every /admin/* route).

const PAGE_SIZE = 25;

type Tab = 'leagues' | 'users' | 'sync' | 'resync' | 'audit';

export function AdminScreen({ onBack }: { onBack: () => void }) {
  const [tab, setTab] = useState<Tab>('leagues');

  return (
    <View style={styles.container}>
      <Pressable onPress={onBack}>
        <Text style={styles.backLink}>← Back</Text>
      </Pressable>
      <Text style={styles.title}>Site admin</Text>

      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.tabRow} contentContainerStyle={{ gap: 8 }}>
        {(['leagues', 'users', 'sync', 'resync', 'audit'] as Tab[]).map((t) => (
          <Pressable key={t} style={[styles.tabButton, tab === t && styles.tabButtonActive]} onPress={() => setTab(t)}>
            <Text style={[styles.tabButtonText, tab === t && styles.tabButtonTextActive]}>
              {t === 'sync' ? 'Sync runs' : t === 'resync' ? 'Resync game' : t === 'audit' ? 'Audit log' : t}
            </Text>
          </Pressable>
        ))}
      </ScrollView>

      {tab === 'leagues' && <LeaguesTab />}
      {tab === 'users' && <UsersTab />}
      {tab === 'sync' && <SyncRunsTab />}
      {tab === 'resync' && <ResyncGameTab />}
      {tab === 'audit' && <AuditLogTab />}
    </View>
  );
}

function PageControls({
  offset,
  total,
  onPrev,
  onNext,
}: {
  offset: number;
  total: number;
  onPrev: () => void;
  onNext: () => void;
}) {
  if (total <= PAGE_SIZE) return null;
  return (
    <View style={styles.pageRow}>
      <Pressable disabled={offset === 0} onPress={onPrev}>
        <Text style={[styles.link, offset === 0 && styles.linkDisabled]}>Previous</Text>
      </Pressable>
      <Text style={styles.meta}>
        {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
      </Text>
      <Pressable disabled={offset + PAGE_SIZE >= total} onPress={onNext}>
        <Text style={[styles.link, offset + PAGE_SIZE >= total && styles.linkDisabled]}>Next</Text>
      </Pressable>
    </View>
  );
}

function LeaguesTab() {
  const { authFetch } = useAuth();
  const [offset, setOffset] = useState(0);

  const query = useQuery({
    queryKey: ['admin', 'leagues', offset],
    queryFn: () => authFetch((token) => api.listAdminLeagues(token, PAGE_SIZE, offset)),
  });

  if (query.isLoading) return <ActivityIndicator color="#f1f5f9" />;
  if (query.error) return <Text style={styles.error}>{errorMessage(query.error, 'Could not load leagues.')}</Text>;

  return (
    <View style={{ flex: 1 }}>
      <FlatList<AdminLeague>
        data={query.data?.leagues ?? []}
        keyExtractor={(item) => item.id}
        ListEmptyComponent={<Text style={styles.empty}>No leagues found.</Text>}
        renderItem={({ item }) => (
          <View style={styles.card}>
            <Text style={styles.cardTitle}>{item.name}</Text>
            <Text style={styles.cardMeta}>
              {item.conference} · {item.season_year} · {item.member_count} members
            </Text>
            <Text style={styles.cardMetaSmall}>
              Commissioner: {item.commissioner.display_name} ({item.commissioner.email})
            </Text>
          </View>
        )}
      />
      <PageControls
        offset={offset}
        total={query.data?.total ?? 0}
        onPrev={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
        onNext={() => setOffset((o) => o + PAGE_SIZE)}
      />
    </View>
  );
}

function UsersTab() {
  const { user: me, authFetch } = useAuth();
  const queryClient = useQueryClient();
  const [offset, setOffset] = useState(0);

  const query = useQuery({
    queryKey: ['admin', 'users', offset],
    queryFn: () => authFetch((token) => api.listAdminUsers(token, PAGE_SIZE, offset)),
  });

  const disableMutation = useMutation({
    mutationFn: (userId: string) => authFetch((token) => api.disableUser(token, userId)),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['admin', 'users'] }),
    onError: (err) => Alert.alert('Failed to disable user', errorMessage(err, 'Unknown error')),
  });

  const enableMutation = useMutation({
    mutationFn: (userId: string) => authFetch((token) => api.enableUser(token, userId)),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['admin', 'users'] }),
    onError: (err) => Alert.alert('Failed to enable user', errorMessage(err, 'Unknown error')),
  });

  const confirmDisable = (u: AdminUser) => {
    Alert.alert('Disable this account?', `${u.display_name} (${u.email}) will be unable to log in until re-enabled.`, [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Disable', style: 'destructive', onPress: () => disableMutation.mutate(u.id) },
    ]);
  };

  const confirmEnable = (u: AdminUser) => {
    Alert.alert('Re-enable this account?', `${u.display_name} (${u.email}) will be able to log in again.`, [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Enable', onPress: () => enableMutation.mutate(u.id) },
    ]);
  };

  if (query.isLoading) return <ActivityIndicator color="#f1f5f9" />;
  if (query.error) return <Text style={styles.error}>{errorMessage(query.error, 'Could not load users.')}</Text>;

  return (
    <View style={{ flex: 1 }}>
      <FlatList<AdminUser>
        data={query.data?.users ?? []}
        keyExtractor={(item) => item.id}
        ListEmptyComponent={<Text style={styles.empty}>No users found.</Text>}
        renderItem={({ item }) => (
          <View style={styles.card}>
            <View style={styles.cardRow}>
              <Text style={styles.cardTitle}>{item.display_name}</Text>
              <View style={[styles.badge, item.status === 'active' ? styles.badgeActive : styles.badgeDisabled]}>
                <Text style={item.status === 'active' ? styles.badgeTextActive : styles.badgeTextDisabled}>
                  {item.status}
                </Text>
              </View>
            </View>
            <Text style={styles.cardMeta}>{item.email}</Text>
            <Text style={styles.cardMetaSmall}>{item.league_count} leagues</Text>
            {item.status === 'active' ? (
              <Pressable disabled={item.id === me?.id} onPress={() => confirmDisable(item)}>
                <Text style={[styles.linkDanger, item.id === me?.id && styles.linkDisabled]}>
                  {item.id === me?.id ? "Can't disable your own account" : 'Disable'}
                </Text>
              </Pressable>
            ) : (
              <Pressable onPress={() => confirmEnable(item)}>
                <Text style={styles.linkSuccess}>Enable</Text>
              </Pressable>
            )}
          </View>
        )}
      />
      <PageControls
        offset={offset}
        total={query.data?.total ?? 0}
        onPrev={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
        onNext={() => setOffset((o) => o + PAGE_SIZE)}
      />
    </View>
  );
}

function SyncRunsTab() {
  const { authFetch } = useAuth();
  const queryClient = useQueryClient();
  const [seasonYear, setSeasonYear] = useState(String(currentSeasonYear()));

  const query = useQuery({
    queryKey: ['admin', 'sync-runs'],
    queryFn: () => authFetch((token) => api.listSyncRuns(token)),
  });

  const triggerMutation = useMutation({
    mutationFn: () => authFetch((token) => api.triggerScheduleSync(token, Number(seasonYear))),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['admin', 'sync-runs'] }),
    onError: (err) => Alert.alert('Failed to trigger sync', errorMessage(err, 'Unknown error')),
  });

  return (
    <View style={{ flex: 1 }}>
      <View style={styles.card}>
        <Text style={styles.cardTitle}>Trigger a schedule sync</Text>
        <View style={styles.cardRow}>
          <TextInput
            style={styles.input}
            value={seasonYear}
            onChangeText={setSeasonYear}
            keyboardType="number-pad"
          />
          <Pressable
            style={[styles.button, triggerMutation.isPending && styles.buttonDisabled]}
            disabled={triggerMutation.isPending}
            onPress={() => triggerMutation.mutate()}
          >
            <Text style={styles.buttonText}>{triggerMutation.isPending ? 'Syncing…' : 'Sync now'}</Text>
          </Pressable>
        </View>
        {triggerMutation.isSuccess && (
          <Text style={styles.linkSuccess}>Sync recorded — status: {triggerMutation.data.status}</Text>
        )}
      </View>

      {query.isLoading && <ActivityIndicator color="#f1f5f9" />}
      {query.error && <Text style={styles.error}>{errorMessage(query.error, 'Could not load sync runs.')}</Text>}
      <FlatList<SyncRun>
        data={query.data ?? []}
        keyExtractor={(item) => item.id}
        ListEmptyComponent={<Text style={styles.empty}>No sync runs yet.</Text>}
        renderItem={({ item }) => (
          <View style={styles.card}>
            <View style={styles.cardRow}>
              <Text style={styles.cardTitle}>{item.kind}</Text>
              <View
                style={[
                  styles.badge,
                  item.status === 'success' ? styles.badgeActive : item.status === 'failed' ? styles.badgeDisabled : styles.badgeRunning,
                ]}
              >
                <Text
                  style={
                    item.status === 'success'
                      ? styles.badgeTextActive
                      : item.status === 'failed'
                        ? styles.badgeTextDisabled
                        : styles.badgeTextRunning
                  }
                >
                  {item.status}
                </Text>
              </View>
            </View>
            <Text style={styles.cardMetaSmall}>Started {new Date(item.started_at).toLocaleString()}</Text>
            {item.error && <Text style={styles.error}>{item.error}</Text>}
          </View>
        )}
      />
    </View>
  );
}

function ResyncGameTab() {
  const { authFetch } = useAuth();
  const [gameId, setGameId] = useState('');

  const resyncMutation = useMutation({
    mutationFn: (id: string) => authFetch((token) => api.resyncGame(token, id)),
    onError: (err) => Alert.alert('Failed to resync game', errorMessage(err, 'Unknown error')),
  });

  return (
    <ScrollView contentContainerStyle={{ gap: 12 }}>
      <Text style={styles.cardMeta}>
        Enter a game's internal id (its UUID, not its CFBD external_id — visible via the web admin's resync
        picker, or the audit log's target_id after any past action on it) to re-fetch it from CFBD. If it's now
        final, this also runs the same grading pass the live poll loop would.
      </Text>
      <View style={styles.cardRow}>
        <TextInput
          style={[styles.input, { flex: 1 }]}
          placeholder="Game id (UUID)"
          placeholderTextColor="#64748b"
          value={gameId}
          onChangeText={setGameId}
          autoCapitalize="none"
        />
        <Pressable
          style={[styles.button, (!gameId || resyncMutation.isPending) && styles.buttonDisabled]}
          disabled={!gameId || resyncMutation.isPending}
          onPress={() => resyncMutation.mutate(gameId)}
        >
          <Text style={styles.buttonText}>{resyncMutation.isPending ? 'Resyncing…' : 'Resync'}</Text>
        </Pressable>
      </View>
      {resyncMutation.data && (
        <View style={styles.card}>
          <Text style={styles.cardTitle}>Now status: {resyncMutation.data.game.status}</Text>
          {resyncMutation.data.finalized_league_weeks.length === 0 ? (
            <Text style={styles.cardMeta}>No league-weeks finalized as a result.</Text>
          ) : (
            <>
              <Text style={styles.linkSuccess}>
                {resyncMutation.data.finalized_league_weeks.length} league-week
                {resyncMutation.data.finalized_league_weeks.length === 1 ? '' : 's'} finalized:
              </Text>
              {resyncMutation.data.finalized_league_weeks.map((f) => (
                <Text key={`${f.league_id}-${f.week_id}`} style={styles.cardMetaSmall}>
                  league {f.league_id.slice(0, 8)}… {f.mass_wipeout ? '(mass wipeout)' : ''}
                </Text>
              ))}
            </>
          )}
        </View>
      )}
    </ScrollView>
  );
}

function AuditLogTab() {
  const { authFetch } = useAuth();
  const [offset, setOffset] = useState(0);
  const [actionFilter, setActionFilter] = useState('');

  const query = useQuery({
    queryKey: ['admin', 'audit-log', offset, actionFilter],
    queryFn: () => authFetch((token) => api.listAuditLog(token, PAGE_SIZE, offset, { action: actionFilter || undefined })),
  });

  if (query.isLoading) return <ActivityIndicator color="#f1f5f9" />;
  if (query.error) return <Text style={styles.error}>{errorMessage(query.error, 'Could not load the audit log.')}</Text>;

  return (
    <View style={{ flex: 1 }}>
      <TextInput
        style={styles.input}
        placeholder="Filter by action (e.g. resync_game)"
        placeholderTextColor="#64748b"
        value={actionFilter}
        onChangeText={(v) => {
          setActionFilter(v);
          setOffset(0);
        }}
      />
      <FlatList<AuditLogEntry>
        data={query.data?.entries ?? []}
        keyExtractor={(item) => item.id}
        ListEmptyComponent={<Text style={styles.empty}>No matching entries.</Text>}
        renderItem={({ item }) => (
          <View style={styles.card}>
            <View style={styles.cardRow}>
              <Text style={styles.cardTitle}>{item.action}</Text>
              <Text style={styles.cardMetaSmall}>{new Date(item.created_at).toLocaleString()}</Text>
            </View>
            <Text style={styles.cardMetaSmall}>
              {item.target_type ? `target: ${item.target_type} ${item.target_id?.slice(0, 8)}…` : ''}
            </Text>
          </View>
        )}
      />
      <PageControls
        offset={offset}
        total={query.data?.total ?? 0}
        onPrev={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
        onNext={() => setOffset((o) => o + PAGE_SIZE)}
      />
    </View>
  );
}

function currentSeasonYear(): number {
  const now = new Date();
  return now.getMonth() + 1 >= 7 ? now.getFullYear() : now.getFullYear() - 1;
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback;
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
  },
  title: {
    color: '#f1f5f9',
    fontSize: 20,
    fontWeight: '600',
  },
  tabRow: {
    flexGrow: 0,
  },
  tabButton: {
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderWidth: 1,
    borderColor: '#334155',
  },
  tabButtonActive: {
    backgroundColor: '#f1f5f9',
    borderColor: '#f1f5f9',
  },
  tabButtonText: {
    color: '#cbd5e1',
    fontSize: 12,
  },
  tabButtonTextActive: {
    color: '#0f172a',
    fontWeight: '600',
  },
  card: {
    backgroundColor: '#1e293b',
    borderRadius: 10,
    padding: 14,
    marginBottom: 8,
    gap: 4,
  },
  cardRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  cardTitle: {
    color: '#f1f5f9',
    fontSize: 14,
    fontWeight: '500',
  },
  cardMeta: {
    color: '#94a3b8',
    fontSize: 12,
  },
  cardMetaSmall: {
    color: '#64748b',
    fontSize: 11,
  },
  badge: {
    borderRadius: 999,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderWidth: 1,
  },
  badgeActive: {
    borderColor: '#047857',
  },
  badgeDisabled: {
    borderColor: '#b91c1c',
  },
  badgeRunning: {
    borderColor: '#b45309',
  },
  badgeTextActive: {
    color: '#34d399',
    fontSize: 11,
  },
  badgeTextDisabled: {
    color: '#f87171',
    fontSize: 11,
  },
  badgeTextRunning: {
    color: '#fbbf24',
    fontSize: 11,
  },
  link: {
    color: '#94a3b8',
    fontSize: 13,
    textDecorationLine: 'underline',
  },
  linkDisabled: {
    opacity: 0.4,
  },
  linkDanger: {
    color: '#f87171',
    fontSize: 12,
    textDecorationLine: 'underline',
  },
  linkSuccess: {
    color: '#34d399',
    fontSize: 12,
    textDecorationLine: 'underline',
  },
  empty: {
    color: '#94a3b8',
    fontSize: 13,
    padding: 16,
    textAlign: 'center',
  },
  error: {
    color: '#f87171',
    fontSize: 13,
  },
  meta: {
    color: '#64748b',
    fontSize: 11,
  },
  pageRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 8,
  },
  input: {
    borderWidth: 1,
    borderColor: '#334155',
    borderRadius: 8,
    paddingHorizontal: 10,
    paddingVertical: 8,
    color: '#f1f5f9',
    fontSize: 13,
  },
  button: {
    backgroundColor: '#f1f5f9',
    borderRadius: 8,
    paddingHorizontal: 14,
    paddingVertical: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  buttonText: {
    color: '#0f172a',
    fontWeight: '600',
    fontSize: 13,
  },
});
