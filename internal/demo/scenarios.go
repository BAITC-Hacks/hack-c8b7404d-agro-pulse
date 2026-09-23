// Package demo owns synthetic fixtures; it never imports loader or reads Excel.
package demo

import (
	"fmt"
	"time"

	"Agro-Pulse/internal/model"
)

const Source = "synthetic_demo"

var scenarioNames = []string{"NORMAL DEMAND", "LARGE OUTLIER", "GROWING DEMAND", "INCOMING GOODS", "STOCKOUT"}

func source(field string) model.Source { return model.Source{Kind: Source, Cell: field} }
func number(v float64, field string) model.Number {
	return model.Number{Value: v, Valid: true, Source: source(field)}
}

// Fixtures returns freshly allocated data on every call; no shared mutable inputs.
func Fixtures() (model.Dataset, model.Config, map[string]string) {
	cfg := model.Config{AsOf: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Months: 1}
	d := model.Dataset{Source: Source, Supplier: "DEMO_SUPPLIER", Products: map[string]*model.Product{}}
	names := map[string]string{}
	for i, scenario := range scenarioNames {
		sku := fmt.Sprintf("DEMO-%02d", i+1)
		names[sku] = scenario
		p := &model.Product{SKU: sku, Name: scenario, Unit: "шт", IncomingListed: true}
		var season [12]model.Number
		for m := range season {
			season[m] = number(1, fmt.Sprintf("%s/seasonality/%d", sku, m+1))
		}
		p.Seasonality = &season
		p.Stocks = []model.Stock{{Date: cfg.AsOf, Kind: "snapshot_total", Quantity: number(45, sku+"/current_stock")}}
		p.Incoming = []model.Incoming{{Due: cfg.AsOf.AddDate(0, 0, 4), Quantity: number(0, sku+"/incoming")}}
		p.MOQ = model.MOQ{Kind: "multiple", Quantity: number(12, sku+"/moq")}
		if scenario == "GROWING DEMAND" {
			p.MOQ = model.MOQ{Kind: "minimum", Quantity: number(400, sku+"/moq")}
		}
		if scenario == "INCOMING GOODS" {
			p.Incoming[0].Quantity = number(200, sku+"/incoming")
		}
		if scenario == "STOCKOUT" {
			p.Stocks[0].Quantity = number(0, sku+"/current_stock")
		}
		for m := 0; m < 12; m++ {
			month := time.Date(2025, time.Month(m+1), 1, 0, 0, 0, 0, time.UTC)
			quantity := 310.0
			if scenario == "GROWING DEMAND" {
				quantity = 120 + 20*float64(m)
			}
			if scenario == "STOCKOUT" && m == 11 {
				quantity = 0
				p.Stockouts = []model.StockoutEvidence{{Month: month, Days: 31, Confirmed: true, Source: source(sku + "/confirmed_stockout")}}
			}
			total := 0.0
			if quantity > 0 {
				for sale := 0; sale < 10; sale++ {
					q := quantity / 10
					if scenario == "LARGE OUTLIER" && m == 5 && sale == 0 {
						q += 100000
					}
					total += q
					p.Sales = append(p.Sales, model.Sale{Date: month.AddDate(0, 0, sale), Document: "Расходная накладная DEMO", Quantity: q, Unit: p.Unit, Source: source(fmt.Sprintf("%s/sales/%d/%d", sku, m+1, sale+1))})
				}
			}
			p.Monthly = append(p.Monthly, model.MonthlySale{Month: month, Quantity: number(total, fmt.Sprintf("%s/monthly/%d", sku, m+1))})
		}
		d.Products[sku] = p
	}
	return d, cfg, names
}
