package demo

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AlisherBaitas/agro-pulse/internal/loader"
	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"github.com/AlisherBaitas/agro-pulse/internal/pipeline"
)

func TestSeasonalGrowthAndExplanation(t *testing.T) {
	d, c, _ := Fixtures()
	r := result(t, d, c, "DEMO-03")
	if r.Forecast.TrendPerMonth <= 0 || r.Forecast.SeasonalityImpact <= 0 || math.Abs(r.Forecast.Quantity-720) > 1e-8 {
		t.Fatal("seasonal growth not reflected", r.Forecast)
	}
	p := d.Products["DEMO-03"]
	for i := range *p.Seasonality {
		p.Seasonality[i].Value = 1
	}
	flat := result(t, d, c, "DEMO-03")
	if r.Forecast.Quantity <= flat.Forecast.Quantity {
		t.Fatal("seasonality had no effect")
	}
	for _, want := range []string{
		fmt.Sprintf("прогноз %.3f", r.Forecast.Quantity),
		fmt.Sprintf("остаток %.3f", r.CurrentStock.Quantity.Value),
		fmt.Sprintf("в пути %.3f", *r.Incoming),
		fmt.Sprintf("заказ %.3f", *r.Quantity),
		fmt.Sprintf("Сезонное влияние %.3f", r.Forecast.SeasonalityImpact),
		fmt.Sprintf("тренд %.3f", r.Forecast.TrendPerMonth),
		fmt.Sprintf("lost demand %.3f", r.Forecast.StockoutAdjustment),
		fmt.Sprintf("выбросов %d", len(r.Forecast.Outliers)),
		fmt.Sprintf("MOQ %.3f", r.MOQ.Quantity.Value),
	} {
		if !strings.Contains(r.Explanation, want) {
			t.Fatalf("explanation missing %q: %s", want, r.Explanation)
		}
	}
}

