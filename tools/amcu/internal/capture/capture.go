// Package capture records what an instrument actually sends, byte for byte,
// without interpreting any of it.
//
// The specification asks for "a representative analyser/controller interface
// captured and mapped". Capturing has to come first and has to be dumb: at the
// moment somebody stands at a bench with a milk analyser, nobody knows the
// protocol. Anything that parses while it records will drop the bytes it did not
// expect, and those are exactly the ones worth having.
//
// So nothing here decodes. It writes down what arrived and when, and lets an
// operator annotate what was happening at the time — the weight on the pan, the
// fat reading on the display. Mapping happens afterwards, from the recording,
// against those annotations.
package capture

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Event is one thing that happened, in the order it happened.
type Event struct {
	At time.Time `json:"t"`
	// Bytes is what arrived, hex-encoded. Hex rather than base64 because a person
	// reading the file with their eyes is a supported use.
	Bytes string `json:"hex,omitempty"`
	Len   int    `json:"len,omitempty"`
	// Mark is what the operator said was happening. This is the whole value of
	// the recording: bytes with nothing known alongside them are a puzzle, bytes
	// recorded next to "known weight 5.00 L" are a Rosetta stone.
	Mark string `json:"mark,omitempty"`
	// Note records something about the capture itself rather than the instrument.
	Note string `json:"note,omitempty"`
}

// Decoded returns the raw bytes of an event.
func (e Event) Decoded() []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(e.Bytes, " ", ""))
	if err != nil {
		return nil
	}
	return b
}

// Recorder writes events to a capture file as they happen.
//
// Every event is flushed immediately. A bench session ends when somebody
// unplugs something, and a buffered recording that loses its last minute loses
// the part where the interesting thing happened.
type Recorder struct {
	mu  sync.Mutex
	w   *bufio.Writer
	enc *json.Encoder
	// Now is overridable so a test can record a known sequence.
	Now func() time.Time
}

func NewRecorder(w io.Writer) *Recorder {
	bw := bufio.NewWriter(w)
	return &Recorder{w: bw, enc: json.NewEncoder(bw), Now: time.Now}
}

func (r *Recorder) write(e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enc.Encode(e); err != nil {
		return err
	}
	return r.w.Flush()
}

// Bytes records what arrived.
func (r *Recorder) Bytes(b []byte) error {
	return r.write(Event{At: r.Now().UTC(), Bytes: hex.EncodeToString(b), Len: len(b)})
}

// Mark records what the operator says is happening right now.
func (r *Recorder) Mark(what string) error {
	return r.write(Event{At: r.Now().UTC(), Mark: what})
}

// Note records something about the capture rather than the instrument.
func (r *Recorder) Note(what string) error {
	return r.write(Event{At: r.Now().UTC(), Note: what})
}

// Read copies from an instrument into the recorder until the source ends,
// calling onChunk with each chunk so a live view can show the operator that
// something is arriving.
//
// Reads are not buffered into lines or frames. Where the chunk boundaries fall
// is itself evidence — an instrument that sends one reading per write tells you
// its framing for free — so they are recorded as they came.
func Read(src io.Reader, rec *Recorder, onChunk func([]byte)) error {
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if werr := rec.Bytes(chunk); werr != nil {
				return fmt.Errorf("record: %w", werr)
			}
			if onChunk != nil {
				onChunk(chunk)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// LoadEvents reads a capture back.
func LoadEvents(r io.Reader) ([]Event, error) {
	var out []Event
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for s.Scan() {
		line++
		text := strings.TrimSpace(s.Text())
		if text == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, e)
	}
	return out, s.Err()
}

// Dump renders bytes the way somebody reverse-engineering a protocol wants to
// see them: offsets, hex, and the printable characters side by side. Most
// instrument protocols turn out to be ASCII with a terminator, and that is
// obvious at a glance in this view and invisible in any other.
func Dump(b []byte) string {
	var out strings.Builder
	for i := 0; i < len(b); i += 16 {
		end := i + 16
		if end > len(b) {
			end = len(b)
		}
		row := b[i:end]

		fmt.Fprintf(&out, "  %04x  ", i)
		for j := 0; j < 16; j++ {
			if j < len(row) {
				fmt.Fprintf(&out, "%02x ", row[j])
			} else {
				out.WriteString("   ")
			}
			if j == 7 {
				out.WriteByte(' ')
			}
		}
		out.WriteString(" |")
		for _, c := range row {
			if c >= 0x20 && c < 0x7f {
				out.WriteByte(c)
			} else {
				out.WriteByte('.')
			}
		}
		out.WriteString("|\n")
	}
	return out.String()
}
