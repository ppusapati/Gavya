// Package repository is material flow's access to its tables.
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
	"github.com/ppusapati/gavya/libs/integrity/quantity"

	"github.com/ppusapati/gavya/services/material-service/internal/domain"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrDuplicateCode is a node code the tenant already uses.
	ErrDuplicateCode = errors.New("a node with that code already exists")
	// ErrTankerBusy is a vehicle already carrying something.
	ErrTankerBusy = errors.New("that tanker is already carrying a movement")
	// ErrReceivedIsFinal is the trigger refusing to rewrite an arrival.
	ErrReceivedIsFinal = errors.New("a movement that has been received cannot be changed")
)

const serviceName = "material-service"

type IDs interface{ New() string }

type Repository interface {
	CreateNode(ctx context.Context, n *domain.Node) (*domain.Node, error)
	GetNode(ctx context.Context, tenantID, id string) (*domain.Node, error)
	GetNodeByCode(ctx context.Context, tenantID, code string) (*domain.Node, error)
	ListNodes(ctx context.Context, tenantID string, kind domain.NodeKind) ([]*domain.Node, error)

	Dispatch(ctx context.Context, m *domain.Movement) (*domain.Movement, error)
	GetMovement(ctx context.Context, tenantID, id string) (*domain.Movement, error)
	Receive(ctx context.Context, m *domain.Movement, actor string) (*domain.Movement, error)
	Abandon(ctx context.Context, tenantID, id, reason, actor string) (*domain.Movement, error)
	ListMovements(ctx context.Context, tenantID, nodeID string, from, to time.Time, limit int) ([]*domain.Movement, error)
}

type repo struct {
	db  *pgxpool.Pool
	ids IDs
}

func New(db *pgxpool.Pool, ids IDs) Repository { return &repo{db: db, ids: ids} }

// ---------------------------------------------------------------------------
// Nodes
// ---------------------------------------------------------------------------

