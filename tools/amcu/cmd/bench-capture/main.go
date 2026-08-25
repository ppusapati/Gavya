// bench-capture records what an analyser or controller actually sends, and
// afterwards helps work out what it meant.
//
// The specification asks for "a representative analyser/controller interface
// captured and mapped". Capturing has to come first and has to be dumb: at the
// bench, nobody knows the protocol yet, and anything that parses while it
// records drops the bytes it did not expect — which are the interesting ones.
//
//	# record from a serial analyser (configure the port first, see below)
//	stty -F /dev/ttyUSB0 9600 cs8 -cstopb -parity raw -echo
//	bench-capture --from /dev/ttyUSB0 --out bench.jsonl
//
//	# record from an instrument that pushes over TCP
//	bench-capture --listen :9100 --out bench.jsonl
//
//	# record from one that waits to be connected to
//	bench-capture --dial 192.168.1.50:4001 --out bench.jsonl
//
//	# afterwards
//	bench-capture --analyse bench.jsonl
//
// While recording, type what is happening and press enter. Those notes are the
// whole value of the recording: bytes with nothing known beside them are a
// puzzle; bytes recorded next to "known weight 5.00 L" are a Rosetta stone.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ppusapati/gavya/tools/amcu/internal/capture"
)

func main() {
	from := flag.String("from", "", "read from a device or file, e.g. /dev/ttyUSB0 (use - for stdin)")
	listen := flag.String("listen", "", "accept one TCP connection on this address, e.g. :9100")
	dial := flag.String("dial", "", "connect out to an instrument, e.g. 192.168.1.50:4001")
	out := flag.String("out", "", "write the recording here")
	analyse := flag.String("analyse", "", "analyse a recording instead of making one")
	gap := flag.Duration("idle-gap", 50*time.Millisecond, "silence taken to end a message when there is no terminator")
	flag.Parse()

	if *analyse != "" {
		runAnalyse(*analyse, *gap)
		return
	}

	sources := 0
	for _, s := range []string{*from, *listen, *dial} {
		if s != "" {
			sources++
		}
	}
	if sources != 1 || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: bench-capture --out FILE (--from DEV | --listen ADDR | --dial ADDR)")
		fmt.Fprintln(os.Stderr, "       bench-capture --analyse FILE")
		flag.PrintDefaults()
		os.Exit(2)
	}
	runCapture(*from, *listen, *dial, *out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "bench-capture:", err)
	os.Exit(2)
}

func runCapture(from, listen, dial, out string) {
	f, err := os.Create(out)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	rec := capture.NewRecorder(f)

	src, what, err := open(from, listen, dial, rec)
	if err != nil {
		fail(err)
	}
	defer src.Close()

	_ = rec.Note("capture started from " + what)
	fmt.Printf("Recording from %s into %s.\n", what, out)

	// When the instrument is on standard input there is nothing left to type
	// notes into, and a reader started here would race the capture for the same
	// bytes and win — the recording would come back empty while the tool
	// reported success.
	if notesFromStdin := from != "-"; notesFromStdin {
		fmt.Println("Type what is happening and press enter — \"known weight 5.00 L\", \"fat reads 4.15\".")
		fmt.Println("Those notes are what lets the analysis find the fields. Ctrl-C to stop.")
		go func() {
			s := bufio.NewScanner(os.Stdin)
			for s.Scan() {
				text := strings.TrimSpace(s.Text())
				if text == "" {
					continue
				}
				if err := rec.Mark(text); err != nil {
					fmt.Fprintln(os.Stderr, "could not record the note:", err)
					continue
				}
				fmt.Printf("  ← noted: %s\n", text)
			}
		}()
	} else {
		fmt.Println("Standard input is the instrument, so there is nowhere to type notes.")
		fmt.Println("Without notes the analysis can map the frames but cannot name any field;")
		fmt.Println("to mark readings, feed the instrument in on --from DEVICE instead.")
	}
	fmt.Println()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan error, 1)

	var total int
	go func() {
		done <- capture.Read(src, rec, func(b []byte) {
			total += len(b)
			fmt.Printf("%s  %d bytes\n", time.Now().Format("15:04:05.000"), len(b))
			fmt.Print(capture.Dump(b))
		})
	}()

	select {
	case <-stop:
		fmt.Printf("\nStopped. %d bytes recorded in %s.\n", total, out)
	case err := <-done:
		if err != nil {
			fmt.Fprintln(os.Stderr, "the instrument stopped sending:", err)
		}
		fmt.Printf("\nThe source ended. %d bytes recorded in %s.\n", total, out)
	}
	_ = rec.Note("capture ended")
	fmt.Printf("Now run: bench-capture --analyse %s\n", out)
}

