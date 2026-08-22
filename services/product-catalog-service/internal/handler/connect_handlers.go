package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/product-catalog-service/internal/domain"
	"github.com/ppusapati/gavya/services/product-catalog-service/internal/repository"
	"github.com/ppusapati/gavya/services/product-catalog-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "productcatalog.v1.ProductCatalogService"

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

	route("CreateCategory", connectjson.Unary(h.CreateCategory))
	route("ListCategories", connectjson.Unary(h.ListCategories))
	route("CreateBrand", connectjson.Unary(h.CreateBrand))
	route("ListBrands", connectjson.Unary(h.ListBrands))
	route("CreateProduct", connectjson.Unary(h.CreateProduct))
	route("GetProduct", connectjson.Unary(h.GetProduct))
	route("ListProducts", connectjson.Unary(h.ListProducts))
	route("CreateSKU", connectjson.Unary(h.CreateSKU))
	route("GetSKU", connectjson.Unary(h.GetSKU))
	route("ListProductSKUs", connectjson.Unary(h.ListProductSKUs))
	route("UpdateSKUPrice", connectjson.Unary(h.UpdateSKUPrice))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing product from an unreachable database — and makes an
// unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, repository.ErrDuplicateCategorySlug),
		errors.Is(err, repository.ErrDuplicateBrandSlug),
		errors.Is(err, repository.ErrDuplicateProductSlug),
		errors.Is(err, repository.ErrDuplicateSKUCode):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateCategoryRequest struct {
	TenantID    string  `json:"tenant_id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	ParentID    *string `json:"parent_id"`
	Description string  `json:"description"`
	SortOrder   int     `json:"sort_order"`
	CreatedBy   string  `json:"created_by"`
}

type CategoryResponse struct {
	Category *domain.Category `json:"category"`
}

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type ListCategoriesResponse struct {
	Categories []*domain.Category `json:"categories"`
}

type CreateBrandRequest struct {
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	LogoURL   string `json:"logo_url"`
	CreatedBy string `json:"created_by"`
}

type BrandResponse struct {
	Brand *domain.Brand `json:"brand"`
}

type ListBrandsResponse struct {
	Brands []*domain.Brand `json:"brands"`
}

type CreateProductRequest struct {
	TenantID    string `json:"tenant_id"`
	CategoryID  string `json:"category_id"`
	BrandID     string `json:"brand_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	ProductType string `json:"product_type"`
	Status      string `json:"status"`
	CreatedBy   string `json:"created_by"`
}

type ProductResponse struct {
	Product *domain.Product `json:"product"`
}

type IDTenantRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type ListProductsRequest struct {
	TenantID    string `json:"tenant_id"`
	ProductType string `json:"product_type"`
	Status      string `json:"status"`
}

type ListProductsResponse struct {
	Products []*domain.Product `json:"products"`
}

type CreateSKURequest struct {
	TenantID  string  `json:"tenant_id"`
	ProductID string  `json:"product_id"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Unit      string  `json:"unit"`
	UnitSize  float64 `json:"unit_size"`
	Status    string  `json:"status"`
	CreatedBy string  `json:"created_by"`
}

type SKUResponse struct {
	SKU *domain.SKU `json:"sku"`
}

type ListProductSKUsRequest struct {
	ProductID string `json:"product_id"`
	TenantID  string `json:"tenant_id"`
}

type ListProductSKUsResponse struct {
	SKUs []*domain.SKU `json:"skus"`
}

type UpdateSKUPriceRequest struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	Price     float64 `json:"price"`
	UpdatedBy string  `json:"updated_by"`
}

func (h *Handler) CreateCategory(ctx context.Context, req *connect.Request[CreateCategoryRequest]) (*connect.Response[CategoryResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateCategory(ctx, &domain.Category{
		TenantID:    m.TenantID,
		Name:        m.Name,
		Slug:        m.Slug,
		ParentID:    m.ParentID,
		Description: m.Description,
		SortOrder:   m.SortOrder,
		CreatedBy:   m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&CategoryResponse{Category: out}), nil
}

func (h *Handler) ListCategories(ctx context.Context, req *connect.Request[TenantRequest]) (*connect.Response[ListCategoriesResponse], error) {
	out, err := h.svc.ListCategories(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListCategoriesResponse{Categories: out}), nil
}

func (h *Handler) CreateBrand(ctx context.Context, req *connect.Request[CreateBrandRequest]) (*connect.Response[BrandResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateBrand(ctx, &domain.Brand{
		TenantID:  m.TenantID,
		Name:      m.Name,
		Slug:      m.Slug,
		LogoURL:   m.LogoURL,
		CreatedBy: m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&BrandResponse{Brand: out}), nil
}

func (h *Handler) ListBrands(ctx context.Context, req *connect.Request[TenantRequest]) (*connect.Response[ListBrandsResponse], error) {
	out, err := h.svc.ListBrands(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListBrandsResponse{Brands: out}), nil
}

func (h *Handler) CreateProduct(ctx context.Context, req *connect.Request[CreateProductRequest]) (*connect.Response[ProductResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateProduct(ctx, &domain.Product{
		TenantID:    m.TenantID,
		CategoryID:  m.CategoryID,
		BrandID:     m.BrandID,
		Name:        m.Name,
		Slug:        m.Slug,
		Description: m.Description,
		ProductType: m.ProductType,
		Status:      m.Status,
		CreatedBy:   m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ProductResponse{Product: out}), nil
}

func (h *Handler) GetProduct(ctx context.Context, req *connect.Request[IDTenantRequest]) (*connect.Response[ProductResponse], error) {
	out, err := h.svc.GetProduct(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ProductResponse{Product: out}), nil
}

func (h *Handler) ListProducts(ctx context.Context, req *connect.Request[ListProductsRequest]) (*connect.Response[ListProductsResponse], error) {
	m := req.Msg
	out, err := h.svc.ListProducts(ctx, m.TenantID, m.ProductType, m.Status)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListProductsResponse{Products: out}), nil
}

func (h *Handler) CreateSKU(ctx context.Context, req *connect.Request[CreateSKURequest]) (*connect.Response[SKUResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateSKU(ctx, &domain.SKU{
		TenantID:  m.TenantID,
		ProductID: m.ProductID,
		Code:      m.Code,
		Name:      m.Name,
		Price:     m.Price,
		Currency:  m.Currency,
		Unit:      m.Unit,
		UnitSize:  m.UnitSize,
		Status:    m.Status,
		CreatedBy: m.CreatedBy,
	})
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&SKUResponse{SKU: out}), nil
}

func (h *Handler) GetSKU(ctx context.Context, req *connect.Request[IDTenantRequest]) (*connect.Response[SKUResponse], error) {
	out, err := h.svc.GetSKU(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&SKUResponse{SKU: out}), nil
}

func (h *Handler) ListProductSKUs(ctx context.Context, req *connect.Request[ListProductSKUsRequest]) (*connect.Response[ListProductSKUsResponse], error) {
	out, err := h.svc.ListProductSKUs(ctx, req.Msg.ProductID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListProductSKUsResponse{SKUs: out}), nil
}

func (h *Handler) UpdateSKUPrice(ctx context.Context, req *connect.Request[UpdateSKUPriceRequest]) (*connect.Response[SKUResponse], error) {
	m := req.Msg
	out, err := h.svc.UpdateSKUPrice(ctx, m.ID, m.TenantID, m.Price, m.UpdatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&SKUResponse{SKU: out}), nil
}
