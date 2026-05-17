package app

import "context"

// callCtx runs fn in a goroutine and returns when fn finishes or ctx is cancelled.
// On cancellation the underlying fn may still run until process exit; callers use
// this so tickers and Run() can return without waiting on slow network/SQLite work.
func callCtx(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
