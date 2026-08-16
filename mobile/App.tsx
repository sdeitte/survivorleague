import { useEffect, useState } from 'react';
import { StatusBar } from 'expo-status-bar';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider, useAuth } from './auth/AuthContext';
import { LoginScreen } from './screens/LoginScreen';
import { RegisterScreen } from './screens/RegisterScreen';
import { HomeScreen } from './screens/HomeScreen';
import { HealthScreen } from './screens/HealthScreen';

const queryClient = new QueryClient();

// Phase 1 is small enough (4 screens, no deep linking needs yet) that a
// hand-rolled state machine is simpler than pulling in React Navigation —
// revisit once the picks/leagues screens in later phases need real nested
// navigation.
type Screen = 'login' | 'register' | 'home' | 'health';

function Root() {
  const { user, isLoading } = useAuth();
  const [screen, setScreen] = useState<Screen>('login');

  // Reset to a sane default screen on sign-in/sign-out so e.g. logging out
  // from the health screen doesn't strand the next session there.
  useEffect(() => {
    setScreen(user ? 'home' : 'login');
  }, [user]);

  if (isLoading) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator color="#f1f5f9" />
      </View>
    );
  }

  if (!user) {
    return screen === 'register' ? (
      <RegisterScreen onNavigateToLogin={() => setScreen('login')} />
    ) : (
      <LoginScreen onNavigateToRegister={() => setScreen('register')} />
    );
  }

  if (screen === 'health') {
    return <HealthScreen onBack={() => setScreen('home')} />;
  }

  return <HomeScreen onNavigateToHealth={() => setScreen('health')} />;
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <Root />
        <StatusBar style="auto" />
      </AuthProvider>
    </QueryClientProvider>
  );
}

const styles = StyleSheet.create({
  loading: {
    flex: 1,
    backgroundColor: '#0f172a',
    alignItems: 'center',
    justifyContent: 'center',
  },
});
