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

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/services/pooling-service/internal/domain"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrDuplicateProducer means a producer already has milk in the pool.
	// Admitting a second row would double their weight in the fund split.
	ErrDuplicateProducer = errors.New("that producer already pooled milk in this pool")
	// ErrOverlappingPolicy means two retroactivity policies would be in force at
	// once, which would give a late correction two different answers.
	ErrOverlappingPolicy = errors.New("another retroactivity policy is already in force for that period")
)

type Repository interface {
	CreatePool(ctx context.Context, p *domain.Pool) (*domain.Pool, error)
	GetPool(ctx context.Context, tenantID, id string) (*domain.Pool, error)
	ListPools(ctx context.Context, tenantID string, from, to time.Time, limit, offset int) ([]*domain.Pool, error)
	SetPoolStatus(ctx context.Context, tenantID, id string, status domain.PoolStatus, actor string) (*domain.Pool, error)

	AddProducerMilk(ctx context.Context, m *domain.ProducerMilk) (*domain.ProducerMilk, error)
	ListProducerMilk(ctx context.Context, tenantID, poolID string) ([]domain.ProducerMilk, error)

	AddUtilisation(ctx context.Context, u *domain.ClassifiedUtilisation) (*domain.ClassifiedUtilisation, error)
	ListUtilisations(ctx context.Context, tenantID, poolID string) ([]domain.ClassifiedUtilisation, error)

	SaveValuation(ctx context.Context, v *domain.PoolValuation, allocations []domain.Allocation) (*domain.PoolValuation, []domain.Allocation, error)
	GetValuation(ctx context.Context, tenantID, poolID string) (*domain.PoolValuation, error)
	ListAllocations(ctx context.Context, tenantID, poolID, valuationID string) ([]domain.Allocation, error)

	CreateEconomicEvents(ctx context.Context, events []domain.ProducerEconomicEvent) ([]domain.ProducerEconomicEvent, error)
	ListEconomicEvents(ctx context.Context, tenantID, poolID, producerRef string, limit, offset int) ([]domain.ProducerEconomicEvent, error)

	CreateRetroactivityPolicy(ctx context.Context, p *domain.RecoveryRetroactivityPolicy) (*domain.RecoveryRetroactivityPolicy, error)
	GetEffectiveRetroactivityPolicy(ctx context.Context, tenantID string, at time.Time) (*domain.RecoveryRetroactivityPolicy, error)
}

type repo struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) Repository { return &repo{db: db} }

const poolCols = `id,tenant_id,name,period_start,period_end,unit,currency,amount_scale,status,
	rate_card_id,policy_version,created_at,updated_at,created_by,updated_by`

func (r *repo) CreatePool(ctx context.Context, p *domain.Pool) (*domain.Pool, error) {
	const q = `INSERT INTO pools
		(id,tenant_id,name,period_start,period_end,unit,currency,amount_scale,status,
		 rate_card_id,policy_version,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
		RETURNING ` + poolCols

	status := p.Status
	if status == "" {
		status = domain.PoolOpen
	}
	return scanPool(r.db.QueryRow(ctx, q,
		p.ID, p.TenantID, p.Name, p.PeriodStart, p.PeriodEnd, p.Unit,
		p.Currency, p.Scale, string(status), p.RateCardID, p.PolicyVersion, p.CreatedBy))
}

func (r *repo) GetPool(ctx context.Context, tenantID, id string) (*domain.Pool, error) {
	const q = `SELECT ` + poolCols + ` FROM pools WHERE tenant_id=$1 AND id=$2`
	return scanPool(r.db.QueryRow(ctx, q, tenantID, id))
}

