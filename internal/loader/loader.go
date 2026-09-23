// Package loader owns all XLSX parsing. It never saves a source workbook.
package loader

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/AlisherBaitas/agro-pulse/internal/model"
	"github.com/AlisherBaitas/agro-pulse/internal/normalize"
	"github.com/xuri/excelize/v2"
)

type table struct {
	file, sheet                                         string
	header, start, key, name, unit, article, monthStart int // zero-based columns
	expected                                            map[int]string
}

type schema struct {
	supplier                                     string
	monthly, stocks, transactions, moq, incoming table
	moqColumn                                    int
	moqKind                                      string
	seasonFile, seasonSheet                      string
	snapshot                                     bool
}

type reader struct {
	d    model.Dataset
	dir  string
	keys map[string]map[string]bool
}

func load(dir string, sc schema) (model.Dataset, error) {
	r := reader{dir: dir, d: model.Dataset{Source: "partner_data", Supplier: sc.supplier, Products: map[string]*model.Product{}, Quality: model.Quality{Sources: map[string]*model.SourceStats{}}}, keys: map[string]map[string]bool{}}
	if err := r.months("monthly", sc.monthly, false); err != nil {
		return r.d, err
	}
	if err := r.months("stocks", sc.stocks, true); err != nil {
		return r.d, err
	}
	if err := r.transactions(sc.transactions); err != nil {
		return r.d, err
	}
	if err := r.minimum(sc.moq, sc.moqColumn, sc.moqKind); err != nil {
		return r.d, err
	}
	if err := r.incoming(sc.incoming, sc.snapshot); err != nil {
		return r.d, err
	}
	if err := r.season(sc.seasonFile, sc.seasonSheet); err != nil {
		return r.d, err
	}
	for kind, keys := range r.keys {
		st := r.d.Quality.Sources[kind]
		st.UniqueSKU = len(keys)
		for sku := range keys {
			if r.keys["monthly"][sku] {
				st.MatchedSKU++
			} else {
				st.UnmatchedSKU = append(st.UnmatchedSKU, sku)
			}
		}
		sort.Strings(st.UnmatchedSKU)
	}
	for sku := range r.d.Products {
		all := true
		for _, keys := range r.keys {
			if !keys[sku] {
				all = false
			}
		}
		if all {
			r.d.Quality.MatchedAllSources = append(r.d.Quality.MatchedAllSources, sku)
		}
	}
	sort.Strings(r.d.Quality.MatchedAllSources)
	return r.d, nil
}

func at(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}
func ref(t table, row, col int) model.Source {
	c, _ := excelize.CoordinatesToCellName(col+1, row)
	return model.Source{Kind: "partner_data", File: t.file, Sheet: t.sheet, Cell: c}
}

func (r *reader) issue(code, sku, message string, s model.Source) {
	r.d.Quality.Issues = append(r.d.Quality.Issues, model.Issue{Code: code, SKU: sku, Message: message, Source: s})
}

func (r *reader) number(kind, sku string, s string, src model.Source) model.Number {
	n, ok, err := normalize.Number(s)
	st := r.d.Quality.Sources[kind]
	if err != nil {
		st.InvalidValues++
		r.issue("invalid_number", sku, err.Error(), src)
	} else if !ok {
		st.MissingValues++
	}
	return model.Number{Value: n, Valid: ok, Source: src}
}

