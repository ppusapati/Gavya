package service

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/signedurl"

	"github.com/ppusapati/gavya/services/reporting-service/internal/repository"
	"p9e.in/samavaya/packages/p9log"
)

// Clock is the current time.
//
// Injected rather than read from the package, because writing a schedule
// computes its first firing and a test that could not fix "now" would be a test
// of what the machine's clock happened to say.
type Clock interface{ Now() time.Time }

// Kicker asks the runner to sweep now.
//
// A report requested from the console is produced in about as long as it takes
// to render rather than within the sweep interval. Nil when no runner is wired
// — a request is still recorded, it simply waits for whichever process is
// sweeping.
type Kicker interface{ Kick() }

type Service struct {
	repo   repository.Repository
	log    *p9log.Helper
	clock  Clock
	kicker Kicker

	// keys signs and checks download links. Nil when the deployment set no
	// key, in which case GetReportDownloadURL refuses with the setting named
	// rather than handing out a link nothing will honour.
	keys *signedurl.Keyring
	// linkBase makes a link absolute. Empty gives a root-relative one, which
	// is right for a console already talking to this platform through a
	// gateway and wrong for a link somebody emails.
	linkBase string
	// linkFor is how long a link lasts.
	linkFor time.Duration
}

func New(repo repository.Repository, log *p9log.Helper) *Service {
	return &Service{repo: repo, log: log}
}

// WithClock fixes what this service thinks the time is.
func (s *Service) WithClock(c Clock) *Service {
	s.clock = c
	return s
}

// WithDownloads gives this service what it needs to issue links.
func (s *Service) WithDownloads(keys *signedurl.Keyring, base string, lifetime time.Duration) *Service {
	s.keys, s.linkBase, s.linkFor = keys, base, lifetime
	return s
}

// SigningKeys is what the download route checks a link against.
func (s *Service) SigningKeys() *signedurl.Keyring { return s.keys }

// Log is this service's logger, for the download route.
func (s *Service) Log() *p9log.Helper { return s.log }

// WithKicker gives this service a runner to nudge.
func (s *Service) WithKicker(k Kicker) *Service {
	s.kicker = k
	return s
}

func (s *Service) kick() {
	if s.kicker != nil {
		s.kicker.Kick()
	}
}
