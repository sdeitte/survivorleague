# App Store Connect listing — draft

Fill these in on the app's "App Information" and version pages in App
Store Connect. Everything below is grounded in what the app actually does
today — nothing aspirational.

## App name (30 char max)
Survivor League

## Subtitle (30 char max)
College football pick 'em

## Category
Primary: Sports
Secondary: (none needed)

## Promotional text (170 char max, editable without a new build)
Pick one team a week. Win, you survive. Lose or miss a pick, you're out.
Run it with friends, family, or coworkers — any FBS conference.

## Description (4000 char max)

Survivor League is a college football pick 'em pool you run with your
own group — no strangers, no house cut, just your league.

Each week, pick one FBS team to win. Win, and you survive to the next
week. Lose — or forget to pick — and you're eliminated. You can only
use each team once all season, so the easy picks run out fast. Whoever
survives the longest wins the league.

HOW IT WORKS
• Create a league for any FBS conference and invite people by code,
  link, or email
• Every member picks one team per week before kickoff
• Picks lock at kickoff — no changes once the game starts
• Games grade automatically from real results; standings update the
  moment a week finishes
• A one-time "buy-back" lets a commissioner reinstate someone who
  got eliminated, if your league plays that way

FOR COMMISSIONERS
• Invite by code or send email invites directly from the app
• Manage your roster — buy back an eliminated player, remove someone,
  or close the league for good once the season's decided
• Everything's visible on the leaderboard: who's alive, who's out, and
  when

FOR PLAYERS
• See the real matchups for your league's conference each week, with
  win probability and point spread to help you decide
• Track your own pick history and everyone else's once it's revealed
• League chat to talk trash without leaving the app
• Push notifications so you never miss a pick deadline

Survivor League is built for the group that already runs this pool by
hand — same rules, no spreadsheet.

## Keywords (100 char max, comma-separated, no spaces needed but helps readability)
survivor pool,pick em,college football,cfb,elimination pool,fantasy football,bracket,league

## Support URL
https://survivorleague.football/support   <!-- TODO: confirm this route exists, or use /privacy's domain with a real support page/email -->

## Marketing URL (optional)
https://survivorleague.football

## Privacy Policy URL
https://survivorleague.football/privacy

## Copyright
2026 Survivor League

<!-- Note: this field is just display text — Apple doesn't verify it
against your developer account. Since the account is enrolled as an
Individual (no LLC yet), the App Store product page will still show
"Provided by: [your legal name]" separately underneath the app name —
that's tied to the account type, not this field, and isn't something
this field can hide. Revisit once an LLC exists (planned for the
public launch). -->

## App Privacy questionnaire (data collection disclosure)
Based on what the app actually collects (see api/internal/auth, /me/device-tokens):

| Data type | Collected? | Linked to user? | Used for |
|---|---|---|---|
| Email address | Yes | Yes | Account creation, login, password reset |
| Name (display name) | Yes | Yes | Shown to other league members |
| Device ID / push token | Yes | Yes | Push notifications (pick reminders) |
| User content (chat messages) | Yes | Yes | League chat feature |

Not collected: precise location, contact list, browsing history,
financial info, health data, photos.

## Age rating
No objectionable content, no gambling (no real-money stakes are handled
by the app itself). Should qualify for 4+ on Apple's questionnaire —
answer "No" to all content categories; the one judgment call is
"Simulated Gambling" — this is a free-to-play pick'em with no in-app
wagering, so "No" is accurate, but flag it for your own read since
Apple is strict here.

## What's New (first release)
Welcome to Survivor League! Create or join a league, make your weekly
pick, and see how far you can survive.
