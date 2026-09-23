package calculator

import (
	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"math"
	"testing"
	"time"
)

func TestStockIncomingMonotonic(t *testing.T) {
	for _, kind := range []string{"minimum", "multiple"} {
		in := Input{Forecast: 103, Unit: "шт", MOQ: model.MOQ{Kind: kind, Quantity: model.Number{Value: 12, Valid: true}}}
		previous := math.Inf(1)
		for stock := 0.; stock < 150; stock++ {
			in.CurrentStock = stock
			r, err := Calculate(in)
			if err != nil || r.Quantity > previous || r.Quantity < 0 {
				t.Fatal(r, err)
			}
			previous = r.Quantity
		}
		in.CurrentStock = 0
		previous = math.Inf(1)
		for incoming := 0.; incoming < 150; incoming++ {
			in.Incoming = incoming
			r, err := Calculate(in)
			if err != nil || r.Quantity > previous {
				t.Fatal(r, err)
			}
			previous = r.Quantity
		}
	}
}

func TestMOQAndInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		kind      string
		raw, want float64
	}{{"minimum", 13, 13}, {"minimum", 1, 12}, {"multiple", 13, 24}, {"multiple", 0, 0}} {
		r, err := Calculate(Input{Forecast: tc.raw, Unit: "шт", MOQ: model.MOQ{Kind: tc.kind, Quantity: model.Number{Value: 12, Valid: true}}})
		if err != nil || r.Quantity != tc.want {
			t.Fatal(tc, r, err)
		}
	}
	r, err := Calculate(Input{Forecast: 2.3, Unit: "шт"})
	if err != nil || r.Quantity != 3 || len(r.Warnings) == 0 {
		t.Fatal(r, err)
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), -1, 1e100} {
		if _, err := Calculate(Input{Forecast: v, Unit: "шт"}); err == nil {
			t.Fatal("invalid accepted", v)
		}
	}
}

func TestInventoryMissingAndReceiptTiming(t *testing.T) {
	d := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	cfg := model.Config{AsOf: d, Months: 1}
	stocks := []model.Stock{{Date: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Quantity: model.Number{Value: 100, Valid: true}, Kind: "opening"}}
	if s, _ := CurrentStock(stocks, cfg); s != nil {
		t.Fatal("silently using stale stock")
	}
	cfg.AllowOpeningStock = true
	if s, _ := CurrentStock(stocks, cfg); s != nil {
		t.Fatal("unconfirmed opening stock proxy applied")
	}
	stocks = append(stocks, model.Stock{Date: d, Kind: "snapshot_free"})
	if s, _ := CurrentStock(stocks, cfg); s != nil {
		t.Fatal("missing latest stock replaced with old stock")
	}
	f := model.ForecastResult{Quantity: 100, Periods: []model.ForecastPeriod{{Start: d, End: d.AddDate(0, 1, 0), Quantity: 100}}}
	inc := []model.Incoming{{Due: d.AddDate(0, 0, 20), Quantity: model.Number{Value: 100, Valid: true}}}
	if Urgency(5, f, inc) != "within_horizon" {
		t.Fatal("late incoming masks deficit")
	}
	if Urgency(100, f, nil) != "covered" {
		t.Fatal("covered demand incorrectly urgent")
	}
}

func TestIncomingUnknownIsNotZero(t *testing.T) {
	cfg := model.Config{AsOf: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), Months: 1, AbsentIncomingZero: true}
	if _, _, _, err := IncomingStock(&model.Product{}, cfg); err == nil {
		t.Fatal("unapproved absent incoming zero")
	}
	p := &model.Product{IncomingListed: true, Incoming: []model.Incoming{{Due: cfg.AsOf.AddDate(0, 0, 8), Quantity: model.Number{}}}}
	if _, _, _, err := IncomingStock(p, cfg); err == nil {
		t.Fatal("blank incoming accepted as zero")
	}
	p.Incoming[0].Quantity = model.Number{Value: 0, Valid: true}
	if n, _, _, err := IncomingStock(p, cfg); err != nil || n != 0 {
		t.Fatal("explicit zero lost", n, err)
	}
}
