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
- `AddTransaction(tx Transaction) error` — проверяет и добавляет транзакцию (сумма больше нуля, категория не пустая);
- `ListTransactions() []Transaction` — возвращает копию списка.

`main` добавляет несколько транзакций, одну заведомо некорректную, и выводит список:

```sh
cd ledger
go run .
```

```text
Ledger service started
Added "продукты на неделю": 1250.50 (еда)
Added "проездной": 300.00 (транспорт)
Added "кино": 2000.00 (развлечения)
Rejected "пустой чек": amount must be a positive number, got 0

Transactions (3):
ID  DATE        CATEGORY     AMOUNT   DESCRIPTION
1   2026-10-02  еда          1250.50  продукты на неделю
2   2026-10-02  транспорт    300.00   проездной
3   2026-10-02  развлечения  2000.00  кино
```

## Тесты

В каталоге модуля: `go test ./...`. Все модули сразу, из корня репозитория:

```sh
for m in hw1 gateway ledger; do (cd "$m" && go test ./...); done
```
