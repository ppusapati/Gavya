package signedurl

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Serving a signed link.
//
// The response headers are here rather than in each service because getting
// them wrong is silent and getting them wrong differently in two services is
// worse. Two of them are doing real work:
//
//   - X-Content-Type-Options: nosniff. A report is a tenant's own text, and a
//     browser allowed to guess at content it was handed may decide a CSV whose
//     first cell begins with a tag is HTML — at which point a producer name
//     somebody typed is script running on this platform's origin. The header
//     is the one-line fix and it only works if it is always there.
//   - Content-Disposition: attachment. The same defence from the other side,
//     and the reason a download lands in the downloads folder with the name
//     the service chose rather than opening in the tab.
//
// The filename is escaped rather than interpolated. It arrives from a service
// that built it, not from a person, and that is exactly the kind of assumption
// that stops being true — a quote or a newline in a header value is a response
// somebody else gets to finish writing.

// Content is a file about to be handed over.
type Content struct {
	// Filename is what to save it as.
	Filename string
	// ContentType is what it is. Empty is refused rather than guessed: a
	// missing type is what invites the browser to sniff, which is the thing
	// nosniff is here to stop.
	ContentType string
	// Length is how many bytes Body will produce. Sent as Content-Length so a
	// truncated transfer is a broken download rather than a short file that
	// opens.
	Length int64
	// Body is the bytes. Closed if it is a Closer, so a caller handing over an
	// open file does not have to.
	Body io.Reader
	// Modified, when known, so a browser and a proxy can avoid fetching it
	// twice. Zero omits the header rather than sending the epoch.
	Modified time.Time
}

// Serve writes a download.
//
// Nothing is written until the headers are settled, so a failure to prepare is
// a clean error rather than half a file with a 200 in front of it. Once the
// body starts, a failure cannot be reported — the status is already sent — so
// it is logged by the caller and the download simply stops short, which
// Content-Length lets the other end notice.
func Serve(w http.ResponseWriter, r *http.Request, c Content) error {
	if closer, ok := c.Body.(io.Closer); ok {
		defer closer.Close()
	}
	if c.Body == nil {
		return errors.New("signedurl: nothing to serve")
	}
	if strings.TrimSpace(c.ContentType) == "" {
		return errors.New("signedurl: a download needs a content type; an absent one is what " +
			"invites a browser to guess, which is what nosniff is here to prevent")
	}

	h := w.Header()
	h.Set("Content-Type", c.ContentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", disposition(c.Filename))
	if c.Length > 0 {
		h.Set("Content-Length", strconv.FormatInt(c.Length, 10))
	}
	if !c.Modified.IsZero() {
		h.Set("Last-Modified", c.Modified.UTC().Format(http.TimeFormat))
	}
	// A signed link is a bearer credential with an expiry. A shared cache
	// holding the answer would serve it to whoever asks next from the same
	// proxy, after the link itself has stopped working.
	h.Set("Cache-Control", "private, no-store")

	if r != nil && r.Method == http.MethodHead {
		// Everything above, and no body. A HEAD that streamed the file would
		// be a download nobody asked for.
		w.WriteHeader(http.StatusOK)
		return nil
	}

	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, c.Body)
	return err
}

// disposition builds the Content-Disposition header.
//
// Two spellings of the name, which is what RFC 6266 asks for: a plain one for
// anything that does not understand the encoded form, and a UTF-8 one for a
// name with a character outside ASCII in it. The plain one is reduced to
// characters that cannot end a quoted string or start a new header line, so a
// filename can never finish this header early.
func disposition(filename string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		name = "download"
	}

	var plain strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			plain.WriteRune(r)
		default:
			// Everything else, including the quote, the backslash, the
			// semicolon, and every control character. Replaced rather than
			// dropped so two different names cannot reduce to one.
			plain.WriteRune('_')
		}
	}
	safe := plain.String()
	if strings.Trim(safe, "_.") == "" {
		safe = "download"
	}

	// url.PathEscape leaves a few characters this header's grammar does not
	// want; the encoded form is percent-encoding, so escaping them too is both
	// correct and stricter than needed.
	encoded := strings.NewReplacer("'", "%27", "(", "%28", ")", "%29", "*", "%2A").
		Replace(url.PathEscape(name))

	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", safe, encoded)
}

// Status maps a verification failure onto an HTTP status.
//
// An expired link is 410 Gone rather than 403, and the distinction is the whole
// point: a person following a link from last week has done nothing wrong and
// should be told the link is old, not that they are not allowed. Told the
// second thing they go and ask for an account they already have.
func Status(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, ErrExpired):
		return http.StatusGone, "This link has expired. Ask for a new one."
	case errors.Is(err, ErrWrongPurpose):
		return http.StatusForbidden, "This link is for something else."
	case errors.Is(err, ErrUnknownKey):
		return http.StatusForbidden,
			"This link was signed with a key this platform no longer holds. Ask for a new one."
	case errors.Is(err, ErrBadSignature), errors.Is(err, ErrMalformed):
		return http.StatusForbidden, "This link is not one this platform issued."
	default:
		return http.StatusInternalServerError, "This link could not be checked."
	}
}

// Refuse answers a request that will not be served.
//
// Plain text, not JSON: whatever is on the other end of a download link is a
// browser window or a terminal rather than this platform's own client, and a
// person reading it wants a sentence.
func Refuse(w http.ResponseWriter, err error) {
	status, message := Status(err)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, message+"\n")
}

// Token reads the token out of a request.
//
// One place to name the parameter, so the service that signs and the service
// that verifies cannot disagree about it.
const TokenParam = "t"

// TokenFrom reads the token a request carries.
func TokenFrom(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get(TokenParam))
}

// Link builds the URL a caller should be handed.
//
// base may be empty, in which case the result is root-relative. That is the
// right answer for a console which is already talking to this platform through
// a gateway, and the wrong one for a link somebody emails — so a deployment
// that wants absolute links sets the base and a deployment that does not gets
// something that still works in the place most links are used.
func Link(base, path, token string) string {
	link := path + "?" + TokenParam + "=" + url.QueryEscape(token)
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	if trimmed == "" {
		return link
	}
	return trimmed + link
}
