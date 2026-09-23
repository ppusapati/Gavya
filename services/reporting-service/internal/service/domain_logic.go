package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/signedurl"

	"github.com/ppusapati/gavya/services/reporting-service/internal/cron"
	"github.com/ppusapati/gavya/services/reporting-service/internal/domain"
	"github.com/ppusapati/gavya/services/reporting-service/internal/report"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "the report is not ready yet" from "the query failed", and would have to
// report both the same way.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

// RequestReport records a request, and now something acts on it.
//
// The type and the parameters are checked here rather than left for the runner
// to discover. Both readings are defensible and this one is better: a request
// refused at the counter tells the person standing there what to correct, and a
// request accepted and failed at two in the morning tells nobody anything until
// they come back to look.
//
// The runner checks them again anyway, because a row can be written by a
// schedule or by a hand on the database and it must not trust that this
// happened.
func (s *Service) RequestReport(ctx context.Context, tenantID, name, reportType, parameters, fileFormat, requestedBy, createdBy string) (*domain.Report, error) {
	kind, err := report.Lookup(reportType)
	if err != nil {
		return nil, invalid(err.Error())
	}
	params, err := report.ParseParams(parameters)
	if err != nil {
		return nil, invalid(err.Error())
	}
	for _, need := range kind.Needs {
		if strings.TrimSpace(params[need]) == "" {
			return nil, invalid(kind.Name + " needs " + need + ", and it is empty")
		}
	}
	if kind.Window {
		if _, err := report.ParseWindow(params, time.UTC); err != nil {
			return nil, invalid(err.Error())
		}
	}

	r := &domain.Report{
		ID:          ulidpkg.New().String(),
		TenantID:    tenantID,
		Name:        name,
		ReportType:  reportType,
		Parameters:  parameters,
		Status:      "pending",
		FileFormat:  fileFormat,
		RequestedBy: requestedBy,
		CreatedBy:   createdBy,
		UpdatedBy:   createdBy,
	}
	out, err := s.repo.CreateReport(ctx, r)
	if err != nil {
		return nil, err
	}
	// Produce it now rather than at the next tick. The request is already
	// committed, so a kick that is dropped costs a wait and nothing else.
	s.kick()
	return out, nil
}

func (s *Service) GetReport(ctx context.Context, id, tenantID string) (*domain.Report, error) {
	return s.repo.GetReport(ctx, id, tenantID)
}

func (s *Service) ListReports(ctx context.Context, tenantID string) ([]*domain.Report, error) {
	return s.repo.ListReports(ctx, tenantID)
}

func (s *Service) GetReportDownloadURL(ctx context.Context, id, tenantID string) (string, error) {
	rep, err := s.repo.GetReport(ctx, id, tenantID)
	if err != nil {
		return "", err
	}
	if rep.Status != "completed" {
		return "", invalid("this report is " + rep.Status + " and there is nothing to download yet")
	}
	if s.keys == nil {
		// No link can be signed, and one handed out unsigned would be a link
		// that fetches nothing. Named, so somebody reading this knows what to
		// set rather than that downloads are simply broken.
		return "", invalid("this deployment has no DOWNLOAD_SIGNING_KEY set, so it cannot " +
			"issue a download link; GetReportContent still returns the report itself")
	}

	lifetime := s.linkFor
	if lifetime <= 0 {
		lifetime = signedurl.DefaultLifetime
	}
	now := s.now()
	token, err := s.keys.Sign(signedurl.Grant{
		Purpose:  handlerPurpose,
		TenantID: tenantID,
		Resource: rep.ID,
		Expires:  now.Add(lifetime),
	}, now)
	if err != nil {
		return "", err
	}
	return signedurl.Link(s.linkBase, downloadPath, token), nil
}

// The path and purpose a report link carries.
//
// Duplicated from the handler package rather than imported, because the handler
// imports this one and Go will not have it both ways. Held to the handler's by
// a test there, so the two cannot drift into a link that points at a route
// nothing serves.
const (
	downloadPath   = "/download/report"
	handlerPurpose = "report"
)

