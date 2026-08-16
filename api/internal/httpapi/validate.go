package httpapi

import (
	"regexp"
	"strings"
)

// A deliberately simple email shape check (not a full RFC 5322 validator —
// that's a rabbit hole with little practical payoff). Good enough to catch
// typos/garbage client-side and server-side; deliverability is ultimately
// proven by the user receiving mail, which is out of scope until Phase 7.
var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func validateEmail(email string) bool {
	email = strings.TrimSpace(email)
	return email != "" && emailRegex.MatchString(email)
}

const minPasswordLength = 8

func validatePassword(password string) bool {
	return len(password) >= minPasswordLength
}
