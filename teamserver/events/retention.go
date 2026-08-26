package events

import (
	"context"
	"time"

	"purpcmd/server/db"
)

const (
	DefaultRetentionCleanupInterval = time.Hour
	DefaultRetentionCleanupBatch    = 1000
)

// RunRetentionCleanup prunes expired events immediately and then at each
// interval until the context is cancelled. The report callback is optional
// and is called after every attempt so callers can surface cleanup failures.
func (bus *Bus) RunRetentionCleanup(
	ctx context.Context,
	interval time.Duration,
	batchSize int,
	report func(deleted int64, err error),
) {
	if interval <= 0 {
		interval = DefaultRetentionCleanupInterval
	}
	if batchSize <= 0 {
		batchSize = DefaultRetentionCleanupBatch
	}
	prune := func() {
		deleted, err := db.DBEventPruneExpired(time.Now(), batchSize)
		if report != nil {
			report(deleted, err)
		}
	}

	prune()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}
