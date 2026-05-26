package retry

import (
	"context"
	"time"
)

type Config struct {
	MaxAttempts int
	BaseBackoff time.Duration
}

type StopError struct {
	Err error
}

func (e StopError) Error() string {
	if e.Err == nil {
		return "retry stopped"
	}
	return e.Err.Error()
}

func DefaultConfig() Config {
	return Config{
		MaxAttempts: 3,
		BaseBackoff: 500 * time.Millisecond,
	}
}

func Do(ctx context.Context, cfg Config, fn func(context.Context) error) error {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 100 * time.Millisecond
	}
	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(ctx); err == nil {
			return nil
		} else {
			var stopErr StopError
			if ok := AsStop(err, &stopErr); ok {
				return stopErr.Err
			}
			lastErr = err
		}
		if attempt == cfg.MaxAttempts {
			break
		}
		timer := time.NewTimer(cfg.BaseBackoff * time.Duration(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func AsStop(err error, target *StopError) bool {
	if err == nil {
		return false
	}
	value, ok := err.(StopError)
	if !ok {
		return false
	}
	if target != nil {
		*target = value
	}
	return true
}
