package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/origin"
	ulidpkg "p9e.in/samavaya/packages/ulid"

	"github.com/ppusapati/gavya/services/ingestion-service/internal/domain"
	"github.com/ppusapati/gavya/services/ingestion-service/internal/repository"
)

// DeliverInput is one record as a device delivered it.
type DeliverInput struct {
	TenantID          string
	DeviceID          string
	Generation        int64
	ExternalSessionID string
	Sequence          int64
	// Payload is the record exactly as received. It is hashed here rather than
	// trusting a hash the device supplies, because a device that miscomputes
	// its own hash would make a conflict look like a replay.
	Payload    []byte
	CapturedAt time.Time
	Actor      string
}

// Deliver admits, replays or quarantines one delivered record.
//
// This is the only entry point for field data, and it is safe to call any
// number of times with the same input: the second and later calls return the
// record the first admitted.
func (s *Service) Deliver(ctx context.Context, in DeliverInput) (*repository.IngestResult, error) {
	if err := validateDelivery(in); err != nil {
		return nil, err
	}

	res, err := s.repo.Ingest(ctx, repository.IngestInput{
		TenantID:          in.TenantID,
		DeviceID:          in.DeviceID,
		Generation:        in.Generation,
		ExternalSessionID: in.ExternalSessionID,
		Sequence:          in.Sequence,
		PayloadHash:       origin.HashPayload(in.Payload),
		Payload:           in.Payload,
		CapturedAt:        in.CapturedAt,
		Actor:             in.Actor,
		RecordID:          ulidpkg.New().String(),
		QuarantineID:      ulidpkg.New().String(),
	}, domain.Admit)
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	switch res.Decision.Outcome {
	case domain.OutcomeQuarantined:
		s.log.Warnf("quarantined %s: device=%s gen=%d session=%s seq=%d: %s",
			res.Decision.Reason, in.DeviceID, in.Generation, in.ExternalSessionID, in.Sequence, res.Decision.Detail)
	case domain.OutcomeDuplicateReplay:
		s.log.Debugf("replay ignored: device=%s gen=%d session=%s seq=%d",
			in.DeviceID, in.Generation, in.ExternalSessionID, in.Sequence)
	}
	return res, nil
}

// DeliverBatch processes a batch in order, so a device can upload everything it
// buffered while offline in one call.
//
// One record's outcome never stops the others: a single quarantined record in
// the middle of a day's collections must not strand the rest.
func (s *Service) DeliverBatch(ctx context.Context, ins []DeliverInput) ([]*repository.IngestResult, error) {
	if len(ins) == 0 {
		return nil, errors.New("batch is empty")
	}
	if len(ins) > 1000 {
		return nil, fmt.Errorf("batch carries %d records, limit is 1000", len(ins))
	}

	out := make([]*repository.IngestResult, 0, len(ins))
	for i, in := range ins {
		res, err := s.Deliver(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("batch record %d (sequence %d): %w", i, in.Sequence, err)
		}
		out = append(out, res)
	}
	return out, nil
}

func (s *Service) RegisterDevice(ctx context.Context, tenantID, serial string, kind domain.DeviceKind, label, actor string) (*domain.Device, error) {
	switch {
	case tenantID == "":
		return nil, errors.New("tenant_id is required")
	case serial == "":
		return nil, errors.New("serial is required")
	case actor == "":
		return nil, errors.New("actor is required")
	}
	if !validDeviceKind(kind) {
		return nil, fmt.Errorf("device kind %q is not recognised", kind)
	}

	d := &domain.Device{
		ID:        ulidpkg.New().String(),
		TenantID:  tenantID,
		Serial:    serial,
		Kind:      kind,
		Label:     label,
		CreatedBy: actor,
		UpdatedBy: actor,
	}
	return s.repo.CreateDevice(ctx, d, ulidpkg.New().String())
}

// RollGeneration starts a new identity epoch for a device.
//
// This must be called whenever a device's sequence counter restarts — a
// factory reset, a reflash, an app reinstall. Skipping it makes the device's
// restarted sequences collide with the ones it already sent, and every one of
// them ends up quarantined.
func (s *Service) RollGeneration(ctx context.Context, tenantID, deviceID string, reason domain.GenerationReason, actor string) (*domain.DeviceGeneration, error) {
	if !validGenerationReason(reason) {
		return nil, fmt.Errorf("generation reason %q is not recognised", reason)
	}
	if actor == "" {
		return nil, errors.New("actor is required")
	}

	gen, err := s.repo.RollGeneration(ctx, tenantID, deviceID, reason, ulidpkg.New().String(), actor)
	if err != nil {
		return nil, err
	}
	s.log.Infof("device %s rolled to generation %d (%s)", deviceID, gen.Generation, reason)
	return gen, nil
}

