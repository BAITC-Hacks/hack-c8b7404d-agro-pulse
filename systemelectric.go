package loader

// Адаптер данных SystemElectric.
//
//   6 Excel-файлов SystemElectric → нормализация → model.SupplierDataset
//
// Адаптер НЕ считает прогноз и заказ. Он только:
//   - читает файлы (исходники не изменяются);
//   - нормализует ключ (код 1С как есть: ведущие нули и "_" сохраняются);
//   - собирает общий датасет;
//   - помечает проблемы (warning / needs_data / blocked) по SKU и по источникам.
//
// Источник истины по полям:
//   продажи (ряд для прогноза)  → «Ежемесячные продажи в кол-м выражении…»
//   транзакции                  → «Динамика продаж…» (отдельно, для сверки/анализа)
//   исторический остаток        → «Ежемесячные остатки…» (НЕ текущий остаток)
//   текущий остаток, в пути     → «Товар в пути… на ДД.ММ.ГГГГ» (снимок)
//   MOQ                         → «MOQ SystemElectric»
//   сезонность                  → «Сезонность…» (уровень компании)
//   lead time поставщика        → NOT PROVIDED (задаётся конфигом ядра)

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"hackalem/internal/model"
)

const SupplierSystemElectric = "SystemElectric"

// Ключи источников (попадают в DataIssue.Source и SKURecord.Sources).
const (
	seSrcMOQ          = "se_moq"
	seSrcTransactions = "se_sales_transactions"
	seSrcStockMonthly = "se_stock_monthly"
	seSrcSalesMonthly = "se_sales_monthly"
	seSrcSeasonality  = "se_seasonality"
	seSrcTransit      = "se_transit_snapshot"
)

// SEFiles — явные пути к файлам. Пустые поля ищутся в SEConfig.Dir.
type SEFiles struct {
	MOQ          string
	Transactions string
	StockMonthly string
	SalesMonthly string
	Seasonality  string
	Transit      string
}

type SEConfig struct {
	Dir   string    // папка с 6 файлами (автопоиск по ключевым словам в имени)
	Files SEFiles   // явные пути (приоритетнее автопоиска)
	AsOf  time.Time // дата среза; по умолчанию — из имени файла «Товар в пути … на ДД.ММ.ГГГГ»
	// Допустимое относительное расхождение сумм транзакций и помесячных
	// продаж по SKU (0.10 = 10%). По умолчанию 0.10.
	ReconcileTolerance float64
}

// Ключевые слова для автопоиска (сравнение по имени файла в нижнем регистре).
var seFileKeywords = []struct {
	source  string
	keyword string
}{
	{seSrcMOQ, "moq"},
	{seSrcTransactions, "динамика продаж"},
	{seSrcStockMonthly, "остатки"},
	{seSrcSalesMonthly, "ежемесячные продажи"},
	{seSrcSeasonality, "сезонность"},
	{seSrcTransit, "товар в пути"},
}

