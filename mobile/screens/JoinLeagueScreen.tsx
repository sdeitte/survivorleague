import { useState } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { Controller, useForm } from 'react-hook-form';
import { ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import * as api from '../api';
import { ApiError, type InvitePreviewResponse } from '../api';
import { joinCodeSchema, type JoinCodeFormValues } from '../leagues/schemas';

// Enter a code -> preview the league it invites to -> confirm -> join ->
// navigate to the league. Mirrors web/src/routes/JoinLeaguePage.tsx.
export function JoinLeagueScreen({
  onJoined,
  onCancel,
}: {
  onJoined: (leagueId: string) => void;
  onCancel: () => void;
}) {
  const { authFetch } = useAuth();
  const [preview, setPreview] = useState<(InvitePreviewResponse & { code: string }) | null>(null);
  const [serverError, setServerError] = useState<string | null>(null);
  const [isJoining, setIsJoining] = useState(false);

  const {
    control,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<JoinCodeFormValues>({ resolver: zodResolver(joinCodeSchema) });

  const onPreview = async (values: JoinCodeFormValues) => {
    setServerError(null);
    try {
      const result = await api.previewInvite(values.code);
      setPreview({ ...result, code: values.code });
    } catch (err) {
      setServerError(
        err instanceof ApiError && err.status === 404
          ? 'No league found for that invite code.'
          : err instanceof ApiError
            ? err.message
            : 'Failed to look up invite code. Please try again.',
      );
    }
  };

  const onConfirmJoin = async () => {
    if (!preview) return;
    setServerError(null);
    setIsJoining(true);
    try {
      const league = await authFetch((token) => api.joinLeagueByCode(token, preview.code));
      onJoined(league.id);
    } catch (err) {
      setServerError(
        err instanceof ApiError && err.status === 409
          ? "You're already a member of this league."
          : err instanceof ApiError
            ? err.message
            : 'Failed to join league. Please try again.',
      );
    } finally {
      setIsJoining(false);
    }
  };

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Join a league</Text>
      <Text style={styles.subtitle}>Enter the invite code your commissioner shared.</Text>

      {!preview ? (
        <>
          <View style={styles.field}>
            <Text style={styles.label}>Invite code</Text>
            <Controller
              control={control}
              name="code"
              render={({ field: { onChange, onBlur, value } }) => (
                <TextInput
                  style={[styles.input, styles.codeInput]}
                  autoCapitalize="characters"
                  autoCorrect={false}
                  onBlur={onBlur}
                  onChangeText={onChange}
                  value={value ?? ''}
                />
              )}
            />
            {errors.code && <Text style={styles.error}>{errors.code.message}</Text>}
          </View>

          {serverError && <Text style={styles.error}>{serverError}</Text>}

          <Pressable
            style={[styles.button, isSubmitting && styles.buttonDisabled]}
            onPress={handleSubmit(onPreview)}
            disabled={isSubmitting}
          >
            {isSubmitting ? <ActivityIndicator color="#0f172a" /> : <Text style={styles.buttonText}>Look up code</Text>}
          </Pressable>
        </>
      ) : (
        <>
          <View style={styles.card}>
            <Text style={styles.cardTitle}>{preview.league_name}</Text>
            <Text style={styles.cardMeta}>
              {preview.conference} · {preview.season_year}
            </Text>
          </View>

          {serverError && <Text style={styles.error}>{serverError}</Text>}

          <View style={styles.row}>
            <Pressable
              style={[styles.buttonOutline, styles.rowButton]}
              onPress={() => {
                setPreview(null);
                setServerError(null);
              }}
            >
              <Text style={styles.buttonOutlineText}>Back</Text>
            </Pressable>
            <Pressable
              style={[styles.button, styles.rowButton, isJoining && styles.buttonDisabled]}
              onPress={() => void onConfirmJoin()}
              disabled={isJoining}
            >
              {isJoining ? <ActivityIndicator color="#0f172a" /> : <Text style={styles.buttonText}>Join league</Text>}
            </Pressable>
          </View>
        </>
      )}

      <Pressable onPress={onCancel}>
        <Text style={styles.link}>Cancel</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0f172a',
    justifyContent: 'center',
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
  input: {
    backgroundColor: '#1e293b',
    borderRadius: 8,
    paddingVertical: 10,
    paddingHorizontal: 12,
    color: '#f1f5f9',
  },
  codeInput: {
    fontSize: 20,
    letterSpacing: 4,
    textAlign: 'center',
  },
  card: {
    backgroundColor: '#1e293b',
    borderRadius: 10,
    padding: 16,
    gap: 4,
  },
  cardTitle: {
    color: '#f1f5f9',
    fontSize: 16,
    fontWeight: '600',
  },
  cardMeta: {
    color: '#94a3b8',
    fontSize: 13,
  },
  row: {
    flexDirection: 'row',
    gap: 8,
  },
  rowButton: {
    flex: 1,
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
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  buttonText: {
    color: '#0f172a',
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
  link: {
    color: '#94a3b8',
    fontSize: 13,
    textAlign: 'center',
    marginTop: 12,
    textDecorationLine: 'underline',
  },
});
