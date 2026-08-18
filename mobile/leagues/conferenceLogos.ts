// Conference logo URLs, keyed by this app's canonical conference names
// (see api/internal/schedule/conferences.go's FBSConferences — every
// league.conference value is guaranteed to be one of these, enforced at
// league-creation time). There's no conference-level entity in this app's
// data model (leagues/teams just carry a plain `conference` text column,
// and CFBD — the schedule data source — has no logo field on its own
// /conferences endpoint), so these are sourced from ESPN's public,
// unauthenticated scoreboard API
// (site.api.espn.com/.../scoreboard/conferences), which does expose a
// stable per-conference logo CDN URL. Static/hardcoded rather than
// fetched at runtime: conference branding doesn't change, and this avoids
// a second external dependency in the request path for something this
// cosmetic. Mirrors web/src/leagues/conferenceLogos.ts exactly.
export const CONFERENCE_LOGOS: Record<string, string> = {
  ACC: 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/acc.png',
  'Big 12': 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/big_12.png',
  'Big Ten': 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/big_ten.png',
  SEC: 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/sec.png',
  'American Athletic Conference': 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/american.png',
  'Conference USA': 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/conference_usa.png',
  'Mid-American Conference': 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/mid_american.png',
  'Mountain West Conference': 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/mountain_west.png',
  'Pac-12': 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/pac_12.png',
  'Sun Belt Conference': 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/sun_belt.png',
  'FBS Independents': 'https://a.espncdn.com/i/teamlogos/ncaa_conf/500/fbs_independents.png',
};

export function getConferenceLogoUrl(conference: string): string | undefined {
  return CONFERENCE_LOGOS[conference];
}
