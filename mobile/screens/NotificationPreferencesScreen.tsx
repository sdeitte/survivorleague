import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Switch, Text, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import { BrandWordmark } from '../components/BrandWordmark';
import * as api from '../api';
import { ApiError, type NotificationPreferences } from '../api';

// Mirrors web's NotificationPreferencesPage — a simple toggle list per
// type plus per channel. `survived` is the plan's one opt-out-by-default-
// ON type (push-only), but every toggle here works the same way.
const TYPE_TOGGLES: { key: keyof NotificationPreferences; label: string; description: string }[] = [
  { key: 'pick_reminder', label: 'Pick reminders', description: "Nudges when you haven't picked yet, ~24h and ~3h before your deadline." },
  { key: 'eliminated', label: 'Eliminated', description: "When you're eliminated from a league." },
  { key: 'survived', label: 'Survived', description: 'When your pick holds up for the week (push only).' },
  { key: 'mass_wipeout', label: 'Mass wipeout', description: 'When everyone in a league loses and nobody is eliminated that week.' },
  { key: 'buyback', label: 'Buy-back', description: 'When a commissioner reinstates you after an elimination.' },
];

const CHANNEL_TOGGLES: { key: keyof NotificationPreferences; label: string; description: string }[] = [
  { key: 'push_enabled', label: 'Push notifications', description: 'Delivered to this device.' },
  { key: 'email_enabled', label: 'Email', description: 'Delivered to your account email.' },
];

export function NotificationPreferencesScreen({ onBack }: { onBack: () => void }) {
  const { authFetch } = useAuth();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<NotificationPreferences | null>(null);

  const prefsQuery = useQuery({
    queryKey: ['notification-preferences'],
    queryFn: () => authFetch((token) => api.getNotificationPreferences(token)),
  });

  useEffect(() => {
    if (prefsQuery.data && !draft) setDraft(prefsQuery.data);
  }, [prefsQuery.data, draft]);

  const saveMutation = useMutation({
    mutationFn: (prefs: NotificationPreferences) => authFetch((token) => api.updateNotificationPreferences(token, prefs)),
    onSuccess: (prefs) => {
      queryClient.setQueryData(['notification-preferences'], prefs);
      setDraft(prefs);
    },
  });

  const toggle = (key: keyof NotificationPreferences) => {
    if (!draft) return;
    setDraft({ ...draft, [key]: !draft[key] });
  };

  return (
    <ScrollView style={styles.container} contentContainerStyle={styles.content}>
      <View style={styles.brandRow}>
        <BrandWordmark size={90} />
      </View>

      <Pressable onPress={onBack}>
        <Text style={styles.backLink}>← Back</Text>
      </Pressable>
      <Text style={styles.title}>Notification preferences</Text>
      <Text style={styles.subtitle}>Choose what you get notified about, and how.</Text>

      {prefsQuery.isLoading && <ActivityIndicator color="#f1f5f9" />}
      {prefsQuery.error && (
        <Text style={styles.error}>
          {prefsQuery.error instanceof ApiError ? prefsQuery.error.message : 'Could not load your preferences.'}
        </Text>
      )}

      {draft && (
        <>
          <View style={styles.card}>
            {TYPE_TOGGLES.map(({ key, label, description }) => (
              <View key={key} style={styles.row}>
                <View style={styles.rowText}>
                  <Text style={styles.rowLabel}>{label}</Text>
                  <Text style={styles.rowDescription}>{description}</Text>
                </View>
                <Switch value={draft[key]} onValueChange={() => toggle(key)} />
              </View>
            ))}
          </View>

          <View style={styles.card}>
            {CHANNEL_TOGGLES.map(({ key, label, description }) => (
              <View key={key} style={styles.row}>
                <View style={styles.rowText}>
                  <Text style={styles.rowLabel}>{label}</Text>
                  <Text style={styles.rowDescription}>{description}</Text>
                </View>
                <Switch value={draft[key]} onValueChange={() => toggle(key)} />
              </View>
            ))}
          </View>

          {saveMutation.error && (
            <Text style={styles.error}>
              {saveMutation.error instanceof ApiError ? saveMutation.error.message : 'Failed to save preferences.'}
            </Text>
          )}

          <Pressable
            style={[styles.saveButton, saveMutation.isPending && styles.saveButtonDisabled]}
            disabled={saveMutation.isPending}
            onPress={() => saveMutation.mutate(draft)}
          >
            <Text style={styles.saveButtonText}>{saveMutation.isPending ? 'Saving…' : 'Save preferences'}</Text>
          </Pressable>
          {saveMutation.isSuccess && <Text style={styles.savedText}>Saved.</Text>}
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
  },
  subtitle: {
    color: '#94a3b8',
    fontSize: 13,
    marginBottom: 8,
  },
  card: {
    backgroundColor: '#1e293b',
    borderRadius: 12,
    padding: 4,
    marginBottom: 12,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: 12,
    gap: 12,
  },
  rowText: {
    flex: 1,
  },
  rowLabel: {
    color: '#f1f5f9',
    fontSize: 14,
  },
  rowDescription: {
    color: '#64748b',
    fontSize: 12,
    marginTop: 2,
  },
  saveButton: {
    backgroundColor: '#059669',
    borderRadius: 10,
    paddingVertical: 12,
    alignItems: 'center',
  },
  saveButtonDisabled: {
    opacity: 0.5,
  },
  saveButtonText: {
    color: '#ffffff',
    fontSize: 14,
    fontWeight: '600',
  },
  savedText: {
    color: '#34d399',
    fontSize: 12,
    textAlign: 'center',
  },
  error: {
    color: '#f87171',
    fontSize: 13,
  },
});
