package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/signedurl"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
)

// Serving a file to a link.
//
// The same arrangement as reporting-service's: the gateway lets this path
// through without a session, so the authority is the token and the tenant comes
// out of the verified grant rather than from any header on the request.
//
// What differs is where the bytes are. reporting-service holds a report in the
// row it produced; this service holds nothing at all — it records where
// something else put a file, and opens that. Every check on the path runs here,
// at the moment the file is opened, rather than being trusted from when the
// link was issued: a store is a filesystem something else writes to, and what
// was a file an hour ago can be a symlink out of the store now.

// DownloadPath is where a signed file link points.
const DownloadPath = "/download/file"

// Purpose is what a file token says it is for.
//
// A token for a file cannot fetch a report, and the other way round. Without
// it, a deployment that gave the two services one key would have each honour
// the other's links.
const Purpose = "file"

// RegisterDownload adds the signed-link route.
//
// Separate from Register because it is a different kind of thing — not a
// procedure, not behind a session, not in the permission table.
func (h *Handler) RegisterDownload(mux *http.ServeMux) {
	mux.HandleFunc(DownloadPath, h.download)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "a download link is fetched, not posted", http.StatusMethodNotAllowed)
		return
	}

	keys := h.svc.SigningKeys()
	if keys == nil {
		h.svc.Log().Errorf("a download was attempted and DOWNLOAD_SIGNING_KEY is not set")
		signedurl.Refuse(w, errors.New("downloads are not configured on this deployment"))
		return
	}

	grant, err := keys.ForPurpose(signedurl.TokenFrom(r), Purpose, time.Now())
	if err != nil {
		// An expired link is ordinary and is not logged as a fault. A forged
		// one is worth seeing.
		if !errors.Is(err, signedurl.ErrExpired) {
			h.svc.Log().Errorf("a download link was refused: %v", err)
		}
		signedurl.Refuse(w, err)
		return
	}

	ctx := tenantdb.WithTenant(r.Context(), grant.TenantID)

	file, record, err := h.svc.OpenForDownload(ctx, grant.Resource, grant.TenantID)
	if err != nil {
		// Logged, because a valid link that cannot be served is a record and a
		// store that have come apart, and somebody should know which.
		h.svc.Log().Errorf("a valid link for file %s could not be served: %v", grant.Resource, err)
		http.Error(w, "This file is no longer available.", http.StatusNotFound)
		return
	}

	// The length from the file on disk rather than from the record.
	//
	// size_bytes is what the caller said when it registered the record, and
	// this service never measured it. Sent as Content-Length it would be a
	// promise about somebody else's number: too small truncates a file that is
	// fine, too large hangs a browser waiting for bytes that are not coming.
	var length int64
	if info, statErr := file.Stat(); statErr == nil {
		length = info.Size()
	}

	contentType := record.ContentType
	if contentType == "" {
		// Also the caller's word, and it may not have given one. A download
		// with no type is what invites a browser to sniff, so an unknown type
		// is named as unknown rather than left out — and it is served with
		// nosniff and as an attachment either way.
		contentType = "application/octet-stream"
	}

	if err := signedurl.Serve(w, r, signedurl.Content{
		// The name the person who uploaded it used, which is the one they
		// expect to see in their downloads folder. It is free text and it is
		// escaped by Serve rather than trusted here.
		Filename:    record.OriginalName,
		ContentType: contentType,
		Length:      length,
		Body:        file,
		Modified:    record.UpdatedAt,
	}); err != nil {
		h.svc.Log().Errorf("a file download stopped part way: %v", err)
	}
}
