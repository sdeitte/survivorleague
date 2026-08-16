// Thin fetch wrapper around the Go API for the mobile client.
//
// Unlike the web client, there is no cookie jar here: both the access
// token and the refresh token are read from / written to expo-secure-store
// (see auth/AuthContext.tsx), and every refresh call sends refresh_token
// explicitly in the JSON body per the API contract.
//
// EXPO_PUBLIC_-prefixed env vars are inlined by Expo at build/start time —
// see https://docs.expo.dev/guides/environment-variables/. Set
// EXPO_PUBLIC_API_BASE_URL in a .env file (see .env.example) to point at a
// non-default API host (e.g. a physical device on the same network).
export const API_BASE_URL: string =
  process.env.EXPO_PUBLIC_API_BASE_URL ?? 'http://localhost:8080';

export interface HealthResponse {
  status: 'ok' | 'error';
  db: 'ok' | 'error';
  error?: string;
}

export interface User {
  id: string;
  email: string;
  display_name: string;
  is_site_admin: boolean;
}

export interface SessionResponse {
  access_token: string;
  refresh_token?: string;
  user: User;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  accessToken?: string | null;
}

async function rawFetch(path: string, opts: RequestOptions = {}): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
  if (opts.accessToken) headers['Authorization'] = `Bearer ${opts.accessToken}`;

  return fetch(`${API_BASE_URL}${path}`, {
    method: opts.method ?? 'GET',
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
}

async function parseJsonOrThrow<T>(res: Response): Promise<T> {
  const text = await res.text();
  const body: unknown = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    const message =
      body && typeof body === 'object' && 'error' in body
        ? String((body as { error: unknown }).error)
        : `request failed with status ${res.status}`;
    throw new ApiError(res.status, message);
  }
  return body as T;
}

export async function fetchHealth(): Promise<HealthResponse> {
  const res = await rawFetch('/health');
  const body = (await res.json()) as HealthResponse;
  if (!res.ok && !body.status) {
    throw new Error(`health check failed with status ${res.status}`);
  }
  return body;
}

export async function register(input: {
  email: string;
  password: string;
  display_name: string;
}): Promise<SessionResponse> {
  const res = await rawFetch('/auth/register', { method: 'POST', body: input });
  return parseJsonOrThrow<SessionResponse>(res);
}

export async function login(input: { email: string; password: string }): Promise<SessionResponse> {
  const res = await rawFetch('/auth/login', { method: 'POST', body: input });
  return parseJsonOrThrow<SessionResponse>(res);
}

export async function refresh(refreshToken: string): Promise<SessionResponse> {
  const res = await rawFetch('/auth/refresh', {
    method: 'POST',
    body: { refresh_token: refreshToken },
  });
  return parseJsonOrThrow<SessionResponse>(res);
}

export async function logout(refreshToken: string | null): Promise<void> {
  await rawFetch('/auth/logout', {
    method: 'POST',
    body: { refresh_token: refreshToken ?? '' },
  });
}

export async function getMe(accessToken: string): Promise<User> {
  const res = await rawFetch('/me', { accessToken });
  return parseJsonOrThrow<User>(res);
}

export async function updateMe(accessToken: string, input: { display_name: string }): Promise<User> {
  const res = await rawFetch('/me', { method: 'PATCH', body: input, accessToken });
  return parseJsonOrThrow<User>(res);
}
