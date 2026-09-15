package notify

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/observe"
)

// Watching the outbox.
//
// The alert rules named this as a blind spot and could do nothing about it,
// because there was no metric and no way to publish one. The failure it guards
// is quiet and expensive: a payable is held, the message is queued in the same
// transaction, and the sweep that delivers it stops — a bad NOTIFICATION_URL, a
// notification-service that has been down since Friday, a panic in the
// dispatcher. Every money event goes on being recorded correctly and nobody is
// told about any of them. Nothing in the platform looks wrong.
//
// Two numbers, because one of them does not say enough on its own.
//
//   - Depth is how many messages are owed. It is normal for this to be
//     non-zero: a sweep runs on an interval and a burst of approvals fills it
//     between sweeps.
//   - Age is how long the oldest has been waiting. This is the one that means
//     something. A deep outbox that is draining is a busy fortnight; an outbox
//     whose oldest message is an hour old is a sweep that has stopped, whatever
//     its depth.

// PublishOutboxDepth makes the outbox watchable.
//
// The count is read on a schedule rather than inside the scrape, and that is
// deliberate. observe calls a gauge's function while serving /metrics, so a
// query there puts the database on the path of every scrape: a slow one makes
// every scrape slow, and a database that is down makes the metrics endpoint fail
// — losing the request counters too, at exactly the moment somebody needs them
// to find out what is wrong.
//
// So the gauges read a number that a ticker refreshes, and one that cannot be
// refreshed keeps its last value rather than reporting zero. Zero is the same
// number as "the outbox is empty", which is the reassuring answer and the wrong
// one.
func PublishOutboxDepth(ctx context.Context, pool *pgxpool.Pool, every time.Duration) {
	if pool == nil {
		return
	}
	if every <= 0 {
		every = 15 * time.Second
	}

	var depth, oldest atomic.Int64
	// -1 until the first reading. A gauge that reports 0 before it has ever
	// looked says the outbox is empty, which is a claim nobody made.
	depth.Store(-1)
	oldest.Store(-1)

	observe.Publish(observe.Gauge{
		Name: "gavya_notification_outbox_depth",
		Help: "Messages owed to somebody and not yet delivered. -1 before the first reading.",
		Read: func() float64 { return float64(depth.Load()) },
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_notification_outbox_oldest_seconds",
		Help: "How long the oldest undelivered message has been waiting. -1 before the first reading.",
		Read: func() float64 { return float64(oldest.Load()) },
	})

	read := func() {
		// Across every tenant, like the sweep itself: this is the platform's
		// own bookkeeping rather than an answer to a tenant's question, and a
		// per-tenant count would be one series per society.
		q, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		var n int64
		var seconds *float64
		err := pool.QueryRow(q, `
			SELECT count(*),
			       EXTRACT(EPOCH FROM (NOW() - min(created_at)))
			  FROM notification_outbox
			 WHERE delivered_at IS NULL`).Scan(&n, &seconds)
		if err != nil {
			// Keep the last reading. The alert on staleness is what catches a
			// reader that has stopped; overwriting with a zero here would hide
			// it behind a healthy-looking number.
			return
		}
		depth.Store(n)
		if seconds == nil {
			oldest.Store(0)
			return
		}
		oldest.Store(int64(*seconds))
	}

	read()
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				read()
			}
		}
	}()
}
