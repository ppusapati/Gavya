package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ppusapati/gavya/services/file-service/internal/service"
	"p9e.in/samavaya/packages/p9log"
)

type Handler struct {
	svc *service.Service
	log *p9log.Helper
}

func New(svc *service.Service, log *p9log.Helper) *Handler {
	return &Handler{svc: svc, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/file.v1.FileService/CreateFileRecord", h.createFileRecord)
	mux.HandleFunc("/file.v1.FileService/GetFileRecord", h.getFileRecord)
	mux.HandleFunc("/file.v1.FileService/ListEntityFiles", h.listEntityFiles)
	mux.HandleFunc("/file.v1.FileService/DeleteFile", h.deleteFile)
	mux.HandleFunc("/file.v1.FileService/GetDownloadURL", h.getDownloadURL)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) createFileRecord(w http.ResponseWriter, r *http.Request) {
	var req struct {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := h.svc.CreateFileRecord(r.Context(), req.TenantID, req.OriginalName, req.StoredName,
		req.ContentType, req.SizeBytes, req.StoragePath, req.EntityType, req.EntityID,
		req.UploadedBy, req.IsPublic, req.CreatedBy)
	if err != nil {
		h.log.Errorf("CreateFileRecord: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(f)
}

func (h *Handler) getFileRecord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := h.svc.GetFileRecord(r.Context(), req.ID, req.TenantID)
	if err != nil {
		h.log.Errorf("GetFileRecord: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(f)
}

func (h *Handler) listEntityFiles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID   string `json:"tenant_id"`
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	files, err := h.svc.ListEntityFiles(r.Context(), req.TenantID, req.EntityType, req.EntityID)
	if err != nil {
		h.log.Errorf("ListEntityFiles: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		TenantID  string `json:"tenant_id"`
		UpdatedBy string `json:"updated_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.svc.DeleteFile(r.Context(), req.ID, req.TenantID, req.UpdatedBy); err != nil {
		h.log.Errorf("DeleteFile: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getDownloadURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	url, err := h.svc.GetDownloadURL(r.Context(), req.ID, req.TenantID)
	if err != nil {
		h.log.Errorf("GetDownloadURL: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": url})
}
