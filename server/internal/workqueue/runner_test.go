package workqueue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunnerDedupesConcurrentJobs(t *testing.T) {
	r := New()
	var runs int32
	done := make(chan struct{})

	started1 := r.Run("k", func(ctx context.Context) error {
		atomic.AddInt32(&runs, 1)
		time.Sleep(50 * time.Millisecond)
		close(done)
		return nil
	})
	started2 := r.Run("k", func(ctx context.Context) error {
		atomic.AddInt32(&runs, 1)
		return nil
	})

	if !started1 {
		t.Fatal("first run should start")
	}
	if started2 {
		t.Fatal("second run should be deduped")
	}
	<-done
	if runs != 1 {
		t.Fatalf("runs = %d want 1", runs)
	}
}
