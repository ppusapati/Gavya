package capture

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

func bytesAt(ms int, s string) Event {
	return Event{At: at(ms), Bytes: hex.EncodeToString([]byte(s)), Len: len(s)}
}

func rawAt(ms int, b []byte) Event {
	return Event{At: at(ms), Bytes: hex.EncodeToString(b), Len: len(b)}
}

func markAt(ms int, what string) Event { return Event{At: at(ms), Mark: what} }

func TestALineTerminatorIsFoundAtTheReadBoundaries(t *testing.T) {
	a := Analyse([]Event{
		bytesAt(0, "FAT 4.15 SNF 8.60\r\n"),
		bytesAt(500, "FAT 4.20 SNF 8.55\r\n"),
		bytesAt(1000, "FAT 4.05 SNF 8.70\r\n"),
	}, AnalyseOptions{})

	if string(a.Framing.Terminator) != "\r\n" {
		t.Fatalf("terminator = %q, want CRLF (%s)", a.Framing.Terminator, a.Framing.Reason)
	}
	if len(a.Frames) != 3 {
		t.Fatalf("%d frames, want 3", len(a.Frames))
	}
}

// The defect this guards: the terminator was only looked for at read boundaries,
// and a message that arrives in two reads — which at 9600 baud is most of them —
// has no terminator at any boundary. The fallback is silence-based framing, which
// at that line rate cuts messages in half and makes every field look inconsistent.
func TestATerminatorIsFoundWhenMessagesSpanTwoReads(t *testing.T) {
	a := Analyse([]Event{
		bytesAt(0, "FAT 4.15 S"),
		bytesAt(20, "NF 8.60\r\nFAT 4.20 S"),
		bytesAt(40, "NF 8.55\r\nFAT 4.05 S"),
		bytesAt(60, "NF 8.70\r\n"),
	}, AnalyseOptions{IdleGap: 10 * time.Millisecond})

	if string(a.Framing.Terminator) != "\r\n" {
		t.Fatalf("terminator = %q, want CRLF (%s)", a.Framing.Terminator, a.Framing.Reason)
	}
	if len(a.Frames) != 3 {
		t.Fatalf("%d frames, want 3 — the reads were reassembled wrongly", len(a.Frames))
	}
	if got := string(a.Frames[0].Bytes); got != "FAT 4.15 SNF 8.60\r\n" {
		t.Errorf("first frame = %q", got)
	}
}

// Between two lone LFs in CRLF data sits a CR, which is not printable text, so
// the stream search must reject LF and go on to find CRLF.
func TestCRLFDataIsNotReadAsLFData(t *testing.T) {
	a := Analyse([]Event{
		bytesAt(0, "A 1.00\r\nA 2.00\r\nA 3.00\r\nA 4.00\r\n"),
	}, AnalyseOptions{})
	if string(a.Framing.Terminator) != "\r\n" {
		t.Fatalf("terminator = %q, want CRLF", a.Framing.Terminator)
	}
}

func TestAFixedLengthRecordIsRecognised(t *testing.T) {
	a := Analyse([]Event{
		rawAt(0, []byte{0x02, 0x10, 0x01, 0xF4}),
		rawAt(500, []byte{0x02, 0x10, 0x01, 0xF5}),
		rawAt(1000, []byte{0x02, 0x10, 0x01, 0xF6}),
	}, AnalyseOptions{})

	if a.Framing.FixedLength != 4 {
		t.Fatalf("fixed length = %d, want 4 (%s)", a.Framing.FixedLength, a.Framing.Reason)
	}
	if len(a.Frames) != 3 {
		t.Fatalf("%d frames, want 3", len(a.Frames))
	}
}

func TestSilenceSeparatesMessagesWhenNothingElseDoes(t *testing.T) {
	a := Analyse([]Event{
		rawAt(0, []byte{0xA1, 0x00}),
		rawAt(1, []byte{0x0F}),
		rawAt(400, []byte{0xA1, 0x00}),
		rawAt(401, []byte{0x11}),
	}, AnalyseOptions{IdleGap: 50 * time.Millisecond})

	if a.Framing.IdleGap == 0 {
		t.Fatalf("framing = %s, want the idle gap", a.Framing.Reason)
	}
	if len(a.Frames) != 2 {
		t.Fatalf("%d frames, want 2", len(a.Frames))
	}
}

