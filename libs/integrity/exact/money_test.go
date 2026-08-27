package exact

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Everything the money bound admits survives the round trip it was chosen for.
//
// The claim MoneyPrecision rests on: a value inside the ceiling goes to the
// database as a decimal literal, comes back as float64, crosses the wire as
// JSON, and is still the same decimal. This checks the Go half of that — the
// literal, the float64 and the JSON — against a large random sample, which is
// how the ceiling was found in the first place.
//
// The database half is checked in e2e, against a real NUMERIC(18,4) column.
func TestEveryMoneyValueTheCeilingAdmitsSurvivesTheRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(20260827))
	const ceiling = int64(MoneyCeiling)

	for i := 0; i < 200000; i++ {
		whole := r.Int63n(ceiling)
		frac := r.Int63n(10000)
		want := fmt.Sprintf("%d.%04d", whole, frac)

		// As the database would hand it back.
		f, err := strconv.ParseFloat(want, 64)
		if err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		// As the wire would carry it.
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var back float64
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%.4f", back); got != want {
			t.Fatalf("%s came back as %s after float64 and JSON; the ceiling of %v is too "+
				"high for float64 to carry four decimals exactly", want, got, MoneyCeiling)
		}
	}
}

// And just above the ceiling, values genuinely do not survive — so the bound is
// where the loss starts rather than an arbitrary round number.
//
// Without this the test above would pass against a ceiling of one rupee, and the
// constant would be needlessly restrictive with nothing saying so.
func TestJustAboveTheCeilingValuesStopSurviving(t *testing.T) {
	r := rand.New(rand.NewSource(20260827))
	lost := 0
	const n = 20000

	for i := 0; i < n; i++ {
		// One decade above the ceiling.
		whole := int64(MoneyCeiling)*10 + r.Int63n(int64(MoneyCeiling)*9)
		frac := r.Int63n(10000)
		want := fmt.Sprintf("%d.%04d", whole, frac)

		f, _ := strconv.ParseFloat(want, 64)
		b, _ := json.Marshal(f)
		var back float64
		_ = json.Unmarshal(b, &back)
		if fmt.Sprintf("%.4f", back) != want {
			lost++
		}
	}
	if lost == 0 {
		t.Errorf("no value in a decade above the ceiling lost a digit, so the ceiling of %v is "+
			"lower than it needs to be and refuses figures the platform could carry",
			MoneyCeiling)
	}
}

// The constant and the ceiling it is derived from must agree.
//
// MoneyPrecision is the column's total digit count; with MoneyScale decimals it
// implies a maximum. If somebody raises one without the other, values pass the
// Go check and fail the database one — which is how this went wrong before the
// constant existed.
func TestThePrecisionAndTheCeilingAgree(t *testing.T) {
	// NUMERIC(precision, scale) holds at most (precision - scale) integer digits.
	integerDigits := MoneyPrecision - MoneyScale
	implied := math.Pow(10, float64(integerDigits))
	if implied != MoneyCeiling {
		t.Errorf("MoneyPrecision %d with scale %d allows values below %v, and MoneyCeiling is "+
			"%v; a value between the two passes the Go check and is refused by the database, "+
			"so the caller gets a constraint violation instead of a sentence",
			MoneyPrecision, MoneyScale, implied, MoneyCeiling)
	}

	// The largest value the precision admits is accepted, and one step past the
	// ceiling is refused. Asserted through Decimal itself rather than by
	// arithmetic on the constants, so the check is on behaviour.
	largest := MoneyCeiling - 0.0001
	if _, err := NonNegativeDecimal(largest, MoneyScale, MoneyPrecision); err != nil {
		t.Errorf("%v is the largest value the ceiling admits and it was refused: %v",
			largest, err)
	}
	if _, err := NonNegativeDecimal(MoneyCeiling, MoneyScale, MoneyPrecision); err == nil {
		t.Errorf("%v is the ceiling itself and was accepted; the database refuses it",
			MoneyCeiling)
	}
}

// The Go constant and the SQL CHECK constraints must say the same number.
//
// They are two expressions of one decision in two languages, and nothing but
// this compares them. Before the constant existed the Go layer accepted eighteen
// digits and the database refused anything above eleven, so a price somebody
// typed came back as a constraint violation rather than as a sentence saying
// what was wrong with it.
func TestTheSchemasUseTheSameCeilingAsTheCode(t *testing.T) {
	root := repoRoot(t)

	schemas, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "db", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) == 0 {
		t.Fatal("no service schemas found; this check would pass against a repository with none")
	}

	// The constraint is written as `abs(%I) < <ceiling>`.
	re := regexp.MustCompile(`abs\(%I\) < (\d+)`)
	want := strconv.FormatInt(int64(MoneyCeiling), 10)

	found := 0
	for _, path := range schemas {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			found++
			if m[1] != want {
				t.Errorf("%s bounds its money columns at %s and exact.MoneyCeiling is %s; a "+
					"value between the two is accepted by one and refused by the other",
					filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path)))), m[1], want)
			}
		}
	}
	if found == 0 {
		t.Errorf("no schema bounds its money columns, so nothing stops the database holding a "+
			"figure float64 cannot carry back out. Expected the constraint written as "+
			"`abs(%%I) < %s`; if it moved, this check has stopped looking at it", want)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("cannot find the repository root from here")
	return ""
}

// Render is for a message, not for the wire: it shows a stored value at the
// field's own scale and trims trailing zeroes, which say nothing about how the
// figure is recorded.
//
// What it must not do is change the number. A refusal that quotes a rounded
// version of the value somebody typed sends them looking for a problem in the
// wrong digit.
func TestRenderShowsTheValueWithoutChangingIt(t *testing.T) {
	for _, c := range []struct {
		v     float64
		scale int32
		want  string
	}{
		{85432.75, 4, "85432.75"}, // trailing zeroes trimmed
		{85432.75, 2, "85432.75"},
		{85432.7512, 4, "85432.7512"},
		{0, 4, "0"},
		{99999999999.9999, 4, "99999999999.9999"}, // the ceiling, undamaged
		{0.0001, 4, "0.0001"},                     // the smallest unit
	} {
		if got := Render(c.v, c.scale); got != c.want {
			t.Errorf("Render(%v, %d) = %q, want %q", c.v, c.scale, got, c.want)
		}
	}

	// Rendering never rounds a value the scale can hold. Checked across the
	// whole admitted range rather than at a few points, because the failure this
	// guards is a digit lost somewhere in the middle of it.
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 50000; i++ {
		whole := r.Int63n(int64(MoneyCeiling))
		frac := r.Int63n(10000)
		want := strings.TrimSuffix(strings.TrimRight(
			fmt.Sprintf("%d.%04d", whole, frac), "0"), ".")

		f, _ := strconv.ParseFloat(fmt.Sprintf("%d.%04d", whole, frac), 64)
		if got := Render(f, MoneyScale); got != want {
			t.Fatalf("Render(%v, %d) = %q, want %q", f, MoneyScale, got, want)
		}
	}
}
