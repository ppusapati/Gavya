//go:build e2e

// Pricing a society's own milk, end to end.
//
// The chart in these tests is the shape a village society has on the wall: fat
// down the side, SNF across, a rate per litre where they meet. The figures are
// round so the arithmetic can be checked by hand, because a test whose expected
// values came out of the code it is testing proves only that the code agrees
// with itself.
//
//	         SNF 8.5   SNF 8.6
//	fat 4.0    40.00     41.00
//	fat 4.1    42.00     43.00
//	fat 4.2    44.00     45.00
package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const procurementSvc = "procurement.v1.ProcurementService"

type pointProto struct {
	Value string `json:"value"`
	Scale int32  `json:"scale"`
}

type cellProto struct {
	Fat       pointProto `json:"fat"`
	SNF       pointProto `json:"snf"`
	Rate      string     `json:"rate"`
	RateScale int32      `json:"rate_scale"`
}

type declareCardReq struct {
	TenantID      string      `json:"tenant_id"`
	Name          string      `json:"name"`
	Kind          string      `json:"kind"`
	Currency      string      `json:"currency"`
	AmountScale   int32       `json:"amount_scale"`
	Basis         string      `json:"basis,omitempty"`
	BetweenPoints string      `json:"between_points,omitempty"`
	OutsideChart  string      `json:"outside_chart,omitempty"`
	Rounding      string      `json:"rounding"`
	Cells         []cellProto `json:"cells,omitempty"`
	ValidFrom     string      `json:"valid_from"`
	ValidTo       string      `json:"valid_to,omitempty"`
	Actor         string      `json:"actor"`
}

type rateCardProto struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Kind          string      `json:"kind"`
	Basis         string      `json:"basis,omitempty"`
	BetweenPoints string      `json:"between_points,omitempty"`
	OutsideChart  string      `json:"outside_chart,omitempty"`
	Rounding      string      `json:"rounding"`
	Cells         []cellProto `json:"cells,omitempty"`
	ValidFrom     string      `json:"valid_from"`
	ValidTo       string      `json:"valid_to,omitempty"`
}

type declareCardResp struct {
	RateCard *rateCardProto `json:"rate_card"`
}

type recordCollectionReq struct {
	TenantID     string     `json:"tenant_id"`
	ProducerRef  string     `json:"producer_ref"`
	SocietyCode  string     `json:"society_code,omitempty"`
	CollectedOn  string     `json:"collected_on"`
	Shift        string     `json:"shift"`
	Quantity     pointProto `json:"quantity"`
	QuantityUnit string     `json:"quantity_unit"`
	Fat          pointProto `json:"fat,omitempty"`
	SNF          pointProto `json:"snf,omitempty"`
	// Where an imported delivery came from. Left empty for milk the society
	// recorded itself, which is what every test before the payment explanation
	// did; set, it is what lets an explanation ask canonical what the member
	// number meant.
	OriginKind     string `json:"origin_kind,omitempty"`
	SourceSystemID string `json:"source_system_id,omitempty"`
	ImportBatchID  string `json:"import_batch_id,omitempty"`
	SourceRecordID string `json:"source_record_id,omitempty"`
	Actor          string `json:"actor"`
}

type pricedCollectionProto struct {
	ID               string `json:"id"`
	ProducerRef      string `json:"producer_ref"`
	CollectedOn      string `json:"collected_on"`
	Shift            string `json:"shift"`
	RateCardID       string `json:"rate_card_id"`
	Rate             string `json:"rate,omitempty"`
	Currency         string `json:"currency"`
	Amount           string `json:"amount"`
	AmountMinorUnits int64  `json:"amount_minor_units"`
	Explanation      string `json:"explanation"`
	OriginKind       string `json:"origin_kind"`

	SupersededAt     string `json:"superseded_at,omitempty"`
	SupersededBy     string `json:"superseded_by,omitempty"`
	Supersedes       string `json:"supersedes,omitempty"`
	CorrectionReason string `json:"correction_reason,omitempty"`
}

type recordCollectionResp struct {
	Collection *pricedCollectionProto `json:"collection"`
}

