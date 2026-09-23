package pipeline

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlisherBaitas/agro-pulse/internal/loader"
	"github.com/AlisherBaitas/agro-pulse/internal/model"
)

// Opt-in: commercial workbooks are never required by the portable unit suite.
func TestRealIEK(t *testing.T) {
	root := os.Getenv("AGROPULSE_CASES_DIR")
	if root == "" {
		t.Skip("set AGROPULSE_CASES_DIR to run the real workbook audit")
	}
	dir := filepath.Join(root, "IEK")
	paths, err := filepath.Glob(filepath.Join(dir, "*.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string][32]byte{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		hashes[p] = sha256.Sum256(b)
	}
	d, err := loader.LoadIEK(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Quality.Sources["monthly"].UniqueSKU != 2463 {
		t.Fatalf("unexpected snapshot: %+v", d.Quality.Sources["monthly"])
	}
	cfg := model.Config{AsOf: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), Months: 1}
	strict := Run(d, cfg)
	for _, r := range strict.Recommendations {
		if r.Quantity != nil {
			t.Fatal("IEK must not invent current stock in strict mode", r.SKU)
		}
	}
	cfg.AllowOpeningStock = true
	cfg.BlankSalesZero = true
	cfg.AbsentIncomingZero = true
	report := Run(d, cfg)
	counts := map[string]int{}
	orders := 0
	for _, r := range report.Recommendations {
		counts[r.Status]++
		if r.Quantity != nil && *r.Quantity > 0 {
			orders++
		}
		if r.Forecast != nil && r.Forecast.StockoutAdjustment != 0 {
			t.Fatal("invented stockout duration")
		}
		if r.Explanation == "" {
			t.Fatal("missing explanation")
		}
		if r.StockoutStatus != "not_confirmed" && r.StockoutStatus != "not_available" {
			t.Fatal("stockout evidence invented", r.SKU)
		}
		if len(r.NeedsData) == 0 {
			t.Fatal("missing structured reason", r.SKU)
		}
	}
	if counts["draft"] != 0 || orders != 0 || counts["needs_data"] != len(d.Products) {
		t.Fatal("unconfirmed rules must not produce numerical drafts", counts)
	}
	t.Logf("IEK products=%d status=%v positive_orders=%d issues=%d", len(report.Recommendations), counts, orders, len(d.Quality.Issues))
	for p, h := range hashes {
		b, err := os.ReadFile(p)
		if err != nil || sha256.Sum256(b) != h {
			t.Fatal("source workbook changed", p, err)
		}
	}
}

func TestUnconfirmedInputsRemainNeedsData(t *testing.T) {
	m := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	p := &model.Product{SKU: "synthetic-test", Unit: "шт", Monthly: []model.MonthlySale{{Month: m, Quantity: model.Number{Value: 10, Valid: true}}}, Stocks: []model.Stock{{Date: m.AddDate(0, 1, 0), Kind: "opening", Quantity: model.Number{Value: 50, Valid: true}}}, MOQ: model.MOQ{Kind: "unconfirmed", Quantity: model.Number{Value: 12, Valid: true}}}
	d := model.Dataset{Supplier: "IEK", Products: map[string]*model.Product{p.SKU: p}}
	r := Run(d, model.Config{AsOf: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), Months: 1, AllowOpeningStock: true, AbsentIncomingZero: true, BlankSalesZero: true}).Recommendations[0]
	if r.Quantity != nil || r.Forecast != nil || r.CurrentStock != nil || r.Incoming != nil || r.Analysis == nil || r.Status != "needs_data" || r.StockoutStatus != "not_confirmed" || r.Urgency != "unknown" {
		t.Fatal(r)
	}
}