func (r *reader) rows(kind string, t table, fn func(*model.Product, []string, int, []string, bool) error) error {
	f, err := excelize.OpenFile(filepath.Join(r.dir, t.file))
	if err != nil {
		return fmt.Errorf("%s: %w", t.file, err)
	}
	defer f.Close()
	rows, err := f.Rows(t.sheet)
	if err != nil {
		return err
	}
	defer rows.Close()
	r.keys[kind] = map[string]bool{}
	r.d.Quality.Sources[kind] = &model.SourceStats{}
	st := r.d.Quality.Sources[kind]
	var header []string
	rn := 0
	for rows.Next() {
		rn++
		row, err := rows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return err
		}
		if rn == t.header {
			header = append([]string(nil), row...)
			for col, want := range t.expected {
				if strings.TrimSpace(at(row, col)) != want {
					return fmt.Errorf("%s!%s: expected %q, got %q", t.file, ref(t, rn, col).Cell, want, at(row, col))
				}
			}
		}
		if rn < t.start {
			continue
		}
		nonempty := false
		total := false
		for _, v := range row {
			if strings.TrimSpace(v) != "" {
				nonempty = true
			}
			if strings.EqualFold(strings.TrimSpace(v), "Итого") {
				total = true
			}
		}
		if !nonempty || total {
			continue
		}
		st.Rows++
		sku := normalize.SKU(at(row, t.key))
		if sku == "" {
			st.MissingSKU++
			r.issue("missing_sku", "", "Nonempty row excluded; SKU NOT PROVIDED", ref(t, rn, t.key))
			continue
		}
		dup := r.keys[kind][sku]
		r.keys[kind][sku] = true
		if dup && kind != "transactions" {
			st.DuplicateRows++
			r.issue("duplicate_sku", sku, "Repeated SKU in "+kind, ref(t, rn, t.key))
		}
		p := r.d.Products[sku]
		if p == nil {
			p = &model.Product{SKU: sku}
			r.d.Products[sku] = p
		}
		if p.Name == "" {
			p.Name = strings.TrimSpace(at(row, t.name))
		}
		if p.Article == "" {
			p.Article = strings.TrimSpace(at(row, t.article))
		}
		u := strings.TrimSpace(at(row, t.unit))
		if u != "" {
			if p.Unit != "" && p.Unit != u {
				p.Blocked = true
				r.issue("unit_conflict", sku, p.Unit+" versus "+u, ref(t, rn, t.unit))
			} else {
				p.Unit = u
			}
		}
		if err := fn(p, row, rn, header, dup); err != nil {
			return fmt.Errorf("%s row %d: %w", t.file, rn, err)
		}
	}
	if rn < t.header {
		return fmt.Errorf("%s: missing header", t.file)
	}
	return rows.Error()
}

func (r *reader) months(kind string, t table, stock bool) error {
	var months []time.Time
	return r.rows(kind, t, func(p *model.Product, row []string, rn int, h []string, dup bool) error {
		if dup {
			p.Blocked = true
			return nil
		}
		if months == nil {
			for i := t.monthStart; i < len(h); i++ {
				if strings.TrimSpace(h[i]) == "Итого" {
					break
				}
				d, err := normalize.MonthHeader(h[i])
				if err != nil {
					return err
				}
				if len(months) > 0 && !d.Equal(months[len(months)-1].AddDate(0, 1, 0)) {
					return fmt.Errorf("nonconsecutive months")
				}
				months = append(months, d)
			}
			if len(months) == 0 {
				return fmt.Errorf("missing monthly columns")
			}
		}
		for i, d := range months {
			col := t.monthStart + i
			n := r.number(kind, p.SKU, at(row, col), ref(t, rn, col))
			if n.Valid && n.Value < 0 {
				r.issue("negative_"+kind, p.SKU, "Signed source value preserved", n.Source)
			}
			if stock {
				p.Stocks = append(p.Stocks, model.Stock{Date: d, Quantity: n, Kind: "opening"})
			} else {
				p.Monthly = append(p.Monthly, model.MonthlySale{Month: d, Quantity: n})
			}
		}
		return nil
	})
}

