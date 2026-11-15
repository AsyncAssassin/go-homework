# Ledger

Пакет `ledger` содержит бизнес-логику учёта расходов: транзакции, бюджеты с периодами, проверку данных и отчёт по категориям. Его импортирует [Gateway](../gateway/README.md), а консольная программа `cmd/ledger` показывает сохранённые данные.

С ДЗ 7 Ledger хранит транзакции и бюджеты в PostgreSQL, а отчёты и список бюджетов кеширует в Redis на 30 секунд. Хранилище в памяти осталось для тестов без Docker и для демо ДЗ 2-4.

## Быстрый старт

Из корня репозитория:

```sh
docker compose up -d --wait   # PostgreSQL 16 и Redis 7, база cashapp создаётся сама
cd ledger
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable'
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/ledger
```

Подробности о каждом шаге ниже.

## Устройство

| Файл | Что внутри |
|------|------------|
| `transaction.go`, `budget.go`, `validate.go` | типы, проверка данных, правило бюджета и функции пакета |
| `report.go` | `GetReportSummary(ctx, from, to)` |
| `storage.go` | интерфейс хранилища, `Connect`, `Close`, `Reset` |
| `memory_store.go` | хранилище в памяти |
| `postgres_store.go` | хранилище в PostgreSQL |
| `cached_store.go` | кеш Redis поверх хранилища |
| `internal/db` | DSN из переменных окружения, `sql.Open("pgx", dsn)`, лимиты пула и `Ping` |
| `internal/cache` | клиент Redis: настройки из `REDIS_*`, чтение и запись JSON с TTL, лимит 0.5 с на запрос |
| `migrations/` | миграции goose |
| `cmd/ledger` | консольная программа |

Функции пакета:
- `Connect(ctx) error` подключает PostgreSQL и Redis по переменным окружения, проверяет, что миграции применены, и пишет в лог оба подключения. При ошибке возвращает понятное сообщение и оставляет прежнее хранилище. `Close() error` закрывает соединения.
- `SetBudget(b) (Budget, error)`, `ListBudgets() ([]Budget, error)`, `AddTransaction(tx) (Transaction, error)`, `ListTransactions() ([]Transaction, error)`, `ListBudgetUsage(date)` и `LoadBudgets(r)` работают как в ДЗ 3-5, но возвращают и ошибку хранилища.
- `GetReportSummary(ctx, from, to) (ReportSummary, error)` возвращает траты по категориям с дня `from` по день `to` включительно и их общую сумму. Дни берутся в местном часовом поясе, допустимы дни с 0001-01-02 по 9999-12-30.
- `Day(year, month, day) time.Time` возвращает начало дня в местном часовом поясе. Обычно это полночь, но в некоторых поясах при переходе на летнее время часы переводятся с 00:00 на 01:00, например 2026-09-06 в America/Santiago, и такой день начинается в 01:00. Через `Day` дни строят Gateway и консольная программа.

## Стенд в Docker

`compose.yaml` в корне репозитория запускает:
- `postgres:16-alpine`: пользователь и пароль `postgres`, база `cashapp`, данные в именованном томе;
- `redis:7-alpine` без пароля.

Порты 5432 и 6379 открыты только на `127.0.0.1`. Если они заняты, порты меняются переменными `PG_PORT` и `REDIS_PORT`, например `PG_PORT=15432 docker compose up -d --wait`. Тот же порт нужно указать в `DATABASE_URL` или `DB_PORT`. Пароль `postgres` годится только для локальной разработки.

Остановка: `docker compose down`. Данные PostgreSQL остаются в томе, `docker compose down -v` удаляет и их.

## Создание базы и миграции

В Docker база `cashapp` создаётся при первом запуске контейнера. На собственном сервере PostgreSQL её нужно создать вручную:

```sh
createdb -h localhost -U postgres cashapp
```

Миграции применяет утилита goose (проверено на v3.30.1): `brew install goose` или `go install github.com/pressly/goose/v3/cmd/goose@v3.30.1`. Команды выполняются из каталога `ledger`:

```sh
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable'
goose -dir migrations postgres "$DATABASE_URL" up       # применить все миграции
goose -dir migrations postgres "$DATABASE_URL" status   # список применённых миграций
goose -dir migrations postgres "$DATABASE_URL" down     # откатить последнюю миграцию
goose -dir migrations postgres "$DATABASE_URL" reset    # откатить все миграции
```

