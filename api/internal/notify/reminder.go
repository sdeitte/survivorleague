package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// ReminderWindow24h and ReminderWindow3h are the two pick_reminder windows
// per the plan's Notifications section ("24h/3h before lock, no pick
// yet"). Each has its own dedupe key component (see EnqueuePickReminder)
// so an hourly scan firing both checks every tick still only ever enqueues
// each window once per membership/week.
const (
	ReminderWindow24h = 24 * time.Hour
	ReminderWindow3h  = 3 * time.Hour
)

// ScanPickReminders implements the plan's "hourly reminder scan for
// members without a pick inside ~24h/~3h of their nearest lock": for every
// active, playing, non-removed member of an active league, finds their
// nearest not-yet-kicked-off conference-relevant game in a week they
// haven't picked for yet (GetNearestUnpickedGameForMembership), and — if
// that deadline falls inside either window — calls EnqueuePickReminder.
// Both windows are checked independently (not else-if): a membership
// whose deadline is 2 hours out is inside BOTH the 24h and 3h windows,
// and per the plan should get (at most) one reminder for each, not just
// the tighter one. Safe to call every hour without over-notifying: the
// dedupe key does that work, not this scan's own logic.
//
// Wired into cmd/server/main.go's cron scheduler on an hourly schedule,
// alongside the Phase 3 twice-daily schedule-sync cron.
func (s *Service) ScanPickReminders(ctx context.Context) error {
	candidates, err := s.queries.ListActiveContestantMembershipsForReminderScan(ctx)
	if err != nil {
		return fmt.Errorf("notify: list reminder-scan candidates: %w", err)
	}

	now := time.Now()
	for _, m := range candidates {
		nearest, err := s.queries.GetNearestUnpickedGameForMembership(ctx, gen.GetNearestUnpickedGameForMembershipParams{
			Conference:   m.Conference,
			SeasonYear:   m.SeasonYear,
			MembershipID: m.MembershipID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue // already picked everything upcoming — nothing to remind.
			}
			log.Printf("notify: reminder scan: get nearest unpicked game for membership %s: %v", db.UUIDString(m.MembershipID), err)
			continue
		}

		untilKickoff := nearest.KickoffAt.Time.Sub(now)
		if untilKickoff <= 0 {
			continue // already kicked off — nothing left to warn about for this game.
		}

		if untilKickoff <= ReminderWindow24h {
			if err := s.EnqueuePickReminder(ctx, m.MembershipID, m.LeagueID, nearest.WeekID, "24h"); err != nil {
				log.Printf("notify: reminder scan: enqueue 24h reminder for membership %s: %v", db.UUIDString(m.MembershipID), err)
			}
		}
		if untilKickoff <= ReminderWindow3h {
			if err := s.EnqueuePickReminder(ctx, m.MembershipID, m.LeagueID, nearest.WeekID, "3h"); err != nil {
				log.Printf("notify: reminder scan: enqueue 3h reminder for membership %s: %v", db.UUIDString(m.MembershipID), err)
			}
		}
	}
	return nil
}
