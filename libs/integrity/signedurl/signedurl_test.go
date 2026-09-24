package signedurl

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func secret(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, MinSecretLength)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func ring(t *testing.T) *Keyring {
	t.Helper()
	k, err := NewKeyring(Key{ID: "k1", Secret: secret(t)})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

var now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func grant() Grant {
	return Grant{
		Purpose: "report", TenantID: "TEN_VALLEY", Resource: "RPT_1",
		Expires: now.Add(time.Hour),
	}
}

func TestATokenRoundTrips(t *testing.T) {
	k := ring(t)
	token, err := k.Sign(grant(), now)
	if err != nil {
		t.Fatal(err)
	}

	got, err := k.Verify(token, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Purpose != "report" || got.TenantID != "TEN_VALLEY" || got.Resource != "RPT_1" {
		t.Errorf("the grant came back as %+v", got)
	}
	if !got.Expires.Equal(now.Add(time.Hour)) {
		t.Errorf("expiry came back as %s", got.Expires)
	}
}

// Every single character of a token, changed to every other character, is
// refused — with no run of luck involved.
//
// The end-to-end suite already changed the last character of a link and asked
// for a refusal, and that is how this was found. It is also why it took until
// the first CI run to find it: a 32-byte signature is 43 base64 characters,
// 258 bits carrying 256, so the final character has two bits encoding nothing.
// With the default decoder those two bits are ignored, four final characters
// decode to the same signature, and the end-to-end test only failed when it
// happened to pick one of the four — 4 times in 64, and it had won that toss
// on every run before.
//
// A test that finds a defect one run in sixteen is not a guard, so the
// exhaustive version lives here where it is cheap and deterministic: every
// position, every replacement, 64 characters wide. It fails on the lenient
// decoder at the final position and passes on the strict one.
//
// The defect was never a way past the signature — all four spellings carry the
// same signature over the same payload — but it meant one grant had four
// tokens, and a token that is not canonical cannot be logged, de-duplicated or
// revoked as one thing.
func TestNoSingleCharacterChangeIsEverAccepted(t *testing.T) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	k := ring(t)
	token, err := k.Sign(grant(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.Verify(token, now); err != nil {
		t.Fatalf("the untouched token does not verify: %v", err)
	}

	accepted := 0
	for i := range len(token) {
		for _, c := range []byte(alphabet) {
			if token[i] == c {
				continue
			}
			altered := []byte(token)
			altered[i] = c
			if _, err := k.Verify(string(altered), now); err == nil {
				accepted++
				if accepted <= 5 {
					t.Errorf("position %d of %d changed from %q to %q and the token "+
						"was still accepted", i, len(token), token[i], c)
				}
			}
		}
	}
	if accepted > 5 {
		t.Errorf("... and %d more single-character changes were accepted", accepted-5)
	}
}

// Changing any part of a token breaks it.
//
// This is the property the whole package exists for. Each case alters one
// field, leaves everything else alone, and requires that the result is refused
// — because each alteration, accepted, is a different way to fetch something
// nobody issued a link for.
func TestAnAlteredTokenIsRefused(t *testing.T) {
	k := ring(t)
	honest, err := k.Sign(grant(), now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(honest, ".")

	rebuild := func(i int, with string) string {
		p := append([]string{}, parts...)
		p[i] = with
		return strings.Join(p, ".")
	}

	// A second tenant's token, honestly signed, to lift fields out of.
	othersGrant := grant()
	othersGrant.TenantID = "TEN_HILL"
	others, err := k.Sign(othersGrant, now)
	if err != nil {
		t.Fatal(err)
	}
	othersParts := strings.Split(others, ".")

	for _, tc := range []struct {
		name  string
		token string
		want  error
	}{
		{"a different version", rebuild(0, "v2"), ErrMalformed},
		{"a key nobody holds", rebuild(1, "k9"), ErrUnknownKey},
		{"a payload from another tenant's token", rebuild(2, othersParts[2]), ErrBadSignature},
		{"a signature from another tenant's token", rebuild(3, othersParts[3]), ErrBadSignature},
		{"one byte of the signature", rebuild(3, flip(parts[3])), ErrBadSignature},
		{"one byte of the payload", rebuild(2, flip(parts[2])), ErrBadSignature},
		{"a field appended", honest + "x", ErrBadSignature},
		{"a part removed", strings.Join(parts[:3], "."), ErrMalformed},
		{"nothing at all", "", ErrMalformed},
		{"something that is not a token", "have-this-report-please", ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := k.Verify(tc.token, now)
			if err == nil {
				t.Fatalf("a token with %s was accepted", tc.name)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("refused with %v, want %v", err, tc.want)
			}
		})
	}
}

// flip changes one character of a base64 string without changing its length.
func flip(s string) string {
	if s == "" {
		return "A"
	}
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}

// A token stops working when it says it will.
func TestAnExpiredTokenIsRefusedAndSaysSo(t *testing.T) {
	k := ring(t)
	token, err := k.Sign(grant(), now)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := k.Verify(token, now.Add(59*time.Minute)); err != nil {
		t.Fatalf("a token an hour old was refused after 59 minutes: %v", err)
	}
	// Exactly at the expiry is expired: the field says when it stops working.
	if _, err := k.Verify(token, now.Add(time.Hour)); !errors.Is(err, ErrExpired) {
		t.Errorf("at its stated expiry the token gave %v, want ErrExpired", err)
	}
	if _, err := k.Verify(token, now.Add(2*time.Hour)); !errors.Is(err, ErrExpired) {
		t.Errorf("an hour past its expiry the token gave %v", err)
	}
}

// A tampered token is never reported as merely expired.
//
// The order matters: reporting expiry first would tell somebody altering a
// token that everything else about it verified, which is a hint about what to
// change next.
func TestATamperedTokenIsNotReportedAsExpired(t *testing.T) {
	k := ring(t)
	g := grant()
	g.Expires = now.Add(time.Minute)
	token, err := k.Sign(g, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	parts[2] = flip(parts[2])

	_, err = k.Verify(strings.Join(parts, "."), now.Add(time.Hour))
	if errors.Is(err, ErrExpired) {
		t.Error("an altered token past its expiry was reported as expired, which says every " +
			"other part of it verified")
	}
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("refused with %v, want ErrBadSignature", err)
	}
}

// A token for one thing cannot fetch another.
func TestPurposeTenantAndResourceAreAllBound(t *testing.T) {
	k := ring(t)
	token, err := k.Sign(grant(), now)
	if err != nil {
		t.Fatal(err)
	}

	// The purpose check is offered by the package so each caller cannot forget
	// it, and forgetting is invisible: the token verifies and the resource
	// loads, and the only thing wrong is which kind of thing it was for.
	if _, err := k.ForPurpose(token, "file", now); !errors.Is(err, ErrWrongPurpose) {
		t.Errorf("a report token passed a file check: %v", err)
	}
	if _, err := k.ForPurpose(token, "report", now); err != nil {
		t.Errorf("a report token failed a report check: %v", err)
	}

	// And the two other bindings come back for the caller to compare against
	// what it is about to serve.
	g, err := k.Verify(token, now)
	if err != nil {
		t.Fatal(err)
	}
	if g.TenantID != "TEN_VALLEY" {
		t.Errorf("tenant is %q", g.TenantID)
	}
	if g.Resource != "RPT_1" {
		t.Errorf("resource is %q", g.Resource)
	}
}

// Two grants must never sign identically.
//
// The fields are joined before they are signed, so a delimiter appearing inside
// one would let a tenant ending in it and a resource beginning with it produce
// the same bytes as a different pair — a signature that verifies for something
// nobody issued a token for. Refused at signing, which is the only place it can
// be caught.
func TestAFieldHoldingTheDelimiterIsRefused(t *testing.T) {
	k := ring(t)
	for _, g := range []Grant{
		{Purpose: "report" + separator + "x", TenantID: "T", Resource: "R", Expires: now.Add(time.Hour)},
		{Purpose: "report", TenantID: "T" + separator, Resource: "R", Expires: now.Add(time.Hour)},
		{Purpose: "report", TenantID: "T", Resource: separator + "R", Expires: now.Add(time.Hour)},
	} {
		if _, err := k.Sign(g, now); err == nil {
			t.Errorf("a grant holding the delimiter was signed: %+v", g)
		}
	}

	// And the pair that would have collided does not.
	a, err := k.Sign(Grant{Purpose: "report", TenantID: "TEN", Resource: "AB",
		Expires: now.Add(time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.Sign(Grant{Purpose: "report", TenantID: "TENA", Resource: "B",
		Expires: now.Add(time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two different grants produced the same token")
	}
}

// A grant with a hole in it is refused rather than signed.
func TestAnIncompleteGrantIsRefused(t *testing.T) {
	k := ring(t)
	for _, tc := range []struct {
		name string
		g    Grant
	}{
		{"no purpose", Grant{TenantID: "T", Resource: "R", Expires: now.Add(time.Hour)}},
		{"no tenant", Grant{Purpose: "report", Resource: "R", Expires: now.Add(time.Hour)}},
		{"no resource", Grant{Purpose: "report", TenantID: "T", Expires: now.Add(time.Hour)}},
		{"already expired", Grant{Purpose: "report", TenantID: "T", Resource: "R",
			Expires: now.Add(-time.Minute)}},
	} {
		if _, err := k.Sign(tc.g, now); err == nil {
			t.Errorf("a grant with %s was signed", tc.name)
		}
	}
}

// A link cannot be made to last for ever, and a caller asking for longer is
// told rather than quietly given less.
func TestLifetimeIsBounded(t *testing.T) {
	k := ring(t)

	g := grant()
	g.Expires = now.Add(MaxLifetime + time.Minute)
	if _, err := k.Sign(g, now); err == nil {
		t.Error("a link lasting longer than the maximum was signed")
	} else if !strings.Contains(err.Error(), MaxLifetime.String()) {
		t.Errorf("the refusal does not say what the maximum is: %v", err)
	}

	// Exactly the maximum is allowed.
	g.Expires = now.Add(MaxLifetime)
	if _, err := k.Sign(g, now); err != nil {
		t.Errorf("a link lasting exactly the maximum was refused: %v", err)
	}

	// And a zero expiry is the default rather than an error or for ever.
	token, err := k.Sign(Grant{Purpose: "report", TenantID: "T", Resource: "R"}, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := k.Verify(token, now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Expires.Equal(now.Add(DefaultLifetime)) {
		t.Errorf("a grant with no expiry got %s, want %s",
			got.Expires, now.Add(DefaultLifetime))
	}
}

// A key can be rotated without breaking the links already sent.
//
// A key changed with no overlap invalidates every link in flight, which
// somebody discovers as a morning of reports that will not open.
func TestAKeyCanBeRotatedWithoutBreakingLinksInFlight(t *testing.T) {
	old := Key{ID: "k1", Secret: secret(t)}
	oldRing, err := NewKeyring(old)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := oldRing.Sign(grant(), now)
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := NewKeyring(Key{ID: "k2", Secret: secret(t)}, old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.Verify(issued, now); err != nil {
		t.Errorf("a link issued before the rotation stopped working: %v", err)
	}

	// New links are signed with the new key.
	fresh, err := rotated.Sign(grant(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fresh, version+".k2.") {
		t.Errorf("a new link was signed with %q", fresh)
	}

	// And once the old key is dropped, its links stop — which is the only way
	// to cancel one early, and cancels every one of them.
	only, err := NewKeyring(Key{ID: "k2", Secret: secret(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := only.Verify(issued, now); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("after dropping the old key its links gave %v", err)
	}
}

func TestAKeyringRefusesWhatItCannotUse(t *testing.T) {
	good := Key{ID: "k1", Secret: secret(t)}

	if _, err := NewKeyring(Key{ID: "k1", Secret: []byte("short")}); err == nil {
		t.Error("a key shorter than the minimum was accepted")
	}
	if _, err := NewKeyring(Key{ID: "", Secret: secret(t)}); err == nil {
		t.Error("a key with no id was accepted")
	}
	if _, err := NewKeyring(Key{ID: "has.dot", Secret: secret(t)}); err == nil {
		t.Error("a key id holding the token delimiter was accepted")
	}
	if _, err := NewKeyring(good, Key{ID: "k1", Secret: secret(t)}); err == nil {
		t.Error("two keys called k1 were accepted, so a token's key id no longer says " +
			"which one signed it")
	}
}

// A nil keyring refuses rather than panicking.
//
// It is what a service with no key configured holds, and a signing call on it
// is a procedure somebody invoked before the deployment was finished.
func TestANilKeyringRefuses(t *testing.T) {
	var k *Keyring
	if _, err := k.Sign(grant(), now); err == nil {
		t.Error("a keyring with no key signed something")
	}
	if _, err := k.Verify("v1.k1.a.b", now); err == nil {
		t.Error("a keyring with no key verified something")
	}
}

// ---------------------------------------------------------------------------
// Serving
// ---------------------------------------------------------------------------

func TestServeSetsTheHeadersThatMatter(t *testing.T) {
	rec := httptest.NewRecorder()
	body := []byte("collected_on,producer_ref\n2026-09-01,PRD_1\n")

	err := Serve(rec, httptest.NewRequest(http.MethodGet, "/download/report?t=x", nil), Content{
		Filename: "collections_RPT_1.csv", ContentType: "text/csv; charset=utf-8",
		Length: int64(len(body)), Body: bytes.NewReader(body),
	})
	if err != nil {
		t.Fatal(err)
	}

	res := rec.Result()
	defer res.Body.Close()

	// The two that are doing real work. A report is a tenant's own text, and a
	// browser allowed to guess may decide a CSV beginning with a tag is HTML.
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options is %q; without it a producer name somebody typed "+
			"can become script on this platform's origin", got)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("Content-Disposition is %q, which does not force a download", got)
	}
	// A signed link is a bearer credential with an expiry; a shared cache
	// holding the answer serves it after the link has stopped working.
	if got := res.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control is %q", got)
	}
	if got := res.Header.Get("Content-Length"); got != "43" {
		t.Errorf("Content-Length is %q, want 43", got)
	}

	read, _ := io.ReadAll(res.Body)
	if string(read) != string(body) {
		t.Errorf("the body came back as %q", read)
	}
}

// A filename can never finish the header early.
//
// It arrives from a service that built it rather than from a person, and that
// is exactly the kind of assumption that stops being true. A quote or a newline
// in a header value is a response somebody else gets to finish writing.
func TestAFilenameCannotEscapeItsHeader(t *testing.T) {
	for _, name := range []string{
		`report".csv`,
		"report\r\nSet-Cookie: a=b",
		"report\n\nHTTP/1.1 200 OK",
		`../../etc/passwd`,
		`report; filename="other.exe`,
		"",
		"संग्रह.csv",
	} {
		t.Run(name, func(t *testing.T) {
			got := disposition(name)
			for _, forbidden := range []string{"\r", "\n"} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("the header holds a line break: %q", got)
				}
			}
			// One filename= parameter, and the quoted value closes where it
			// should: everything after the closing quote is this package's own
			// text.
			if strings.Count(got, `filename="`) != 1 {
				t.Fatalf("the header has more than one quoted filename: %q", got)
			}
			quoted := got[strings.Index(got, `filename="`)+len(`filename="`):]
			quoted = quoted[:strings.Index(quoted, `"`)]
			if strings.ContainsAny(quoted, `"\/;`) {
				t.Errorf("the quoted filename holds a character that ends it early: %q", quoted)
			}

			// And a name that reduces to nothing still names something.
			if strings.Contains(got, `filename=""`) {
				t.Errorf("the header names an empty file: %q", got)
			}
		})
	}

	// A name outside ASCII survives in the encoded form, which is what it is
	// for: a producer's name is not ASCII in most of the places this runs.
	got := disposition("संग्रह.csv")
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Errorf("a non-ASCII name has no encoded form: %q", got)
	}
}

func TestServeRefusesWhatItCannotDescribe(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := Serve(rec, nil, Content{Filename: "a.csv", Body: strings.NewReader("x")}); err == nil {
		t.Error("a download with no content type was served; an absent type is what invites " +
			"a browser to guess")
	}
	if err := Serve(rec, nil, Content{Filename: "a.csv", ContentType: "text/csv"}); err == nil {
		t.Error("a download with no body was served")
	}
}

// A HEAD answers with the headers and no file.
func TestHeadDoesNotStreamTheFile(t *testing.T) {
	rec := httptest.NewRecorder()
	err := Serve(rec, httptest.NewRequest(http.MethodHead, "/download/report?t=x", nil), Content{
		Filename: "a.csv", ContentType: "text/csv; charset=utf-8",
		Length: 3, Body: strings.NewReader("abc"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("a HEAD streamed %d bytes", rec.Body.Len())
	}
	if rec.Header().Get("Content-Disposition") == "" {
		t.Error("a HEAD answered without the headers it is asked for")
	}
}

// An expired link reads as expired, not as a refusal.
//
// A person following a link from last week has done nothing wrong. Told they
// are not allowed, they go and ask for an account they already have.
func TestAnExpiredLinkIsGoneRatherThanForbidden(t *testing.T) {
	status, message := Status(ErrExpired)
	if status != http.StatusGone {
		t.Errorf("an expired link answers %d, want %d", status, http.StatusGone)
	}
	if !strings.Contains(strings.ToLower(message), "expired") {
		t.Errorf("the message does not say the link is old: %q", message)
	}

	forged, _ := Status(ErrBadSignature)
	if forged != http.StatusForbidden {
		t.Errorf("an altered link answers %d", forged)
	}
	if ok, _ := Status(nil); ok != http.StatusOK {
		t.Errorf("no error answers %d", ok)
	}
}

func TestRefuseAnswersInPlainText(t *testing.T) {
	rec := httptest.NewRecorder()
	Refuse(rec, ErrExpired)
	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusGone {
		t.Errorf("status is %d", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("content type is %q; whatever is on the other end of a download link is a "+
			"browser window rather than this platform's own client", got)
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("even a refusal needs nosniff; it is %q", got)
	}
}

func TestLinkIsRelativeWithoutABaseAndAbsoluteWithOne(t *testing.T) {
	rel := Link("", "/download/report", "v1.k1.a.b")
	if rel != "/download/report?t=v1.k1.a.b" {
		t.Errorf("relative link is %q", rel)
	}

	abs := Link("https://gavya.example/", "/download/report", "v1.k1.a.b")
	if abs != "https://gavya.example/download/report?t=v1.k1.a.b" {
		t.Errorf("absolute link is %q", abs)
	}

	// A token is URL-safe base64 and needs no escaping, but the escaping has to
	// be there for the day it is not.
	escaped := Link("", "/download/report", "a b&c=d")
	if strings.Contains(escaped[len("/download/report?t="):], "&") {
		t.Errorf("a token was not escaped into the query: %q", escaped)
	}
}

func TestTokenFrom(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/download/report?t=%20v1.k1.a.b%20", nil)
	if got := TokenFrom(r); got != "v1.k1.a.b" {
		t.Errorf("token read as %q", got)
	}
	if got := TokenFrom(httptest.NewRequest(http.MethodGet, "/download/report", nil)); got != "" {
		t.Errorf("a request with no token gave %q", got)
	}
}

// The signature comparison is constant time, and only the source can say so.
//
// A behavioural test cannot tell hmac.Equal from == : both refuse the same
// tokens and return the same errors, and the difference is a timing signal a
// unit test has no way to observe. That is exactly why it is worth pinning —
// a property nothing checks is one somebody simplifies away, and the
// simplification looks correct in every test.
//
// The source is read with its comments stripped. A check that searched the
// whole file would be satisfied by this paragraph, which mentions hmac.Equal
// four times and compares nothing.
func TestTheSignatureComparisonIsConstantTime(t *testing.T) {
	src, err := os.ReadFile("signedurl.go")
	if err != nil {
		t.Fatal(err)
	}
	code := withoutComments(string(src))

	if !strings.Contains(code, "hmac.Equal(") {
		t.Error("the signature is not compared with hmac.Equal. A plain comparison returns " +
			"early on the first byte that differs, so a token can be guessed a byte at a " +
			"time by measuring how long each refusal takes.")
	}
	// And nothing compares the two with an operator that short-circuits.
	for _, bad := range []string{"got == string(want)", "string(got) == string(want)",
		"got != string(want)", "string(got) != string(want)"} {
		if strings.Contains(code, bad) {
			t.Errorf("the source holds %q, which is not constant time", bad)
		}
	}
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

// And the check above is worth only as much as its comment stripping.
func TestTheSourceCheckReadsCodeRatherThanComments(t *testing.T) {
	stripped := withoutComments("a := 1 // hmac.Equal(x, y)\nb := 2\n/* hmac.Equal */ c := 3")
	if strings.Contains(stripped, "hmac.Equal") {
		t.Errorf("a mention in a comment survived stripping: %q", stripped)
	}
	for _, want := range []string{"a := 1", "b := 2", "c := 3"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("stripping removed code: %q is missing from %q", want, stripped)
		}
	}
}

// ---------------------------------------------------------------------------
// Reading the key out of the environment
// ---------------------------------------------------------------------------

// A deployment with no key set still starts, and says what is missing.
//
// Refusing to start would take down the twenty procedures that have nothing to
// do with downloads to protect the one that does.
func TestNoKeyIsNotAnError(t *testing.T) {
	t.Setenv(KeyEnv, "")
	s := FromEnv()
	if s.Keys != nil {
		t.Error("a keyring was built with no key set")
	}
	if !strings.Contains(s.Why, KeyEnv) {
		t.Errorf("the reason does not name the setting: %q", s.Why)
	}
}

// A key that is set and unusable is reported as that.
//
// "Not configured" and "configured wrongly" are different, and only the second
// means somebody intended this to work.
func TestAnUnusableKeyIsReported(t *testing.T) {
	for _, tc := range []struct{ name, value, expect string }{
		{"too short", "c2hvcnQ=", "shortest"},
		{"not base64 or hex", "not a key at all!!", "neither base64 nor hex"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(KeyEnv, tc.value)
			s := FromEnv()
			if s.Keys != nil {
				t.Fatal("a keyring was built from an unusable key")
			}
			if !strings.Contains(s.Why, tc.expect) {
				t.Errorf("the reason does not say %q: %s", tc.expect, s.Why)
			}
		})
	}
}

// Both encodings are accepted, because both are what somebody reaches for.
func TestAKeyMayBeBase64OrHex(t *testing.T) {
	raw := make([]byte, MinSecretLength)
	for i := range raw {
		raw[i] = byte(i)
	}

	for _, encoded := range []string{
		base64.StdEncoding.EncodeToString(raw),
		base64.RawURLEncoding.EncodeToString(raw),
		hex.EncodeToString(raw),
	} {
		t.Setenv(KeyEnv, encoded)
		s := FromEnv()
		if s.Keys == nil {
			t.Fatalf("%q was refused: %s", encoded, s.Why)
		}
		if s.Why != "" {
			t.Errorf("%q was accepted with a complaint: %s", encoded, s.Why)
		}
	}
}

// The same key always gets the same id, and two keys get different ones.
//
// The id is what a verifier holding several uses to pick one, so two keys
// sharing an id would make a token's key id say nothing — and a key whose id
// changed between two processes would have each refuse the other's links.
func TestTheKeyIdIsStableAndDistinct(t *testing.T) {
	a := make([]byte, MinSecretLength)
	b := make([]byte, MinSecretLength)
	b[0] = 1

	// The same bytes, held separately, as two processes would hold them. A key
	// whose id differed between two processes would have each refuse the
	// other's links.
	same := append([]byte(nil), a...)
	if keyID(a) != keyID(same) {
		t.Error("the same key held twice got two ids, so two processes sharing it would " +
			"refuse each other's links")
	}
	if keyID(a) == keyID(b) {
		t.Error("two keys got the same id, so a token's key id no longer says which " +
			"one signed it")
	}
	// And it is not the key.
	if strings.Contains(hex.EncodeToString(a), keyID(a)) {
		t.Error("the id is a slice of the key itself")
	}
}

// A previous key is read, so a rotation does not break links in flight.
func TestAPreviousKeyIsReadAndUsedForVerificationOnly(t *testing.T) {
	old := make([]byte, MinSecretLength)
	old[0] = 9
	current := make([]byte, MinSecretLength)
	current[0] = 7

	// Sign something with the old key alone, as the process before the
	// rotation would have.
	before, err := NewKeyring(Key{ID: keyID(old), Secret: old})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := before.Sign(grant(), now)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv(KeyEnv, base64.StdEncoding.EncodeToString(current))
	t.Setenv(PreviousKeyEnv, base64.StdEncoding.EncodeToString(old))
	s := FromEnv()
	if s.Keys == nil {
		t.Fatalf("no keyring: %s", s.Why)
	}

	if _, err := s.Keys.Verify(issued, now); err != nil {
		t.Errorf("a link issued before the rotation stopped working: %v", err)
	}
	fresh, err := s.Keys.Sign(grant(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fresh, "."+keyID(current)+".") {
		t.Error("a new link was not signed with the current key")
	}
}

// A lifetime beyond the maximum is refused rather than clamped.
//
// A deployment that asked for a week and silently got a day would build on the
// week — and find out when a link it promised would last stops working.
func TestAnOverlongLifetimeIsRefusedAndSaidSo(t *testing.T) {
	key := make([]byte, MinSecretLength)
	t.Setenv(KeyEnv, base64.StdEncoding.EncodeToString(key))

	t.Setenv(LifetimeEnv, "168h")
	s := FromEnv()
	if s.Lifetime != DefaultLifetime {
		t.Errorf("lifetime is %s, want the default %s", s.Lifetime, DefaultLifetime)
	}
	if !strings.Contains(s.Why, MaxLifetime.String()) {
		t.Errorf("the reason does not say what the maximum is: %s", s.Why)
	}
	// And the keyring is still built: an overlong lifetime is a setting to
	// correct, not a reason to stop issuing links.
	if s.Keys == nil {
		t.Error("an overlong lifetime stopped downloads working altogether")
	}

	t.Setenv(LifetimeEnv, "2h")
	if got := FromEnv(); got.Lifetime != 2*time.Hour {
		t.Errorf("a valid lifetime read as %s", got.Lifetime)
	}
}