Вывод `up` на пустой базе:

```text
2026/11/14 15:24:55 OK   00001_create_budgets.sql (7.01ms)
2026/11/14 15:24:55 OK   00002_create_expenses.sql (5.07ms)
2026/11/14 15:24:55 goose: successfully migrated database to version: 2
```

В каждом файле миграции есть секции `-- +goose Up` и `-- +goose Down`: так goose хранит применение и откат одной миграции вместе.

### Схема

| Таблица | Столбцы |
|---------|---------|
| `budgets` (`00001_create_budgets.sql`) | `id SERIAL PRIMARY KEY`, `category TEXT UNIQUE NOT NULL`, `limit_amount NUMERIC(14,2) NOT NULL CHECK (limit_amount > 0)`, `period TEXT NOT NULL DEFAULT ''` со значениями `''`, `month` или `year` |
| `expenses` (`00002_create_expenses.sql`) | `id SERIAL PRIMARY KEY`, `amount NUMERIC(14,2) NOT NULL CHECK (amount <> 0)`, `category TEXT NOT NULL`, `description TEXT`, `date DATE NOT NULL`; индекс `(category, date)` |

Столбец `period` добавлен к схеме из условия, чтобы сохранить периоды бюджетов из ДЗ 3: пустое значение означает один лимит на всё время. Индекс ускоряет подсчёт трат категории за период.

## Переменные окружения

| Переменная | По умолчанию | Назначение |
|------------|--------------|------------|
| `DATABASE_URL` | не задана | строка подключения к PostgreSQL; если задана, `DB_*` не читаются |
| `DB_HOST` | `localhost` | хост PostgreSQL |
| `DB_PORT` | `5432` | порт PostgreSQL |
| `DB_USER` | `postgres` | пользователь |
| `DB_PASS` | `postgres` | пароль, спецсимволы экранируются |
| `DB_NAME` | `cashapp` | база данных |
| `REDIS_ADDR` | `localhost:6379` | адрес Redis |
| `REDIS_DB` | `0` | номер базы Redis; не число или отрицательное число дают ошибку |
| `REDIS_PASSWORD` | пусто | пароль Redis |

Без `DATABASE_URL` строка подключения собирается из `DB_*` с `sslmode=disable`. Значения по умолчанию совпадают со стендом из `compose.yaml`, поэтому для него переменные можно не задавать. Пример с явными настройками:

```sh
DATABASE_URL='postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable' \
REDIS_ADDR=localhost:6379 REDIS_DB=0 REDIS_PASSWORD= \
go run ./cmd/ledger
```

## Подключение и пул

`internal/db` открывает пул через `sql.Open("pgx", dsn)` с драйвером `github.com/jackc/pgx/v5/stdlib` и задаёт лимиты:

```go
db.SetMaxOpenConns(10)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(30 * time.Minute)
```

Затем `PingContext` с таймаутом 5 секунд проверяет соединение, а запрос `SELECT to_regclass(...)` проверяет, что в базе есть таблицы `budgets` и `expenses`, то есть миграции применены. Redis проверяется командой `PING`. Каждая операция с хранилищем ограничена 5 секундами. Любой запрос к Redis, включая подключение, ограничен 0.5 секунды и при ошибке не повторяется: кеш только экономит запросы к PostgreSQL, поэтому ledger не ждёт медленный Redis. В лог пишутся сервер и база, пароль не выводится:

```text
2026/11/14 15:25:17 connected to PostgreSQL at localhost:5432/cashapp
2026/11/14 15:25:17 connected to Redis at localhost:6379, db 0
```

Если PostgreSQL или Redis недоступен, Gateway и консольная программа пишут понятную ошибку и завершаются с кодом 1:

```text
$ DB_PORT=1 go run ./cmd/ledger
2026/11/15 19:55:42 Ledger service failed: connect to PostgreSQL at localhost:1/cashapp: failed to connect to `user=postgres database=cashapp`:
	[::1]:1 (localhost): dial error: dial tcp [::1]:1: connect: connection refused
	127.0.0.1:1 (localhost): dial error: dial tcp 127.0.0.1:1: connect: connection refused
exit status 1

$ REDIS_ADDR=localhost:1 go run ./cmd/ledger
2026/11/15 19:55:46 connected to PostgreSQL at localhost:5432/cashapp
redis: 2026/11/15 19:55:46 pool.go:617: redis: connection pool: failed to dial after 1 attempts: dial tcp [::1]:1: connect: connection refused
2026/11/15 19:55:46 Ledger service failed: connect to Redis at localhost:1, db 0: dial tcp [::1]:1: connect: connection refused
exit status 1
```

