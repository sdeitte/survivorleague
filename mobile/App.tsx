import { useEffect, useState } from 'react';
import { StatusBar } from 'expo-status-bar';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider, useAuth } from './auth/AuthContext';
import { LoginScreen } from './screens/LoginScreen';
import { RegisterScreen } from './screens/RegisterScreen';
import { HealthScreen } from './screens/HealthScreen';
import { MyLeaguesScreen } from './screens/MyLeaguesScreen';
import { CreateLeagueScreen } from './screens/CreateLeagueScreen';
import { JoinLeagueScreen } from './screens/JoinLeagueScreen';
import { LeagueDetailScreen } from './screens/LeagueDetailScreen';

const queryClient = new QueryClient();

// A hand-rolled state machine remains simpler than pulling in React
// Navigation for this screen count, even as Phase 2 adds four more
// screens on top of Phase 1's — 'leagueDetail' carries a selectedLeagueId
// alongside the screen tag since it's the one screen that needs a param.
type Screen =
  | { name: 'login' }
  | { name: 'register' }
  | { name: 'health' }
  | { name: 'myLeagues' }
  | { name: 'createLeague' }
  | { name: 'joinLeague' }
  | { name: 'leagueDetail'; leagueId: string };

function Root() {
  const { user, isLoading } = useAuth();
  const [screen, setScreen] = useState<Screen>({ name: 'login' });

  // Reset to a sane default screen on sign-in/sign-out so e.g. logging out
  // from a league detail screen doesn't strand the next session there.
  useEffect(() => {
    setScreen(user ? { name: 'myLeagues' } : { name: 'login' });
  }, [user]);

  if (isLoading) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator color="#f1f5f9" />
      </View>
    );
  }

  if (!user) {
    return screen.name === 'register' ? (
      <RegisterScreen onNavigateToLogin={() => setScreen({ name: 'login' })} />
    ) : (
      <LoginScreen onNavigateToRegister={() => setScreen({ name: 'register' })} />
    );
  }

  switch (screen.name) {
    case 'health':
      return <HealthScreen onBack={() => setScreen({ name: 'myLeagues' })} />;
    case 'createLeague':
      return (
        <CreateLeagueScreen
          onCreated={(leagueId) => setScreen({ name: 'leagueDetail', leagueId })}
          onCancel={() => setScreen({ name: 'myLeagues' })}
        />
      );
    case 'joinLeague':
      return (
        <JoinLeagueScreen
          onJoined={(leagueId) => setScreen({ name: 'leagueDetail', leagueId })}
          onCancel={() => setScreen({ name: 'myLeagues' })}
        />
      );
    case 'leagueDetail':
      return <LeagueDetailScreen leagueId={screen.leagueId} onBack={() => setScreen({ name: 'myLeagues' })} />;
    default:
      return (
        <MyLeaguesScreen
          onNavigateToCreate={() => setScreen({ name: 'createLeague' })}
          onNavigateToJoin={() => setScreen({ name: 'joinLeague' })}
          onNavigateToLeague={(leagueId) => setScreen({ name: 'leagueDetail', leagueId })}
          onNavigateToHealth={() => setScreen({ name: 'health' })}
        />
      );
  }
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
