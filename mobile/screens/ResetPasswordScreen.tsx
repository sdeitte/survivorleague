import { useState } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { Controller, useForm } from 'react-hook-form';
import { ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import { resetPasswordSchema, type ResetPasswordFormValues } from '../auth/schemas';
import { resetPassword, ApiError } from '../api';

// There's no deep-linking infrastructure set up in this app yet (out of
// scope for this addition — see the plan), so unlike the web reset-password
// page (which reads the token from a URL query param), this screen asks the
// user to manually paste the token from their reset email.
export function ResetPasswordScreen({ onSucceeded, onNavigateToLogin }: { onSucceeded: () => void; onNavigateToLogin: () => void }) {
  const [serverError, setServerError] = useState<string | null>(null);

  const {
    control,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<ResetPasswordFormValues>({ resolver: zodResolver(resetPasswordSchema) });

  const onSubmit = async (values: ResetPasswordFormValues) => {
    setServerError(null);
    try {
      await resetPassword({ token: values.token.trim(), new_password: values.new_password });
      onSucceeded();
    } catch (err) {
      // Backend deliberately returns the same generic message whether the
      // token is malformed, expired, or already used.
      setServerError(err instanceof ApiError ? err.message : 'Something went wrong. Please try again.');
    }
  };

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Reset password</Text>
      <Text style={styles.subtitle}>Paste the reset token from your email, then choose a new password.</Text>

      <View style={styles.field}>
        <Text style={styles.label}>Reset token</Text>
        <Controller
          control={control}
          name="token"
          render={({ field: { onChange, onBlur, value } }) => (
            <TextInput
              style={styles.input}
              autoCapitalize="none"
              autoCorrect={false}
              multiline
              onBlur={onBlur}
              onChangeText={onChange}
              value={value ?? ''}
            />
          )}
        />
        {errors.token && <Text style={styles.error}>{errors.token.message}</Text>}
      </View>

      <View style={styles.field}>
        <Text style={styles.label}>New password</Text>
        <Controller
          control={control}
          name="new_password"
          render={({ field: { onChange, onBlur, value } }) => (
            <TextInput
              style={styles.input}
              secureTextEntry
              autoComplete="new-password"
              onBlur={onBlur}
              onChangeText={onChange}
              value={value ?? ''}
            />
          )}
        />
        {errors.new_password && <Text style={styles.error}>{errors.new_password.message}</Text>}
      </View>

      {serverError && <Text style={styles.error}>{serverError}</Text>}

      <Pressable
        style={[styles.button, isSubmitting && styles.buttonDisabled]}
        onPress={handleSubmit(onSubmit)}
        disabled={isSubmitting}
      >
        {isSubmitting ? <ActivityIndicator color="#0f172a" /> : <Text style={styles.buttonText}>Reset password</Text>}
      </Pressable>

      <Pressable onPress={onNavigateToLogin}>
        <Text style={styles.link}>Back to log in</Text>
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