// The defect this guards: a recording that stops mid-message — which is how
// every bench session ends — produced a truncated last frame. A field is only
// reported when it appears at the same offset in every frame, so the stub either
// suppressed the field or, when the cut fell inside the reading, shrank it to
// whatever survived: "5.25" at offset 3 came back as a one-byte field. The
// shrunken answer is the dangerous one, because somebody would write a parser to
// it and it would look like it worked until the reading passed 9.9.
//
// The stubs below are the two shapes: one cut through the number, one cut before
// it.
func TestAnUnfinishedLastMessageDoesNotDistortTheFindings(t *testing.T) {
	for _, stub := range []string{"W  5.2", "W "} {
		a := Analyse([]Event{
			markAt(0, "known weight 5.25 L"),
			bytesAt(10, "W  5.25 L\r\n"),
			bytesAt(20, "W  5.25 L\r\n"),
			bytesAt(30, "W  5.25 L\r\n"),
			bytesAt(40, stub), // somebody pulled the cable
		}, AnalyseOptions{})

		if len(a.Frames) != 3 {
			t.Fatalf("stub %q: %d frames, want 3 complete ones with the stub left out", stub, len(a.Frames))
		}
		if len(a.Correlations) != 1 {
			t.Fatalf("stub %q: %d correlations, want the weight field found", stub, len(a.Correlations))
		}
		if c := a.Correlations[0]; c.Offset != 3 || c.Length != 4 {
			t.Errorf("stub %q: weight field reported at offset %d, %d bytes wide; want offset 3, 4 bytes",
				stub, c.Offset, c.Length)
		}
		var said bool
		for _, n := range a.Notes {
			if strings.Contains(n, "mid-message") {
				said = true
			}
		}
		if !said {
			t.Errorf("stub %q: the leftover bytes were dropped without saying so: %v", stub, a.Notes)
		}
	}
}

// The whole point of marking. An operator puts a known five-litre weight on the
// pan and types what they did; the analysis has to say where that number lives.
func TestAKnownValueIsLocatedInTheFrame(t *testing.T) {
	a := Analyse([]Event{
		markAt(0, "known weight 5.00 L"),
		bytesAt(10, "ST,GS,+  5.00 L\r\n"),
		bytesAt(20, "ST,GS,+  5.00 L\r\n"),
		bytesAt(30, "ST,GS,+  5.00 L\r\n"),
	}, AnalyseOptions{})

	if len(a.Correlations) != 1 {
		t.Fatalf("%d correlations, want 1: %+v", len(a.Correlations), a.Correlations)
	}
	c := a.Correlations[0]
	if c.Offset != 9 || c.Length != 4 {
		t.Errorf("found at offset %d length %d, want offset 9 length 4", c.Offset, c.Length)
	}
	if c.Encoding != "ASCII decimal" {
		t.Errorf("encoding = %q", c.Encoding)
	}
}

// A number that appears in one frame is a coincidence. A number at the same
// offset in every frame is a field. Reporting the first would send an engineer
// after a byte that means nothing.
func TestANumberInOnlyOneFrameIsNotReportedAsAField(t *testing.T) {
	a := Analyse([]Event{
		markAt(0, "fat reads 4.15"),
		bytesAt(10, "FAT 4.15 X\r\n"),
		bytesAt(20, "FAT 3.90 X\r\n"),
		bytesAt(30, "FAT 4.02 X\r\n"),
	}, AnalyseOptions{})

	for _, c := range a.Correlations {
		if c.Value == 4.15 {
			t.Fatalf("a value present in one frame of three was reported as a field at offset %d", c.Offset)
		}
	}
}

func TestAScaledBinaryIntegerIsFound(t *testing.T) {
	// 5.00 litres stored as 500 = 0x01F4, big-endian, after a two-byte header.
	frame := []byte{0x02, 0x10, 0x01, 0xF4, 0x03}
	a := Analyse([]Event{
		markAt(0, "known weight 5.00 L"),
		rawAt(10, frame), rawAt(20, frame), rawAt(30, frame),
	}, AnalyseOptions{})

	if len(a.Correlations) == 0 {
		t.Fatal("a scaled integer was not found")
	}
	c := a.Correlations[0]
	if c.Offset != 2 || !strings.Contains(c.Encoding, "big-endian") {
		t.Errorf("found at offset %d as %q, want offset 2 big-endian", c.Offset, c.Encoding)
	}
}

func TestPackedBCDIsFound(t *testing.T) {
	// 4.15 as 0x04 0x15, which is not a plausible ASCII or scaled-int match.
	frame := []byte{0xAA, 0x04, 0x15, 0xBB}
	a := Analyse([]Event{
		markAt(0, "fat reads 4.15"),
		rawAt(10, frame), rawAt(20, frame), rawAt(30, frame),
	}, AnalyseOptions{})

	if len(a.Correlations) == 0 {
		t.Fatal("a BCD value was not found")
	}
	if got := a.Correlations[0].Encoding; !strings.Contains(got, "BCD") {
		t.Errorf("encoding = %q, want packed BCD", got)
	}
}

// An operator writes prose, and prose ends in a full stop.
func TestANoteEndingInAFullStopStillYieldsItsValue(t *testing.T) {
	got := numbersIn("fat reads 4.15.")
	if len(got) != 1 || got[0] != 4.15 {
		t.Fatalf("numbersIn = %v, want [4.15]", got)
	}
}

