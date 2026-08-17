import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import * as SecureStore from 'expo-secure-store';
import * as api from '../api';
import type { User } from '../api';
import { registerForPushNotificationsAsync } from '../notifications';

const ACCESS_TOKEN_KEY = 'survivor_league_access_token';
const REFRESH_TOKEN_KEY = 'survivor_league_refresh_token';

interface AuthContextValue {
  user: User | null;
  /** True while the app-launch session bootstrap (see below) is in flight. */
  isLoading: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, displayName: string) => Promise<void>;
  logout: () => Promise<void>;
  /** Re-fetches GET /me (refreshing once on 401) and updates `user`. */
  refreshProfile: () => Promise<void>;
  /**
   * Calls fn with a valid access token, transparently refreshing once (via
   * the refresh token in secure storage) on a 401 before giving up and
   * clearing the session. Exposed so any screen can make an authenticated
   * API call (leagues, invites, ...) without re-implementing the
   * refresh-on-401 dance that api.ts's mobile client (no cookie jar)
   * requires — same role as web's apiFetch wrapper.
   */
  authFetch: <T>(fn: (token: string) => Promise<T>) => Promise<T>;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  // In-memory mirror of the access token so calls within a render cycle
  // don't need to await a SecureStore read; SecureStore (both tokens) is
  // the durable source of truth that survives app relaunch.
  const accessTokenRef = useRef<string | null>(null);

  const persistSession = useCallback(async (session: api.SessionResponse) => {
    accessTokenRef.current = session.access_token;
    setUser(session.user);
    await SecureStore.setItemAsync(ACCESS_TOKEN_KEY, session.access_token);
    if (session.refresh_token) {
      await SecureStore.setItemAsync(REFRESH_TOKEN_KEY, session.refresh_token);
    }
  }, []);

  const clearSession = useCallback(async () => {
    accessTokenRef.current = null;
    setUser(null);
    await SecureStore.deleteItemAsync(ACCESS_TOKEN_KEY);
    await SecureStore.deleteItemAsync(REFRESH_TOKEN_KEY);
  }, []);

  // Calls fn with a valid access token, transparently refreshing once (via
  // the refresh token in secure storage) on a 401 before giving up and
  // clearing the session.
  const callWithRefresh = useCallback(
    async <T,>(fn: (token: string) => Promise<T>): Promise<T> => {
      const token = accessTokenRef.current ?? (await SecureStore.getItemAsync(ACCESS_TOKEN_KEY));
      if (!token) throw new api.ApiError(401, 'not authenticated');

      try {
        return await fn(token);
      } catch (err) {
        if (!(err instanceof api.ApiError) || err.status !== 401) throw err;

        const storedRefresh = await SecureStore.getItemAsync(REFRESH_TOKEN_KEY);
        if (!storedRefresh) {
          await clearSession();
          throw err;
        }

        try {
          const session = await api.refresh(storedRefresh);
          await persistSession(session);
          return await fn(session.access_token);
        } catch (refreshErr) {
          await clearSession();
          throw refreshErr;
        }
      }
    },
    [clearSession, persistSession],
  );

  const refreshProfile = useCallback(async () => {
    const me = await callWithRefresh((token) => api.getMe(token));
    setUser(me);
  }, [callWithRefresh]);

  // On app launch: check secure storage for a token; if present, validate
  // it with GET /me, refreshing once on 401 before falling back to the
  // login screen.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const storedAccess = await SecureStore.getItemAsync(ACCESS_TOKEN_KEY);
      if (!storedAccess) {
        if (!cancelled) setIsLoading(false);
        return;
      }
      accessTokenRef.current = storedAccess;
      try {
        await refreshProfile();
      } catch {
        // refreshProfile/callWithRefresh already cleared storage+state.
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // Intentionally run once on mount only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Fire-and-forget: push registration must never block or fail a login/
  // register flow (no EAS project / no physical device in this
  // environment are both expected, non-fatal outcomes — see
  // notifications.ts's doc comment). Runs after the session is already
  // persisted so a failure here can't leave the user half-signed-in.
  const registerPushTokenInBackground = useCallback((accessToken: string) => {
    void (async () => {
      try {
        const registration = await registerForPushNotificationsAsync();
        if (!registration) return;
        await api.registerDeviceToken(accessToken, {
          platform: registration.platform,
          expo_push_token: registration.token,
        });
      } catch (err) {
        console.warn('registerPushTokenInBackground: failed to register device token:', err);
      }
    })();
  }, []);

  const login = useCallback(
    async (email: string, password: string) => {
      const session = await api.login({ email, password });
      await persistSession(session);
      registerPushTokenInBackground(session.access_token);
    },
    [persistSession, registerPushTokenInBackground],
  );

  const registerFn = useCallback(
    async (email: string, password: string, displayName: string) => {
      const session = await api.register({ email, password, display_name: displayName });
      await persistSession(session);
      registerPushTokenInBackground(session.access_token);
    },
    [persistSession, registerPushTokenInBackground],
  );

  const logout = useCallback(async () => {
    const storedRefresh = await SecureStore.getItemAsync(REFRESH_TOKEN_KEY);
    try {
      await api.logout(storedRefresh);
    } finally {
      await clearSession();
    }
  }, [clearSession]);

  return (
    <AuthContext.Provider
      value={{ user, isLoading, login, register: registerFn, logout, refreshProfile, authFetch: callWithRefresh }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider');
  return ctx;
}
