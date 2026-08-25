import { Link } from 'react-router-dom'
import { BrandWordmark } from '../components/BrandWordmark'

// Plain-language rules/FAQ — public (see App.tsx, same reasoning as
// PrivacyPolicyPage) since a prospective joiner previewing an invite
// link should be able to read this before ever creating an account.
// Content mirrors the actual enforced behavior as of when this was
// written (internal/grading, internal/leagues, internal/picks) — keep
// this in sync if any of those rules change.
export function RulesPage() {
  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-2xl mx-auto space-y-6 py-8">
        <div>
          <div className="flex justify-center text-lg mb-4">
            <BrandWordmark size={200} />
          </div>
          <Link to="/" className="text-xs text-slate-500 underline">
            ← Home
          </Link>
          <h1 className="text-2xl font-semibold mt-2">How to Play</h1>
          <p className="text-sm text-slate-500 mt-1">The rules, in plain language.</p>
        </div>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">The basics</h2>
          <p>
            Each week, pick one team from your league's conference to win their game. If your team wins, you
            survive to the next week. If your team loses, you're eliminated.
          </p>
          <p>
            You can only pick each team <span className="text-slate-100">once per season</span> — once you've used
            a team, it's off the board for every future week, win or lose.
          </p>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">Locks and deadlines</h2>
          <p>
            Your pick locks the moment that team's game kicks off — no changes after that, and you can't see who
            anyone else picked until their game has locked too.
          </p>
          <p>
            If you don't make a pick before every game in the week has kicked off, that counts as a loss for the
            week — same as picking a team that loses.
          </p>
          <p>
            A postponed or canceled game voids that pick — it doesn't count as a win or a loss, and nobody is
            eliminated because of it.
          </p>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">Mass wipeout</h2>
          <p>
            If every remaining player loses in the same week, nobody is eliminated that week — the league carries
            on with everyone who was still alive going in.
          </p>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">If more than one player survives to the end</h2>
          <p>
            There's no sudden-death tiebreaker. If the season ends with more than one player still alive, they're
            all declared co-champions.
          </p>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">Player names and team names</h2>
          <p>
            Your player name is who you are across every league you're in. Your team name is your squad's name
            just within one league — set it when you join or create a league, and change it anytime from that
            league's page.
          </p>
        </section>

        <section className="space-y-2 text-sm text-slate-300">
          <h2 className="text-base font-semibold text-slate-100">League chat</h2>
          <p>
            Every league has a chat for trash talk. Messages are visible for 7 days, and your commissioner can
            delete any message.
          </p>
        </section>
      </div>
    </main>
  )
}
