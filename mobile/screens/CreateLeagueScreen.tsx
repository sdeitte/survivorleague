import { useState } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { useQuery } from '@tanstack/react-query';
import { Controller, useForm } from 'react-hook-form';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import * as api from '../api';
import { createLeagueSchema, type CreateLeagueFormValues } from '../leagues/schemas';
import { ApiError } from '../api';

export function CreateLeagueScreen({
  onCreated,
  onCancel,
}: {
  onCreated: (leagueId: string) => void;
  onCancel: () => void;
}) {
  const { authFetch } = useAuth();
  const [serverError, setServerError] = useState<string | null>(null);

  const { data: conferences, isLoading: conferencesLoading } = useQuery({
    queryKey: ['conferences'],
    queryFn: api.listConferences,
  });

  const {
    control,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<CreateLeagueFormValues>({
    resolver: zodResolver(createLeagueSchema),
    defaultValues: { name: '', season_year: new Date().getFullYear(), conference: '' },
  });

  const onSubmit = async (values: CreateLeagueFormValues) => {
    setServerError(null);
    try {
      const league = await authFetch((token) => api.createLeague(token, values));
      onCreated(league.id);
    } catch (err) {
      setServerError(err instanceof ApiError ? err.message : 'Failed to create league. Please try again.');
    }
  };

  return (
    <ScrollView contentContainerStyle={styles.container}>
      <Text style={styles.title}>Create a league</Text>
      <Text style={styles.subtitle}>You'll be its commissioner and a playing contestant.</Text>

      <View style={styles.field}>
        <Text style={styles.label}>League name</Text>
        <Controller
          control={control}
          name="name"
          render={({ field: { onChange, onBlur, value } }) => (
            <TextInput style={styles.input} onBlur={onBlur} onChangeText={onChange} value={value} />
          )}
        />
        {errors.name && <Text style={styles.error}>{errors.name.message}</Text>}
      </View>

      <View style={styles.field}>
        <Text style={styles.label}>Season year</Text>
        <Controller
          control={control}
          name="season_year"
          render={({ field: { onChange, onBlur, value } }) => (
            <TextInput
              style={styles.input}
              keyboardType="number-pad"
              onBlur={onBlur}
              onChangeText={(text) => onChange(Number(text) || 0)}
              value={String(value)}
            />
          )}
        />
        {errors.season_year && <Text style={styles.error}>{errors.season_year.message}</Text>}
      </View>

      <View style={styles.field}>
        <Text style={styles.label}>Conference</Text>
        <Text style={styles.hint}>Locked for the league's lifetime once created.</Text>
        {conferencesLoading && <ActivityIndicator color="#f1f5f9" />}
        <Controller
          control={control}
          name="conference"
          render={({ field: { onChange, value } }) => (
            <View style={styles.conferenceList}>
              {conferences?.map((c) => (
                <Pressable
                  key={c}
                  onPress={() => onChange(c)}
                  style={[styles.conferenceOption, value === c && styles.conferenceOptionSelected]}
                >
                  <Text style={[styles.conferenceOptionText, value === c && styles.conferenceOptionTextSelected]}>
                    {c}
                  </Text>
                </Pressable>
              ))}
            </View>
          )}
        />
        {errors.conference && <Text style={styles.error}>{errors.conference.message}</Text>}
      </View>

      {serverError && <Text style={styles.error}>{serverError}</Text>}

      <Pressable
        style={[styles.button, isSubmitting && styles.buttonDisabled]}
        onPress={handleSubmit(onSubmit)}
        disabled={isSubmitting}
      >
        {isSubmitting ? <ActivityIndicator color="#0f172a" /> : <Text style={styles.buttonText}>Create league</Text>}
      </Pressable>

      <Pressable onPress={onCancel}>
        <Text style={styles.link}>Cancel</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: {
    flexGrow: 1,
    backgroundColor: '#0f172a',
    padding: 24,
    gap: 12,
  },
  title: {
    color: '#f1f5f9',
    fontSize: 22,
    fontWeight: '600',
  },
  subtitle: {
    color: '#94a3b8',
    fontSize: 13,
    marginBottom: 12,
  },
  field: {
    gap: 4,
  },
  label: {
    color: '#cbd5e1',
    fontSize: 13,
  },
  hint: {
    color: '#64748b',
    fontSize: 11,
  },
  input: {
    backgroundColor: '#1e293b',
    borderRadius: 8,
    paddingVertical: 10,
    paddingHorizontal: 12,
    color: '#f1f5f9',
  },
  conferenceList: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
    marginTop: 4,
  },
  conferenceOption: {
    borderRadius: 999,
    paddingVertical: 8,
    paddingHorizontal: 12,
    borderWidth: 1,
    borderColor: '#334155',
  },
  conferenceOptionSelected: {
    backgroundColor: '#f1f5f9',
    borderColor: '#f1f5f9',
  },
  conferenceOptionText: {
    color: '#cbd5e1',
    fontSize: 13,
  },
  conferenceOptionTextSelected: {
    color: '#0f172a',
    fontWeight: '600',
  },
  error: {
    color: '#f87171',
    fontSize: 12,
  },
  button: {
    backgroundColor: '#f1f5f9',
    borderRadius: 8,
    paddingVertical: 12,
    alignItems: 'center',
    marginTop: 8,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  buttonText: {
    color: '#0f172a',
    fontWeight: '600',
  },
  link: {
    color: '#94a3b8',
    fontSize: 13,
    textAlign: 'center',
    marginTop: 12,
    textDecorationLine: 'underline',
  },
});
