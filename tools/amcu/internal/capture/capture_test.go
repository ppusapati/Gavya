package capture

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// A recorder that writes on a schedule, so a test can assert about timing
// without waiting for it.
func recorderAt(w io.Writer, start time.Time, step time.Duration) *Recorder {
	r := NewRecorder(w)
	now := start
	r.Now = func() time.Time {
		t := now
		now = now.Add(step)
		return t
	}
	return r
}

// Every event must reach the file as it happens. A bench session ends when
// somebody unplugs something, and a buffered recording that loses its last
// minute loses the part where the interesting thing happened.
func TestEachEventIsOnDiskBeforeTheNextOne(t *testing.T) {
	var buf bytes.Buffer
	rec := recorderAt(&buf, time.Unix(0, 0).UTC(), time.Millisecond)

	if err := rec.Mark("known weight 5.00 L"); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("the mark was still in a buffer after it was recorded")
	}
	before := buf.Len()

	if err := rec.Bytes([]byte{0x01, 0x02}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() <= before {
		t.Fatal("the bytes were still in a buffer after they were recorded")
	}
}

func TestARecordingReadsBackAsWhatWasWritten(t *testing.T) {
	var buf bytes.Buffer
	rec := recorderAt(&buf, time.Unix(0, 0).UTC(), 10*time.Millisecond)
	_ = rec.Mark("known weight 5.00 L")
	_ = rec.Bytes([]byte("W  5.00 L\r\n"))
	_ = rec.Note("capture ended")

	events, err := LoadEvents(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("read back %d events, want 3", len(events))
	}
	if events[0].Mark != "known weight 5.00 L" {
		t.Errorf("mark = %q", events[0].Mark)
	}
	if got := string(events[1].Decoded()); got != "W  5.00 L\r\n" {
		t.Errorf("bytes = %q, want them back byte for byte", got)
	}
	if events[1].Len != 11 {
		t.Errorf("len = %d, want 11", events[1].Len)
	}
	if events[2].Note != "capture ended" {
		t.Errorf("note = %q", events[2].Note)
	}
}

// Where the writes fell is evidence — an instrument that sends one message per
// write has told you its framing for free — so Read must not join them up.
func TestReadKeepsTheChunkBoundariesTheInstrumentUsed(t *testing.T) {
	var buf bytes.Buffer
	rec := recorderAt(&buf, time.Unix(0, 0).UTC(), time.Millisecond)

	src := &scriptedReader{chunks: [][]byte{[]byte("AAA"), []byte("BB"), []byte("CCCC")}}
	if err := Read(src, rec, nil); err != nil {
		t.Fatal(err)
	}

	events, err := LoadEvents(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var lens []int
	for _, e := range events {
		lens = append(lens, e.Len)
	}
	if len(lens) != 3 || lens[0] != 3 || lens[1] != 2 || lens[2] != 4 {
		t.Errorf("chunk lengths = %v, want [3 2 4] — the boundaries were not preserved", lens)
	}
}

// Bytes that arrived alongside the error still have to be kept: an instrument
// that says something and then drops the link has said the interesting part.
func TestBytesArrivingWithAnErrorAreStillRecorded(t *testing.T) {
	var buf bytes.Buffer
	rec := recorderAt(&buf, time.Unix(0, 0).UTC(), time.Millisecond)

	src := &scriptedReader{chunks: [][]byte{[]byte("LAST")}, err: errors.New("device disconnected")}
	err := Read(src, rec, nil)
	if err == nil {
		t.Fatal("the disconnection was not reported")
	}
	events, _ := LoadEvents(&buf)
	if len(events) != 1 || string(events[0].Decoded()) != "LAST" {
		t.Errorf("events = %v, want the final bytes kept", events)
	}
}

func TestDumpShowsOffsetsHexAndText(t *testing.T) {
	got := Dump([]byte("W 5.00\r\n"))
	if !strings.Contains(got, "0000") {
		t.Error("no offset")
	}
	if !strings.Contains(got, "57 20 35") {
		t.Errorf("no hex:\n%s", got)
	}
	if !strings.Contains(got, "|W 5.00..|") {
		t.Errorf("the printable column is wrong — this is the view that makes an ASCII protocol obvious:\n%s", got)
	}
}

type scriptedReader struct {
	chunks [][]byte
	err    error
	i      int
}

func (s *scriptedReader) Read(p []byte) (int, error) {
	if s.i >= len(s.chunks) {
		if s.err != nil {
			return 0, s.err
		}
		return 0, io.EOF
	}
	n := copy(p, s.chunks[s.i])
	s.i++
	if s.i >= len(s.chunks) && s.err != nil {
		return n, s.err
	}
	return n, nil
}

func (s *scriptedReader) Close() error { return nil }
