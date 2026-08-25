import { Link } from 'react-router-dom'

// Required by both the App Store and Play Store as a hosted, public URL
// before a store listing can be submitted — see the mobile deployment
// plan's Part B step 5. Content reflects what the app actually collects
// (internal/auth, internal/notify) as of when this was written: email,
// password (hashed, never stored in plain text), display name, league/
// pick data, and — only if the user opts in — an Expo push token tied to
// their device. No analytics or ad SDKs are integrated.
export function PrivacyPolicyPage() {
  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-2xl mx-auto space-y-6 py-8">
        <div>
          <Link to="/" className="text-xs text-slate-500 underline">
            ← Survivor League
          </Link>
          <h1 className="text-2xl font-semibold mt-2">Privacy Policy</h1>
          <p className="text-sm text-slate-500 mt-1">Last updated August 2026</p>
        </div>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">What we collect</h2>
          <p>When you create an account, we collect your email address, a player name you choose, and your password (stored as a salted hash — we never store or have access to your plain-text password). If you join a league, we also store the team name you choose for it.</p>
          <p>When you use the app, we store the leagues you join, the weekly picks you make, and league membership/results data needed to run the game.</p>
          <p>If you enable push notifications, we store a device push token (issued by Apple/Google via Expo's push service) so we can send you pick reminders and league updates. You can disable this at any time in Notification Preferences.</p>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">How we use it</h2>
          <p>Your data is used solely to run the app: authenticating you, tracking your picks and league standings, and sending you transactional email (password reset, email verification, league invites) or push notifications you've opted into.</p>
          <p>We do not sell your data, show ads, or use any third-party analytics or advertising SDKs.</p>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">Third parties we use</h2>
          <ul className="list-disc list-inside space-y-1">
            <li>Resend — delivers our transactional email (verification, password reset, league invites).</li>
            <li>Expo — delivers push notifications to your device, if you opt in.</li>
            <li>CollegeFootballData.com — the source of team/schedule/score data shown in the app. No personal data is sent to or received from this service.</li>
          </ul>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">Data retention and deletion</h2>
          <p>We keep your account and league data for as long as your account is active. To request deletion of your account and associated data, email us at the address below — we'll confirm once it's done.</p>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">Children's privacy</h2>
          <p>Survivor League is not directed at children under 13, and we do not knowingly collect data from anyone under 13.</p>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">Contact</h2>
          <p>
            Questions about this policy or your data:{' '}
            <a href="mailto:admin@survivorleague.football" className="underline text-slate-200">
              admin@survivorleague.football
            </a>
          </p>
        </section>
      </div>
    </main>
  )
}
