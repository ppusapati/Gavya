package handler

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/signedurl"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
)

// Serving a report to a link.
//
// Every other route in this service is a Connect procedure behind a session.
// This one is not, and the difference is the whole point: a browser following
// an <a href> sends no Authorization header, and neither does curl or whoever
// the link was forwarded to.
//
// So the authority is the token, and the token alone. The gateway lets this
// path through without a session — deliberately — which means every header on
// the request is whatever the caller typed, including any that names a tenant.
// The tenant used below comes out of the verified grant and from nowhere else.
// That is not a style preference: reading a tenant from a header here would be
// a link to one society's report that fetches another's by editing a header.

// DownloadPath is where a signed report link points.
//
// Named here and used by the code that signs, so the two cannot disagree. The
// gateway routes the prefix to this service.
const DownloadPath = "/download/report"

// Purpose is what a report token says it is for.
//
// A token for a report cannot fetch a file: file-service serves its own
// downloads under its own purpose, and without this a deployment that gave the
// two services one key would have each honour the other's links.
const Purpose = "report"

// RegisterDownload adds the signed-link route.
//
// Separate from Register because it is a different kind of thing — not a
// procedure, not behind a session, not in the permission table — and a reader
// looking at the route list should see that rather than have it hidden among
// twenty Connect handlers.
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
		// A link cannot have been issued either, so this is a deployment that
		// is half configured rather than a caller doing anything wrong.
		h.svc.Log().Errorf("a download was attempted and DOWNLOAD_SIGNING_KEY is not set")
		signedurl.Refuse(w, errors.New("downloads are not configured on this deployment"))
		return
	}

	grant, err := keys.ForPurpose(signedurl.TokenFrom(r), Purpose, time.Now())
	if err != nil {
		// Not logged as an error. An expired link is ordinary — links expire,
		// and somebody following one from last week has done nothing worth an
		// alert. A forged one is worth seeing, and is logged as itself.
		if !errors.Is(err, signedurl.ErrExpired) {
			h.svc.Log().Errorf("a download link was refused: %v", err)
		}
		signedurl.Refuse(w, err)
		return
	}

	// The tenant is the grant's. Set on the context so the row is read under
	// the policies that belong to it, exactly as a session-scoped read would
	// be — the authority is different, the scoping is not.
	ctx := tenantdb.WithTenant(r.Context(), grant.TenantID)

	file, err := h.svc.DownloadReport(ctx, grant.Resource, grant.TenantID)
	if err != nil {
		// A genuine token for a report that has since been deleted, or that
		// failed. Answered as not-found rather than as a refusal: the link was
		// real, and there is nothing behind it now.
		h.svc.Log().Errorf("a valid link for report %s could not be served: %v", grant.Resource, err)
		http.Error(w, "This report is no longer available.", http.StatusNotFound)
		return
	}

	produced := file.UpdatedAt
	if file.CompletedAt != nil {
		produced = *file.CompletedAt
	}
	if err := signedurl.Serve(w, r, signedurl.Content{
		// Built by the service from the type and the identifier, never from
		// the report's name — a name is free text somebody typed and a
		// filename assembled from free text is how a download writes
		// somewhere nobody meant.
		Filename:    filenameFor(file.ReportType, file.ID, file.FileFormat),
		ContentType: file.ContentType,
		Length:      int64(len(file.Content)),
		Body:        bytes.NewReader(file.Content),
		Modified:    produced,
	}); err != nil {
		// The status and headers are already sent, so this cannot be reported
		// to the caller. Content-Length is what lets the other end notice that
		// what arrived is short.
		h.svc.Log().Errorf("a report download stopped part way: %v", err)
	}
}
