package probe

import (
	"context"
	"errors"
	"time"
)

var Unacknowledged = errors.New("unobservable: request not acknowledged; effect must still be observed")

// AwaitChange sends at most one request, then observes on later run-loop ticks.
func AwaitChange(ctx context.Context, issue func() error, observe func() (bool, error), ticks <-chan time.Time) (bool, error) {
	err := issue()
	ack := err == nil
	if err != nil && !errors.Is(err, Unacknowledged) {
		return ack, err
	}
	for {
		select {
		case <-ctx.Done():
			return ack, ctx.Err()
		case <-ticks:
			observed, err := observe()
			if err != nil {
				return ack, err
			}
			if observed {
				return ack, nil
			}
		}
	}
}
