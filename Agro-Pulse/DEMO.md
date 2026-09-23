# Synthetic demo: отдельный режим, тот же двигатель Go

Все значения этого режима — **synthetic_demo**, не данные заказчика.
Оригинальные XLSX не читаются и не изменяются. SystemElectric не подключается.

```powershell
go run ./cmd/app --demo
go run ./cmd/app --demo --output outputs/demo-results.json
```

Каталог outputs должен существовать; CLI никогда не перезаписывает существующий файл.
`--demo` несовместим с `--iek-dir`, `--as-of` и `--months`. Для этих фиксированных пяти
сценариев дата и горизонт объявлены во входах каждого результата.

## Общие явные demo inputs

- Supplier: DEMO_SUPPLIER; units: шт.
- Дата точного snapshot: 01.01.2026; горизонт: январь 2026, один месяц.
- История: январь–декабрь 2025; каждый месяц обычно состоит из десяти транзакций.
- Сезонность каждого SKU явно плоская: 12 коэффициентов равны 1. Это synthetic input,
  а не применение общей сезонности партнёра ко всем товарам.
- Safety stock = 0. Значение incoming = 0 явно задано там, где партий нет.
- MOQ кратность 12, кроме GROWING DEMAND: минимум 400. Округление дискретных единиц вверх.
- Demo urgency: равномерное потребление внутри месяца; партия доступна в начале даты прибытия.
  `now` — нулевой текущий запас при спросе; `within_horizon` — дефицит в горизонте.
- IQR, Theil–Sen, stockout estimator и calculator используются из существующего ядра.

## Результаты

| SKU / scenario | Regular demand в месяц | Выбросы | Тренд | Stockout adjustment | Forecast | Current stock | Incoming | Raw order | MOQ | Final |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|---:|
| DEMO-01 NORMAL DEMAND | 310 | 0 | 0 | 0 | 310 | 45 | 0 | 265 | кратность 12 | 276 |
| DEMO-02 LARGE OUTLIER | 310 | 1 | 0 | 0 | 310 | 45 | 0 | 265 | кратность 12 | 276 |
| DEMO-03 GROWING DEMAND | 230 | 0 | +20 | 0 | 360 | 45 | 0 | 315 | минимум 400 | 400 |
| DEMO-04 INCOMING GOODS | 310 | 0 | 0 | 0 | 310 | 45 | 200 | 65 | кратность 12 | 72 |
| DEMO-05 STOCKOUT | 284.167 | 0 | 0 | 310 | 310 | 0 | 0 | 310 | кратность 12 | 312 |

Для всех: seasonality_status = explicit_synthetic_sku_profile, seasonal impact = 0,
status = demo_recommendation. Urgency = within_horizon в первых четырёх; now в STOCKOUT.

1. NORMAL: спрос 310 каждый месяц. 310 − 45 = 265; округление до кратности 12 даёт 276.
2. OUTLIER: к одной июньской транзакции добавлено 100000. IQR восстанавливает её
   регулярную величину 31; регулярный спрос и заказ совпадают с NORMAL.
3. GROWING: месяцы от 120 до 340, рост +20. Прогноз следующего месяца 360;
   потребность 315 повышается до явно заданного demo minimum 400.
4. INCOMING: 200 прибывают 05.01.2026. Заказ уменьшается с 276 до 72.
5. STOCKOUT: подтверждены 31 день отсутствия в декабре. Наблюдаемый спрос декабря 0,
   оценка lost demand 310, corrected December demand 310. Среднее по истории увеличивается
   с 284.167 до 310. Lost demand уже внутри corrected history и не прибавляется к заказу повторно.
   Robust forecast мог бы сгладить один нулевой месяц и без коррекции; демонстрируется
   увеличение оценки исторического спроса, а не обязательный рост самого forecast.

## Provenance

Фикстуры создаются только в `internal/demo/scenarios.go`, без загрузчика Excel.
`pipeline.RunDemo` проверяет принадлежность всех количеств, коэффициентов и evidence к
synthetic_demo и отклоняет partner_data, отсутствующие метки, внешние файлы и неполные входы.

В demo JSON каждое входное и выходное значение обёрнуто в `{value, source}`.
Для расчётных показателей `derived_from` указывает на входы/предыдущие результаты.
Входы включают даты, supplier/units, MOQ rule, все транзакции, месяцы, партии,
stockout evidence, сезонные коэффициенты и horizon. Это позволяет воспроизвести расчёт.
У результатов есть полное deterministic explanation и отдельный список MVP assumptions.
Значения из реального loader маркируются partner_data; существующие поля файла/листа/ячейки сохранены.

## Изоляция strict

`pipeline.Run` и `RunDemo` используют одну внутреннюю функцию run и те же модули.
Demo policies передаются локально; глобального mutable-переключателя нет.
Strict никогда не активирует demo stockout или urgency. Synthetic dataset,
переданный в strict entrance, не получает числовые рекомендации.

`TestStrictRealIEKUnchanged` читает реальные шесть файлов и сравнивает весь результат
strict до/после demo через DeepEqual. Ожидается 3185 needs_data, 0 numeric,
1719 ключей во всех источниках. Исходные Excel не изменяются.

## Проверки

```powershell
gofmt -w cmd internal
$env:AGROPULSE_CASES_DIR='C:\Users\assem\OneDrive\Desktop\Cases'
go test ./... -count=1
go vet ./...
```

Без AGROPULSE_CASES_DIR тесты реальных данных пропускаются; demo tests выполняются всегда.
Проверяются обе монотонности, giant outlier, подтверждённый stockout, MOQ minimum/multiple,
неотрицательность, полнота provenance, отказ от смешанных входов и неизменность strict.
