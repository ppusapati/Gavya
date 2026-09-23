// Package report is the catalogue of reports this platform can produce, and
// the rendering of one.
//
// # Why there is a catalogue at all
//
// `report_type` is a free string on the wire and was a free string all the way
// down, because nothing ever read it. A runner has to read it, and the moment
// it does the question becomes: what happens to a type nobody wrote? The answer
// here is that it is refused by name. The alternative — produce an empty file
// and mark the report complete — is the defect this platform keeps finding in
// itself: a control that reports success while doing nothing.
//
// # What is in it, and what is not
//
// Three types, each drawn from a procedure another service already serves. This
// package does not reimplement anybody's query and does not reach into anybody's
// database; it asks the service that owns the data, which is the only way the
// answer carries that service's rules with it.
//
// Two things are deliberately absent.
//
// There is no yield report. A yield needs a density to convert between litres
// and kilograms, production-service refuses to assume one, and a runner working
// unattended has nobody to ask — so the only unattended yield report this
// platform could produce is one resting on a number nobody measured. That is a
// report it should not produce, and the honest way to say so is to have no such
// type rather than a type that quietly picks 1.03.
//
// There is no "all of it" type. Every report names a period or a cycle, because
// a report with no bound is one whose size depends on how long the co-operative
// has been running.
package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The three ways a render can fail before it has read anything, kept apart
// because a caller acts on them differently.
//
// All three are permanent for the report that hit them: nothing about waiting
// and trying again makes an unknown type known or a missing setting set. The
// runner records the reason and stops, rather than retrying a request that
// cannot succeed until a person changes something.
var (
	// ErrUnknownKind is a report_type that is not in the catalogue.
	ErrUnknownKind = errors.New("unknown report type")
	// ErrBadParameters is a parameters string that is not what the type needs.
	ErrBadParameters = errors.New("parameters")
	// ErrNoSource is a type whose data comes from a service this deployment was
	// not told how to reach.
	ErrNoSource = errors.New("no source configured")
)

// MaxRows is where a render stops.
//
// A report is pulled into memory and stored as one value, so it has to have a
// ceiling; without one a co-operative with five years of history asks for a
// collections report and the runner is killed by the pod's memory limit,
// repeatedly, with nothing in the reports table to say why.
//
// Fifty thousand rows of collections is about six megabytes, which is a large
// answer and a reasonable one. What matters more than the number is that the
// report says when it stopped: see Rendered.Truncated.
const MaxRows = 50_000

// Rendered is a finished report.
type Rendered struct {
	Content     []byte
	ContentType string
	// Rows is how many rows of data the report carries, not counting its header.
	Rows int64
	// Truncated says the answer stopped at MaxRows.
	//
	// Carried separately and written to its own column, because a truncated
	// report that does not say so is worse than no report: it is a total
	// somebody will act on, short by an amount nothing on the page discloses.
	Truncated bool
}

// Window is a period a report covers, resolved to instants.
type Window struct {
	From time.Time
	To   time.Time
}

// Params is a report's parameters, as the caller wrote them.
//
// Held as a decoded map rather than a struct because each type needs different
// keys, and as strings because every value that matters here — a date, a cycle
// id — is one. A JSON number here would be the one place in this service where
// a figure passed through a float.
type Params map[string]string

// ParseParams reads the parameters column.
//
// An empty string is an empty set rather than an error: a type that needs
// nothing is entitled to be asked for with nothing. Anything else has to be a
// JSON object whose values are strings, numbers or booleans — an object or an
// array inside would be a shape no type here reads, and accepting it would let
// a caller believe it had been understood.
func ParseParams(raw string) (Params, error) {
	out := Params{}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" || trimmed == "null" {
		return out, nil
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(trimmed), &generic); err != nil {
		return nil, fmt.Errorf("%w: %s is not a JSON object", ErrBadParameters, trimmed)
	}
	for k, v := range generic {
		switch t := v.(type) {
		case string:
			out[k] = t
		case bool:
			out[k] = fmt.Sprintf("%t", t)
		case float64:
			// Formatted without an exponent and without a trailing ".0", so a
			// limit written as 100 arrives as "100".
			out[k] = strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%f", t), "0"), ".")
		case nil:
			out[k] = ""
		default:
			return nil, fmt.Errorf("%w: %q holds a %T, and every parameter this platform "+
				"reads is a single value", ErrBadParameters, k, v)
		}
	}
	return out, nil
}

// Kind is one report this platform can produce.
type Kind struct {
	// Name is what goes in report_type.
	Name string
	// Summary is one line, for a person choosing between them.
	Summary string
	// Needs are the parameter keys without which this type cannot run.
	Needs []string
	// Schedulable says whether a schedule can produce this type.
	//
	// False for a type that needs something a schedule has no way to supply.
	// A settlement summary names a cycle, and a schedule firing at two in the
	// morning does not know which cycle is meant — so it is refused when the
	// schedule is written rather than at two in the morning, which is the
	// difference between an error message and a report that never arrives.
	Schedulable bool
	// Window says this type is bounded by a period rather than by an
	// identifier, which is what a schedule can supply.
	Window bool

	render func(context.Context, *Sources, string, Params) (Rendered, error)
}

