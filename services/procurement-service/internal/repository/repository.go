// Package repository is procurement's access to its tables.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/libs/integrity/ratecard"

	"github.com/ppusapati/gavya/services/procurement-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing row from a failed query. Without it
// every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such card" from "the database is
// unreachable".
var ErrNotFound = errors.New("not found")

// ErrOverlappingCard is returned when a card would be in force at the same time
// as another.
var ErrOverlappingCard = errors.New("another rate card is already in force over part of that period")

// ErrDuplicateCollection is returned when a producer already has a collection
// recorded for that day and shift.
var ErrDuplicateCollection = errors.New("this producer already has a collection recorded for that day and shift")

type IDs interface{ New() string }

type Repository interface {
	CreateRateCard(ctx context.Context, c *ratecard.Card, actor string) (*ratecard.Card, error)
	GetRateCard(ctx context.Context, tenantID, id string) (*ratecard.Card, error)
	ListRateCards(ctx context.Context, tenantID string, limit, offset int) ([]*ratecard.Card, error)
	// CardInForce returns the one card pricing milk collected at this instant.
	CardInForce(ctx context.Context, tenantID string, at time.Time) (*ratecard.Card, error)

	SaveCollection(ctx context.Context, p *domain.PricedCollection) (*domain.PricedCollection, error)
	// CorrectCollection supersedes one version and writes its replacement in the
	// same transaction.
	CorrectCollection(ctx context.Context, oldID string, p *domain.PricedCollection, reason string) (*domain.PricedCollection, error)
	GetCollection(ctx context.Context, tenantID, id string) (*domain.PricedCollection, error)
	// ListCollections returns the live version of each collection in a period.
	// includeSuperseded adds the corrected-away versions, which is what a
	// history view wants and what a settlement must never see.
	ListCollections(ctx context.Context, tenantID, producerRef string, from, to time.Time, limit, offset int, includeSuperseded bool) ([]*domain.PricedCollection, error)
	// Versions returns one collection and every version of it, oldest first.
	Versions(ctx context.Context, tenantID, id string) ([]*domain.PricedCollection, error)
}

type repo struct {
	db  *pgxpool.Pool
	ids IDs
}

func New(db *pgxpool.Pool, ids IDs) Repository { return &repo{db: db, ids: ids} }

