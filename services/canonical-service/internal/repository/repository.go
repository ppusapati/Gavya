package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/services/canonical-service/internal/domain"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrOverlappingIdentity means a mapping was proposed whose valid interval
	// overlaps one already on record for the same external identifier.
	ErrOverlappingIdentity = errors.New("an overlapping identity mapping already exists")
	// ErrOverlappingPolicy means two collection identity policies would be in
	// force at once, which would make a slot key ambiguous.
	ErrOverlappingPolicy = errors.New("another policy is already in force for that period")
)

// Placer is the pure slot decision, supplied by the service and evaluated
// inside the repository's transaction.
type Placer func(*domain.CollectionIdentityPolicy, *domain.AuthoritativeCollectionSlot, domain.Claim) (domain.ClaimDecision, error)

// ClaimInput is one record asserting it is the collection for a slot.
type ClaimInput struct {
	TenantID string
	SlotKey  string
	Claim    domain.Claim
	Policy   *domain.CollectionIdentityPolicy
	Actor    string
	// SlotID is pre-generated so the identifier is assigned outside the
	// transaction and a retry stays deterministic.
	SlotID string
}

// ClaimResult reports what a claim did to its slot.
type ClaimResult struct {
	Decision domain.ClaimDecision
	Slot     *domain.AuthoritativeCollectionSlot
}

type Repository interface {
	CreateIdentity(ctx context.Context, i *domain.ExternalIdentity) (*domain.ExternalIdentity, error)
	ResolveIdentity(ctx context.Context, tenantID, sourceSystemID string, kind domain.EntityKind, externalID string, asOf time.Time) (*domain.ExternalIdentity, error)
	ReverseResolve(ctx context.Context, tenantID string, kind domain.EntityKind, entityID string) ([]*domain.ExternalIdentity, error)
	SupersedeIdentity(ctx context.Context, tenantID, id, supersededBy string) error
	ListIdentities(ctx context.Context, tenantID, sourceSystemID string, limit, offset int) ([]*domain.ExternalIdentity, error)

	CreatePolicy(ctx context.Context, p *domain.CollectionIdentityPolicy) (*domain.CollectionIdentityPolicy, error)
	GetEffectivePolicy(ctx context.Context, tenantID string, at time.Time) (*domain.CollectionIdentityPolicy, error)
	GetPolicy(ctx context.Context, id, tenantID string) (*domain.CollectionIdentityPolicy, error)
	ListPolicies(ctx context.Context, tenantID string) ([]*domain.CollectionIdentityPolicy, error)

	ClaimSlot(ctx context.Context, in ClaimInput, place Placer) (*ClaimResult, error)
	GetSlot(ctx context.Context, tenantID, slotKey string, originKind origin.Kind) (*domain.AuthoritativeCollectionSlot, error)
	ListConflicts(ctx context.Context, tenantID string, limit, offset int) ([]*domain.AuthoritativeCollectionSlot, error)
	ResolveConflict(ctx context.Context, tenantID, slotID, authoritativeRef, resolution, actor string) (*domain.AuthoritativeCollectionSlot, error)
}

// serviceName is what this service's audit entries are attributed to.
const serviceName = "canonical-service"

type repo struct {
	db  *pgxpool.Pool
	ids audit.IDs
}

func New(db *pgxpool.Pool, ids audit.IDs) Repository { return &repo{db: db, ids: ids} }

const identityCols = `id,tenant_id,source_system_id,entity_kind,external_id,entity_id,
	method,COALESCE(confidence,0),note,valid_from,valid_to,
	recorded_at,superseded_at,COALESCE(superseded_by,''),created_at,created_by`

func (r *repo) CreateIdentity(ctx context.Context, i *domain.ExternalIdentity) (*domain.ExternalIdentity, error) {
	const q = `INSERT INTO external_identities
		(id,tenant_id,source_system_id,entity_kind,external_id,entity_id,method,confidence,note,valid_from,valid_to,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,0)::numeric,$9,$10,$11,$12)
		RETURNING ` + identityCols

	out, err := scanIdentity(r.db.QueryRow(ctx, q,
		i.ID, i.TenantID, i.SourceSystemID, string(i.EntityKind), i.ExternalID, i.EntityID,
		string(i.Method), i.Confidence, i.Note, i.ValidFrom, i.ValidTo, i.CreatedBy))
	if isExclusionViolation(err, "no_overlapping_identity") {
		return nil, ErrOverlappingIdentity
	}
	return out, err
}

