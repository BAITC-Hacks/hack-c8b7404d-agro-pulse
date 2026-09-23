package pipeline

import (
	"fmt"
	"strings"

	"Agro-Pulse/internal/model"
	"Agro-Pulse/internal/normalize"
)

// RunDemo is a separate, provenance-checked entrance to the SAME run function.
// No global switches are changed and real-data Run never enables these rules.
func RunDemo(d model.Dataset, cfg model.Config) (model.SupplierReport, error) {
	if d.Source != "synthetic_demo" || d.Supplier != "DEMO_SUPPLIER" || len(d.Products) == 0 {
		return model.SupplierReport{}, fmt.Errorf("demo requires isolated synthetic_demo dataset")
	}
	if cfg.AsOf.IsZero() || cfg.Months < 1 || cfg.Months > 24 || cfg.AllowOpeningStock || cfg.AbsentIncomingZero || cfg.BlankSalesZero {
		return model.SupplierReport{}, fmt.Errorf("demo requires explicit inputs, no imputation flags")
	}
	check := func(src model.Source) error {
		if src.Kind != "synthetic_demo" || src.File != "" {
			return fmt.Errorf("demo rejects unlabelled or partner source: %+v", src)
		}
		return nil
	}
	number := func(n model.Number) error {
		if err := check(n.Source); err != nil {
			return err
		}
		if !n.Valid || !normalize.Finite(n.Value) || n.Value < 0 {
			return fmt.Errorf("demo requires a finite explicit nonnegative number")
		}
		return nil
	}
	for key, p := range d.Products {
		if p == nil || key != p.SKU || !strings.HasPrefix(key, "DEMO-") || p.Unit != "шт" || p.Blocked || p.Seasonality == nil || len(p.Monthly) == 0 || len(p.Stocks) != 1 || !p.IncomingListed || len(p.Incoming) == 0 {
			return model.SupplierReport{}, fmt.Errorf("incomplete demo product %s", key)
		}
		for _, n := range *p.Seasonality {
			if err := number(n); err != nil {
				return model.SupplierReport{}, err
			}
			if n.Value <= 0 {
				return model.SupplierReport{}, fmt.Errorf("invalid seasonality")
			}
		}
		for _, m := range p.Monthly {
			if err := number(m.Quantity); err != nil {
				return model.SupplierReport{}, err
			}
		}
		for _, s := range p.Sales {
			if err := check(s.Source); err != nil {
				return model.SupplierReport{}, err
			}
			if !normalize.Finite(s.Quantity) || s.Quantity < 0 {
				return model.SupplierReport{}, fmt.Errorf("invalid sale")
			}
		}
		for _, s := range p.Stocks {
			if err := number(s.Quantity); err != nil {
				return model.SupplierReport{}, err
			}
			if !s.Date.Equal(cfg.AsOf) || s.Kind != "snapshot_total" {
				return model.SupplierReport{}, fmt.Errorf("demo requires actual dated snapshot, not opening stock")
			}
		}
		for _, i := range p.Incoming {
			if err := number(i.Quantity); err != nil {
				return model.SupplierReport{}, err
			}
			if i.Due.IsZero() {
				return model.SupplierReport{}, fmt.Errorf("missing due date")
			}
		}
		if err := number(p.MOQ.Quantity); err != nil {
			return model.SupplierReport{}, err
		}
		if p.MOQ.Quantity.Value <= 0 || (p.MOQ.Kind != "minimum" && p.MOQ.Kind != "multiple") {
			return model.SupplierReport{}, fmt.Errorf("explicit MOQ rule required")
		}
		for _, e := range p.Stockouts {
			if err := check(e.Source); err != nil {
				return model.SupplierReport{}, err
			}
			if !e.Confirmed {
				return model.SupplierReport{}, fmt.Errorf("unconfirmed synthetic stockout")
			}
		}
	}
	return run(d, cfg, true), nil
}