// CreateRateCard writes a card and its cells or terms together.
//
// One transaction, because a card without its chart is a card that prices
// nothing and would sit there looking usable. The audit record goes in the same
// transaction for the same reason it does everywhere else: a rate card is the
// policy every payment is computed from, so a change to one that nobody can
// attribute is the change most worth attributing.
func (r *repo) CreateRateCard(ctx context.Context, c *ratecard.Card, actor string) (*ratecard.Card, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	c.ID = r.ids.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO rate_cards
			(id,tenant_id,name,kind,currency,amount_scale,basis,between_points,outside_chart,
			 rounding,valid_from,valid_to,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,nullif($7,''),nullif($8,''),nullif($9,''),$10,$11,$12,$13,$13)`,
		c.ID, c.TenantID, c.Name, string(c.Kind), c.Currency, c.Scale,
		string(c.Basis), string(c.Between), string(c.Outside),
		string(c.Rounding), c.ValidFrom, c.ValidTo, actor)
	if err != nil {
		if isExclusionViolation(err) {
			return nil, ErrOverlappingCard
		}
		return nil, fmt.Errorf("create rate card: %w", err)
	}

	for _, cell := range c.Cells {
		if _, err := tx.Exec(ctx, `
			INSERT INTO rate_card_cells
				(id,tenant_id,rate_card_id,fat_value,fat_scale,snf_value,snf_scale,
				 rate_numerator,rate_scale,created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			r.ids.New(), c.TenantID, c.ID,
			cell.Fat.Value, cell.Fat.Scale, cell.SNF.Value, cell.SNF.Scale,
			cell.Rate.Numerator, cell.Rate.Scale, actor); err != nil {
			return nil, fmt.Errorf("create chart cell at fat %s SNF %s: %w", cell.Fat, cell.SNF, err)
		}
	}
	for _, term := range c.Terms {
		if _, err := tx.Exec(ctx, `
			INSERT INTO rate_card_terms
				(id,tenant_id,rate_card_id,component,rate_numerator,rate_scale,created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			r.ids.New(), c.TenantID, c.ID, term.Component,
			term.Rate.Numerator, term.Rate.Scale, actor); err != nil {
			return nil, fmt.Errorf("create formula term %s: %w", term.Component, err)
		}
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "declare_rate_card", ResourceType: "rate_card", ResourceID: c.ID,
		After: map[string]any{
			"name": c.Name, "kind": string(c.Kind),
			"basis": string(c.Basis), "between_points": string(c.Between),
			"outside_chart": string(c.Outside), "rounding": string(c.Rounding),
			"valid_from": c.ValidFrom, "valid_to": c.ValidTo,
			"cells": len(c.Cells), "terms": len(c.Terms),
		},
		ServiceName: "procurement-service",
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

const cardCols = `id,tenant_id,name,kind,currency,amount_scale,
	COALESCE(basis,''),COALESCE(between_points,''),COALESCE(outside_chart,''),
	rounding,valid_from,valid_to`

func (r *repo) GetRateCard(ctx context.Context, tenantID, id string) (*ratecard.Card, error) {
	c, err := scanCard(r.db.QueryRow(ctx,
		`SELECT `+cardCols+` FROM rate_cards WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`,
		tenantID, id))
	if err != nil {
		return nil, err
	}
	return r.fill(ctx, c)
}

// CardInForce finds the card pricing milk collected at an instant.
//
// The exclusion constraint on the table means at most one can match, so this
// does not have to choose — and that is the point of the constraint. Choosing
// here would make the choice invisible and a society would find out when a
// producer compared two statements.
func (r *repo) CardInForce(ctx context.Context, tenantID string, at time.Time) (*ratecard.Card, error) {
	c, err := scanCard(r.db.QueryRow(ctx,
		`SELECT `+cardCols+` FROM rate_cards
		 WHERE tenant_id=$1 AND deleted_at IS NULL
		   AND valid_from <= $2 AND (valid_to IS NULL OR valid_to > $2)`,
		tenantID, at))
	if errors.Is(err, ErrNotFound) {
		return nil, &domain.ErrNoCardInForce{TenantID: tenantID, At: at}
	}
	if err != nil {
		return nil, err
	}
	return r.fill(ctx, c)
}

func (r *repo) ListRateCards(ctx context.Context, tenantID string, limit, offset int) ([]*ratecard.Card, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+cardCols+` FROM rate_cards WHERE tenant_id=$1 AND deleted_at IS NULL
		 ORDER BY valid_from DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ratecard.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Filled after the rows are closed: the connection is busy until then, and
	// querying on it mid-iteration is what turns a working list into an
	// intermittent one under a pool.
	for i, c := range out {
		filled, err := r.fill(ctx, c)
		if err != nil {
			return nil, err
		}
		out[i] = filled
	}
	return out, nil
}

// fill loads a card's chart or formula.
func (r *repo) fill(ctx context.Context, c *ratecard.Card) (*ratecard.Card, error) {
	switch c.Kind {
	case ratecard.Chart:
		rows, err := r.db.Query(ctx, `
			SELECT fat_value,fat_scale,snf_value,snf_scale,rate_numerator,rate_scale
			FROM rate_card_cells WHERE tenant_id=$1 AND rate_card_id=$2
			ORDER BY fat_value, snf_value`, c.TenantID, c.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var cell ratecard.Cell
			if err := rows.Scan(&cell.Fat.Value, &cell.Fat.Scale, &cell.SNF.Value, &cell.SNF.Scale,
				&cell.Rate.Numerator, &cell.Rate.Scale); err != nil {
				return nil, err
			}
			c.Cells = append(c.Cells, cell)
		}
		return c, rows.Err()

	case ratecard.Formula:
		rows, err := r.db.Query(ctx, `
			SELECT component,rate_numerator,rate_scale
			FROM rate_card_terms WHERE tenant_id=$1 AND rate_card_id=$2 ORDER BY component`,
			c.TenantID, c.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var t ratecard.Term
			if err := rows.Scan(&t.Component, &t.Rate.Numerator, &t.Rate.Scale); err != nil {
				return nil, err
			}
			c.Terms = append(c.Terms, t)
		}
		return c, rows.Err()
	}
	return c, nil
}

// SaveCollection writes a priced collection and the record of who priced it.
func (r *repo) SaveCollection(ctx context.Context, p *domain.PricedCollection) (*domain.PricedCollection, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO priced_collections
			(id,tenant_id,producer_ref,society_code,collected_on,shift,
			 quantity_value,quantity_scale,quantity_unit,
			 fat_value,fat_scale,snf_value,snf_scale,
			 rate_card_id,rate_numerator,rate_scale,
			 currency,amount_scale,amount_minor_units,explanation,
			 origin_kind,source_system_id,import_batch_id,source_record_id,
			 created_by,updated_by)
		VALUES ($1,$2,$3,nullif($4,''),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
		        $17,$18,$19,$20,$21,nullif($22,''),nullif($23,''),nullif($24,''),$25,$25)`,
		p.ID, p.TenantID, p.ProducerRef, p.SocietyCode, p.CollectedOn, string(p.Shift),
		p.Quantity.Value, p.Quantity.Scale, string(p.Unit),
		p.Fat.Value, p.Fat.Scale, p.SNF.Value, p.SNF.Scale,
		p.RateCardID, p.Rate.Numerator, p.Rate.Scale,
		p.Amount.Currency, p.Amount.Scale, p.Amount.Value, p.Explanation,
		string(p.Origin.Kind), p.SourceSystemID, p.ImportBatchID, p.SourceRecordID,
		p.CreatedBy)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateCollection
		}
		return nil, fmt.Errorf("save collection: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "price_collection", ResourceType: "priced_collection", ResourceID: p.ID,
		After: map[string]any{
			"producer_ref": p.ProducerRef, "collected_on": p.CollectedOn.Format("2006-01-02"),
			"shift": string(p.Shift), "quantity": p.Quantity.String(),
			"rate_card_id": p.RateCardID, "rate": p.Rate.String(),
			"amount": p.Amount.String(), "explanation": p.Explanation,
		},
		ServiceName: "procurement-service",
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// CorrectCollection replaces a collection with a restated one.
//
// One transaction, and the supersession is written before the replacement. The
// live-version index permits exactly one unsuperseded row per producer, day and
// shift, so doing it the other way round would be refused by the database —
// which is the index doing its job, and the reason the order here is not
// arbitrary.
func (r *repo) CorrectCollection(ctx context.Context, oldID string, p *domain.PricedCollection, reason string) (*domain.PricedCollection, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// Locked and re-read rather than trusting what the service saw. Two
	// operators correcting the same slip at once would otherwise both pass the
	// current-version check and write two live corrections of one delivery.
	var supersededAt *time.Time
	var beforeAmount int64
	var beforeScale int32
	var beforeCurrency string
	if err := tx.QueryRow(ctx,
		`SELECT superseded_at, amount_minor_units, amount_scale, currency
		 FROM priced_collections
		 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
		p.TenantID, oldID).Scan(&supersededAt, &beforeAmount, &beforeScale, &beforeCurrency); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if supersededAt != nil {
		return nil, domain.ErrAlreadySuperseded
	}

	if _, err := tx.Exec(ctx,
		`UPDATE priced_collections SET superseded_at=NOW(), superseded_by=$3, updated_at=NOW(), updated_by=$4
		 WHERE tenant_id=$1 AND id=$2`,
		p.TenantID, oldID, p.ID, p.CreatedBy); err != nil {
		return nil, fmt.Errorf("supersede collection %s: %w", oldID, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO priced_collections
			(id,tenant_id,producer_ref,society_code,collected_on,shift,
			 quantity_value,quantity_scale,quantity_unit,
			 fat_value,fat_scale,snf_value,snf_scale,
			 rate_card_id,rate_numerator,rate_scale,
			 currency,amount_scale,amount_minor_units,explanation,
			 origin_kind,source_system_id,import_batch_id,source_record_id,
			 supersedes,correction_reason,created_by,updated_by)
		VALUES ($1,$2,$3,nullif($4,''),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
		        $17,$18,$19,$20,$21,nullif($22,''),nullif($23,''),nullif($24,''),$25,$26,$27,$27)`,
		p.ID, p.TenantID, p.ProducerRef, p.SocietyCode, p.CollectedOn, string(p.Shift),
		p.Quantity.Value, p.Quantity.Scale, string(p.Unit),
		p.Fat.Value, p.Fat.Scale, p.SNF.Value, p.SNF.Scale,
		p.RateCardID, p.Rate.Numerator, p.Rate.Scale,
		p.Amount.Currency, p.Amount.Scale, p.Amount.Value, p.Explanation,
		string(p.Origin.Kind), p.SourceSystemID, p.ImportBatchID, p.SourceRecordID,
		oldID, reason, p.CreatedBy); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateCollection
		}
		return nil, fmt.Errorf("write the corrected collection: %w", err)
	}

	// Both figures on the record, because the question asked afterwards is
	// always what it was before and what it is now.
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "correct_collection", ResourceType: "priced_collection", ResourceID: p.ID,
		Before: map[string]any{
			"collection_id": oldID,
			"amount":        money.Money{Value: beforeAmount, Scale: beforeScale, Currency: beforeCurrency}.String(),
		},
		After: map[string]any{
			"producer_ref": p.ProducerRef, "collected_on": p.CollectedOn.Format("2006-01-02"),
			"shift": string(p.Shift), "quantity": p.Quantity.String(),
			"rate": p.Rate.String(), "amount": p.Amount.String(),
			"supersedes": oldID, "reason": reason,
		},
		ServiceName: "procurement-service",
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

const collectionCols = `id,tenant_id,producer_ref,COALESCE(society_code,''),collected_on,shift,
	quantity_value,quantity_scale,quantity_unit,
	COALESCE(fat_value,0),COALESCE(fat_scale,0),COALESCE(snf_value,0),COALESCE(snf_scale,0),
	rate_card_id,COALESCE(rate_numerator,0),COALESCE(rate_scale,0),
	currency,amount_scale,amount_minor_units,explanation,
	origin_kind,COALESCE(source_system_id,''),COALESCE(import_batch_id,''),
	COALESCE(source_record_id,''),created_at,created_by,
	superseded_at,COALESCE(superseded_by,''),COALESCE(supersedes,''),COALESCE(correction_reason,'')`

func (r *repo) GetCollection(ctx context.Context, tenantID, id string) (*domain.PricedCollection, error) {
	return scanCollection(r.db.QueryRow(ctx,
		`SELECT `+collectionCols+` FROM priced_collections
		 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, id))
}

