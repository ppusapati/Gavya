package service

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/signedurl"

	"github.com/ppusapati/gavya/services/file-service/internal/config"
	"github.com/ppusapati/gavya/services/file-service/internal/repository"
	"p9e.in/samavaya/packages/p9log"
)

// Clock is the current time, so a test can decide when a link expires.
type Clock interface{ Now() time.Time }

type Service struct {
	repo repository.Repository
	cfg  *config.Config
	log  *p9log.Helper

	clock Clock
	// keys signs and checks download links. Nil when the deployment set no
	// key, in which case GetDownloadURL refuses with the setting named rather
	// than handing out a link nothing will honour.
	keys     *signedurl.Keyring
	linkBase string
	linkFor  time.Duration
}

func New(repo repository.Repository, cfg *config.Config, log *p9log.Helper) *Service {
	return &Service{repo: repo, cfg: cfg, log: log}
}

// WithDownloads gives this service what it needs to issue links.
func (s *Service) WithDownloads(keys *signedurl.Keyring, base string, lifetime time.Duration, clock Clock) *Service {
	s.keys, s.linkBase, s.linkFor, s.clock = keys, base, lifetime, clock
	return s
}

// SigningKeys is what the download route checks a link against.
func (s *Service) SigningKeys() *signedurl.Keyring { return s.keys }

// Log is this service's logger, for the download route.
func (s *Service) Log() *p9log.Helper { return s.log }

func (s *Service) now() time.Time {
	if s.clock == nil {
		return time.Now()
	}
	return s.clock.Now()
}
