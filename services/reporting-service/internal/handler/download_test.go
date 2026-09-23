package handler

import (
	"os"
	"strings"
	"testing"
)

// The path and purpose are written twice, and the two copies must agree.
//
// The service package signs a link and this package serves it. The handler
// imports the service, so the service cannot import the handler, so the two
// constants are declared in both places — which is the arrangement that ends
// with a link pointing at a route nothing serves, or a token this route refuses
// as being for something else. Neither failure says anything useful: one is a
// 404 on a link that looks right, the other is "this link is for something
// else" about a link that is for exactly this.
//
// So the source is read and the values compared. Comments are stripped first,
// because a check that searched the whole file would be satisfied by this
// paragraph, which names both constants and pins nothing.
func TestTheSignedAndTheServedPathAgree(t *testing.T) {
	signing := withoutComments(read(t, "../service/domain_logic.go"))

	for _, tc := range []struct{ name, here, want string }{
		{"path", DownloadPath, `downloadPath   = "` + DownloadPath + `"`},
		{"purpose", Purpose, `handlerPurpose = "` + Purpose + `"`},
	} {
		if !strings.Contains(signing, tc.want) {
			t.Errorf("this package serves %s %q and the service package does not sign that "+
				"value; a link would point at a route nothing serves, or carry a purpose "+
				"this route refuses.\nLooked for: %s", tc.name, tc.here, tc.want)
		}
	}
}

// And the gateway routes what this service serves.
//
// The download route is not a Connect procedure, so none of the checks that
// compare the permission table against the registered routes covers it. A path
// served here and not routed there is a link that works when the service is
// called directly and 404s through the gateway — which is every deployment.
func TestTheGatewayRoutesTheDownloadPath(t *testing.T) {
	gateway := withoutComments(read(t, "../../../gateway-service/handler/connect_handlers.go"))
	if !strings.Contains(gateway, `"`+DownloadPath+`"`) {
		t.Errorf("the gateway has no route for %q, so a signed link 404s in every deployment "+
			"and works only when this service is called directly", DownloadPath)
	}

	// And it is let through without a session, which is the point of a signed
	// link: a browser following an <a href> sends no Authorization header.
	auth := withoutComments(read(t, "../../../gateway-service/handler/authentication.go"))
	if !strings.Contains(auth, `"`+DownloadPath+`"`) {
		t.Errorf("the gateway does not exempt %q from session checking, so every signed "+
			"link is refused as not signed in — which is the one thing a signed link "+
			"exists to avoid", DownloadPath)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// withoutComments removes // and /* */ comments, so a check reads the code.
func withoutComments(src string) string {
	var out strings.Builder
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				return out.String()
			}
			i += end
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return out.String()
			}
			i += end + 4
		default:
			out.WriteByte(src[i])
			i++
		}
	}
	return out.String()
}

func TestTheSourceCheckReadsCodeRatherThanComments(t *testing.T) {
	stripped := withoutComments(`a := 1 // downloadPath = "/download/report"` + "\nb := 2\n")
	if strings.Contains(stripped, "downloadPath") {
		t.Errorf("a mention in a comment survived stripping: %q", stripped)
	}
	if !strings.Contains(stripped, "b := 2") {
		t.Errorf("stripping removed code: %q", stripped)
	}
}

// A filename is built from the type and the identifier, never from the name.
//
// A report's name is free text somebody typed. Interpolated into a
// Content-Disposition it is a header somebody else gets to finish writing, and
// interpolated into a path it is a download that writes somewhere nobody meant.
func TestAFilenameIsBuiltFromWhatTheServiceKnows(t *testing.T) {
	for _, tc := range []struct{ kind, id, format string }{
		{"collections", "RPT_1", "csv"},
		{"../../etc/passwd", "RPT_1", "csv"},
		{"collections", "RPT/1", "csv"},
		{"collections", "RPT_1", "../sh"},
		{"", "", ""},
	} {
		got := filenameFor(tc.kind, tc.id, tc.format)
		if strings.ContainsAny(got, `/\."`+"\r\n ") && !strings.HasSuffix(got, "."+lastExt(got)) {
			t.Errorf("filenameFor(%q,%q,%q) = %q holds a separator", tc.kind, tc.id, tc.format, got)
		}
		if strings.Contains(got, "..") {
			t.Errorf("filenameFor(%q,%q,%q) = %q still traverses", tc.kind, tc.id, tc.format, got)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("filenameFor(%q,%q,%q) = %q holds a path separator", tc.kind, tc.id, tc.format, got)
		}
	}

	if got := filenameFor("collections", "RPT_1", "csv"); got != "collections_RPT_1.csv" {
		t.Errorf("the ordinary case reads %q", got)
	}
}

func lastExt(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return ""
}
