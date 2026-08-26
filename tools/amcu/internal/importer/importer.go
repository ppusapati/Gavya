// Package importer turns a vendor's export into collections the platform holds.
//
// amcu-profile reads an export and produces a profile describing it. Until now
// nothing consumed that profile: a society could be told exactly what their file
// contained and still had no way to get it in. This closes that, so the work
// between "we have their file" and "their file is in the platform" is an
// afternoon rather than a fortnight of bespoke parsing.
//
// # It refuses more than it accepts
//
// The importer's job is not to get as many rows in as possible. A row that
// cannot be trusted is worse in than out, because once it is in, it joins to
// everything else and the settlement computed from it looks as ordinary as any
// other. So:
//
//   - A draft profile is refused outright. Every unresolved question in it —
//     which way round the dates read, litres or kilograms — is a silent decision
//     about somebody's milk payment.
//   - A file whose shape has drifted from its profile is refused before a single
//     row is read. Vendors change their exports between versions, and a column
//     that moved turns fat into a rate without anything looking wrong.
//   - A row that fails is quarantined with its reason, never dropped. The rows
//     that fail are the ones somebody needs to see.
//
// # Every row keeps its provenance
//
// Each record carries the batch it came in, the line it was on, and a hash of
// the source text. That is what makes a re-import a no-op rather than a second
// payment, and what lets somebody answer "where did this number come from" a
// year later with the row from the file rather than a recollection.
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ppusapati/gavya/tools/amcu/internal/mapping"
	"github.com/ppusapati/gavya/tools/amcu/internal/pathology"
	"github.com/ppusapati/gavya/tools/amcu/internal/profile"
	"github.com/ppusapati/gavya/tools/amcu/internal/source"
)

// Collection is one row of the export, read through its profile.
//
// The fields are the facts the platform records, not the columns the vendor
// wrote. A vendor who calls it MEMBERNO and one who calls it Producer_Code both
// arrive here as ProducerCode.
type Collection struct {
	// Line is the line of the file this came from, one-based and counting the
	// header. It is in the record because "row 4,812 is wrong" is something
	// somebody can act on and "one of the rows is wrong" is not.
	Line int

	SocietyCode  string
	ProducerCode string
	CollectedOn  time.Time
	Shift        string

	// Quantity as written, with the unit the profile declares. Kept as a decimal
	// string rather than a float: the file says 12.34 and the platform must
	// record 12.34, not the nearest binary approximation of it.
	Quantity     string
	QuantityUnit string

	Fat string
	SNF string
	CLR string

	Rate   string
	Amount string

	// Raw is the source line, so the record can be shown to whoever wrote it.
	Raw string
	// Hash is over Raw, and is what makes re-importing the same file a no-op.
	Hash string
}

// Rejection is a row that could not be read.
//
// Held rather than dropped: a file where nine rows in ten import cleanly and one
// silently vanishes is worse than one that refuses outright, because nobody
// counts the rows.
type Rejection struct {
	Line   int
	Raw    string
	Reason string
	Detail string
}

// Result is what an import run produced.
type Result struct {
	Vendor      string
	Collections []Collection
	Rejected    []Rejection

	// Findings are what amcu-profile's checks said about this file, minus the
	// ones the profile answers. Blocking findings stop the import; the rest are
	// carried so whoever runs it sees them rather than having to run a second
	// tool.
	Findings []pathology.Finding

	// Answered are the findings the profile settled — reported rather than
	// discarded, so "the dates in this file are ambiguous and the profile says
	// they are day-first" is something the operator reads rather than something
	// that happened out of sight.
	Answered []pathology.Finding

	// HandledPerRow are findings about particular rows, which Rejected already
	// accounts for. Reported separately so they do not appear alongside the
	// file-wide findings as though nothing had been done about them.
	HandledPerRow []pathology.Finding
}

// ErrDraftProfile is returned for a profile that still has open questions.
type ErrDraftProfile struct{ Unresolved []string }

func (e *ErrDraftProfile) Error() string {
	// One per line. Joined into a paragraph these run to a screenful of prose
	// that nobody reads to the end of, and the whole point is that a person goes
	// through them one at a time.
	var b strings.Builder
	fmt.Fprintf(&b, "this profile is still a draft. %d thing(s) have to be settled by a person "+
		"before anything is imported through it:", len(e.Unresolved))
	for _, u := range e.Unresolved {
		fmt.Fprintf(&b, "\n\n  - %s", u)
	}
	return b.String()
}

// ErrShapeChanged is returned when the file no longer matches its profile.
type ErrShapeChanged struct{ Differences []string }

func (e *ErrShapeChanged) Error() string {
	return fmt.Sprintf("the file does not match the profile: %s. A vendor that changed their "+
		"export moves columns, and a moved column turns one measurement into another without "+
		"anything looking wrong", strings.Join(e.Differences, "; "))
}