func TestConstantBytesAreSeparatedFromVaryingOnes(t *testing.T) {
	a := Analyse([]Event{
		bytesAt(0, "FAT 4.15\r\n"),
		bytesAt(10, "FAT 3.90\r\n"),
		bytesAt(20, "FAT 4.02\r\n"),
	}, AnalyseOptions{})

	var constant string
	for _, p := range a.Constant {
		constant += p.Printable
	}
	if !strings.HasPrefix(constant, "FAT ") {
		t.Errorf("the constant offsets read %q, want the header among them", constant)
	}
	if len(a.Varying) == 0 {
		t.Error("nothing was reported as varying, so the reading has nowhere to live")
	}
}

// A mark applies to what happens after it, and holds until the operator says
// something else — they annotate once and then do the thing.
func TestAMarkAppliesUntilTheNextOne(t *testing.T) {
	a := Analyse([]Event{
		markAt(0, "empty pan"),
		bytesAt(10, "W  0.00\r\n"),
		markAt(20, "known weight 5.00 L"),
		bytesAt(30, "W  5.00\r\n"),
		bytesAt(40, "W  5.00\r\n"),
	}, AnalyseOptions{})

	if len(a.Frames) != 3 {
		t.Fatalf("%d frames, want 3", len(a.Frames))
	}
	if a.Frames[0].Mark != "empty pan" {
		t.Errorf("first frame mark = %q", a.Frames[0].Mark)
	}
	if a.Frames[2].Mark != "known weight 5.00 L" {
		t.Errorf("third frame mark = %q, want the mark to carry forward", a.Frames[2].Mark)
	}
}

func TestARecordingWithNoBytesSaysSo(t *testing.T) {
	a := Analyse([]Event{{At: t0, Note: "capture started"}}, AnalyseOptions{})
	if len(a.Notes) == 0 || !strings.Contains(a.Notes[0], "no bytes") {
		t.Errorf("notes = %v", a.Notes)
	}
}

func TestUnmarkedFramesAreNotCorrelated(t *testing.T) {
	a := Analyse([]Event{
		bytesAt(0, "W  5.00\r\n"),
		bytesAt(10, "W  5.00\r\n"),
		bytesAt(20, "W  5.00\r\n"),
	}, AnalyseOptions{})
	if len(a.Correlations) != 0 {
		t.Errorf("correlations without any operator note: %+v", a.Correlations)
	}
	var told bool
	for _, n := range a.Notes {
		if strings.Contains(n, "mark a few frames") {
			told = true
		}
	}
	if !told {
		t.Errorf("the operator was not told why nothing was found: %v", a.Notes)
	}
}

// Weigh 5 kg and then 12.5 kg on the same instrument and the raw findings read
// like two fields at two offsets. It is one right-aligned field, and saying so is
// the difference between a parser that works and one that fails the first time a
// reading reaches double figures.
func TestARightAlignedFieldIsReportedAsOneField(t *testing.T) {
	a := Analyse([]Event{
		markAt(0, "known weight 5.00 L"),
		bytesAt(10, "ST,GS,+  5.00 L\r\n"),
		bytesAt(20, "ST,GS,+  5.00 L\r\n"),
		bytesAt(30, "ST,GS,+  5.00 L\r\n"),
		markAt(40, "known weight 12.50 L"),
		bytesAt(50, "ST,GS,+ 12.50 L\r\n"),
		bytesAt(60, "ST,GS,+ 12.50 L\r\n"),
		bytesAt(70, "ST,GS,+ 12.50 L\r\n"),
	}, AnalyseOptions{})

	var said string
	for _, n := range a.Notes {
		if strings.Contains(n, "right-aligned") {
			said = n
		}
	}
	if said == "" {
		t.Fatalf("two offsets ending at the same place were not recognised as one field: %v", a.Notes)
	}
	if !strings.Contains(said, "8 and 9") || !strings.Contains(said, "offsets 8–12") {
		t.Errorf("the note does not name the field correctly: %s", said)
	}
}

// A single mark cannot show alignment — one reading at one offset is just a
// field. Claiming right-alignment from it would be invention.
func TestOneMarkDoesNotClaimAlignment(t *testing.T) {
	a := Analyse([]Event{
		markAt(0, "known weight 5.00 L"),
		bytesAt(10, "ST,GS,+  5.00 L\r\n"),
		bytesAt(20, "ST,GS,+  5.00 L\r\n"),
		bytesAt(30, "ST,GS,+  5.00 L\r\n"),
	}, AnalyseOptions{})
	for _, n := range a.Notes {
		if strings.Contains(n, "right-aligned") {
			t.Errorf("alignment claimed from a single mark: %s", n)
		}
	}
}