// LoadSystemElectric — единственная публичная точка входа адаптера.
//
// Возвращает error только при критической ошибке обязательного источника
// (MOQ, помесячные продажи, помесячные остатки, товар в пути): файл не
// найден / не открывается / нет листа / нет ключевой колонки.
// Проблемы необязательных источников (транзакции, сезонность) и все
// локальные проблемы SKU возвращаются внутри датасета.
func LoadSystemElectric(cfg SEConfig) (*model.SupplierDataset, error) {
	files, err := resolveSEFiles(cfg)
	if err != nil {
		return nil, err
	}
	tol := cfg.ReconcileTolerance
	if tol <= 0 {
		tol = 0.10
	}
	sink := newSEIssueSink()

	// --- обязательные источники ---
	moq, err := parseSEMOQ(files.MOQ, sink)
	if err != nil {
		return nil, err
	}
	sales, err := parseSEMonthly(seSrcSalesMonthly, files.SalesMonthly, "sales", sink)
	if err != nil {
		return nil, err
	}
	stock, err := parseSEMonthly(seSrcStockMonthly, files.StockMonthly, "stock", sink)
	if err != nil {
		return nil, err
	}
	transit, err := parseSETransit(files.Transit, sink)
	if err != nil {
		return nil, err
	}

	asOf := cfg.AsOf
	if asOf.IsZero() {
		asOf = transit.asOf
	}

	// --- необязательные источники ---
	var tx *seTxData
	if files.Transactions == "" {
		sink.add(seSourceIssue(seSrcTransactions, model.SeverityWarning, "SOURCE_NOT_FOUND",
			"файл транзакций не найден; сверка продаж пропущена"))
	} else if tx, err = parseSETransactions(files.Transactions, sink); err != nil {
		sink.add(seSourceIssue(seSrcTransactions, model.SeverityBlocked, "SOURCE_UNREADABLE", err.Error()))
		tx = nil
	}
	if asOf.IsZero() && tx != nil && !tx.maxDate.IsZero() {
		d := tx.maxDate
		asOf = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		sink.add(seSourceIssue(seSrcTransactions, model.SeverityWarning, "ASOF_FROM_TRANSACTIONS",
			"дата среза взята по последней транзакции: "+asOf.Format("2006-01-02")))
	}
	if asOf.IsZero() {
		return nil, fmt.Errorf("SystemElectric: дата среза неизвестна — задайте SEConfig.AsOf")
	}
	if transit.asOf.IsZero() {
		transit.asOf = asOf
		for _, r := range transit.rows {
			if r.snapshot != nil {
				r.snapshot.AsOf = asOf
			}
		}
	}
	if transit.incomingDate == nil && transit.incomingLabel != "" {
		transit.incomingDate, _ = seParseHeaderDate(transit.incomingLabel, asOf.Year())
	}

	var season *model.SeasonalityProfile
	if files.Seasonality == "" {
		sink.add(seSourceIssue(seSrcSeasonality, model.SeverityWarning, "SOURCE_NOT_FOUND",
			"файл сезонности не найден"))
	} else if season, err = parseSESeasonality(files.Seasonality, asOf, sink); err != nil {
		sink.add(seSourceIssue(seSrcSeasonality, model.SeverityBlocked, "SOURCE_UNREADABLE", err.Error()))
		season = nil
	}

	sink.add(seSourceIssue("", model.SeverityNeedsData, "LEAD_TIME_NOT_PROVIDED",
		"срок поставки SystemElectric в файлах отсутствует; ядро должно взять его из конфигурации"))

	return assembleSE(asOf, tol, moq, sales, stock, transit, tx, season, sink), nil
}

// ---------------------------------------------------------------------------
// Поиск файлов
// ---------------------------------------------------------------------------

func resolveSEFiles(cfg SEConfig) (SEFiles, error) {
	f := cfg.Files
	explicit := map[string]*string{
		seSrcMOQ: &f.MOQ, seSrcTransactions: &f.Transactions, seSrcStockMonthly: &f.StockMonthly,
		seSrcSalesMonthly: &f.SalesMonthly, seSrcSeasonality: &f.Seasonality, seSrcTransit: &f.Transit,
	}
	if cfg.Dir != "" {
		entries, err := os.ReadDir(cfg.Dir)
		if err != nil {
			return f, fmt.Errorf("SystemElectric: не удалось прочитать папку %s: %w", cfg.Dir, err)
		}
		for _, kw := range seFileKeywords {
			dst := explicit[kw.source]
			if *dst != "" {
				continue
			}
			var matches []string
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() || strings.HasPrefix(name, "~$") || !strings.EqualFold(filepath.Ext(name), ".xlsx") {
					continue
				}
				if strings.Contains(strings.ToLower(seDecodeZipName(name)), kw.keyword) {
					matches = append(matches, filepath.Join(cfg.Dir, name))
				}
			}
			switch len(matches) {
			case 0:
			case 1:
				*dst = matches[0]
			default:
				return f, fmt.Errorf("SystemElectric: для %s найдено несколько файлов %v — укажите путь явно в SEConfig.Files",
					kw.source, matches)
			}
		}
	}
	var missing []string
	for _, req := range []string{seSrcMOQ, seSrcSalesMonthly, seSrcStockMonthly, seSrcTransit} {
		if *explicit[req] == "" {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		return f, fmt.Errorf("SystemElectric: не найдены обязательные файлы: %v", missing)
	}
	return f, nil
}