// ErrBlocked is returned when the file itself is unsound.
type ErrBlocked struct{ Findings []pathology.Finding }

func (e *ErrBlocked) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d blocking finding(s) in this file:", len(e.Findings))
	for _, f := range e.Findings {
		fmt.Fprintf(&b, "\n  - %s: %s", f.Title, f.Detail)
	}
	return b.String()
}

// Read imports an export through its profile.
//
// The order is deliberate. The profile is checked before the file is opened, the
// shape before the rows are read, and the file's own soundness before anything
// is converted — so a run that is going to fail fails on the cheapest check
// that would have caught it, and says which.
func Read(p *mapping.Profile, t *source.Table) (*Result, error) {
	if len(p.Unresolved) > 0 {
		return nil, &ErrDraftProfile{Unresolved: p.Unresolved}
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}

	prof := profile.Profile(t)
	if diffs := mapping.Compare(p, t, prof); len(diffs) > 0 {
		return nil, &ErrShapeChanged{Differences: diffs}
	}

	// The file's own checks run without knowing the profile, so two of the
	// things they can block on are questions the profile has already answered:
	// which way round the dates read, and what unit the quantity is in. A
	// profile exists precisely to record those answers, given once by a person,
	// so a finding of that kind is settled rather than fatal.
	//
	// Nothing else is settled by a profile. It cannot make two people stop
	// sharing a producer code or a day's milk stop being recorded twice.
	report := pathology.Inspect(t, prof)
	var findings, answered, handled, blocking []pathology.Finding
	for _, f := range report.Findings {
		if f.Kind.AnsweredByProfile() {
			answered = append(answered, f)
			continue
		}
		// A finding about particular rows is dealt with row by row below: those
		// rows are held with their reason and the rest are imported. Refusing
		// the whole file for four bad rows in four thousand would leave the
		// other 3,996 collections unrecorded until a source system is fixed
		// that may never be fixed.
		if f.Kind.PerRow() {
			handled = append(handled, f)
			continue
		}
		findings = append(findings, f)
		if f.Severity == pathology.Blocking {
			blocking = append(blocking, f)
		}
	}
	if len(blocking) > 0 {
		return nil, &ErrBlocked{Findings: blocking}
	}

	res := &Result{Vendor: p.Vendor, Findings: findings, Answered: answered, HandledPerRow: handled}
	byRole := map[string]int{}
	for _, f := range p.Fields {
		byRole[f.Role] = f.Column
	}

	// Rows holds data only — the header is a field of its own — so there is
	// nothing to skip here. Offsetting by one instead would silently drop the
	// first collection of every file, which is a row of somebody's milk.
	offset := 1
	if p.Format.HasHeader {
		offset = 2
	}
	for i, row := range t.Rows {
		line := i + offset
		raw := strings.Join(row, string(rune(0x1f)))

		c, rej := convert(row, line, raw, byRole, p)
		if rej != nil {
			res.Rejected = append(res.Rejected, *rej)
			continue
		}
		res.Collections = append(res.Collections, *c)
	}
	return res, nil
}

func convert(row []string, line int, raw string, byRole map[string]int, p *mapping.Profile) (*Collection, *Rejection) {
	get := func(role string) string {
		col, ok := byRole[role]
		if !ok || col >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[col])
	}

	c := &Collection{
		Line:         line,
		SocietyCode:  get(string(profile.RoleSociety)),
		ProducerCode: get(string(profile.RoleProducer)),
		Quantity:     get(string(profile.RoleQuantity)),
		QuantityUnit: p.Format.QuantityUnit,
		Fat:          get(string(profile.RoleFat)),
		SNF:          get(string(profile.RoleSNF)),
		CLR:          get(string(profile.RoleCLR)),
		Rate:         get(string(profile.RoleRate)),
		Amount:       get(string(profile.RoleAmount)),
		Raw:          raw,
		Hash:         hash(raw),
	}

	// A collection with no producer cannot be paid to anyone and a collection
	// with no date cannot be placed in a settlement period. Neither is a row to
	// carry in and sort out later.
	if c.ProducerCode == "" {
		return nil, &Rejection{line, raw, "no producer", "this row names nobody to pay"}
	}

	dateText := get(string(profile.RoleDate))
	if dateText == "" {
		return nil, &Rejection{line, raw, "no date",
			"this row cannot be placed in a settlement period"}
	}
	// The layout comes from the profile, never from guessing at the value. A
	// date that reads two ways has to have been decided once, by a person.
	when, err := time.Parse(p.Format.DateLayout, dateText)
	if err != nil {
		return nil, &Rejection{line, raw, "unreadable date",
			fmt.Sprintf("%q does not read as %s", dateText, p.Format.DateLayout)}
	}
	c.CollectedOn = when

	if c.Quantity == "" {
		return nil, &Rejection{line, raw, "no quantity", "this row records no milk"}
	}
	if _, err := strconv.ParseFloat(c.Quantity, 64); err != nil {
		return nil, &Rejection{line, raw, "unreadable quantity",
			fmt.Sprintf("%q is not a number", c.Quantity)}
	}

	// Shift is mapped through the profile's own vocabulary, because M/E, 1/2 and
	// AM/PM all occur and none of them is more standard than the others.
	if shiftText := get(string(profile.RoleShift)); shiftText != "" {
		mapped, ok := p.Format.ShiftValues[shiftText]
		if !ok {
			mapped, ok = p.Format.ShiftValues[strings.ToUpper(shiftText)]
		}
		if !ok {
			return nil, &Rejection{line, raw, "unknown shift",
				fmt.Sprintf("%q is not one of the shift values this vendor's profile declares", shiftText)}
		}
		c.Shift = mapped
	}

	return c, nil
}

