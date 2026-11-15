# Gateway

HTTP-шлюз проекта: `GET /ping` и REST API для транзакций, бюджетов и отчёта под префиксом `/api`. Gateway импортирует пакет [ledger](../ledger/README.md), поэтому бизнес-логика и подключения к PostgreSQL и Redis работают в процессе Gateway.

## Таймауты, ДЗ 9

Все маршруты получают контекст через middleware `Timeout` с `context.WithTimeout`. По умолчанию срок запроса равен 2 секундам; более ранний дедлайн или отмена клиента сохраняются. Настройка из каталога `gateway`:

```sh
go run . -request-timeout 50ms    # короткий таймаут для проверки
go run .                        # стандартные 2 секунды
```

Флаг должен быть положительным, например `50ms` или `2s`. Внутренние операции Ledger дополнительно ограничены 5 секундами. Сетевые ReadTimeout и WriteTimeout сервера равны 10 секундам; увеличение флага не снимает эти ограничения.

Таймаут и отмена в любом API-методе дают один ответ:

```text
504 {"error":"request timed out"}
```

Обработчики проверяют контекст также после успешного возврата сервиса: дедлайн мог истечь во время обращения к кешу. Ответ 504 после записи не гарантирует откат уже завершенной транзакции; состояние можно проверить запросом списка.

Если клиент уже разорвал соединение, получить этот ответ он не сможет, но работа прекращается, а отмена видна в логе. Middleware не запускает обработчик в отдельной горутине: сервисы и SQL завершаются по контексту, и Gateway отвечает после их завершения. Чтение тела запроса ограничено сетевым дедлайном, чтобы незаконченный JSON не удерживал обработчик. Обычный неверный JSON или дата дают 400, ошибка хранилища без отмены 500.

При промахе кеша отчет считает отдельная горутина на каждую категорию. Heartbeat раз в 400 мс пишет ход расчета; перед ответом сервис дожидается всех работников и останавливает heartbeat. При попадании в кеш расчет не нужен. Пример ответа:

```json
{"from":"2026-11-01","to":"2026-11-30","categories":[{"category":"еда","amount":450},{"category":"транспорт","amount":120}],"total":570}
```

### Воспроизводимая проверка отмены

Используется отдельная база `cashapp_test` стенда из `compose.yaml` и Redis DB 15. В это время интеграционные тесты не запускаются. Подготовка из корня репозитория; если база уже существует, команду `createdb` пропустить:

```sh
docker compose up -d --wait
docker compose exec postgres createdb -U postgres cashapp_test
goose -dir ledger/migrations postgres 'postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable' up
cd gateway
DATABASE_URL='postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable' \
REDIS_DB=15 go run . -request-timeout 50ms
```

В другом терминале, из корня репозитория, удалить только ключ проверяемого отчета и открыть psql:

```sh
docker compose exec redis redis-cli -n 15 DEL report:summary:2026-11-01:2026-11-30
docker compose exec postgres psql -U postgres -d cashapp_test
```

В psql выполнить и оставить транзакцию открытой:

```sql
BEGIN;
LOCK TABLE expenses IN ACCESS EXCLUSIVE MODE;
```

В третьем терминале выполнить:

```sh
curl -i 'http://localhost:8080/api/reports/summary?from=2026-11-01&to=2026-11-30'
```

Ожидается 504 примерно через 50 мс. В логе есть `report cache miss`, `report heartbeat stopped` и `report calculation canceled: context deadline exceeded`. Для наблюдения периодических сообщений повторить с обычным `go run .`: до 504 за 2 секунды появятся сообщения `report calculation in progress`.

Завершить блокирующую транзакцию командой `ROLLBACK;`, выйти через `\q` и повторить curl. Ожидается 200, на пустой базе `categories: []` и `total: 0`. Затем вернуть обычный запуск без флага. Сам по себе таймаут 50 мс не гарантирует ошибку на быстрой БД, поэтому для проверки используется контролируемая блокировка и промах кеша.

## Что изменилось в ДЗ 8

