// Package mapping turns what was learned about one export into a reusable
// vendor profile.
//
// An AMCU vendor's export format is a fact about their software, not about
// dairy. SmartDairy, a state federation's in-house system and a twenty-year-old
// DOS terminal all carry the same handful of facts — who delivered, when, how
// much, how rich — in different columns, orders, units and date conventions.
//
// So a vendor is a declaration here, never code. The profiler proposes one from
// a real file; a person corrects it where the guess was wrong; and from then on
// that vendor's exports import without anybody rediscovering the format. Adding
// support for a new AMCU is writing a small file, not changing this program.
package mapping

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ppusapati/gavya/tools/amcu/internal/profile"
	"github.com/ppusapati/gavya/tools/amcu/internal/source"
)

// Profile is one vendor's export format, as a document a person can read and
// edit.
type Profile struct {
	// Vendor is the system that wrote the file — "SmartDairy", "Akashganga",
	// "in-house", whatever the society calls it.
	Vendor string `json:"vendor"`
	// Variant distinguishes formats from the same vendor, because they change
	// between versions and between the states they were sold into.
	Variant string `json:"variant,omitempty"`
	// Notes is where a person records what they learned that the file does not
	// say: which unit the quantity is in, which way round the dates read, what
	// the unexplained column turned out to be.
	Notes string `json:"notes,omitempty"`

	Format Format  `json:"format"`
	Fields []Field `json:"fields"`

	// Unresolved lists what a person still has to decide before this profile can
	// be trusted. A profile with anything here is a draft.
	Unresolved []string `json:"unresolved,omitempty"`
}

// Format is how to read the file, not what is in it.
type Format struct {
	Encoding  string `json:"encoding"`
	Delimiter string `json:"delimiter,omitempty"`
	// FixedWidths is set instead of Delimiter for a column-aligned report.
	FixedWidths []int `json:"fixed_widths,omitempty"`
	HasHeader   bool  `json:"has_header"`
	// DateLayout is the Go layout the date column is written in. It is required
	// rather than inferred at import time: a file whose dates read two ways must
	// have that decided once, by a person, and recorded here.
	DateLayout string `json:"date_layout,omitempty"`
	// QuantityUnit is "L" or "kg". Nothing in the file says which, and the two
	// differ by about three per cent.
	QuantityUnit string `json:"quantity_unit,omitempty"`
	// ShiftValues maps what this vendor writes onto morning and evening.
	ShiftValues map[string]string `json:"shift_values,omitempty"`
}

// Field maps one column of the export onto one fact the platform records.
type Field struct {
	Role   string `json:"role"`
	Column int    `json:"column"`
	Header string `json:"header,omitempty"`
	// Confidence is how sure the profiler was. Anything the operator has
	// confirmed should be set to 1 and the reason replaced with their own.
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason,omitempty"`
}

