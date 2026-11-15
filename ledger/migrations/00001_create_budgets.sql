-- +goose Up
-- period keeps the budget periods of homework 3: '' (all time), 'month' or 'year'.
CREATE TABLE budgets (
    id           SERIAL PRIMARY KEY,
    category     TEXT UNIQUE NOT NULL,
    limit_amount NUMERIC(14,2) NOT NULL CHECK (limit_amount > 0),
    period       TEXT NOT NULL DEFAULT '' CHECK (period IN ('', 'month', 'year'))
);

-- +goose Down
DROP TABLE budgets;
