import { useEffect, useState } from 'react';
import { StatusBar } from 'expo-status-bar';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { useFonts, Creepster_400Regular } from '@expo-google-fonts/creepster';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider, useAuth } from './auth/AuthContext';
import { LoginScreen } from './screens/LoginScreen';
import { RegisterScreen } from './screens/RegisterScreen';
import { ForgotPasswordScreen } from './screens/ForgotPasswordScreen';
import { ResetPasswordScreen } from './screens/ResetPasswordScreen';
import { HealthScreen } from './screens/HealthScreen';
import { MyLeaguesScreen } from './screens/MyLeaguesScreen';
import { CreateLeagueScreen } from './screens/CreateLeagueScreen';
import { JoinLeagueScreen } from './screens/JoinLeagueScreen';
import { LeagueDetailScreen } from './screens/LeagueDetailScreen';
import { PicksScreen } from './screens/PicksScreen';
import { LeaderboardScreen } from './screens/LeaderboardScreen';
import { NotificationPreferencesScreen } from './screens/NotificationPreferencesScreen';
import { AdminScreen } from './screens/AdminScreen';
import { ErrorBoundary } from './components/ErrorBoundary';

const queryClient = new QueryClient();

// A hand-rolled state machine remains simpler than pulling in React
// Navigation for this screen count, even as Phase 2 adds four more
// screens on top of Phase 1's — 'leagueDetail' carries a selectedLeagueId
// alongside the screen tag since it's the one screen that needs a param.
type Screen =
  | { name: 'login' }
  | { name: 'register' }
  | { name: 'forgotPassword' }
  | { name: 'resetPassword' }
  | { name: 'health' }
  | { name: 'myLeagues' }
  | { name: 'createLeague' }
  | { name: 'joinLeague' }
  | { name: 'leagueDetail'; leagueId: string }
  | { name: 'picks'; leagueId: string }
  | { name: 'leaderboard'; leagueId: string }
  | { name: 'notificationPreferences' }
  | { name: 'admin' };

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
    switch (screen.name) {
      case 'register':
        return <RegisterScreen onNavigateToLogin={() => setScreen({ name: 'login' })} />;
      case 'forgotPassword':
        return (
          <ForgotPasswordScreen
            onNavigateToResetPassword={() => setScreen({ name: 'resetPassword' })}
            onNavigateToLogin={() => setScreen({ name: 'login' })}
          />
        );
      case 'resetPassword':
        return (
          <ResetPasswordScreen
            onSucceeded={() => setScreen({ name: 'login' })}
            onNavigateToLogin={() => setScreen({ name: 'login' })}
          />
        );
      default:
        return (
          <LoginScreen
            onNavigateToRegister={() => setScreen({ name: 'register' })}
            onNavigateToForgotPassword={() => setScreen({ name: 'forgotPassword' })}
          />
        );
    }
  }

  switch (screen.name) {
    case 'health':
      return <HealthScreen onBack={() => setScreen({ name: 'myLeagues' })} />;
    case 'notificationPreferences':
      return <NotificationPreferencesScreen onBack={() => setScreen({ name: 'myLeagues' })} />;
    case 'admin':
      return <AdminScreen onBack={() => setScreen({ name: 'myLeagues' })} />;
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
      return (
        <LeagueDetailScreen
          leagueId={screen.leagueId}
          onBack={() => setScreen({ name: 'myLeagues' })}
          onNavigateToPicks={(leagueId) => setScreen({ name: 'picks', leagueId })}
          onNavigateToLeaderboard={(leagueId) => setScreen({ name: 'leaderboard', leagueId })}
        />
      );
    case 'picks':
      return (
        <PicksScreen leagueId={screen.leagueId} onBack={() => setScreen({ name: 'leagueDetail', leagueId: screen.leagueId })} />
      );
    case 'leaderboard':
      return (
        <LeaderboardScreen
          leagueId={screen.leagueId}
          onBack={() => setScreen({ name: 'leagueDetail', leagueId: screen.leagueId })}
        />
      );
    default:
      return (
        <MyLeaguesScreen
          onNavigateToCreate={() => setScreen({ name: 'createLeague' })}
          onNavigateToJoin={() => setScreen({ name: 'joinLeague' })}
          onNavigateToLeague={(leagueId) => setScreen({ name: 'leagueDetail', leagueId })}
          onNavigateToHealth={() => setScreen({ name: 'health' })}
          onNavigateToNotificationPreferences={() => setScreen({ name: 'notificationPreferences' })}
          onNavigateToAdmin={user.is_site_admin ? () => setScreen({ name: 'admin' }) : undefined}
        />
      );
  }
}

export default function App() {
  // Creepster backs the "Survivor"-style scary-letters brand wordmark (see
  // styles.titleScary in screens that use it) — gated behind useFonts since
  // React Native has no CSS @font-face fallback-while-loading like the web
  // app gets for free; fontError still lets the app proceed (falls back to
  // the system font rather than blocking the whole app on a font CDN blip).
  const [fontsLoaded, fontError] = useFonts({ Creepster_400Regular });

  if (!fontsLoaded && !fontError) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator color="#f1f5f9" />
      </View>
    );
  }

  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <Root />
          <StatusBar style="auto" />
        </AuthProvider>
      </QueryClientProvider>
    </ErrorBoundary>
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
