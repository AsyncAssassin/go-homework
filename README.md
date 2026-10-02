# Язык Go — домашние задания

| ДЗ | Тема | Где лежит код |
|----|------|---------------|
| 1 | Введение в Go | `hw1/` |
| 2 | Функции: сервисы Gateway и Ledger | `gateway/`, `ledger/` |
| 3 | Ввод-вывод, обработка ошибок: бюджеты в Ledger | `ledger/` |

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
User: artur
Arguments (2):
  1: "hello"
  2: "big world"
Go version: go1.26.5
```

## ДЗ 2. Функции: сервисы Gateway и Ledger

### Gateway — HTTP-шлюз

HTTP-сервер на порту 8080, `GET /ping` отвечает `pong` со статусом 200.

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
Content-Type: text/plain; charset=utf-8
Content-Length: 4

pong
```

Остановить сервер — `Ctrl+C`. Если порт занят, сервис сразу завершится с ошибкой `address already in use`.

### Ledger — бизнес-логика

Хранит транзакции в памяти:
- `AddTransaction(tx Transaction) error` — проверяет и добавляет транзакцию (сумма не меньше 0.01, категория не пустая);
- `ListTransactions() []Transaction` — возвращает копию списка.

Запуск: `cd ledger && go run .`. Сейчас ledger показывает и бюджеты из ДЗ 3, пример вывода — в следующем разделе. Состояние на момент сдачи ДЗ 2 — по тегу [`hw2`](https://github.com/AsyncAssassin/go-homework/tree/hw2).

## ДЗ 3. Ввод-вывод и обработка ошибок: бюджеты в Ledger

- `Budget{Category, Limit}` и хранилище бюджетов `map[string]Budget` (ключ — категория).
- `SetBudget(b Budget) error` добавляет бюджет или обновляет существующий.
- `AddTransaction` проверяет бюджет категории: если сумма трат вместе с новой транзакцией больше лимита, возвращается ошибка `*BudgetExceededError` (`errors.Is(err, ErrBudgetExceeded)`), и транзакция не сохраняется. Ровно до лимита — можно; категории без бюджета не ограничены. Суммы сравниваются в копейках, чтобы не мешала погрешность float.
- `LoadBudgets(r io.Reader) error` читает JSON-массив бюджетов и применяет его через `SetBudget`, только если весь файл корректный. Ошибки чтения и разбора возвращаются с понятным описанием.

Начальные бюджеты задаются в коде через `SetBudget`, затем загружаются из `ledger/budgets.json` (`os.Open` + `bufio.NewReader`):

```json
[
  {"category": "еда", "limit": 5000},
  {"category": "транспорт", "limit": 2500},
  {"category": "развлечения", "limit": 3000}
]
```

```sh
cd ledger
go run .              # другой файл бюджетов: go run . -budgets path/to/file.json
```

```text
Ledger service started
Budgets loaded from budgets.json

Budgets (3):
CATEGORY     LIMIT    SPENT  LEFT
еда          5000.00  0.00   5000.00
развлечения  3000.00  0.00   3000.00
транспорт    2500.00  0.00   2500.00

Adding transactions:
  added    "продукты на неделю" 1250.50 (еда)
  added    "проездной" 2000.00 (транспорт)
  added    "кафе" 3500.00 (еда)
  rejected "ресторан": budget exceeded for "еда": spent 4750.50 + new 500.00 > limit 5000.00, only 249.50 left
  added    "хлеб и молоко" 249.50 (еда)
  added    "лекарства" 700.00 (здоровье)
  rejected "пустой чек": invalid amount 0: must be a finite number of at least 0.01

Budget loading errors:
  broken JSON: load budgets: decode JSON: unexpected EOF
  wrong type: load budgets: decode JSON: json: cannot unmarshal string into Go struct field Budget.limit of type float64
  invalid limit: load budgets: budget #2: invalid limit -5 for "такси": must be a finite number of at least 0.01
  missing file: load budgets: open missing.json: no such file or directory

Transactions (5):
ID  DATE        CATEGORY   AMOUNT   DESCRIPTION
1   2026-10-02  еда        1250.50  продукты на неделю
2   2026-10-02  транспорт  2000.00  проездной
3   2026-10-02  еда        3500.00  кафе
4   2026-10-02  еда        249.50   хлеб и молоко
5   2026-10-02  здоровье   700.00   лекарства

Budgets (3):
CATEGORY     LIMIT    SPENT    LEFT
еда          5000.00  5000.00  0.00
развлечения  3000.00  0.00     3000.00
транспорт    2500.00  2000.00  500.00
```

Отклонённые транзакции («ресторан», «пустой чек») в список не попали. После неудачных загрузок бюджеты не изменились («кафе» и «такси» не появились).

## Тесты

В каталоге модуля: `go test ./...`. Все модули сразу, из корня репозитория:

```sh
for m in hw1 gateway ledger; do (cd "$m" && go test ./...); done
```
