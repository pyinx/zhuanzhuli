package probe_test

import (
	"context"
	"errors"
	shared "focus.local/zhuanzhuli/pkg/observe"
	"testing"
	"time"
)

func TestUnacknowledgedRequestCanStillHaveDelayedEffect(t *testing.T) {
	ticks := make(chan time.Time, 3)
	for i := 0; i < 3; i++ {
		ticks <- time.Time{}
	}
	n := 0
	ack, err := shared.AwaitChange(context.Background(), func() error { return shared.Unacknowledged }, func() (bool, error) { n++; return n == 3, nil }, ticks)
	if err != nil || ack || n != 3 {
		t.Fatalf("discarded delayed observation: ack=%v err=%v n=%d", ack, err, n)
	}
}
func TestIdentityFailureNeverIssuesObservationOrRetry(t *testing.T) {
	n := 0
	_, err := shared.AwaitChange(context.Background(), func() error { return errors.New("identity-changed") }, func() (bool, error) { n++; return true, nil }, nil)
	if err == nil || n != 0 {
		t.Fatal("ignored identity error")
	}
}
func TestUnknownOutcomeStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := shared.AwaitChange(ctx, func() error { return nil }, func() (bool, error) { return false, nil }, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("unknown request falsely succeeded", err)
	}
}
