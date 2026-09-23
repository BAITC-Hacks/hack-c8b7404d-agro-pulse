package loader

import (
	"testing"
	"time"

	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"github.com/AlisherBaitas/agro-pulse/internal/pipeline"
)

func TestSystemElectricStrictBridge(t *testing.T) {
	asof := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	moq := 12
	ds := &model.SupplierDataset{Supplier: SupplierSystemElectric, AsOf: asof, SKUs: map[string]*model.SKURecord{
		"001_": {SupplierProduct: model.SupplierProduct{SKU: "001_", Unit: "шт", Category: "1", MOQ: &moq},
			MonthlySales: []model.MonthlyValue{{Period: model.YearMonth{Year: 2026, Month: time.August}, Qty: 100, Present: true}},
			CurrentStock: &model.StockSnapshot{AsOf: asof, Total: 90, TotalPresent: true},
			Incoming:     []model.IncomingShipment{{Qty: 30, Date: &asof, DateConfirmed: false}},
		},
		"missing": {SupplierProduct: model.SupplierProduct{SKU: "missing"}, Issues: []model.DataIssue{{Code: "NO_SALES_HISTORY", SKU: "missing", Message: "missing history"}}},
	}, Seasonality: &model.SeasonalityProfile{Coefficients: map[time.Month]float64{time.January: 2}}}
	d := SystemElectricDataset(ds)
	if d.Source != "partner_data" || len(d.Products) != 2 || len(d.Quality.Issues) != 1 {
		t.Fatal("lost provenance, SKU or issues", d)
	}
	p := d.Products["001_"]
	if p.Category != "1" || p.Seasonality != nil || p.MOQ.Kind != "unconfirmed" || !p.Incoming[0].Due.IsZero() || p.Stocks[0].Quantity.Valid {
		t.Fatal("bridge invented confirmed inputs", p)
	}
	r := pipeline.Run(d, model.Config{AsOf: asof, Months: 1})
	for _, rec := range r.Recommendations {
		if rec.Status != "needs_data" || rec.Quantity != nil || rec.Incoming != nil || rec.CurrentStock != nil {
			t.Fatal("invented order", rec)
		}
	}
	ds.SKUs["001_"].CurrentStock.FreePresent = true
	d = SystemElectricDataset(ds)
	if !d.Products["001_"].Stocks[0].Quantity.Valid || d.Products["001_"].Stocks[0].Quantity.Value != 0 {
		t.Fatal("explicit zero lost")
	}
}