func (r *repo) ListPools(ctx context.Context, tenantID string, from, to time.Time, limit, offset int) ([]*domain.Pool, error) {
	const q = `SELECT ` + poolCols + ` FROM pools
		WHERE tenant_id=$1
		  AND ($2::timestamptz IS NULL OR period_end > $2)
		  AND ($3::timestamptz IS NULL OR period_start < $3)
		ORDER BY period_start DESC, id
		LIMIT $4 OFFSET $5`

	rows, err := r.db.Query(ctx, q, tenantID, nullTime(from), nullTime(to), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*domain.Pool, 0, limit)
	for rows.Next() {
		p, err := scanPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *repo) SetPoolStatus(ctx context.Context, tenantID, id string, status domain.PoolStatus, actor string) (*domain.Pool, error) {
	const q = `UPDATE pools SET status=$3, updated_at=NOW(), updated_by=$4
		WHERE tenant_id=$1 AND id=$2
		RETURNING ` + poolCols
	return scanPool(r.db.QueryRow(ctx, q, tenantID, id, string(status), actor))
}

// Quantities cross the boundary as decimal literals at three decimals. The
// column is NUMERIC(18,3) and is cast back to text on the way out so the
// literal the domain parses is the literal that was stored, rather than a
// float reconstruction of it.
const producerMilkCols = `id,tenant_id,pool_id,producer_ref,quantity::text,components,slot_refs,
	origin_kind,origin_source_system_id,origin_import_batch_id,origin_source_record_id,
	origin_source_payload_hash,origin_derivation_id,created_at,created_by`

func (r *repo) AddProducerMilk(ctx context.Context, m *domain.ProducerMilk) (*domain.ProducerMilk, error) {
	components, err := json.Marshal(m.Components)
	if err != nil {
		return nil, fmt.Errorf("encode components: %w", err)
	}
	slotRefs := m.SlotRefs
	if slotRefs == nil {
		slotRefs = []string{}
	}

	const q = `INSERT INTO producer_milk
		(id,tenant_id,pool_id,producer_ref,quantity,components,slot_refs,
		 origin_kind,origin_source_system_id,origin_import_batch_id,origin_source_record_id,
		 origin_source_payload_hash,origin_derivation_id,created_by)
		VALUES ($1,$2,$3,$4,$5::text::numeric,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING ` + producerMilkCols

	out, err := scanProducerMilk(r.db.QueryRow(ctx, q,
		m.ID, m.TenantID, m.PoolID, m.ProducerRef, m.Quantity, components, slotRefs,
		string(m.Origin.Kind), m.Origin.SourceSystemID, m.Origin.ImportBatchID,
		m.Origin.SourceRecordID, m.Origin.SourcePayloadHash, m.Origin.DerivationID, m.CreatedBy))
	if isUniqueViolation(err, "uq_producer_per_pool") {
		return nil, ErrDuplicateProducer
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *repo) ListProducerMilk(ctx context.Context, tenantID, poolID string) ([]domain.ProducerMilk, error) {
	const q = `SELECT ` + producerMilkCols + ` FROM producer_milk
		WHERE tenant_id=$1 AND pool_id=$2 ORDER BY producer_ref`

	rows, err := r.db.Query(ctx, q, tenantID, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ProducerMilk
	for rows.Next() {
		m, err := scanProducerMilk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

const utilisationCols = `id,tenant_id,pool_id,class,quantity::text,price_numerator,price_scale,created_at,created_by`

func (r *repo) AddUtilisation(ctx context.Context, u *domain.ClassifiedUtilisation) (*domain.ClassifiedUtilisation, error) {
	const q = `INSERT INTO classified_utilisations
		(id,tenant_id,pool_id,class,quantity,price_numerator,price_scale,created_by)
		VALUES ($1,$2,$3,$4,$5::text::numeric,$6,$7,$8)
		RETURNING ` + utilisationCols

	return scanUtilisation(r.db.QueryRow(ctx, q,
		u.ID, u.TenantID, u.PoolID, string(u.Class), u.Quantity,
		u.Price.Numerator, u.Price.Scale, u.CreatedBy))
}

func (r *repo) ListUtilisations(ctx context.Context, tenantID, poolID string) ([]domain.ClassifiedUtilisation, error) {
	const q = `SELECT ` + utilisationCols + ` FROM classified_utilisations
		WHERE tenant_id=$1 AND pool_id=$2 ORDER BY class`

	rows, err := r.db.Query(ctx, q, tenantID, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ClassifiedUtilisation
	for rows.Next() {
		u, err := scanUtilisation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

const valuationCols = `id,tenant_id,pool_id,currency,amount_scale,
	classified_value_minor_units,component_value_minor_units,producer_settlement_fund_minor_units,
	total_quantity::text,blend_price_numerator,blend_price_scale,rounding_trail,
	origin_derivation_id,computed_at,created_at,created_by`

// SaveValuation writes a valuation and every one of its allocations in a single
// transaction.
//
// A valuation without its allocations is a corrupt record: the pool would claim
// a value that no producer has a share of, and a settlement run reading it
// would pay nobody while reporting the pool as valued. Superseding the pool's
// previous live valuation happens in the same transaction for the same reason —
// a pool is never observable with zero or two live valuations.
func (r *repo) SaveValuation(ctx context.Context, v *domain.PoolValuation, allocations []domain.Allocation) (*domain.PoolValuation, []domain.Allocation, error) {
	trail, err := json.Marshal(v.RoundingTrail)
	if err != nil {
		return nil, nil, fmt.Errorf("encode rounding trail: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	const supersede = `UPDATE pool_valuations SET superseded_at=NOW()
		WHERE tenant_id=$1 AND pool_id=$2 AND superseded_at IS NULL`
	if _, err := tx.Exec(ctx, supersede, v.TenantID, v.PoolID); err != nil {
		return nil, nil, fmt.Errorf("supersede the previous valuation: %w", err)
	}

	const insertValuation = `INSERT INTO pool_valuations
		(id,tenant_id,pool_id,currency,amount_scale,
		 classified_value_minor_units,component_value_minor_units,producer_settlement_fund_minor_units,
		 total_quantity,blend_price_numerator,blend_price_scale,rounding_trail,
		 origin_kind,origin_derivation_id,computed_at,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::text::numeric,$10,$11,$12,'DERIVED',$13,$14,$15)
		RETURNING ` + valuationCols

	saved, err := scanValuation(tx.QueryRow(ctx, insertValuation,
		v.ID, v.TenantID, v.PoolID, v.ClassifiedValue.Currency, v.ClassifiedValue.Scale,
		v.ClassifiedValue.Value, v.ComponentValue.Value, v.ProducerSettlementFund.Value,
		v.TotalQuantity, v.BlendPrice.Numerator, v.BlendPrice.Scale, trail,
		v.Origin.DerivationID, v.ComputedAt, v.CreatedBy))
	if err != nil {
		return nil, nil, fmt.Errorf("insert valuation: %w", err)
	}

	const insertAllocation = `INSERT INTO allocations
		(id,tenant_id,pool_id,valuation_id,producer_ref,currency,amount_scale,
		 component_value_minor_units,fund_share_minor_units,total_minor_units,weight,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING ` + allocationCols

	stored := make([]domain.Allocation, 0, len(allocations))
	for _, a := range allocations {
		out, err := scanAllocation(tx.QueryRow(ctx, insertAllocation,
			a.ID, a.TenantID, a.PoolID, saved.ID, a.ProducerRef,
			a.Total.Currency, a.Total.Scale,
			a.ComponentValue.Value, a.FundShare.Value, a.Total.Value, a.Weight, a.CreatedBy))
		if err != nil {
			return nil, nil, fmt.Errorf("insert allocation for producer %s: %w", a.ProducerRef, err)
		}
		stored = append(stored, *out)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}
	return saved, stored, nil
}

func (r *repo) GetValuation(ctx context.Context, tenantID, poolID string) (*domain.PoolValuation, error) {
	const q = `SELECT ` + valuationCols + ` FROM pool_valuations
		WHERE tenant_id=$1 AND pool_id=$2 AND superseded_at IS NULL`
	return scanValuation(r.db.QueryRow(ctx, q, tenantID, poolID))
}

const allocationCols = `id,tenant_id,pool_id,valuation_id,producer_ref,currency,amount_scale,
	component_value_minor_units,fund_share_minor_units,total_minor_units,weight,created_at,created_by`

func (r *repo) ListAllocations(ctx context.Context, tenantID, poolID, valuationID string) ([]domain.Allocation, error) {
	const q = `SELECT ` + allocationCols + ` FROM allocations
		WHERE tenant_id=$1 AND pool_id=$2 AND ($3='' OR valuation_id=$3)
		ORDER BY producer_ref`

	rows, err := r.db.Query(ctx, q, tenantID, poolID, valuationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Allocation
	for rows.Next() {
		a, err := scanAllocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

const eventCols = `id,tenant_id,pool_id,allocation_id,producer_ref,currency,amount_scale,
	amount_minor_units,kind,COALESCE(supersedes_event_id,''),origin_derivation_id,created_at,created_by`

// CreateEconomicEvents raises a batch of payables all or nothing.
//
// A partial batch would settle some of a pool's producers and leave the rest
// unpaid with the pool already marked settled, and nothing in the data would
// distinguish that from producers who were genuinely owed nothing.
func (r *repo) CreateEconomicEvents(ctx context.Context, events []domain.ProducerEconomicEvent) ([]domain.ProducerEconomicEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	const q = `INSERT INTO producer_economic_events
		(id,tenant_id,pool_id,allocation_id,producer_ref,currency,amount_scale,
		 amount_minor_units,kind,supersedes_event_id,origin_kind,origin_derivation_id,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),'DERIVED',$11,$12)
		RETURNING ` + eventCols

	out := make([]domain.ProducerEconomicEvent, 0, len(events))
	for _, e := range events {
		stored, err := scanEvent(tx.QueryRow(ctx, q,
			e.ID, e.TenantID, e.PoolID, e.AllocationID, e.ProducerRef,
			e.Amount.Currency, e.Amount.Scale, e.Amount.Value, string(e.Kind),
			e.SupersedesEventID, e.Origin.DerivationID, e.CreatedBy))
		if err != nil {
			return nil, fmt.Errorf("raise %s event for producer %s: %w", e.Kind, e.ProducerRef, err)
		}
		out = append(out, *stored)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

func (r *repo) ListEconomicEvents(ctx context.Context, tenantID, poolID, producerRef string, limit, offset int) ([]domain.ProducerEconomicEvent, error) {
	const q = `SELECT ` + eventCols + ` FROM producer_economic_events
		WHERE tenant_id=$1 AND ($2='' OR pool_id=$2) AND ($3='' OR producer_ref=$3)
		ORDER BY created_at DESC, id DESC
		LIMIT $4 OFFSET $5`

	rows, err := r.db.Query(ctx, q, tenantID, poolID, producerRef, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.ProducerEconomicEvent, 0, limit)
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

const retroPolicyCols = `id,tenant_id,name,mode,max_lookback_days,
	currency,amount_scale,minimum_adjustment_minor_units,effective_from,effective_to,created_at,created_by`

func (r *repo) CreateRetroactivityPolicy(ctx context.Context, p *domain.RecoveryRetroactivityPolicy) (*domain.RecoveryRetroactivityPolicy, error) {
	const q = `INSERT INTO recovery_retroactivity_policies
		(id,tenant_id,name,mode,max_lookback_days,
		 currency,amount_scale,minimum_adjustment_minor_units,effective_from,effective_to,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING ` + retroPolicyCols

	out, err := scanRetroPolicy(r.db.QueryRow(ctx, q,
		p.ID, p.TenantID, p.Name, string(p.Mode), p.MaxLookbackDays,
		p.MinimumAdjustment.Currency, p.MinimumAdjustment.Scale, p.MinimumAdjustment.Value,
		p.EffectiveFrom, p.EffectiveTo, p.CreatedBy))
	if isExclusionViolation(err, "one_retroactivity_policy_in_force") {
		return nil, ErrOverlappingPolicy
	}
	return out, err
}

func (r *repo) GetEffectiveRetroactivityPolicy(ctx context.Context, tenantID string, at time.Time) (*domain.RecoveryRetroactivityPolicy, error) {
	const q = `SELECT ` + retroPolicyCols + ` FROM recovery_retroactivity_policies
		WHERE tenant_id=$1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`
	return scanRetroPolicy(r.db.QueryRow(ctx, q, tenantID, at))
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPool(row scanner) (*domain.Pool, error) {
	var p domain.Pool
	var status string
	if err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.PeriodStart, &p.PeriodEnd, &p.Unit,
		&p.Currency, &p.Scale, &status, &p.RateCardID, &p.PolicyVersion,
		&p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.Status = domain.PoolStatus(status)
	return &p, nil
}

func scanProducerMilk(row scanner) (*domain.ProducerMilk, error) {
	var m domain.ProducerMilk
	var components []byte
	var originKind string
	if err := row.Scan(&m.ID, &m.TenantID, &m.PoolID, &m.ProducerRef, &m.Quantity,
		&components, &m.SlotRefs,
		&originKind, &m.Origin.SourceSystemID, &m.Origin.ImportBatchID, &m.Origin.SourceRecordID,
		&m.Origin.SourcePayloadHash, &m.Origin.DerivationID, &m.CreatedAt, &m.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	m.Origin.Kind = origin.Kind(originKind)
	if err := json.Unmarshal(components, &m.Components); err != nil {
		return nil, fmt.Errorf("decode components: %w", err)
	}
	return &m, nil
}

func scanUtilisation(row scanner) (*domain.ClassifiedUtilisation, error) {
	var u domain.ClassifiedUtilisation
	var class string
	if err := row.Scan(&u.ID, &u.TenantID, &u.PoolID, &class, &u.Quantity,
		&u.Price.Numerator, &u.Price.Scale, &u.CreatedAt, &u.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.Class = domain.UtilisationClass(class)
	return &u, nil
}

func scanValuation(row scanner) (*domain.PoolValuation, error) {
	var v domain.PoolValuation
	var currency string
	var scale int32
	var trail []byte
	if err := row.Scan(&v.ID, &v.TenantID, &v.PoolID, &currency, &scale,
		&v.ClassifiedValue.Value, &v.ComponentValue.Value, &v.ProducerSettlementFund.Value,
		&v.TotalQuantity, &v.BlendPrice.Numerator, &v.BlendPrice.Scale, &trail,
		&v.Origin.DerivationID, &v.ComputedAt, &v.CreatedAt, &v.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	for _, m := range []*money.Money{&v.ClassifiedValue, &v.ComponentValue, &v.ProducerSettlementFund} {
		m.Currency, m.Scale = currency, scale
	}
	v.Origin.Kind = origin.Derived
	if err := json.Unmarshal(trail, &v.RoundingTrail); err != nil {
		return nil, fmt.Errorf("decode rounding trail: %w", err)
	}
	return &v, nil
}

func scanAllocation(row scanner) (*domain.Allocation, error) {
	var a domain.Allocation
	var currency string
	var scale int32
	if err := row.Scan(&a.ID, &a.TenantID, &a.PoolID, &a.ValuationID, &a.ProducerRef,
		&currency, &scale,
		&a.ComponentValue.Value, &a.FundShare.Value, &a.Total.Value, &a.Weight,
		&a.CreatedAt, &a.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	for _, m := range []*money.Money{&a.ComponentValue, &a.FundShare, &a.Total} {
		m.Currency, m.Scale = currency, scale
	}
	return &a, nil
}

func scanEvent(row scanner) (*domain.ProducerEconomicEvent, error) {
	var e domain.ProducerEconomicEvent
	var kind string
	if err := row.Scan(&e.ID, &e.TenantID, &e.PoolID, &e.AllocationID, &e.ProducerRef,
		&e.Amount.Currency, &e.Amount.Scale, &e.Amount.Value, &kind, &e.SupersedesEventID,
		&e.Origin.DerivationID, &e.CreatedAt, &e.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.Kind = domain.EventKind(kind)
	e.Origin.Kind = origin.Derived
	return &e, nil
}

func scanRetroPolicy(row scanner) (*domain.RecoveryRetroactivityPolicy, error) {
	var p domain.RecoveryRetroactivityPolicy
	var mode string
	if err := row.Scan(&p.ID, &p.TenantID, &p.Name, &mode, &p.MaxLookbackDays,
		&p.MinimumAdjustment.Currency, &p.MinimumAdjustment.Scale, &p.MinimumAdjustment.Value,
		&p.EffectiveFrom, &p.EffectiveTo, &p.CreatedAt, &p.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.Mode = domain.RetroactivityMode(mode)
	return &p, nil
}

// nullTime makes an unset bound mean "unbounded" rather than the zero instant,
// so a listing without a period filter is not silently empty.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	// 23505 is unique_violation.
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func isExclusionViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	// 23P01 is exclusion_violation.
	return errors.As(err, &pgErr) && pgErr.Code == "23P01" && pgErr.ConstraintName == constraint
}