// ListCollections returns a period's collections.
//
// Superseded versions are excluded unless asked for, and that default is the
// one that matters: settlement gathers a fortnight through this call, and a
// corrected collection returned alongside the version it corrected would pay
// the producer for the same milk twice — once at the wrong figure and once at
// the right one — with both lines looking entirely ordinary on the statement.
func (r *repo) ListCollections(ctx context.Context, tenantID, producerRef string, from, to time.Time, limit, offset int, includeSuperseded bool) ([]*domain.PricedCollection, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+collectionCols+` FROM priced_collections
		WHERE tenant_id=$1 AND deleted_at IS NULL
		  AND ($7 OR superseded_at IS NULL)
		  AND ($2='' OR producer_ref=$2)
		  AND collected_on >= $3 AND collected_on <= $4
		ORDER BY collected_on, producer_ref, shift, created_at
		LIMIT $5 OFFSET $6`, tenantID, producerRef, from, to, limit, offset, includeSuperseded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.PricedCollection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Versions returns every version of a collection, oldest first.
//
// Reached by walking the supersedes chain in both directions from whichever
// version was asked for, because the id a person has is usually the one printed
// on a statement — which is the version that was current when it was printed,
// not necessarily the first or the last.
func (r *repo) Versions(ctx context.Context, tenantID, id string) ([]*domain.PricedCollection, error) {
	rows, err := r.db.Query(ctx, `
		WITH RECURSIVE back AS (
			SELECT * FROM priced_collections WHERE tenant_id=$1 AND id=$2
			UNION
			SELECT c.* FROM priced_collections c JOIN back b ON c.id = b.supersedes
				AND c.tenant_id = $1
		), forward AS (
			SELECT * FROM priced_collections WHERE tenant_id=$1 AND id=$2
			UNION
			SELECT c.* FROM priced_collections c JOIN forward f ON c.supersedes = f.id
				AND c.tenant_id = $1
		)
		SELECT `+collectionCols+` FROM (
			SELECT * FROM back UNION SELECT * FROM forward
		) v ORDER BY created_at, id`, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.PricedCollection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanCard(s scanner) (*ratecard.Card, error) {
	var c ratecard.Card
	var kind, basis, between, outside, rounding string
	err := s.Scan(&c.ID, &c.TenantID, &c.Name, &kind, &c.Currency, &c.Scale,
		&basis, &between, &outside, &rounding, &c.ValidFrom, &c.ValidTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Kind = ratecard.Kind(kind)
	c.Basis = ratecard.Basis(basis)
	c.Between = ratecard.Between(between)
	c.Outside = ratecard.Outside(outside)
	c.Rounding = money.RoundingMode(rounding)
	return &c, nil
}

func scanCollection(s scanner) (*domain.PricedCollection, error) {
	var p domain.PricedCollection
	var shift, unit, originKind string
	err := s.Scan(&p.ID, &p.TenantID, &p.ProducerRef, &p.SocietyCode, &p.CollectedOn, &shift,
		&p.Quantity.Value, &p.Quantity.Scale, &unit,
		&p.Fat.Value, &p.Fat.Scale, &p.SNF.Value, &p.SNF.Scale,
		&p.RateCardID, &p.Rate.Numerator, &p.Rate.Scale,
		&p.Amount.Currency, &p.Amount.Scale, &p.Amount.Value, &p.Explanation,
		&originKind, &p.SourceSystemID, &p.ImportBatchID, &p.SourceRecordID,
		&p.CreatedAt, &p.CreatedBy,
		&p.SupersededAt, &p.SupersededBy, &p.Supersedes, &p.CorrectionReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Shift = domain.Shift(shift)
	p.Unit = ratecard.Basis(unit)
	p.Origin = origin.Origin{Kind: origin.Kind(originKind)}
	return &p, nil
}

func isExclusionViolation(err error) bool { return sqlState(err) == "23P01" }
func isUniqueViolation(err error) bool    { return sqlState(err) == "23505" }

func sqlState(err error) string {
	type coded interface{ SQLState() string }
	var c coded
	if errors.As(err, &c) {
		return c.SQLState()
	}
	return ""
}
