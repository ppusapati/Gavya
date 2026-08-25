package capture

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// chunk is one write from the instrument, with what the operator said was
// happening at the time. Where the writes fell is itself evidence — an
// instrument that sends one reading per write has told you its framing.
type chunk struct {
	at   time.Time
	b    []byte
	mark string
}

// Frame is one message, as the recording suggests the instrument meant it.
type Frame struct {
	At    time.Time
	Bytes []byte
	// Mark is what the operator said was happening when this arrived. It is
	// carried forward from the last mark, because an operator annotates once and
	// then does the thing.
	Mark string
}

// Framing is how the messages were separated, and why that was believed.
type Framing struct {
	// Terminator is the byte sequence every frame ended with, when there was one.
	Terminator []byte
	// FixedLength is set when every frame was the same length instead.
	FixedLength int
	// IdleGap is the silence that separated messages when neither of the above
	// held.
	IdleGap time.Duration
	Reason  string
}

// Analysis is what a recording turned out to contain.
type Analysis struct {
	Framing Framing
	Frames  []Frame

	// Lengths counts how many frames had each length. One length is a fixed
	// record; a handful is a small set of message types; hundreds means the
	// framing is probably wrong.
	Lengths map[int]int

	// Constant is the byte offsets that never varied across frames of the
	// commonest length — headers, addresses, unit markers. Everything else is
	// where the reading lives.
	Constant []Position
	Varying  []Position

	// Correlations are offsets whose contents match a value the operator wrote
	// down. This is what turns a recording into a protocol.
	Correlations []Correlation

	Notes []string
}

// Position is one byte offset and what was seen there.
type Position struct {
	Offset int
	// Values are the distinct bytes seen at this offset, most common first.
	Values []byte
	// Printable is the ASCII rendering when the bytes are text, which most
	// instrument protocols turn out to be.
	Printable string
}

// Correlation is a place in the frame that carries a number the operator knew.
type Correlation struct {
	// Mark is what the operator wrote down, e.g. "known weight 5.00 L".
	Mark string
	// Value is the number found inside it.
	Value float64
	// Offset and Length say where in the frame it was found.
	Offset, Length int
	// Encoding is how it was written there.
	Encoding string
	// Sample is the bytes at that position, as they appeared.
	Sample string
}

// AnalyseOptions tunes the framing search.
type AnalyseOptions struct {
	// IdleGap is the silence taken to end a message when no terminator is found.
	IdleGap time.Duration
}

// Analyse reconstructs messages from a recording and reports what varies.
//
// It never claims to have decoded anything. Instrument protocols are various
// enough that a confident wrong answer is worse than a set of observations an
// engineer can read — so this reports what is constant, what varies, and where
// the operator's own known values appear in the bytes, and leaves the conclusion
// to a person.
func Analyse(events []Event, opts AnalyseOptions) *Analysis {
	if opts.IdleGap <= 0 {
		opts.IdleGap = 50 * time.Millisecond
	}
	a := &Analysis{Lengths: map[int]int{}}

	// Marks and bytes interleave; a mark applies to what comes after it.
	var chunks []chunk
	mark := ""
	for _, e := range events {
		if e.Mark != "" {
			mark = e.Mark
			continue
		}
		if e.Bytes == "" {
			continue
		}
		chunks = append(chunks, chunk{e.At, e.Decoded(), mark})
	}
	if len(chunks) == 0 {
		a.Notes = append(a.Notes, "the recording contains no bytes")
		return a
	}

	// Concatenate, keeping each byte's arrival time and mark so framing can use
	// silence and so a frame knows what was happening when it arrived.
	var flat []byte
	var times []time.Time
	var marks []string
	for _, c := range chunks {
		for range c.b {
			times = append(times, c.at)
			marks = append(marks, c.mark)
		}
		flat = append(flat, c.b...)
	}

	a.Framing = findFraming(chunks, flat, opts.IdleGap)
	var tail int
	a.Frames, tail = splitFrames(flat, times, marks, a.Framing, opts.IdleGap)
	if tail > 0 {
		a.Notes = append(a.Notes, fmt.Sprintf(
			"the recording ends mid-message: the last %d bytes are not a complete frame and were left out. "+
				"That is the normal way a bench session ends, and counting them as a frame would make every "+
				"field look inconsistent", tail))
	}

	for _, f := range a.Frames {
		a.Lengths[len(f.Bytes)]++
	}
	if len(a.Frames) == 0 {
		a.Notes = append(a.Notes, "no frames could be separated; try a different idle gap")
		return a
	}

	common := commonestLength(a.Lengths)
	var sameLength [][]byte
	for _, f := range a.Frames {
		if len(f.Bytes) == common {
			sameLength = append(sameLength, f.Bytes)
		}
	}
	a.Constant, a.Varying = positions(sameLength)

	if len(a.Lengths) > 1 {
		a.Notes = append(a.Notes, fmt.Sprintf(
			"frames came in %d different lengths; the byte map below covers only the commonest (%d bytes, %d frames)",
			len(a.Lengths), common, a.Lengths[common]))
	}

	a.Correlations = correlate(a.Frames)
	a.Notes = append(a.Notes, alignmentNotes(a.Correlations)...)
	if len(a.Correlations) == 0 {
		a.Notes = append(a.Notes, "no operator marks contained a number, so nothing could be tied to a known "+
			"reading — mark a few frames with what the display said and run this again")
	}
	return a
}

