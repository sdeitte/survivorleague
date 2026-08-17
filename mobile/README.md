# mobile

Expo (managed workflow) React Native + TypeScript app. Phase 0 scaffold: a
single screen that fetches `GET /health` from the API to prove the pipeline
works end to end. Phases 1-7 add real feature screens (auth, leagues,
picks, leaderboard, notification preferences) — see the roadmap in
`/Users/sdeitte/.claude/plans/witty-questing-barto.md`.

## Push notifications (Phase 7) — open item

`notifications.ts` registers the device for Expo push on login
(`registerForPushNotificationsAsync`, called from `auth/AuthContext.tsx`)
and POSTs the resulting token to `/me/device-tokens`. This is real,
type-checked registration code matching the backend's documented contract,
but it **cannot be exercised end-to-end in this environment**:

- No EAS project exists yet (`app.json` has no `extra.eas.projectId`) —
  `expo-notifications`' `getExpoPushTokenAsync` needs one to mint a real
  token. Run `eas init` (and `eas build`/`eas submit` per Expo's docs) to
  set this up.
- No physical iOS/Android device or APNs/FCM credentials are available
  here to actually receive a push.

`registerForPushNotificationsAsync` fails soft (returns `null`, logs a
warning) in both cases, so login/register are unaffected — this is the
same "flagged but unavailable" treatment as `CFBD_API_KEY` in Phase 3 and
`RESEND_API_KEY` in Phase 7's backend.

## Setup

```sh
npm install
cp .env.example .env   # adjust EXPO_PUBLIC_API_BASE_URL if needed
npm run start           # then press i / a / w, or scan the QR with Expo Go
```

By default the app points at `http://localhost:8080` (the `api/` service run
locally). If you're testing on a physical device, `localhost` won't resolve
to your dev machine — set `EXPO_PUBLIC_API_BASE_URL` in `.env` to your
machine's LAN IP instead (e.g. `http://192.168.1.23:8080`).
