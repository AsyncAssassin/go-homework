package ledger

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The tests change the storage of the package, so they do not run in
// parallel. The report from PostgreSQL is checked by
// TestPostgresReportSummary, the cached one by TestReportCache.

func TestGetReportSummary(t *testing.T) {
	Reset()
	day := func(d int) time.Time {
		return time.Date(2025, time.September, d, 0, 0, 0, 0, time.Local)
	}
	for _, tx := range []Transaction{
		{Amount: 450, Category: "еда", Date: day(10)},
		{Amount: 120, Category: "транспорт", Date: day(11)},
		{Amount: 0.1, Category: "еда", Date: day(30)},
		{Amount: 0.2, Category: "еда", Date: day(30)},
		{Amount: 30, Category: "ёлка", Date: day(1)},
		{Amount: 50, Category: "кафе", Date: day(0)},   // August 31
		{Amount: 999, Category: "кафе", Date: day(31)}, // October 1
	} {
		if _, err := AddTransaction(tx); err != nil {
			t.Fatalf("AddTransaction(%+v) error = %v", tx, err)
		}
	}

	tests := []struct {
		name     string
		from, to time.Time
		want     ReportSummary
	}{
		{
			name: "month",
			from: day(1),
			to:   day(30),
			// Sorted by bytes: "ё" comes after "т". 450 + 0.1 + 0.2 is
			// rounded to kopecks.
			want: ReportSummary{
				From: day(1),
				To:   day(30),
				Categories: []CategoryTotal{
					{Category: "еда", Amount: 450.3},
					{Category: "транспорт", Amount: 120},
					{Category: "ёлка", Amount: 30},
				},
				Total: 600.3,
			},
		},
		{
			name: "one day, to earlier in the day than from",
			from: day(30).Add(23 * time.Hour),
			to:   day(30).Add(time.Hour),
			want: ReportSummary{
				From:       day(30),
				To:         day(30),
				Categories: []CategoryTotal{{Category: "еда", Amount: 0.3}},
				Total:      0.3,
			},
		},
		{
			name: "no expenses",
			from: day(12),
			to:   day(29),
			want: ReportSummary{From: day(12), To: day(29)},
		},
		{
			// The first and the last day are valid in every time zone, the
			// last one until its end.
			name: "whole range",
			from: Day(1, time.January, 2),
			to:   Day(9999, time.December, 30).Add(23 * time.Hour),
			want: ReportSummary{
				From: Day(1, time.January, 2),
				To:   Day(9999, time.December, 30),
				Categories: []CategoryTotal{
					{Category: "еда", Amount: 450.3},
					{Category: "кафе", Amount: 1049},
					{Category: "транспорт", Amount: 120},
					{Category: "ёлка", Amount: 30},
				},
				Total: 1649.3,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetReportSummary(context.Background(), tt.from, tt.to)
			if err != nil {
				t.Fatalf("GetReportSummary() error = %v", err)
			}
			if !reportsEqual(got, tt.want) {
				t.Errorf("GetReportSummary() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestGetReportSummaryError(t *testing.T) {
	Reset()
	day := time.Date(2025, time.September, 2, 0, 0, 0, 0, time.Local)
	tests := []struct {
		name     string
		from, to time.Time
		wantErr  error
		wantMsg  string
	}{
		{
			name:    "range ends before it starts",
			from:    day,
			to:      day.Add(-time.Minute),
			wantErr: ErrInvalidRange,
			wantMsg: "invalid report range: from 2025-09-02 is after to 2025-09-01",
		},
		// The days are local: the same moments in UTC would fall on other
		// days in other time zones.
		{
			// PostgreSQL has no year 0.
			name:    "year 0",
			from:    time.Date(0, time.January, 1, 12, 0, 0, 0, time.Local),
			to:      day,
			wantErr: ErrInvalidDate,
			wantMsg: "invalid date 0000-01-01: must be from 0001-01-02 to 9999-12-30",
		},
		{
			name:    "before the first day",
			from:    time.Date(1, time.January, 1, 12, 0, 0, 0, time.Local),
			to:      day,
			wantErr: ErrInvalidDate,
			wantMsg: "invalid date 0001-01-01: must be from 0001-01-02 to 9999-12-30",
		},
		{
			name:    "after the last day",
			from:    day,
			to:      time.Date(9999, time.December, 31, 12, 0, 0, 0, time.Local),
			wantErr: ErrInvalidDate,
			wantMsg: "invalid date 9999-12-31: must be from 0001-01-02 to 9999-12-30",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetReportSummary(context.Background(), tt.from, tt.to)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetReportSummary() error = %v, want %v", err, tt.wantErr)
			}
			if err.Error() != tt.wantMsg {
				t.Errorf("error = %q, want %q", err, tt.wantMsg)
			}
		})
	}
}