func seSourceIssue(source string, sev model.Severity, code, msg string) model.DataIssue {
	return model.DataIssue{Severity: sev, Code: code, Source: source, Message: msg}
}

// ---------------------------------------------------------------------------
// Сборка общего датасета
// ---------------------------------------------------------------------------

// Код 1С: цифры/латиница, опционально "_" в конце.
var seSKUPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.\-]*_?$`)
var seDigitsOnly = regexp.MustCompile(`^[0-9]+$`)

func assembleSE(
	asOf time.Time, tol float64,
	moq map[string]*seMOQRow,
	sales, stock *seMonthlyData,
	transit *seTransitData,
	tx *seTxData,
	season *model.SeasonalityProfile,
	sink *seIssueSink,
) *model.SupplierDataset {
	ds := &model.SupplierDataset{
		Supplier:     SupplierSystemElectric,
		AsOf:         asOf,
		SKUs:         map[string]*model.SKURecord{},
		Seasonality:  season,
		Coverage:     map[string]int{},
		StatusCounts: map[model.Severity]int{},
	}

	// --- объединение ключей ---
	union := map[string]bool{}
	addKeys := func(src string, n int, keys func(func(string))) {
		ds.Coverage[src] = n
		keys(func(k string) { union[k] = true })
	}
	addKeys(seSrcMOQ, len(moq), func(f func(string)) {
		for k := range moq {
			f(k)
		}
	})
	addKeys(seSrcSalesMonthly, len(sales.rows), func(f func(string)) {
		for k := range sales.rows {
			f(k)
		}
	})
	addKeys(seSrcStockMonthly, len(stock.rows), func(f func(string)) {
		for k := range stock.rows {
			f(k)
		}
	})
	addKeys(seSrcTransit, len(transit.rows), func(f func(string)) {
		for k := range transit.rows {
			f(k)
		}
	})
	if tx != nil {
		addKeys(seSrcTransactions, len(tx.bySKU), func(f func(string)) {
			for k := range tx.bySKU {
				f(k)
			}
		})
	}
	nSources := len(ds.Coverage)
	ds.Coverage["union"] = len(union)

	skus := make([]string, 0, len(union))
	for k := range union {
		skus = append(skus, k)
	}
	sort.Strings(skus)
	ds.SKUOrder = skus

	// сверка транзакций: окно от первого месяца с положительными отгрузками до AsOf
	asOfYM := model.YearMonthOf(asOf)
	var txWinFrom model.YearMonth
	if tx != nil {
		txWinFrom = tx.firstPos
	}
	var gTx, gMonthly float64
	var nTxMismatch int

	for _, sku := range skus {
		m := moq[sku]
		s := sales.rows[sku]
		st := stock.rows[sku]
		tr := transit.rows[sku]
		var txs []model.SalesTransaction
		if tx != nil {
			txs = tx.bySKU[sku]
		}

		rec := &model.SKURecord{Product: model.Product{SKU: sku, Supplier: SupplierSystemElectric}}
		var issues []model.DataIssue
		add := func(sev model.Severity, src, code, msg, value string) {
			issues = append(issues, model.DataIssue{
				Severity: sev, Code: code, SKU: sku, Source: src, Message: msg, Value: value,
			})
		}

		if m != nil {
			rec.Sources = append(rec.Sources, seSrcMOQ)
		}
		if s != nil {
			rec.Sources = append(rec.Sources, seSrcSalesMonthly)
		}
		if st != nil {
			rec.Sources = append(rec.Sources, seSrcStockMonthly)
		}
		if tr != nil {
			rec.Sources = append(rec.Sources, seSrcTransit)
		}
		if len(txs) > 0 {
			rec.Sources = append(rec.Sources, seSrcTransactions)
		}

		// --- формат ключа ---
		if !seSKUPattern.MatchString(sku) {
			add(model.SeverityWarning, "", "SKU_SUSPICIOUS_FORMAT",
				"код 1С содержит нетипичные символы; оставлен без изменений", sku)
		} else if seDigitsOnly.MatchString(sku) {
			add(model.SeverityWarning, "", "SKU_DIGITS_WITHOUT_UNDERSCORE",
				"код из одних цифр без '_' — проверьте, не потерян ли суффикс/ведущие нули в выгрузке", sku)
		}
		if strings.HasSuffix(sku, "_") && union[strings.TrimSuffix(sku, "_")] {
			add(model.SeverityWarning, "", "SKU_UNDERSCORE_VARIANT",
				"в данных есть этот же код без '_'; записи НЕ объединены автоматически", strings.TrimSuffix(sku, "_"))
		}

		// --- справочные поля ---
		rec.Name, _ = sePickFirst(sku, "NAME_MISMATCH", "наименование различается между файлами", &issues,
			seNamed(seSrcMOQ, sePtrStr(m, func() string { return m.name })),
			seNamed(seSrcSalesMonthly, sePtrStr(s, func() string { return s.name })),
			seNamed(seSrcTransit, sePtrStr(tr, func() string { return tr.name })),
			seNamed(seSrcStockMonthly, sePtrStr(st, func() string { return st.name })),
			seNamed(seSrcTransactions, seTxName(tx, sku)),
		)
		rec.SupplierArticle, _ = sePickFirst(sku, "ARTICLE_CONFLICT", "артикул поставщика различается между файлами", &issues,
			seNamed(seSrcMOQ, sePtrStr(m, func() string { return m.article })),
			seNamed(seSrcSalesMonthly, sePtrStr(s, func() string { return s.article })),
			seNamed(seSrcTransit, sePtrStr(tr, func() string { return tr.article })),
		)
		var txUnit string
		if len(txs) > 0 {
			txUnit = txs[0].Unit
		}
		rec.Unit, _ = sePickFirst(sku, "UNIT_CONFLICT", "единица измерения различается между файлами", &issues,
			seNamed(seSrcStockMonthly, sePtrStr(st, func() string { return st.unit })),
			seNamed(seSrcTransactions, txUnit),
		)
		if tr != nil {
			rec.Category = tr.category
			rec.CostPrice = tr.cost
		}

		// --- MOQ ---
		switch {
		case m == nil:
			add(model.SeverityNeedsData, seSrcMOQ, "MOQ_NOT_PROVIDED", "SKU отсутствует в файле MOQ", "")
		case m.moq != nil:
			rec.MOQ = m.moq
			if s != nil && s.multiplicity != nil {
				sm := *s.multiplicity
				if sm == 0 && *m.moq == 1 {
					add(model.SeverityWarning, seSrcMOQ, "MOQ_DEFAULT_SUSPECTED",
						"в файле продаж «Кратность»=0, в файле MOQ =1 — возможно, 1 подставлена по умолчанию", "1")
				} else if sm > 0 && sm != float64(*m.moq) {
					add(model.SeverityWarning, seSrcMOQ, "MOQ_CONFLICT",
						fmt.Sprintf("кратность в файле продаж %g ≠ MOQ %d; используется файл MOQ", sm, *m.moq),
						fmt.Sprintf("%g", sm))
				}
			}
		}
		// (m != nil && m.moq == nil — issue уже добавлен парсером MOQ)

		// --- продажи ---
		if s == nil {
			add(model.SeverityNeedsData, seSrcSalesMonthly, "NO_SALES_HISTORY",
				"SKU отсутствует в помесячных продажах", "")
		} else {
			rec.MonthlySales = seMarkPartial(s.values, asOf)
			if seLast12Zero(rec.MonthlySales, asOfYM) {
				add(model.SeverityWarning, seSrcSalesMonthly, "NO_RECENT_SALES",
					"нет продаж за последние 12 месяцев", "")
			}
		}

		// --- исторический остаток ---
		if st == nil {
			add(model.SeverityWarning, seSrcStockMonthly, "NO_STOCK_HISTORY",
				"SKU отсутствует в помесячных остатках", "")
		} else {
			rec.MonthlyStock = seMarkPartial(st.values, asOf)
		}

		// --- текущий остаток и товар в пути ---
		if tr == nil {
			msg := "SKU отсутствует в снимке «Товар в пути» на " + asOf.Format("02.01.2006") +
				": текущий остаток и товар в пути NOT PROVIDED"
			if last, ok := seLastPresent(rec.MonthlyStock); ok {
				msg += fmt.Sprintf("; есть только исторический остаток за %s (%g) — как текущий НЕ используется", last.Period, last.Qty)
			}
			add(model.SeverityNeedsData, seSrcTransit, "CURRENT_STOCK_NOT_PROVIDED", msg, "")
		} else {
			rec.CurrentStock = tr.snapshot
			if tr.incomingQty > 0 {
				rec.Incoming = append(rec.Incoming, model.IncomingShipment{
					Qty: tr.incomingQty, DateLabel: transit.incomingLabel, Date: transit.incomingDate,
					DateConfirmed: false, Source: seSrcTransit,
				})
			}
			// сверка помесячных продаж «Товар в пути» с основным файлом продаж
			if s != nil {
				var diff []string
				for _, mv := range s.values {
					if mv.Period == asOfYM {
						continue // незавершённый месяц выгружен в разные моменты
					}
					if tv, ok := tr.monthly[mv.Period.Index()]; ok && seAbs(tv-mv.Qty) > 1e-6 {
						diff = append(diff, fmt.Sprintf("%s: %g vs %g", mv.Period, mv.Qty, tv))
					}
				}
				if len(diff) > 0 {
					add(model.SeverityWarning, seSrcTransit, "TRANSIT_SALES_MISMATCH",
						fmt.Sprintf("помесячные продажи в «Товар в пути» расходятся с основным файлом в %d мес. (основной файл приоритетнее)", len(diff)),
						strings.Join(seFirstN(diff, 6), "; "))
				}
			}
		}

		// --- транзакции ---
		rec.Transactions = txs
		if tx != nil && s != nil && txWinFrom.Year > 0 {
			var sumTx, sumM float64
			for _, t := range txs {
				ym := model.YearMonthOf(t.Date)
				if t.DocType == seShipmentDocType && ym.Index() >= txWinFrom.Index() && ym.Index() <= asOfYM.Index() {
					sumTx += t.Qty
				}
			}
			for _, mv := range s.values {
				if mv.Period.Index() >= txWinFrom.Index() && mv.Period.Index() <= asOfYM.Index() {
					sumM += mv.Qty
				}
			}
			gTx += sumTx
			gMonthly += sumM
			den := seMaxF(seAbs(sumTx), seAbs(sumM))
			if den > 0 && seAbs(sumTx-sumM)/den > tol {
				nTxMismatch++
				add(model.SeverityWarning, seSrcTransactions, "TX_MONTHLY_MISMATCH",
					fmt.Sprintf("%s…%s: сумма транзакций %g ≠ помесячные продажи %g; ряд для прогноза — помесячные продажи",
						txWinFrom, asOfYM, sumTx, sumM),
					fmt.Sprintf("%.2f", sumTx/seMaxF(sumM, 1e-9)))
			}
		}

		// --- покрытие ---
		if len(rec.Sources) == 1 {
			add(model.SeverityWarning, rec.Sources[0], "SKU_SINGLE_SOURCE",
				"SKU встречается только в одном источнике", rec.Sources[0])
		}

		// --- статус ---
		all := append(sink.bySKU[sku], issues...)
		delete(sink.bySKU, sku)
		rec.Issues = all
		rec.Status = model.SeverityOK
		for _, is := range all {
			rec.Status = model.MaxSeverity(rec.Status, is.Severity)
		}
		ds.SKUs[sku] = rec
		ds.StatusCounts[rec.Status]++
	}

	// SKU из issues, которых нет в объединении (например, строка отброшена) — в source
	for _, iss := range sink.bySKU {
		ds.SourceIssues = append(ds.SourceIssues, iss...)
	}
	if nTxMismatch > 0 {
		ds.SourceIssues = append(ds.SourceIssues, model.DataIssue{
			Severity: model.SeverityWarning, Code: "TX_MONTHLY_MISMATCH_SUMMARY", Source: seSrcTransactions,
			Value: fmt.Sprintf("%.2f", gTx/seMaxF(gMonthly, 1e-9)),
			Message: fmt.Sprintf("%s…%s: транзакции %g шт vs помесячные продажи %g шт; расхождение > %.0f%% у %d SKU. "+
				"Источники не согласованы — транзакции не смешиваются с помесячным рядом.",
				txWinFrom, asOfYM, gTx, gMonthly, tol*100, nTxMismatch),
		})
	}
	ds.SourceIssues = append(sink.source, ds.SourceIssues...)
	if all := seCountInAll(ds.SKUs, nSources); all >= 0 {
		ds.Coverage["in_all_sources"] = all
	}
	return ds
}

// ---------------------------------------------------------------------------
// помощники сборки
// ---------------------------------------------------------------------------

type seNamedVal struct{ src, val string }

func seNamed(src, val string) seNamedVal { return seNamedVal{src, val} }

// ptrStr безопасно достаёт строку из возможно-nil записи.
func sePtrStr[T any](p *T, get func() string) string {
	if p == nil {
		return ""
	}
	return get()
}

func seTxName(tx *seTxData, sku string) string {
	if tx == nil {
		return ""
	}
	return tx.names[sku]
}

// pickFirst берёт первое непустое значение по приоритету источников;
// если непустые значения различаются — добавляет warning (не исправляет).
func sePickFirst(sku, code, msg string, issues *[]model.DataIssue, vals ...seNamedVal) (string, bool) {
	var chosen string
	distinct := map[string]bool{}
	var parts []string
	for _, v := range vals {
		n := seNormText(v.val)
		if n == "" {
			continue
		}
		if chosen == "" {
			chosen = n
		}
		if !distinct[strings.ToLower(n)] {
			distinct[strings.ToLower(n)] = true
			parts = append(parts, v.src+"="+n)
		}
	}
	if len(distinct) > 1 {
		*issues = append(*issues, model.DataIssue{
			Severity: model.SeverityWarning, Code: code, SKU: sku, Message: msg + "; взято первое по приоритету",
			Value: strings.Join(parts, " | "),
		})
		return chosen, false
	}
	return chosen, true
}

func seMarkPartial(vals []model.MonthlyValue, asOf time.Time) []model.MonthlyValue {
	out := make([]model.MonthlyValue, len(vals))
	copy(out, vals)
	for i := range out {
		out[i].Partial = seIsPartialMonth(out[i].Period, asOf)
	}
	return out
}

func seLast12Zero(vals []model.MonthlyValue, asOf model.YearMonth) bool {
	from := asOf.Index() - 11
	for _, v := range vals {
		if v.Period.Index() >= from && v.Period.Index() <= asOf.Index() && v.Qty > 0 {
			return false
		}
	}
	return true
}

func seLastPresent(vals []model.MonthlyValue) (model.MonthlyValue, bool) {
	for i := len(vals) - 1; i >= 0; i-- {
		if vals[i].Present {
			return vals[i], true
		}
	}
	return model.MonthlyValue{}, false
}

func seCountInAll(skus map[string]*model.SKURecord, nSources int) int {
	n := 0
	for _, r := range skus {
		if len(r.Sources) == nSources {
			n++
		}
	}
	return n
}

func seFirstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(append([]string{}, s[:n]...), fmt.Sprintf("…ещё %d", len(s)-n))
}

func seMaxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
