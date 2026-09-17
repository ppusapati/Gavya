// amcu-profile reads a real collection export and reports what is in it, what
// is wrong with it, and how it maps onto the platform.
//
// It exists to make the reality-acquisition phase productive. The specification
// asks for "at least one real AMCU/collection export mapped and schema
// pathologies documented"; this turns that from a fortnight of reading somebody
// else's CSV into an afternoon of checking a draft.
//
//	amcu-profile collections.csv
//	amcu-profile --vendor SmartDairy --write-profile smartdairy.json collections.csv
//	amcu-profile --check smartdairy.json collections.csv
//
// Nothing here writes to the platform. It reads a file and prints what it found.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ppusapati/gavya/tools/amcu/internal/mapping"
	"github.com/ppusapati/gavya/tools/amcu/internal/pathology"
	"github.com/ppusapati/gavya/tools/amcu/internal/profile"
	"github.com/ppusapati/gavya/tools/amcu/internal/source"
)

func main() {
	vendor := flag.String("vendor", "", "the system that wrote this export, as the society calls it")
	writeProfile := flag.String("write-profile", "", "write a draft vendor profile to this path")
	check := flag.String("check", "", "check the file against an existing vendor profile instead of guessing")
	quiet := flag.Bool("quiet", false, "print only findings, not the column profile")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: amcu-profile [flags] <export-file>")
		flag.PrintDefaults()
		os.Exit(2)
	}

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fail(err)
	}
	defer f.Close()

	table, err := source.Read(f)
	if err != nil {
		fail(err)
	}
	prof := profile.Profile(table)
	report := pathology.Inspect(table, prof)

	printShape(table.Shape)
	if !*quiet {
		printColumns(prof)
	}
	printFindings(report)

	if *check != "" {
		checkAgainst(*check, prof)
	}

	if *writeProfile != "" {
		name := *vendor
		if name == "" {
			name = "unnamed"
		}
		draft := mapping.Propose(name, table, prof)
		out, err := os.Create(*writeProfile)
		if err != nil {
			fail(err)
		}
		if err := draft.Write(out); err != nil {
			_ = out.Close()
			fail(err)
		}
		// Closed here rather than deferred, and the error is read. A buffered
		// write is not on disk until the close succeeds, so a deferred close
		// whose error is dropped is how a profile is reported as written and
		// is not there.
		if err := out.Close(); err != nil {
			fail(err)
		}
		fmt.Printf("\nWrote a draft profile to %s.\n", *writeProfile)
		if draft.IsDraft() {
			fmt.Printf("It is a draft: %d things still need a person's decision, listed under\n"+
				"\"unresolved\" in the file. The import will refuse it until they are settled.\n",
				len(draft.Unresolved))
		}
	}

	// A blocking finding means importing this file would produce wrong money.
	for _, fnd := range report.Findings {
		if fnd.Severity == pathology.Blocking {
			os.Exit(1)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "amcu-profile:", err)
	os.Exit(2)
}

func rule(title string) {
	fmt.Printf("\n%s\n%s\n", title, strings.Repeat("─", len([]rune(title))))
}

func printShape(s source.Shape) {
	rule("How the file was read")
	fmt.Printf("  encoding    %s\n", s.Encoding)
	if s.Delimiter != 0 {
		fmt.Printf("  delimiter   %q\n", string(s.Delimiter))
	} else {
		fmt.Printf("  layout      fixed width %v\n", s.FixedWidths)
	}
	fmt.Printf("  header      %v — %s\n", s.HasHeader, s.HeaderReason)
	fmt.Printf("  size        %d columns, %d rows\n", s.Columns, s.Rows)
	for _, n := range s.Notes {
		fmt.Printf("  note        %s\n", n)
	}
}

