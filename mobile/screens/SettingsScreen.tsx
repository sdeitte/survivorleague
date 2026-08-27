import { useEffect, useState } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation } from '@tanstack/react-query';
import { Controller, useForm } from 'react-hook-form';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useAuth } from '../auth/AuthContext';
import { BrandWordmark } from '../components/BrandWordmark';
import * as api from '../api';
import { ApiError } from '../api';
import { settingsSchema, type SettingsFormValues } from '../auth/schemas';

// Account-level settings — mirrors web's SettingsPage. Currently just the
// player name (see PATCH /me), which has always been supported
// server-side but had no mobile entry point until now. Team names are
// per-league and edited from each league's own page instead (the
// one-time backfill prompt on LeagueDetailScreen).
export function SettingsScreen({ onBack }: { onBack: () => void }) {
  const { user, authFetch, refreshProfile } = useAuth();
  const [savedMessage, setSavedMessage] = useState<string | null>(null);

  const {
    control,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<SettingsFormValues>({ resolver: zodResolver(settingsSchema), defaultValues: { display_name: '' } });

  useEffect(() => {
    if (user) reset({ display_name: user.display_name });
  }, [user, reset]);

  const saveMutation = useMutation({
    mutationFn: (values: SettingsFormValues) => authFetch((token) => api.updateMe(token, values)),
    onSuccess: async () => {
      await refreshProfile();
      setSavedMessage('Saved.');
      setTimeout(() => setSavedMessage(null), 2000);
    },
  });

  return (
    <SafeAreaView style={styles.container} edges={['top']}>
    <ScrollView contentContainerStyle={styles.content}>
      <View style={styles.brandRow}>
        <BrandWordmark size={90} />
      </View>

      <Pressable onPress={onBack}>
        <Text style={styles.backLink}>← Back</Text>
      </Pressable>
      <Text style={styles.title}>Settings</Text>
      <Text style={styles.subtitle}>Manage your account details.</Text>

      <View style={styles.card}>
        <View style={styles.field}>
          <Text style={styles.label}>Player name</Text>
          <Controller
            control={control}
            name="display_name"
            render={({ field: { onChange, onBlur, value } }) => (
              <TextInput style={styles.input} onBlur={onBlur} onChangeText={onChange} value={value} />
            )}
          />
          {errors.display_name && <Text style={styles.error}>{errors.display_name.message}</Text>}
        </View>

        {saveMutation.error && (
          <Text style={styles.error}>
            {saveMutation.error instanceof ApiError ? saveMutation.error.message : 'Failed to save.'}
          </Text>
        )}

        <View style={styles.rowBetween}>
          <Pressable
            style={[styles.button, (isSubmitting || saveMutation.isPending) && styles.buttonDisabled]}
            onPress={handleSubmit((values) => saveMutation.mutate(values))}
            disabled={isSubmitting || saveMutation.isPending}
          >
            {saveMutation.isPending ? <ActivityIndicator color="#0f172a" /> : <Text style={styles.buttonText}>Save</Text>}
          </Pressable>
          {savedMessage && <Text style={styles.savedText}>{savedMessage}</Text>}
        </View>
      </View>
    </ScrollView>
    </SafeAreaView>
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
  brandRow: {
    alignItems: 'center',
    marginBottom: 12,
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
    marginBottom: 4,
  },
  card: {
    backgroundColor: '#1e293b',
    borderRadius: 12,
    padding: 16,
    gap: 12,
  },
  field: {
    gap: 4,
  },
  label: {
    color: '#cbd5e1',
    fontSize: 13,
  },
  input: {
    backgroundColor: '#0f172a',
    borderRadius: 8,
    paddingVertical: 10,
    paddingHorizontal: 12,
    color: '#f1f5f9',
  },
  error: {
    color: '#f87171',
    fontSize: 12,
  },
  rowBetween: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  button: {
    backgroundColor: '#f1f5f9',
    borderRadius: 8,
    paddingVertical: 10,
    paddingHorizontal: 20,
    alignItems: 'center',
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  buttonText: {
    color: '#0f172a',
    fontWeight: '600',
  },
  savedText: {
    color: '#34d399',
    fontSize: 12,
  },
});
