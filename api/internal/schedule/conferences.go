package schedule

// FBSConferences is the canonical list of FBS conference names a league's
// `conference` field is validated against at creation time (see
// internal/leagues and POST /leagues).
//
// This is hardcoded rather than sourced from `teams.conference` (as the
// plan's original API-surface note for GET /conferences suggested) because
// Phase 3 (CFBD schedule sync, which populates `teams`) hasn't landed yet —
// league creation in Phase 2 must not depend on data that doesn't exist.
// These names are stable identifiers independent of which teams currently
// belong to them; team-to-conference mappings come later from CFBD and
// don't affect this list.
//
// When Phase 3's CFBD sync lands, it should normalize incoming
// `teams.conference` strings to match this canonical set exactly (CFBD's
// own naming/casing may differ, e.g. abbreviations or trailing
// "Conference" variants) so pick-eligibility filtering (which joins
// leagues.conference against teams.conference) works correctly.
var FBSConferences = []string{
	"ACC",
	"Big 12",
	"Big Ten",
	"SEC",
	"American Athletic Conference",
	"Conference USA",
	"Mid-American Conference",
	"Mountain West Conference",
	"Pac-12",
	"Sun Belt Conference",
	"FBS Independents",
}

// IsValidConference reports whether name is an exact, case-sensitive match
// for one of the canonical FBS conference names above.
func IsValidConference(name string) bool {
	for _, c := range FBSConferences {
		if c == name {
			return true
		}
	}
	return false
}

// cfbdConferenceNormalization maps CFBD's raw `conference` strings (as
// returned by GET /teams/fbs and GET /games) to the canonical FBSConferences
// names above. CFBD does NOT use the "...Conference" suffix the canonical
// list uses for four of its entries — confirmed against two independent
// sources (no live API key was available to confirm against an authenticated
// JSON payload directly, so this was cross-checked rather than taken from a
// single source):
//
//  1. collegefootballdata.com/teams — the official site's own conference
//     filter list, which reads directly off this same `conference` field.
//  2. cfbfastR's (the widely-used CFBD R client) published sample output
//     tables at https://cfbfastr.sportsdataverse.org/articles/cfbd_teams.html,
//     which show CFBD's raw values verbatim.
//
// Both sources independently show: "ACC", "American Athletic", "Big 12",
// "Big Ten", "Conference USA", "FBS Independents", "Mid-American",
// "Mountain West", "Pac-12", "SEC", "Sun Belt" — i.e. only "Conference USA"
// and "FBS Independents" already match the canonical list verbatim; the
// other four canonical entries that end in "...Conference" (American
// Athletic Conference, Mid-American Conference, Mountain West Conference,
// Sun Belt Conference) need this map. ACC/Big 12/Big Ten/SEC/Pac-12 pass
// through unchanged either way (kept in the map for clarity/completeness).
//
// A raw CFBD name with no entry here is NOT silently dropped: it's stored
// as-is by NormalizeConference (see below) so the sync never crashes or
// loses a team, but SyncResult surfaces it as an "unmapped conference" so a
// real CFBD sync (once a live API key exists) can be checked against this
// table immediately and the map corrected if CFBD has since renamed
// anything.
var cfbdConferenceNormalization = map[string]string{
	"ACC":               "ACC",
	"American Athletic": "American Athletic Conference",
	"Big 12":            "Big 12",
	"Big Ten":           "Big Ten",
	"Conference USA":    "Conference USA",
	"FBS Independents":  "FBS Independents",
	"Mid-American":      "Mid-American Conference",
	"Mountain West":     "Mountain West Conference",
	"Pac-12":            "Pac-12",
	"SEC":               "SEC",
	"Sun Belt":          "Sun Belt Conference",
}

// NormalizeConference maps a raw CFBD conference string to the canonical
// FBSConferences name via cfbdConferenceNormalization. If raw has no
// mapping entry, it's returned unchanged (not dropped) and ok is false —
// callers should still store the team (never silently lose sync data) but
// surface the miss to an operator, since it means CFBD's naming has drifted
// from this table and pick-eligibility filtering (leagues.conference joined
// against teams.conference) will silently fail to match for that
// conference until it's fixed.
func NormalizeConference(raw string) (normalized string, ok bool) {
	if mapped, found := cfbdConferenceNormalization[raw]; found {
		return mapped, true
	}
	return raw, false
}