type listCollectionsReq struct {
	TenantID    string `json:"tenant_id"`
	ProducerRef string `json:"producer_ref,omitempty"`
	From        string `json:"from"`
	To          string `json:"to,omitempty"`
}

type listCollectionsResp struct {
	Collections     []*pricedCollectionProto `json:"collections"`
	Total           string                   `json:"total"`
	TotalMinorUnits int64                    `json:"total_minor_units"`
	Currency        string                   `json:"currency"`
}

// societyChart is the chart in the file header, written out rather than
// computed. A rate is a declaration and writing one as arithmetic on another
// invites exactly the floating-point sloppiness this whole package exists to
// avoid.
func societyChart() []cellProto {
	return chartOf([3][2]string{
		{"40.0000", "41.0000"}, // fat 4.0 at SNF 8.5 and 8.6
		{"42.0000", "43.0000"}, // fat 4.1
		{"44.0000", "45.0000"}, // fat 4.2
	})
}

// dearerChart is the same shape two rupees higher everywhere, which is what a
// society does when the season turns.
func dearerChart() []cellProto {
	return chartOf([3][2]string{
		{"42.0000", "43.0000"},
		{"44.0000", "45.0000"},
		{"46.0000", "47.0000"},
	})
}

func chartOf(rates [3][2]string) []cellProto {
	fats := []string{"4.0", "4.1", "4.2"}
	snfs := []string{"8.5", "8.6"}
	var cells []cellProto
	for i, fat := range fats {
		for j, snf := range snfs {
			cells = append(cells, cellProto{
				Fat:  pointProto{Value: fat, Scale: 1},
				SNF:  pointProto{Value: snf, Scale: 1},
				Rate: rates[i][j], RateScale: 4,
			})
		}
	}
	return cells
}

// declareChart puts a card on the wall for a period.
func declareChart(t *testing.T, p *platform, name, from, to string, mods ...func(*declareCardReq)) *rateCardProto {
	t.Helper()
	req := declareCardReq{
		TenantID: p.tenant, Name: name, Kind: "CHART",
		Currency: "INR", AmountScale: 2,
		Basis: "PER_LITRE", BetweenPoints: "BAND", OutsideChart: "REFUSE",
		Rounding: "HALF_UP", Cells: societyChart(),
		ValidFrom: from, ValidTo: to, Actor: "e2e",
	}
	for _, m := range mods {
		m(&req)
	}
	resp, err := svcclient.Call[declareCardReq, declareCardResp](
		context.Background(), p.procurement(), procurementSvc+"/DeclareRateCard", req, p.opts())
	if err != nil {
		t.Fatalf("DeclareRateCard %s: %v", name, err)
	}
	return resp.RateCard
}

func collect(t *testing.T, p *platform, in recordCollectionReq) (*pricedCollectionProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e"
	}
	resp, err := svcclient.Call[recordCollectionReq, recordCollectionResp](
		context.Background(), p.procurement(), procurementSvc+"/RecordCollection", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Collection, nil
}

func morning(producer, on, qty, fat, snf string) recordCollectionReq {
	return recordCollectionReq{
		ProducerRef: producer, CollectedOn: on, Shift: "MORNING",
		Quantity: pointProto{Value: qty, Scale: 3}, QuantityUnit: "PER_LITRE",
		Fat: pointProto{Value: fat, Scale: 1}, SNF: pointProto{Value: snf, Scale: 1},
	}
}