func printColumns(p *profile.Table) {
	rule("What each column holds")
	for _, c := range p.Columns {
		role := string(c.Role)
		if role == "" {
			role = "unidentified"
		}
		fmt.Printf("\n  [%d] %s\n", c.Index+1, display(c.Header))
		fmt.Printf("      %-12s %s", "kind", c.Kind)
		if c.Kind == profile.KindDecimal || c.Kind == profile.KindInteger {
			fmt.Printf("  range %.3f–%.3f  mean %.3f", c.Min, c.Max, c.Mean)
			if c.Decimals > 0 {
				fmt.Printf("  to %d places", c.Decimals)
			}
		}
		fmt.Println()
		fmt.Printf("      %-12s %d distinct, %d blank of %d\n", "coverage", c.Distinct, c.Blank, c.Rows)
		fmt.Printf("      %-12s %s", "reads as", role)
		if c.Role != profile.RoleUnknown {
			fmt.Printf("  (confidence %.2f)", c.RoleConfidence)
		}
		fmt.Println()
		if c.RoleReason != "" {
			fmt.Printf("      %-12s %s\n", "because", c.RoleReason)
		}
		if len(c.Samples) > 0 {
			fmt.Printf("      %-12s %s\n", "examples", strings.Join(c.Samples, ", "))
		}
	}
}

func display(h string) string {
	if strings.TrimSpace(h) == "" {
		return "(no header)"
	}
	return h
}

func printFindings(r *pathology.Report) {
	rule("What is wrong with it")
	if len(r.Findings) == 0 {
		fmt.Println("  Nothing found. That is unusual for a real export — check that the columns above")
		fmt.Println("  were identified correctly, because a check cannot fire on a field it did not find.")
		return
	}
	for _, f := range r.Findings {
		fmt.Printf("\n  %s  %s\n", badge(f.Severity), f.Title)
		for _, line := range wrap(f.Detail, 76) {
			fmt.Printf("      %s\n", line)
		}
		if len(f.Examples) > 0 {
			fmt.Printf("      evidence:\n")
			for _, e := range f.Examples {
				fmt.Printf("        %s\n", e)
			}
		}
		if len(f.Rows) > 0 {
			more := ""
			if f.Count > len(f.Rows) {
				more = fmt.Sprintf(" (and %d more)", f.Count-len(f.Rows))
			}
			fmt.Printf("      rows: %v%s\n", f.Rows, more)
		}
	}

	var blocking, serious int
	for _, f := range r.Findings {
		switch f.Severity {
		case pathology.Blocking:
			blocking++
		case pathology.Serious:
			serious++
		}
	}
	fmt.Println()
	if blocking > 0 {
		fmt.Printf("  %d blocking: importing this file as it stands would produce wrong money.\n", blocking)
	}
	if serious > 0 {
		fmt.Printf("  %d serious: the import would work but something is not what it seems.\n", serious)
	}
}

func badge(s pathology.Severity) string {
	switch s {
	case pathology.Blocking:
		return "BLOCKING"
	case pathology.Serious:
		return "SERIOUS "
	default:
		return "NOTE    "
	}
}

func checkAgainst(path string, p *profile.Table) {
	rule("Against the saved profile")
	f, err := os.Open(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()

	saved, err := mapping.Load(f)
	if err != nil {
		fail(err)
	}
	if err := saved.Validate(); err != nil {
		fmt.Printf("  The profile is not usable yet: %v\n", err)
		return
	}

	found := map[string]int{}
	for _, c := range p.Columns {
		if c.Role != profile.RoleUnknown {
			found[string(c.Role)] = c.Index
		}
	}
	agreed := 0
	for _, fld := range saved.Fields {
		at, ok := found[fld.Role]
		switch {
		case !ok:
			fmt.Printf("  %-16s the profile expects column %d but this file does not look like it has one\n",
				fld.Role, fld.Column+1)
		case at != fld.Column:
			fmt.Printf("  %-16s the profile says column %d, this file reads it at column %d — the export "+
				"format has changed\n", fld.Role, fld.Column+1, at+1)
		default:
			agreed++
		}
	}
	fmt.Printf("\n  %d of %d mapped fields are where %s said they would be.\n",
		agreed, len(saved.Fields), saved.Vendor)
}

func wrap(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur += " " + w
	}
	return append(lines, cur)
}