`main.go` вызывает `ledger.New(ctx)` и передает готовый `service.Service` в `api.NewRouter(svc, timeout)`. После завершения HTTP-запросов отложенный вызов закрывает ресурсы. При ошибке фабрики Gateway пишет ее в лог и завершается. Настройки БД, SQL, Redis и миграции HTTP-слою неизвестны.

Обработчики вызывают методы экземпляра сервиса с `r.Context()`. Разбор JSON и строковых дат остается в Gateway, проверка доменных значений и бюджета выполняется в сервисе. Ошибки валидации дают прежний 400, превышение бюджета 409, ошибка хранения 500 с общим сообщением. DTO, маршруты и формат ответов ДЗ 7 сохранены. Тесты получают отдельный `ledger.NewMemory()` или подменяемый сервис.

## Что изменилось в ДЗ 7, историческая справка

Эндпоинты ДЗ 5 остались прежними и вызывают те же функции ledger, формат JSON не изменился. Но теперь Ledger хранит транзакции и бюджеты в PostgreSQL, поэтому данные переживают перезапуск, а отчёты кешируются в Redis на 30 секунд.

Изменения в коде Gateway минимальные:
- в ДЗ 7 `main.go` до открытия порта вызывал `ledger.Connect`; с ДЗ 8 это `ledger.New(ctx)`. Если PostgreSQL или Redis недоступен или к базе не применены миграции, Gateway пишет ошибку в лог и завершается с кодом 1. По `Ctrl+C` или SIGTERM он дожидается текущих запросов и закрывает соединения, а повторный `Ctrl+C` завершает процесс сразу;
- обработчики списков отвечают 500, если хранилище вернуло ошибку, например при потере связи с PostgreSQL;
- добавлен `GET /api/reports/summary`. Без него нельзя выполнить проверку из условия, где отчёт запрашивается через curl два раза подряд.

Ответы отличаются от ДЗ 6 в трёх случаях:
- транзакции одного дня в `GET /api/transactions` идут от последней добавленной к первой. Столбец `DATE` хранит только день, а в ДЗ 6 транзакция без даты хранила время добавления и поэтому стояла выше транзакций с той же датой, даже добавленных позже;
- категория длиннее 100 символов, а также категория или описание с символом `\u0000` дают 400 вместо 201. PostgreSQL не хранит нулевой байт в `TEXT`, а категория в несколько тысяч символов не помещается в индекс, поэтому длина категории ограничена;
- дата `0001-01-01` даёт `{"error":"invalid date 0001-01-01: out of range"}` в любом часовом поясе, а в ДЗ 6 текст этой ошибки зависел от пояса. Код ответа прежний, 400.

## Запуск

Из корня репозитория:

```sh
docker compose up -d --wait   # PostgreSQL и Redis
cd ledger
goose -dir migrations postgres 'postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable' up
cd ../gateway
DATABASE_URL='postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable' \
REDIS_ADDR=localhost:6379 REDIS_DB=0 REDIS_PASSWORD= \
go run .                      # другой адрес: go run . -addr :9090
```

Для стенда из `compose.yaml` значения переменных по умолчанию подходят, и достаточно `go run .`. Создание базы, миграции и все переменные окружения (`DATABASE_URL` или `DB_*`, `REDIS_ADDR`, `REDIS_DB`, `REDIS_PASSWORD`) описаны в [README Ledger](../ledger/README.md).

При запуске в логе видны оба подключения:

```text
2026/11/14 15:25:07 connected to PostgreSQL at localhost:5432/cashapp
2026/11/14 15:25:07 connected to Redis at localhost:6379, db 0
2026/11/14 15:25:07 Gateway service listening on [::]:8080
```

## Эндпоинты

| Метод и путь | Тело запроса | Ответ |
|--------------|--------------|-------|
| `GET /ping` | нет | 200 `{"message":"pong"}` |
| `POST /api/transactions` | `{"amount", "category", "description", "date"}` | 201 и созданная транзакция с `id`; 400; 409 |
| `GET /api/transactions` | нет | 200 и массив транзакций от новых к старым, `[]`, если их нет |
| `POST /api/budgets` | `{"category", "limit", "period"}` | 201 и сохранённый бюджет, новый или обновлённый; 400 |
| `GET /api/budgets` | нет | 200 и массив бюджетов по алфавиту категорий, `[]`, если их нет |
| `GET /api/reports/summary?from=YYYY-MM-DD&to=YYYY-MM-DD` | нет | 200 и траты по категориям за период; 400 |