// 12.500 litres at fat 4.1 and SNF 8.6 reads 43.0000 a litre, so 537.50.
func TestACollectionIsPricedFromTheSocietysChart(t *testing.T) {
	p := startPlatform(t)
	card := declareChart(t, p, "February", "2026-02-01", "2026-03-01")

	got, err := collect(t, p, morning("producer:101", "2026-02-10", "12.500", "4.1", "8.6"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount != "537.50" {
		t.Errorf("amount = %s, want 537.50", got.Amount)
	}
	if got.AmountMinorUnits != 53750 {
		t.Errorf("minor units = %d, want 53750", got.AmountMinorUnits)
	}
	if got.Rate != "43.0000" {
		t.Errorf("rate = %s, want 43.0000", got.Rate)
	}
	if got.RateCardID != card.ID {
		t.Errorf("priced from card %s, want %s", got.RateCardID, card.ID)
	}
	if got.Explanation == "" {
		t.Error("no explanation stored; a producer disputing this is owed the workings")
	}
	if got.OriginKind != "NATIVE" {
		t.Errorf("origin = %s, want NATIVE", got.OriginKind)
	}
}

// The card is chosen by when the milk was collected, not by when it is entered.
// A society catching up on a week of paper slips after a rate change must price
// each day at what it was worth that day.
func TestMilkIsPricedAtWhatItWasWorthOnTheDayItWasCollected(t *testing.T) {
	p := startPlatform(t)
	feb := declareChart(t, p, "February", "2026-02-01", "2026-03-01")

	// March's chart pays two rupees more everywhere.
	march := declareChart(t, p, "March", "2026-03-01", "", func(r *declareCardReq) {
		r.Cells = dearerChart()
	})

	febMilk, err := collect(t, p, morning("producer:101", "2026-02-10", "10.000", "4.1", "8.6"))
	if err != nil {
		t.Fatal(err)
	}
	marchMilk, err := collect(t, p, morning("producer:101", "2026-03-10", "10.000", "4.1", "8.6"))
	if err != nil {
		t.Fatal(err)
	}

	if febMilk.RateCardID != feb.ID {
		t.Errorf("February's milk was priced from card %s, want February's", febMilk.RateCardID)
	}
	if marchMilk.RateCardID != march.ID {
		t.Errorf("March's milk was priced from card %s, want March's", marchMilk.RateCardID)
	}
	if febMilk.Amount != "430.00" {
		t.Errorf("February's ten litres came to %s, want 430.00", febMilk.Amount)
	}
	if marchMilk.Amount != "450.00" {
		t.Errorf("March's ten litres came to %s, want 450.00", marchMilk.Amount)
	}
}

// Two cards in force at once is not a conflict to resolve when pricing, it is a
// conflict to prevent. Resolved later the choice is invisible, and a society
// finds out when a producer compares two statements.
func TestTwoCardsCannotBeInForceAtOnce(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "February", "2026-02-01", "2026-03-01")

	_, err := svcclient.Call[declareCardReq, declareCardResp](
		context.Background(), p.procurement(), procurementSvc+"/DeclareRateCard",
		declareCardReq{
			TenantID: p.tenant, Name: "Also February", Kind: "CHART",
			Currency: "INR", AmountScale: 2, Basis: "PER_LITRE",
			BetweenPoints: "BAND", OutsideChart: "REFUSE", Rounding: "HALF_UP",
			Cells: societyChart(), ValidFrom: "2026-02-15", ValidTo: "2026-03-15", Actor: "e2e",
		}, p.opts())
	if err == nil {
		t.Fatal("a second card overlapping February was accepted")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "already in force") {
		t.Errorf("refused with %v, want it to say another card is in force", err)
	}
}

// And a card starting exactly where the last one ends is how a society actually
// replaces a chart, so it must be allowed.
func TestACardStartingWhereTheLastOneEndsIsAccepted(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "February", "2026-02-01", "2026-03-01")
	declareChart(t, p, "March", "2026-03-01", "2026-04-01")
}

// Milk collected when nothing priced it must be refused rather than given a
// nearby card. A society that has not set a rate for a period has not decided
// what that milk was worth, and the platform has no business deciding for them.
func TestMilkCollectedWhenNoCardWasInForceIsRefused(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "February", "2026-02-01", "2026-03-01")

	_, err := collect(t, p, morning("producer:101", "2026-01-15", "10.000", "4.1", "8.6"))
	if err == nil {
		t.Fatal("milk from January was priced against February's chart")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no rate card") {
		t.Errorf("refused with %v, want it to say no card was in force", err)
	}
}