Строку с префиксом `redis:` пишет библиотека go-redis: это её неудачная попытка соединения перед итоговой ошибкой.

Если миграции не применены, например сразу после первого `docker compose up`, программа не запускается и подсказывает, что сделать:

```text
$ go run ./cmd/ledger
2026/11/15 19:55:38 Ledger service failed: PostgreSQL at localhost:5432/cashapp has no table budgets: apply the migrations with goose up, see ledger/README.md
exit status 1
```

Если `DATABASE_URL` не удаётся разобрать, ошибка не цитирует строку подключения, потому что в ней может быть пароль. Спецсимволы пароля в URL кодируются через `%`, например `#` записывается как `%23`, `/` как `%2F`, `?` как `%3F`:

```text
$ DATABASE_URL='postgres://postgres:pa#ss@localhost:5432/cashapp?sslmode=disable' go run ./cmd/ledger
2026/11/15 19:55:42 Ledger service failed: invalid PostgreSQL connection string: check DATABASE_URL or the DB_* variables; special characters in a URL password must be percent-encoded
exit status 1
```

## Хранение в PostgreSQL

- `SetBudget` выполняет UPSERT (`INSERT ... ON CONFLICT (category) DO UPDATE`), поэтому у категории один бюджет.
- `LoadBudgets` сохраняет все бюджеты файла в одной транзакции БД в порядке категорий. Параллельные загрузки с общими категориями блокируют строки в одном порядке, поэтому не попадают во взаимоблокировку.
- `ListBudgets` сортирует по категории (`ORDER BY category COLLATE "C"`): это тот же побайтовый порядок, что и в памяти.
- `ListTransactions` сортирует по `date DESC, id DESC`, от новых транзакций к старым. Столбец `date` хранит только день, поэтому транзакции одного дня идут от последней добавленной к первой.
- `AddTransaction` работает в одной транзакции БД: блокирует строку бюджета категории (`SELECT ... FOR UPDATE`), считает траты за период новой транзакции и при превышении лимита возвращает `*BudgetExceededError` без вставки, иначе выполняет `INSERT ... RETURNING id`. Блокировка не даёт параллельным запросам вместе превысить лимит. Категории без бюджета не ограничены, как и раньше.
- `Validate()` по-прежнему вызывается до записи, поэтому неверные данные до БД не доходят. С ДЗ 7 он отклоняет и текст, который PostgreSQL не может сохранить: категорию длиннее 100 символов, а также категорию или описание с нулевым байтом или с неверной последовательностью UTF-8.
- Суммы и лимиты хранятся в `NUMERIC(14,2)`, поэтому с ДЗ 7 их верхняя граница 999 999 999 999.99 вместо 10¹²: большее значение не помещается в столбец.
- Столбец `date` хранит календарный день. Дата транзакции приводится к началу дня по местному времени (см. `Day`), а дата из БД читается в местном часовом поясе, поэтому транзакция не сдвигается на соседний день, в том числе в день без полуночи.
- Пустой список бюджетов и отчёт без трат возвращаются как пустые срезы, как в хранилище в памяти, поэтому в Redis кешируется `[]`, а не `null`. Траты бюджета (`Spent`) оба хранилища округляют до копеек.

## Кеш в Redis

| Ключ | Значение | TTL | Сброс |
|------|----------|-----|-------|
| `report:summary:<from>:<to>`, например `report:summary:2025-09-01:2025-09-30` | отчёт `GetReportSummary` в JSON | 30 с | по истечении TTL |
| `budgets:all` | список бюджетов в JSON | 30 с | `DEL` после каждого `SetBudget` и `LoadBudgets` |

`GetReportSummary` сначала ищет отчёт в Redis. При промахе он считает отчёт запросом `SELECT category, SUM(amount) FROM expenses WHERE date BETWEEN $1 AND $2 GROUP BY category` и сохраняет результат на 30 секунд. В лог пишется `report cache miss` или `report cache hit` с ключом. Отчёт из кеша может не учитывать транзакции, добавленные за последние 30 секунд.