// terminators are the common ones, longest first so CRLF beats CR and LF.
var terminators = [][]byte{{'\r', '\n'}, {'\n'}, {'\r'}, {0x03}, {0x04}}

// findFraming works out how messages were separated.
//
// A terminator is looked for first because it is the strongest evidence: if
// every chunk ends in the same bytes, that is the instrument telling you where
// its messages end.
func findFraming(chunks []chunk, flat []byte, gap time.Duration) Framing {
	for _, term := range terminators {
		if endsConsistentlyWith(chunks, term) {
			return Framing{
				Terminator: term,
				Reason: fmt.Sprintf("every chunk the instrument sent ended with %s, so that is where its "+
					"messages end", describe(term)),
			}
		}
	}

	// A message often arrives in more than one read — at 9600 baud a forty-byte
	// line usually does — so a terminator that is not at every read boundary is
	// still there to be found in the stream itself. Missing it costs the whole
	// analysis: the fallback is silence-based framing, which at that line rate
	// cuts messages in half.
	for _, term := range terminators {
		if n, ok := terminatesStream(flat, term); ok {
			return Framing{
				Terminator: term,
				Reason: fmt.Sprintf("the stream divides into %d printable messages ending with %s, though not "+
					"at the read boundaries — a message arriving in two reads is normal", n, describe(term)),
			}
		}
	}

	// Every write the same size is a fixed record.
	size, uniform := uniformChunkSize(chunks)
	if uniform && size > 1 {
		return Framing{
			FixedLength: size,
			Reason:      fmt.Sprintf("every write was exactly %d bytes, which is a fixed-length record", size),
		}
	}
	return Framing{
		IdleGap: gap,
		Reason: fmt.Sprintf("no terminator or fixed length was evident, so messages were separated by "+
			"silences longer than %s", gap),
	}
}

// terminatesStream reports whether the stream divides cleanly on this
// terminator.
//
// Requiring what sits between the terminators to be printable text is what keeps
// this from firing on binary data that happens to contain an 0x0a. It also
// rejects the wrong half of a CRLF: between two lone LFs sits a CR, which is not
// printable, so CRLF data cannot be misread as LF data.
func terminatesStream(flat, term []byte) (int, bool) {
	n, start := 0, 0
	for i := 0; i+len(term) <= len(flat); i++ {
		if string(flat[i:i+len(term)]) != string(term) {
			continue
		}
		if body := flat[start:i]; len(body) == 0 || !printableASCII(body) {
			return 0, false
		}
		n++
		start = i + len(term)
		i += len(term) - 1
	}
	// Three is the smallest number that distinguishes a pattern from a pair of
	// bytes that happened to line up.
	return n, n >= 3
}

