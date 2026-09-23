package loader

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/AlisherBaitas/agro-pulse/internal/model"
)

// Низкоуровневые помощники чтения Excel для адаптера SystemElectric.
// Весь разбор Excel живёт в internal/loader/systemelectric*.go.

// seTable — лист Excel с найденной строкой заголовков.
type seTable struct {
	source string
	path   string
	sheet  string
	hdrIdx int        // индекс строки заголовков (0-based)
	header []string   // нормализованные заголовки
	rawHdr []string   // заголовки как в файле
	rows   [][]string // все строки листа (сырые значения)
}

// openSETable открывает файл и ищет лист, в первых 20 строках которого
// есть все required-заголовки (и хотя бы одна колонка-месяц, если needMonths).
// Сначала проверяются preferred-листы, затем остальные.
// Ошибка = критическая ошибка источника (файл не открывается / нет листа /
// нет ключевой колонки).
func openSETable(source, path string, preferred, required []string, needMonths bool) (*seTable, error) {
	base := filepath.Base(path)
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s (%s): не удалось открыть Excel: %w", source, base, err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("%s (%s): в книге нет листов", source, base)
	}
	var checked []string
	for _, sh := range seOrderSheets(sheets, preferred) {
		rows, err := f.GetRows(sh, excelize.Options{RawCellValue: true})
		if err != nil {
			checked = append(checked, fmt.Sprintf("%s (ошибка чтения: %v)", sh, err))
			continue
		}
		limit := len(rows)
		if limit > 20 {
			limit = 20
		}
		for i := 0; i < limit; i++ {
			hdr := make([]string, len(rows[i]))
			for j, c := range rows[i] {
				hdr[j] = seNormHeader(c)
			}
			if !seHasAllHeaders(hdr, required) {
				continue
			}
			if needMonths && seCountMonthHeaders(hdr) == 0 {
				continue
			}
			return &seTable{
				source: source, path: path, sheet: sh,
				hdrIdx: i, header: hdr, rawHdr: rows[i], rows: rows,
			}, nil
		}
		checked = append(checked, sh)
	}
	need := strings.Join(required, ", ")
	if needMonths {
		need += " + колонки месяцев"
	}
	return nil, fmt.Errorf("%s (%s): не найден лист с обязательными колонками [%s]; проверены листы: %v",
		source, base, need, checked)
}

func seOrderSheets(all, preferred []string) []string {
	out := make([]string, 0, len(all))
	used := map[string]bool{}
	for _, p := range preferred {
		for _, s := range all {
			if s == p && !used[s] {
				out = append(out, s)
				used[s] = true
			}
		}
	}
	for _, s := range all {
		if !used[s] {
			out = append(out, s)
		}
	}
	return out
}

