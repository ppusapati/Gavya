package audit

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
)

type recorder struct {
	sql  string
	args []any
	err  error
	n    int
}

func (r *recorder) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.n++
	r.sql, r.args = sql, args
	return pgconn.CommandTag{}, r.err
}

type fixedIDs struct{}

func (fixedIDs) New() string { return "AA00000000000000000000000A" }

func signedIn(user string) context.Context {
	ctx := tenantctx.With(context.Background(), "T_ALPHA")
	return tenantctx.WithActor(ctx, tenantctx.Actor{ID: user})
}

func entry() Entry {
	return Entry{
		Action:       "record_milk",
		ResourceType: "milk_record",
		ResourceID:   "R_1",
		After:        map[string]any{"litres": 5.5},
		ServiceName:  "milk-service",
	}
}

func TestARecordCarriesWhoDidWhatToWhich(t *testing.T) {
	rec := &recorder{}
	if err := Write(signedIn("US_1"), rec, fixedIDs{}, entry()); err != nil {
		t.Fatal(err)
	}
	if rec.n != 1 {
		t.Fatalf("%d statements", rec.n)
	}
	want := []any{
		"AA00000000000000000000000A", "T_ALPHA", "US_1", "user",
		"record_milk", "milk_record", "R_1",
	}
	for i, w := range want {
		if rec.args[i] != w {
			t.Errorf("arg %d = %v, want %v", i, rec.args[i], w)
		}
	}
}

// The actor comes from the verified request, never from the caller. A caller
// that could name the actor could put somebody else's name on a change, and a
// trail that records the name the actor chose is a record of nothing.
//
// So Entry has no field for it, and this reads the struct to check — an earlier
// version of this test compared against a hardcoded list of field names, which
// meant it could never fail whatever Entry gained.
func TestTheActorCannotBeChosenByTheCaller(t *testing.T) {
	forbidden := []string{"actor", "user", "createdby", "author", "by", "tenant"}
	typ := reflect.TypeOf(Entry{})
	if typ.NumField() == 0 {
		t.Fatal("Entry has no fields, so this test would pass vacuously")
	}
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		for _, f := range forbidden {
			if name == f || strings.HasPrefix(name, f) {
				t.Errorf("Entry has a %s field; who is acting must come from the verified "+
					"request, not from whoever is writing the record", typ.Field(i).Name)
			}
		}
	}
}

func TestAServiceIsRecordedAsAServiceAndNotAsAPerson(t *testing.T) {
	ctx := tenantctx.WithActor(
		tenantctx.With(context.Background(), "T_ALPHA"),
		tenantctx.Actor{ServiceID: "SI_INGESTION"})

	rec := &recorder{}
	if err := Write(ctx, rec, fixedIDs{}, entry()); err != nil {
		t.Fatal(err)
	}
	if rec.args[2] != "SI_INGESTION" || rec.args[3] != "service" {
		t.Errorf("recorded actor %v of type %v, want the service as itself", rec.args[2], rec.args[3])
	}
}

// A record that cannot be attributed is not written at all. Written with blanks,
// it produces a trail that has rows for everything and answers for nothing.
func TestAnUnattributableRecordIsRefusedRatherThanWrittenBlank(t *testing.T) {
	full := signedIn("US_1")
	noTenant := tenantctx.WithActor(context.Background(), tenantctx.Actor{ID: "US_1"})
	noActor := tenantctx.With(context.Background(), "T_ALPHA")

	for _, tc := range []struct {
		name string
		ctx  context.Context
		e    Entry
		want string
	}{
		{"no tenant", noTenant, entry(), "no tenant"},
		{"no actor", noActor, entry(), "no actor"},
		{"no action", full, func() Entry { e := entry(); e.Action = ""; return e }(), "no action"},
		{"no resource type", full, func() Entry { e := entry(); e.ResourceType = ""; return e }(), "no resource type"},
		{"no resource id", full, func() Entry { e := entry(); e.ResourceID = ""; return e }(), "no resource id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			err := Write(tc.ctx, rec, fixedIDs{}, tc.e)
			if err == nil {
				t.Fatal("written anyway")
			}
			if !errors.Is(err, ErrIncomplete) {
				t.Errorf("err = %v, want ErrIncomplete", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to say %q", err, tc.want)
			}
			if rec.n != 0 {
				t.Error("a statement was run for a record that could not be attributed")
			}
		})
	}
}

// A creation has no before and a deletion has no after. Those must be a SQL null
// rather than the four characters "null", which is what a field explicitly set
// to null looks like — two different facts that must not render the same.
func TestAMissingStateIsNullAndNotTheWordNull(t *testing.T) {
	rec := &recorder{}
	e := entry()
	e.Before = nil
	if err := Write(signedIn("US_1"), rec, fixedIDs{}, e); err != nil {
		t.Fatal(err)
	}
	if rec.args[7] != (*string)(nil) {
		t.Errorf("before = %#v, want a SQL null", rec.args[7])
	}

	// And a state that is genuinely null renders as JSON null.
	rec = &recorder{}
	e.Before = map[string]any{"rate": nil}
	if err := Write(signedIn("US_1"), rec, fixedIDs{}, e); err != nil {
		t.Fatal(err)
	}
	before, ok := rec.args[7].(*string)
	if !ok || before == nil || !strings.Contains(*before, "null") {
		t.Errorf("before = %#v, want the field's null preserved", rec.args[7])
	}
}

func TestTheStateEitherSideIsRecordedAsJSON(t *testing.T) {
	rec := &recorder{}
	e := entry()
	e.Before = map[string]any{"litres": 5.0}
	e.After = map[string]any{"litres": 5.5}
	if err := Write(signedIn("US_1"), rec, fixedIDs{}, e); err != nil {
		t.Fatal(err)
	}
	before := rec.args[7].(*string)
	after := rec.args[8].(*string)
	if !strings.Contains(*before, "5") || !strings.Contains(*after, "5.5") {
		t.Errorf("before %q, after %q", *before, *after)
	}
}

// Something that cannot be turned into JSON must fail the write rather than be
// dropped, or the record would say a change happened and not what it was.
func TestAStateThatCannotBeRecordedFailsTheWrite(t *testing.T) {
	rec := &recorder{}
	e := entry()
	e.After = make(chan int)
	err := Write(signedIn("US_1"), rec, fixedIDs{}, e)
	if err == nil {
		t.Fatal("written anyway")
	}
	if rec.n != 0 {
		t.Error("a statement was run for a record whose content could not be encoded")
	}
}

// The write goes into the caller's transaction, so a failure has to reach the
// caller and roll the change back with it.
func TestAFailedWriteIsReturnedRatherThanSwallowed(t *testing.T) {
	rec := &recorder{err: errors.New("append-only")}
	err := Write(signedIn("US_1"), rec, fixedIDs{}, entry())
	if err == nil {
		t.Fatal("a failed audit write was swallowed; the change would commit unrecorded")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Errorf("err = %v, want the underlying cause", err)
	}
}

func TestTheRecordSaysWhichServiceMadeTheChange(t *testing.T) {
	rec := &recorder{}
	if err := Write(signedIn("US_1"), rec, fixedIDs{}, entry()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range rec.args {
		if a == "milk-service" {
			found = true
		}
	}
	if !found {
		t.Error("the record does not say which service made the change")
	}
}
