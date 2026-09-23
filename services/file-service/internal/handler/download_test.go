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
// as being for something else.
//
// Comments are stripped first, because a check that searched the whole file
// would be satisfied by this paragraph, which names both constants and pins
// nothing.
func TestTheSignedAndTheServedPathAgree(t *testing.T) {
	signing := withoutComments(read(t, "../service/domain_logic.go"))

	for _, tc := range []struct{ name, here, want string }{
		{"path", DownloadPath, `downloadPath   = "` + DownloadPath + `"`},
		{"purpose", Purpose, `handlerPurpose = "` + Purpose + `"`},
	} {
		if !strings.Contains(signing, tc.want) {
			t.Errorf("this package serves %s %q and the service package does not sign that "+
				"value.\nLooked for: %s", tc.name, tc.here, tc.want)
		}
	}
}

// A file token and a report token are for different things.
//
// Two services, and a deployment may give them one key. Without distinct
// purposes each would honour the other's links, and the narrower permission
// would be the one that decided nothing.
func TestTheFilePurposeIsNotTheReportPurpose(t *testing.T) {
	reporting := withoutComments(read(t, "../../../reporting-service/internal/handler/download.go"))
	if strings.Contains(reporting, `Purpose = "`+Purpose+`"`) {
		t.Errorf("reporting-service signs its links with the purpose %q as well, so a link "+
			"to one service's resource would be honoured by the other", Purpose)
	}
}

// And the gateway routes and exempts what this service serves.
func TestTheGatewayRoutesTheDownloadPath(t *testing.T) {
	gateway := withoutComments(read(t, "../../../gateway-service/handler/connect_handlers.go"))
	if !strings.Contains(gateway, `"`+DownloadPath+`"`) {
		t.Errorf("the gateway has no route for %q, so a signed link 404s in every deployment",
			DownloadPath)
	}
	auth := withoutComments(read(t, "../../../gateway-service/handler/authentication.go"))
	if !strings.Contains(auth, `"`+DownloadPath+`"`) {
		t.Errorf("the gateway does not exempt %q from session checking, so every signed "+
			"link is refused as not signed in", DownloadPath)
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
	stripped := withoutComments(`a := 1 // downloadPath = "/download/file"` + "\nb := 2\n")
	if strings.Contains(stripped, "downloadPath") {
		t.Errorf("a mention in a comment survived stripping: %q", stripped)
	}
	if !strings.Contains(stripped, "b := 2") {
		t.Errorf("stripping removed code: %q", stripped)
	}
}