Правило бюджета в `AddTransaction` читает лимит только из PostgreSQL, кеш на него не влияет. Если Redis перестал отвечать или недоступен во время работы, каждое обращение к нему прерывается не позже чем через 0.5 секунды, ошибка пишется в лог, а данные берутся из PostgreSQL. Например, после `docker compose pause redis` отчёт и список бюджетов отвечают примерно за секунду (чтение и запись кеша), сохранение бюджета за 0.5 секунды (`DEL`), а транзакции кеш не используют и отвечают как обычно. Лог Gateway при запросе отчёта:

```text
2026/11/15 19:56:16 cache: read report:summary:2025-09-01:2025-09-30 from Redis: read tcp 127.0.0.1:57452->127.0.0.1:6379: i/o timeout
2026/11/15 19:56:16 report cache miss: report:summary:2025-09-01:2025-09-30
2026/11/15 19:56:16 cache: write report:summary:2025-09-01:2025-09-30 to Redis: i/o timeout
2026/11/15 19:56:16 GET /api/reports/summary 200 1.009935875s
```

Список бюджетов в кеше тоже может отставать от базы, но не дольше TTL. Это возможно, если Redis не ответил на `DEL` после `SetBudget` или `LoadBudgets`. Ещё один случай: параллельный `ListBudgets` прочитал старый список до изменения, а записал его в кеш после `DEL`.

Ключи не содержат имени базы, поэтому ledger с разными базами PostgreSQL должен использовать разные базы Redis (`REDIS_DB`). Так сделано в тестах: база `cashapp_test` работает с Redis DB 15.

## Консольная программа

```sh
cd ledger
go run ./cmd/ledger          # данные из PostgreSQL и отчёт за текущий месяц
go run ./cmd/ledger -demo    # демо ДЗ 2-4 в памяти, база не меняется
```