// Records turns collections into what ingestion-service accepts.
//
// The sequence is the line number, not a counter. A counter would renumber
// everything after a row that was later fixed, and the platform's duplicate
// detection is on (session, sequence) — so a re-import after an edit would look
// like a different set of records rather than the same ones. The line a row was
// on does not move.
func (r *Result) Records(tenantID, deviceID, batchID string, generation int64) []Record {
	out := make([]Record, 0, len(r.Collections))
	for _, c := range r.Collections {
		payload, _ := json.Marshal(collectionPayload{
			SocietyCode:  c.SocietyCode,
			ProducerCode: c.ProducerCode,
			CollectedOn:  c.CollectedOn.Format("2006-01-02"),
			Shift:        c.Shift,
			Quantity:     c.Quantity,
			QuantityUnit: c.QuantityUnit,
			Fat:          c.Fat,
			SNF:          c.SNF,
			CLR:          c.CLR,
			Rate:         c.Rate,
			Amount:       c.Amount,
			SourceLine:   c.Line,
			SourceHash:   c.Hash,
			Vendor:       r.Vendor,
			ImportBatch:  batchID,
		})
		out = append(out, Record{
			TenantID:          tenantID,
			DeviceID:          deviceID,
			Generation:        generation,
			ExternalSessionID: batchID,
			Sequence:          int64(c.Line),
			Payload:           payload,
			CapturedAt:        c.CollectedOn.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// Record is one delivery to ingestion-service. Declared here rather than
// imported so this tool does not depend on a service's internal packages.
type Record struct {
	TenantID          string          `json:"tenant_id"`
	DeviceID          string          `json:"device_id"`
	Generation        int64           `json:"generation"`
	ExternalSessionID string          `json:"external_session_id"`
	Sequence          int64           `json:"sequence"`
	Payload           json.RawMessage `json:"payload"`
	CapturedAt        string          `json:"captured_at"`
	Actor             string          `json:"actor"`
}

// collectionPayload is what the platform stores for a collection.
//
// Every measurement is a string. The file said 4.15 and the platform must record
// 4.15; a float would record the nearest binary value to it, and the difference
// multiplies into a rate.
type collectionPayload struct {
	SocietyCode  string `json:"society_code,omitempty"`
	ProducerCode string `json:"producer_code"`
	CollectedOn  string `json:"collected_on"`
	Shift        string `json:"shift,omitempty"`
	Quantity     string `json:"quantity"`
	QuantityUnit string `json:"quantity_unit"`
	Fat          string `json:"fat,omitempty"`
	SNF          string `json:"snf,omitempty"`
	CLR          string `json:"clr,omitempty"`
	Rate         string `json:"rate,omitempty"`
	Amount       string `json:"amount,omitempty"`

	// Where this came from, kept with the record rather than in a log that ages
	// out. A year later somebody asks why a producer was paid what they were,
	// and the answer has to be the line from the file.
	SourceLine  int    `json:"source_line"`
	SourceHash  string `json:"source_hash"`
	Vendor      string `json:"vendor"`
	ImportBatch string `json:"import_batch"`
}

// BatchID is a deterministic name for one import of one file.
//
// Derived from the file's own content, so importing the same file twice produces
// the same batch and the platform's replay detection sees it as the replay it
// is. A timestamp or a random id would make every re-import look like new milk.
func BatchID(vendor string, collections []Collection) string {
	hashes := make([]string, 0, len(collections))
	for _, c := range collections {
		hashes = append(hashes, c.Hash)
	}
	sort.Strings(hashes)
	sum := sha256.Sum256([]byte(vendor + "\x1f" + strings.Join(hashes, "\x1f")))
	// 26 characters, which is what an identifier column holds.
	return "IB" + strings.ToUpper(hex.EncodeToString(sum[:])[:24])
}

func hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
