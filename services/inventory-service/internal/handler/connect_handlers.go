package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/inventory-service/internal/domain"
	"github.com/ppusapati/gavya/services/inventory-service/internal/repository"
	"github.com/ppusapati/gavya/services/inventory-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "inventory.v1.InventoryService"

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("CreateWarehouse", connectjson.Unary(h.CreateWarehouse))
	route("GetWarehouse", connectjson.Unary(h.GetWarehouse))
	route("ListWarehouses", connectjson.Unary(h.ListWarehouses))
	route("AdjustStock", connectjson.Unary(h.AdjustStock))
	route("ListStockMovements", connectjson.Unary(h.ListStockMovements))
	route("CreateBatch", connectjson.Unary(h.CreateBatch))
	route("GetBatch", connectjson.Unary(h.GetBatch))
	route("ListExpiringBatches", connectjson.Unary(h.ListExpiringBatches))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing warehouse from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrDuplicateWarehouseCode):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateWarehouseRequest struct {
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Address   string `json:"address"`
	ManagerID string `json:"manager_id"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
}

type IDTenantRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type AdjustStockRequest struct {
	TenantID      string    `json:"tenant_id"`
	WarehouseID   string    `json:"warehouse_id"`
	SKUID         string    `json:"sku_id"`
	MovementType  string    `json:"movement_type"`
	Quantity      float64   `json:"quantity"`
	ReferenceID   string    `json:"reference_id"`
	ReferenceType string    `json:"reference_type"`
	Notes         string    `json:"notes"`
	MovedAt       time.Time `json:"moved_at"`
	MovedBy       string    `json:"moved_by"`
	CreatedBy     string    `json:"created_by"`
}

type ListStockMovementsRequest struct {
	TenantID    string `json:"tenant_id"`
	WarehouseID string `json:"warehouse_id"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
}

type CreateBatchRequest struct {
	TenantID       string     `json:"tenant_id"`
	WarehouseID    string     `json:"warehouse_id"`
	SKUID          string     `json:"sku_id"`
	BatchNumber    string     `json:"batch_number"`
	Quantity       float64    `json:"quantity"`
	ManufacturedAt *time.Time `json:"manufactured_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Status         string     `json:"status"`
	CreatedBy      string     `json:"created_by"`
}

type WarehouseResponse struct {
	Warehouse *domain.Warehouse `json:"warehouse"`
}

type ListWarehousesResponse struct {
	Warehouses []*domain.Warehouse `json:"warehouses"`
}

type StockMovementResponse struct {
	Movement *domain.StockMovement `json:"movement"`
}

type ListStockMovementsResponse struct {
	Movements []*domain.StockMovement `json:"movements"`
}

type BatchResponse struct {
	Batch *domain.Batch `json:"batch"`
}

type ListBatchesResponse struct {
	Batches []*domain.Batch `json:"batches"`
}

func (h *Handler) CreateWarehouse(ctx context.Context, req *connect.Request[CreateWarehouseRequest]) (*connect.Response[WarehouseResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateWarehouse(ctx, &domain.Warehouse{
		TenantID:  m.TenantID,
		Name:      m.Name,
		Code:      m.Code,
		Address:   m.Address,
		ManagerID: m.ManagerID,
		Status:    m.Status,
		CreatedBy: m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&WarehouseResponse{Warehouse: out}), nil
}

func (h *Handler) GetWarehouse(ctx context.Context, req *connect.Request[IDTenantRequest]) (*connect.Response[WarehouseResponse], error) {
	out, err := h.svc.GetWarehouse(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&WarehouseResponse{Warehouse: out}), nil
}

func (h *Handler) ListWarehouses(ctx context.Context, req *connect.Request[TenantRequest]) (*connect.Response[ListWarehousesResponse], error) {
	out, err := h.svc.ListWarehouses(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListWarehousesResponse{Warehouses: out}), nil
}

func (h *Handler) AdjustStock(ctx context.Context, req *connect.Request[AdjustStockRequest]) (*connect.Response[StockMovementResponse], error) {
	m := req.Msg
	out, err := h.svc.AdjustStock(ctx, &domain.StockMovement{
		TenantID:      m.TenantID,
		WarehouseID:   m.WarehouseID,
		SKUID:         m.SKUID,
		MovementType:  m.MovementType,
		Quantity:      m.Quantity,
		ReferenceID:   m.ReferenceID,
		ReferenceType: m.ReferenceType,
		Notes:         m.Notes,
		MovedAt:       m.MovedAt,
		MovedBy:       m.MovedBy,
		CreatedBy:     m.CreatedBy,
	}, m.CreatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&StockMovementResponse{Movement: out}), nil
}

func (h *Handler) ListStockMovements(ctx context.Context, req *connect.Request[ListStockMovementsRequest]) (*connect.Response[ListStockMovementsResponse], error) {
	m := req.Msg
	out, err := h.svc.ListStockMovements(ctx, m.TenantID, m.WarehouseID, m.Limit, m.Offset)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListStockMovementsResponse{Movements: out}), nil
}

func (h *Handler) CreateBatch(ctx context.Context, req *connect.Request[CreateBatchRequest]) (*connect.Response[BatchResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateBatch(ctx, &domain.Batch{
		TenantID:       m.TenantID,
		WarehouseID:    m.WarehouseID,
		SKUID:          m.SKUID,
		BatchNumber:    m.BatchNumber,
		Quantity:       m.Quantity,
		ManufacturedAt: m.ManufacturedAt,
		ExpiresAt:      m.ExpiresAt,
		Status:         m.Status,
		CreatedBy:      m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&BatchResponse{Batch: out}), nil
}

func (h *Handler) GetBatch(ctx context.Context, req *connect.Request[IDTenantRequest]) (*connect.Response[BatchResponse], error) {
	out, err := h.svc.GetBatch(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&BatchResponse{Batch: out}), nil
}

func (h *Handler) ListExpiringBatches(ctx context.Context, req *connect.Request[TenantRequest]) (*connect.Response[ListBatchesResponse], error) {
	out, err := h.svc.ListExpiringBatches(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListBatchesResponse{Batches: out}), nil
}