type closer interface {
	Read(p []byte) (int, error)
	Close() error
}

func open(from, listen, dial string, rec *capture.Recorder) (closer, string, error) {
	switch {
	case from == "-":
		return os.Stdin, "standard input", nil

	case from != "":
		// A serial port is read as a file. The port is configured with stty
		// rather than in here, which keeps this tool free of a serial library
		// and its cgo, and means a bench laptop needs nothing installed.
		f, err := os.Open(from)
		if err != nil {
			return nil, "", fmt.Errorf("open %s: %w (configure the port first with stty)", from, err)
		}
		return f, from, nil

	case listen != "":
		ln, err := net.Listen("tcp", listen)
		if err != nil {
			return nil, "", err
		}
		fmt.Printf("Waiting for the instrument to connect on %s…\n", listen)
		conn, err := ln.Accept()
		if err != nil {
			return nil, "", err
		}
		_ = ln.Close()
		_ = rec.Note("connection from " + conn.RemoteAddr().String())
		return conn, conn.RemoteAddr().String(), nil

	default:
		conn, err := net.DialTimeout("tcp", dial, 10*time.Second)
		if err != nil {
			return nil, "", err
		}
		return conn, dial, nil
	}
}

func runAnalyse(path string, gap time.Duration) {
	f, err := os.Open(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()

	events, err := capture.LoadEvents(f)
	if err != nil {
		fail(err)
	}
	a := capture.Analyse(events, capture.AnalyseOptions{IdleGap: gap})

	rule("How the messages were separated")
	fmt.Printf("  %s\n", a.Framing.Reason)
	fmt.Printf("  %d frames\n", len(a.Frames))
	if len(a.Lengths) > 0 {
		var ls []int
		for l := range a.Lengths {
			ls = append(ls, l)
		}
		sort.Ints(ls)
		var parts []string
		for _, l := range ls {
			parts = append(parts, fmt.Sprintf("%d bytes × %d", l, a.Lengths[l]))
		}
		fmt.Printf("  lengths: %s\n", strings.Join(parts, ", "))
	}

	if len(a.Frames) > 0 {
		rule("The first few messages")
		for i, f := range a.Frames {
			if i >= 3 {
				break
			}
			if f.Mark != "" {
				fmt.Printf("\n  while: %s\n", f.Mark)
			} else {
				fmt.Println()
			}
			fmt.Print(capture.Dump(f.Bytes))
		}
	}

	rule("What never changes, and what does")
	fmt.Printf("  constant offsets: %s\n", offsets(a.Constant))
	if s := printableOf(a.Constant); s != "" {
		fmt.Printf("  they read as:     %q\n", s)
		fmt.Println("  a constant run of text is usually a header, an address or a unit marker")
	}
	fmt.Printf("  varying offsets:  %s\n", offsets(a.Varying))
	fmt.Println("  the reading lives among the varying ones")

	rule("Where the values you noted appear")
	if len(a.Correlations) == 0 {
		fmt.Println("  Nothing matched. Either no note contained a number, or the value is encoded in a")
		fmt.Println("  way this does not search for — a checksum-protected field, a different scaling,")
		fmt.Println("  or text in a codepage. The frames above are still the evidence; read them.")
	}
	for _, c := range a.Correlations {
		fmt.Printf("\n  %g  from note %q\n", c.Value, c.Mark)
		fmt.Printf("      at offset %d, %d bytes, as %s\n", c.Offset, c.Length, c.Encoding)
		fmt.Printf("      bytes there: %s\n", c.Sample)
	}

	if len(a.Notes) > 0 {
		rule("Notes")
		for _, n := range a.Notes {
			fmt.Printf("  %s\n", n)
		}
	}
}

func rule(title string) {
	fmt.Printf("\n%s\n%s\n", title, strings.Repeat("─", len([]rune(title))))
}

func offsets(ps []capture.Position) string {
	if len(ps) == 0 {
		return "none"
	}
	var out []string
	start, prev := ps[0].Offset, ps[0].Offset
	for _, p := range ps[1:] {
		if p.Offset == prev+1 {
			prev = p.Offset
			continue
		}
		out = append(out, span(start, prev))
		start, prev = p.Offset, p.Offset
	}
	out = append(out, span(start, prev))
	return strings.Join(out, ", ")
}

func span(a, b int) string {
	if a == b {
		return fmt.Sprintf("%d", a)
	}
	return fmt.Sprintf("%d–%d", a, b)
}

func printableOf(ps []capture.Position) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.Printable)
	}
	s := b.String()
	if strings.Trim(s, ".") == "" {
		return ""
	}
	return s
}
