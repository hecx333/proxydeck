package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDoRetriesUntilSuccess(t *testing.T) {
	attempts := 0
	err := Do(context.Background(), Config{MaxAttempts: 3, BaseBackoff: time.Millisecond}, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestDoStopsOnStopError(t *testing.T) {
	attempts := 0
	err := Do(context.Background(), Config{MaxAttempts: 5, BaseBackoff: time.Millisecond}, func(context.Context) error {
		attempts++
		return StopError{Err: errors.New("bad request")}
	})
	if err == nil || err.Error() != "bad request" {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