// ResolveIdentity answers what an external identifier meant at a given instant.
//
// The instant is required, not optional: identifiers are reused, so "what does
// P-001 mean" has no single answer without a time to ask about.
func (r *repo) ResolveIdentity(ctx context.Context, tenantID, sourceSystemID string, kind domain.EntityKind, externalID string, asOf time.Time) (*domain.ExternalIdentity, error) {
	const q = `SELECT ` + identityCols + ` FROM external_identities
		WHERE tenant_id=$1 AND source_system_id=$2 AND entity_kind=$3 AND external_id=$4
		  AND superseded_at IS NULL
		  AND valid_from <= $5 AND valid_to > $5`
	return scanIdentity(r.db.QueryRow(ctx, q, tenantID, sourceSystemID, string(kind), externalID, asOf))
}

func (r *repo) ReverseResolve(ctx context.Context, tenantID string, kind domain.EntityKind, entityID string) ([]*domain.ExternalIdentity, error) {
	const q = `SELECT ` + identityCols + ` FROM external_identities
		WHERE tenant_id=$1 AND entity_kind=$2 AND entity_id=$3 AND superseded_at IS NULL
		ORDER BY source_system_id, valid_from`
	return queryIdentities(ctx, r.db, q, tenantID, string(kind), entityID)
}

