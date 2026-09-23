package loader

import (
	"fmt"
	"time"

	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"github.com/AlisherBaitas/agro-pulse/internal/normalize"
)

// SystemElectricDataset connects the existing adapter to the strict core.
// Missing values and unconfirmed rules remain unavailable, never synthetic.
func SystemElectricDataset(ds *model.SupplierDataset) model.Dataset {
	d := model.Dataset{Source: "partner_data", Supplier: ds.Supplier, Products: map[string]*model.Product{}, Quality: model.Quality{Sources: map[string]*model.SourceStats{}}}
	source := func(name string) model.Source { return model.Source{Kind: "partner_data", File: name} }
	number := func(v float64, present bool, name string) model.Number {
		return model.Number{Value: v, Valid: present && normalize.Finite(v), Source: source(name)}
	}
	issue := func(i model.DataIssue) {
		s := source(i.Source)
		s.Sheet, s.Cell = i.Sheet, fmt.Sprintf("%s%d", i.Column, i.Row)
		d.Quality.Issues = append(d.Quality.Issues, model.Issue{Code: i.Code, SKU: i.SKU, Message: i.Message, Source: s})
	}
	for _, i := range ds.SourceIssues {
		issue(i)
	}
	for name, count := range ds.Coverage {
		d.Quality.Sources[name] = &model.SourceStats{UniqueSKU: count}
	}
	for sku, rec := range ds.SKUs {
		p := &model.Product{SKU: sku, Name: rec.Name, Article: rec.SupplierArticle, Unit: rec.Unit, Category: rec.Category, Blocked: rec.Status == model.SeverityBlocked}
		for _, i := range rec.Issues {
			issue(i)
			p.Warnings = append(p.Warnings, i.Code+": "+i.Message)
		}
		for _, m := range rec.MonthlySales {
			if m.Partial {
				continue
			}
			date := time.Date(m.Period.Year, m.Period.Month, 1, 0, 0, 0, 0, time.UTC)
			p.Monthly = append(p.Monthly, model.MonthlySale{Month: date, Quantity: number(m.Qty, m.Present, seSrcSalesMonthly)})
		}
		for _, m := range rec.MonthlyStock {
			date := time.Date(m.Period.Year, m.Period.Month, 1, 0, 0, 0, 0, time.UTC)
			p.Stocks = append(p.Stocks, model.Stock{Date: date, Kind: "opening", Quantity: number(m.Qty, m.Present, seSrcStockMonthly)})
		}
		for _, t := range rec.Transactions {
			p.Sales = append(p.Sales, model.Sale{Date: t.Date, Document: t.DocType + " " + t.DocNumber, Quantity: t.Qty, Unit: t.Unit, Source: source(seSrcTransactions)})
		}
		if s := rec.CurrentStock; s != nil {
			// Only explicitly supplied free stock is available for replenishment.
			p.Stocks = append(p.Stocks, model.Stock{Date: s.AsOf, Kind: "snapshot_free", Quantity: number(s.Free, s.FreePresent, s.Source)})
		}
		p.IncomingListed = len(rec.Incoming) > 0
		for _, in := range rec.Incoming {
			var due time.Time
			if in.DateConfirmed && in.Date != nil {
				due = *in.Date
			}
			p.Incoming = append(p.Incoming, model.Incoming{Due: due, Quantity: number(in.Qty, true, in.Source)})
		}
		if len(rec.Incoming) == 0 {
			p.Warnings = append(p.Warnings, "incoming_zero_not_confirmed_by_adapter")
		}
		p.MOQ.Kind = "unconfirmed"
		if rec.MOQ != nil {
			p.MOQ.Quantity = number(float64(*rec.MOQ), true, seSrcMOQ)
		}
		// Company coefficients are diagnostic only; never assigned to a SKU.
		d.Products[sku] = p
	}
	if ds.Seasonality != nil {
		for month, v := range ds.Seasonality.Coefficients {
			if month >= time.January && month <= time.December {
				d.Seasonality[int(month)-1] = number(v, true, seSrcSeasonality)
			}
		}
	}
	return d
}
