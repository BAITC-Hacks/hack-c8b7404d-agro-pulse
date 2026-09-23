package loader

import (
	"math"
	"os"
	"testing"
	"time"

	"hackalem/internal/model"
)

func TestParseRuMonthHeader(t *testing.T) {
	cases := []struct {
		in   string
		want model.YearMonth
		ok   bool
	}{
		{"янв. 2024", model.YearMonth{Year: 2024, Month: time.January}, true},
		{"февр. 2025", model.YearMonth{Year: 2025, Month: time.February}, true},
		{"май 2026", model.YearMonth{Year: 2026, Month: time.May}, true},
		{"сент. 2026", model.YearMonth{Year: 2026, Month: time.September}, true},
		{"Сентябрь 2026 г.", model.YearMonth{Year: 2026, Month: time.September}, true},
		{"Январь 2024 г.", model.YearMonth{Year: 2024, Month: time.January}, true},
		{"Продажи 2024", model.YearMonth{}, false},
		{"Ср мес 2024", model.YearMonth{}, false},
		{"Сумма последние 12 мес", model.YearMonth{}, false},
		{"Итого", model.YearMonth{}, false},
	}
	for _, c := range cases {
		got, ok := seParseRuMonthHeader(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseRuMonthHeader(%q) = %v,%v; want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNormSKUKeepsSignificantChars(t *testing.T) {
	cases := map[string]string{
		" 030200128_ ":     "030200128_",
		"030200128_":       "030200128_",
		"ATN000446":        "ATN000446",
		"\u00a0300200428_": "300200428_",
	}
	for in, want := range cases {
		if got := seNormSKU(in); got != want {
			t.Errorf("normSKU(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestParseNum(t *testing.T) {
	if v, ok, err := seParseNum("1050.61"); !ok || err != nil || v != 1050.61 {
		t.Fatalf("1050.61 → %v %v %v", v, ok, err)
	}
	if v, ok, err := seParseNum("-10"); !ok || err != nil || v != -10 {
		t.Fatalf("-10 → %v %v %v", v, ok, err)
	}
	if _, ok, err := seParseNum("  "); ok || err != nil {
		t.Fatalf("empty must be not present, no error")
	}
	if _, ok, err := seParseNum("#N/A"); !ok || err == nil {
		t.Fatalf("#N/A must be present with error")
	}
	if v, _, err := seParseNum("1 234,5"); err != nil || v != 1234.5 {
		t.Fatalf("1 234,5 → %v %v", v, err)
	}
}

func TestParseSEDate(t *testing.T) {
	for _, s := range []string{"18.01.2023 16:00:11", "07.08.2025 9:55:01", "22.09.2026"} {
		if _, err := parseSEDate(s); err != nil {
			t.Errorf("parseSEDate(%q): %v", s, err)
		}
	}
	if _, err := parseSEDate("вчера"); err == nil {
		t.Error("ожидалась ошибка")
	}
}

func TestDecodeZipName(t *testing.T) {
	in := "#U0422#U043e#U0432#U0430#U0440 #U0432 #U043f#U0443#U0442#U0438_SystemElectric #U043d#U0430 22.09.2026.xlsx"
	want := "Товар в пути_SystemElectric на 22.09.2026.xlsx"
	if got := seDecodeZipName(in); got != want {
		t.Errorf("decodeZipName = %q; want %q", got, want)
	}
}

func TestHeaderAndFileDates(t *testing.T) {
	d, ok := seDateFromFileName("/x/Товар в пути_SystemElectric на 22.09.2026.xlsx")
	if !ok || d.Format("2006-01-02") != "2026-09-22" {
		t.Fatalf("dateFromFileName: %v %v", d, ok)
	}
	h, ok := seParseHeaderDate("24.09", 2026)
	if !ok || h.Format("2006-01-02") != "2026-09-24" {
		t.Fatalf("parseHeaderDate: %v %v", h, ok)
	}
	if !seIsPartialMonth(model.YearMonth{Year: 2026, Month: time.September}, d) {
		t.Error("сентябрь 2026 на 22.09 должен быть незавершённым")
	}
}

// Интеграционный тест на реальных файлах:
//   SE_DATA_DIR="/path/to/Systeme electric" go test ./internal/loader -run TestLoadSystemElectricReal -v
// Ожидаемые числа получены при аудите файлов (см. docs/DATA_AUDIT_SYSTEMELECTRIC.md).
func TestLoadSystemElectricReal(t *testing.T) {
	dir := os.Getenv("SE_DATA_DIR")
	if dir == "" {
		t.Skip("SE_DATA_DIR не задан")
	}
	ds, err := LoadSystemElectric(SEConfig{Dir: dir})
	if err != nil {
		t.Fatalf("LoadSystemElectric: %v", err)
	}
	if got := ds.AsOf.Format("2006-01-02"); got != "2026-09-22" {
		t.Errorf("AsOf = %s", got)
	}
	wantCov := map[string]int{
		seSrcMOQ: 554, seSrcSalesMonthly: 554, seSrcStockMonthly: 701,
		seSrcTransit: 497, seSrcTransactions: 565, "union": 724,
	}
	for k, v := range wantCov {
		if ds.Coverage[k] != v {
			t.Errorf("Coverage[%s] = %d; want %d", k, ds.Coverage[k], v)
		}
	}

	r := ds.SKUs["130300028_"]
	if r == nil || r.MOQ == nil || *r.MOQ != 140 {
		t.Errorf("MOQ 130300028_ ожидался 140: %+v", r)
	}
	r = ds.SKUs["300200428_"]
	if r == nil || r.CurrentStock == nil || r.CurrentStock.Total != 1118 || r.Category != "1" {
		t.Errorf("снимок 300200428_ неверен: %+v", r)
	}
	if r != nil && len(r.MonthlySales) != 33 {
		t.Errorf("ожидалось 33 месяца продаж, получено %d", len(r.MonthlySales))
	}

	var incoming float64
	for _, rec := range ds.SKUs {
		for _, in := range rec.Incoming {
			incoming += in.Qty
		}
	}
	if incoming != 50160 {
		t.Errorf("сумма товара в пути = %g; want 50160", incoming)
	}

	// ведущий ноль и "_" сохранены
	if _, ok := ds.SKUs["030200128_"]; !ok {
		t.Error("ключ 030200128_ потерян")
	}

	if ds.Seasonality == nil || math.Abs(ds.Seasonality.Coefficients[time.January]-0.8431691) > 1e-6 {
		t.Errorf("сезонность января неверна: %+v", ds.Seasonality)
	}

	t.Logf("status: %v", ds.StatusCounts)
	t.Logf("source issues: %d", len(ds.SourceIssues))
	for _, is := range ds.SourceIssues {
		t.Logf("  [%s] %s %s: %s", is.Severity, is.Source, is.Code, is.Message)
	}
}