func (r *repo) SupersedeIdentity(ctx context.Context, tenantID, id, supersededBy string) error {
	const q = `UPDATE external_identities SET superseded_at=NOW(), superseded_by=$3
		WHERE tenant_id=$1 AND id=$2 AND superseded_at IS NULL`
	tag, err := r.db.Exec(ctx, q, tenantID, id, supersededBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repo) ListIdentities(ctx context.Context, tenantID, sourceSystemID string, limit, offset int) ([]*domain.ExternalIdentity, error) {
	const q = `SELECT ` + identityCols + ` FROM external_identities
		WHERE tenant_id=$1 AND ($2='' OR source_system_id=$2) AND superseded_at IS NULL
		ORDER BY entity_kind, external_id, valid_from LIMIT $3 OFFSET $4`
	return queryIdentities(ctx, r.db, q, tenantID, sourceSystemID, limit, offset)
}

const policyCols = `id,tenant_id,name,dimensions,resolution,version,effective_from,effective_to,created_at,created_by`

func (r *repo) CreatePolicy(ctx context.Context, p *domain.CollectionIdentityPolicy) (*domain.CollectionIdentityPolicy, error) {
	dims := make([]string, 0, len(p.Dimensions))
	for _, d := range p.Dimensions {
		dims = append(dims, string(d))
	}
	const q = `INSERT INTO collection_identity_policies
		(id,tenant_id,name,dimensions,resolution,version,effective_from,effective_to,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING ` + policyCols

	out, err := scanPolicy(r.db.QueryRow(ctx, q,
		p.ID, p.TenantID, p.Name, dims, string(p.Resolution), p.Version,
		p.EffectiveFrom, p.EffectiveTo, p.CreatedBy))
	if isExclusionViolation(err, "one_policy_in_force") {
		return nil, ErrOverlappingPolicy
	}
	return out, err
}

func (r *repo) GetEffectivePolicy(ctx context.Context, tenantID string, at time.Time) (*domain.CollectionIdentityPolicy, error) {
	const q = `SELECT ` + policyCols + ` FROM collection_identity_policies
		WHERE tenant_id=$1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`
	return scanPolicy(r.db.QueryRow(ctx, q, tenantID, at))
}

func (r *repo) GetPolicy(ctx context.Context, id, tenantID string) (*domain.CollectionIdentityPolicy, error) {
	const q = `SELECT ` + policyCols + ` FROM collection_identity_policies WHERE id=$1 AND tenant_id=$2`
	return scanPolicy(r.db.QueryRow(ctx, q, id, tenantID))
}

func (r *repo) ListPolicies(ctx context.Context, tenantID string) ([]*domain.CollectionIdentityPolicy, error) {
	const q = `SELECT ` + policyCols + ` FROM collection_identity_policies
		WHERE tenant_id=$1 ORDER BY effective_from DESC`
	rows, err := r.db.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.CollectionIdentityPolicy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const slotCols = `id,tenant_id,slot_key,origin_kind,policy_id,policy_version,
	authoritative_ref,status,incumbent_recorded_at,incumbent_quality,contenders,values,
	resolved_at,COALESCE(resolved_by,''),COALESCE(resolution,''),created_at,updated_at,created_by,updated_by`

// ClaimSlot places a claim, taking a row lock on the slot so two claims for the
// same collection cannot both read an empty slot and both take it.
func (r *repo) ClaimSlot(ctx context.Context, in ClaimInput, place Placer) (*ClaimResult, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	slot, err := scanSlot(tx.QueryRow(ctx,
		`SELECT `+slotCols+` FROM authoritative_collection_slots
		 WHERE tenant_id=$1 AND slot_key=$2 AND origin_kind=$3 FOR UPDATE`,
		in.TenantID, in.SlotKey, string(in.Claim.Origin)))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("load slot: %w", err)
	}

	decision, err := place(in.Policy, slot, in.Claim)
	if err != nil {
		return nil, err
	}

	updated, err := applyDecision(ctx, tx, in, slot, decision)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &ClaimResult{Decision: decision, Slot: updated}, nil
}

func applyDecision(ctx context.Context, tx pgx.Tx, in ClaimInput, slot *domain.AuthoritativeCollectionSlot, d domain.ClaimDecision) (*domain.AuthoritativeCollectionSlot, error) {
	if slot == nil {
		return insertSlot(ctx, tx, in, d)
	}

	contenders := slot.Contenders
	if d.Demoted != nil {
		contenders = append(contenders, *d.Demoted)
	}
	encoded, err := json.Marshal(contenders)
	if err != nil {
		return nil, fmt.Errorf("encode contenders: %w", err)
	}

	switch d.Outcome {
	case domain.OutcomeReasserted:
		return slot, nil

	case domain.OutcomeRetained:
		const q = `UPDATE authoritative_collection_slots
			SET contenders=$3, updated_at=NOW(), updated_by=$4
			WHERE tenant_id=$1 AND id=$2
			RETURNING ` + slotCols
		return scanSlot(tx.QueryRow(ctx, q, in.TenantID, slot.ID, encoded, in.Actor))

	case domain.OutcomeReplaced:
		const q = `UPDATE authoritative_collection_slots
			SET authoritative_ref=$3, status='SETTLED',
			    incumbent_recorded_at=$4, incumbent_quality=$5,
			    contenders=$6, updated_at=NOW(), updated_by=$7
			WHERE tenant_id=$1 AND id=$2
			RETURNING ` + slotCols
		return scanSlot(tx.QueryRow(ctx, q, in.TenantID, slot.ID, d.Authoritative,
			in.Claim.RecordedAt, in.Claim.Quality, encoded, in.Actor))

	case domain.OutcomeConflict:
		// The holder is cleared: a conflicted slot has no authoritative answer,
		// and leaving one in place would let a downstream settlement quietly
		// consume a collection that is under dispute.
		const q = `UPDATE authoritative_collection_slots
			SET status='CONFLICT', authoritative_ref='', contenders=$3, updated_at=NOW(), updated_by=$4
			WHERE tenant_id=$1 AND id=$2
			RETURNING ` + slotCols
		demoted := contenders
		if slot.Status == domain.SlotSettled && slot.AuthoritativeRef != "" {
			demoted = append(demoted, domain.Contender{
				SourceRef:  slot.AuthoritativeRef,
				Origin:     slot.OriginKind,
				RecordedAt: slot.IncumbentRecordedAt,
				Quality:    slot.IncumbentQuality,
				Reason:     "held the slot when the conflict was raised",
			})
		}
		if encoded, err = json.Marshal(demoted); err != nil {
			return nil, fmt.Errorf("encode contenders: %w", err)
		}
		return scanSlot(tx.QueryRow(ctx, q, in.TenantID, slot.ID, encoded, in.Actor))

	default:
		return nil, fmt.Errorf("unexpected outcome %q for an existing slot", d.Outcome)
	}
}

func insertSlot(ctx context.Context, tx pgx.Tx, in ClaimInput, d domain.ClaimDecision) (*domain.AuthoritativeCollectionSlot, error) {
	values := make(map[string]string, len(in.Claim.Values))
	for k, v := range in.Claim.Values {
		values[string(k)] = v
	}
	encodedValues, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode values: %w", err)
	}

	const q = `INSERT INTO authoritative_collection_slots
		(id,tenant_id,slot_key,origin_kind,policy_id,policy_version,
		 authoritative_ref,status,incumbent_recorded_at,incumbent_quality,contenders,values,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'SETTLED',$8,$9,'[]'::jsonb,$10,$11,$11)
		RETURNING ` + slotCols

	return scanSlot(tx.QueryRow(ctx, q,
		in.SlotID, in.TenantID, in.SlotKey, string(in.Claim.Origin),
		in.Policy.ID, in.Policy.Version, d.Authoritative,
		in.Claim.RecordedAt, in.Claim.Quality, encodedValues, in.Actor))
}

func (r *repo) GetSlot(ctx context.Context, tenantID, slotKey string, originKind origin.Kind) (*domain.AuthoritativeCollectionSlot, error) {
	const q = `SELECT ` + slotCols + ` FROM authoritative_collection_slots
		WHERE tenant_id=$1 AND slot_key=$2 AND origin_kind=$3`
	return scanSlot(r.db.QueryRow(ctx, q, tenantID, slotKey, string(originKind)))
}