// A producer delivers once in the morning and once in the evening. The same
// producer, day and shift twice means somebody is paid twice for one delivery,
// and it is the commonest defect in imported dairy data.
func TestTheSameProducerDayAndShiftCannotBeRecordedTwice(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "February", "2026-02-01", "2026-03-01")

	if _, err := collect(t, p, morning("producer:101", "2026-02-10", "12.500", "4.1", "8.6")); err != nil {
		t.Fatal(err)
	}
	_, err := collect(t, p, morning("producer:101", "2026-02-10", "12.500", "4.1", "8.6"))
	if err == nil {
		t.Fatal("the same morning's milk was recorded twice")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "already") {
		t.Errorf("refused with %v", err)
	}

	// The evening of the same day is a different delivery and must be accepted.
	evening := morning("producer:101", "2026-02-10", "11.000", "4.2", "8.6")
	evening.Shift = "EVENING"
	if _, err := collect(t, p, evening); err != nil {
		t.Fatalf("the evening collection was refused: %v", err)
	}
}

// A fat of 12 is a failed analyser. Pricing it at the chart's edge buries the
// failure inside a payment that looks ordinary.
func TestAReadingOffTheChartIsRefused(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "February", "2026-02-01", "2026-03-01")

	_, err := collect(t, p, morning("producer:101", "2026-02-10", "12.500", "12.0", "8.6"))
	if err == nil {
		t.Fatal("a fat of 12 was priced")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "outside this chart") {
		t.Errorf("refused with %v, want it to say the reading is off the chart", err)
	}
}

// A fortnight's collections add up to what the producer is owed for the milk,
// before anything is deducted. Summed by the platform rather than by the caller,
// so a client is not adding decimal strings.
func TestAFortnightOfCollectionsAddsUp(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "February", "2026-02-01", "2026-03-01")

	// Ten litres at 43.0000 every morning for five days: 430.00 each, 2150.00.
	for _, day := range []string{"2026-02-01", "2026-02-02", "2026-02-03", "2026-02-04", "2026-02-05"} {
		if _, err := collect(t, p, morning("producer:101", day, "10.000", "4.1", "8.6")); err != nil {
			t.Fatalf("%s: %v", day, err)
		}
	}
	// And one from somebody else, which must not be counted.
	if _, err := collect(t, p, morning("producer:102", "2026-02-01", "10.000", "4.1", "8.6")); err != nil {
		t.Fatal(err)
	}

	got, err := svcclient.Call[listCollectionsReq, listCollectionsResp](
		context.Background(), p.procurement(), procurementSvc+"/ListCollections",
		listCollectionsReq{
			TenantID: p.tenant, ProducerRef: "producer:101",
			From: "2026-02-01", To: "2026-02-28",
		}, p.opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Collections) != 5 {
		t.Fatalf("%d collections, want the 5 that are this producer's", len(got.Collections))
	}
	if got.Total != "2150.00" {
		t.Errorf("total = %s, want 2150.00", got.Total)
	}
	if got.TotalMinorUnits != 215000 {
		t.Errorf("total minor units = %d, want 215000", got.TotalMinorUnits)
	}
	if got.Currency != "INR" {
		t.Errorf("currency = %s", got.Currency)
	}
}

// The rate and the card are stored on the collection rather than looked up when
// a statement is printed. A card corrected next week must not silently change
// what a producer was told they earned last week.
func TestAStoredCollectionKeepsTheRateItWasPricedAt(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "February", "2026-02-01", "2026-03-01")

	first, err := collect(t, p, morning("producer:101", "2026-02-10", "10.000", "4.1", "8.6"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := svcclient.Call[getCollectionReq, recordCollectionResp](
		context.Background(), p.procurement(), procurementSvc+"/GetCollection",
		getCollectionReq{TenantID: p.tenant, ID: first.ID}, p.opts())
	if err != nil {
		t.Fatal(err)
	}
	if got.Collection.Rate != "43.0000" {
		t.Errorf("stored rate = %s, want the 43.0000 it was priced at", got.Collection.Rate)
	}
	if got.Collection.Amount != "430.00" {
		t.Errorf("stored amount = %s", got.Collection.Amount)
	}
	if got.Collection.Explanation == "" {
		t.Error("the stored collection has no explanation")
	}
}

type getCollectionReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}