// Catalogue is every type, by name.
func Catalogue() []Kind {
	out := make([]Kind, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lookup finds a type by name.
func Lookup(name string) (Kind, error) {
	k, ok := kinds[name]
	if !ok {
		names := make([]string, 0, len(kinds))
		for n := range kinds {
			names = append(names, n)
		}
		sort.Strings(names)
		return Kind{}, fmt.Errorf("%w %q; this platform produces: %s",
			ErrUnknownKind, name, strings.Join(names, ", "))
	}
	return k, nil
}

// Render produces one report.
func Render(ctx context.Context, src *Sources, name, tenantID string, params Params) (Rendered, error) {
	k, err := Lookup(name)
	if err != nil {
		return Rendered{}, err
	}
	for _, need := range k.Needs {
		if strings.TrimSpace(params[need]) == "" {
			return Rendered{}, fmt.Errorf("%w: %s needs %q and it is empty",
				ErrBadParameters, k.Name, need)
		}
	}
	if src == nil {
		return Rendered{}, fmt.Errorf("%w for %s", ErrNoSource, k.Name)
	}
	return k.render(ctx, src, tenantID, params)
}

// ParseWindow reads a from/to pair out of parameters.
//
// Dates are read as whole days in loc and the period is half-open: from the
// start of `from` to the start of the day after `to`. A fortnight asked for as
// 1 to 15 September includes the whole of the 15th, which is what everybody
// means by it and not what an instant-to-instant reading gives.
func ParseWindow(p Params, loc *time.Location) (Window, error) {
	if loc == nil {
		loc = time.UTC
	}
	from, err := parseDay(p["from"], "from", loc)
	if err != nil {
		return Window{}, err
	}
	to, err := parseDay(p["to"], "to", loc)
	if err != nil {
		return Window{}, err
	}
	if to.Before(from) {
		return Window{}, fmt.Errorf("%w: the period runs backwards, from %s to %s",
			ErrBadParameters, p["from"], p["to"])
	}
	return Window{From: from, To: to.AddDate(0, 0, 1)}, nil
}

func parseDay(text, field string, loc *time.Location) (time.Time, error) {
	t := strings.TrimSpace(text)
	if t == "" {
		return time.Time{}, fmt.Errorf("%w: %s is required and a report is never unbounded",
			ErrBadParameters, field)
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, t, loc); err == nil {
			return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, loc), nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %s is %q, which is neither a date nor a timestamp",
		ErrBadParameters, field, t)
}

// Windows a schedule can ask for, resolved against the moment it fired.
//
// Three, named, and anything else refused. The alternative — letting a schedule
// carry arbitrary from/to dates — would have every firing produce the same
// report over the same fixed period for ever, which is a scheduled report that
// is wrong in a way nobody notices until they compare two of them.
const (
	WindowYesterday = "yesterday"
	WindowLast7Days = "last_7_days"
	WindowLastMonth = "last_month"
)

// ResolveWindow turns a schedule's window keyword into from/to dates.
//
// Resolved in loc, which is the schedule's own zone: "yesterday" is a local
// day, and read in UTC it is a different day for more than half the world.
func ResolveWindow(keyword string, firedAt time.Time, loc *time.Location) (Params, error) {
	if loc == nil {
		loc = time.UTC
	}
	day := func(t time.Time) string { return t.In(loc).Format("2006-01-02") }
	today := firedAt.In(loc)

	switch strings.TrimSpace(keyword) {
	case WindowYesterday:
		y := today.AddDate(0, 0, -1)
		return Params{"from": day(y), "to": day(y)}, nil
	case WindowLast7Days:
		// The seven whole days before today, so a report that fires this
		// morning does not include a few hours of this morning and call it a
		// day.
		return Params{"from": day(today.AddDate(0, 0, -7)), "to": day(today.AddDate(0, 0, -1))}, nil
	case WindowLastMonth:
		first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
		lastDay := first.AddDate(0, 0, -1)
		firstDay := time.Date(lastDay.Year(), lastDay.Month(), 1, 0, 0, 0, 0, loc)
		return Params{"from": day(firstDay), "to": day(lastDay)}, nil
	case "":
		return nil, fmt.Errorf("%w: a scheduled report needs a window, because the period it "+
			"covers has to move with the firing; this platform resolves %s, %s and %s",
			ErrBadParameters, WindowYesterday, WindowLast7Days, WindowLastMonth)
	default:
		return nil, fmt.Errorf("%w: window %q is not one this platform resolves; it reads "+
			"%s, %s and %s", ErrBadParameters, keyword,
			WindowYesterday, WindowLast7Days, WindowLastMonth)
	}
}
