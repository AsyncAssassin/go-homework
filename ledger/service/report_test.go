package service

import (
	"context"
	"errors"
	"log"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AsyncAssassin/go-homework/ledger/domain"
)

type reportRepository struct {
	domain.ExpenseRepository
	categories []string
	listErr    error
	sum        func(context.Context, string, time.Time, time.Time) (float64, error)
}

func (r reportRepository) ListCategories(ctx context.Context, _, _ time.Time) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.categories, r.listErr
}
func (r reportRepository) SumByCategory(ctx context.Context, category string, from, to time.Time) (float64, error) {
	return r.sum(ctx, category, from, to)
}

type reportCache struct{ writes int }

func (*reportCache) GetReport(context.Context, time.Time, time.Time) ([]domain.CategoryTotal, bool) {
	return nil, false
}
func (c *reportCache) SetReport(context.Context, time.Time, time.Time, []domain.CategoryTotal) {
	c.writes++
}

type reportResult struct {
	report domain.ReportSummary
	err    error
}

func TestReportWorkers(t *testing.T) {
	for _, mode := range []string{"success", "storage failure", "parent cancellation", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if mode == "deadline" {
				var shortCancel context.CancelFunc
				ctx, shortCancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer shortCancel()
			}
			started := make(chan string, 3)
			release := make(chan struct{})
			var finished atomic.Int32
			failure := errors.New("sum failed")
			from, to := domain.Day(2026, 11, 1), domain.Day(2026, 11, 30)
			repo := reportRepository{categories: []string{"a", "b", "c"}, sum: func(ctx context.Context, category string, gotFrom, until time.Time) (float64, error) {
				defer finished.Add(1)
				if !gotFrom.Equal(from) || !until.Equal(domain.Day(2026, 12, 1)) {
					return 0, errors.New("incorrect report bounds")
				}
				started <- category
				select {
				case <-ctx.Done():
					return 0, ctx.Err()
				case <-release:
				}
				if mode == "storage failure" {
					if category == "b" {
						return 0, failure
					}
					<-ctx.Done()
					return 0, ctx.Err()
				}
				return map[string]float64{"a": 1.1, "b": 2.2, "c": 3.3}[category], nil
			}}
			cache := &reportCache{}
			svc := New(nil, repo, nil, cache)
			done := make(chan reportResult, 1)
			go func() { r, err := svc.GetReportSummary(ctx, from, to); done <- reportResult{r, err} }()
			// No worker can finish before all three enter SumByCategory. A sequential
			// implementation cannot pass this barrier.
			for range repo.categories {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("workers did not start concurrently")
				}
			}
			switch mode {
			case "success", "storage failure":
				close(release)
			case "parent cancellation":
				cancel()
			}
			var result reportResult
			select {
			case result = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("report did not stop")
			}
			if finished.Load() != 3 {
				t.Fatalf("returned with %d of 3 workers finished", finished.Load())
			}
			var wantErr error
			switch mode {
			case "storage failure":
				wantErr = failure
			case "parent cancellation":
				wantErr = context.Canceled
			case "deadline":
				wantErr = context.DeadlineExceeded
			}
			if !errors.Is(result.err, wantErr) {
				t.Fatalf("error = %v, want %v", result.err, wantErr)
			}
			if wantErr != nil {
				if cache.writes != 0 || len(result.report.Categories) != 0 || result.report.Total != 0 {
					t.Fatal("partial report was returned or cached")
				}
				return
			}
			want := []domain.CategoryTotal{{Category: "a", Amount: 1.1}, {Category: "b", Amount: 2.2}, {Category: "c", Amount: 3.3}}
			if !slices.Equal(result.report.Categories, want) || result.report.Total != 6.6 || cache.writes != 1 {
				t.Fatalf("report = %+v; cache writes = %d", result.report, cache.writes)
			}
		})
	}
}

// A buffered channel allows observation while the heartbeat is still running.
// log serializes calls to Write; no unsynchronized bytes.Buffer is shared.
type reportLog chan string

func (l reportLog) Write(p []byte) (int, error) { l <- string(p); return len(p), nil }

func TestReportHeartbeatStops(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "success"
		if canceled {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			logs := make(reportLog, 32)
			old := log.Writer()
			log.SetOutput(logs)
			defer log.SetOutput(old)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			release := make(chan struct{})
			repo := reportRepository{categories: []string{"food"}, sum: func(ctx context.Context, _ string, _, _ time.Time) (float64, error) {
				select {
				case <-release:
					return 12, nil
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			}}
			done := make(chan error, 1)
			go func() {
				_, err := New(nil, repo, nil, nil).GetReportSummary(ctx, domain.Day(2026, 11, 1), domain.Day(2026, 11, 2))
				done <- err
			}()
			select {
			case line := <-logs:
				if !strings.Contains(line, "report calculation in progress") {
					t.Fatalf("heartbeat = %q", line)
				}
			case <-ctx.Done():
				t.Fatal("heartbeat did not tick")
			}
			if canceled {
				cancel()
			} else {
				close(release)
			}
			select {
			case err := <-done:
				if canceled && !errors.Is(err, context.Canceled) || !canceled && err != nil {
					t.Fatalf("error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("report did not finish")
			}
			stopped := false
			for len(logs) > 0 {
				if strings.Contains(<-logs, "report heartbeat stopped") {
					stopped = true
				}
			}
			if !stopped {
				t.Fatal("heartbeat did not log its stop")
			}
			select {
			case line := <-logs:
				t.Fatalf("log after method returned: %q", line)
			case <-time.After(450 * time.Millisecond):
			}
		})
	}
}

func TestReportCategoryFailure(t *testing.T) {
	failure := errors.New("categories unavailable")
	cache := &reportCache{}
	repo := reportRepository{listErr: failure, sum: func(context.Context, string, time.Time, time.Time) (float64, error) {
		t.Error("sum called after listing failure")
		return 0, nil
	}}
	_, err := New(nil, repo, nil, cache).GetReportSummary(context.Background(), domain.Day(2026, 11, 1), domain.Day(2026, 11, 2))
	if !errors.Is(err, failure) || cache.writes != 0 {
		t.Fatalf("error = %v; cache writes = %d", err, cache.writes)
	}
}
