package loader

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"hackalem/internal/model"
)

// Парсеры шести файлов SystemElectric. Каждый парсер:
//   - возвращает error только при критической ошибке источника;
//   - все локальные проблемы кладёт в issueSink и продолжает работу.

// ---------------------------------------------------------------------------
// issueSink
// ---------------------------------------------------------------------------

type seIssueSink struct {
	bySKU  map[string][]model.DataIssue
	source []model.DataIssue
}

func newSEIssueSink() *seIssueSink {
	return &seIssueSink{bySKU: map[string][]model.DataIssue{}}
}

func (s *seIssueSink) add(is model.DataIssue) {
	if is.SKU == "" {
		s.source = append(s.source, is)
		return
	}
	s.bySKU[is.SKU] = append(s.bySKU[is.SKU], is)
}

// dupTracker различает точные дубликаты строк (warning) и конфликтующие
// записи одного SKU (blocked). В обоих случаях используется первая строка.
type seDupTracker struct {
	seen map[string]seDupEntry
}

type seDupEntry struct {
	fp  string
	row int
}

func newSEDupTracker() *seDupTracker { return &seDupTracker{seen: map[string]seDupEntry{}} }

// check возвращает true, если строку надо пропустить (SKU уже встречался).
func (d *seDupTracker) check(t *seTable, sink *seIssueSink, sku, fp string, rowIdx int) bool {
	e, ok := d.seen[sku]
	if !ok {
		d.seen[sku] = seDupEntry{fp: fp, row: rowIdx + 1}
		return false
	}
	if e.fp == fp {
		sink.add(t.issue(model.SeverityWarning, "DUPLICATE_IDENTICAL", sku, rowIdx, -1, "",
			fmt.Sprintf("точный дубликат строки %d, используется первая строка", e.row)))
	} else {
		sink.add(t.issue(model.SeverityBlocked, "DUPLICATE_CONFLICT", sku, rowIdx, -1, "",
			fmt.Sprintf("SKU уже встречался в строке %d с другими значениями; нужна ручная проверка", e.row)))
	}
	return true
}

// ---------------------------------------------------------------------------
// 1. MOQ SystemElectric.xlsx
// ---------------------------------------------------------------------------

type seMOQRow struct {
	sku, name, article string
	moq                *int
	row                int
}

