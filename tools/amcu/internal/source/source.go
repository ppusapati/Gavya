// Package source reads a collection export as it actually arrives, rather than
// as a specification says it should.
//
// An AMCU export is written by whatever the vendor shipped to that society,
// often years ago. In practice it arrives as a delimited file whose separator
// nobody wrote down, in an encoding that is not UTF-8, with a header row that
// may or may not be there, sometimes with a byte-order mark, sometimes with
// trailing junk from the printer driver. Reading it is the first place a naive
// importer gets the data wrong — and getting it wrong here corrupts everything
// downstream while looking like it worked.
//
// So nothing here is assumed. Every choice the reader makes is reported, so an
// operator can see what it decided and disagree.
package source

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Encoding is what the bytes turned out to be.
type Encoding string

const (
	UTF8    Encoding = "UTF-8"
	UTF8BOM Encoding = "UTF-8 with BOM"
	UTF16LE Encoding = "UTF-16 LE"
	UTF16BE Encoding = "UTF-16 BE"
	// Latin1 stands for any single-byte codepage. Which one it really is cannot
	// be known from the bytes alone; what matters is that it is not UTF-8 and
	// must not be read as if it were.
	Latin1 Encoding = "single-byte (Latin-1 or a regional codepage)"
)

// Shape is everything the reader worked out about the file, so that an operator
// can check its reasoning rather than trust it.
type Shape struct {
	Encoding Encoding
	// Delimiter is the byte that separated fields, or 0 for a fixed-width file.
	Delimiter byte
	// FixedWidths are the column widths when no delimiter was found.
	FixedWidths []int
	HasHeader   bool
	// HeaderReason says why the first row was or was not taken as a header.
	HeaderReason string
	Columns      int
	Rows         int
	// RaggedRows are rows whose field count differs from the header's. They are
	// kept, not dropped: a ragged row is usually a name containing the delimiter,
	// and discarding it silently loses a producer.
	RaggedRows []int
	// Notes are anything else worth an operator's attention.
	Notes []string
}

// Table is the file, read.
type Table struct {
	Shape  Shape
	Header []string
	Rows   [][]string
}

// Read works out what the file is and reads it.
func Read(r io.Reader) (*Table, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("the file is empty")
	}

	text, enc := decode(raw)
	lines := splitLines(text)
	if len(lines) == 0 {
		return nil, fmt.Errorf("the file has no lines")
	}

	shape := Shape{Encoding: enc}
	if enc == Latin1 {
		shape.Notes = append(shape.Notes,
			"the file is not UTF-8; producer names have been decoded as Latin-1 and may be wrong "+
				"for Devanagari, Gujarati or any other regional script")
	}

	delim, confidence := sniffDelimiter(lines)
	var rows [][]string
	if delim != 0 {
		shape.Delimiter = delim
		rows = splitDelimited(lines, delim)
		if confidence < 0.9 {
			shape.Notes = append(shape.Notes, fmt.Sprintf(
				"the delimiter %q was inferred with low confidence; check the field count", string(delim)))
		}
	} else {
		widths := sniffFixedWidths(lines)
		if len(widths) == 0 {
			return nil, fmt.Errorf("no delimiter was found and the columns do not line up, so this is not a table")
		}
		shape.FixedWidths = widths
		rows = splitFixed(lines, widths)
		shape.Notes = append(shape.Notes,
			"no delimiter was found; the file was read as fixed-width from where the columns line up")
	}

	t := &Table{Shape: shape}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no rows could be read")
	}

	t.Shape.HasHeader, t.Shape.HeaderReason = looksLikeHeader(rows)
	if t.Shape.HasHeader {
		t.Header = rows[0]
		rows = rows[1:]
	} else {
		for i := range rows[0] {
			t.Header = append(t.Header, fmt.Sprintf("column_%d", i+1))
		}
	}

	want := len(t.Header)
	for i, row := range rows {
		if len(row) != want {
			t.Shape.RaggedRows = append(t.Shape.RaggedRows, i+1)
		}
		// Pad or keep as-is; nothing is discarded.
		for len(row) < want {
			row = append(row, "")
		}
		rows[i] = row
	}

	t.Rows = rows
	t.Shape.Columns = want
	t.Shape.Rows = len(rows)

	if n := len(t.Shape.RaggedRows); n > 0 {
		t.Shape.Notes = append(t.Shape.Notes, fmt.Sprintf(
			"%d rows have a different field count from the header, which usually means a value "+
				"contains the delimiter — a producer name with a comma, most often. They are kept.", n))
	}
	return t, nil
}

// decode turns bytes into text, saying which encoding it decided on.
func decode(raw []byte) (string, Encoding) {
	switch {
	case bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}):
		return string(raw[3:]), UTF8BOM
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		return decodeUTF16(raw[2:], false), UTF16LE
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		return decodeUTF16(raw[2:], true), UTF16BE
	case utf8.Valid(raw):
		return string(raw), UTF8
	}
	// Not valid UTF-8. Decoding each byte as a code point is Latin-1, which at
	// least round-trips the digits and ASCII that the numbers live in.
	var b strings.Builder
	b.Grow(len(raw))
	for _, c := range raw {
		b.WriteRune(rune(c))
	}
	return b.String(), Latin1
}

func decodeUTF16(raw []byte, bigEndian bool) string {
	if len(raw)%2 == 1 {
		raw = raw[:len(raw)-1]
	}
	u := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		if bigEndian {
			u = append(u, uint16(raw[i])<<8|uint16(raw[i+1]))
		} else {
			u = append(u, uint16(raw[i+1])<<8|uint16(raw[i]))
		}
	}
	return string(utf16.Decode(u))
}