func (r *repo) ListConflicts(ctx context.Context, tenantID string, limit, offset int) ([]*domain.AuthoritativeCollectionSlot, error) {
	const q = `SELECT ` + slotCols + ` FROM authoritative_collection_slots
		WHERE tenant_id=$1 AND status='CONFLICT' AND resolved_at IS NULL
		ORDER BY updated_at DESC LIMIT $2 OFFSET $3`
	rows, err := r.db.Query(ctx, q, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*domain.AuthoritativeCollectionSlot, 0, limit)
	for rows.Next() {
		s, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ResolveConflict records a human's choice of which claim holds a slot.
func (r *repo) ResolveConflict(ctx context.Context, tenantID, slotID, authoritativeRef, resolution, actor string) (*domain.AuthoritativeCollectionSlot, error) {
	// In a transaction so the decision and the record of it commit together. A
	// slot that changed hands with no entry beside it is indistinguishable
	// afterwards from one somebody overwrote, which is the thing the resolution
	// note exists to prevent — and the note is only worth having if it is on the
	// record rather than in a column somebody can update again.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	const q = `UPDATE authoritative_collection_slots
		SET authoritative_ref=$3, status='SETTLED', resolution=$4,
		    resolved_at=NOW(), resolved_by=$5, updated_at=NOW(), updated_by=$5
		WHERE tenant_id=$1 AND id=$2 AND status='CONFLICT'
		RETURNING ` + slotCols
	slot, err := scanSlot(tx.QueryRow(ctx, q, tenantID, slotID, authoritativeRef, resolution, actor))
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "resolve_slot_conflict", ResourceType: "collection_slot", ResourceID: slotID,
		After: map[string]any{
			"authoritative_ref": authoritativeRef,
			"resolution":        resolution,
			"slot_key":          slot.SlotKey,
			"origin_kind":       string(slot.OriginKind),
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return slot, nil
}

type scanner interface {
	Scan(dest ...any) error
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func queryIdentities(ctx context.Context, db querier, q string, args ...any) ([]*domain.ExternalIdentity, error) {
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.ExternalIdentity
	for rows.Next() {
		i, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func scanIdentity(row scanner) (*domain.ExternalIdentity, error) {
	var i domain.ExternalIdentity
	var kind, method string
	if err := row.Scan(&i.ID, &i.TenantID, &i.SourceSystemID, &kind, &i.ExternalID, &i.EntityID,
		&method, &i.Confidence, &i.Note, &i.ValidFrom, &i.ValidTo,
		&i.RecordedAt, &i.SupersededAt, &i.SupersededBy, &i.CreatedAt, &i.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	i.EntityKind = domain.EntityKind(kind)
	i.Method = domain.MappingMethod(method)
	return &i, nil
}

func scanPolicy(row scanner) (*domain.CollectionIdentityPolicy, error) {
	var p domain.CollectionIdentityPolicy
	var dims []string
	var resolution string
	if err := row.Scan(&p.ID, &p.TenantID, &p.Name, &dims, &resolution, &p.Version,
		&p.EffectiveFrom, &p.EffectiveTo, &p.CreatedAt, &p.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.Resolution = domain.ResolutionMode(resolution)
	p.Dimensions = make([]domain.IdentityDimension, 0, len(dims))
	for _, d := range dims {
		p.Dimensions = append(p.Dimensions, domain.IdentityDimension(d))
	}
	return &p, nil
}

func scanSlot(row scanner) (*domain.AuthoritativeCollectionSlot, error) {
	var s domain.AuthoritativeCollectionSlot
	var originKind, status string
	var contenders, values []byte
	if err := row.Scan(&s.ID, &s.TenantID, &s.SlotKey, &originKind, &s.PolicyID, &s.PolicyVersion,
		&s.AuthoritativeRef, &status, &s.IncumbentRecordedAt, &s.IncumbentQuality,
		&contenders, &values, &s.ResolvedAt, &s.ResolvedBy, &s.Resolution,
		&s.CreatedAt, &s.UpdatedAt, &s.CreatedBy, &s.UpdatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.OriginKind = origin.Kind(originKind)
	s.Status = domain.SlotStatus(status)
	if err := json.Unmarshal(contenders, &s.Contenders); err != nil {
		return nil, fmt.Errorf("decode contenders: %w", err)
	}

	raw := map[string]string{}
	if err := json.Unmarshal(values, &raw); err != nil {
		return nil, fmt.Errorf("decode values: %w", err)
	}
	s.Values = make(map[domain.IdentityDimension]string, len(raw))
	for k, v := range raw {
		s.Values[domain.IdentityDimension(k)] = v
	}
	return &s, nil
}

func isExclusionViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	// 23P01 is exclusion_violation.
	return errors.As(err, &pgErr) && pgErr.Code == "23P01" && pgErr.ConstraintName == constraint
}