func (r *repo) CreateNode(ctx context.Context, n *domain.Node) (*domain.Node, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	n.ID = r.ids.New()
	var capValue *int64
	var capUnit *string
	if n.Capacity != nil {
		v, u := n.Capacity.Value(), string(n.Capacity.Unit)
		capValue, capUnit = &v, &u
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO material_nodes (id,tenant_id,code,name,kind,capacity_value,capacity_unit,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)`,
		n.ID, n.TenantID, n.Code, n.Name, string(n.Kind), capValue, capUnit, n.CreatedBy); err != nil {
		if sqlState(err) == "23505" {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateCode, n.Code)
		}
		return nil, fmt.Errorf("create node: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "register_material_node", ResourceType: "material_node", ResourceID: n.ID,
		After:       map[string]any{"code": n.Code, "name": n.Name, "kind": string(n.Kind)},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	n.Active = true
	return n, tx.Commit(ctx)
}

const nodeCols = `id,tenant_id,code,name,kind,capacity_value,capacity_unit,active,created_at,created_by`

func (r *repo) GetNode(ctx context.Context, tenantID, id string) (*domain.Node, error) {
	return scanNode(r.db.QueryRow(ctx,
		`SELECT `+nodeCols+` FROM material_nodes WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`,
		tenantID, id))
}

func (r *repo) GetNodeByCode(ctx context.Context, tenantID, code string) (*domain.Node, error) {
	return scanNode(r.db.QueryRow(ctx,
		`SELECT `+nodeCols+` FROM material_nodes WHERE tenant_id=$1 AND code=$2 AND deleted_at IS NULL`,
		tenantID, code))
}

func (r *repo) ListNodes(ctx context.Context, tenantID string, kind domain.NodeKind) ([]*domain.Node, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+nodeCols+` FROM material_nodes
		 WHERE tenant_id=$1 AND deleted_at IS NULL AND ($2='' OR kind=$2)
		 ORDER BY kind, code`, tenantID, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Movements
// ---------------------------------------------------------------------------

func (r *repo) Dispatch(ctx context.Context, m *domain.Movement) (*domain.Movement, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	m.ID = r.ids.New()
	m.Status = domain.InTransit
	var holdValue *int64
	var holdUnit *string
	if m.Holdup != nil {
		v, u := m.Holdup.Value(), string(m.Holdup.Unit)
		holdValue, holdUnit = &v, &u
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO material_movements
			(id,tenant_id,from_node_id,to_node_id,dispatched_at,dispatched_value,dispatched_unit,
			 dispatch_method,dispatched_by,holdup_value,holdup_unit,status,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$9,$9)`,
		m.ID, m.TenantID, m.FromNodeID, m.ToNodeID, m.DispatchedAt,
		m.Dispatched.Value(), string(m.Dispatched.Unit),
		string(m.DispatchMethod), m.CreatedBy, holdValue, holdUnit, string(m.Status)); err != nil {
		// The trigger raises a unique violation when a tanker is already out.
		if sqlState(err) == "23505" {
			return nil, fmt.Errorf("%w: %s", ErrTankerBusy, err.Error())
		}
		return nil, fmt.Errorf("dispatch: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "dispatch_milk", ResourceType: "material_movement", ResourceID: m.ID,
		After: map[string]any{
			"from_node_id": m.FromNodeID, "to_node_id": m.ToNodeID,
			"dispatched_at": m.DispatchedAt, "quantity": m.Dispatched.Describe(),
			"method": string(m.DispatchMethod),
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetMovement(ctx, m.TenantID, m.ID)
}

// Receive records an arrival and what the two ends came to.
//
// The status is re-read under a row lock rather than trusted from what the
// service saw. Two clerks receiving the same tanker at once would otherwise
// both pass the check and both write, and the second would be refused by the
// trigger with a message about editing rather than about a double receipt.
func (r *repo) Receive(ctx context.Context, m *domain.Movement, actor string) (*domain.Movement, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM material_movements
		 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
		m.TenantID, m.ID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != string(domain.InTransit) {
		return nil, fmt.Errorf("%w: it is %s", domain.ErrAlreadyClosed, status)
	}

	var varValue *int64
	var varUnit *string
	if m.Variance != nil {
		v, u := m.Variance.Value(), string(m.Variance.Unit)
		varValue, varUnit = &v, &u
	}
	var dNum *int64
	var dScale *int32
	var dCelsius *int32
	var dSource *string
	if m.Density != nil {
		n, s, c, src := m.Density.KgPerLitre.Numerator, m.Density.KgPerLitre.Scale,
			m.Density.AtCelsius, string(m.Density.Source)
		dNum, dScale, dCelsius, dSource = &n, &s, &c, &src
	}

	if _, err := tx.Exec(ctx, `
		UPDATE material_movements
		SET received_at=$3, received_value=$4, received_unit=$5, receipt_method=$6, received_by=$7,
		    density_numerator=$8, density_scale=$9, density_celsius=$10, density_source=$11,
		    variance_value=$12, variance_unit=$13,
		    variance_unavailable_reason=nullif($14,''),
		    status=$15, updated_at=NOW(), updated_by=$7
		WHERE tenant_id=$1 AND id=$2`,
		m.TenantID, m.ID, m.ReceivedAt, m.Received.Value(), string(m.Received.Unit),
		string(m.ReceiptMethod), actor,
		dNum, dScale, dCelsius, dSource,
		varValue, varUnit, m.VarianceUnavailableReason, string(domain.Received)); err != nil {
		if sqlState(err) == "23514" {
			return nil, fmt.Errorf("%w: %s", ErrReceivedIsFinal, err.Error())
		}
		return nil, fmt.Errorf("receive: %w", err)
	}

	after := map[string]any{
		"received_at": m.ReceivedAt, "quantity": m.Received.Describe(),
		"method": string(m.ReceiptMethod),
	}
	if m.Variance != nil {
		after["variance"] = m.Variance.Describe()
	} else {
		after["variance_unavailable_reason"] = m.VarianceUnavailableReason
	}
	if m.Density != nil {
		after["density"] = m.Density.String()
	}
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "receive_milk", ResourceType: "material_movement", ResourceID: m.ID,
		Before: map[string]any{"status": status},
		After:  after, ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetMovement(ctx, m.TenantID, m.ID)
}

func (r *repo) Abandon(ctx context.Context, tenantID, id, reason, actor string) (*domain.Movement, error) {
	if reason == "" {
		return nil, domain.ErrNoReason
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM material_movements
		 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`,
		tenantID, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if status != string(domain.InTransit) {
		return nil, fmt.Errorf("%w: it is %s", domain.ErrAlreadyClosed, status)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE material_movements SET status=$3, abandoned_reason=$4, updated_at=NOW(), updated_by=$5
		 WHERE tenant_id=$1 AND id=$2`,
		tenantID, id, string(domain.Abandoned), reason, actor); err != nil {
		return nil, err
	}
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "abandon_movement", ResourceType: "material_movement", ResourceID: id,
		Before:      map[string]any{"status": status},
		After:       map[string]any{"status": string(domain.Abandoned), "reason": reason},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetMovement(ctx, tenantID, id)
}

const movementCols = `id,tenant_id,from_node_id,to_node_id,
	dispatched_at,dispatched_value,dispatched_unit,dispatch_method,dispatched_by,
	received_at,received_value,received_unit,COALESCE(receipt_method,''),COALESCE(received_by,''),
	holdup_value,holdup_unit,
	density_numerator,density_scale,density_celsius,density_source,
	variance_value,variance_unit,COALESCE(variance_unavailable_reason,''),
	status,COALESCE(abandoned_reason,''),created_at,created_by`

func (r *repo) GetMovement(ctx context.Context, tenantID, id string) (*domain.Movement, error) {
	return scanMovement(r.db.QueryRow(ctx,
		`SELECT `+movementCols+` FROM material_movements
		 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, id))
}

// ListMovements returns a period's movements, optionally touching one node.
//
// A node matches at either end. Asked about a tanker, somebody wants both what
// it loaded and what it delivered, and a query that returned only one half
// would show a vessel that fills and never empties.
func (r *repo) ListMovements(ctx context.Context, tenantID, nodeID string, from, to time.Time, limit int) ([]*domain.Movement, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+movementCols+` FROM material_movements
		WHERE tenant_id=$1 AND deleted_at IS NULL
		  AND ($2='' OR from_node_id=$2 OR to_node_id=$2)
		  AND dispatched_at >= $3 AND dispatched_at <= $4
		ORDER BY dispatched_at, id
		LIMIT $5`, tenantID, nodeID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Movement
	for rows.Next() {
		m, err := scanMovement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------

type scanner interface{ Scan(dest ...any) error }

func scanNode(s scanner) (*domain.Node, error) {
	var n domain.Node
	var kind string
	var capValue *int64
	var capUnit *string
	err := s.Scan(&n.ID, &n.TenantID, &n.Code, &n.Name, &kind, &capValue, &capUnit,
		&n.Active, &n.CreatedAt, &n.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	n.Kind = domain.NodeKind(kind)
	if capValue != nil && capUnit != nil {
		q, err := quantity.New(*capValue, quantity.Unit(*capUnit))
		if err != nil {
			return nil, err
		}
		n.Capacity = &q
	}
	return &n, nil
}

func scanMovement(s scanner) (*domain.Movement, error) {
	var m domain.Movement
	var dispValue int64
	var dispUnit, dispMethod, status string
	var recValue, holdValue, varValue *int64
	var recUnit, holdUnit, varUnit *string
	var recMethod, recBy string
	var dNum *int64
	var dScale, dCelsius *int32
	var dSource *string

	err := s.Scan(&m.ID, &m.TenantID, &m.FromNodeID, &m.ToNodeID,
		&m.DispatchedAt, &dispValue, &dispUnit, &dispMethod, &m.DispatchedBy,
		&m.ReceivedAt, &recValue, &recUnit, &recMethod, &recBy,
		&holdValue, &holdUnit,
		&dNum, &dScale, &dCelsius, &dSource,
		&varValue, &varUnit, &m.VarianceUnavailableReason,
		&status, &m.AbandonedReason, &m.CreatedAt, &m.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	m.Dispatched, err = quantity.New(dispValue, quantity.Unit(dispUnit))
	if err != nil {
		return nil, err
	}
	m.DispatchMethod = domain.Method(dispMethod)
	m.ReceiptMethod, m.ReceivedBy = domain.Method(recMethod), recBy
	m.Status = domain.MovementStatus(status)

	for _, f := range []struct {
		value *int64
		unit  *string
		out   **quantity.Quantity
	}{
		{recValue, recUnit, &m.Received},
		{holdValue, holdUnit, &m.Holdup},
		{varValue, varUnit, &m.Variance},
	} {
		if f.value == nil || f.unit == nil {
			continue
		}
		q, err := quantity.New(*f.value, quantity.Unit(*f.unit))
		if err != nil {
			return nil, err
		}
		*f.out = &q
	}

	if dNum != nil && dScale != nil && dSource != nil {
		d := quantity.Density{
			KgPerLitre: money.Rate{Numerator: *dNum, Scale: *dScale},
			Source:     quantity.DensitySource(*dSource),
		}
		if dCelsius != nil {
			d.AtCelsius = *dCelsius
		}
		m.Density = &d
	}
	return &m, nil
}

func sqlState(err error) string {
	type coded interface{ SQLState() string }
	var c coded
	if errors.As(err, &c) {
		return c.SQLState()
	}
	return ""
}