func splitLines(text string) []string {
	s := bufio.NewScanner(strings.NewReader(text))
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var out []string
	for s.Scan() {
		line := strings.TrimRight(s.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// candidates are the separators an export is realistically written with. Tab
// first: it is the least likely to appear inside a producer's name.
var candidates = []byte{'\t', '|', ';', ',', ':'}

// sniffDelimiter picks the separator that gives the most consistent field count
// across the file, which is a stronger signal than simply counting occurrences —
// a name full of commas beats a comma-delimited file on frequency alone.
func sniffDelimiter(lines []string) (byte, float64) {
	sample := lines
	if len(sample) > 200 {
		sample = sample[:200]
	}

	best, bestScore := byte(0), 0.0
	for _, d := range candidates {
		counts := map[int]int{}
		for _, l := range sample {
			n := strings.Count(l, string(d))
			if n > 0 {
				counts[n]++
			}
		}
		if len(counts) == 0 {
			continue
		}
		// The score is the share of lines agreeing on the most common count.
		top, total := 0, 0
		for _, c := range counts {
			total += c
			if c > top {
				top = c
			}
		}
		score := float64(top) / float64(len(sample))
		// A single field is not a table.
		if total == 0 || score <= bestScore {
			continue
		}
		best, bestScore = d, score
	}
	if bestScore < 0.5 {
		return 0, 0
	}
	return best, bestScore
}

// splitDelimited splits on the separator, honouring double quotes so a quoted
// name containing the delimiter stays one field.
func splitDelimited(lines []string, d byte) [][]string {
	out := make([][]string, 0, len(lines))
	for _, l := range lines {
		var fields []string
		var cur strings.Builder
		inQuote := false
		for i := 0; i < len(l); i++ {
			c := l[i]
			switch {
			case c == '"' && inQuote && i+1 < len(l) && l[i+1] == '"':
				cur.WriteByte('"')
				i++
			case c == '"':
				inQuote = !inQuote
			case c == d && !inQuote:
				fields = append(fields, strings.TrimSpace(cur.String()))
				cur.Reset()
			default:
				cur.WriteByte(c)
			}
		}
		fields = append(fields, strings.TrimSpace(cur.String()))
		out = append(out, fields)
	}
	return out
}

// sniffFixedWidths finds the byte offsets that are blank on every line, which is
// where a fixed-width report's columns meet.
func sniffFixedWidths(lines []string) []int {
	sample := lines
	if len(sample) > 200 {
		sample = sample[:200]
	}
	width := 0
	for _, l := range sample {
		if len(l) > width {
			width = len(l)
		}
	}
	if width == 0 {
		return nil
	}

	blank := make([]bool, width)
	for i := range blank {
		blank[i] = true
	}
	for _, l := range sample {
		for i := 0; i < width; i++ {
			if i < len(l) && l[i] != ' ' {
				blank[i] = false
			}
		}
	}

	var bounds []int
	inGap := false
	for i := 0; i < width; i++ {
		if blank[i] && !inGap {
			inGap = true
			bounds = append(bounds, i)
		} else if !blank[i] {
			inGap = false
		}
	}
	if len(bounds) < 2 {
		return nil
	}

	sort.Ints(bounds)
	widths := make([]int, 0, len(bounds))
	prev := 0
	for _, b := range bounds {
		if b > prev {
			widths = append(widths, b-prev)
			prev = b
		}
	}
	if width > prev {
		widths = append(widths, width-prev)
	}
	return widths
}

func splitFixed(lines []string, widths []int) [][]string {
	out := make([][]string, 0, len(lines))
	for _, l := range lines {
		fields := make([]string, 0, len(widths))
		pos := 0
		for _, w := range widths {
			end := pos + w
			if pos >= len(l) {
				fields = append(fields, "")
				continue
			}
			if end > len(l) {
				end = len(l)
			}
			fields = append(fields, strings.TrimSpace(l[pos:end]))
			pos += w
		}
		out = append(out, fields)
	}
	return out
}

// looksLikeHeader decides whether the first row names the columns or is data.
//
// The test is not whether the row contains words: a producer name is a word too.
// It is whether the first row looks unlike the rows beneath it — headers are
// text where the body is numeric, and headers do not repeat.
func looksLikeHeader(rows [][]string) (bool, string) {
	if len(rows) < 2 {
		return false, "there is only one row, so it was read as data"
	}
	first := rows[0]

	firstNumeric := 0
	for _, v := range first {
		if isNumeric(v) {
			firstNumeric++
		}
	}
	if firstNumeric > len(first)/2 {
		return false, "the first row is mostly numbers, so it was read as data"
	}

	body := rows[1:]
	if len(body) > 50 {
		body = body[:50]
	}
	bodyNumeric := 0
	for _, r := range body {
		for _, v := range r {
			if isNumeric(v) {
				bodyNumeric++
			}
		}
	}
	share := float64(bodyNumeric) / float64(len(body)*len(first))
	if share > 0.25 && firstNumeric == 0 {
		return true, "the first row is entirely non-numeric while the rows below are largely numeric"
	}

	// Distinct, short, non-empty values with no repeats read like column names.
	seen := map[string]bool{}
	for _, v := range first {
		if v == "" || seen[strings.ToLower(v)] {
			return false, "the first row has blank or repeated values, which column names do not"
		}
		seen[strings.ToLower(v)] = true
	}
	return true, "the first row has distinct non-numeric values, unlike the rows below"
}

func isNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.' || r == ',' || r == '-' || r == '+' || r == ' ':
		default:
			return false
		}
	}
	return digits > 0
}