func result(t *testing.T, d model.Dataset, c model.Config, sku string) model.Recommendation {
	t.Helper()
	r, err := pipeline.RunDemo(d, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range r.Recommendations {
		if rec.SKU == sku {
			if rec.Quantity == nil {
				t.Fatal(rec.NeedsData)
			}
			return rec
		}
	}
	t.Fatal("missing scenario", sku)
	return model.Recommendation{}
}

func TestDemoScenariosAndMOQ(t *testing.T) {
	d, c, _ := Fixtures()
	expected := map[string]float64{"DEMO-01": 276, "DEMO-02": 276, "DEMO-03": 675, "DEMO-04": 72, "DEMO-05": 312}
	for sku, want := range expected {
		r := result(t, d, c, sku)
		if *r.Quantity != want || r.Status != "demo_recommendation" || r.Urgency == "unknown" {
			t.Fatalf("%s quantity=%v want=%v status=%s", sku, *r.Quantity, want, r.Status)
		}
		if r.MOQ.Kind == "multiple" && math.Mod(*r.Quantity, r.MOQ.Quantity.Value) != 0 {
			t.Fatal("not a multiple")
		}
		if r.MOQ.Kind == "minimum" && *r.Quantity > 0 && *r.Quantity < r.MOQ.Quantity.Value {
			t.Fatal("below minimum")
		}
	}
}

func TestDemoStockAndIncomingMonotonicNonnegative(t *testing.T) {
	for _, sku := range []string{"DEMO-01", "DEMO-03"} {
		for _, field := range []string{"stock", "incoming"} {
			d, c, _ := Fixtures()
			p := d.Products[sku]
			d.Products = map[string]*model.Product{sku: p}
			previous := math.Inf(1)
			for _, n := range []float64{0, 1, 12, 50, 100, 300, 400, 1000, 1000000} {
				if field == "stock" {
					p.Stocks[0].Quantity.Value = n
				} else {
					p.Incoming[0].Quantity.Value = n
				}
				r := result(t, d, c, sku)
				if *r.RawOrder < 0 || *r.Quantity < 0 || *r.Quantity > previous {
					t.Fatalf("%s %s=%v increased order to %v", sku, field, n, *r.Quantity)
				}
				previous = *r.Quantity
			}
			if previous != 0 {
				t.Fatal("excess inventory must cover demand")
			}
		}
	}
}

func TestDemoGiantOutlier(t *testing.T) {
	d, c, _ := Fixtures()
	normal := result(t, d, c, "DEMO-01")
	giant := result(t, d, c, "DEMO-02")
	if len(giant.Forecast.Outliers) != 1 || !giant.Forecast.Outliers[0].Applied {
		t.Fatal("giant transaction not corrected", giant.Forecast.Outliers)
	}
	if math.Abs(giant.Analysis.RegularDemand-normal.Analysis.RegularDemand) > 1e-8 || math.Abs(giant.Forecast.Quantity-normal.Forecast.Quantity) > 1e-8 {
		t.Fatal("giant sale distorted regular demand")
	}
}

func TestDemoConfirmedStockout(t *testing.T) {
	d, c, _ := Fixtures()
	r := result(t, d, c, "DEMO-05")
	last := r.Forecast.History[len(r.Forecast.History)-1]
	if r.StockoutStatus != "confirmed_synthetic" || last.Observed != 0 || last.Lost != 310 || last.Corrected <= last.Observed || r.Forecast.StockoutAdjustment != 310 {
		t.Fatal("lost demand not restored", last)
	}
	observed, corrected := 0.0, 0.0
	for _, h := range r.Forecast.History {
		observed += h.Observed
		corrected += h.Corrected
	}
	if corrected <= observed {
		t.Fatal("correction did not increase demand estimate")
	}
	if math.Abs(r.Forecast.Quantity-310) > 1e-8 {
		t.Fatal("lost demand added twice to forecast", r.Forecast.Quantity)
	}
	d.Products["DEMO-05"].Stockouts[0].Confirmed = false
	if _, err := pipeline.RunDemo(d, c); err == nil {
		t.Fatal("unconfirmed evidence accepted")
	}
}

func TestRejectMixedProvenance(t *testing.T) {
	mutations := []func(*model.Dataset){
		func(d *model.Dataset) { d.Source = "partner_data" },
		func(d *model.Dataset) { d.Products["DEMO-01"].Monthly[0].Quantity.Source.Kind = "partner_data" },
		func(d *model.Dataset) { d.Products["DEMO-01"].Stocks[0].Quantity.Source.Kind = "" },
		func(d *model.Dataset) { d.Products["DEMO-01"].Incoming[0].Quantity.Source.Kind = "partner_data" },
		func(d *model.Dataset) { d.Products["DEMO-01"].MOQ.Quantity.Source.Kind = "partner_data" },
		func(d *model.Dataset) { d.Products["DEMO-01"].Seasonality[0].Source.Kind = "partner_data" },
		func(d *model.Dataset) { d.Products["DEMO-05"].Stockouts[0].Source.Kind = "partner_data" },
		func(d *model.Dataset) { d.Products["DEMO-01"].Sales[0].Source.Kind = "partner_data" },
	}
	for _, mutate := range mutations {
		d, c, _ := Fixtures()
		mutate(&d)
		if _, err := pipeline.RunDemo(d, c); err == nil {
			t.Fatal("mixed or missing provenance accepted")
		}
	}
}

func TestEveryExposedValueHasSource(t *testing.T) {
	r, err := Run()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err := json.Unmarshal(b, &tree); err != nil {
		t.Fatal(err)
	}
	var walk func(any, bool)
	walk = func(v any, covered bool) {
		switch x := v.(type) {
		case map[string]any:
			if src, ok := x["source"]; ok && src != Source {
				t.Fatal("invalid source", src)
			}
			if _, has := x["value"]; has {
				if x["source"] != Source {
					t.Fatal("value without source")
				}
				walk(x["value"], true)
				return
			}
			for k, v := range x {
				if k != "source" {
					walk(v, false)
				}
			}
		case []any:
			for _, v := range x {
				walk(v, false)
			}
		default:
			if !covered && v != nil {
				t.Fatalf("unlabelled scalar %#v", v)
			}
		}
	}
	walk(tree, false)
}

func TestStrictCannotEnableDemo(t *testing.T) {
	d, c, _ := Fixtures()
	before := pipeline.Run(d, c)
	if _, err := pipeline.RunDemo(d, c); err != nil {
		t.Fatal(err)
	}
	after := pipeline.Run(d, c)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("demo mutated strict state")
	}
	for _, r := range after.Recommendations {
		if r.Quantity != nil || r.Status != "needs_data" || r.Urgency != "unknown" || r.StockoutStatus == "confirmed_synthetic" {
			t.Fatal("strict enabled demo policy")
		}
	}
}

func TestStrictRealIEKUnchanged(t *testing.T) {
	root := os.Getenv("AGROPULSE_CASES_DIR")
	if root == "" {
		t.Skip("real IEK path not provided")
	}
	d, err := loader.LoadIEK(filepath.Join(root, "IEK"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Source != "partner_data" {
		t.Fatal("partner dataset source lost")
	}
	for _, p := range d.Products {
		for _, m := range p.Monthly {
			if m.Quantity.Source.Kind != "partner_data" {
				t.Fatal("mixed real provenance", p.SKU)
			}
		}
	}
	_, c, _ := Fixtures()
	c.AsOf = c.AsOf.AddDate(0, 8, 21) // 2026-09-22, partner snapshot date
	before := pipeline.Run(d, c)
	if _, err := Run(); err != nil {
		t.Fatal(err)
	}
	after := pipeline.Run(d, c)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("strict IEK changed after demo")
	}
	if len(after.Recommendations) != 3185 || len(d.Quality.MatchedAllSources) != 1719 {
		t.Fatal("IEK coverage changed")
	}
	for _, r := range after.Recommendations {
		if r.Quantity != nil || r.Status != "needs_data" {
			t.Fatal("IEK got synthetic recommendation", r.SKU)
		}
	}
	t.Log("STRICT IEK unchanged: 3185 needs_data, 0 numeric, 1719 matched")
}
