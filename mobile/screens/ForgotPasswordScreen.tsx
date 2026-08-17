import { useState } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { Controller, useForm } from 'react-hook-form';
import { ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import { forgotPasswordSchema, type ForgotPasswordFormValues } from '../auth/schemas';
import { forgotPassword, ApiError } from '../api';

// POST /auth/forgot-password always responds 202 with the same message
// whether or not the email matches an account (see api/internal/auth/
// password_reset.go) — so this screen always shows the same confirmation
// state on submit, never a per-email success/failure branch.
export function ForgotPasswordScreen({
  onNavigateToResetPassword,
  onNavigateToLogin,
}: {
  onNavigateToResetPassword: () => void;
  onNavigateToLogin: () => void;
}) {
  const [submitted, setSubmitted] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);

  const {
    control,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<ForgotPasswordFormValues>({ resolver: zodResolver(forgotPasswordSchema) });

  const onSubmit = async (values: ForgotPasswordFormValues) => {
    setServerError(null);
    try {
      await forgotPassword(values.email);
      setSubmitted(true);
    } catch (err) {
      setServerError(err instanceof ApiError ? err.message : 'Something went wrong. Please try again.');
    }
  };

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Forgot password</Text>
      <Text style={styles.subtitle}>Survivor League</Text>

      {submitted ? (
        <View style={{ gap: 12 }}>
          <Text style={styles.info}>
            If an account exists for that email, a password reset link has been sent. Check your inbox, then come
            back here to enter the reset token.
          </Text>
          <Pressable style={styles.button} onPress={onNavigateToResetPassword}>
            <Text style={styles.buttonText}>I have my reset token</Text>
          </Pressable>
        </View>
      ) : (
        <>
          <View style={styles.field}>
            <Text style={styles.label}>Email</Text>
            <Controller
              control={control}
              name="email"
              render={({ field: { onChange, onBlur, value } }) => (
                <TextInput
                  style={styles.input}
                  autoCapitalize="none"
                  autoComplete="email"
                  keyboardType="email-address"
                  onBlur={onBlur}
                  onChangeText={onChange}
                  value={value ?? ''}
                />
              )}
            />
            {errors.email && <Text style={styles.error}>{errors.email.message}</Text>}
          </View>

          {serverError && <Text style={styles.error}>{serverError}</Text>}

          <Pressable
            style={[styles.button, isSubmitting && styles.buttonDisabled]}
            onPress={handleSubmit(onSubmit)}
            disabled={isSubmitting}
          >
            {isSubmitting ? <ActivityIndicator color="#0f172a" /> : <Text style={styles.buttonText}>Send reset link</Text>}
          </Pressable>
        </>
      )}

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
  info: {
    color: '#cbd5e1',
    fontSize: 13,
    lineHeight: 18,
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
