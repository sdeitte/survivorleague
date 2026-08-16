# mobile

Expo (managed workflow) React Native + TypeScript app. Phase 0 scaffold: a
single screen that fetches `GET /health` from the API to prove the pipeline
works end to end. No feature UI yet — see the roadmap in
`/Users/sdeitte/.claude/plans/witty-questing-barto.md`.

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
