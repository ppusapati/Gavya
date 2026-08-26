// amcu-import loads a society's collection export into the platform.
//
// It is the other half of amcu-profile. That reads a real export and produces a
// profile describing it; this reads the same export through that profile and
// delivers the collections. Between them, supporting a new AMCU vendor is
// writing a small file rather than writing a parser.
//
//	# see what would be loaded, without loading it
//	amcu-import --profile smartdairy.json collections.csv
//
//	# load it
//	amcu-import --profile smartdairy.json \
//	            --into http://gateway:8000 \
//	            --tenant T_01HZ... --device D_01HZ... \
//	            collections.csv
//
// A dry run is the default. Loading somebody's milk into a ledger is not
// something to do because a flag was forgotten.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ppusapati/gavya/tools/amcu/internal/importer"
	"github.com/ppusapati/gavya/tools/amcu/internal/mapping"
	"github.com/ppusapati/gavya/tools/amcu/internal/pathology"
	"github.com/ppusapati/gavya/tools/amcu/internal/source"
)

func main() {
	profilePath := flag.String("profile", "", "the vendor profile to read the file through")
	into := flag.String("into", "", "the gateway to deliver to; without this nothing is sent")
	tenant := flag.String("tenant", "", "the tenant these collections belong to")
	device := flag.String("device", "", "the registered device this import is attributed to")
	session := flag.String("session", "", "the token from a sign-in, so the gateway knows who is importing")
	generation := flag.Int64("generation", 1, "the device generation this import belongs to")
	batchSize := flag.Int("batch", 500, "how many records to deliver at a time")
	flag.Parse()

	if *profilePath == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: amcu-import --profile VENDOR.json [--into URL --tenant ID --device ID --session TOKEN] FILE")
		flag.PrintDefaults()
		os.Exit(2)
	}

	prof := loadProfile(*profilePath)
	table := loadFile(flag.Arg(0))

	result, err := importer.Read(prof, table)
	if err != nil {
		// A refusal is the point of this tool, so it is reported as a refusal
		// rather than as a crash: the message says what is wrong with the file
		// or the profile and what would settle it.
		fmt.Fprintf(os.Stderr, "\nNothing was imported.\n\n%v\n", err)
		os.Exit(1)
	}

	report(result, prof)

	if *into == "" {
		fmt.Printf("\nThis was a dry run. Nothing has been delivered.\n")
		fmt.Printf("To load it, add --into, --tenant, --device and --session.\n")
		return
	}
	switch {
	case *tenant == "", *device == "":
		fmt.Fprintln(os.Stderr, "\n--into needs --tenant and --device: a collection with no tenant "+
			"belongs to nobody, and one with no device cannot be traced to how it arrived.")
		os.Exit(2)
	}

	deliver(*into, *session, result, *tenant, *device, *generation, *batchSize)
}

