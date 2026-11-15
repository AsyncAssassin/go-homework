package domain

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestTransactionValidate(t *testing.T) {
	t.Parallel()

	date := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.Local)
	// Fixed zones at the edges of the real ones: the result must depend only
	// on the moment, not on the zone it is given in or on the local zone.
	west := time.FixedZone("UTC-12", -12*60*60)
	east := time.FixedZone("UTC+14", 14*60*60)
	latest := maxDate.Add(-time.Nanosecond)

	tests := []struct {
		name    string
		tx      Transaction
		wantErr error
	}{
		{name: "valid", tx: Transaction{Amount: 450, Category: "еда", Description: "ланч", Date: date}},
		{name: "one kopeck", tx: Transaction{Amount: 0.01, Category: "еда"}},
		{name: "upper limit", tx: Transaction{Amount: maxAmount, Category: "еда"}},
		{name: "category with spaces and capitals", tx: Transaction{Amount: 450, Category: " Еда "}},
		{name: "zero amount", tx: Transaction{Amount: 0, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "negative amount", tx: Transaction{Amount: -450, Category: "еда"}, wantErr: ErrInvalidAmount},
		// 0.009 would round to one kopeck: the raw amount is what counts.
		{name: "less than a kopeck", tx: Transaction{Amount: 0.009, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "just over the upper limit", tx: Transaction{Amount: maxAmount + 0.01, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "NaN amount", tx: Transaction{Amount: math.NaN(), Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "infinite amount", tx: Transaction{Amount: math.Inf(1), Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "empty category", tx: Transaction{Amount: 450}, wantErr: ErrEmptyCategory},
		{name: "blank category", tx: Transaction{Amount: 450, Category: " \t"}, wantErr: ErrEmptyCategory},
		// PostgreSQL stores neither NUL characters nor invalid UTF-8 in TEXT.
		{name: "NUL in the category", tx: Transaction{Amount: 450, Category: "е\x00да"}, wantErr: ErrInvalidCategory},
		{name: "invalid UTF-8 in the category", tx: Transaction{Amount: 450, Category: "еда\xff"}, wantErr: ErrInvalidCategory},
		{name: "NUL in the description", tx: Transaction{Amount: 450, Category: "еда", Description: "a\x00b"}, wantErr: ErrInvalidDescription},
		{name: "invalid UTF-8 in the description", tx: Transaction{Amount: 450, Category: "еда", Description: "\xffланч"}, wantErr: ErrInvalidDescription},
		{name: "category of 100 letters", tx: Transaction{Amount: 450, Category: strings.Repeat("я", maxCategoryLength)}},
		{name: "spaces around a long category do not count", tx: Transaction{Amount: 450, Category: " " + strings.Repeat("a", maxCategoryLength) + " "}},
		{name: "category of 101 letters", tx: Transaction{Amount: 450, Category: strings.Repeat("a", maxCategoryLength+1)}, wantErr: ErrInvalidCategory},
		{name: "zero date is filled in later", tx: Transaction{Amount: 450, Category: "еда"}},
		{name: "zero date in a western zone", tx: Transaction{Amount: 450, Category: "еда", Date: time.Time{}.In(west)}},
		{name: "earliest date", tx: Transaction{Amount: 450, Category: "еда", Date: minDate}},
		{name: "earliest date in a western zone", tx: Transaction{Amount: 450, Category: "еда", Date: minDate.In(west)}},
		{
			name:    "just before the earliest date",
			tx:      Transaction{Amount: 450, Category: "еда", Date: minDate.Add(-time.Nanosecond)},
			wantErr: ErrInvalidDate,
		},
		{
			// The year is still 1 in this zone, but the moment is too early.
			name:    "just before the earliest date in an eastern zone",
			tx:      Transaction{Amount: 450, Category: "еда", Date: minDate.Add(-time.Nanosecond).In(east)},
			wantErr: ErrInvalidDate,
		},
		{name: "latest date", tx: Transaction{Amount: 450, Category: "еда", Date: latest}},
		{name: "latest date in an eastern zone", tx: Transaction{Amount: 450, Category: "еда", Date: latest.In(east)}},
		{
			name:    "after the latest date",
			tx:      Transaction{Amount: 450, Category: "еда", Date: maxDate},
			wantErr: ErrInvalidDate,
		},
		{
			// The year is still 9999 in this zone, but the moment is too late.
			name:    "after the latest date in a western zone",
			tx:      Transaction{Amount: 450, Category: "еда", Date: maxDate.In(west)},
			wantErr: ErrInvalidDate,
		},
		{
			// The year is 1 in UTC but 0 west of it: the margin rejects it.
			name:    "first hour of the year 1 in UTC",
			tx:      Transaction{Amount: 450, Category: "еда", Date: time.Date(1, time.January, 1, 1, 0, 0, 0, time.UTC)},
			wantErr: ErrInvalidDate,
		},
		{
			// The year is 9999 in UTC but 10000 east of it: the margin rejects it.
			name:    "last hour of the year 9999 in UTC",
			tx:      Transaction{Amount: 450, Category: "еда", Date: time.Date(9999, time.December, 31, 23, 0, 0, 0, time.UTC)},
			wantErr: ErrInvalidDate,
		},
		{
			name:    "year 0",
			tx:      Transaction{Amount: 450, Category: "еда", Date: time.Date(0, time.June, 1, 12, 0, 0, 0, time.Local)},
			wantErr: ErrInvalidDate,
		},
		{
			name:    "year 10000",
			tx:      Transaction{Amount: 450, Category: "еда", Date: time.Date(10000, time.June, 1, 12, 0, 0, 0, time.Local)},
			wantErr: ErrInvalidDate,
		},
		{
			name:    "the amount is checked first",
			tx:      Transaction{Amount: 0, Category: " "},
			wantErr: ErrInvalidAmount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.tx.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestBudgetValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		budget  Budget
		wantErr error
	}{
		{name: "valid", budget: Budget{Category: "еда", Limit: 5000}},
		{name: "monthly", budget: Budget{Category: "еда", Limit: 5000, Period: PeriodMonth}},
		{name: "yearly with spaces and capitals", budget: Budget{Category: " Отпуск ", Limit: 90000, Period: " Year "}},
		{name: "one kopeck", budget: Budget{Category: "еда", Limit: 0.01}},
		{name: "upper limit", budget: Budget{Category: "еда", Limit: maxAmount}},
		{name: "zero limit", budget: Budget{Category: "еда", Limit: 0}, wantErr: ErrInvalidLimit},
		{name: "negative limit", budget: Budget{Category: "еда", Limit: -5000}, wantErr: ErrInvalidLimit},
		// 0.009 would round to one kopeck: the raw limit is what counts.
		{name: "less than a kopeck", budget: Budget{Category: "еда", Limit: 0.009}, wantErr: ErrInvalidLimit},
		{name: "just over the upper limit", budget: Budget{Category: "еда", Limit: maxAmount + 0.01}, wantErr: ErrInvalidLimit},
		{name: "NaN limit", budget: Budget{Category: "еда", Limit: math.NaN()}, wantErr: ErrInvalidLimit},
		{name: "infinite limit", budget: Budget{Category: "еда", Limit: math.Inf(1)}, wantErr: ErrInvalidLimit},
		{name: "empty category", budget: Budget{Limit: 5000}, wantErr: ErrEmptyCategory},
		{name: "blank category", budget: Budget{Category: "  ", Limit: 5000}, wantErr: ErrEmptyCategory},
		{name: "NUL in the category", budget: Budget{Category: "е\x00да", Limit: 5000}, wantErr: ErrInvalidCategory},
		{name: "invalid UTF-8 in the category", budget: Budget{Category: "\xffеда", Limit: 5000}, wantErr: ErrInvalidCategory},
		{name: "category of 100 letters", budget: Budget{Category: strings.Repeat("я", maxCategoryLength), Limit: 5000}},
		{name: "category of 101 letters", budget: Budget{Category: strings.Repeat("я", maxCategoryLength+1), Limit: 5000}, wantErr: ErrInvalidCategory},
		{name: "the category text is checked before the limit", budget: Budget{Category: "е\x00да", Limit: -1}, wantErr: ErrInvalidCategory},
		{name: "unknown period", budget: Budget{Category: "еда", Limit: 5000, Period: "week"}, wantErr: ErrInvalidPeriod},
		{name: "the category is checked first", budget: Budget{Limit: -1, Period: "week"}, wantErr: ErrEmptyCategory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.budget.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckValid(t *testing.T) {
	t.Parallel()

	// One function checks values of different types through the interface.
	tests := []struct {
		name    string
		v       Validatable
		wantErr error
	}{
		{name: "valid transaction", v: Transaction{Amount: 450, Category: "еда"}},
		{name: "invalid transaction", v: Transaction{Amount: -1, Category: "еда"}, wantErr: ErrInvalidAmount},
		{name: "pointer to a transaction", v: &Transaction{Amount: 450}, wantErr: ErrEmptyCategory},
		{name: "valid budget", v: Budget{Category: "еда", Limit: 5000}},
		{name: "invalid budget", v: Budget{Category: "еда"}, wantErr: ErrInvalidLimit},
		{name: "pointer to a budget", v: &Budget{Category: "еда", Limit: 5000, Period: PeriodYear}},
		{name: "nil", v: nil, wantErr: ErrNothingToValidate},
		{name: "nil pointer to a transaction", v: (*Transaction)(nil), wantErr: ErrNothingToValidate},
		{name: "nil pointer to a budget", v: (*Budget)(nil), wantErr: ErrNothingToValidate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CheckValid(tt.v)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("CheckValid() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestPeriodLabel(t *testing.T) {
	// The first and the last half hour of October 2026 in the local time zone.
	// Shown in UTC-12 and UTC+14, at least one of them falls on another month,
	// whatever the local zone is, but the labels must still say October.
	first := time.Date(2026, time.October, 1, 0, 30, 0, 0, time.Local)
	last := time.Date(2026, time.October, 31, 23, 30, 0, 0, time.Local)
	west := time.FixedZone("UTC-12", -12*60*60)
	east := time.FixedZone("UTC+14", 14*60*60)

	tests := []struct {
		period Period
		want   string
	}{
		{period: PeriodNone, want: ""},
		{period: PeriodMonth, want: "2026-10"},
		{period: PeriodYear, want: "2026"},
	}
	for _, date := range []time.Time{first, first.In(west), last, last.In(east)} {
		for _, tt := range tests {
			if got := tt.period.Label(date); got != tt.want {
				t.Errorf("Period(%q).Label(%v) = %q, want %q", tt.period, date, got, tt.want)
			}
		}
	}
}

func TestPeriodBounds(t *testing.T) {
	t.Parallel()

	day := Day
	tests := []struct {
		name      string
		period    Period
		t         time.Time
		wantStart time.Time
		wantEnd   time.Time
		wantOK    bool
	}{
		{name: "last hour of a month", period: PeriodMonth, t: day(2026, time.October, 31).Add(23 * time.Hour), wantStart: day(2026, time.October, 1), wantEnd: day(2026, time.November, 1), wantOK: true},
		// In America/Asuncion this month starts at 01:00: the clocks skipped
		// midnight of its first day.
		{name: "month that starts after midnight", period: PeriodMonth, t: day(2023, time.October, 15), wantStart: day(2023, time.October, 1), wantEnd: day(2023, time.November, 1), wantOK: true},
		{name: "December", period: PeriodMonth, t: day(2026, time.December, 15), wantStart: day(2026, time.December, 1), wantEnd: day(2027, time.January, 1), wantOK: true},
		{name: "local day given in another zone", period: PeriodMonth, t: day(2026, time.November, 1).In(time.FixedZone("UTC-12", -12*60*60)), wantStart: day(2026, time.November, 1), wantEnd: day(2026, time.December, 1), wantOK: true},
		{name: "first day of a year", period: PeriodYear, t: day(2026, time.January, 1), wantStart: day(2026, time.January, 1), wantEnd: day(2027, time.January, 1), wantOK: true},
		{name: "no period", period: PeriodNone, t: day(2026, time.May, 5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			start, end, ok := tt.period.Bounds(tt.t)
			if !start.Equal(tt.wantStart) || !end.Equal(tt.wantEnd) || ok != tt.wantOK {
				t.Fatalf("bounds() = %v, %v, %v; want %v, %v, %v", start, end, ok, tt.wantStart, tt.wantEnd, tt.wantOK)
			}
			// The bounds and the labels split time the same way.
			if ok && (tt.period.Label(start) != tt.period.Label(tt.t) ||
				tt.period.Label(end.Add(-time.Nanosecond)) != tt.period.Label(tt.t) ||
				tt.period.Label(end) == tt.period.Label(tt.t)) {
				t.Errorf("bounds() = %v, %v do not match the label %q", start, end, tt.period.Label(tt.t))
			}
		})
	}
}

func TestDay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		zone  string
		year  int
		month time.Month
		day   int
		want  string // RFC 3339
	}{
		{name: "ordinary day", zone: "Asia/Tbilisi", year: 2026, month: time.November, day: 15, want: "2026-11-15T00:00:00+04:00"},
		// The clocks go from 00:00 to 01:00, so the day starts at 01:00.
		{name: "no midnight in Santiago", zone: "America/Santiago", year: 2026, month: time.September, day: 6, want: "2026-09-06T01:00:00-03:00"},
		{name: "no midnight in Havana", zone: "America/Havana", year: 2026, month: time.March, day: 8, want: "2026-03-08T01:00:00-04:00"},
		{name: "first day of a month without midnight", zone: "America/Asuncion", year: 2023, month: time.October, day: 1, want: "2023-10-01T01:00:00-03:00"},
		// East of UTC time.Date already gives 01:00.
		{name: "no midnight in Cairo", zone: "Africa/Cairo", year: 2026, month: time.April, day: 24, want: "2026-04-24T01:00:00+03:00"},
		// Samoa skipped this day, so the next one starts instead.
		{name: "day that did not exist", zone: "Pacific/Apia", year: 2011, month: time.December, day: 30, want: "2011-12-31T00:00:00+14:00"},
		{name: "day 0 is the last day of the previous month", zone: "UTC", year: 2026, month: time.October, day: 0, want: "2026-09-30T00:00:00Z"},
		{name: "month 13 is January of the next year", zone: "UTC", year: 2026, month: 13, day: 1, want: "2027-01-01T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			loc, err := time.LoadLocation(tt.zone)
			if err != nil {
				t.Fatal(err)
			}
			if got := dayStart(tt.year, tt.month, tt.day, loc).Format(time.RFC3339); got != tt.want {
				t.Errorf("dayStart(%d, %d, %d, %s) = %s, want %s", tt.year, tt.month, tt.day, tt.zone, got, tt.want)
			}
		})
	}
}
