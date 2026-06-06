package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ppusapati/gavya/services/product-catalog-service/internal/domain"
	"github.com/ppusapati/gavya/services/product-catalog-service/internal/service"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/CreateCategory", h.CreateCategory)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/ListCategories", h.ListCategories)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/CreateBrand", h.CreateBrand)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/ListBrands", h.ListBrands)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/CreateProduct", h.CreateProduct)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/GetProduct", h.GetProduct)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/ListProducts", h.ListProducts)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/CreateSKU", h.CreateSKU)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/GetSKU", h.GetSKU)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/ListProductSKUs", h.ListProductSKUs)
	mux.HandleFunc("/catalog.v1.ProductCatalogService/UpdateSKUPrice", h.UpdateSKUPrice)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
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

type TenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type CreateBrandRequest struct {
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	LogoURL   string `json:"logo_url"`
	CreatedBy string `json:"created_by"`
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

type IDTenantRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type ListProductsRequest struct {
	TenantID    string `json:"tenant_id"`
	ProductType string `json:"product_type"`
	Status      string `json:"status"`
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

type ListProductSKUsRequest struct {
	ProductID string `json:"product_id"`
	TenantID  string `json:"tenant_id"`
}

type UpdateSKUPriceRequest struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	Price     float64 `json:"price"`
	UpdatedBy string  `json:"updated_by"`
}

func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c := &domain.Category{
		TenantID:    req.TenantID,
		Name:        req.Name,
		Slug:        req.Slug,
		ParentID:    req.ParentID,
		Description: req.Description,
		SortOrder:   req.SortOrder,
		CreatedBy:   req.CreatedBy,
	}
	result, err := h.svc.CreateCategory(r.Context(), c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	var req TenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListCategories(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreateBrand(w http.ResponseWriter, r *http.Request) {
	var req CreateBrandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	b := &domain.Brand{
		TenantID:  req.TenantID,
		Name:      req.Name,
		Slug:      req.Slug,
		LogoURL:   req.LogoURL,
		CreatedBy: req.CreatedBy,
	}
	result, err := h.svc.CreateBrand(r.Context(), b)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListBrands(w http.ResponseWriter, r *http.Request) {
	var req TenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListBrands(r.Context(), req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var req CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := &domain.Product{
		TenantID:    req.TenantID,
		CategoryID:  req.CategoryID,
		BrandID:     req.BrandID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		ProductType: req.ProductType,
		Status:      req.Status,
		CreatedBy:   req.CreatedBy,
	}
	result, err := h.svc.CreateProduct(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetProduct(w http.ResponseWriter, r *http.Request) {
	var req IDTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetProduct(r.Context(), req.ID, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListProducts(w http.ResponseWriter, r *http.Request) {
	var req ListProductsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListProducts(r.Context(), req.TenantID, req.ProductType, req.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) CreateSKU(w http.ResponseWriter, r *http.Request) {
	var req CreateSKURequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sku := &domain.SKU{
		TenantID:  req.TenantID,
		ProductID: req.ProductID,
		Code:      req.Code,
		Name:      req.Name,
		Price:     req.Price,
		Currency:  req.Currency,
		Unit:      req.Unit,
		UnitSize:  req.UnitSize,
		Status:    req.Status,
		CreatedBy: req.CreatedBy,
	}
	result, err := h.svc.CreateSKU(r.Context(), sku)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetSKU(w http.ResponseWriter, r *http.Request) {
	var req IDTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.GetSKU(r.Context(), req.ID, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListProductSKUs(w http.ResponseWriter, r *http.Request) {
	var req ListProductSKUsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.ListProductSKUs(r.Context(), req.ProductID, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) UpdateSKUPrice(w http.ResponseWriter, r *http.Request) {
	var req UpdateSKUPriceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.svc.UpdateSKUPrice(r.Context(), req.ID, req.TenantID, req.Price, req.UpdatedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