func loadProfile(path string) *mapping.Profile {
	f, err := os.Open(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	p, err := mapping.Load(f)
	if err != nil {
		fail(fmt.Errorf("read the profile: %w", err))
	}
	return p
}

func loadFile(path string) *source.Table {
	f, err := os.Open(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	t, err := source.Read(f)
	if err != nil {
		fail(fmt.Errorf("read %s: %w", path, err))
	}
	return t
}

func report(r *importer.Result, p *mapping.Profile) {
	rule(fmt.Sprintf("Read through %s's profile", r.Vendor))
	fmt.Printf("  %d collections\n", len(r.Collections))
	fmt.Printf("  %d rows held back\n", len(r.Rejected))
	fmt.Printf("  dates read as %s, quantity in %s\n", p.Format.DateLayout, p.Format.QuantityUnit)

	if len(r.Answered) > 0 {
		rule("Questions the profile answered")
		for _, f := range r.Answered {
			fmt.Printf("  %s\n", f.Title)
		}
		fmt.Println("  These would have stopped an import with no profile. The profile settles them,")
		fmt.Println("  which is what it is for — but check they were settled the way this file needs.")
	}

	if len(r.Rejected) > 0 {
		rule("Rows held back")
		byReason := map[string][]importer.Rejection{}
		for _, rej := range r.Rejected {
			byReason[rej.Reason] = append(byReason[rej.Reason], rej)
		}
		reasons := make([]string, 0, len(byReason))
		for k := range byReason {
			reasons = append(reasons, k)
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			rows := byReason[reason]
			fmt.Printf("\n  %s — %d row(s)\n", reason, len(rows))
			for i, rej := range rows {
				if i >= 5 {
					fmt.Printf("      … and %d more\n", len(rows)-5)
					break
				}
				fmt.Printf("      line %d: %s\n", rej.Line, rej.Detail)
			}
		}
		fmt.Println("\n  These are held, not discarded. They are the rows somebody needs to look at.")
	}

	if len(r.HandledPerRow) > 0 {
		rule("Dealt with row by row")
		for _, f := range r.HandledPerRow {
			fmt.Printf("  %s — those rows are in the list above\n", f.Title)
		}
	}

	if len(r.Findings) > 0 {
		rule("What else is in this file")
		for _, f := range r.Findings {
			fmt.Printf("\n  [%s] %s\n", f.Severity, f.Title)
			for _, line := range wrap(f.Detail, 74) {
				fmt.Printf("      %s\n", line)
			}
		}
	}
	if len(r.Findings) == 0 && len(r.Rejected) == 0 && len(r.HandledPerRow) == 0 {
		rule("What else is in this file")
		fmt.Println("  Nothing. That is unusual for a real export — check that the profile's fields")
		fmt.Println("  are where this file actually holds them, because a check cannot fire on a")
		fmt.Println("  column it did not find.")
	}
}

func deliver(gateway, session string, r *importer.Result, tenant, device string, generation int64, batchSize int) {
	batch := importer.BatchID(r.Vendor, r.Collections)
	records := r.Records(tenant, device, batch, generation)

	rule("Delivering")
	fmt.Printf("  batch %s\n", batch)
	fmt.Printf("  %d records in groups of %d\n", len(records), batchSize)
	fmt.Println("  The batch is derived from the file's own content, so delivering this file again")
	fmt.Println("  is recognised as a replay rather than paid for twice.")
	fmt.Println()

	client := &http.Client{Timeout: 60 * time.Second}
	var accepted, replayed, quarantined int

	for start := 0; start < len(records); start += batchSize {
		end := start + batchSize
		if end > len(records) {
			end = len(records)
		}
		body, err := json.Marshal(map[string]any{"records": records[start:end]})
		if err != nil {
			fail(err)
		}
		req, err := http.NewRequest(http.MethodPost,
			strings.TrimRight(gateway, "/")+"/ingestion.v1.IngestionService/DeliverBatch",
			bytes.NewReader(body))
		if err != nil {
			fail(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if session != "" {
			req.Header.Set("Authorization", "Bearer "+session)
		}

		resp, err := client.Do(req)
		if err != nil {
			fail(fmt.Errorf("deliver records %d-%d: %w", start+1, end, err))
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fail(fmt.Errorf("deliver records %d-%d: the platform returned %d: %s",
				start+1, end, resp.StatusCode, strings.TrimSpace(string(payload))))
		}

		var out struct {
			Accepted    int `json:"accepted"`
			Replayed    int `json:"replayed"`
			Quarantined int `json:"quarantined"`
			Results     []struct {
				Outcome      string `json:"outcome"`
				Reason       string `json:"reason"`
				Detail       string `json:"detail"`
				QuarantineID string `json:"quarantine_id"`
			} `json:"results"`
		}
		if err := json.Unmarshal(payload, &out); err != nil {
			fail(fmt.Errorf("the platform replied with something unreadable: %w", err))
		}
		accepted += out.Accepted
		replayed += out.Replayed
		quarantined += out.Quarantined

		for i, res := range out.Results {
			if res.Outcome == "QUARANTINED" {
				fmt.Printf("  line %d quarantined (%s): %s\n",
					records[start+i].Sequence, res.Reason, res.Detail)
			}
		}
		fmt.Printf("  %d-%d: %d accepted, %d already present, %d quarantined\n",
			start+1, end, out.Accepted, out.Replayed, out.Quarantined)
	}

	rule("Done")
	fmt.Printf("  %d accepted\n", accepted)
	fmt.Printf("  %d already present — this file, or these rows, had been delivered before\n", replayed)
	fmt.Printf("  %d quarantined — held by the platform, not lost\n", quarantined)
	if quarantined > 0 {
		fmt.Println("\n  Quarantined records are visible through ingestion.v1.IngestionService/ListQuarantined.")
	}
}

func rule(title string) {
	fmt.Printf("\n%s\n%s\n", title, strings.Repeat("─", len([]rune(title))))
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

func fail(err error) {
	fmt.Fprintln(os.Stderr, "amcu-import:", err)
	os.Exit(1)
}

var _ = pathology.Blocking
