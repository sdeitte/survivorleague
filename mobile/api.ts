// Phase 0: a single hand-rolled helper for the /health smoke test. Once the
// OpenAPI spec grows in later phases, this is replaced by the generated
// client in packages/api-client.
//
// EXPO_PUBLIC_-prefixed env vars are inlined by Expo at build/start time —
// see https://docs.expo.dev/guides/environment-variables/. Set
// EXPO_PUBLIC_API_BASE_URL in a .env file (see .env.example) to point at a
// non-default API host (e.g. a physical device on the same network).
export const API_BASE_URL: string =
  process.env.EXPO_PUBLIC_API_BASE_URL ?? 'http://localhost:8080'

export interface HealthResponse {
  status: 'ok' | 'error'
  db: 'ok' | 'error'
  error?: string
}

export async function fetchHealth(): Promise<HealthResponse> {
  const res = await fetch(`${API_BASE_URL}/health`)
  const body = (await res.json()) as HealthResponse
  if (!res.ok && !body.status) {
    throw new Error(`health check failed with status ${res.status}`)
  }
  return body
}
