# Язык Go — домашние задания

| ДЗ | Тема | Где лежит код |
|----|------|---------------|
| 1 | Введение в Go | `hw1/` |
| 2 | Функции: сервисы Gateway и Ledger | `gateway/`, `ledger/` |
| 3 | Ввод-вывод, обработка ошибок: бюджеты в Ledger | `ledger/` |
| 4 | ООП: валидация через интерфейс в Ledger | `ledger/` |
| 5 | Веб-приложения: REST API в Gateway | `gateway/`, `ledger/` |

Состояние репозитория на момент сдачи каждого ДЗ отмечено тегом `hwN`.

## Требования

- Go 1.22+ (проверено на 1.22.12 и 1.26.5)

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

С ДЗ 5 ledger стал пакетом, который импортирует Gateway, демо переехало в `ledger/cmd/ledger`, а `AddTransaction` возвращает ещё и сохранённую транзакцию. Запуск демо: `cd ledger && go run ./cmd/ledger`. Оно показывает также бюджеты из ДЗ 3 и проверку через интерфейс из ДЗ 4, примеры вывода приведены в их разделах. Состояние на момент сдачи ДЗ 2 сохранено в теге [`hw2`](https://github.com/AsyncAssassin/go-homework/tree/hw2).

## ДЗ 3. Ввод-вывод и обработка ошибок: бюджеты в Ledger

```sh
cd ledger
go run ./cmd/ledger   # другой файл бюджетов: go run ./cmd/ledger -budgets path/to/file.json
```

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
  rejected "пустой чек": invalid amount 0: must be from 0.01 to 1e+12

Budget loading errors:
  broken JSON: load budgets: decode JSON: unexpected EOF
  wrong type: load budgets: decode JSON: json: cannot unmarshal string into Go struct field Budget.limit of type float64
  invalid limit: load budgets: budget #2: invalid limit -5 for "такси": must be from 0.01 to 1e+12
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
go run ./cmd/ledger
```

Проверка данных отделена от бизнес-логики интерфейсом `Validatable` (`ledger/validate.go`):

```go
type Validatable interface {
	Validate() error
}
```

### Что реализовано

- `Transaction.Validate()`: сумма от 0.01 до 10¹² (ноль, отрицательные суммы, NaN и бесконечность не проходят), категория не пустая, заданная дата от 2 января 1 года до 30 декабря 9999 года по UTC. Запас в сутки нужен, чтобы в любом часовом поясе дата записывалась как `YYYY-MM-DD` с годом от 1 до 9999, а результат проверки зависел только от момента времени. Пустая дата допустима: при добавлении подставляется текущая.
- `Budget.Validate()`: категория не пустая, лимит от 0.01 до 10¹², период `month`, `year` или пусто.
- `Validate()` только возвращает ошибку и ничего не логирует, результат печатает вызывающий код.
- `AddTransaction`, `SetBudget` и `LoadBudgets` вызывают `Validate()` до сохранения. Если он вернул ошибку, объект не добавляется, а ошибка возвращается вызывающему коду. Проверки бюджетов из ДЗ 3 работают как раньше.
- `CheckValid(v Validatable) error` вызывает `v.Validate()` и одинаково работает с транзакциями, бюджетами и указателями на них. Для `nil`, в том числе для nil-указателя вроде `(*Transaction)(nil)`, возвращает ошибку `nothing to validate`, а не панику.

### Пример вывода

Вывод из ДЗ 3 не изменился, в конец добавлен раздел, где `CheckValid` проверяет разные типы через интерфейс. Первый столбец (`%T`) показывает, какой тип лежит в интерфейсе. С ДЗ 5 типы находятся в пакете `ledger`, поэтому они названы `ledger.Transaction` и `ledger.Budget` (в ДЗ 4 `main.Transaction` и `main.Budget`):

```text
Validation through the Validatable interface:
  ledger.Transaction  450.00 "ланч" (еда)               ok
  ledger.Transaction  -100.00 "возврат" (еда)           invalid: invalid amount -100: must be from 0.01 to 1e+12
  ledger.Transaction  300.00 "такси" (no category)      invalid: category must not be empty
  ledger.Transaction  50.00 "кофе" (еда) on 0000-03-01  invalid: invalid date 0000-03-01: must be from 0001-01-02 to 9999-12-30 UTC
  ledger.Budget       кафе 3000.00 a month              ok
  ledger.Budget       кафе 0.00                         invalid: invalid limit 0 for "кафе": must be from 0.01 to 1e+12
  ledger.Budget       no category 1000.00               invalid: category must not be empty
```

Табличные тесты `Validate()` и `CheckValid` находятся в `ledger/ledger_test.go`.

## ДЗ 5. Веб-приложения: REST API в Gateway

```sh
cd gateway
go run .              # другой адрес: go run . -addr :9090
```

Gateway импортирует модуль `ledger` из этого же репозитория: в `gateway/go.mod` указаны `require` и `replace github.com/AsyncAssassin/go-homework/ledger => ../ledger`, go.work не нужен. Хранилище, как в ДЗ 2-4, в памяти: после остановки сервера данные пропадают.

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

Поля запросов:
- `amount` и `limit`: число от 0.01 до 10¹², округляется до копеек;
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

Проверка бюджета и добавление транзакции выполняются в ledger под одной блокировкой, поэтому параллельные запросы не превышают лимит вместе.

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
400 {"error":"invalid amount 0: must be from 0.01 to 1e+12"}
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

Тесты HTTP-слоя находятся в `gateway/internal/api`: обработчики проверяются через `httptest` вместе с маршрутами и middleware.

## Тесты

В каталоге модуля: `go test ./...`. Все модули сразу, из корня репозитория:

```sh
for m in hw1 gateway ledger; do (cd "$m" && go test ./...); done
```
