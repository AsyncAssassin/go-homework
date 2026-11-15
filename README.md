# Язык Go — домашние задания

| ДЗ | Тема | Где лежит код |
|----|------|---------------|
| 1 | Введение в Go | `hw1/` |
| 2 | Функции: сервисы Gateway и Ledger | `gateway/`, `ledger/` |
| 3 | Ввод-вывод, обработка ошибок: бюджеты в Ledger | `ledger/` |
| 4 | ООП: валидация через интерфейс в Ledger | `ledger/` |
| 5 | Веб-приложения: REST API в Gateway | `gateway/`, `ledger/` |
| 6 | Тестирование | `gateway/`, `ledger/` |
| 7 | Базы данных: PostgreSQL и Redis | `ledger/`, `gateway/`, `compose.yaml` |
| 8 | Архитектура: домен, репозитории, сервис и внедрение зависимостей | `ledger/`, `gateway/` |
| 9 | Конкурентный отчет, контекст, HTTP-таймауты и heartbeat | `ledger/`, `gateway/` |

Состояние репозитория на момент сдачи каждого ДЗ отмечается тегом `hwN`. Разделы ДЗ 1-7 описывают развитие проекта; с ДЗ 8 пакетные операции заменены методами `service.Service`, модели перенесены в `ledger/domain`. Текущая структура описана в [README Ledger](ledger/README.md#устройство).

## Требования

- Go 1.22+ (проверено на 1.22.12 и 1.26.5)
- С ДЗ 7 для Gateway и Ledger: Docker с Compose (PostgreSQL и Redis) и goose v3.30.1 для миграций

Каждая папка — отдельный Go-модуль, команды ниже запускаются из её каталога.

## ДЗ 1. Введение в Go

Программа выводит:
- имя пользователя из переменной окружения `USER` (на Windows — `USERNAME`);
- аргументы командной строки;
- версию Go через `runtime.Version()`.

```sh
cd hw1
go run . hello "big world"
```

```text
User: alice
Arguments (2):
  1: "hello"
  2: "big world"
Go version: go1.26.5
```

## ДЗ 2. Функции: сервисы Gateway и Ledger

### Gateway — HTTP-шлюз

HTTP-сервер на порту 8080, `GET /ping` отвечает со статусом 200. С ДЗ 5 все ответы Gateway в JSON, поэтому вместо текста `pong` из ДЗ 2 приходит `{"message":"pong"}`.

```sh
cd gateway
go run .              # другой адрес: go run . -addr :9090
```

Проверка из другого терминала:

```sh
curl -i http://localhost:8080/ping
```

```text
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
Content-Length: 19

{"message":"pong"}
```

Остановить сервер — `Ctrl+C`. Если порт занят, сервис сразу завершится с ошибкой `address already in use`.

### Ledger — бизнес-логика

Хранит транзакции в памяти:
- `AddTransaction(tx Transaction) error` — проверяет и добавляет транзакцию: сумма от 0.01 до 10¹², категория не пустая;
- `ListTransactions() []Transaction` — возвращает копию списка.

С ДЗ 5 ledger стал пакетом, который импортирует Gateway, демо переехало в `ledger/cmd/ledger`, а `AddTransaction` возвращает ещё и сохранённую транзакцию. Запуск демо: `cd ledger && go run ./cmd/ledger -demo` (до ДЗ 7 без флага). Оно показывает также бюджеты из ДЗ 3 и проверку через интерфейс из ДЗ 4, примеры вывода приведены в их разделах. С ДЗ 7 демо работает в памяти, как раньше, а основное хранилище ledger находится в PostgreSQL, и верхняя граница суммы равна 999 999 999 999.99. Состояние на момент сдачи ДЗ 2 сохранено в теге [`hw2`](https://github.com/AsyncAssassin/go-homework/tree/hw2).

## ДЗ 3. Ввод-вывод и обработка ошибок: бюджеты в Ledger

```sh
cd ledger
go run ./cmd/ledger -demo   # другой файл бюджетов: go run ./cmd/ledger -demo -budgets path/to/file.json
```

С ДЗ 7 демо запускается флагом `-demo`, а без флага программа показывает данные из PostgreSQL (см. [README Ledger](ledger/README.md#консольная-программа)).

Сервис задаёт начальные бюджеты в коде через `SetBudget`, затем загружает `budgets.json` из текущего каталога (`os.Open` + `bufio.NewReader`) и прогоняет демо-сценарии: транзакции в пределах лимита, сверх лимита, без бюджета, с нулевой суммой и загрузку бюджетов из битых источников.

Файл бюджетов необязателен: если его нет (например, при запуске из корня репозитория), сервис предупреждает об этом и работает на бюджетах из кода. Если файл есть, но его не удаётся прочитать или разобрать, сервис завершается с ошибкой и кодом 1.

### Что реализовано

- `Budget{Category, Limit, Period}` и хранилище бюджетов `map[string]Budget` (ключ — категория).
- `Period` — усложнение из задания, поле необязательное:
  - `"month"` — лимит действует отдельно на каждый календарный месяц;
  - `"year"` — отдельно на каждый календарный год;
  - пусто или не указано — один лимит на все траты категории.
- `SetBudget(b Budget) error` добавляет бюджет или целиком заменяет существующий, вместе с периодом. С ДЗ 5 функция возвращает ещё и сохранённый бюджет: `(Budget, error)`.
- `AddTransaction` проверяет бюджет категории: если траты за период новой транзакции вместе с ней больше лимита, возвращается ошибка `*BudgetExceededError` (`errors.Is(err, ErrBudgetExceeded)`), и транзакция не сохраняется. Ровно до лимита — можно; категории без бюджета не ограничены.
- Период определяется по дате транзакции, а если дата не указана — по текущей. Месяцы и годы считаются в локальном часовом поясе, поэтому каждая транзакция попадает ровно в один период, в каком бы поясе ни была задана её дата.
- Суммы и лимиты округляются до копеек и сравниваются в копейках, чтобы не мешала погрешность float. Регистр и пробелы по краям в категориях и периодах не важны: «Еда » и «еда» — одна категория, `"Month"` — то же, что `"month"`.
- `LoadBudgets(r io.Reader) error` читает JSON-массив бюджетов и применяет его так же, как `SetBudget`, только если весь файл корректный. Ошибки чтения и разбора возвращаются с понятным описанием.

### Файл бюджетов `ledger/budgets.json`

```json
[
  {"category": "еда", "limit": 5000, "period": "month"},
  {"category": "транспорт", "limit": 2500, "period": "month"},
  {"category": "развлечения", "limit": 30000, "period": "year"},
  {"category": "ремонт", "limit": 100000}
]
```

### Пример вывода

```text
Ledger service started
Budgets loaded from budgets.json

Budgets (4):
CATEGORY     PERIOD    LIMIT      SPENT  LEFT
еда          2026-10   5000.00    0.00   5000.00
развлечения  2026      30000.00   0.00   30000.00
ремонт       all time  100000.00  0.00   100000.00
транспорт    2026-10   2500.00    0.00   2500.00

Adding transactions:
  added    "продукты на неделю" 1250.50 (еда)
  added    "проездной" 2000.00 (транспорт)
  added    "кафе" 3500.00 (еда)
  rejected "ресторан": budget exceeded for "еда" in 2026-10: spent 4750.50 + new 500.00 > limit 5000.00, only 249.50 left
  added    "хлеб и молоко" 249.50 (еда)
  added    "ресторан в прошлом месяце" 500.00 (еда)
  added    "лекарства" 700.00 (здоровье)
  rejected "пустой чек": invalid amount 0: must be from 0.01 to 999999999999.99

Budget loading errors:
  broken JSON: load budgets: decode JSON: unexpected EOF
  wrong type: load budgets: decode JSON: json: cannot unmarshal string into Go struct field Budget.limit of type float64
  invalid limit: load budgets: budget #2: invalid limit -5 for "такси": must be from 0.01 to 999999999999.99
  invalid period: load budgets: budget #1: invalid period "week" for "кафе": must be "month", "year" or empty
  missing file: load budgets: open no-such-dir/budgets.json: no such file or directory

Transactions (6):
ID  DATE        CATEGORY   AMOUNT   DESCRIPTION
6   2026-10-03  здоровье   700.00   лекарства
4   2026-10-03  еда        249.50   хлеб и молоко
3   2026-10-03  еда        3500.00  кафе
2   2026-10-03  транспорт  2000.00  проездной
1   2026-10-03  еда        1250.50  продукты на неделю
5   2026-09-30  еда        500.00   ресторан в прошлом месяце

Budgets (4):
CATEGORY     PERIOD    LIMIT      SPENT    LEFT
еда          2026-10   5000.00    5000.00  0.00
развлечения  2026      30000.00   0.00     30000.00
ремонт       all time  100000.00  0.00     100000.00
транспорт    2026-10   2500.00    2000.00  500.00
```

Отклонённые транзакции («ресторан», «пустой чек») в список не попали. «Ресторан в прошлом месяце» добавлен, хотя лимит на еду в текущем месяце исчерпан: у прошлого месяца свой лимит. После неудачных загрузок бюджеты не изменились («кафе» и «такси» не появились). С ДЗ 5 транзакции выводятся от новых к старым, при одинаковой дате по убыванию ID. Даты и периоды в выводе зависят от дня запуска.

## ДЗ 4. ООП: валидация через интерфейс в Ledger

```sh
cd ledger
go run ./cmd/ledger -demo
```

Проверка данных отделена от бизнес-логики интерфейсом `Validatable` (`ledger/domain/validate.go`):

```go
type Validatable interface {
	Validate() error
}
```

### Что реализовано

- `Transaction.Validate()`: сумма от 0.01 до 999 999 999 999.99, до ДЗ 7 до 10¹² (ноль, отрицательные суммы, NaN и бесконечность не проходят), категория не пустая, заданная дата от 2 января 1 года до 30 декабря 9999 года по UTC. Запас в сутки нужен, чтобы в любом часовом поясе дата записывалась как `YYYY-MM-DD` с годом от 1 до 9999, а результат проверки зависел только от момента времени. Пустая дата допустима: при добавлении подставляется текущая. С ДЗ 7 категория не длиннее 100 символов, а в категории и описании нет нулевого байта и неверных последовательностей UTF-8: такой текст PostgreSQL не сохранит.
- `Budget.Validate()`: категория не пустая, с ДЗ 7 к ней применяются те же правила длины и текста, лимит от 0.01 до 999 999 999 999.99 (до ДЗ 7 до 10¹²), период `month`, `year` или пусто. Граница уменьшилась, чтобы значение помещалось в столбец `NUMERIC(14,2)`.
- `Validate()` только возвращает ошибку и ничего не логирует, результат печатает вызывающий код.
- `AddTransaction`, `SetBudget` и `LoadBudgets` вызывают `Validate()` до сохранения. Если он вернул ошибку, объект не добавляется, а ошибка возвращается вызывающему коду. Проверки бюджетов из ДЗ 3 работают как раньше.
- `CheckValid(v Validatable) error` вызывает `v.Validate()` и одинаково работает с транзакциями, бюджетами и указателями на них. Для `nil`, в том числе для nil-указателя вроде `(*Transaction)(nil)`, возвращает ошибку `nothing to validate`, а не панику.

### Пример вывода

Вывод из ДЗ 3 не изменился, в конец добавлен раздел, где `CheckValid` проверяет разные типы через интерфейс. Первый столбец (`%T`) показывает, какой тип лежит в интерфейсе. С ДЗ 8 типы находятся в пакете `domain`, поэтому вывод показывает `domain.Transaction` и `domain.Budget`. В ДЗ 5-7 это были типы пакета `ledger`, в ДЗ 4 пакета `main`:

```text
Validation through the Validatable interface:
  domain.Transaction  450.00 "ланч" (еда)               ok
  domain.Transaction  -100.00 "возврат" (еда)           invalid: invalid amount -100: must be from 0.01 to 999999999999.99
  domain.Transaction  300.00 "такси" (no category)      invalid: category must not be empty
  domain.Transaction  50.00 "кофе" (еда) on 0000-03-01  invalid: invalid date 0000-03-01: must be from 0001-01-02 to 9999-12-30 UTC
  domain.Budget       кафе 3000.00 a month              ok
  domain.Budget       кафе 0.00                         invalid: invalid limit 0 for "кафе": must be from 0.01 to 999999999999.99
  domain.Budget       no category 1000.00               invalid: category must not be empty
```

Табличные тесты `Validate()` и `CheckValid` находятся в `ledger/domain/domain_test.go` (до ДЗ 8 в `ledger/ledger_test.go`).

## ДЗ 5. Веб-приложения: REST API в Gateway

```sh
cd gateway
go run .              # другой адрес: go run . -addr :9090
```

Gateway импортирует модуль `ledger` из этого же репозитория: в `gateway/go.mod` указаны `require` и `replace github.com/AsyncAssassin/go-homework/ledger => ../ledger`, go.work не нужен. В ДЗ 5 хранилище было в памяти, как в ДЗ 2-4, и данные пропадали после остановки сервера. С ДЗ 7 они хранятся в PostgreSQL, поэтому перед запуском нужны PostgreSQL, Redis и миграции, см. [ДЗ 7](#дз-7-базы-данных-postgresql-и-redis). Состояние на момент сдачи ДЗ 5 сохранено в теге [`hw5`](https://github.com/AsyncAssassin/go-homework/tree/hw5).

HTTP-слой находится в `gateway/internal/api`:
- `dto.go`: `CreateTransactionRequest`, `TransactionResponse`, `CreateBudgetRequest`, `BudgetResponse` и функции-преобразователи между DTO и `ledger.Transaction`, `ledger.Budget`;
- `handlers.go`: обработчики, которые проверяют данные через `Validate()` и вызывают `ledger.AddTransaction`, `ledger.ListTransactions`, `ledger.SetBudget`, `ledger.ListBudgets`;
- `router.go`: маршруты, сгруппированные под префиксом `/api`;
- `middleware.go`: логирование запросов, перехват паник и единый заголовок `Content-Type`;
- `respond.go`: запись JSON-ответов и разбор тела запроса.

### Эндпоинты

| Метод и путь | Тело запроса | Ответ |
|--------------|--------------|-------|
| `GET /ping` | нет | 200 `{"message":"pong"}` |
| `POST /api/transactions` | `{"amount", "category", "description", "date"}` | 201 и созданная транзакция с `id`; 400; 409 |
| `GET /api/transactions` | нет | 200 и массив транзакций от новых к старым, `[]`, если их нет |
| `POST /api/budgets` | `{"category", "limit", "period"}` | 201 и сохранённый бюджет, новый или обновлённый; 400 |
| `GET /api/budgets` | нет | 200 и массив бюджетов по алфавиту категорий, `[]`, если их нет |
| `GET /api/reports/summary?from=YYYY-MM-DD&to=YYYY-MM-DD` | нет | с ДЗ 7: 200 и траты по категориям за период; 400, см. [README Gateway](gateway/README.md#отчёт) |

Поля запросов:
- `amount` и `limit`: число от 0.01 до 999 999 999 999.99 (до ДЗ 7 до 10¹²), округляется до копеек;
- `category`: непустая строка, регистр и пробелы по краям не важны;
- `description`: необязательно;
- `date`: `YYYY-MM-DD`, необязательно, без даты берётся сегодняшняя. Дата считается в локальном часовом поясе, как и периоды бюджетов;
- `period`: необязательно, `month`, `year` или пусто (один лимит на всё время), как в ДЗ 3.

### Ответы и ошибки

Все ответы, в том числе ошибки, приходят с заголовком `Content-Type: application/json; charset=utf-8`. Ошибка всегда имеет вид `{"error":"описание"}`:

| Статус | Когда |
|--------|-------|
| 400 | данные не прошли `Validate()`, битый JSON, неизвестное поле, поле не того типа, дата не в формате `YYYY-MM-DD` |
| 409 | транзакция не помещается в бюджет категории: ровно `{"error":"budget exceeded"}` |
| 404 | неизвестный путь |
| 405 | метод не поддерживается, допустимые методы перечислены в заголовке `Allow` |
| 413 | тело запроса больше 1 МБ |
| 500 | внутренняя ошибка: клиент получает `{"error":"internal server error"}`, подробности пишутся в лог |

Проверка бюджета и добавление транзакции выполняются в ledger под одной блокировкой, поэтому параллельные запросы не превышают лимит вместе. С ДЗ 7 ту же роль играет транзакция PostgreSQL, которая блокирует строку бюджета.

### Проверка через curl

Сервер запущен командой выше, запросы выполняются из другого терминала:

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

curl -i http://localhost:8080/api/transactions
curl -i http://localhost:8080/api/budgets
```

Ответы по порядку (статус и тело):

```text
201 {"category":"еда","limit":5000,"period":"month"}
201 {"id":1,"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}
409 {"error":"budget exceeded"}
400 {"error":"invalid amount 0: must be from 0.01 to 999999999999.99"}
200 [{"id":1,"amount":450,"category":"еда","description":"ланч","date":"2025-09-10"}]
200 [{"category":"еда","limit":5000,"period":"month"}]
```

Третий запрос получил 409: 450 + 4600 больше лимита 5000 на сентябрь 2025 года.

### Лог запросов

Middleware `Logging` пишет в консоль метод, путь, статус и длительность каждого запроса:

```text
2026/11/14 14:29:51 Gateway service listening on [::]:8080
2026/11/14 14:29:52 POST /api/budgets 201 185.958µs
2026/11/14 14:29:52 POST /api/transactions 201 51µs
2026/11/14 14:29:52 POST /api/transactions 409 42.125µs
2026/11/14 14:29:52 POST /api/transactions 400 20µs
2026/11/14 14:29:52 GET /api/transactions 200 22.125µs
2026/11/14 14:29:52 GET /api/budgets 200 16.542µs
```

С ДЗ 7 перед этими строками лог показывает подключения к PostgreSQL и Redis, а запросы отчёта отмечаются строками `report cache miss` и `report cache hit` (пример в [README Gateway](gateway/README.md#лог)).

Тесты HTTP-слоя находятся в `gateway/internal/api`: обработчики проверяются через `httptest` вместе с маршрутами и middleware.

## ДЗ 6. Тестирование

```sh
cd ledger                                    # затем то же в gateway
go test ./...
go test ./... -cover -coverprofile=cover.out
go tool cover -html=cover.out -o cover.html  # отчёт по строкам, открывается в браузере
go tool cover -func=cover.out | tail -1      # итоговый процент
```

В корне репозитория нет `go.mod`, поэтому тесты и покрытие запускаются в каталоге каждого модуля. Файлы `cover.out` и `cover.html` в Git не попадают.

С ДЗ 7 часть тестов ledger работает с PostgreSQL и Redis и без них пропускается, порядок запуска описан в [README Ledger](ledger/README.md#тесты).

### Покрытие тестами

Измерено командами выше на момент сдачи ДЗ 6, тег [`hw6`](https://github.com/AsyncAssassin/go-homework/tree/hw6). Покрытие с ДЗ 7 приведено в [README Ledger](ledger/README.md#покрытие-тег-hw7) и [README Gateway](gateway/README.md#тесты).

| Пакет | Покрытие |
|-------|----------|
| `ledger` | 100% |
| `ledger/cmd/ledger` | 53.6% |
| `gateway/internal/api` | 97.4% |
| `gateway` (`main.go`) | 0% |
| Модуль `ledger` целиком | 81.5% |
| Модуль `gateway` целиком | 92.1% |

Без тестов остаются функции `main` и печать консольного демо: их проверяет запуск программ (`go run ./cmd/ledger -demo`, `go run .` и curl из раздела ДЗ 5). В `gateway/internal/api` на момент ДЗ 6 не были покрыты ответы 500 на прочие ошибки ledger: обработчики проверяют данные через `Validate()` до вызова ledger, а хранилище в памяти других ошибок не возвращает. С ДЗ 7 эти ветки проверяет `TestLedgerError`. Также на момент ДЗ 6 не был покрыт отказ для даты `0001-01-01`, который срабатывал только в поясе UTC. С ДЗ 7 он срабатывает в любом поясе, его проверяют `TestCreateTransaction` и `TestReportSummary`.

### Что проверяют тесты

| Требование | Тест |
|------------|------|
| Изоляция тестовых данных | с ДЗ 8 каждый сценарий получает отдельный `ledger.NewMemory()`; `TestMemoryServicesAreIndependent` проверяет независимость |
| Табличный тест `Transaction.Validate()` | `TestTransactionValidate` в `ledger/domain/domain_test.go`: валидная транзакция, нулевая и отрицательная сумма, пустая категория, границы суммы и даты |
| Табличный тест `Budget.Validate()` | `TestBudgetValidate` там же: положительный, нулевой и отрицательный лимит, пустая категория, период |
| Превышение бюджета | `TestBudgetRule` в `ledger/ledger_test.go`: бюджет «еда» 5000, транзакция на 4500 проходит, на 600 получает `ErrBudgetExceeded`, длина `ListTransactions()` не меняется. Граничные случаи в `TestAddTransactionBudget` (`ledger/budget_test.go`) |
| HTTP `/api/budgets` | `TestBudgetsFlow` в `gateway/internal/api/handlers_test.go`: подтесты `create` (201), `list` (200), `bad_json` и `zero_limit` (400 и `{"error":"..."}`), `update`. Отрицательный лимит и другие ошибки в таблице `TestSetBudget` |
| HTTP `/api/transactions` | `TestTransactionsFlow` там же: `budget`, `ok` (201), `list` (200), `exceeded` (409 и `{"error":"budget exceeded"}`), `bad_json` (400), отклонённые запросы не сохраняются. Ошибки валидации в таблице `TestCreateTransaction` |
| Заголовок и формат ответов | каждый HTTP-тест сверяет статус и `Content-Type: application/json; charset=utf-8`, почти все случаи сравнивают и тело ответа целиком |

Цепочки `TestBudgetsFlow` и `TestTransactionsFlow` отправляют запросы настоящему HTTP-серверу `httptest.NewServer` с роутером и middleware, каждый шаг в своём `t.Run`. Остальные тесты обработчиков (`TestCreateTransaction`, `TestSetBudget` и другие) разбирают отдельные случаи таблицами через `httptest.NewRecorder`.

Табличные unit-тесты `Validate`, `CheckValid` и преобразователей DTO не трогают общие данные, поэтому они и их подтесты помечены `t.Parallel()`. Тесты, которые меняют хранилище ledger или стандартный логгер, идут последовательно. Независимость от порядка запуска проверяет `go test -race -shuffle=on -count=3 ./...`.

## ДЗ 7. Базы данных: PostgreSQL и Redis

Ledger хранит транзакции и бюджеты в PostgreSQL вместо памяти, а отчёты кеширует в Redis на 30 секунд. Gateway отвечает на те же HTTP-эндпоинты, что и в ДЗ 5, но данные теперь читаются из PostgreSQL и пишутся туда же, поэтому переживают перезапуск.

```sh
docker compose up -d --wait   # PostgreSQL 16 и Redis 7 из compose.yaml
cd ledger
goose -dir migrations postgres 'postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable' up
cd ../gateway
go run .
```

Подробности:
- [README Ledger](ledger/README.md): создание базы и миграции goose (up и down), схема, переменные `DATABASE_URL`, `DB_*` и `REDIS_*`, пул соединений, хранение в PostgreSQL, кеш в Redis, консольная программа, интеграционные тесты и известные уязвимости зависимостей;
- [README Gateway](gateway/README.md): запуск, отличия ответов от ДЗ 6, эндпоинты, новый отчёт `GET /api/reports/summary`, проверка через curl с записью в таблицы, перезапуском и двумя запросами отчёта подряд.

### Что реализовано

- Миграции goose в `ledger/migrations/`: таблицы `budgets` и `expenses` по условию, столбец `period` для периодов бюджетов из ДЗ 3 и индекс `(category, date)`.
- `ledger/internal/db`: `sql.Open("pgx", dsn)` и `Ping`, пул `SetMaxOpenConns(10)`, `SetMaxIdleConns(5)` и `SetConnMaxLifetime(30 * time.Minute)`. Строка подключения берётся из `DATABASE_URL` или из `DB_*`. При ошибке программа выводит понятное сообщение и завершается с кодом 1, при успехе пишет подключение в лог. Ошибка разбора строки подключения не цитирует её, чтобы не показать пароль. Если миграции не применены, программа не запускается и подсказывает команду goose.
- `SetBudget` выполняет UPSERT, `ListBudgets` и `ListTransactions` читают из таблиц. `AddTransaction` проверяет лимит и вставляет запись в одной транзакции БД, при превышении лимита возвращает ошибку и ничего не вставляет. `LoadBudgets` сохраняет бюджеты в порядке категорий, поэтому параллельные загрузки не попадают во взаимоблокировку.
- Дни транзакций и отчётов считаются в местном часовом поясе через `domain.Day` (до ДЗ 8 `ledger.Day`), в том числе в дни без полуночи при переходе на летнее время.
- `ledger/internal/cache`: клиент go-redis с настройками из `REDIS_ADDR`, `REDIS_DB`, `REDIS_PASSWORD` и записью в лог о подключении. `GetReportSummary(ctx, from, to)` берёт отчёт из Redis или считает его в PostgreSQL и сохраняет на 30 секунд, в логе видно `report cache miss` и `report cache hit`. Список бюджетов кешируется под ключом `budgets:all` на 30 секунд и сбрасывается после `SetBudget` и `LoadBudgets`. Каждый запрос к Redis ограничен 0.5 секунды без повторов: если Redis завис или недоступен, ledger пишет ошибку в лог и работает с PostgreSQL.
- Gateway подключает ledger при старте, корректно останавливается по `Ctrl+C` (повторный `Ctrl+C` завершает его сразу), отвечает 500 при ошибке хранилища и отдаёт отчёт по `GET /api/reports/summary`.
- Внешние зависимости: `github.com/jackc/pgx/v5` v5.7.4 и `github.com/redis/go-redis/v9` v9.18.0, последние версии, которые работают с Go 1.22.

## ДЗ 8. Архитектура и дизайн приложений

- `ledger/domain`: сущности, валидация, ошибки и доменные контракты хранения.
- `ledger/service`: интерфейс приложения и бизнес-логика с внедренными репозиториями. Проверка бюджета выполняется внутри атомарной операции, сохраняя защиту от конкурентного превышения лимита.
- `ledger/internal/repository/{pg,memory,cached}`: PostgreSQL, память для демо/тестов и кеш Redis. Репозитории не импортируют Gateway и не решают, допустим ли расход по бюджету.
- `ledger.New(ctx)`: конфигурация, подключения, проверка таблиц, сборка сервиса и функция закрытия ресурсов. Миграции запускаются отдельно, как в ДЗ 7.
- `api.NewRouter(svc, timeout)`: Gateway получает интерфейс сервиса, вызывает его методы и передает контекст запроса. HTTP-контракт не изменился.

[План выполнения](docs/hw8-plan.md), [подробное устройство Ledger](ledger/README.md#устройство), [инициализация Gateway](gateway/README.md#что-изменилось-в-дз-8).

Проверки: прежние тесты ДЗ 6-7, независимость экземпляров сервиса, отмена контекста, подмена сервисов и репозиториев, ошибки этапов сохранения, откат при ошибке и панике, PostgreSQL/Redis и curl-сценарии. Команды запуска приложений и миграций остались прежними.

## ДЗ 9. Основы многопоточности

`GET /api/reports/summary?from=2026-11-01&to=2026-11-30` сохраняет формат ответа ДЗ 7. При промахе кеша сервис получает категории с расходами в диапазоне и запускает отдельную горутину суммы для каждой. Результаты объединяются после `sync.WaitGroup.Wait`, поэтому нет одновременной записи в общую map. Отдельный `time.Ticker` пишет heartbeat раз в 400 мс. Сервис отменяет остальные вычисления при ошибке и дожидается всех работников и heartbeat перед возвратом.

Middleware передает каждому обработчику контекст с таймаутом 2 секунды. Флаг `-request-timeout` меняет его, например `go run . -request-timeout 50ms` из `gateway`. Таймаут и отмена дают 504 с `{"error":"request timed out"}`. Контекст доходит до SQL, чтение тела HTTP-запроса также ограничено дедлайном. Ошибки дат и JSON сохраняют 400. Кеш отчетов сохраняет TTL 30 секунд; попадание не запускает расчет.

[Чистый план](docs/hw9-plan.md), [расчет в Ledger](ledger/README.md#конкурентный-отчет-дз-9), [таймауты и проверка через curl](gateway/README.md#таймауты-дз-9), [задание для независимого ревью](docs/hw9-review-prompt.md).

Проверено: параллельный запуск категорий, отмена соседних работ при ошибке, отсутствие частичного кеша, остановка heartbeat, таймауты всех API-методов, медленное тело запроса, отмена заблокированного SQL и восстановление после нее. На живом Gateway обычный отчет вернул 200; блокировка БД дала 504 за 2.003 с при стандартном таймауте и за 0.052 с при `50ms`. Разрыв клиентского соединения отменил SQL и heartbeat. После проверки восстановлено значение по умолчанию 2 секунды.

## Тесты

В каталоге модуля: `go test ./...`. Все модули сразу, из корня репозитория:

```sh
for m in hw1 gateway ledger; do (cd "$m" && go test ./...); done
```

Интеграционные тесты ledger с PostgreSQL и Redis выполняются, если заданы `LEDGER_TEST_DATABASE_URL` и `LEDGER_TEST_REDIS_ADDR`, см. [README Ledger](ledger/README.md#тесты).
