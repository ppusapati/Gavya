package service

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/observe"
)

// Watching the chain.
//
// The trail is hash-chained so that a row altered after the fact can be shown to
// have been altered. gavya_verify_audit_chain answers whether it still holds,
// and nothing ran it: a broken chain was found when somebody thought to ask,
// which in practice means during the audit it was supposed to survive.
//
// That is the wrong moment. The whole value of the chain is that it is evidence,
// and evidence nobody has checked since it was written is a claim rather than
// evidence. So it is verified on a schedule and the answer is published.
//
// Verified here rather than inside the scrape, and the distinction matters more
// for this than for anything else in the platform: walking the chain reads every
// row, so doing it per scrape would put a full table scan on a fifteen-second
// timer. The gauge reads a variable; the walk has its own clock.

// Interval is how often the chain is walked.
//
// An hour. The chain is append-only and enforced by a trigger, so a break is
// either a database somebody reached around the application to edit, or
// corruption — neither of which is a thing that happens between one minute and
// the next, and both of which want finding the same day.
const Interval = time.Hour

// WatchChain verifies the trail on a schedule and publishes what it found.
//
// tenants names which tenants to walk. The chain is per tenant, so there is no
// single answer: the gauge reports the worst one, because "some tenant's trail
// is broken" is the fact somebody needs and which tenant is the next question
// rather than the first.
func (s *Service) WatchChain(ctx context.Context, tenants func(context.Context) ([]string, error)) {
	var intact, checkedAt, broken atomic.Int64
	// -1 until the first walk. Zero would say "not intact", which is an
	// accusation nobody has made yet, and reporting it would page somebody for
	// a service that has merely just started.
	intact.Store(-1)
	broken.Store(0)

	observe.Publish(observe.Gauge{
		Name: "gavya_audit_chain_intact",
		Help: "1 when every tenant's hash chain verified at the last walk, 0 when one did not, -1 before the first.",
		Read: func() float64 { return float64(intact.Load()) },
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_audit_chain_broken_tenants",
		Help: "How many tenants' trails did not verify at the last walk.",
		Read: func() float64 { return float64(broken.Load()) },
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_audit_chain_checked_seconds_ago",
		Help: "How long since the chain was last walked. Rises without limit if the walk stops.",
		Read: func() float64 {
			at := checkedAt.Load()
			if at == 0 {
				return -1
			}
			return time.Since(time.Unix(at, 0)).Seconds()
		},
	})

	walk := func() {
		list, err := tenants(ctx)
		if err != nil {
			// Leave the last answer standing. The staleness gauge is what
			// reports a walk that has stopped; overwriting the verdict here
			// would turn "we could not look" into "it is broken", and a page
			// nobody can act on is one that gets silenced.
			s.log.Errorf("audit chain watch: could not list tenants: %v", err)
			return
		}
		var failed int64
		for _, tenant := range list {
			report, err := s.VerifyChain(ctx, tenant)
			if err != nil {
				s.log.Errorf("audit chain watch: %s: %v", tenant, err)
				continue
			}
			if !report.Intact {
				failed++
				// Loudly, and with the row named. A broken chain is the one
				// event in this platform that means somebody may have edited
				// the record of what was done.
				at := int64(-1)
				if report.BrokenAt != nil {
					at = *report.BrokenAt
				}
				s.log.Errorf("audit chain watch: %s DOES NOT VERIFY at row %d (%s): %s",
					tenant, at, report.BrokenID, report.Detail)
			}
		}
		broken.Store(failed)
		if failed == 0 {
			intact.Store(1)
		} else {
			intact.Store(0)
		}
		checkedAt.Store(time.Now().Unix())
	}

	go func() {
		walk()
		t := time.NewTicker(Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				walk()
			}
		}
	}()
}
