package notify

import (
	"context"
	"log"
	"time"
)

// DefaultDispatchInterval is how often the Dispatcher ticks in production —
// the plan's "10-30s range is fine" for the notification dispatcher,
// distinct from internal/livepoll's own 90s live-score poll interval.
const DefaultDispatchInterval = 20 * time.Second

// Dispatcher runs the background ticker loop that drains
// notification_outbox via repeated Service.DispatchBatch calls. Same
// Start/Stop ticker shape as internal/livepoll.Poller.
type Dispatcher struct {
	service   *Service
	interval  time.Duration
	batchSize int32

	cancel context.CancelFunc
	done   chan struct{}
}

// DispatcherOption configures a Dispatcher at construction time — only
// used to override production defaults in tests (a short interval so a
// test doesn't have to wait 20 real seconds for a tick).
type DispatcherOption func(*Dispatcher)

// WithDispatchInterval overrides DefaultDispatchInterval.
func WithDispatchInterval(d time.Duration) DispatcherOption {
	return func(disp *Dispatcher) { disp.interval = d }
}

// WithBatchSize overrides DefaultBatchSize.
func WithBatchSize(n int32) DispatcherOption {
	return func(disp *Dispatcher) { disp.batchSize = n }
}

// NewDispatcher constructs a Dispatcher. It does nothing until Start is
// called.
func NewDispatcher(service *Service, opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{
		service:   service,
		interval:  DefaultDispatchInterval,
		batchSize: DefaultBatchSize,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Start launches the background ticker goroutine. Call Stop to shut it
// down cleanly — Stop blocks until any in-flight tick finishes, so it's
// safe to call from cmd/server's shutdown sequence without racing a
// half-finished dispatch transaction.
func (d *Dispatcher) Start(ctx context.Context) {
	tickerCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	d.done = make(chan struct{})

	go func() {
		defer close(d.done)
		ticker := time.NewTicker(d.interval)
		defer ticker.Stop()
		for {
			select {
			case <-tickerCtx.Done():
				return
			case <-ticker.C:
				if _, err := d.service.DispatchBatch(tickerCtx, d.batchSize); err != nil {
					log.Printf("notify: dispatch tick error: %v", err)
				}
			}
		}
	}()
}

// Stop cancels the background ticker and waits for the current tick (if
// any) to finish. Safe to call even if Start was never called.
func (d *Dispatcher) Stop() {
	if d.cancel == nil {
		return
	}
	d.cancel()
	<-d.done
}
