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
