package schedule

import "testing"

func TestIsValidConference_AcceptsCanonicalNames(t *testing.T) {
	for _, c := range FBSConferences {
		if !IsValidConference(c) {
			t.Errorf("IsValidConference(%q) = false, want true", c)
		}
	}
}

func TestIsValidConference_CaseSensitive(t *testing.T) {
	if IsValidConference("sec") {
		t.Error(`IsValidConference("sec") = true, want false (case-sensitive match required)`)
	}
	if IsValidConference("BIG TEN") {
		t.Error(`IsValidConference("BIG TEN") = true, want false (case-sensitive match required)`)
	}
}

func TestIsValidConference_RejectsUnknown(t *testing.T) {
	if IsValidConference("Not A Real Conference") {
		t.Error("IsValidConference on a bogus name = true, want false")
	}
	if IsValidConference("") {
		t.Error("IsValidConference(\"\") = true, want false")
	}
}

func TestFBSConferences_NoDuplicates(t *testing.T) {
	seen := make(map[string]bool, len(FBSConferences))
	for _, c := range FBSConferences {
		if seen[c] {
			t.Errorf("duplicate conference name in FBSConferences: %q", c)
		}
		seen[c] = true
	}
}