Без флагов программа подключается к PostgreSQL и Redis по тем же переменным окружения, печатает бюджеты с тратами за текущий период, транзакции и отчёт за текущий месяц. Отчёт запрашивается два раза подряд, поэтому в логе видны промах и попадание в кеш. Пример после [сценария Gateway](../gateway/README.md#проверка-через-curl) и ещё одной транзакции без даты, которая получила сегодняшнюю дату:

```text
2026/11/14 15:25:17 connected to PostgreSQL at localhost:5432/cashapp
2026/11/14 15:25:17 connected to Redis at localhost:6379, db 0
2026/11/14 15:25:17 Ledger service started
Budgets (1):
CATEGORY  PERIOD   LIMIT    SPENT   LEFT
еда       2026-11  5000.00  250.00  4750.00

Transactions (3):
ID  DATE        CATEGORY   AMOUNT  DESCRIPTION
3   2026-11-14  еда        250.00  кофе
2   2025-09-12  транспорт  120.00  метро
1   2025-09-10  еда        450.00  ланч
2026/11/14 15:25:17 report cache miss: report:summary:2026-11-01:2026-11-30
2026/11/14 15:25:17 report cache hit: report:summary:2026-11-01:2026-11-30

Report from 2026-11-01 to 2026-11-30:
CATEGORY  SPENT
еда       250.00
total     250.00
```

Повторный запуск показывает те же данные: они хранятся в PostgreSQL и переживают перезапуск.

Флаг `-demo` запускает демо ДЗ 2-4 (бюджеты из `budgets.json`, правило бюджета, ошибки загрузки, проверка через интерфейс) на хранилище в памяти, чтобы не засорять базу. Его вывод приведён в [корневом README](../README.md#пример-вывода).

## Тесты

```sh
cd ledger
go test ./...
```

Без переменных окружения выполняются тесты, которым не нужен Docker: правила на хранилище в памяти, отчёт, работа после `Close`, сборка DSN, разбор `REDIS_*`, ошибки подключения и лимит запросов к зависшему серверу Redis. Тесты с PostgreSQL и Redis пропускаются с подсказкой. Для них нужна отдельная тестовая база с миграциями:

```sh
docker compose exec postgres createdb -U postgres cashapp_test   # из корня репозитория
cd ledger
export LEDGER_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable'
goose -dir migrations postgres "$LEDGER_TEST_DATABASE_URL" up
LEDGER_TEST_REDIS_ADDR=localhost:6379 go test ./...
```

Интеграционные тесты очищают таблицы тестовой базы, поэтому её имя должно оканчиваться на `_test`, иначе тесты остановятся с ошибкой. В Redis они работают с базой 15 и удаляют свои ключи до и после теста, поэтому после прогона база 15 остаётся пустой. Пароль Redis, если он нужен, задаётся в `LEDGER_TEST_REDIS_PASSWORD`.

Тесты дат проверяют разные часовые пояса через явные `time.Location`, а день без полуночи в местном поясе проверяет запуск в поясе Сантьяго: `TZ=America/Santiago go test ./...`. Остальные ожидания тестов от пояса не зависят.

| Что проверяется | Тест |
|-----------------|------|
| UPSERT бюджета и порядок категорий | `TestPostgresBudgets` |
| отказ при превышении лимита без вставки, категория без бюджета, `NULL` в `description`, порядок транзакций | `TestPostgresAddTransaction` |
| границы месяца и года | `TestPostgresBudgetPeriods` |
| из 30 параллельных `AddTransaction` проходят ровно 10, которые помещаются в лимит | `TestPostgresAddTransactionConcurrent` |
| параллельные `LoadBudgets` с общими категориями в разном порядке проходят без взаимоблокировки | `TestPostgresLoadBudgetsConcurrent` |
| данные сохраняются после переподключения | `TestPostgresDataSurvivesReconnect` |
| отчёт в обоих хранилищах: дни на границах периода входят, соседние нет, суммы округлены, категории в побайтовом порядке | `TestPostgresReportSummary` |
| пустой список бюджетов и отчёт без трат дают пустые срезы из PostgreSQL и из кеша | `TestPostgresEmptyResults` |
| траты бюджета округляются до копеек в обоих хранилищах | `TestSpentIsRounded` |
| транзакции и отчёт в день без полуночи в обоих хранилищах | `TestDayWithoutMidnight` |
| промах, попадание и истечение TTL отчёта | `TestReportCache` |
| кеш бюджетов: старый список до истечения TTL, новый после него, сброс после `SetBudget` и `LoadBudgets` | `TestBudgetsCache` |
| работа без Redis: данные из PostgreSQL, ошибки в логе | `TestRedisFailure` |
| зависший Redis: каждый вызов ledger занимает не больше 2 секунд, ошибки кеша в логе | `TestRedisHangs` |
| отказ запуска на базе без миграций | `TestConnectWithoutTables` |
| запись и чтение JSON в Redis | `TestCache` в `internal/cache` |
| каждый запрос к зависшему Redis завершается не позже чем через 1 секунду, без Docker | `TestHungServer` в `internal/cache` |
| ошибка разбора строки подключения не содержит пароль, без Docker | `TestTargetError` в `internal/db` |

### Покрытие (тег hw7)

| Пакет | С PostgreSQL и Redis | Без них |
|-------|----------------------|---------|
| `ledger` | 93.0% | 57.3% |
| `ledger/cmd/ledger` | 43.0% | 43.0% |
| `ledger/internal/cache` | 100% | 81.4% |
| `ledger/internal/db` | 88.9% | 88.9% |
| Модуль целиком | 82.3% | 57.4% |

Без тестов остаются `main`, чтение данных в консольной программе и демо: их проверяет запуск программы. Также не покрыты ветки ошибок SQL-запросов после подключения: обрыв связи с PostgreSQL посреди операции, ошибки чтения строк и фиксации транзакции.

## Известные уязвимости зависимостей

`govulncheck ./...` (golang.org/x/vuln v1.1.4, проверка 15.11.2026) находит в зависимостях:
- GO-2026-5970 и GO-2026-6629 в `golang.org/x/text v0.21.0`. pgx вызывает x/text только для нормализации пароля при SCRAM-аутентификации, на вход идёт пароль из конфигурации, а не данные запросов.
- GO-2026-5004 в `github.com/jackc/pgx/v5 v5.7.4`: при подстановке параметров на стороне клиента pgx не распознаёт строки в долларовых кавычках (`$$...$$`). Такая подстановка используется только в режиме `default_query_exec_mode=simple_protocol`, а запросы без параметров подставлять нечего. Ledger этот режим не включает, задавать его в `DATABASE_URL` не нужно, а в запросах Ledger нет строк в долларовых кавычках.

Исправленные версии (x/text v0.39.0 и новее, pgx v5.9.2) требуют Go 1.25, а домашние задания совместимы с Go 1.22. Поэтому зависимости закреплены на последних версиях, которые работают с Go 1.22: pgx v5.7.4 и go-redis v9.18.0. Остальные находки относятся к стандартной библиотеке локального Go и устраняются обновлением его патч-версии.
