package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/services/file-service/internal/domain"
	"github.com/ppusapati/gavya/services/file-service/internal/repository"
	"github.com/ppusapati/gavya/services/file-service/internal/service"
)

// ServiceName is the fully qualified Connect service these procedures are
// addressed under.
const ServiceName = "file.v1.FileService"

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

	route("CreateFileRecord", connectjson.Unary(h.CreateFileRecord))
	route("GetFileRecord", connectjson.Unary(h.GetFileRecord))
	route("ListEntityFiles", connectjson.Unary(h.ListEntityFiles))
	route("DeleteFile", connectjson.Unary(h.DeleteFile))
	route("GetDownloadURL", connectjson.Unary(h.GetDownloadURL))
}

// classify maps a failure onto the code that describes it.
//
// Reporting everything as internal, as this service used to, leaves a caller
// unable to tell a missing file record from an unreachable database — and makes
// an unrecoverable mistake look like something worth retrying.
func classify(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, service.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

type CreateFileRecordRequest struct {
	TenantID     string `json:"tenant_id"`
	OriginalName string `json:"original_name"`
	StoredName   string `json:"stored_name"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	StoragePath  string `json:"storage_path"`
	EntityType   string `json:"entity_type"`
	EntityID     string `json:"entity_id"`
	UploadedBy   string `json:"uploaded_by"`
	IsPublic     bool   `json:"is_public"`
	CreatedBy    string `json:"created_by"`
}

type FileRecordResponse struct {
	File *domain.FileRecord `json:"file"`
}

type GetFileRecordRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type ListEntityFilesRequest struct {
	TenantID   string `json:"tenant_id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
}

type ListEntityFilesResponse struct {
	Files []*domain.FileRecord `json:"files"`
}

type DeleteFileRequest struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}

type DeleteFileResponse struct{}

type GetDownloadURLRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type GetDownloadURLResponse struct {
	URL string `json:"url"`
}

func (h *Handler) CreateFileRecord(ctx context.Context, req *connect.Request[CreateFileRecordRequest]) (*connect.Response[FileRecordResponse], error) {
	m := req.Msg
	out, err := h.svc.CreateFileRecord(ctx, m.TenantID, m.OriginalName, m.StoredName,
		m.ContentType, m.SizeBytes, m.StoragePath, m.EntityType, m.EntityID,
		m.UploadedBy, m.IsPublic, m.CreatedBy)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FileRecordResponse{File: out}), nil
}

func (h *Handler) GetFileRecord(ctx context.Context, req *connect.Request[GetFileRecordRequest]) (*connect.Response[FileRecordResponse], error) {
	out, err := h.svc.GetFileRecord(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&FileRecordResponse{File: out}), nil
}

func (h *Handler) ListEntityFiles(ctx context.Context, req *connect.Request[ListEntityFilesRequest]) (*connect.Response[ListEntityFilesResponse], error) {
	m := req.Msg
	out, err := h.svc.ListEntityFiles(ctx, m.TenantID, m.EntityType, m.EntityID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&ListEntityFilesResponse{Files: out}), nil
}

func (h *Handler) DeleteFile(ctx context.Context, req *connect.Request[DeleteFileRequest]) (*connect.Response[DeleteFileResponse], error) {
	m := req.Msg
	if err := h.svc.DeleteFile(ctx, m.ID, m.TenantID, m.UpdatedBy); err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&DeleteFileResponse{}), nil
}

func (h *Handler) GetDownloadURL(ctx context.Context, req *connect.Request[GetDownloadURLRequest]) (*connect.Response[GetDownloadURLResponse], error) {
	url, err := h.svc.GetDownloadURL(ctx, req.Msg.ID, req.Msg.TenantID)
	if err != nil {
		return nil, classify(err)
	}
	return connect.NewResponse(&GetDownloadURLResponse{URL: url}), nil
}