func printableASCII(b []byte) bool {
	for _, c := range b {
		if c == '\t' {
			continue
		}
		if c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

func endsConsistentlyWith(chunks []chunk, term []byte) bool {
	n := 0
	for _, c := range chunks {
		if len(c.b) < len(term) {
			return false
		}
		if string(c.b[len(c.b)-len(term):]) != string(term) {
			return false
		}
		n++
	}
	return n >= 2
}

func uniformChunkSize(chunks []chunk) (int, bool) {
	if len(chunks) < 2 {
		return 0, false
	}
	size := len(chunks[0].b)
	for _, c := range chunks {
		if len(c.b) != size {
			return 0, false
		}
	}
	return size, true
}

func describe(b []byte) string {
	names := map[byte]string{'\r': "CR", '\n': "LF", 0x03: "ETX", 0x04: "EOT", 0x02: "STX"}
	var parts []string
	for _, c := range b {
		if n, ok := names[c]; ok {
			parts = append(parts, n)
		} else {
			parts = append(parts, fmt.Sprintf("0x%02x", c))
		}
	}
	return strings.Join(parts, " ")
}

// splitFrames cuts the stream into messages, and reports how many trailing bytes
// were left over.
//
// The leftovers matter. A bench session ends when somebody unplugs something, so
// a recording usually stops in the middle of a message. Counting that stub as a
// frame is quietly destructive, because a field is only reported when it appears
// at the same offset in every frame: a stub cut before the reading suppresses the
// field entirely, and one cut through it shortens the field to whatever survived
// the cut — a four-byte weight reported as one byte. The second is the worse of
// the two, because somebody would go and write a parser to it.
func splitFrames(flat []byte, times []time.Time, marks []string, f Framing, gap time.Duration) ([]Frame, int) {
	var out []Frame
	emit := func(start, end int) {
		if end <= start {
			return
		}
		b := make([]byte, end-start)
		copy(b, flat[start:end])
		out = append(out, Frame{At: times[start], Bytes: b, Mark: marks[start]})
	}

	switch {
	case len(f.Terminator) > 0:
		start := 0
		for i := 0; i+len(f.Terminator) <= len(flat); i++ {
			if string(flat[i:i+len(f.Terminator)]) == string(f.Terminator) {
				emit(start, i+len(f.Terminator))
				start = i + len(f.Terminator)
				i += len(f.Terminator) - 1
			}
		}
		return out, len(flat) - start

	case f.FixedLength > 0:
		for i := 0; i+f.FixedLength <= len(flat); i += f.FixedLength {
			emit(i, i+f.FixedLength)
		}
		return out, len(flat) % f.FixedLength

	default:
		// With no terminator and no fixed length, silence is all there is to go
		// on, and the last run is as trustworthy as any other — there is nothing
		// that says it was cut short.
		start := 0
		for i := 1; i < len(flat); i++ {
			if times[i].Sub(times[i-1]) >= gap {
				emit(start, i)
				start = i
			}
		}
		emit(start, len(flat))
		return out, 0
	}
}

func commonestLength(lengths map[int]int) int {
	best, bestN := 0, 0
	for l, n := range lengths {
		if n > bestN || (n == bestN && l > best) {
			best, bestN = l, n
		}
	}
	return best
}

// positions reports, for each byte offset, whether it ever changed.
func positions(frames [][]byte) (constant, varying []Position) {
	if len(frames) == 0 {
		return nil, nil
	}
	width := len(frames[0])
	for off := 0; off < width; off++ {
		seen := map[byte]int{}
		for _, f := range frames {
			if off < len(f) {
				seen[f[off]]++
			}
		}
		var vals []byte
		for b := range seen {
			vals = append(vals, b)
		}
		sort.Slice(vals, func(i, j int) bool { return seen[vals[i]] > seen[vals[j]] })
		if len(vals) > 12 {
			vals = vals[:12]
		}

		p := Position{Offset: off, Values: vals, Printable: printable(vals)}
		if len(seen) == 1 {
			constant = append(constant, p)
		} else {
			varying = append(varying, p)
		}
	}
	return constant, varying
}

func printable(b []byte) string {
	var out strings.Builder
	for _, c := range b {
		if c >= 0x20 && c < 0x7f {
			out.WriteByte(c)
		} else {
			out.WriteByte('.')
		}
	}
	return out.String()
}

// correlate finds where a value the operator wrote down appears in the bytes.
//
// This is the point of marking. An operator puts a known five-litre weight on
// the pan and types "known weight 5.00 L"; if 35 2e 30 30 — "5.00" — appears at
// offset 7 of every frame that arrived while that mark stood, the weight field
// is at offset 7 and it is ASCII. That is a protocol mapped in one afternoon
// rather than one guessed at over a week.
//
// Three encodings are searched, which between them cover most instruments: ASCII
// decimal, packed BCD, and a scaled binary integer.
func correlate(frames []Frame) []Correlation {
	byMark := map[string][]Frame{}
	for _, f := range frames {
		if f.Mark == "" {
			continue
		}
		byMark[f.Mark] = append(byMark[f.Mark], f)
	}

	var out []Correlation
	for mark, fs := range byMark {
		for _, value := range numbersIn(mark) {
			if c, ok := findASCII(mark, value, fs); ok {
				out = append(out, c)
				continue
			}
			if c, ok := findBCD(mark, value, fs); ok {
				out = append(out, c)
				continue
			}
			if c, ok := findScaledInt(mark, value, fs); ok {
				out = append(out, c)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Offset != out[j].Offset {
			return out[i].Offset < out[j].Offset
		}
		return out[i].Mark < out[j].Mark
	})
	return out
}

// alignmentNotes turns two puzzling findings into one conclusion.
//
// Weigh 5 kg and then 12.5 kg and the analysis reports "5.00 at offset 9" and
// "12.50 at offset 8", which reads like two different fields. It is one field,
// right-aligned in a fixed column — the commonest way an instrument prints a
// number, and the difference between a parser that works and one that fails the
// first time a reading reaches double figures.
func alignmentNotes(cs []Correlation) []string {
	type field struct {
		offsets map[int]bool
		marks   int
	}
	byEnd := map[int]*field{}
	for _, c := range cs {
		if c.Encoding != "ASCII decimal" {
			continue
		}
		end := c.Offset + c.Length
		f := byEnd[end]
		if f == nil {
			f = &field{offsets: map[int]bool{}}
			byEnd[end] = f
		}
		f.offsets[c.Offset] = true
		f.marks++
	}

	var ends []int
	for end := range byEnd {
		ends = append(ends, end)
	}
	sort.Ints(ends)

	var out []string
	for _, end := range ends {
		f := byEnd[end]
		if f.marks < 2 || len(f.offsets) < 2 {
			continue
		}
		var offs []int
		for o := range f.offsets {
			offs = append(offs, o)
		}
		sort.Ints(offs)
		out = append(out, fmt.Sprintf(
			"the values found at offsets %s all end at offset %d, so that is one right-aligned field "+
				"occupying offsets %d–%d, not several fields — read it as a whole and trim the padding",
			join(offs), end-1, offs[0], end-1))
	}
	return out
}

func join(ns []int) string {
	var parts []string
	for _, n := range ns {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, " and ")
}

// numbersIn pulls the numbers out of an operator's note.
func numbersIn(s string) []float64 {
	var out []float64
	var cur strings.Builder
	flush := func() {
		// A note is written by a person, so it ends in a full stop as often as
		// not, and "fat reads 4.15." must not be thrown away for it.
		text := strings.Trim(cur.String(), ".")
		cur.Reset()
		if text == "" {
			return
		}
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			out = append(out, f)
		}
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// findASCII looks for the number written as text, at the same offset in every
// frame under the mark. The same offset matters: a match in one frame is a
// coincidence, a match in all of them is a field.
func findASCII(mark string, value float64, frames []Frame) (Correlation, bool) {
	// Try the plausible renderings, most specific first.
	var forms []string
	for _, dp := range []int{2, 1, 3, 0} {
		forms = append(forms, strconv.FormatFloat(value, 'f', dp, 64))
	}
	for _, form := range forms {
		offs := offsetsOf([]byte(form), frames)
		if len(offs) == 0 {
			continue
		}
		return Correlation{
			Mark: mark, Value: value, Offset: offs[0], Length: len(form),
			Encoding: "ASCII decimal", Sample: form,
		}, true
	}
	return Correlation{}, false
}

// findBCD looks for the number packed two digits to a byte.
func findBCD(mark string, value float64, frames []Frame) (Correlation, bool) {
	for _, dp := range []int{2, 1, 3, 0} {
		digits := strconv.FormatFloat(value, 'f', dp, 64)
		digits = strings.ReplaceAll(digits, ".", "")
		if len(digits)%2 == 1 {
			digits = "0" + digits
		}
		b := make([]byte, 0, len(digits)/2)
		ok := true
		for i := 0; i+1 < len(digits); i += 2 {
			hi, e1 := strconv.Atoi(string(digits[i]))
			lo, e2 := strconv.Atoi(string(digits[i+1]))
			if e1 != nil || e2 != nil {
				ok = false
				break
			}
			b = append(b, byte(hi<<4|lo))
		}
		if !ok || len(b) == 0 {
			continue
		}
		offs := offsetsOf(b, frames)
		if len(offs) == 0 {
			continue
		}
		return Correlation{
			Mark: mark, Value: value, Offset: offs[0], Length: len(b),
			Encoding: fmt.Sprintf("packed BCD, %d decimal places implied", dp),
			Sample:   fmt.Sprintf("% x", b),
		}, true
	}
	return Correlation{}, false
}

// findScaledInt looks for the number as a binary integer scaled by a power of
// ten — 5.00 stored as 500 — in either byte order and two widths.
func findScaledInt(mark string, value float64, frames []Frame) (Correlation, bool) {
	for _, scale := range []float64{100, 10, 1, 1000} {
		n := int64(math.Round(value * scale))
		if n < 0 || n > 0xFFFFFFFF {
			continue
		}
		for _, width := range []int{2, 4} {
			if width == 2 && n > 0xFFFF {
				continue
			}
			for _, big := range []bool{true, false} {
				b := make([]byte, width)
				for i := 0; i < width; i++ {
					shift := uint(8 * i)
					if big {
						shift = uint(8 * (width - 1 - i))
					}
					b[i] = byte(n >> shift)
				}
				offs := offsetsOf(b, frames)
				if len(offs) == 0 {
					continue
				}
				order := "little-endian"
				if big {
					order = "big-endian"
				}
				return Correlation{
					Mark: mark, Value: value, Offset: offs[0], Length: width,
					Encoding: fmt.Sprintf("%d-byte %s integer scaled by %.0f", width, order, scale),
					Sample:   fmt.Sprintf("% x", b),
				}, true
			}
		}
	}
	return Correlation{}, false
}

// offsetsOf returns the offsets where the needle appears in *every* frame.
func offsetsOf(needle []byte, frames []Frame) []int {
	if len(needle) == 0 || len(frames) == 0 {
		return nil
	}
	var candidates []int
	first := frames[0].Bytes
	for i := 0; i+len(needle) <= len(first); i++ {
		if string(first[i:i+len(needle)]) == string(needle) {
			candidates = append(candidates, i)
		}
	}
	var kept []int
	for _, off := range candidates {
		everywhere := true
		for _, f := range frames[1:] {
			if off+len(needle) > len(f.Bytes) ||
				string(f.Bytes[off:off+len(needle)]) != string(needle) {
				everywhere = false
				break
			}
		}
		if everywhere {
			kept = append(kept, off)
		}
	}
	return kept
}
