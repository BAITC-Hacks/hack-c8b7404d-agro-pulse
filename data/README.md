# Исходные данные

Логические наборы: `data/iek/` и `data/systemelectric/`.
Оригиналы уже находятся вне репозитория, в `Desktop/Cases/IEK` и `Desktop/Cases/Systeme electric`.
Копирование не требуется: IEK передаётся через `--iek-dir`, SystemElectric — через `--se-dir` в `go run ./cmd/app`.
CLI читает XLSX без сохранения и без обновления внешних ссылок.
Не добавляйте исходные коммерческие данные в Git без разрешения команды.