func (r *reader) transactions(t table) error {
	seen := map[string]bool{}
	return r.rows("transactions", t, func(p *model.Product, row []string, rn int, _ []string, _ bool) error {
		identity := strings.Join(row, "\x00")
		if seen[identity] {
			r.d.Quality.Sources["transactions"].DuplicateRows++
			r.issue("duplicate_transaction", p.SKU, "Identical row retained for review; product blocked", ref(t, rn, 0))
			p.Blocked = true
			return nil
		}
		seen[identity] = true
		d, err := normalize.Date(at(row, 0))
		if err != nil {
			r.issue("invalid_date", p.SKU, err.Error(), ref(t, rn, 0))
			r.d.Quality.Sources["transactions"].InvalidValues++
			return nil
		}
		n := r.number("transactions", p.SKU, at(row, 7), ref(t, rn, 7))
		if !n.Valid {
			return nil
		}
		p.Sales = append(p.Sales, model.Sale{Date: d, Document: at(row, 2), Quantity: n.Value, Unit: at(row, 5), Source: n.Source})
		return nil
	})
}

func (r *reader) minimum(t table, col int, kind string) error {
	return r.rows("moq", t, func(p *model.Product, row []string, rn int, _ []string, dup bool) error {
		n := r.number("moq", p.SKU, at(row, col), ref(t, rn, col))
		if n.Valid && n.Value <= 0 {
			n.Valid = false
			r.issue("invalid_moq", p.SKU, "MOQ must be positive", n.Source)
		}
		if dup {
			if p.MOQ.Quantity.Valid != n.Valid || p.MOQ.Quantity.Value != n.Value {
				p.Blocked = true
				r.issue("conflicting_moq", p.SKU, "Conflicting duplicate MOQ", n.Source)
			}
			return nil
		}
		p.MOQ = model.MOQ{Quantity: n, Kind: kind}
		return nil
	})
}

var duePattern = regexp.MustCompile(`поступление до (\d{2}\.\d{2}\.\d{4})`)

func (r *reader) incoming(t table, snapshot bool) error {
	return r.rows("incoming", t, func(p *model.Product, row []string, rn int, h []string, dup bool) error {
		if dup {
			p.Blocked = true
			return nil
		}
		p.IncomingListed = true
		if snapshot {
			p.Category = at(row, 4)
			n := r.number("incoming", p.SKU, at(row, 51), ref(t, rn, 51))
			p.Stocks = append(p.Stocks, model.Stock{Date: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), Quantity: n, Kind: "snapshot_free"})
			q := r.number("incoming", p.SKU, at(row, 54), ref(t, rn, 54))
			p.Incoming = append(p.Incoming, model.Incoming{Due: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), Quantity: q})
			return nil
		}
		for col := 3; col < 9; col++ {
			m := duePattern.FindStringSubmatch(at(h, col))
			if len(m) != 2 {
				return fmt.Errorf("missing incoming due date in column %d", col+1)
			}
			d, err := normalize.Date(m[1])
			if err != nil {
				return err
			}
			n := r.number("incoming", p.SKU, at(row, col), ref(t, rn, col))
			// Blank is unknown, including on an otherwise listed SKU.
			p.Incoming = append(p.Incoming, model.Incoming{Due: d, Quantity: n})
		}
		return nil
	})
}

func (r *reader) season(file, sheet string) error {
	f, err := excelize.OpenFile(filepath.Join(r.dir, file))
	if err != nil {
		return err
	}
	defer f.Close()
	h, err := f.GetCellValue(sheet, "L10")
	if err != nil {
		return err
	}
	if h != "СЕЗОННОСТЬ" {
		return fmt.Errorf("%s!L10: unexpected seasonality header %q", file, h)
	}
	for i := 0; i < 12; i++ {
		cell := fmt.Sprintf("L%d", i+11)
		s, err := f.GetCellValue(sheet, cell, excelize.Options{RawCellValue: true})
		if err != nil {
			return err
		}
		n, ok, err := normalize.Number(s)
		if err != nil || !ok || n <= 0 || math.IsInf(n, 0) {
			return fmt.Errorf("%s!%s: missing/invalid cached seasonality", file, cell)
		}
		r.d.Seasonality[i] = model.Number{Value: n, Valid: true, Source: model.Source{Kind: "partner_data", File: file, Sheet: sheet, Cell: cell}}
	}
	return nil
}