Поля запросов, коды ошибок и формат `{"error":"описание"}` описаны в [разделе ДЗ 5 корневого README](../README.md#дз-5-веб-приложения-rest-api-в-gateway). С ДЗ 7 верхняя граница `amount` и `limit` равна 999999999999.99, чтобы значение помещалось в `NUMERIC(14,2)`. Категория ограничена 100 символами, а в категории и описании нельзя передать символ `\u0000`:

```text
400 {"error":"invalid category: must be at most 100 characters, got 101"}
400 {"error":"invalid category: must be valid UTF-8 without NUL characters"}
400 {"error":"invalid description: must be valid UTF-8 without NUL characters"}
```

### Отчёт

`from` и `to` задают первый и последний день отчёта, оба включаются. Дни считаются в местном часовом поясе Gateway, допустимы дни с 0001-01-02 по 9999-12-30. Без обоих параметров отчёт охватывает текущий месяц. Если задан только один, отчёт не выходит за месяц этой даты: `from=2025-09-10` означает период по 2025-09-30, а `to=2025-09-10` период с 2025-09-01. В крайних месяцах период не выходит и за допустимые дни: `to=0001-01-15` означает период с 0001-01-02, а `from=9999-12-15` период по 9999-12-30. Неверная дата, дата вне диапазона или `from` позже `to` дают 400:

```text
400 {"error":"invalid date \"bad\": must be YYYY-MM-DD"}
400 {"error":"invalid date 0000-01-01: must be from 0001-01-02 to 9999-12-30"}
400 {"error":"invalid date 0001-01-01: out of range"}
400 {"error":"invalid report range: from 2025-09-30 is after to 2025-09-01"}
```

Дата транзакции, как и в ДЗ 4, проверяется по моменту начала дня в UTC. Поэтому восточнее UTC транзакция на 0001-01-02 отклоняется, а на 9999-12-31 принимается, хотя отчёт этот день не охватывает.

В ответе категории идут по алфавиту, категории без трат за период не выводятся. Если трат нет, приходит `"categories":[]` и `"total":0`. Ledger кеширует отчёт в Redis на 30 секунд под ключом `report:summary:<from>:<to>`, поэтому новая транзакция может появиться в отчёте с задержкой до 30 секунд.

## Проверка через curl

Gateway запущен командой выше на пустой базе, запросы выполняются из другого терминала:

```sh
curl -i -X POST http://localhost:8080/api/budgets \
  -H "Content-Type: application/json" \
  -d '{"category":"еда","limit":5000,"period":"month"}'

curl -i -X POST http://localhost:8080/api/transactions \
  -H "Content-Type: application/json" \
  -d '{"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}'

curl -i -X POST http://localhost:8080/api/transactions \
  -H "Content-Type: application/json" \
  -d '{"amount":4600,"category":"еда","date":"2025-09-11"}'

curl -i -X POST http://localhost:8080/api/transactions \
  -H "Content-Type: application/json" \
  -d '{"amount":0,"category":"еда"}'

curl -i -X POST http://localhost:8080/api/transactions \
  -H "Content-Type: application/json" \
  -d '{"amount":120,"category":"транспорт","description":"метро","date":"2025-09-12"}'

curl -i http://localhost:8080/api/transactions
curl -i http://localhost:8080/api/budgets
```

Ответы по порядку (статус и тело):

```text
201 {"category":"еда","limit":5000,"period":"month"}
201 {"id":1,"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}
409 {"error":"budget exceeded"}
400 {"error":"invalid amount 0: must be from 0.01 to 999999999999.99"}
201 {"id":2,"amount":120,"category":"транспорт","description":"метро","date":"2025-09-12"}
200 [{"id":2,"amount":120,"category":"транспорт","description":"метро","date":"2025-09-12"},{"id":1,"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}]
200 [{"category":"еда","limit":5000,"period":"month"}]
```

Третий запрос получил 409: 450 + 4600 больше лимита 5000 на сентябрь 2025 года.

### Данные в PostgreSQL

```sh
docker compose exec postgres psql -U postgres -d cashapp -c 'SELECT * FROM budgets' -c 'SELECT * FROM expenses'
```

```text
 id | category | limit_amount | period
----+----------+--------------+--------
  1 | еда      |      5000.00 | month
(1 row)

 id | amount | category  | description |    date
----+--------+-----------+-------------+------------
  1 | 450.00 | еда       | ланч        | 2025-09-10
  2 | 120.00 | транспорт | метро       | 2025-09-12
(2 rows)
```

Бюджет записан в `budgets`, принятые транзакции в `expenses`. Отклонённые с кодами 409 и 400 в таблицу не попали.

### Перезапуск

Остановить Gateway через `Ctrl+C` и снова выполнить `go run .`. Те же запросы возвращают те же данные:

```sh
curl -s http://localhost:8080/api/transactions
curl -s http://localhost:8080/api/budgets
```

```text
[{"id":2,"amount":120,"category":"транспорт","description":"метро","date":"2025-09-12"},{"id":1,"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}]
[{"category":"еда","limit":5000,"period":"month"}]
```

Консольная программа `cd ledger && go run ./cmd/ledger` тоже показывает эти данные, см. [README Ledger](../ledger/README.md#консольная-программа).

### Отчёт два раза подряд

```sh
curl -s 'http://localhost:8080/api/reports/summary?from=2025-09-01&to=2025-09-30'
curl -s 'http://localhost:8080/api/reports/summary?from=2025-09-01&to=2025-09-30'
```

Оба ответа одинаковые:

```json
{"from":"2025-09-01","to":"2025-09-30","categories":[{"category":"еда","amount":450},{"category":"транспорт","amount":120}],"total":570}
```

Эффект кеша виден в логе Gateway: первый запрос считается в PostgreSQL (`miss`), второй берётся из Redis (`hit`) и выполняется быстрее.

### Лог

Лог Gateway после перезапуска, двух запросов списков и двух запросов отчёта:

```text
2026/11/14 15:25:08 connected to PostgreSQL at localhost:5432/cashapp
2026/11/14 15:25:08 connected to Redis at localhost:6379, db 0
2026/11/14 15:25:08 Gateway service listening on [::]:8080
2026/11/14 15:25:09 GET /api/transactions 200 6.99825ms
2026/11/14 15:25:09 GET /api/budgets 200 1.814958ms
2026/11/14 15:25:09 report cache miss: report:summary:2025-09-01:2025-09-30
2026/11/14 15:25:09 GET /api/reports/summary 200 4.834834ms
2026/11/14 15:25:09 report cache hit: report:summary:2025-09-01:2025-09-30
2026/11/14 15:25:09 GET /api/reports/summary 200 666.167µs
2026/11/14 15:25:09 Gateway service shutting down
```

Через 30 секунд ключ отчёта истекает, и следующий запрос снова даёт `report cache miss`.

## Тесты

```sh
cd gateway
go test ./...
```

Тесты HTTP-слоя в `internal/api` работают на хранилище ledger в памяти и не требуют Docker: обработчики проверяются через `httptest` вместе с маршрутами и middleware. `TestReportSummary` и `TestReportRange` проверяют отчёт и разбор параметров, включая крайние месяцы диапазона, `TestLedgerError` проверяет ответ 500 при отказе хранилища. День без полуночи проверяет запуск в поясе Сантьяго: `TZ=America/Santiago go test ./...`. Работу с PostgreSQL и Redis проверяют интеграционные тесты [Ledger](../ledger/README.md#тесты).

Покрытие (тег hw7): `gateway/internal/api` 100%, модуль целиком 87.8%. Функции `main` и `run` в `main.go` проверяет запуск Gateway по сценарию выше.

Для ДЗ 9 `TestAPITimeout` проверяет 504 при дедлайне и отмене во всех пяти операциях API, `TestTimeoutPreservesEarlierDeadline` проверяет родительский срок и уже отмененный запрос, `TestSlowRequestBodyTimeout` проверяет таймаут незаконченного тела на настоящем HTTP-соединении.