func parseSEMOQ(path string, sink *seIssueSink) (map[string]*seMOQRow, error) {
	t, err := openSETable(seSrcMOQ, path, []string{"Лист_1"},
		[]string{"Номенклатура.Код", "Кратность"}, false)
	if err != nil {
		return nil, err
	}
	cCode, cMOQ := t.col("Номенклатура.Код"), t.col("Кратность")
	cName, cArt, cNum := t.col("Номенклатура"), t.col("Артикул"), t.col("№")

	out := map[string]*seMOQRow{}
	dups := newSEDupTracker()
	for i := t.hdrIdx + 1; i < len(t.rows); i++ {
		row := t.rows[i]
		if seIsBlankRow(row) || seIsTotalRow(row) {
			continue
		}
		sku := seNormSKU(seCell(row, cCode))
		if sku == "" {
			sink.add(t.issue(model.SeverityWarning, "EMPTY_SKU", "", i, cCode, "",
				"строка без кода 1С пропущена"))
			continue
		}
		if dups.check(t, sink, sku, seRowFingerprint(row, cNum), i) {
			continue
		}
		rec := &seMOQRow{
			sku: sku, row: i + 1,
			name:    seNormText(seCell(row, cName)),
			article: strings.TrimSpace(seCell(row, cArt)),
		}
		raw := seCell(row, cMOQ)
		v, present, perr := seParseNum(raw)
		switch {
		case !present:
			sink.add(t.issue(model.SeverityNeedsData, "MOQ_MISSING", sku, i, cMOQ, raw,
				"кратность (MOQ) не заполнена"))
		case perr != nil:
			sink.add(t.issue(model.SeverityBlocked, "MOQ_INVALID", sku, i, cMOQ, raw, perr.Error()))
		case v <= 0:
			sink.add(t.issue(model.SeverityBlocked, "MOQ_NON_POSITIVE", sku, i, cMOQ, raw,
				"кратность должна быть > 0"))
		case !seIsInteger(v):
			sink.add(t.issue(model.SeverityBlocked, "MOQ_NOT_INTEGER", sku, i, cMOQ, raw,
				"кратность должна быть целым числом штук"))
		default:
			n := int(v)
			rec.moq = &n
		}
		out[sku] = rec
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 3–4. Помесячные матрицы: продажи в кол-ве и остатки
// ---------------------------------------------------------------------------

type seMonthlyRow struct {
	sku, name, article, unit string
	multiplicity             *float64 // «Кратность» из файла продаж (для сверки с MOQ)
	values                   []model.MonthlyValue
	row                      int
}

type seMonthlyData struct {
	rows   map[string]*seMonthlyRow
	months []model.YearMonth
}

// kind: "sales" | "stock" — влияет на коды issues.
func parseSEMonthly(source, path, kind string, sink *seIssueSink) (*seMonthlyData, error) {
	t, err := openSETable(source, path, []string{"Лист_1"},
		[]string{"Номенклатура.Код"}, true)
	if err != nil {
		return nil, err
	}
	mcols := t.monthCols()
	months := make([]model.YearMonth, len(mcols))
	for i, mc := range mcols {
		months[i] = mc.ym
		if i > 0 && mc.ym == mcols[i-1].ym {
			sink.add(t.issue(model.SeverityWarning, "DUPLICATE_MONTH_COLUMN", "", t.hdrIdx, mc.idx,
				mc.ym.String(), "месяц встречается в заголовке дважды; используются обе колонки как есть"))
		}
	}
	cCode := t.col("Номенклатура.Код")
	cName, cArt, cNum := t.col("Номенклатура"), t.col("Артикул"), t.col("№")
	cMult, cTotal, cUnit := t.col("Кратность"), t.col("Итого"), t.col("Ед.изм")

	upper := strings.ToUpper(kind)
	out := &seMonthlyData{rows: map[string]*seMonthlyRow{}, months: months}
	dups := newSEDupTracker()
	for i := t.hdrIdx + 1; i < len(t.rows); i++ {
		row := t.rows[i]
		if seIsBlankRow(row) || seIsTotalRow(row) {
			continue
		}
		sku := seNormSKU(seCell(row, cCode))
		if sku == "" {
			// подзаголовок «Количество» под объединёнными ячейками — служебная строка
			if strings.Contains(seNormHeader(strings.Join(row, " ")), "количество") {
				continue
			}
			sink.add(t.issue(model.SeverityWarning, "EMPTY_SKU", "", i, cCode, "",
				"строка без кода 1С пропущена"))
			continue
		}
		if dups.check(t, sink, sku, seRowFingerprint(row, cNum), i) {
			continue
		}
		rec := &seMonthlyRow{
			sku: sku, row: i + 1,
			name:    seNormText(seCell(row, cName)),
			article: strings.TrimSpace(seCell(row, cArt)),
			unit:    strings.TrimSpace(seCell(row, cUnit)),
			values:  make([]model.MonthlyValue, len(mcols)),
		}
		if cMult >= 0 {
			if v, ok, e := seParseNum(seCell(row, cMult)); ok && e == nil {
				rec.multiplicity = &v
			}
		}
		var negatives, fractional []string
		var sum float64
		for k, mc := range mcols {
			raw := seCell(row, mc.idx)
			v, present, perr := seParseNum(raw)
			mv := model.MonthlyValue{Period: mc.ym}
			if perr != nil {
				sink.add(t.issue(model.SeverityWarning, "INVALID_NUMBER", sku, i, mc.idx, raw,
					perr.Error()+"; значение считается отсутствующим"))
			} else if present {
				mv.Qty, mv.Present = v, true
				sum += v
				if v < 0 {
					negatives = append(negatives, fmt.Sprintf("%s=%g", mc.ym, v))
				}
				if !seIsInteger(v) {
					fractional = append(fractional, fmt.Sprintf("%s=%g", mc.ym, v))
				}
			}
			rec.values[k] = mv
		}
		if len(negatives) > 0 {
			sink.add(t.issue(model.SeverityWarning, "NEGATIVE_"+upper, sku, i, -1, strings.Join(negatives, "; "),
				fmt.Sprintf("отрицательные значения в %d мес.; сохранены как есть (вероятно возвраты/корректировки)", len(negatives))))
		}
		if len(fractional) > 0 {
			sink.add(t.issue(model.SeverityWarning, "FRACTIONAL_QTY", sku, i, -1, strings.Join(fractional, "; "),
				"дробное количество для штучного товара"))
		}
		if cTotal >= 0 {
			raw := seCell(row, cTotal)
			if tv, ok, e := seParseNum(raw); ok && e == nil && seAbs(tv-sum) > 1e-6 {
				sink.add(t.issue(model.SeverityWarning, "TOTAL_MISMATCH", sku, i, cTotal, raw,
					fmt.Sprintf("«Итого» в файле %g ≠ сумма месяцев %g", tv, sum)))
			}
		}
		out.rows[sku] = rec
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 2. Динамика продаж (транзакции)
// ---------------------------------------------------------------------------

const seShipmentDocType = "Расходная накладная"

type seTxData struct {
	bySKU    map[string][]model.SalesTransaction
	names    map[string]string
	maxDate  time.Time
	firstPos model.YearMonth // первый месяц с положительными отгрузками
	rows     int
}

var seDocTypeRe = regexp.MustCompile(`^(.*?)\s+\S+\s+от\s+\d{1,2}\.\d{1,2}\.\d{4}`)

func parseSETransactions(path string, sink *seIssueSink) (*seTxData, error) {
	t, err := openSETable(seSrcTransactions, path, []string{"Лист_1"},
		[]string{"Дата", "Код", "Количество"}, false)
	if err != nil {
		return nil, err
	}
	cDate, cCode, cQty := t.col("Дата"), t.col("Код"), t.col("Количество")
	cNum, cDoc, cName := t.col("Номер"), t.col("Документ"), t.col("Номенклатура")
	cUnit, cWh := t.col("Ед."), t.col("Склад")

	out := &seTxData{bySKU: map[string][]model.SalesTransaction{}, names: map[string]string{}}
	type skuAgg struct {
		negRows, badQty, badDate, nonShip int
		negSum                            float64
		firstRow                          int
	}
	agg := map[string]*skuAgg{}
	negByYear := map[int]int{}
	docTypes := map[string]int{}
	units := map[string]int{}
	firstPosIdx := -1

	for i := t.hdrIdx + 1; i < len(t.rows); i++ {
		row := t.rows[i]
		if seIsBlankRow(row) || seIsTotalRow(row) {
			continue
		}
		sku := seNormSKU(seCell(row, cCode))
		if sku == "" {
			sink.add(t.issue(model.SeverityWarning, "EMPTY_SKU", "", i, cCode, "", "транзакция без кода пропущена"))
			continue
		}
		a := agg[sku]
		if a == nil {
			a = &skuAgg{firstRow: i}
			agg[sku] = a
		}
		out.rows++
		if _, ok := out.names[sku]; !ok {
			out.names[sku] = seNormText(seCell(row, cName))
		}
		dt, derr := parseSEDate(seCell(row, cDate))
		if derr != nil {
			a.badDate++
			continue
		}
		qty, present, qerr := seParseNum(seCell(row, cQty))
		if !present || qerr != nil {
			a.badQty++
			continue
		}
		doc := strings.TrimSpace(seCell(row, cDoc))
		docType := doc
		if m := seDocTypeRe.FindStringSubmatch(doc); m != nil {
			docType = strings.TrimSpace(m[1])
		}
		docTypes[docType]++
		unit := strings.TrimSpace(seCell(row, cUnit))
		units[unit]++
		if docType != seShipmentDocType {
			a.nonShip++
		}
		if qty < 0 {
			a.negRows++
			a.negSum += qty
			negByYear[dt.Year()]++
		} else if qty > 0 && docType == seShipmentDocType {
			ym := model.YearMonthOf(dt)
			if firstPosIdx < 0 || ym.Index() < firstPosIdx {
				firstPosIdx = ym.Index()
				out.firstPos = ym
			}
		}
		if dt.After(out.maxDate) {
			out.maxDate = dt
		}
		out.bySKU[sku] = append(out.bySKU[sku], model.SalesTransaction{
			Date: dt, DocType: docType, DocNumber: strings.TrimSpace(seCell(row, cNum)),
			Warehouse: strings.TrimSpace(seCell(row, cWh)), Unit: unit, Qty: qty, Row: i + 1,
		})
	}

	for sku, a := range agg {
		if a.badDate > 0 {
			sink.add(t.issue(model.SeverityWarning, "TX_INVALID_DATE", sku, -1, cDate, "",
				fmt.Sprintf("%d транзакц. с нераспознанной датой исключены", a.badDate)))
		}
		if a.badQty > 0 {
			sink.add(t.issue(model.SeverityWarning, "TX_INVALID_QTY", sku, -1, cQty, "",
				fmt.Sprintf("%d транзакц. с пустым/нечисловым количеством исключены", a.badQty)))
		}
		if a.negRows > 0 {
			sink.add(t.issue(model.SeverityWarning, "TX_NEGATIVE_QTY", sku, -1, cQty, fmt.Sprintf("%g", a.negSum),
				fmt.Sprintf("%d транзакц. с отрицательным количеством (сумма %g); сохранены как есть", a.negRows, a.negSum)))
		}
		if a.nonShip > 0 {
			sink.add(t.issue(model.SeverityWarning, "TX_NON_SHIPMENT_DOC", sku, -1, cDoc, "",
				fmt.Sprintf("%d строк с типом документа ≠ «%s»; в сверке с помесячными продажами не участвуют", a.nonShip, seShipmentDocType)))
		}
	}
	if len(negByYear) > 0 {
		sink.add(t.issue(model.SeverityWarning, "TX_NEGATIVE_ROWS", "", -1, cQty, seFormatIntMap(negByYear),
			"строки с отрицательным количеством по годам; знак не нормализован"))
	}
	if len(docTypes) > 1 {
		sink.add(t.issue(model.SeverityWarning, "TX_MIXED_DOC_TYPES", "", -1, cDoc, seFormatStrMap(docTypes),
			"в файле несколько типов документов"))
	}
	if len(units) > 1 {
		sink.add(t.issue(model.SeverityWarning, "TX_MIXED_UNITS", "", -1, cUnit, seFormatStrMap(units),
			"в файле несколько единиц измерения"))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 6. Товар в пути (снимок на дату)
// ---------------------------------------------------------------------------

type seTransitRow struct {
	sku, article, name, category string
	cost                         *float64
	snapshot                     *model.StockSnapshot
	incomingQty                  float64
	monthly                      map[int]float64 // YearMonth.Index() → qty
	row                          int
}

type seTransitData struct {
	rows          map[string]*seTransitRow
	asOf          time.Time
	hasIncoming   bool
	incomingLabel string
	incomingDate  *time.Time
}

// Колонки складов, которые кладём в ByLocation как есть (без суммирования).
var seTransitLocations = []string{"Остаток ТЗ", "Розничный склад", "РЦ ЕКТ Рыскулова", "Витрина"}

func parseSETransit(path string, sink *seIssueSink) (*seTransitData, error) {
	t, err := openSETable(seSrcTransit, path, []string{"TDSheet"},
		[]string{"Код 1с", "Остаток"}, false)
	if err != nil {
		return nil, err
	}
	out := &seTransitData{rows: map[string]*seTransitRow{}}
	if d, ok := seDateFromFileName(path); ok {
		out.asOf = d
	} else {
		sink.add(t.issue(model.SeverityWarning, "ASOF_NOT_IN_FILENAME", "", -1, -1, "",
			"дата среза не найдена в имени файла"))
	}

	cCode, cArt, cName := t.col("Код 1с"), t.col("Артикул поставщика"), t.col("Наименование")
	cCat, cCost, cNum := t.colPrefix("Категория"), t.col("СС реал"), t.col("№")
	cTotal, cRes, cFree := t.col("Остаток"), t.col("Зарезервировано"), t.col("Свободный остаток")
	cIn := t.colPrefix("СЭ в пути")
	cSum12 := t.col("Сумма последние 12 мес")

	if cFree < 0 || cRes < 0 {
		sink.add(t.issue(model.SeverityWarning, "STOCK_COLUMNS_PARTIAL", "", t.hdrIdx, -1, "",
			"нет колонки «Свободный остаток» или «Зарезервировано»"))
	}
	if cIn < 0 {
		sink.add(t.issue(model.SeverityNeedsData, "INCOMING_COLUMN_MISSING", "", t.hdrIdx, -1, "",
			"колонка «СЭ в пути …» не найдена: товар в пути NOT PROVIDED"))
	} else {
		out.hasIncoming = true
		out.incomingLabel = strings.TrimSpace(strings.TrimPrefix(seNormHeader(t.rawHdr[cIn]), seNormHeader("СЭ в пути")))
		if d, ok := seParseHeaderDate(out.incomingLabel, out.asOf.Year()); ok {
			out.incomingDate = d
		}
		sink.add(t.issue(model.SeverityWarning, "INCOMING_DATE_UNCONFIRMED", "", t.hdrIdx, cIn, out.incomingLabel,
			"дата товара в пути есть только в заголовке колонки; это не подтверждённая дата прихода и не lead time"))
	}
	locCols := map[string]int{}
	for _, name := range seTransitLocations {
		if c := t.col(name); c >= 0 {
			locCols[name] = c
		}
	}
	mcols := t.monthCols()

	var sum12Checked, sum12Is13 int
	dups := newSEDupTracker()
	for i := t.hdrIdx + 1; i < len(t.rows); i++ {
		row := t.rows[i]
		if seIsBlankRow(row) || seIsTotalRow(row) {
			continue
		}
		sku := seNormSKU(seCell(row, cCode))
		if sku == "" {
			sink.add(t.issue(model.SeverityWarning, "EMPTY_SKU", "", i, cCode, "", "строка без кода 1С пропущена"))
			continue
		}
		if dups.check(t, sink, sku, seRowFingerprint(row, cNum), i) {
			continue
		}
		rec := &seTransitRow{
			sku: sku, row: i + 1,
			article:  strings.TrimSpace(seCell(row, cArt)),
			name:     seNormText(seCell(row, cName)),
			category: strings.TrimSpace(seCell(row, cCat)),
			monthly:  map[int]float64{},
		}
		if v, ok, e := seParseNum(seCell(row, cCost)); ok && e == nil {
			rec.cost = &v
		}

		// --- текущий остаток ---
		num := func(c int, label string) (float64, bool) {
			if c < 0 {
				return 0, false
			}
			raw := seCell(row, c)
			v, present, perr := seParseNum(raw)
			if perr != nil {
				sink.add(t.issue(model.SeverityBlocked, "STOCK_INVALID", sku, i, c, raw, label+": "+perr.Error()))
				return 0, false
			}
			if !present {
				return 0, false
			}
			if v < 0 {
				sink.add(t.issue(model.SeverityWarning, "NEGATIVE_STOCK", sku, i, c, raw, label+" < 0"))
			}
			return v, true
		}
		total, okTotal := num(cTotal, "Остаток")
		reserved, okRes := num(cRes, "Зарезервировано")
		free, okFree := num(cFree, "Свободный остаток")
		if okTotal || okFree {
			snap := &model.StockSnapshot{
				AsOf: out.asOf, Source: seSrcTransit,
				Total: total, Reserved: reserved, Free: free,
				ByLocation: map[string]float64{},
			}
			if !okFree && okTotal && okRes {
				// «Свободный» пуст — не вычисляем молча, только сообщаем
				sink.add(t.issue(model.SeverityWarning, "FREE_STOCK_MISSING", sku, i, cFree, "",
					"«Свободный остаток» пуст; Free оставлен 0"))
			}
			if okTotal && okRes && okFree && seAbs(free-(total-reserved)) > 1e-6 {
				sink.add(t.issue(model.SeverityWarning, "FREE_STOCK_INCONSISTENT", sku, i, cFree,
					fmt.Sprintf("%g", free), fmt.Sprintf("Свободный %g ≠ Остаток %g − Резерв %g", free, total, reserved)))
			}
			for name, c := range locCols {
				if v, ok := num(c, name); ok {
					snap.ByLocation[name] = v
				}
			}
			rec.snapshot = snap
		} else {
			sink.add(t.issue(model.SeverityNeedsData, "CURRENT_STOCK_EMPTY", sku, i, cTotal, "",
				"в снимке нет ни «Остаток», ни «Свободный остаток»"))
		}

		// --- товар в пути ---
		if cIn >= 0 {
			raw := seCell(row, cIn)
			v, present, perr := seParseNum(raw)
			switch {
			case perr != nil:
				sink.add(t.issue(model.SeverityWarning, "INCOMING_INVALID", sku, i, cIn, raw, perr.Error()))
			case present && v < 0:
				sink.add(t.issue(model.SeverityWarning, "INCOMING_NEGATIVE", sku, i, cIn, raw,
					"отрицательное количество в пути; не используется"))
			case present:
				rec.incomingQty = v
			}
		}

		// --- помесячные продажи (только для сверки) ---
		var vals []float64
		for _, mc := range mcols {
			if v, ok, e := seParseNum(seCell(row, mc.idx)); ok && e == nil {
				rec.monthly[mc.ym.Index()] = v
				vals = append(vals, v)
			} else {
				vals = append(vals, 0)
			}
		}
		if cSum12 >= 0 && len(vals) >= 13 {
			if s, ok, e := seParseNum(seCell(row, cSum12)); ok && e == nil {
				s12, s13 := seSumLast(vals, 12), seSumLast(vals, 13)
				if s12 != s13 {
					sum12Checked++
					if seAbs(s-s13) < 1e-6 && seAbs(s-s12) > 1e-6 {
						sum12Is13++
					}
				}
			}
		}
		out.rows[sku] = rec
	}
	if sum12Checked > 0 && sum12Is13*2 > sum12Checked {
		sink.add(t.issue(model.SeverityWarning, "SOURCE_FORMULA_13_MONTHS", "", t.hdrIdx, cSum12, "",
			fmt.Sprintf("«Сумма последние 12 мес» фактически = сумма 13 месяцев в %d из %d проверенных строк; "+
				"«Ср мес за последние 12 мес» и «Запас» в файле завышены. Адаптер эти колонки не использует.",
				sum12Is13, sum12Checked)))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// 5. Сезонность
// ---------------------------------------------------------------------------

func parseSESeasonality(path string, asOf time.Time, sink *seIssueSink) (*model.SeasonalityProfile, error) {
	t, err := openSETable(seSrcSeasonality, path, nil, []string{"Месяц", "СЕЗОННОСТЬ"}, false)
	if err != nil {
		return nil, err
	}
	cMonth, cFinal := t.col("Месяц"), t.col("СЕЗОННОСТЬ")

	// колонки «Коэф. сезонности [YYYY]»; год без суффикса берём из соседней «Продажи YYYY»
	yearCols := map[int]int{}
	for j, h := range t.header {
		if !strings.HasPrefix(h, "коэф. сезонности") {
			continue
		}
		y := 0
		if m := seYearRe.FindStringSubmatch(h); m != nil {
			fmt.Sscanf(m[1], "%d", &y)
		} else {
			for k := j - 1; k >= 0; k-- {
				if strings.HasPrefix(t.header[k], "продажи") {
					if m := seYearRe.FindStringSubmatch(t.header[k]); m != nil {
						fmt.Sscanf(m[1], "%d", &y)
					}
					break
				}
			}
		}
		if y > 0 {
			yearCols[y] = j
		}
	}

	prof := &model.SeasonalityProfile{
		Scope:        "company_total",
		ValueUnit:    "NOT PROVIDED",
		Coefficients: map[time.Month]float64{},
		ByYear:       map[int]map[time.Month]float64{},
		Source:       seSrcSeasonality,
	}
	for i := t.hdrIdx + 1; i < len(t.rows); i++ {
		row := t.rows[i]
		label := seCell(row, cMonth)
		if seNormHeader(label) == "итого" {
			break
		}
		mon, ok := seParseRuMonthName(label)
		if !ok {
			continue
		}
		raw := seCell(row, cFinal)
		if v, present, perr := seParseNum(raw); present && perr == nil && v > 0 {
			prof.Coefficients[mon] = v
		} else {
			sink.add(t.issue(model.SeverityWarning, "SEASONALITY_INVALID", "", i, cFinal, raw,
				fmt.Sprintf("коэффициент для месяца %d пуст/невалиден", int(mon))))
		}
		for y, c := range yearCols {
			if !asOf.IsZero() && y == asOf.Year() && mon > asOf.Month() {
				continue // будущий месяц текущего года — в файле 0, не данные
			}
			if v, present, perr := seParseNum(seCell(row, c)); present && perr == nil {
				if prof.ByYear[y] == nil {
					prof.ByYear[y] = map[time.Month]float64{}
				}
				prof.ByYear[y][mon] = v
			}
		}
	}
	// «Поправка 2026/2025 (янв-сен)»: значение в следующей непустой ячейке
	for i := t.hdrIdx + 1; i < len(t.rows) && prof.Adjustment == nil; i++ {
		row := t.rows[i]
		for j := range row {
			if !strings.HasPrefix(seNormHeader(row[j]), "поправка") {
				continue
			}
			for k := j + 1; k < len(row); k++ {
				if v, ok, e := seParseNum(row[k]); ok && e == nil {
					prof.Adjustment = &v
					break
				}
			}
			break
		}
	}
	if len(prof.Coefficients) != 12 {
		sink.add(t.issue(model.SeverityWarning, "SEASONALITY_INCOMPLETE", "", -1, cFinal,
			fmt.Sprintf("%d", len(prof.Coefficients)), "итоговая сезонность задана не для всех 12 месяцев"))
	}
	if seIsPartialMonth(model.YearMonthOf(asOf), asOf) {
		if _, ok := prof.ByYear[asOf.Year()][asOf.Month()]; ok {
			sink.add(t.issue(model.SeverityWarning, "SEASONALITY_PARTIAL_MONTH", "", -1, -1,
				model.YearMonthOf(asOf).String(),
				"незавершённый месяц на дату среза включён в коэффициенты года; итоговая сезонность им искажена"))
		}
	}
	sink.add(t.issue(model.SeverityWarning, "SEASONALITY_AGGREGATE_ONLY", "", -1, -1, "",
		"сезонность одна на всю компанию (не по SKU/категориям), единица базовых сумм не указана"))
	return prof, nil
}

// ---------------------------------------------------------------------------
// мелочи
// ---------------------------------------------------------------------------

func seAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func seSumLast(v []float64, n int) float64 {
	s := 0.0
	for i := len(v) - n; i < len(v); i++ {
		if i >= 0 {
			s += v[i]
		}
	}
	return s
}

func seFormatIntMap(m map[int]int) string {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%d:%d", k, m[k])
	}
	return strings.Join(parts, ", ")
}

func seFormatStrMap(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%q:%d", k, m[k])
	}
	return strings.Join(parts, ", ")
}
