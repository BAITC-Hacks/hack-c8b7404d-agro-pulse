package model

import (
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Общие модели, в которые loader'ы поставщиков (IEK, SystemElectric)
// нормализуют данные. Если в ядре уже есть эквивалентные типы —
// удалите дубли здесь и поправьте маппинг в loader'е (он собран в
// assembleSE в internal/loader/systemelectric.go).
// ---------------------------------------------------------------------------

// Severity — уровень проблемы данных. Статус SKU = максимальный уровень
// среди его issues.
type Severity string

const (
	SeverityOK        Severity = "ok"
	SeverityWarning   Severity = "warning"    // считать можно, но есть что проверить
	SeverityNeedsData Severity = "needs_data" // для расчёта не хватает данных
	SeverityBlocked   Severity = "blocked"    // данные противоречивы/невалидны, считать нельзя
)

func (s Severity) Rank() int {
	switch s {
	case SeverityWarning:
		return 1
	case SeverityNeedsData:
		return 2
	case SeverityBlocked:
		return 3
	default:
		return 0
	}
}

// MaxSeverity возвращает более тяжёлый из двух уровней.
func MaxSeverity(a, b Severity) Severity {
	if b.Rank() > a.Rank() {
		return b
	}
	if a == "" {
		return SeverityOK
	}
	return a
}

// DataIssue — одна найденная проблема данных. Ничего не исправляется молча:
// всё, что адаптер заметил, попадает сюда.
type DataIssue struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`             // машинный код, напр. MOQ_MISSING
	SKU      string   `json:"sku,omitempty"`    // пусто для проблем уровня источника
	Source   string   `json:"source"`           // ключ источника, напр. se_moq
	Sheet    string   `json:"sheet,omitempty"`  //
	Row      int      `json:"row,omitempty"`    // номер строки Excel (1-based)
	Column   string   `json:"column,omitempty"` // буква колонки Excel или заголовок
	Month    string   `json:"month,omitempty"`  // YYYY-MM
	Value    string   `json:"value,omitempty"`  // сырое значение ячейки
	Message  string   `json:"message"`
}

// YearMonth — календарный месяц без дня.
type YearMonth struct {
	Year  int        `json:"year"`
	Month time.Month `json:"month"`
}

func (ym YearMonth) String() string { return fmt.Sprintf("%04d-%02d", ym.Year, int(ym.Month)) }

// Index — монотонный номер месяца, удобен для сравнения и сортировки.
func (ym YearMonth) Index() int { return ym.Year*12 + int(ym.Month) - 1 }

func YearMonthOf(t time.Time) YearMonth { return YearMonth{Year: t.Year(), Month: t.Month()} }

// MonthlyValue — значение за месяц.
// Present=false означает пустую ячейку в источнике (Qty при этом 0).
// Partial=true — месяц не завершён на дату среза (AsOf).
type MonthlyValue struct {
	Period  YearMonth `json:"period"`
	Qty     float64   `json:"qty"`
	Present bool      `json:"present"`
	Partial bool      `json:"partial,omitempty"`
}

// StockSnapshot — остаток на конкретную дату (НЕ исторический помесячный).
type StockSnapshot struct {
	TotalPresent bool               `json:"total_present"`
	FreePresent  bool               `json:"free_present"`
	AsOf         time.Time          `json:"as_of"`
	Source       string             `json:"source"`
	Total        float64            `json:"total"`                 // «Остаток»
	Reserved     float64            `json:"reserved"`              // «Зарезервировано»
	Free         float64            `json:"free"`                  // «Свободный остаток»
	ByLocation   map[string]float64 `json:"by_location,omitempty"` // прочие склады как есть, без суммирования
}

// IncomingShipment — товар в пути.
// DateConfirmed=false: дата взята из заголовка колонки и её смысл
// (дата прихода / дата выгрузки) источником не подтверждён.
type IncomingShipment struct {
	Qty           float64    `json:"qty"`
	DateLabel     string     `json:"date_label"`
	Date          *time.Time `json:"date,omitempty"`
	DateConfirmed bool       `json:"date_confirmed"`
	Source        string     `json:"source"`
}

// SalesTransaction — строка отгрузочного документа.
type SalesTransaction struct {
	Date      time.Time `json:"date"`
	DocType   string    `json:"doc_type"`
	DocNumber string    `json:"doc_number"`
	Warehouse string    `json:"warehouse"`
	Unit      string    `json:"unit"`
	Qty       float64   `json:"qty"`
	Row       int       `json:"row"`
}

// SupplierProduct — справочная часть SKU.
type SupplierProduct struct {
	SKU             string   `json:"sku"` // код 1С как есть (с ведущими нулями и "_")
	Name            string   `json:"name"`
	SupplierArticle string   `json:"supplier_article"` // артикул поставщика
	Supplier        string   `json:"supplier"`
	Category        string   `json:"category,omitempty"` // сырое значение, расшифровка не предоставлена
	Unit            string   `json:"unit,omitempty"`
	MOQ             *int     `json:"moq,omitempty"`        // nil = NOT PROVIDED / невалиден
	CostPrice       *float64 `json:"cost_price,omitempty"` // себестоимость, если есть
}

// SKURecord — всё, что известно о SKU после нормализации.
type SKURecord struct {
	SupplierProduct
	MonthlySales []MonthlyValue     `json:"monthly_sales"` // основной ряд продаж
	MonthlyStock []MonthlyValue     `json:"monthly_stock"` // исторические остатки (НЕ текущие)
	Transactions []SalesTransaction `json:"transactions,omitempty"`
	CurrentStock *StockSnapshot     `json:"current_stock,omitempty"` // nil = NOT PROVIDED
	Incoming     []IncomingShipment `json:"incoming,omitempty"`
	LeadTimeDays *int               `json:"lead_time_days,omitempty"` // nil = NOT PROVIDED
	Sources      []string           `json:"sources"`
	Status       Severity           `json:"status"`
	Issues       []DataIssue        `json:"issues,omitempty"`
}

// SeasonalityProfile — коэффициенты сезонности.
type SeasonalityProfile struct {
	Scope        string                         `json:"scope"`      // напр. "company_total"
	ValueUnit    string                         `json:"value_unit"` // единица базовых сумм
	Coefficients map[time.Month]float64         `json:"coefficients"`
	ByYear       map[int]map[time.Month]float64 `json:"by_year"`
	Adjustment   *float64                       `json:"adjustment,omitempty"`
	Source       string                         `json:"source"`
}

// SupplierDataset — результат загрузки одного поставщика.
type SupplierDataset struct {
	Supplier     string                `json:"supplier"`
	AsOf         time.Time             `json:"as_of"`
	SKUs         map[string]*SKURecord `json:"skus"`
	SKUOrder     []string              `json:"sku_order"`
	Seasonality  *SeasonalityProfile   `json:"seasonality,omitempty"`
	SourceIssues []DataIssue           `json:"source_issues,omitempty"`
	Coverage     map[string]int        `json:"coverage"`
	StatusCounts map[Severity]int      `json:"status_counts"`
}
