import { useState } from 'react';
import { zodResolver } from '@hookform/resolvers/zod';
import { Controller, useForm } from 'react-hook-form';
import { ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import { BrandWordmark } from '../components/BrandWordmark';
import { loginSchema, type LoginFormValues } from '../auth/schemas';
import { ApiError } from '../api';

export function LoginScreen({
  onNavigateToRegister,
  onNavigateToForgotPassword,
}: {
  onNavigateToRegister: () => void;
  onNavigateToForgotPassword: () => void;
}) {
  const { login } = useAuth();
  const [serverError, setServerError] = useState<string | null>(null);

  const {
    control,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginFormValues>({ resolver: zodResolver(loginSchema) });

  const onSubmit = async (values: LoginFormValues) => {
    setServerError(null);
    try {
      await login(values.email, values.password);
    } catch (err) {
      setServerError(err instanceof ApiError ? err.message : 'Failed to log in. Please try again.');
    }
  };

  return (
    <View style={styles.container}>
      <View style={styles.brandRow}>
        <BrandWordmark size={260} />
      </View>

      <Text style={styles.title}>Log in</Text>
      <Text style={styles.subtitle}>Survivor League</Text>

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

      <View style={styles.field}>
        <Text style={styles.label}>Password</Text>
        <Controller
          control={control}
          name="password"
          render={({ field: { onChange, onBlur, value } }) => (
            <TextInput
              style={styles.input}
              secureTextEntry
              autoComplete="current-password"
              onBlur={onBlur}
              onChangeText={onChange}
              value={value ?? ''}
            />
          )}
        />
        {errors.password && <Text style={styles.error}>{errors.password.message}</Text>}
      </View>

      {serverError && <Text style={styles.error}>{serverError}</Text>}

      <Pressable
        style={[styles.button, isSubmitting && styles.buttonDisabled]}
        onPress={handleSubmit(onSubmit)}
        disabled={isSubmitting}
      >
        {isSubmitting ? <ActivityIndicator color="#0f172a" /> : <Text style={styles.buttonText}>Log in</Text>}
      </Pressable>

      <Pressable onPress={onNavigateToForgotPassword}>
        <Text style={styles.link}>Forgot password?</Text>
      </Pressable>

      <Pressable onPress={onNavigateToRegister}>
        <Text style={styles.link}>No account? Register</Text>
      </Pressable>
    </View>
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