// Propose builds a draft profile from a profiled export.
//
// It is explicitly a draft: every guess carries its confidence and its reasoning,
// and everything the file cannot answer is listed as unresolved rather than
// filled in with a plausible default. A default here would be a silent decision
// about somebody's milk payment.
func Propose(vendor string, t *source.Table, p *profile.Table) *Profile {
	out := &Profile{
		Vendor: vendor,
		Format: Format{
			Encoding:    string(t.Shape.Encoding),
			HasHeader:   t.Shape.HasHeader,
			FixedWidths: t.Shape.FixedWidths,
		},
	}
	if t.Shape.Delimiter != 0 {
		out.Format.Delimiter = string(t.Shape.Delimiter)
	}

	for _, c := range p.Columns {
		if c.Role == profile.RoleUnknown {
			continue
		}
		out.Fields = append(out.Fields, Field{
			Role:       string(c.Role),
			Column:     c.Index,
			Header:     c.Header,
			Confidence: round2(c.RoleConfidence),
			Reason:     c.RoleReason,
		})
	}
	sort.Slice(out.Fields, func(i, j int) bool { return out.Fields[i].Column < out.Fields[j].Column })

	// Everything the file genuinely cannot answer.
	for _, c := range p.Columns {
		if c.Role != profile.RoleDate {
			continue
		}
		switch {
		case len(c.DateFormats) == 1:
			out.Format.DateLayout = c.DateFormats[0]
		case len(c.DateFormats) > 1:
			out.Unresolved = append(out.Unresolved, fmt.Sprintf(
				"date_layout: column %q parses as %s. Only one is right and the file does not say which. "+
					"Set it here once, from the society, not per import.",
				c.Header, strings.Join(c.DateFormats, " or ")))
		}
	}

	for _, c := range p.Columns {
		if c.Role == profile.RoleQuantity {
			out.Unresolved = append(out.Unresolved, fmt.Sprintf(
				"quantity_unit: column %q runs %.2f–%.2f, which is plausible as either litres or "+
					"kilograms. They differ by about three per cent, which is larger than most divergences "+
					"worth finding.", c.Header, c.Min, c.Max))
		}
		if c.Role == profile.RoleShift {
			var vals []string
			for _, v := range c.TopValues {
				vals = append(vals, v.Value)
			}
			out.Format.ShiftValues = map[string]string{}
			for _, v := range vals {
				out.Format.ShiftValues[v] = ""
			}
			out.Unresolved = append(out.Unresolved, fmt.Sprintf(
				"shift_values: column %q holds %s. Map each onto MORNING or EVENING; a wrong mapping "+
					"moves a collection to the other end of the day.", c.Header, strings.Join(vals, ", ")))
		}
	}

	for _, c := range p.Columns {
		if c.Role != profile.RoleUnknown || c.Kind == profile.KindEmpty {
			continue
		}
		out.Unresolved = append(out.Unresolved, fmt.Sprintf(
			"column %d %q (%s) was not identified. Samples: %s. Either map it or record that it is not "+
				"needed — an unexplained column in a payment file is worth one question.",
			c.Index+1, c.Header, c.Kind, strings.Join(c.Samples, ", ")))
	}

	// Low-confidence guesses are unresolved too, however plausible they look.
	for _, f := range out.Fields {
		if f.Confidence < 0.6 {
			out.Unresolved = append(out.Unresolved, fmt.Sprintf(
				"%s was matched to column %d %q with confidence %.2f — %s. Confirm before importing.",
				f.Role, f.Column+1, f.Header, f.Confidence, f.Reason))
		}
	}

	return out
}

// IsDraft reports whether anything still needs a person's decision.
func (p *Profile) IsDraft() bool { return len(p.Unresolved) > 0 }

// Write emits the profile as JSON for a person to edit and a later import to read.
func (p *Profile) Write(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// Load reads a profile a person has completed.
func Load(r io.Reader) (*Profile, error) {
	var p Profile
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	return &p, nil
}

// Validate refuses a profile that would import money incorrectly.
func (p *Profile) Validate() error {
	if p.Vendor == "" {
		return fmt.Errorf("the profile does not say which system wrote the file")
	}
	have := map[string]bool{}
	for _, f := range p.Fields {
		have[f.Role] = true
	}
	for _, r := range []string{"producer_code", "collected_on", "quantity", "fat_percent"} {
		if !have[r] {
			return fmt.Errorf("no column is mapped to %s, so a collection could not be settled", r)
		}
	}
	if have["collected_on"] && p.Format.DateLayout == "" {
		return fmt.Errorf("date_layout is not set; the import would have to guess which way round the dates read")
	}
	if have["quantity"] && p.Format.QuantityUnit == "" {
		return fmt.Errorf("quantity_unit is not set; litres and kilograms differ by about three per cent")
	}
	for raw, mapped := range p.Format.ShiftValues {
		if mapped == "" {
			return fmt.Errorf("shift value %q is not mapped to a milking session", raw)
		}
	}
	return nil
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