// DownloadReport is what the signed-link route serves.
//
// Separate from ReportContent because the two are reached differently and it is
// worth being able to see which is which: this one is reached by a link with no
// session behind it, and the tenant it is given came out of a verified token.
func (s *Service) DownloadReport(ctx context.Context, id, tenantID string) (*domain.Report, error) {
	rep, err := s.repo.ReportContent(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if rep.Status != "completed" || len(rep.Content) == 0 {
		return nil, invalid("this report has produced nothing")
	}
	return rep, nil
}

// CreateSchedule writes a schedule the runner can actually fire.
//
// Everything a firing needs is checked now: that the type exists, that it is
// one a schedule can ask for, that the expression parses, that the zone is
// real, and that the window is one this platform resolves. Each of those is a
// thing that would otherwise be discovered at two in the morning by a sweep
// with nobody watching, and the symptom of every one of them is the same —
// a report that does not arrive.
//
// The timezone is required and has no default. Seven in the morning is seven
// where the society is; choosing UTC here would be this platform deciding what
// time a co-operative starts work, quietly and differently from what they
// typed.
func (s *Service) CreateSchedule(ctx context.Context, tenantID, reportType, schedule, parameters, timezone, createdBy string) (*domain.ReportSchedule, error) {
	kind, err := report.Lookup(reportType)
	if err != nil {
		return nil, invalid(err.Error())
	}
	if !kind.Schedulable {
		return nil, invalid(reportType + " names something a schedule cannot supply, so it can " +
			"only be asked for one report at a time: " + kind.Summary)
	}

	zone := strings.TrimSpace(timezone)
	if zone == "" {
		return nil, invalid("a schedule needs a timezone, because a time of day means nothing " +
			"without one; set it to the society's own zone, such as Asia/Kolkata")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, invalid("timezone " + zone + " is not one this platform knows: " + err.Error())
	}

	expr, err := cron.Parse(schedule)
	if err != nil {
		return nil, invalid(err.Error())
	}

	params, err := report.ParseParams(parameters)
	if err != nil {
		return nil, invalid(err.Error())
	}
	if _, err := report.ResolveWindow(params["window"], s.now(), loc); err != nil {
		return nil, invalid(err.Error())
	}

	// The first firing is computed here rather than left for the sweep to
	// adopt, so the row says when it will next run from the moment it is
	// written. A schedule whose next run is a blank is one nobody can check.
	next, err := expr.Next(s.now(), loc)
	if err != nil {
		return nil, invalid(err.Error())
	}

	sched := &domain.ReportSchedule{
		ID:         ulidpkg.New().String(),
		TenantID:   tenantID,
		ReportType: reportType,
		Schedule:   schedule,
		Parameters: parameters,
		Timezone:   zone,
		IsActive:   true,
		NextRunAt:  &next,
		CreatedBy:  createdBy,
		UpdatedBy:  createdBy,
	}
	return s.repo.CreateReportSchedule(ctx, sched)
}

// now is the current time, through the service's clock so a test can fix it.
func (s *Service) now() time.Time {
	if s.clock == nil {
		return time.Now()
	}
	return s.clock.Now()
}

// ReportContent hands over the bytes a run produced.
//
// A report that has not been produced is refused with the state it is actually
// in, rather than with an empty body: "not ready" and "produced nothing" are
// different answers and only one of them means somebody should wait.
func (s *Service) ReportContent(ctx context.Context, id, tenantID string) (*domain.Report, error) {
	rep, err := s.repo.ReportContent(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	switch rep.Status {
	case "completed":
		return rep, nil
	case "failed":
		reason := rep.FailureReason
		if reason == "" {
			reason = "no reason was recorded"
		}
		return nil, invalid("this report failed and has no content: " + reason)
	default:
		return nil, invalid("this report is " + rep.Status + " and has not produced anything yet")
	}
}

// Catalogue is what this platform can produce.
func (s *Service) Catalogue() []report.Kind { return report.Catalogue() }

func (s *Service) ListSchedules(ctx context.Context, tenantID string) ([]*domain.ReportSchedule, error) {
	return s.repo.ListReportSchedules(ctx, tenantID)
}

func (s *Service) UpdateSchedule(ctx context.Context, id, tenantID string, isActive bool, updatedBy string) (*domain.ReportSchedule, error) {
	return s.repo.UpdateScheduleActive(ctx, id, tenantID, isActive, updatedBy)
}

func (s *Service) DeleteSchedule(ctx context.Context, id, tenantID, updatedBy string) error {
	return s.repo.SoftDeleteSchedule(ctx, id, tenantID, updatedBy)
}
