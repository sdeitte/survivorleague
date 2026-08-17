package notify

import (
	"context"
	"fmt"
	"sync"
)

// fakePushSender is the test double for PushSender used throughout this
// package's tests (and, via internal/grading and internal/leagues'
// Notifier-satisfying *Service, indirectly by their own tests too) — no
// live Expo Push call is ever made in a test, per the plan's "all testing
// must use a fake/mock implementation" instruction. Safe for concurrent
// use (the concurrency test drives DispatchBatch from multiple
// goroutines).
type fakePushSender struct {
	mu        sync.Mutex
	sent      []PushMessage
	failCount int   // number of upcoming Send calls that should fail
	failErr   error // error to return while failCount > 0
}

func (f *fakePushSender) Send(_ context.Context, msg PushMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCount > 0 {
		f.failCount--
		if f.failErr != nil {
			return f.failErr
		}
		return fmt.Errorf("fakePushSender: forced failure")
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakePushSender) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakePushSender) messages() []PushMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]PushMessage, len(f.sent))
	copy(out, f.sent)
	return out
}

// fakeEmailSender is the EmailSender test double — same role as
// fakePushSender above.
type fakeEmailSender struct {
	mu        sync.Mutex
	sent      []EmailMessage
	failCount int
	failErr   error
}

func (f *fakeEmailSender) Send(_ context.Context, msg EmailMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCount > 0 {
		f.failCount--
		if f.failErr != nil {
			return f.failErr
		}
		return fmt.Errorf("fakeEmailSender: forced failure")
	}
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeEmailSender) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeEmailSender) messages() []EmailMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]EmailMessage, len(f.sent))
	copy(out, f.sent)
	return out
}