// OpenSession starts a capture session on a device's current generation.
//
// The generation is looked up rather than accepted from the caller: a device
// reporting its own epoch could report a stale one and quietly append to a
// sequence space that belongs to the run before its reset.
func (s *Service) OpenSession(ctx context.Context, tenantID, deviceID, externalSessionID, operatorRef, actor string) (*domain.CaptureSession, error) {
	switch {
	case tenantID == "":
		return nil, errors.New("tenant_id is required")
	case deviceID == "":
		return nil, errors.New("device_id is required")
	case externalSessionID == "":
		return nil, errors.New("external_session_id is required")
	case actor == "":
		return nil, errors.New("actor is required")
	}

	device, err := s.repo.GetDevice(ctx, deviceID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load device: %w", err)
	}

	return s.repo.OpenSession(ctx, &domain.CaptureSession{
		ID:                ulidpkg.New().String(),
		TenantID:          tenantID,
		DeviceID:          deviceID,
		Generation:        device.CurrentGeneration,
		ExternalSessionID: externalSessionID,
		OperatorRef:       operatorRef,
		CreatedBy:         actor,
	})
}

func (s *Service) CloseSession(ctx context.Context, tenantID, sessionID, actor string) (*domain.CaptureSession, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}
	return s.repo.CloseSession(ctx, tenantID, sessionID, actor)
}

func (s *Service) GetSession(ctx context.Context, id, tenantID string) (*domain.CaptureSession, error) {
	return s.repo.GetSession(ctx, id, tenantID)
}

func (s *Service) ListSessions(ctx context.Context, tenantID, deviceID string, limit, offset int) ([]*domain.CaptureSession, error) {
	return s.repo.ListSessions(ctx, tenantID, deviceID, clampLimit(limit), clampOffset(offset))
}

func (s *Service) GetDevice(ctx context.Context, id, tenantID string) (*domain.Device, error) {
	return s.repo.GetDevice(ctx, id, tenantID)
}

func (s *Service) ListDevices(ctx context.Context, tenantID string, limit, offset int) ([]*domain.Device, error) {
	return s.repo.ListDevices(ctx, tenantID, clampLimit(limit), clampOffset(offset))
}

func (s *Service) ListGenerations(ctx context.Context, tenantID, deviceID string) ([]*domain.DeviceGeneration, error) {
	return s.repo.ListGenerations(ctx, tenantID, deviceID)
}

func (s *Service) ListQuarantined(ctx context.Context, tenantID, reason string, limit, offset int) ([]*domain.QuarantinedRecord, error) {
	return s.repo.ListQuarantined(ctx, tenantID, reason, clampLimit(limit), clampOffset(offset))
}

func (s *Service) GetQuarantined(ctx context.Context, id, tenantID string) (*domain.QuarantinedRecord, error) {
	return s.repo.GetQuarantined(ctx, id, tenantID)
}

// ResolveQuarantine records an operator's decision about a held record.
func (s *Service) ResolveQuarantine(ctx context.Context, tenantID, id, resolution, actor string) (*domain.QuarantinedRecord, error) {
	if resolution == "" {
		return nil, errors.New("a resolution note is required so the decision is auditable")
	}
	if actor == "" {
		return nil, errors.New("actor is required")
	}
	return s.repo.ResolveQuarantine(ctx, tenantID, id, resolution, actor)
}

func validateDelivery(in DeliverInput) error {
	switch {
	case in.TenantID == "":
		return errors.New("tenant_id is required")
	case in.DeviceID == "":
		return errors.New("device_id is required")
	case in.ExternalSessionID == "":
		return errors.New("external_session_id is required")
	case in.Generation < 1:
		return errors.New("generation must be at least 1")
	case len(in.Payload) == 0:
		return errors.New("payload is required")
	case in.CapturedAt.IsZero():
		return errors.New("captured_at is required: without the device clock a late upload cannot be placed in time")
	case in.Actor == "":
		return errors.New("actor is required")
	}
	return nil
}

func validDeviceKind(k domain.DeviceKind) bool {
	switch k {
	case domain.DeviceWeighbridge, domain.DeviceMilkAnalyser, domain.DeviceScale,
		domain.DeviceMobileApp, domain.DeviceManual:
		return true
	default:
		return false
	}
}

func validGenerationReason(r domain.GenerationReason) bool {
	switch r {
	case domain.ReasonInitialProvisioning, domain.ReasonFactoryReset, domain.ReasonFirmwareReflash,
		domain.ReasonAppReinstall, domain.ReasonClockReset, domain.ReasonSuspectedTampering,
		domain.ReasonOperatorRequest:
		return true
	default:
		return false
	}
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}
