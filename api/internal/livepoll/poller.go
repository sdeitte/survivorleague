package livepoll

import (
	"context"
	"log"
	"time"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/grading"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

// DefaultInterval is how often the poll loop ticks in production — the
// plan's "every 60-120s" range, no need for a second interval tier.
const DefaultInterval = 90 * time.Second

// DefaultLiveWindow is how long after kickoff a game is still considered
// "possibly live" and worth polling for — the plan's fixed 4.5 hour
// window, comfortably longer than any regulation-length game.
const DefaultLiveWindow = 4*time.Hour + 30*time.Minute

// Poller runs the adaptive live-score poll loop described in the package
// doc comment.
type Poller struct {
	schedule *schedule.Service
	grading  *grading.Service

	interval   time.Duration
	liveWindow time.Duration

	cancel context.CancelFunc
	done   chan struct{}
}

// Option configures a Poller at construction time. Only used to override
// the production defaults in tests (a short interval so a test doesn't
// have to wait 90 real seconds for a tick).
type Option func(*Poller)

// WithInterval overrides DefaultInterval.
func WithInterval(d time.Duration) Option {
	return func(p *Poller) { p.interval = d }
}

// WithLiveWindow overrides DefaultLiveWindow.
func WithLiveWindow(d time.Duration) Option {
	return func(p *Poller) { p.liveWindow = d }
}

// NewPoller constructs a Poller. It does nothing until Start is called.
func NewPoller(scheduleService *schedule.Service, gradingService *grading.Service, opts ...Option) *Poller {
	p := &Poller{
		schedule:   scheduleService,
		grading:    gradingService,
		interval:   DefaultInterval,
		liveWindow: DefaultLiveWindow,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Start launches the background ticker goroutine. Call Stop to shut it
// down cleanly — Stop blocks until any in-flight tick finishes, so it's
// safe to call from cmd/server's shutdown sequence without racing a
// half-finished grading transaction.
func (p *Poller) Start(ctx context.Context) {
	tickerCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.done = make(chan struct{})

	go func() {
		defer close(p.done)
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-tickerCtx.Done():
				return
			case <-ticker.C:
				if err := p.tick(tickerCtx); err != nil {
					log.Printf("livepoll: tick error: %v", err)
				}
			}
		}
	}()
}

// Stop cancels the background ticker and waits for the current tick (if
// any) to finish. Safe to call even if Start was never called.
func (p *Poller) Stop() {
	if p.cancel == nil {
		return
	}
	p.cancel()
	<-p.done
}

// tick is one poll cycle: the cheap live-window check, and — only if that
// finds something — a week refresh followed by grading/finalization. This
// is the exact code path both the production ticker in Start and a test
// driving the real Start/Stop loop exercise; it is deliberately not part
// of the public API surface (tests should let the ticker fire it, not call
// it directly, per the plan's E2E verification note about proving the
// actual loop path).
func (p *Poller) tick(ctx context.Context) error {
	weeks, err := p.schedule.ListLiveWindowWeeks(ctx, time.Now(), p.liveWindow)
	if err != nil {
		return err
	}
	if len(weeks) == 0 {
		return nil // cheap no-op: no CFBD call this tick
	}

	for _, w := range weeks {
		result, err := p.schedule.RefreshWeek(ctx, int(w.SeasonYear), int(w.WeekNumber))
		if err != nil {
			log.Printf("livepoll: refresh week %d/%d: %v", w.SeasonYear, w.WeekNumber, err)
			continue
		}

		games, err := p.schedule.ListGamesByWeek(ctx, result.WeekID)
		if err != nil {
			log.Printf("livepoll: list games for week %s: %v", db.UUIDString(result.WeekID), err)
			continue
		}
		for _, g := range games {
			if g.Status != "final" {
				continue
			}
			if _, err := p.grading.GradeGame(ctx, g.ID); err != nil {
				log.Printf("livepoll: grade game %s: %v", db.UUIDString(g.ID), err)
			}
		}

		leagueIDs, err := p.grading.ListLeagueIDsForWeek(ctx, result.WeekID)
		if err != nil {
			log.Printf("livepoll: list leagues for week %s: %v", db.UUIDString(result.WeekID), err)
			continue
		}
		for _, leagueID := range leagueIDs {
			if _, err := p.grading.TryFinalizeLeagueWeek(ctx, leagueID, result.WeekID); err != nil {
				log.Printf("livepoll: finalize league %s week %s: %v", db.UUIDString(leagueID), db.UUIDString(result.WeekID), err)
			}
		}
	}
	return nil
}
