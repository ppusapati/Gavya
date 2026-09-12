package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/credential"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/identity-service/internal/domain"
	"github.com/ppusapati/gavya/services/identity-service/internal/repository"
)

var at = time.Date(2026, 8, 25, 6, 30, 0, 0, time.UTC)

// cheap keeps the suite quick without changing which paths run.
var cheap = credential.Params{Memory: 8 * 1024, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

// fake stands in for the database and records what was asked of it, so the order
// of operations — which is the security property here — can be asserted rather
// than assumed.
type fake struct {
	// The administration half of the interface, embedded rather than stubbed out
	// method by method. These tests are about signing in, and a nil embedded
	// interface panics loudly if one of them is ever reached — which is the right
	// answer for a test double being used for something it was not written for,
	// and better than ten methods quietly returning zero values.
	repository.Repository

	account     domain.Account
	memberships []domain.Membership
	identity    domain.ServiceIdentity

	calls    []string
	attempts []repository.Attempt
	sessions []repository.Session
	states   []state

	// tenantOnCreate records the tenant the connection carried when the session
	// was written, which is what the policies would have enforced.
	tenantOnCreate string

	resolved  repository.Resolved
	revoked   []string
	failFind  bool
	failWrite bool
}

type state struct {
	userID string
	failed int
	locked *time.Time
	ok     bool
}

func (f *fake) note(s string) { f.calls = append(f.calls, s) }

func (f *fake) FindAccount(_ context.Context, email string) (domain.Account, error) {
	f.note("find:" + email)
	if f.failFind {
		return domain.Account{}, errors.New("database is down")
	}
	return f.account, nil
}

func (f *fake) MembershipsFor(_ context.Context, userID string) ([]domain.Membership, error) {
	f.note("memberships:" + userID)
	return f.memberships, nil
}

func (f *fake) FindServiceIdentity(_ context.Context, name string) (domain.ServiceIdentity, error) {
	f.note("service:" + name)
	return f.identity, nil
}

func (f *fake) RecordAttempt(_ context.Context, a repository.Attempt) error {
	f.note("attempt")
	f.attempts = append(f.attempts, a)
	if f.failWrite {
		return errors.New("cannot write the log")
	}
	return nil
}

func (f *fake) RecordLoginState(_ context.Context, userID string, failed int, locked *time.Time, ok bool) error {
	f.note("state")
	f.states = append(f.states, state{userID, failed, locked, ok})
	if f.failWrite {
		return errors.New("cannot write the state")
	}
	return nil
}

func (f *fake) CreateSession(ctx context.Context, s repository.Session) error {
	f.note("session")
	if t, err := tenantdb.TenantFrom(ctx); err == nil {
		f.tenantOnCreate = t
	}
	f.sessions = append(f.sessions, s)
	return nil
}

func (f *fake) ResolveSession(_ context.Context, id string) (repository.Resolved, error) {
	f.note("resolve:" + id)
	return f.resolved, nil
}

func (f *fake) RevokeSession(_ context.Context, id, reason string) (bool, error) {
	f.note("revoke:" + id)
	f.revoked = append(f.revoked, id)
	return true, nil
}

func (f *fake) RevokeSessionsFor(_ context.Context, userID, reason string) (int64, error) {
	f.note("revoke-all:" + userID)
	return 3, nil
}

type fixedIDs struct{ n int }

func (f *fixedIDs) New() string {
	f.n++
	return strings.Repeat("0", 24) + string(rune('A'+f.n-1)) + "1"
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return at }

type quietLog struct{ errs []string }

func (q *quietLog) Infof(string, ...any) {}
func (q *quietLog) Errorf(f string, a ...any) {
	q.errs = append(q.errs, f)
}

func newService(f *fake) (*Service, *quietLog) {
	l := &quietLog{}
	return New(f, &fixedIDs{}, fixedClock{}, l), l
}

func withPassword(t *testing.T, password string) domain.Account {
	t.Helper()
	h, err := credential.HashWith(password, cheap)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Account{Exists: true, UserID: "US_1", PasswordHash: h, Status: "active"}
}

func TestARightPasswordOpensASession(t *testing.T) {
	f := &fake{
		account:     withPassword(t, "a good password"),
		memberships: []domain.Membership{{TenantID: "T_A", RoleID: "RL_1", RoleName: "manager", Status: "active"}},
	}
	svc, _ := newService(f)

	res, err := svc.SignIn(context.Background(), SignInRequest{Email: "Someone@Example.Test", Password: "a good password"})
	if err != nil {
		t.Fatal(err)
	}
	if res.TenantID != "T_A" || res.UserID != "US_1" {
		t.Errorf("session for %s / %s", res.TenantID, res.UserID)
	}
	if !res.ExpiresAt.Equal(at.Add(domain.SessionLifetime)) {
		t.Errorf("expires at %v", res.ExpiresAt)
	}
	// Folded before it reached the store, because that is what the unique index
	// is on and two accounts for one person is how a revoked login stays usable.
	if f.calls[0] != "find:someone@example.test" {
		t.Errorf("looked up %q, want it folded", f.calls[0])
	}
}

// The session is written on a connection scoped to the chosen tenant, so the
// policies apply to it like every other write in the platform.
func TestTheSessionIsWrittenUnderTheTenantItIsFor(t *testing.T) {
	f := &fake{
		account:     withPassword(t, "pw"),
		memberships: []domain.Membership{{TenantID: "T_B", Status: "active"}},
	}
	svc, _ := newService(f)
	if _, err := svc.SignIn(context.Background(), SignInRequest{Email: "a@b.test", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	if f.tenantOnCreate != "T_B" {
		t.Errorf("the session was written under tenant %q, want T_B", f.tenantOnCreate)
	}
}

// The whole reason SignIn does not return early. An address with no account must
// cost the same as one with, or the difference answers "does this address have
// an account" to anybody who can time a request.
func TestAnUnknownAddressStillVerifiesAPassword(t *testing.T) {
	known := &fake{
		account:     withPassword(t, "right"),
		memberships: []domain.Membership{{TenantID: "T_A", Status: "active"}},
	}
	unknown := &fake{account: domain.Account{}}

	for _, tc := range []struct {
		name string
		f    *fake
	}{{"known address, wrong password", known}, {"unknown address", unknown}} {
		svc, _ := newService(tc.f)
		if _, err := svc.SignIn(context.Background(), SignInRequest{Email: "a@b.test", Password: "wrong"}); err == nil {
			t.Fatalf("%s: signed in", tc.name)
		} else if !errors.Is(err, domain.ErrSignInFailed) {
			t.Errorf("%s: err = %v, want the one refusal", tc.name, err)
		}
	}

	// Both reached the store and both recorded an attempt: no path shortcuts.
	for _, tc := range []struct {
		name string
		f    *fake
	}{{"known", known}, {"unknown", unknown}} {
		if len(tc.f.attempts) != 1 {
			t.Errorf("%s: %d attempts recorded, want 1", tc.name, len(tc.f.attempts))
		}
	}
}

// The attempts worth keeping are the ones against addresses that do not exist.
// A hundred failures against ninety addresses is a credential-stuffing run and
// looks like nothing if only the successes are recorded.
func TestAFailureAgainstAnUnknownAddressIsRecordedWithTheAddress(t *testing.T) {
	f := &fake{account: domain.Account{}}
	svc, _ := newService(f)
	_, _ = svc.SignIn(context.Background(), SignInRequest{
		Email: "NoSuch@Example.Test", Password: "x", IPAddress: "203.0.113.9"})

	if len(f.attempts) != 1 {
		t.Fatalf("%d attempts recorded", len(f.attempts))
	}
	a := f.attempts[0]
	if a.EmailNormalised != "nosuch@example.test" {
		t.Errorf("recorded address %q", a.EmailNormalised)
	}
	if a.Succeeded {
		t.Error("recorded as a success")
	}
	if a.IPAddress != "203.0.113.9" {
		t.Errorf("recorded address %q", a.IPAddress)
	}
	if a.FailureReason == "" {
		t.Error("no reason recorded, which is the half an operator needs")
	}
}

// The reason goes in the log and never to the caller.
func TestTheCallerIsToldTheSameThingWhateverWentWrong(t *testing.T) {
	locked := at.Add(time.Hour)
	cases := []struct {
		name string
		f    *fake
	}{
		{"no account", &fake{account: domain.Account{}}},
		{"wrong password", &fake{account: withPassword(t, "right")}},
		{"suspended", &fake{account: func() domain.Account {
			a := withPassword(t, "wrong-on-purpose")
			a.Status = "suspended"
			return a
		}()}},
		{"locked", &fake{account: func() domain.Account {
			a := withPassword(t, "wrong-on-purpose")
			a.LockedUntil = &locked
			return a
		}()}},
	}

	var messages []string
	for _, tc := range cases {
		svc, _ := newService(tc.f)
		_, err := svc.SignIn(context.Background(), SignInRequest{Email: "a@b.test", Password: "guess"})
		if err == nil {
			t.Fatalf("%s: signed in", tc.name)
		}
		messages = append(messages, err.Error())
	}
	for i, m := range messages {
		if m != messages[0] {
			t.Errorf("%s gives %q and %s gives %q; the difference tells an attacker which "+
				"addresses are worth attacking", cases[0].name, messages[0], cases[i].name, m)
		}
	}
}

// A person in several tenants who did not say which is refused, and told what
// the options are. Picking one would be a silent decision about whose data they
// are about to edit.
func TestSigningInWithoutSayingWhichTenantIsRefusedWithTheOptions(t *testing.T) {
	f := &fake{
		account: withPassword(t, "pw"),
		memberships: []domain.Membership{
			{TenantID: "T_A", Status: "active"},
			{TenantID: "T_B", Status: "active"},
		},
	}
	svc, _ := newService(f)
	_, err := svc.SignIn(context.Background(), SignInRequest{Email: "a@b.test", Password: "pw"})

	var amb *domain.AmbiguousTenantError
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, want the refusal that lists the tenants", err)
	}
	if len(f.sessions) != 0 {
		t.Error("a session was opened despite the tenant being unsettled")
	}
}

// The bookkeeping must not be able to lock everybody out. A database hiccup
// writing the failure count is worth logging, not worth refusing a sign-in that
// otherwise succeeded.
func TestAFailureToWriteTheBookkeepingDoesNotRefuseAValidSignIn(t *testing.T) {
	f := &fake{
		account:     withPassword(t, "pw"),
		memberships: []domain.Membership{{TenantID: "T_A", Status: "active"}},
		failWrite:   true,
	}
	svc, log := newService(f)

	if _, err := svc.SignIn(context.Background(), SignInRequest{Email: "a@b.test", Password: "pw"}); err != nil {
		t.Fatalf("a valid sign-in was refused because the log could not be written: %v", err)
	}
	if len(log.errs) == 0 {
		t.Error("the write failure was swallowed without a word")
	}
}

// But a database that cannot be read at all must fail, rather than being treated
// as "no such account" — which would refuse everybody while looking like a wave
// of wrong passwords.
func TestADatabaseThatCannotBeReadFailsRatherThanRefusingEverybody(t *testing.T) {
	f := &fake{failFind: true}
	svc, _ := newService(f)
	_, err := svc.SignIn(context.Background(), SignInRequest{Email: "a@b.test", Password: "pw"})
	if err == nil {
		t.Fatal("no error")
	}
	if errors.Is(err, domain.ErrSignInFailed) {
		t.Error("a database failure was reported as a wrong password, which would hide an " +
			"outage behind what looks like user error")
	}
}

func TestAnEmptyPasswordIsRefusedWithoutTouchingTheDatabase(t *testing.T) {
	f := &fake{account: withPassword(t, "pw")}
	svc, _ := newService(f)
	if _, err := svc.SignIn(context.Background(), SignInRequest{Email: "a@b.test"}); !errors.Is(err, domain.ErrSignInFailed) {
		t.Errorf("err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("the store was consulted for an empty password: %v", f.calls)
	}
}

// -------------------------------------------------------------------------
// Sessions
// -------------------------------------------------------------------------

func TestVerifyingALiveSessionGivesItsTenant(t *testing.T) {
	f := &fake{resolved: repository.Resolved{Valid: true, TenantID: "T_A", UserID: "US_1"}}
	svc, _ := newService(f)

	v, err := svc.Verify(context.Background(), "SE_1")
	if err != nil {
		t.Fatal(err)
	}
	if v.TenantID != "T_A" {
		t.Errorf("tenant = %q", v.TenantID)
	}
}

// Expired, revoked and never-existed are different things to an operator and the
// same thing to whoever is holding the session.
func TestEveryUnusableSessionIsRefusedTheSameWay(t *testing.T) {
	var messages []string
	for _, r := range []repository.Resolved{
		{Valid: false, Reason: "expired"},
		{Valid: false, Reason: "signed out"},
		{Valid: false, Reason: "no such session"},
	} {
		svc, _ := newService(&fake{resolved: r})
		_, err := svc.Verify(context.Background(), "SE_1")
		if err == nil {
			t.Fatalf("%s was accepted", r.Reason)
		}
		if !errors.Is(err, ErrNotSignedIn) {
			t.Errorf("%s gave %v, want ErrNotSignedIn", r.Reason, err)
		}
		messages = append(messages, err.Error())
	}
	for _, m := range messages {
		if m != messages[0] {
			t.Errorf("the refusals differ: %q vs %q", messages[0], m)
		}
	}
}

func TestAnEmptySessionIdIsRefusedWithoutAskingTheDatabase(t *testing.T) {
	f := &fake{}
	svc, _ := newService(f)
	if _, err := svc.Verify(context.Background(), ""); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("the store was consulted for an empty session id: %v", f.calls)
	}
}

// A client whose sign-out reply was lost retries. Told it failed, it retries
// again, against a session that is already gone.
func TestSigningOutTwiceIsNotAnError(t *testing.T) {
	f := &fake{resolved: repository.Resolved{Valid: true, TenantID: "T_A"}}
	svc, _ := newService(f)
	if err := svc.SignOut(context.Background(), "SE_1", ""); err != nil {
		t.Fatal(err)
	}

	// Second time: the session no longer resolves.
	f.resolved = repository.Resolved{Valid: false, Reason: "signed out"}
	if err := svc.SignOut(context.Background(), "SE_1", ""); err != nil {
		t.Errorf("a repeated sign-out reported %v", err)
	}
}

// Signing out somebody else's tenant's session must not be possible, which is
// why the revoke runs scoped rather than unscoped.
func TestSigningOutRunsUnderTheSessionsOwnTenant(t *testing.T) {
	f := &fake{resolved: repository.Resolved{Valid: true, TenantID: "T_C"}}
	svc, _ := newService(f)
	if err := svc.SignOut(context.Background(), "SE_1", "signed out"); err != nil {
		t.Fatal(err)
	}
	if len(f.revoked) != 1 {
		t.Fatalf("%d sessions revoked", len(f.revoked))
	}
}

// -------------------------------------------------------------------------
// Services
// -------------------------------------------------------------------------

func TestAServiceIdentityWithNoTenantCannotOpenASession(t *testing.T) {
	h, _ := credential.HashWith("a-secret", cheap)
	f := &fake{identity: domain.ServiceIdentity{
		Exists: true, ID: "SI_1", Status: "active",
		ExpiresAt: at.Add(24 * time.Hour), SecretHash: h,
		// No tenant: a platform-wide identity.
	}}
	svc, _ := newService(f)

	_, err := svc.SignInService(context.Background(), "recomputer", "a-secret", "")
	if err == nil {
		t.Fatal("a session was opened with no tenant to scope it to")
	}
	if errors.Is(err, domain.ErrSignInFailed) {
		t.Error("reported as a bad credential, which would send somebody looking for the " +
			"wrong problem — the secret was right")
	}
	if len(f.sessions) != 0 {
		t.Error("a session was written")
	}
}

func TestAServiceWithAGoodSecretGetsAShortSession(t *testing.T) {
	h, _ := credential.HashWith("a-secret", cheap)
	f := &fake{identity: domain.ServiceIdentity{
		Exists: true, ID: "SI_1", TenantID: "T_A", Status: "active",
		ExpiresAt: at.Add(24 * time.Hour), SecretHash: h,
	}}
	svc, _ := newService(f)

	res, err := svc.SignInService(context.Background(), "ingestion", "a-secret", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.ExpiresAt.Equal(at.Add(domain.ServiceSessionLifetime)) {
		t.Errorf("expires at %v, want the shorter service lifetime", res.ExpiresAt)
	}
	if f.tenantOnCreate != "T_A" {
		t.Errorf("written under tenant %q", f.tenantOnCreate)
	}
}

func TestAServiceWithNoSuchNameIsRefusedLikeAnyOther(t *testing.T) {
	f := &fake{identity: domain.ServiceIdentity{}}
	svc, _ := newService(f)
	if _, err := svc.SignInService(context.Background(), "nobody", "x", ""); !errors.Is(err, domain.ErrSignInFailed) {
		t.Errorf("err = %v", err)
	}
}