func seHasAllHeaders(hdr []string, required []string) bool {
	for _, r := range required {
		n := seNormHeader(r)
		found := false
		for _, h := range hdr {
			if h == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// col — индекс колонки с точным (нормализованным) заголовком, -1 если нет.
func (t *seTable) col(name string) int {
	n := seNormHeader(name)
	for i, h := range t.header {
		if h == n {
			return i
		}
	}
	return -1
}

// colPrefix — индекс первой колонки, заголовок которой начинается с prefix.
func (t *seTable) colPrefix(prefix string) int {
	p := seNormHeader(prefix)
	for i, h := range t.header {
		if strings.HasPrefix(h, p) {
			return i
		}
	}
	return -1
}

func (t *seTable) colName(idx int) string {
	if idx < 0 {
		return ""
	}
	letter, err := excelize.ColumnNumberToName(idx + 1)
	if err != nil {
		return ""
	}
	return letter
}

// issue собирает DataIssue с координатами ячейки. rowIdx — 0-based индекс
// строки в t.rows (или -1), colIdx — 0-based индекс колонки (или -1).
func (t *seTable) issue(sev model.Severity, code, sku string, rowIdx, colIdx int, value, msg string) model.DataIssue {
	is := model.DataIssue{
		Severity: sev, Code: code, SKU: sku,
		Source: t.source, Sheet: t.sheet,
		Value: value, Message: msg,
	}
	if rowIdx >= 0 {
		is.Row = rowIdx + 1
	}
	if colIdx >= 0 {
		is.Column = t.colName(colIdx)
		if colIdx < len(t.rawHdr) && strings.TrimSpace(t.rawHdr[colIdx]) != "" {
			is.Column += " «" + strings.TrimSpace(t.rawHdr[colIdx]) + "»"
		}
	}
	return is
}

// monthCol — колонка, заголовок которой распознан как месяц.
type seMonthCol struct {
	idx int
	ym  model.YearMonth
}

func (t *seTable) monthCols() []seMonthCol {
	var out []seMonthCol
	for i, h := range t.header {
		if ym, ok := seParseRuMonthHeader(h); ok {
			out = append(out, seMonthCol{idx: i, ym: ym})
		}
	}
	// сортировка вставками: колонок немного, порядок в файле обычно уже верный
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ym.Index() < out[j-1].ym.Index(); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func seCountMonthHeaders(hdr []string) int {
	n := 0
	for _, h := range hdr {
		if _, ok := seParseRuMonthHeader(h); ok {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Нормализация
// ---------------------------------------------------------------------------

func seCell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return row[idx]
}

// normHeader: нижний регистр, ё→е, NBSP→пробел, схлопывание пробелов.
func seNormHeader(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "ё", "е")
	return strings.Join(strings.Fields(s), " ")
}

// normSKU убирает только внешние пробелы/NBSP. Ведущие нули, "_",
// регистр и прочие символы сохраняются как есть.
func seNormSKU(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.TrimSpace(s)
}

// normText — для сравнения наименований: трим + схлопывание пробелов.
func seNormText(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\u00a0", " ")), " ")
}

func seIsBlankRow(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// isTotalRow — строка «Итого» в одной из первых трёх ячеек.
func seIsTotalRow(row []string) bool {
	for i := 0; i < 3 && i < len(row); i++ {
		if seNormHeader(row[i]) == "итого" {
			return true
		}
	}
	return false
}

// rowFingerprint — склейка значений строки (без указанных колонок,
// напр. «№»), чтобы отличать точные дубликаты от конфликтующих.
func seRowFingerprint(row []string, skip ...int) string {
	var b strings.Builder
	for i, c := range row {
		isSkip := false
		for _, s := range skip {
			if s == i {
				isSkip = true
				break
			}
		}
		if isSkip {
			continue
		}
		b.WriteString(strings.TrimSpace(c))
		b.WriteByte(0x1f)
	}
	return strings.TrimRight(b.String(), "\x1f")
}

// parseNum разбирает число. present=false для пустой ячейки.
// Ошибка — если ячейка непустая, но это не конечное число.
func seParseNum(raw string) (v float64, present bool, err error) {
	s := strings.ReplaceAll(raw, "\u00a0", "")
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if s == "" {
		return 0, false, nil
	}
	s = strings.ReplaceAll(s, ",", ".")
	v, err = strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, true, fmt.Errorf("не число: %q", raw)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, true, fmt.Errorf("не конечное число: %q", raw)
	}
	return v, true, nil
}

func seIsInteger(v float64) bool { return v == math.Trunc(v) }

// ---------------------------------------------------------------------------
// Даты и месяцы
// ---------------------------------------------------------------------------

var seRuMonthPrefix = map[string]time.Month{
	"янв": time.January, "фев": time.February, "мар": time.March,
	"апр": time.April, "май": time.May, "мая": time.May, "июн": time.June,
	"июл": time.July, "авг": time.August, "сен": time.September,
	"окт": time.October, "ноя": time.November, "дек": time.December,
}

var seYearRe = regexp.MustCompile(`(?:^|\D)((?:19|20)\d{2})(?:\D|$)`)

// parseRuMonthName: «янв», «февр.», «Сентябрь», «май» → месяц.
func seParseRuMonthName(s string) (time.Month, bool) {
	fields := strings.Fields(seNormHeader(s))
	if len(fields) == 0 {
		return 0, false
	}
	r := []rune(strings.TrimRight(fields[0], "."))
	if len(r) < 3 {
		return 0, false
	}
	m, ok := seRuMonthPrefix[string(r[:3])]
	return m, ok
}

// parseRuMonthHeader: «янв. 2024», «Сентябрь 2026 г.» → YearMonth.
// «Продажи 2024», «Ср мес 2024» не распознаются как месяц.
func seParseRuMonthHeader(h string) (model.YearMonth, bool) {
	h = seNormHeader(h)
	m := seYearRe.FindStringSubmatch(h)
	if m == nil {
		return model.YearMonth{}, false
	}
	mon, ok := seParseRuMonthName(h)
	if !ok {
		return model.YearMonth{}, false
	}
	y, _ := strconv.Atoi(m[1])
	return model.YearMonth{Year: y, Month: mon}, true
}

var seDateLayouts = []string{
	"02.01.2006 15:04:05",
	"02.01.2006 15:04",
	"02.01.2006",
	"2.1.2006 15:04:05",
	"2.1.2006",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// parseSEDate: строка 1С «18.01.2023 16:00:11» (час может быть однозначным)
// или серийный номер Excel.
func parseSEDate(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, fmt.Errorf("пустая дата")
	}
	for _, l := range seDateLayouts {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			return t, nil
		}
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil && v > 1 && v < 100000 {
		return excelize.ExcelDateToTime(v, false)
	}
	return time.Time{}, fmt.Errorf("нераспознанная дата %q", raw)
}

var seDDMMYYYYRe = regexp.MustCompile(`(\d{2})\.(\d{2})\.(\d{4})`)

// dateFromFileName: «Товар в пути_SystemElectric на 22.09.2026.xlsx» → 2026-09-22.
func seDateFromFileName(path string) (time.Time, bool) {
	m := seDDMMYYYYRe.FindStringSubmatch(filepath.Base(path))
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("02.01.2006", m[1]+"."+m[2]+"."+m[3], time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

var seDayMonthRe = regexp.MustCompile(`(\d{1,2})\.(\d{1,2})(?:\.(\d{2,4}))?`)

// parseHeaderDate: «24.09» (год берётся из fallbackYear) или «24.09.2026».
func seParseHeaderDate(label string, fallbackYear int) (*time.Time, bool) {
	m := seDayMonthRe.FindStringSubmatch(label)
	if m == nil {
		return nil, false
	}
	d, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	y := fallbackYear
	if m[3] != "" {
		y, _ = strconv.Atoi(m[3])
		if y < 100 {
			y += 2000
		}
	}
	if y == 0 || mo < 1 || mo > 12 || d < 1 || d > 31 {
		return nil, false
	}
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d { // напр. 31.02
		return nil, false
	}
	return &t, true
}

// isPartialMonth — месяц ym не завершён на дату asOf.
func seIsPartialMonth(ym model.YearMonth, asOf time.Time) bool {
	if asOf.IsZero() || ym != model.YearMonthOf(asOf) {
		return false
	}
	lastDay := time.Date(asOf.Year(), asOf.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return asOf.Day() < lastDay
}

// decodeZipName: zip без UTF-8 флага распаковывается в имена вида
// «#U0415#U0436...». Возвращаем нормальную кириллицу.
var seZipURe = regexp.MustCompile(`#U([0-9a-fA-F]{4})`)

func seDecodeZipName(name string) string {
	return seZipURe.ReplaceAllStringFunc(name, func(m string) string {
		v, err := strconv.ParseUint(m[2:], 16, 32)
		if err != nil {
			return m
		}
		return string(rune(v))
	})
}
